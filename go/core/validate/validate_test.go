// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func validFixture(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "registry.json"),
		`{"version":"1","kits":{"demo":{"description":"d","types":["skill","rule"]}}}`)
	writeFile(t, filepath.Join(root, "kits/demo/kit.json"),
		`{"name":"demo","description":"d","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"}]}`)
	writeFile(t, filepath.Join(root, "kits/demo/skills/review/SKILL.md"), "# Review")
	writeFile(t, filepath.Join(root, "kits/demo/rules/no-secrets.md"),
		"---\nname: no-secrets\ndescription: Never commit secrets to source control.\n---\n\nNo secrets.\n")
}

func TestMissingRegistry(t *testing.T) {
	dir := t.TempDir()
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != MissingRegistry {
		t.Fatalf("want one MissingRegistry, got %+v", issues)
	}
}

func TestValidFixture(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	if issues := Run(dir, ""); len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestMissingKitJson(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"),
		`{"version":"1","kits":{"demo":{"description":"d","types":["skill"]}}}`)
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != MissingKitJson {
		t.Fatalf("want MissingKitJson, got %+v", issues)
	}
}

func TestInvalidRegistryVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"),
		`{"version":"99","kits":{"demo":{"description":"d","types":["skill"]}}}`)
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != InvalidRegistry {
		t.Fatalf("want InvalidRegistry, got %+v", issues)
	}
}

func TestUnreadableRegistry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"), "not json")
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != UnreadableRegistry {
		t.Fatalf("want UnreadableRegistry, got %+v", issues)
	}
}

func TestMissingSkillMd(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	if err := os.Remove(filepath.Join(dir, "kits/demo/skills/review/SKILL.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != MissingSkill {
		t.Fatalf("want MissingSkill, got %+v", issues)
	}
}

func TestMissingRuleFile(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	if err := os.Remove(filepath.Join(dir, "kits/demo/rules/no-secrets.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != MissingRule {
		t.Fatalf("want MissingRule, got %+v", issues)
	}
}

func TestDuplicateNameAcrossKits(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"),
		`{"version":"1","kits":{"demo":{"description":"d","types":["rule"]},"other":{"description":"o","types":["rule"]}}}`)
	writeFile(t, filepath.Join(dir, "kits/demo/kit.json"),
		`{"name":"demo","description":"d","files":[{"path":"rules/shared.md","type":"rule","priority":"always"}]}`)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/shared.md"), "a")
	writeFile(t, filepath.Join(dir, "kits/other/kit.json"),
		`{"name":"other","description":"o","files":[{"path":"rules/shared.md","type":"rule","priority":"always"}]}`)
	writeFile(t, filepath.Join(dir, "kits/other/rules/shared.md"), "b")
	found := false
	for _, i := range Run(dir, "") {
		if i.Kind == DuplicateName {
			found = true
		}
	}
	if !found {
		t.Fatalf("want DuplicateName")
	}
}

func TestReservedNameInatUsage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"),
		`{"version":"1","kits":{"demo":{"description":"d","types":["rule"]}}}`)
	writeFile(t, filepath.Join(dir, "kits/demo/kit.json"),
		`{"name":"demo","description":"d","files":[{"path":"rules/inat-usage.md","type":"rule","priority":"contextual"}]}`)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/inat-usage.md"), "x")
	found := false
	for _, i := range Run(dir, "") {
		if i.Kind == ReservedName {
			found = true
		}
	}
	if !found {
		t.Fatalf("want ReservedName")
	}
}

func TestReservedNameInatKits(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"),
		`{"version":"1","kits":{"demo":{"description":"d","types":["rule"]}}}`)
	writeFile(t, filepath.Join(dir, "kits/demo/kit.json"),
		`{"name":"demo","description":"d","files":[{"path":"rules/inat-kits.md","type":"rule","priority":"contextual"}]}`)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/inat-kits.md"), "x")
	found := false
	for _, i := range Run(dir, "") {
		if i.Kind == ReservedName {
			found = true
		}
	}
	if !found {
		t.Fatalf("want ReservedName")
	}
}

func TestSubpathResolves(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "pkg")
	validFixture(t, nested)
	if issues := Run(dir, "pkg"); len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestSubpathMissingRegistry(t *testing.T) {
	dir := t.TempDir()
	issues := Run(dir, "does-not-exist")
	if len(issues) != 1 || issues[0].Kind != MissingRegistry {
		t.Fatalf("want MissingRegistry, got %+v", issues)
	}
}

func TestKitNameMismatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"),
		`{"version":"1","kits":{"demo":{"description":"d","types":["rule"]}}}`)
	writeFile(t, filepath.Join(dir, "kits/demo/kit.json"),
		`{"name":"other","description":"d","files":[{"path":"rules/r.md","type":"rule","priority":"always"}]}`)
	found := false
	for _, i := range Run(dir, "") {
		if i.Kind == InvalidKit {
			found = true
		}
	}
	if !found {
		t.Fatalf("want InvalidKit (name mismatch)")
	}
}

func TestRuleMissingFrontmatterIsWarning(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/no-secrets.md"), "No secrets here.\n")
	issues := Run(dir, "")
	if len(issues) != 1 {
		t.Fatalf("want 1 issue, got %+v", issues)
	}
	if issues[0].Kind != MissingRuleDescription {
		t.Errorf("kind = %v, want MissingRuleDescription", issues[0].Kind)
	}
	if issues[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", issues[0].Severity)
	}
	if issues[0].IsError() {
		t.Errorf("warning should not be error-severity")
	}
}

func TestRuleMissingDescriptionIsWarning(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/no-secrets.md"),
		"---\nname: no-secrets\n---\nbody\n")
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != MissingRuleDescription {
		t.Fatalf("want MissingRuleDescription, got %+v", issues)
	}
}

func TestRuleNameMismatchIsError(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/no-secrets.md"),
		"---\nname: wrong-name\ndescription: x\n---\n")
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != InvalidRuleFrontmatter {
		t.Fatalf("want InvalidRuleFrontmatter, got %+v", issues)
	}
	if !issues[0].IsError() {
		t.Errorf("name mismatch must be error-severity")
	}
}

func TestRuleOversizeDescriptionIsError(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	long := "description: " + strings.Repeat("x", 250)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/no-secrets.md"),
		"---\nname: no-secrets\n"+long+"\n---\n")
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != InvalidRuleFrontmatter {
		t.Fatalf("want InvalidRuleFrontmatter, got %+v", issues)
	}
}

func TestRuleMalformedFrontmatterIsError(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	writeFile(t, filepath.Join(dir, "kits/demo/rules/no-secrets.md"),
		"---\nname: no-secrets\nbody continues\n")
	issues := Run(dir, "")
	if len(issues) != 1 || issues[0].Kind != InvalidRuleFrontmatter {
		t.Fatalf("want InvalidRuleFrontmatter, got %+v", issues)
	}
}

func TestCountKits(t *testing.T) {
	dir := t.TempDir()
	validFixture(t, dir)
	if got := CountKits(dir, ""); got != 1 {
		t.Fatalf("want 1, got %d", got)
	}
}

func TestCountKitsMissingRegistry(t *testing.T) {
	dir := t.TempDir()
	if got := CountKits(dir, ""); got != -1 {
		t.Fatalf("want -1, got %d", got)
	}
}
