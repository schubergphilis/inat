// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

// installAndEditReview installs test-kit, overwrites its delivered
// review skill with a local edit, and pushes a new upstream version of
// that same file. Returns the project dir, home dir and the edited path.
func installAndEditReview(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	skill := filepath.Join(project, ".claude/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated review skill.",
	})
	return project, home, skill
}

// TestUpdateDriftWithoutForceKeepsLocalEdit is the regression test for
// issue #1: update used to warn "skipping" and then overwrite the
// locally-modified file with the new kit content. Without --force the
// kit must abort as needs-attention, leave the edit and the recorded
// tree untouched.
func TestUpdateDriftWithoutForceKeepsLocalEdit(t *testing.T) {
	project, home, skill := installAndEditReview(t)
	preCfg, _ := config.LoadConfig(project)
	preTree := preCfg.Installed["test-source/test-kit"].Tree

	err := Update(project, "test-source/test-kit", false, false, home)
	if !errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("want KitsNeedAttention, got %v", err)
	}

	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("modified file gone: %v", err)
	}
	if string(got) != "local edit" {
		t.Fatalf("local edit overwritten: %q", got)
	}
	postCfg, _ := config.LoadConfig(project)
	if postTree := postCfg.Installed["test-source/test-kit"].Tree; postTree != preTree {
		t.Fatalf("tree advanced despite drift abort: pre=%s post=%s", preTree, postTree)
	}
}

// TestUpdateDriftRestoresNonDriftedFiles: the abort happens after the
// unmodified files were already removed; the snapshot must put them
// back so the kit is left exactly as it was.
func TestUpdateDriftRestoresNonDriftedFiles(t *testing.T) {
	project, home, _ := installAndEditReview(t)
	cfg, _ := config.LoadConfig(project)
	var others []string
	for _, d := range cfg.Installed["test-source/test-kit"].Delivered {
		if d.Path != ".claude/skills/review/SKILL.md" {
			others = append(others, d.Path)
		}
	}
	if len(others) == 0 {
		t.Fatal("fixture has no unmodified delivered files to check")
	}

	if err := Update(project, "test-source/test-kit", false, false, home); err == nil {
		t.Fatal("want drift error, got nil")
	}

	for _, p := range others {
		if _, err := os.Stat(filepath.Join(project, p)); err != nil {
			t.Fatalf("non-drifted file %s not restored: %v", p, err)
		}
	}
}

// TestUpdateDriftWithForceOverwrites: --force discards the local edit
// and delivers the new kit content.
func TestUpdateDriftWithForceOverwrites(t *testing.T) {
	project, home, skill := installAndEditReview(t)

	if err := Update(project, "test-source/test-kit", true, false, home); err != nil {
		t.Fatalf("update --force: %v", err)
	}

	got, _ := os.ReadFile(skill)
	if !strings.Contains(string(got), "Review v2") {
		t.Fatalf("--force did not deliver new content: %q", got)
	}
}
