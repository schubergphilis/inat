// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func TestInstallHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()

	if err := Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	rec, ok := cfg.Installed["test-source/test-kit"]
	if !ok {
		t.Fatalf("not installed: %+v", cfg.Installed)
	}
	if rec.Tree == "" {
		t.Fatalf("tree empty")
	}
	wantNames := []string{"review", "no-secrets", "style"}
	if len(rec.Names) != 3 {
		t.Fatalf("names: %v", rec.Names)
	}
	for i, n := range wantNames {
		if rec.Names[i] != n {
			t.Fatalf("names[%d]: got %q want %q", i, rec.Names[i], n)
		}
	}
	skill, err := os.ReadFile(filepath.Join(project, ".claude/skills/review/SKILL.md"))
	if err != nil || !strings.Contains(string(skill), "code review") {
		t.Fatalf("skill: %v %q", err, skill)
	}
	md, err := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md: %v", err)
	}
	s := string(md)
	if !strings.Contains(s, "Never commit secrets.") || !strings.Contains(s, "test-source/test-kit/no-secrets.md") {
		t.Fatalf("always rule missing: %s", s)
	}
	ctxFile, err := os.ReadFile(filepath.Join(project, ".inat/rules/style.md"))
	if err != nil {
		t.Fatalf("contextual rule: %v", err)
	}
	if !strings.Contains(string(ctxFile), "style guidelines") {
		t.Fatalf("contextual content: %s", ctxFile)
	}
	if !strings.Contains(s, ".inat/rules/style.md") {
		t.Fatalf("contextual ref missing: %s", s)
	}
}

func TestInstallDuplicateName(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()

	cfg, _ := config.LoadConfig(project)
	cfg.Installed["other/other-kit"] = config.InstalledKit{
		Tree:      "fake",
		Names:     []string{"review"},
		Delivered: nil,
	}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}
	err := Install(project, "test-source/test-kit", home)
	if !errors.Is(err, errs.ErrDuplicateName) {
		t.Fatalf("want DuplicateName, got %v", err)
	}
}

func TestInstallMissingKit(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()

	err := Install(project, "test-source/nonexistent", home)
	if !errors.Is(err, errs.ErrKitNotFound) {
		t.Fatalf("want KitNotFound, got %v", err)
	}
}

func TestInstallAlreadyInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()

	if err := Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := Install(project, "test-source/test-kit", home)
	if !errors.Is(err, errs.ErrKitAlreadyInstalled) {
		t.Fatalf("want KitAlreadyInstalled, got %v", err)
	}
}

func TestInstallBareName(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()

	if err := Install(project, "test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("not installed under expected key")
	}
}

func TestInstallNoAgentsRecordsButDeliversNothing(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()

	if err := Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	rec := cfg.Installed["test-source/test-kit"]
	if rec.Tree == "" {
		t.Fatalf("tree empty")
	}
	if len(rec.Delivered) != 0 {
		t.Fatalf("delivered should be empty: %+v", rec.Delivered)
	}
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("CLAUDE.md should not exist: %v", err)
	}
}
