// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/scope"
)

// Spec 0025 §6.2 / acceptance 2: inat-usage at user scope lands in
// ~/.inat/rules and is referenced from ~/.claude/CLAUDE.md in ~/ form.
func TestInatUsageUserScope(t *testing.T) {
	home := t.TempDir()
	rec, err := DeliverInatUsage(home, "claude-code", scope.User)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if rec.Path != ".inat/rules/inat-usage.md" {
		t.Errorf("recorded path = %q", rec.Path)
	}
	if _, err := os.Stat(filepath.Join(home, ".inat/rules/inat-usage.md")); err != nil {
		t.Fatalf("rule file: %v", err)
	}
	md, err := os.ReadFile(filepath.Join(home, ".claude/CLAUDE.md"))
	if err != nil || !strings.Contains(string(md), "- ~/.inat/rules/inat-usage.md") {
		t.Fatalf("~/.claude/CLAUDE.md reference: %v\n%s", err, md)
	}
	if _, err := os.Stat(filepath.Join(home, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("project-scope CLAUDE.md written at home root")
	}
	if _, _, err := RefreshInatUsage(home, "claude-code", &rec, false, scope.User); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := RemoveInatUsage(home, "claude-code", scope.User); err != nil {
		t.Fatalf("remove: %v", err)
	}
	md, _ = os.ReadFile(filepath.Join(home, ".claude/CLAUDE.md"))
	if strings.Contains(string(md), "inat-usage") {
		t.Errorf("reference left behind:\n%s", md)
	}
	if _, err := os.Stat(filepath.Join(home, ".inat/rules/inat-usage.md")); !os.IsNotExist(err) {
		t.Errorf("rule file left behind")
	}
}
