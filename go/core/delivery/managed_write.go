// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package delivery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/schubergphilis/inat/errs"
)

// AddAlwaysBlock writes (or replaces) an always-priority rule block in
// targetFile's managed section. The file is created if absent.
func AddAlwaysBlock(targetFile, header, content string) error {
	fileContent, err := readOrEmpty(targetFile)
	if err != nil {
		return err
	}
	parts, err := loadOrInit(fileContent, targetFile)
	if err != nil {
		return err
	}
	block := ManagedBlock{Header: header, Content: content}
	replaced := false
	for i := range parts.Section.Blocks {
		if parts.Section.Blocks[i].Header == header {
			parts.Section.Blocks[i] = block
			replaced = true
			break
		}
	}
	if !replaced {
		parts.Section.Blocks = append(parts.Section.Blocks, block)
	}
	return writeManagedFile(targetFile, parts)
}

// AddContextualReference appends rulePath to the managed section's
// references list (deduplicated by path). When the reference already
// exists, its Description is updated to the supplied value so that
// `refresh` can correct stale or newly-authored hints. Creates the
// section if absent. An empty description is rendered as a bare bullet.
func AddContextualReference(targetFile, rulePath, description string) error {
	fileContent, err := readOrEmpty(targetFile)
	if err != nil {
		return err
	}
	parts, err := loadOrInit(fileContent, targetFile)
	if err != nil {
		return err
	}
	for i := range parts.Section.References {
		if parts.Section.References[i].Path == rulePath {
			parts.Section.References[i].Description = description
			return writeManagedFile(targetFile, parts)
		}
	}
	parts.Section.References = append(parts.Section.References, Reference{
		Path:        rulePath,
		Description: description,
	})
	return writeManagedFile(targetFile, parts)
}

// RemoveContextualReference drops a single reference from the managed
// section, matching by Path. No-op when the file has no managed
// section or the path is absent. If the section becomes empty, markers
// are stripped.
func RemoveContextualReference(targetFile, rulePath string) error {
	fileContent, err := os.ReadFile(targetFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errs.WrapIO(err)
	}
	parts, err := ParseManagedSection(string(fileContent), targetFile)
	if err != nil {
		return err
	}
	if parts == nil {
		return nil
	}
	parts.Section.References = filterRefs(parts.Section.References, func(r Reference) bool {
		return r.Path != rulePath
	})
	return writeManagedFile(targetFile, parts)
}

// RenameKitBlocks rewrites every block whose header begins with
// `oldKey + "/"` to use `newKey + "/"` instead, preserving the trailing
// filename component and the block content. No-op when the file has no
// managed section or no headers match. Idempotent.
//
// Used by `inat rename` (spec 0019) so always-rule managed-section
// headers reflect the new install key without a follow-up `inat
// refresh`.
func RenameKitBlocks(targetFile, oldKey, newKey string) error {
	fileContent, err := os.ReadFile(targetFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errs.WrapIO(err)
	}
	parts, err := ParseManagedSection(string(fileContent), targetFile)
	if err != nil {
		return err
	}
	if parts == nil {
		return nil
	}
	oldPrefix := oldKey + "/"
	newPrefix := newKey + "/"
	changed := 0
	for i := range parts.Section.Blocks {
		if strings.HasPrefix(parts.Section.Blocks[i].Header, oldPrefix) {
			parts.Section.Blocks[i].Header = newPrefix +
				strings.TrimPrefix(parts.Section.Blocks[i].Header, oldPrefix)
			changed++
		}
	}
	if changed == 0 {
		return nil
	}
	return writeManagedFile(targetFile, parts)
}

// RemoveKitContent strips every block whose header begins with `kitKey/`
// and every reference whose Path is in referencePaths. If the section
// becomes empty, markers are stripped.
func RemoveKitContent(targetFile, kitKey string, referencePaths []string) error {
	fileContent, err := os.ReadFile(targetFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errs.WrapIO(err)
	}
	parts, err := ParseManagedSection(string(fileContent), targetFile)
	if err != nil {
		return err
	}
	if parts == nil {
		return nil
	}
	prefix := kitKey + "/"
	parts.Section.Blocks = filterBlocks(parts.Section.Blocks, func(b ManagedBlock) bool {
		return !strings.HasPrefix(b.Header, prefix)
	})
	refSet := map[string]struct{}{}
	for _, r := range referencePaths {
		refSet[r] = struct{}{}
	}
	parts.Section.References = filterRefs(parts.Section.References, func(r Reference) bool {
		_, hit := refSet[r.Path]
		return !hit
	})
	return writeManagedFile(targetFile, parts)
}

func readOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", errs.WrapIO(err)
	}
	return string(data), nil
}

func loadOrInit(fileContent, targetFile string) (*ParsedSection, error) {
	parts, err := ParseManagedSection(fileContent, targetFile)
	if err != nil {
		return nil, err
	}
	if parts == nil {
		return &ParsedSection{Before: fileContent, Section: ManagedSection{}, After: ""}, nil
	}
	return parts, nil
}

func writeManagedFile(targetFile string, parts *ParsedSection) error {
	if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
		return errs.WrapIO(err)
	}
	rendered := RenderManagedSection(&parts.Section)
	var out strings.Builder
	if rendered == "" {
		out.WriteString(strings.TrimRight(parts.Before, " \t\n\r"))
		afterTrimmed := strings.TrimLeft(parts.After, " \t\n\r")
		if afterTrimmed != "" {
			out.WriteByte('\n')
			out.WriteString(afterTrimmed)
		}
		final := out.String()
		if final != "" {
			final += "\n"
		}
		if err := os.WriteFile(targetFile, []byte(final), 0o644); err != nil {
			return errs.WrapIO(err)
		}
		return nil
	}
	out.WriteString(parts.Before)
	if parts.Before != "" && !strings.HasSuffix(parts.Before, "\n") {
		out.WriteByte('\n')
	}
	out.WriteString(rendered)
	out.WriteString(parts.After)
	final := out.String()
	if !strings.HasSuffix(final, "\n") {
		final += "\n"
	}
	if err := os.WriteFile(targetFile, []byte(final), 0o644); err != nil {
		return errs.WrapIO(err)
	}
	return nil
}

func filterRefs(in []Reference, keep func(Reference) bool) []Reference {
	out := in[:0]
	for _, r := range in {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func filterBlocks(in []ManagedBlock, keep func(ManagedBlock) bool) []ManagedBlock {
	out := in[:0]
	for _, b := range in {
		if keep(b) {
			out = append(out, b)
		}
	}
	return out
}
