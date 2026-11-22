// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package frontmatter parses YAML-style frontmatter blocks at the top of
// rule and skill markdown files.
//
// The parser is intentionally minimal: it handles only the flat
// `key: value` shape we ship in inat, surfaces `name` and `description`
// onto a typed struct, and tolerates other keys without error. It does
// not implement YAML at large (no anchors, no nested maps, no folded
// scalars). When kits need richer metadata, the answer is to extend
// this parser deliberately, not to import a YAML library — the
// dependency allowlist (CLAUDE.md rule 8) is cobra + go-git + stdlib.
package frontmatter

import (
	"fmt"
	"strings"

	"github.com/schubergphilis/inat/errs"
)

// Frontmatter is the parsed form of a YAML-style frontmatter block.
//
// Only `name` and `description` are surfaced. Other keys present in the
// block are tolerated and ignored.
type Frontmatter struct {
	Name        string
	Description string
}

// utf8BOM is the UTF-8 byte-order mark (U+FEFF). Tolerated as the very
// first bytes of a file so frontmatter still parses on Windows editors
// that add a BOM by default. Built from explicit bytes so the BOM
// character itself never appears in this source file (Go rejects a
// raw BOM mid-file).
var utf8BOM = string([]byte{0xEF, 0xBB, 0xBF})

// Parse extracts a YAML-style frontmatter block at the start of content.
//
// Recognised format: a block fenced by lines containing only `---`
// (trailing whitespace tolerated). Inside the block, flat `key: value`
// pairs (one per line). Surrounding single/double quotes on values are
// stripped. Lines beginning with `#` are treated as comments and
// skipped; blank lines are skipped. Unknown keys are tolerated.
//
// Returns:
//   - fm: parsed Name and Description (zero-valued when those keys are
//     absent from the block).
//   - body: content with the frontmatter block (and the line ending
//     immediately following the closing fence) removed; or content
//     unchanged when no frontmatter is present.
//   - found: true when a frontmatter block was detected (regardless of
//     which keys it contained).
//   - err: *errs.Error (Kind=KitValidation) when an opened block is
//     malformed: missing closing fence, line that is not `key: value`,
//     or duplicate `name`/`description`.
func Parse(content string) (Frontmatter, string, bool, error) {
	stripped := strings.TrimPrefix(content, utf8BOM)
	if stripped == "" {
		return Frontmatter{}, content, false, nil
	}
	sep := detectLineSep(stripped)
	lines := strings.Split(stripped, sep)
	if !isFence(lines[0]) {
		return Frontmatter{}, content, false, nil
	}
	closeIdx := findClosingFence(lines)
	if closeIdx == -1 {
		return Frontmatter{}, content, true, errs.NewKitValidation("frontmatter has no closing `---` fence")
	}
	fm, err := parseFields(lines[1:closeIdx])
	if err != nil {
		return Frontmatter{}, content, true, err
	}
	body := ""
	if closeIdx+1 < len(lines) {
		body = strings.Join(lines[closeIdx+1:], sep)
	}
	return fm, body, true, nil
}

func detectLineSep(s string) string {
	if strings.Contains(s, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func isFence(line string) bool {
	return strings.TrimRight(line, " \t\r") == "---"
}

func findClosingFence(lines []string) int {
	for i := 1; i < len(lines); i++ {
		if isFence(lines[i]) {
			return i
		}
	}
	return -1
}

func parseFields(lines []string) (Frontmatter, error) {
	var fm Frontmatter
	seen := map[string]bool{}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colonIdx := strings.IndexByte(trimmed, ':')
		if colonIdx == -1 {
			return Frontmatter{}, errs.NewKitValidation(
				fmt.Sprintf("frontmatter line %q is not key: value", trimmed))
		}
		key := strings.TrimSpace(trimmed[:colonIdx])
		value := unquote(strings.TrimSpace(trimmed[colonIdx+1:]))
		if key == "name" || key == "description" {
			if seen[key] {
				return Frontmatter{}, errs.NewKitValidation(
					fmt.Sprintf("frontmatter has duplicate key %q", key))
			}
			seen[key] = true
		}
		switch key {
		case "name":
			fm.Name = value
		case "description":
			fm.Description = value
		}
	}
	return fm, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' || first == '\'') && first == last {
			return s[1 : len(s)-1]
		}
	}
	return s
}
