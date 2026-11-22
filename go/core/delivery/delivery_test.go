// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package delivery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/hash"
)

func TestDeliverSkillCopiesFiles(t *testing.T) {
	dir := t.TempDir()
	files := []FileBlob{
		{Path: "SKILL.md", Content: []byte("# Review\nDo code review.")},
		{Path: "scripts/check.sh", Content: []byte("#!/bin/bash\necho ok")},
	}
	delivered, err := DeliverSkill(dir, "review", files, ".claude/skills/")
	if err != nil {
		t.Fatalf("DeliverSkill: %v", err)
	}
	if len(delivered) != 2 {
		t.Fatalf("want 2, got %d", len(delivered))
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".claude/skills/review/SKILL.md")); string(got) != "# Review\nDo code review." {
		t.Fatalf("SKILL.md mismatch: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/skills/review/scripts/check.sh")); err != nil {
		t.Fatalf("script missing: %v", err)
	}
	if delivered[0].Path != ".claude/skills/review/SKILL.md" {
		t.Fatalf("path mismatch: %q", delivered[0].Path)
	}
	if delivered[1].Path != ".claude/skills/review/scripts/check.sh" {
		t.Fatalf("path mismatch: %q", delivered[1].Path)
	}
}

func TestDeliverSkillRecordsHash(t *testing.T) {
	dir := t.TempDir()
	content := []byte("# Skill content")
	delivered, err := DeliverSkill(dir, "test-skill",
		[]FileBlob{{Path: "SKILL.md", Content: content}}, ".claude/skills/")
	if err != nil {
		t.Fatalf("DeliverSkill: %v", err)
	}
	if delivered[0].Hash != hash.HashBytes(content) {
		t.Fatalf("hash mismatch")
	}
}

func TestDeliverRuleContextual(t *testing.T) {
	dir := t.TempDir()
	content := []byte("Follow Python PEP 8 style.")
	delivered, err := DeliverRuleContextual(dir, "python-style", content, ".inat/rules/")
	if err != nil {
		t.Fatalf("DeliverRuleContextual: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".inat/rules/python-style.md"))
	if string(got) != string(content) {
		t.Fatalf("file content mismatch")
	}
	if delivered.Path != ".inat/rules/python-style.md" {
		t.Fatalf("path mismatch: %q", delivered.Path)
	}
	if delivered.Hash != hash.HashBytes(content) {
		t.Fatalf("hash mismatch")
	}
}

func TestParseNoMarkers(t *testing.T) {
	parts, err := ParseManagedSection("# My Project\n\nSome user content.\n", "test.md")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if parts != nil {
		t.Fatalf("expected nil, got %+v", parts)
	}
}

func TestParseCorruptedStartOnly(t *testing.T) {
	_, err := ParseManagedSection("before\n"+markerStart+"\nsome content\n", "test.md")
	if err == nil {
		t.Fatalf("expected corruption error")
	}
}

func TestParseCorruptedEndOnly(t *testing.T) {
	_, err := ParseManagedSection("before\n"+markerEnd+"\n", "test.md")
	if err == nil {
		t.Fatalf("expected corruption error")
	}
}

func TestParseExisting(t *testing.T) {
	content := "# My Project\n\nUser content here.\n\n" + markerStart + "\n\n# --- company/coding-standards/no-secrets.md ---\nNever commit .env files or API keys.\n\n" + markerEnd + "\n\nMore user content.\n"
	parts, err := ParseManagedSection(content, "test.md")
	if err != nil || parts == nil {
		t.Fatalf("parse: %v parts=%v", err, parts)
	}
	if !strings.Contains(parts.Before, "User content here.") {
		t.Fatalf("before mismatch")
	}
	if len(parts.Section.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(parts.Section.Blocks))
	}
	if parts.Section.Blocks[0].Header != "company/coding-standards/no-secrets.md" {
		t.Fatalf("header mismatch")
	}
	if parts.Section.Blocks[0].Content != "Never commit .env files or API keys." {
		t.Fatalf("block content mismatch: %q", parts.Section.Blocks[0].Content)
	}
	if !strings.Contains(parts.After, "More user content.") {
		t.Fatalf("after mismatch")
	}
}

func TestParseMultipleBlocksAndReferences(t *testing.T) {
	content := markerStart + "\n\n# --- company/pkg-a/rule-a.md ---\nRule A content.\n\n# --- company/pkg-b/rule-b.md ---\nRule B content.\n\nRead these rules when relevant:\n- .inat/rules/style.md\n- .inat/rules/naming.md\n\n" + markerEnd + "\n"
	parts, err := ParseManagedSection(content, "test.md")
	if err != nil || parts == nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parts.Section.Blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(parts.Section.Blocks))
	}
	if parts.Section.Blocks[0].Header != "company/pkg-a/rule-a.md" || parts.Section.Blocks[1].Header != "company/pkg-b/rule-b.md" {
		t.Fatalf("block headers mismatch")
	}
	if len(parts.Section.References) != 2 {
		t.Fatalf("want 2 refs, got %d", len(parts.Section.References))
	}
	if parts.Section.References[0].Path != ".inat/rules/style.md" || parts.Section.References[1].Path != ".inat/rules/naming.md" {
		t.Fatalf("ref contents mismatch")
	}
}

func TestParseReferencesWithDescriptions(t *testing.T) {
	content := markerStart + "\n\nRead these rules when relevant:\n- .inat/rules/style.md — Read when editing Python\n- .inat/rules/naming.md\n- .inat/rules/em-dash.md — Description with — embedded em-dash\n\n" + markerEnd + "\n"
	parts, err := ParseManagedSection(content, "test.md")
	if err != nil || parts == nil {
		t.Fatalf("parse: %v", err)
	}
	refs := parts.Section.References
	if len(refs) != 3 {
		t.Fatalf("want 3 refs, got %d", len(refs))
	}
	if refs[0].Path != ".inat/rules/style.md" || refs[0].Description != "Read when editing Python" {
		t.Errorf("refs[0] = %#v", refs[0])
	}
	if refs[1].Path != ".inat/rules/naming.md" || refs[1].Description != "" {
		t.Errorf("refs[1] = %#v", refs[1])
	}
	if refs[2].Path != ".inat/rules/em-dash.md" || refs[2].Description != "Description with — embedded em-dash" {
		t.Errorf("refs[2] = %#v", refs[2])
	}
}

func TestRenderEmpty(t *testing.T) {
	if got := RenderManagedSection(&ManagedSection{}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestRenderRoundTrip(t *testing.T) {
	section := ManagedSection{
		Blocks: []ManagedBlock{
			{Header: "co/pkg-a/r1.md", Content: "Content A."},
			{Header: "co/pkg-b/r2.md", Content: "Content B."},
		},
		References: []Reference{
			{Path: ".inat/rules/style.md", Description: "Read when editing Python"},
			{Path: ".inat/rules/naming.md"},
		},
	}
	rendered := RenderManagedSection(&section)
	if !strings.Contains(rendered, "- .inat/rules/style.md — Read when editing Python") {
		t.Errorf("rendered missing em-dash bullet: %q", rendered)
	}
	if !strings.Contains(rendered, "- .inat/rules/naming.md\n") {
		t.Errorf("rendered missing bare bullet: %q", rendered)
	}
	parts, err := ParseManagedSection(rendered, "test.md")
	if err != nil || parts == nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(parts.Section.Blocks) != 2 || parts.Section.Blocks[0].Header != "co/pkg-a/r1.md" || parts.Section.Blocks[1].Content != "Content B." {
		t.Fatalf("round-trip mismatch")
	}
	if len(parts.Section.References) != 2 {
		t.Fatalf("round-trip refs mismatch: %#v", parts.Section.References)
	}
	if parts.Section.References[0].Description != "Read when editing Python" {
		t.Errorf("description lost on round-trip: %#v", parts.Section.References[0])
	}
	if parts.Section.References[1].Description != "" {
		t.Errorf("bare ref gained description: %#v", parts.Section.References[1])
	}
}

func TestAddAlwaysBlockCreatesFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	if err := AddAlwaysBlock(file, "company/kit/rule.md", "Never do X."); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ := os.ReadFile(file)
	for _, want := range []string{markerStart, "# --- company/kit/rule.md ---", "Never do X.", markerEnd} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestAddAlwaysBlockAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(file, []byte("# My Project\n\nUser content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddAlwaysBlock(file, "company/kit/rule.md", "Never do X."); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ := os.ReadFile(file)
	if !strings.HasPrefix(string(got), "# My Project") {
		t.Fatalf("prefix mismatch: %q", got)
	}
	if !strings.Contains(string(got), "User content.") || !strings.Contains(string(got), "Never do X.") {
		t.Fatalf("content mismatch")
	}
}

func TestAddAlwaysBlockAppendsSecond(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	if err := AddAlwaysBlock(file, "co/kit-a/r1.md", "Rule A."); err != nil {
		t.Fatal(err)
	}
	if err := AddAlwaysBlock(file, "co/kit-b/r2.md", "Rule B."); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	s := string(got)
	if !strings.Contains(s, "Rule A.") || !strings.Contains(s, "Rule B.") {
		t.Fatalf("missing blocks")
	}
	if strings.Count(s, markerStart) != 1 || strings.Count(s, markerEnd) != 1 {
		t.Fatalf("markers duplicated")
	}
}

func TestAddAlwaysBlockReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = AddAlwaysBlock(file, "co/kit/rule.md", "Old content.")
	_ = AddAlwaysBlock(file, "co/kit/rule.md", "New content.")
	got, _ := os.ReadFile(file)
	s := string(got)
	if strings.Contains(s, "Old content.") || !strings.Contains(s, "New content.") {
		t.Fatalf("replacement failed: %s", s)
	}
}

func TestAddContextualReferenceCreates(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	if err := AddContextualReference(file, ".inat/rules/style.md", "Read when editing Python"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), "Read these rules when relevant:") {
		t.Fatalf("ref header missing: %s", got)
	}
	if !strings.Contains(string(got), "- .inat/rules/style.md — Read when editing Python") {
		t.Fatalf("ref bullet missing or unhinted: %s", got)
	}
}

func TestAddContextualReferenceCreatesBare(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	if err := AddContextualReference(file, ".inat/rules/style.md", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), "- .inat/rules/style.md\n") {
		t.Fatalf("bare bullet missing: %s", got)
	}
	if strings.Contains(string(got), "—") {
		t.Fatalf("empty description must not render separator: %s", got)
	}
}

func TestAddContextualReferenceDedupes(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = AddContextualReference(file, ".inat/rules/style.md", "")
	_ = AddContextualReference(file, ".inat/rules/style.md", "")
	got, _ := os.ReadFile(file)
	if strings.Count(string(got), ".inat/rules/style.md") != 1 {
		t.Fatalf("dedup failed: %s", got)
	}
}

func TestAddContextualReferenceUpdatesDescriptionOnDedup(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = AddContextualReference(file, ".inat/rules/style.md", "Old hint")
	_ = AddContextualReference(file, ".inat/rules/style.md", "New hint")
	got, _ := os.ReadFile(file)
	s := string(got)
	if strings.Contains(s, "Old hint") {
		t.Fatalf("old description not replaced: %s", s)
	}
	if !strings.Contains(s, "- .inat/rules/style.md — New hint") {
		t.Fatalf("new description missing: %s", s)
	}
	if strings.Count(s, ".inat/rules/style.md") != 1 {
		t.Fatalf("dedup failed: %s", s)
	}
}

func TestAddContextualReferenceClearsDescription(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = AddContextualReference(file, ".inat/rules/style.md", "had hint")
	_ = AddContextualReference(file, ".inat/rules/style.md", "")
	got, _ := os.ReadFile(file)
	s := string(got)
	if strings.Contains(s, "had hint") {
		t.Fatalf("description not cleared: %s", s)
	}
	if !strings.Contains(s, "- .inat/rules/style.md\n") {
		t.Fatalf("bare bullet missing: %s", s)
	}
}

func TestRemoveKitContent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(file, []byte("# Project\n"), 0o644)
	_ = AddAlwaysBlock(file, "co/kit-a/r1.md", "Rule A.")
	_ = AddAlwaysBlock(file, "co/kit-b/r2.md", "Rule B.")
	_ = AddContextualReference(file, ".inat/rules/ctx-a.md", "")
	_ = AddContextualReference(file, ".inat/rules/ctx-b.md", "")
	if err := RemoveKitContent(file, "co/kit-a", []string{".inat/rules/ctx-a.md"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	s := string(got)
	if strings.Contains(s, "Rule A.") || !strings.Contains(s, "Rule B.") {
		t.Fatalf("block removal mismatch: %s", s)
	}
	if strings.Contains(s, "ctx-a.md") || !strings.Contains(s, "ctx-b.md") {
		t.Fatalf("ref removal mismatch: %s", s)
	}
}

func TestRemoveAllStripsMarkers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(file, []byte("# Project\n"), 0o644)
	_ = AddAlwaysBlock(file, "co/kit/rule.md", "Rule.")
	if err := RemoveKitContent(file, "co/kit", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	s := string(got)
	if strings.Contains(s, markerStart) || strings.Contains(s, markerEnd) {
		t.Fatalf("markers should be gone: %s", s)
	}
	if !strings.Contains(s, "# Project") {
		t.Fatalf("user content lost: %s", s)
	}
}

func TestRenameKitBlocksMissingFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "does-not-exist.md")
	if err := RenameKitBlocks(file, "co/old", "co/new"); err != nil {
		t.Fatalf("missing file should be no-op: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("file should not have been created: %v", err)
	}
}

func TestRenameKitBlocksNoManagedSection(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	original := "# Project\n\nUser content.\n"
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	statBefore, _ := os.Stat(file)
	if err := RenameKitBlocks(file, "co/old", "co/new"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if string(got) != original {
		t.Fatalf("content changed: %q", got)
	}
	statAfter, _ := os.Stat(file)
	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Fatalf("mtime should be unchanged when no rewrite occurred")
	}
}

func TestRenameKitBlocksNoMatchingBlock(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(file, []byte("# Project\n"), 0o644)
	_ = AddAlwaysBlock(file, "co/other/rule.md", "Other.")
	contentBefore, _ := os.ReadFile(file)
	statBefore, _ := os.Stat(file)

	if err := RenameKitBlocks(file, "co/target", "co/target-renamed"); err != nil {
		t.Fatal(err)
	}
	contentAfter, _ := os.ReadFile(file)
	statAfter, _ := os.Stat(file)
	if string(contentAfter) != string(contentBefore) {
		t.Fatalf("content changed when no headers matched")
	}
	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Fatalf("mtime should be unchanged when no rewrite occurred")
	}
}

func TestRenameKitBlocksRewritesSingleHeader(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(file, []byte("# Project\n"), 0o644)
	_ = AddAlwaysBlock(file, "co/old/no-secrets.md", "Never commit secrets.")

	if err := RenameKitBlocks(file, "co/old", "co/new"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	s := string(got)
	if strings.Contains(s, "co/old/no-secrets.md") {
		t.Fatalf("old header still present: %s", s)
	}
	if !strings.Contains(s, "co/new/no-secrets.md") {
		t.Fatalf("new header missing: %s", s)
	}
	if !strings.Contains(s, "Never commit secrets.") {
		t.Fatalf("block content lost: %s", s)
	}
}

func TestRenameKitBlocksRewritesMultipleAndPreservesOthers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(file, []byte("# Project\n"), 0o644)
	_ = AddAlwaysBlock(file, "co/target/r1.md", "Target rule 1.")
	_ = AddAlwaysBlock(file, "co/other/r2.md", "Other rule.")
	_ = AddAlwaysBlock(file, "co/target/r3.md", "Target rule 3.")

	if err := RenameKitBlocks(file, "co/target", "co/target-renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	s := string(got)
	if strings.Contains(s, "co/target/r1.md") || strings.Contains(s, "co/target/r3.md") {
		t.Fatalf("old headers still present: %s", s)
	}
	if !strings.Contains(s, "co/target-renamed/r1.md") || !strings.Contains(s, "co/target-renamed/r3.md") {
		t.Fatalf("new headers missing: %s", s)
	}
	if !strings.Contains(s, "co/other/r2.md") {
		t.Fatalf("non-matching header rewritten: %s", s)
	}
	// Ordering preserved: target/r1, other/r2, target/r3 originally;
	// after rename: target-renamed/r1, other/r2, target-renamed/r3.
	idx1 := strings.Index(s, "co/target-renamed/r1.md")
	idx2 := strings.Index(s, "co/other/r2.md")
	idx3 := strings.Index(s, "co/target-renamed/r3.md")
	if !(idx1 < idx2 && idx2 < idx3) {
		t.Fatalf("block ordering not preserved: r1=%d r2=%d r3=%d", idx1, idx2, idx3)
	}
}

// TestRenameKitBlocksExactKeyNoSlashIgnored ensures the prefix match
// requires `oldKey + "/"`. A block whose header equals oldKey exactly
// (no trailing slash) MUST NOT be rewritten — otherwise renaming `co/f`
// would silently mutate blocks from a kit named `co/foo`.
func TestRenameKitBlocksExactKeyNoSlashIgnored(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "CLAUDE.md")
	_ = os.WriteFile(file, []byte("# Project\n"), 0o644)
	// Header is exactly "co/foo" with no trailing slash. (Not a real
	// shape today — headers always carry a filename — but the prefix
	// match must still treat it correctly.)
	_ = AddAlwaysBlock(file, "co/foo", "Edge case.")

	if err := RenameKitBlocks(file, "co/f", "co/f-renamed"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), "co/foo") {
		t.Fatalf("'co/foo' header should not have been rewritten by 'co/f' rename: %s", got)
	}
	if strings.Contains(string(got), "co/f-renamed") {
		t.Fatalf("rewrite leaked into unrelated header: %s", got)
	}
}
