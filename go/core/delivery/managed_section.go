// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package delivery

import (
	"strings"

	"github.com/schubergphilis/inat/errs"
)

const (
	markerStart      = "<!-- inat:managed - DO NOT EDIT THIS SECTION -->"
	markerEnd        = "<!-- inat:end -->"
	blockPrefix      = "# --- "
	blockSuffix      = " ---"
	referencesHeader = "Read these rules when relevant:"
	// referenceSeparator joins a contextual reference path to its
	// human-readable "when to read" hint inside the rendered bullet.
	// Using U+2014 em-dash with surrounding ASCII spaces; the same
	// triplet is what parseReferenceLine splits on.
	referenceSeparator = " — "
)

// ManagedBlock is one always-priority rule block inside a managed section,
// identified by its delimiter header line.
type ManagedBlock struct {
	Header  string
	Content string
}

// Reference is one entry in the contextual references list. Path is the
// agent-relative path to the rule file (e.g. ".inat/rules/foo.md").
// Description is the optional "when to read" hint sourced from the
// rule's frontmatter; when empty, the rendered bullet has no separator.
type Reference struct {
	Path        string
	Description string
}

// ManagedSection is the parsed form of an inat-managed region.
type ManagedSection struct {
	Blocks     []ManagedBlock
	References []Reference
}

// ParsedSection holds the three pieces returned by ParseManagedSection:
// content before the start marker, the parsed managed section, content
// after the end marker.
type ParsedSection struct {
	Before  string
	Section ManagedSection
	After   string
}

// ParseManagedSection splits a file's full content into (before, section,
// after). Returns nil + nil when the content has no markers at all.
//
// Returns *errs.Error with Kind=ManagedSectionCorrupted when only one
// marker is present or the markers are out of order.
func ParseManagedSection(content, filePath string) (*ParsedSection, error) {
	startPos := strings.Index(content, markerStart)
	endPos := strings.Index(content, markerEnd)
	switch {
	case startPos == -1 && endPos == -1:
		return nil, nil
	case startPos == -1 || endPos == -1:
		return nil, errs.NewManagedSectionCorrupted(filePath)
	case startPos >= endPos:
		return nil, errs.NewManagedSectionCorrupted(filePath)
	}
	before := content[:startPos]
	managedStart := startPos + len(markerStart)
	managedContent := content[managedStart:endPos]
	after := content[endPos+len(markerEnd):]
	return &ParsedSection{
		Before:  before,
		Section: parseSectionContent(managedContent),
		After:   after,
	}, nil
}

func parseSectionContent(content string) ManagedSection {
	var blocks []ManagedBlock
	var references []Reference
	var currentHeader string
	var currentContent strings.Builder
	hasHeader := false
	inReferences := false

	flush := func() {
		if hasHeader {
			blocks = append(blocks, ManagedBlock{
				Header:  currentHeader,
				Content: strings.TrimSpace(currentContent.String()),
			})
			currentContent.Reset()
			hasHeader = false
			currentHeader = ""
		}
	}

	for _, line := range strings.Split(content, "\n") {
		if header, ok := parseBlockDelimiter(line); ok {
			flush()
			inReferences = false
			currentHeader = header
			hasHeader = true
			continue
		}
		if strings.TrimSpace(line) == referencesHeader {
			flush()
			inReferences = true
			continue
		}
		if inReferences {
			trimmed := strings.TrimSpace(line)
			if rest, ok := strings.CutPrefix(trimmed, "- "); ok {
				references = append(references, parseReferenceLine(rest))
			}
		} else if hasHeader {
			currentContent.WriteString(line)
			currentContent.WriteByte('\n')
		}
	}
	flush()
	return ManagedSection{Blocks: blocks, References: references}
}

// parseReferenceLine splits a bullet body (the text after the leading
// "- ") into its Path and Description. The first occurrence of
// referenceSeparator marks the boundary; an absent separator means the
// whole text is the path with an empty description.
func parseReferenceLine(rest string) Reference {
	if idx := strings.Index(rest, referenceSeparator); idx >= 0 {
		return Reference{
			Path:        strings.TrimSpace(rest[:idx]),
			Description: strings.TrimSpace(rest[idx+len(referenceSeparator):]),
		}
	}
	return Reference{Path: strings.TrimSpace(rest)}
}

func parseBlockDelimiter(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	rest, ok := strings.CutPrefix(trimmed, blockPrefix)
	if !ok {
		return "", false
	}
	header, ok := strings.CutSuffix(rest, blockSuffix)
	if !ok || header == "" {
		return "", false
	}
	return header, true
}

// RenderManagedSection produces the textual form of a managed section
// (markers included). Returns the empty string when the section has no
// blocks and no references — in that case, callers should remove the
// markers entirely instead of writing this output.
func RenderManagedSection(s *ManagedSection) string {
	if len(s.Blocks) == 0 && len(s.References) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(markerStart)
	b.WriteByte('\n')
	for _, block := range s.Blocks {
		b.WriteByte('\n')
		b.WriteString(blockPrefix)
		b.WriteString(block.Header)
		b.WriteString(blockSuffix)
		b.WriteByte('\n')
		b.WriteString(block.Content)
		b.WriteByte('\n')
	}
	if len(s.References) > 0 {
		b.WriteByte('\n')
		b.WriteString(referencesHeader)
		b.WriteByte('\n')
		for _, r := range s.References {
			b.WriteString("- ")
			b.WriteString(r.Path)
			if r.Description != "" {
				b.WriteString(referenceSeparator)
				b.WriteString(r.Description)
			}
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	b.WriteString(markerEnd)
	return b.String()
}
