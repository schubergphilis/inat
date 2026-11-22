// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package validate runs an offline structural check of a kits-publishing
// repo. Mirrors the Rust implementation's `src/core/validate/`.
//
// Given a filesystem root (plus optional subpath), Run enumerates every
// structural defect that would make the directory an invalid inat kit
// source. An empty []Issue means the target is valid.
package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/schubergphilis/inat/core/frontmatter"
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/core/reserved"
	"github.com/schubergphilis/inat/errs"
)

// IssueKind identifies a defect category. JSON output uses the variant name
// as a string, matching Rust's `serde(rename_all = "PascalCase")`.
type IssueKind string

const (
	MissingRegistry        IssueKind = "MissingRegistry"
	UnreadableRegistry     IssueKind = "UnreadableRegistry"
	InvalidRegistry        IssueKind = "InvalidRegistry"
	MissingKitJson         IssueKind = "MissingKitJson"
	UnreadableKitJson      IssueKind = "UnreadableKitJson"
	InvalidKit             IssueKind = "InvalidKit"
	MissingSkill           IssueKind = "MissingSkill"
	MissingRule            IssueKind = "MissingRule"
	DuplicateName          IssueKind = "DuplicateName"
	ReservedName           IssueKind = "ReservedName"
	InvalidRuleFrontmatter IssueKind = "InvalidRuleFrontmatter"
	MissingRuleDescription IssueKind = "MissingRuleDescription"
)

// Severity classifies an issue. Errors block (non-zero exit); warnings
// are advisory and never affect exit code. Frontmatter shape violations
// are errors; missing description is a warning.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one defect surfaced by Run. Path is forward-slash relative to
// the validation root.
type Issue struct {
	Kind     IssueKind `json:"kind"`
	Path     string    `json:"path"`
	Message  string    `json:"message"`
	Severity Severity  `json:"severity,omitempty"`
}

// IsError reports whether the issue counts toward the failing exit code.
// Issues without an explicit Severity are treated as errors so existing
// kinds (defined before the severity field landed) keep their old
// blocking behaviour.
func (i Issue) IsError() bool { return i.Severity != SeverityWarning }

// Run executes structural validation against `<path>[/<subpath>]` and
// returns every issue found. Empty result means valid.
func Run(path string, subpath string) []Issue {
	root := path
	if subpath != "" {
		root = filepath.Join(path, subpath)
	}
	var issues []Issue

	reg, ok := loadRegistry(root, &issues)
	if !ok {
		return issues
	}

	derived := map[string][]string{}
	kitNames := make([]string, 0, len(reg.Kits))
	for name := range reg.Kits {
		kitNames = append(kitNames, name)
	}
	sort.Strings(kitNames)
	for _, name := range kitNames {
		k, ok := loadKit(root, name, &issues)
		if !ok {
			continue
		}
		checkKitFiles(root, name, &k, &issues)
		collectDerived(&k, derived)
	}

	detectDuplicates(derived, &issues)
	detectReservedNames(derived, &issues)
	return issues
}

// CountKits returns the number of kits declared in the registry under
// `<path>[/<subpath>]`. Returns -1 when the registry is missing or
// malformed (mirrors Rust's `Option<usize>`).
func CountKits(path string, subpath string) int {
	root := path
	if subpath != "" {
		root = filepath.Join(path, subpath)
	}
	data, err := os.ReadFile(filepath.Join(root, "registry.json"))
	if err != nil {
		return -1
	}
	parsed, err := registry.ParseRegistry(string(data))
	if err != nil {
		return -1
	}
	return len(parsed.Kits)
}

func loadRegistry(root string, issues *[]Issue) (registry.Registry, bool) {
	abs := filepath.Join(root, "registry.json")
	if _, err := os.Stat(abs); err != nil {
		*issues = append(*issues, Issue{
			Kind:    MissingRegistry,
			Path:    "registry.json",
			Message: fmt.Sprintf("no registry.json at %s", root),
		})
		return registry.Registry{}, false
	}
	content, ok := readText(abs, "registry.json", UnreadableRegistry, issues)
	if !ok {
		return registry.Registry{}, false
	}
	parsed, err := registry.ParseRegistry(content)
	if err != nil {
		*issues = append(*issues, Issue{
			Kind:    UnreadableRegistry,
			Path:    "registry.json",
			Message: fmt.Sprintf("cannot parse registry.json: %s", err),
		})
		return registry.Registry{}, false
	}
	if err := registry.ValidateRegistry(&parsed); err != nil {
		*issues = append(*issues, Issue{
			Kind:    InvalidRegistry,
			Path:    "registry.json",
			Message: err.Error(),
		})
		return registry.Registry{}, false
	}
	return parsed, true
}

func loadKit(root, kitName string, issues *[]Issue) (kit.KitConfig, bool) {
	rel := "kits/" + kitName + "/kit.json"
	abs := filepath.Join(root, "kits", kitName, "kit.json")
	if _, err := os.Stat(abs); err != nil {
		*issues = append(*issues, Issue{
			Kind:    MissingKitJson,
			Path:    rel,
			Message: fmt.Sprintf("registry declares kit '%s' but no kit.json on disk", kitName),
		})
		return kit.KitConfig{}, false
	}
	content, ok := readText(abs, rel, UnreadableKitJson, issues)
	if !ok {
		return kit.KitConfig{}, false
	}
	parsed, err := kit.ParseKit(content)
	if err != nil {
		*issues = append(*issues, Issue{
			Kind:    UnreadableKitJson,
			Path:    rel,
			Message: fmt.Sprintf("cannot parse: %s", err),
		})
		return kit.KitConfig{}, false
	}
	if err := kit.ValidateKit(&parsed); err != nil {
		*issues = append(*issues, Issue{
			Kind:    InvalidKit,
			Path:    rel,
			Message: err.Error(),
		})
		return kit.KitConfig{}, false
	}
	if parsed.Name != kitName {
		*issues = append(*issues, Issue{
			Kind: InvalidKit,
			Path: rel,
			Message: fmt.Sprintf(
				"kit.json name '%s' does not match registry kit name '%s'",
				parsed.Name, kitName),
		})
		return kit.KitConfig{}, false
	}
	return parsed, true
}

func readText(abs, rel string, kind IssueKind, issues *[]Issue) (string, bool) {
	data, err := os.ReadFile(abs)
	if err != nil {
		*issues = append(*issues, Issue{
			Kind:    kind,
			Path:    rel,
			Message: fmt.Sprintf("cannot read: %s", err),
		})
		return "", false
	}
	return string(data), true
}

func checkKitFiles(root, kitName string, k *kit.KitConfig, issues *[]Issue) {
	kitRoot := filepath.Join(root, "kits", kitName)
	for _, entry := range k.Files {
		switch entry.FileType {
		case kit.Skill:
			checkSkillEntry(kitRoot, kitName, entry, issues)
		case kit.Rule:
			checkRuleEntry(kitRoot, kitName, entry, issues)
		}
	}
}

func checkSkillEntry(kitRoot, kitName string, entry kit.FileEntry, issues *[]Issue) {
	skillMd := filepath.Join(kitRoot, entry.Path, "SKILL.md")
	rel := "kits/" + kitName + "/" + entry.Path
	if !isFile(skillMd) {
		*issues = append(*issues, Issue{
			Kind: MissingSkill,
			Path: rel,
			Message: fmt.Sprintf(
				"skill entry '%s' missing SKILL.md (expected at %s)",
				entry.Path, relativeDisplay(skillMd, kitRoot)),
		})
	}
}

func checkRuleEntry(kitRoot, kitName string, entry kit.FileEntry, issues *[]Issue) {
	abs := filepath.Join(kitRoot, entry.Path)
	rel := "kits/" + kitName + "/" + entry.Path
	if !isFile(abs) {
		*issues = append(*issues, Issue{
			Kind:    MissingRule,
			Path:    rel,
			Message: fmt.Sprintf("rule file '%s' not found on disk", entry.Path),
		})
		return
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		*issues = append(*issues, Issue{
			Kind:    MissingRule,
			Path:    rel,
			Message: fmt.Sprintf("cannot read rule '%s': %s", entry.Path, err),
		})
		return
	}
	checkRuleFrontmatter(rel, entry, content, issues)
}

func checkRuleFrontmatter(rel string, entry kit.FileEntry, content []byte, issues *[]Issue) {
	derived := kit.DeriveEntryName(entry.Path, entry.FileType)
	fm, _, found, err := frontmatter.Parse(string(content))
	if err != nil {
		*issues = append(*issues, Issue{
			Kind:     InvalidRuleFrontmatter,
			Path:     rel,
			Message:  frontmatterReason(err),
			Severity: SeverityError,
		})
		return
	}
	if !found {
		*issues = append(*issues, Issue{
			Kind:     MissingRuleDescription,
			Path:     rel,
			Message:  fmt.Sprintf("rule '%s' has no frontmatter — rendered without description", entry.Path),
			Severity: SeverityWarning,
		})
		return
	}
	if fm.Name == "" {
		*issues = append(*issues, Issue{
			Kind:     InvalidRuleFrontmatter,
			Path:     rel,
			Message:  fmt.Sprintf("rule '%s' frontmatter missing required 'name' field", entry.Path),
			Severity: SeverityError,
		})
		return
	}
	if fm.Name != derived {
		*issues = append(*issues, Issue{
			Kind: InvalidRuleFrontmatter,
			Path: rel,
			Message: fmt.Sprintf(
				"rule '%s' frontmatter name '%s' does not match filename-derived name '%s'",
				entry.Path, fm.Name, derived),
			Severity: SeverityError,
		})
		return
	}
	if fm.Description == "" {
		*issues = append(*issues, Issue{
			Kind:     MissingRuleDescription,
			Path:     rel,
			Message:  fmt.Sprintf("rule '%s' frontmatter has no 'description' — rendered without hint", entry.Path),
			Severity: SeverityWarning,
		})
		return
	}
	if strings.ContainsAny(fm.Description, "\r\n") {
		*issues = append(*issues, Issue{
			Kind:     InvalidRuleFrontmatter,
			Path:     rel,
			Message:  fmt.Sprintf("rule '%s' description must be single-line", entry.Path),
			Severity: SeverityError,
		})
		return
	}
	if len(fm.Description) > kit.MaxDescriptionLength {
		*issues = append(*issues, Issue{
			Kind: InvalidRuleFrontmatter,
			Path: rel,
			Message: fmt.Sprintf("rule '%s' description is %d chars (max %d)",
				entry.Path, len(fm.Description), kit.MaxDescriptionLength),
			Severity: SeverityError,
		})
	}
}

func frontmatterReason(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return err.Error()
}

func collectDerived(k *kit.KitConfig, derived map[string][]string) {
	for _, entry := range k.Files {
		name := kit.DeriveEntryName(entry.Path, entry.FileType)
		derived[name] = append(derived[name], k.Name)
	}
}

func detectDuplicates(derived map[string][]string, issues *[]Issue) {
	keys := make([]string, 0, len(derived))
	for k := range derived {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		kits := uniqSorted(derived[name])
		if len(kits) > 1 {
			*issues = append(*issues, Issue{
				Kind: DuplicateName,
				Path: fmt.Sprintf("kits/%s/kit.json", kits[0]),
				Message: fmt.Sprintf(
					"derived name '%s' declared by kits %s",
					name, strings.Join(kits, ", ")),
			})
		}
	}
}

func detectReservedNames(derived map[string][]string, issues *[]Issue) {
	keys := make([]string, 0, len(derived))
	for k := range derived {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if name != reserved.BuiltinRuleName && name != reserved.BuiltinKitsRuleName {
			continue
		}
		origins := uniqSorted(derived[name])
		first := ""
		if len(origins) > 0 {
			first = origins[0]
		}
		*issues = append(*issues, Issue{
			Kind: ReservedName,
			Path: fmt.Sprintf("kits/%s/kit.json", first),
			Message: fmt.Sprintf(
				"derived name '%s' is reserved by inat and cannot be used by a kit", name),
		})
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func relativeDisplay(target, base string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return strings.ReplaceAll(rel, "\\", "/")
}

func uniqSorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	w := 0
	for i, s := range out {
		if i == 0 || s != out[w-1] {
			out[w] = s
			w++
		}
	}
	return out[:w]
}
