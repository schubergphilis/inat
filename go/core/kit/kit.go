// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package kit parses and validates `kit.json` manifests.
//
// Mirrors the Rust implementation's `src/core/kit.rs`. JSON schema is identical so manifests
// written by the Rust binary parse cleanly here.
package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/schubergphilis/inat/core/frontmatter"
	"github.com/schubergphilis/inat/core/naming"
	"github.com/schubergphilis/inat/errs"
)

// MaxDescriptionLength bounds the soft cap on a rule's frontmatter
// `description` field. Descriptions above this length are rejected at
// validate time so the rendered managed-section bullet stays terse.
const MaxDescriptionLength = 200

// FileType is the kind of file an entry in `kit.json` ships.
type FileType string

const (
	Skill FileType = "skill"
	Rule  FileType = "rule"
)

// Priority is the load semantics for a rule. Skills must not have one.
type Priority string

const (
	Always     Priority = "always"
	Contextual Priority = "contextual"
)

// FileEntry describes one file (skill directory or rule file) inside a kit.
type FileEntry struct {
	Path     string    `json:"path"`
	FileType FileType  `json:"type"`
	Priority *Priority `json:"priority,omitempty"`
}

// KitConfig is the parsed form of a `kit.json` manifest.
type KitConfig struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Author      *string     `json:"author,omitempty"`
	License     *string     `json:"license,omitempty"`
	Files       []FileEntry `json:"files"`
}

// ParseKit deserialises kit.json content into a KitConfig.
//
// Returns *errs.Error with Kind=JSON on malformed input, missing required
// fields (`name`, `files`), or invalid file types. Mirrors Rust's serde
// behaviour where required fields without `#[serde(default)]` error at
// parse time.
func ParseKit(content string) (KitConfig, error) {
	if err := requireFields(content, "name", "files"); err != nil {
		return KitConfig{}, err
	}
	var k KitConfig
	if err := json.Unmarshal([]byte(content), &k); err != nil {
		return KitConfig{}, errs.WrapJSON(err)
	}
	for i, entry := range k.Files {
		if entry.FileType != Skill && entry.FileType != Rule {
			return KitConfig{}, errs.WrapJSON(fmt.Errorf(
				"files[%d]: unknown type %q", i, entry.FileType))
		}
	}
	return k, nil
}

// requireFields asserts a JSON object literally contains each named key
// before the second-pass strongly-typed unmarshal runs. Used to preserve
// Rust serde's "missing required field" parity.
func requireFields(content string, fields ...string) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &probe); err != nil {
		return errs.WrapJSON(err)
	}
	for _, f := range fields {
		if _, ok := probe[f]; !ok {
			return errs.WrapJSON(fmt.Errorf("missing required field '%s'", f))
		}
	}
	return nil
}

// ValidateKit applies the structural rules:
//   - kit.Name passes core/naming.ValidateName.
//   - Files list is non-empty.
//   - Skill entries reject `priority`.
//   - Each entry's derived name passes core/naming.ValidateName.
//
// Mirrors `validate_kit` in Rust. Returns *errs.Error (KitValidation or
// InvalidName).
func ValidateKit(k *KitConfig) error {
	if err := naming.ValidateName(k.Name); err != nil {
		return err
	}
	if len(k.Files) == 0 {
		return errs.NewKitValidation("kit must contain at least one file entry")
	}
	for _, entry := range k.Files {
		if entry.FileType == Skill && entry.Priority != nil {
			return errs.NewKitValidation(fmt.Sprintf(
				"skill entry '%s' must not have a priority (priority applies to rules only)",
				entry.Path))
		}
		name := DeriveEntryName(entry.Path, entry.FileType)
		if err := naming.ValidateName(name); err != nil {
			return err
		}
	}
	return nil
}

// DeriveEntryName returns the delivered name for a file entry, computed
// from its path and type.
//
//   - Skill: last path component (directory name), trailing slashes
//     stripped.
//   - Rule: filename without the `.md` extension.
//
// Mirrors `derive_entry_name` in Rust.
func DeriveEntryName(path string, ft FileType) string {
	path = strings.TrimRight(path, "/")
	last := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		last = path[i+1:]
	}
	switch ft {
	case Skill:
		return last
	case Rule:
		return strings.TrimSuffix(last, ".md")
	}
	return last
}

// DeriveNames returns derived names for every entry in kit.Files.
// Mirrors `derive_names` in Rust.
func DeriveNames(k *KitConfig) []string {
	out := make([]string, 0, len(k.Files))
	for _, e := range k.Files {
		out = append(out, DeriveEntryName(e.Path, e.FileType))
	}
	return out
}

// ReadAndValidateKit finds `kit.json` in a list of (relative path, content)
// pairs (typically the output of a tree walk), parses it, validates the
// kit.json, and validates each rule entry's frontmatter.
//
// Returns:
//   - the parsed KitConfig.
//   - warnings: human-readable strings the caller should print to
//     stderr (e.g. "rule 'foo' has no frontmatter — rendered without
//     description"). Warnings never block; an empty slice means no
//     soft issues.
//   - err: non-nil on any hard validation failure (kit.json malformed,
//     name mismatch, oversize description, multi-line description,
//     malformed YAML).
func ReadAndValidateKit(allFiles []FileBlob) (KitConfig, []string, error) {
	var k KitConfig
	found := false
	for _, f := range allFiles {
		if f.Path == "kit.json" {
			parsed, err := ParseKit(string(f.Content))
			if err != nil {
				return KitConfig{}, nil, err
			}
			if err := ValidateKit(&parsed); err != nil {
				return KitConfig{}, nil, err
			}
			k = parsed
			found = true
			break
		}
	}
	if !found {
		return KitConfig{}, nil, errs.NewKitValidation("kit.json not found in kit tree")
	}
	warnings, err := validateRuleFrontmatters(allFiles, &k)
	if err != nil {
		return KitConfig{}, warnings, err
	}
	return k, warnings, nil
}

// validateRuleFrontmatters parses each rule entry's frontmatter and
// applies the agreed severity model (see CLAUDE.md Spec 0010):
//
//   - Missing frontmatter → warning ("rendered without description").
//   - Frontmatter present but `name` empty / mismatched → error.
//   - Frontmatter present but `description` empty → warning.
//   - Description longer than MaxDescriptionLength chars → error.
//   - Description spans multiple lines → error.
//   - Malformed YAML (no closing fence, malformed line, dup key) → error.
//
// Rule entries whose content blob is absent from allFiles are silently
// skipped — that defect is the responsibility of `core/validate`, which
// walks the disk separately.
func validateRuleFrontmatters(allFiles []FileBlob, k *KitConfig) ([]string, error) {
	var warnings []string
	for _, entry := range k.Files {
		if entry.FileType != Rule {
			continue
		}
		content, ok := findContent(allFiles, entry.Path)
		if !ok {
			continue
		}
		warning, err := validateRuleFrontmatter(entry, content)
		if err != nil {
			return warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings, nil
}

func validateRuleFrontmatter(entry FileEntry, content []byte) (string, error) {
	derived := DeriveEntryName(entry.Path, entry.FileType)
	fm, _, found, err := frontmatter.Parse(string(content))
	if err != nil {
		return "", errs.NewKitValidation(fmt.Sprintf(
			"rule '%s': %s", entry.Path, frontmatterReason(err)))
	}
	if !found {
		return fmt.Sprintf(
			"rule '%s' has no frontmatter — rendered without description",
			entry.Path), nil
	}
	if fm.Name == "" {
		return "", errs.NewKitValidation(fmt.Sprintf(
			"rule '%s' frontmatter missing required 'name' field", entry.Path))
	}
	if fm.Name != derived {
		return "", errs.NewKitValidation(fmt.Sprintf(
			"rule '%s' frontmatter name '%s' does not match filename-derived name '%s'",
			entry.Path, fm.Name, derived))
	}
	if fm.Description == "" {
		return fmt.Sprintf(
			"rule '%s' frontmatter has no 'description' — rendered without hint",
			entry.Path), nil
	}
	if strings.ContainsAny(fm.Description, "\r\n") {
		return "", errs.NewKitValidation(fmt.Sprintf(
			"rule '%s' description must be single-line", entry.Path))
	}
	if len(fm.Description) > MaxDescriptionLength {
		return "", errs.NewKitValidation(fmt.Sprintf(
			"rule '%s' description is %d chars (max %d)",
			entry.Path, len(fm.Description), MaxDescriptionLength))
	}
	return "", nil
}

func frontmatterReason(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return err.Error()
}

func findContent(allFiles []FileBlob, path string) ([]byte, bool) {
	for _, f := range allFiles {
		if f.Path == path {
			return f.Content, true
		}
	}
	return nil, false
}

// FileBlob is a (path, bytes) pair returned by a tree walk. Used by
// ReadAndValidateKit; mirrors Rust's `(String, Vec<u8>)` tuple shape.
type FileBlob struct {
	Path    string
	Content []byte
}
