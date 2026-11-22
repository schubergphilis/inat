// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package removecmd

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

func setupInstalled(t *testing.T) (project, home string) {
	t.Helper()
	ctx := testfixture.CreateTestRemote(t)
	project = testfixture.SetupProject(t, ctx.URL)
	home = t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	return
}

func TestRemoveHappyPath(t *testing.T) {
	project, home := setupInstalled(t)
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); err != nil {
		t.Fatalf("skill should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".inat/rules/style.md")); err != nil {
		t.Fatalf("style should exist: %v", err)
	}
	if err := Remove(project, "test-source/test-kit", false, home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("skill should be gone")
	}
	if _, err := os.Stat(filepath.Join(project, ".inat/rules/style.md")); !os.IsNotExist(err) {
		t.Fatalf("style should be gone")
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review")); !os.IsNotExist(err) {
		t.Fatalf("empty skill dir should be cleaned: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Fatalf("config still has kit")
	}
}

func TestRemoveModifiedFileSkips(t *testing.T) {
	project, home := setupInstalled(t)
	style := filepath.Join(project, ".inat/rules/style.md")
	if err := os.WriteFile(style, []byte("Modified by user."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(project, "test-source/test-kit", false, home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, err := os.ReadFile(style)
	if err != nil {
		t.Fatalf("style should still exist: %v", err)
	}
	if string(got) != "Modified by user." {
		t.Fatalf("style overwritten: %s", got)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Fatalf("config should drop kit despite skipped files")
	}
}

// TestRemoveCRLFCheckoutRemoves: issue #2. A delivered file whose only
// change is CRLF line endings is unmodified and must be removed.
func TestRemoveCRLFCheckoutRemoves(t *testing.T) {
	project, home := setupInstalled(t)
	skill := filepath.Join(project, ".claude/skills/review/SKILL.md")
	testfixture.ConvertToCRLF(t, skill)
	if err := Remove(project, "test-source/test-kit", false, home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Fatalf("CRLF-only file kept as if modified: %v", err)
	}
}

func TestRemoveForce(t *testing.T) {
	project, home := setupInstalled(t)
	style := filepath.Join(project, ".inat/rules/style.md")
	if err := os.WriteFile(style, []byte("Modified by user."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(project, "test-source/test-kit", true, home); err != nil {
		t.Fatalf("remove --force: %v", err)
	}
	if _, err := os.Stat(style); !os.IsNotExist(err) {
		t.Fatalf("style should be gone with --force")
	}
}

func TestRemoveCleansManagedSection(t *testing.T) {
	project, home := setupInstalled(t)
	claudeMd := filepath.Join(project, "CLAUDE.md")
	before, _ := os.ReadFile(claudeMd)
	if !strings.Contains(string(before), "Never commit secrets.") || !strings.Contains(string(before), ".inat/rules/style.md") {
		t.Fatalf("pre-condition failed: %s", before)
	}
	if err := Remove(project, "test-source/test-kit", false, home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	after, _ := os.ReadFile(claudeMd)
	s := string(after)
	if strings.Contains(s, "Never commit secrets.") || strings.Contains(s, ".inat/rules/style.md") || strings.Contains(s, "<!-- inat:managed") {
		t.Fatalf("managed section not cleaned: %s", s)
	}
}

func TestRemoveBareName(t *testing.T) {
	project, home := setupInstalled(t)
	if err := Remove(project, "test-kit", false, home); err != nil {
		t.Fatalf("remove bare: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Fatalf("not removed")
	}
}

func TestRemoveNotInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	err := Remove(project, "test-source/test-kit", false, home)
	if !errors.Is(err, errs.ErrKitNotInstalled) {
		t.Fatalf("want KitNotInstalled, got %v", err)
	}
}
