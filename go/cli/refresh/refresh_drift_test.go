// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package refresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/internal/testfixture"
)

const (
	driftSkillPath = ".claude/skills/review/SKILL.md"
	driftRulePath  = ".inat/rules/style.md"
)

// installDriftKit installs test-kit into a fresh project and returns
// the project dir and the home override.
func installDriftKit(t *testing.T) (string, string) {
	t.Helper()
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	return project, home
}

func writeDriftFile(t *testing.T, project, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(project, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readDriftFile(t *testing.T, project, rel string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(project, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(got)
}

func recordedHashes(t *testing.T, project string) map[string]string {
	t.Helper()
	cfg, err := config.LoadConfig(project)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string)
	for _, d := range cfg.Installed["test-source/test-kit"].Delivered {
		out[d.Path] = d.Hash
	}
	return out
}

// TestRefreshWithoutForceKeepsTheEdit: issue #4. Refresh reported
// locally-modified files as skipped and then overwrote them.
func TestRefreshWithoutForceKeepsTheEdit(t *testing.T) {
	project, home := installDriftKit(t)
	before := recordedHashes(t, project)
	writeDriftFile(t, project, driftSkillPath, "My skill edit.")
	writeDriftFile(t, project, driftRulePath, "My rule edit.")

	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if got := readDriftFile(t, project, driftSkillPath); got != "My skill edit." {
		t.Errorf("skill edit overwritten: %q", got)
	}
	if got := readDriftFile(t, project, driftRulePath); got != "My rule edit." {
		t.Errorf("rule edit overwritten: %q", got)
	}
	after := recordedHashes(t, project)
	for _, p := range []string{driftSkillPath, driftRulePath} {
		if before[p] == "" || after[p] != before[p] {
			t.Errorf("recorded hash for %s changed: %q -> %q", p, before[p], after[p])
		}
	}
}

// TestRefreshSkipStillRestoresSiblings: a skipped file must not stop
// refresh from restoring a deleted file or the rule's managed-section
// reference in the same kit.
func TestRefreshSkipStillRestoresSiblings(t *testing.T) {
	project, home := installDriftKit(t)
	writeDriftFile(t, project, driftRulePath, "My rule edit.")
	if err := os.Remove(filepath.Join(project, driftSkillPath)); err != nil {
		t.Fatal(err)
	}
	writeDriftFile(t, project, "CLAUDE.md", "# Project\n")

	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if got := readDriftFile(t, project, driftRulePath); got != "My rule edit." {
		t.Errorf("rule edit overwritten: %q", got)
	}
	if got := readDriftFile(t, project, driftSkillPath); !strings.Contains(got, "code review") {
		t.Errorf("deleted skill not restored: %q", got)
	}
	if got := readDriftFile(t, project, "CLAUDE.md"); !strings.Contains(got, driftRulePath) {
		t.Errorf("contextual reference not restored: %s", got)
	}
}
