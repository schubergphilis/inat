// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package kit

import (
	"errors"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

const validKitJSON = `{
    "name": "coding-standards",
    "description": "Common coding rules and skills",
    "author": "Team",
    "license": "MIT",
    "files": [
        { "path": "skills/review", "type": "skill" },
        { "path": "rules/no-secrets.md", "type": "rule", "priority": "always" },
        { "path": "rules/python-style.md", "type": "rule", "priority": "contextual" }
    ]
}`

func TestParseValidKit(t *testing.T) {
	k, err := ParseKit(validKitJSON)
	if err != nil {
		t.Fatalf("ParseKit: %v", err)
	}
	if k.Name != "coding-standards" {
		t.Errorf("Name = %q, want %q", k.Name, "coding-standards")
	}
	if len(k.Files) != 3 {
		t.Fatalf("len(Files) = %d, want 3", len(k.Files))
	}
	if k.Files[0].FileType != Skill {
		t.Errorf("Files[0].FileType = %v, want Skill", k.Files[0].FileType)
	}
	if k.Files[1].Priority == nil || *k.Files[1].Priority != Always {
		t.Errorf("Files[1].Priority = %v, want Always", k.Files[1].Priority)
	}
	if k.Files[2].Priority == nil || *k.Files[2].Priority != Contextual {
		t.Errorf("Files[2].Priority = %v, want Contextual", k.Files[2].Priority)
	}
}

func TestValidateValidKit(t *testing.T) {
	k, _ := ParseKit(validKitJSON)
	if err := ValidateKit(&k); err != nil {
		t.Errorf("ValidateKit: %v", err)
	}
}

func TestParseMissingName(t *testing.T) {
	_, err := ParseKit(`{ "description": "no name", "files": [] }`)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrJSON) {
		t.Errorf("err kind = %v, want JSON", err)
	}
}

func TestParseMissingFiles(t *testing.T) {
	_, err := ParseKit(`{ "name": "test", "description": "no files" }`)
	if err == nil {
		t.Fatalf("expected error for missing 'files' field")
	}
	if !errors.Is(err, errs.ErrJSON) {
		t.Errorf("err kind = %v, want JSON", err)
	}
}

func TestValidateEmptyFiles(t *testing.T) {
	_, err := ParseKit(`{ "name": "test", "description": "empty files", "files": [] }`)
	if err != nil {
		t.Fatalf("ParseKit (empty files but field present): %v", err)
	}
	// ValidateKit catches the empty list per Rust's `validate_empty_files` test.
	k := KitConfig{Name: "test", Description: "x", Files: []FileEntry{}}
	verr := ValidateKit(&k)
	if verr == nil || !strings.Contains(verr.Error(), "at least one file entry") {
		t.Errorf("ValidateKit on empty files = %v, want 'at least one file entry'", verr)
	}
}

func TestParseInvalidFileType(t *testing.T) {
	_, err := ParseKit(`{
        "name": "test", "description": "bad type",
        "files": [ { "path": "foo", "type": "unknown" } ]
    }`)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrJSON) {
		t.Errorf("err kind = %v, want JSON", err)
	}
}

func TestValidateInvalidKitName(t *testing.T) {
	k, _ := ParseKit(`{
        "name": "Invalid", "description": "bad name",
        "files": [ { "path": "skills/review", "type": "skill" } ]
    }`)
	err := ValidateKit(&k)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrInvalidName) {
		t.Errorf("err kind = %v, want InvalidName", err)
	}
}

func TestValidateSkillWithPriorityRejected(t *testing.T) {
	k, _ := ParseKit(`{
        "name": "test", "description": "skill with priority",
        "files": [ { "path": "skills/review", "type": "skill", "priority": "always" } ]
    }`)
	err := ValidateKit(&k)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "must not have a priority") {
		t.Errorf("err %q missing 'must not have a priority'", err)
	}
}

func TestValidateInvalidSkillNameInPath(t *testing.T) {
	k, _ := ParseKit(`{
        "name": "test", "description": "bad skill name",
        "files": [ { "path": "skills/Bad_Name", "type": "skill" } ]
    }`)
	err := ValidateKit(&k)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrInvalidName) {
		t.Errorf("err kind = %v, want InvalidName", err)
	}
}

func TestRulePriorityDefaultsToNil(t *testing.T) {
	k, _ := ParseKit(`{
        "name": "test", "description": "rule no priority",
        "files": [ { "path": "rules/style.md", "type": "rule" } ]
    }`)
	if k.Files[0].Priority != nil {
		t.Errorf("Priority = %v, want nil", k.Files[0].Priority)
	}
}

func TestDeriveSkillNameFromPath(t *testing.T) {
	got := DeriveEntryName("skills/review", Skill)
	if got != "review" {
		t.Errorf("DeriveEntryName(skills/review, Skill) = %q, want %q", got, "review")
	}
}

func TestDeriveRuleNameFromPath(t *testing.T) {
	got := DeriveEntryName("rules/no-secrets.md", Rule)
	if got != "no-secrets" {
		t.Errorf("DeriveEntryName(rules/no-secrets.md, Rule) = %q, want %q", got, "no-secrets")
	}
}

func TestDeriveSkillNameStripsTrailingSlash(t *testing.T) {
	got := DeriveEntryName("skills/review/", Skill)
	if got != "review" {
		t.Errorf("DeriveEntryName(skills/review/, Skill) = %q, want %q", got, "review")
	}
}

func TestReadAndValidateKitFindsKitJSON(t *testing.T) {
	files := []FileBlob{
		{Path: "skills/review/SKILL.md", Content: []byte("# Review")},
		{Path: "kit.json", Content: []byte(validKitJSON)},
	}
	k, _, err := ReadAndValidateKit(files)
	if err != nil {
		t.Fatalf("ReadAndValidateKit: %v", err)
	}
	if k.Name != "coding-standards" {
		t.Errorf("Name = %q, want coding-standards", k.Name)
	}
}

func TestReadAndValidateKitMissingKitJSON(t *testing.T) {
	files := []FileBlob{
		{Path: "skills/review/SKILL.md", Content: []byte("# Review")},
	}
	_, _, err := ReadAndValidateKit(files)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Errorf("err kind = %v, want KitValidation", err)
	}
}

const fmKitJSON = `{
    "name": "fm-kit",
    "description": "kit with rule frontmatter",
    "files": [
        { "path": "rules/foo.md", "type": "rule", "priority": "contextual" }
    ]
}`

func TestReadAndValidateKitWarnsOnMissingFrontmatter(t *testing.T) {
	files := []FileBlob{
		{Path: "kit.json", Content: []byte(fmKitJSON)},
		{Path: "rules/foo.md", Content: []byte("# Foo\n\nNo frontmatter here.\n")},
	}
	_, warnings, err := ReadAndValidateKit(files)
	if err != nil {
		t.Fatalf("ReadAndValidateKit: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no frontmatter") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestReadAndValidateKitWarnsOnMissingDescription(t *testing.T) {
	files := []FileBlob{
		{Path: "kit.json", Content: []byte(fmKitJSON)},
		{Path: "rules/foo.md", Content: []byte("---\nname: foo\n---\n# Foo body\n")},
	}
	_, warnings, err := ReadAndValidateKit(files)
	if err != nil {
		t.Fatalf("ReadAndValidateKit: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no 'description'") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestReadAndValidateKitErrorsOnNameMismatch(t *testing.T) {
	files := []FileBlob{
		{Path: "kit.json", Content: []byte(fmKitJSON)},
		{Path: "rules/foo.md", Content: []byte("---\nname: bar\ndescription: x\n---\n")},
	}
	_, _, err := ReadAndValidateKit(files)
	if err == nil || !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("expected KitValidation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "does not match filename-derived name") {
		t.Errorf("err message = %q", err.Error())
	}
}

func TestReadAndValidateKitErrorsOnMissingName(t *testing.T) {
	files := []FileBlob{
		{Path: "kit.json", Content: []byte(fmKitJSON)},
		{Path: "rules/foo.md", Content: []byte("---\ndescription: x\n---\n")},
	}
	_, _, err := ReadAndValidateKit(files)
	if err == nil || !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("expected KitValidation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "missing required 'name'") {
		t.Errorf("err message = %q", err.Error())
	}
}

func TestReadAndValidateKitErrorsOnOversizeDescription(t *testing.T) {
	long := strings.Repeat("x", MaxDescriptionLength+1)
	files := []FileBlob{
		{Path: "kit.json", Content: []byte(fmKitJSON)},
		{Path: "rules/foo.md", Content: []byte("---\nname: foo\ndescription: " + long + "\n---\n")},
	}
	_, _, err := ReadAndValidateKit(files)
	if err == nil || !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("expected KitValidation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "max 200") {
		t.Errorf("err message = %q", err.Error())
	}
}

func TestReadAndValidateKitErrorsOnMalformedFrontmatter(t *testing.T) {
	files := []FileBlob{
		{Path: "kit.json", Content: []byte(fmKitJSON)},
		{Path: "rules/foo.md", Content: []byte("---\nname: foo\nbody continues\n")},
	}
	_, _, err := ReadAndValidateKit(files)
	if err == nil || !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("expected KitValidation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "no closing") {
		t.Errorf("err message = %q", err.Error())
	}
}

func TestReadAndValidateKitCleanWhenFrontmatterValid(t *testing.T) {
	files := []FileBlob{
		{Path: "kit.json", Content: []byte(fmKitJSON)},
		{Path: "rules/foo.md", Content: []byte("---\nname: foo\ndescription: Read when foo\n---\n# body\n")},
	}
	_, warnings, err := ReadAndValidateKit(files)
	if err != nil {
		t.Fatalf("ReadAndValidateKit: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}
