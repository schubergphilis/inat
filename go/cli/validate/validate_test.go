// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package validatecmd

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, root string) {
	t.Helper()
	mustWrite := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mustWrite("registry.json", `{"version":"1","kits":{"demo":{"description":"d","types":["skill","rule"]}}}`)
	mustWrite("kits/demo/kit.json", `{"name":"demo","description":"d","files":[{"path":"skills/review","type":"skill"},{"path":"rules/r.md","type":"rule","priority":"always"}]}`)
	mustWrite("kits/demo/skills/review/SKILL.md", "# Review")
	mustWrite("kits/demo/rules/r.md", "rule")
}

func TestRunValidateValidFixture(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir)
	out, err := RunValidate(dir, "", false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.IssueCount != 0 || !out.IsOK() {
		t.Fatalf("expected clean, got %+v", out)
	}
}

func TestRunValidateMissingRegistry(t *testing.T) {
	dir := t.TempDir()
	out, err := RunValidate(dir, "", false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.IssueCount != 1 || out.IsOK() {
		t.Fatalf("expected one issue, got %+v", out)
	}
}

func TestRunValidateJSON(t *testing.T) {
	dir := t.TempDir()
	out, err := RunValidate(dir, "", true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.IssueCount != 1 {
		t.Fatalf("expected one issue, got %+v", out)
	}
}

func TestRunValidateSubpath(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "pkg"))
	out, err := RunValidate(dir, "pkg", false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.IssueCount != 0 {
		t.Fatalf("expected clean, got %+v", out)
	}
}
