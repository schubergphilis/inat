// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package status

import (
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func TestStatusUpToDate(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	if err := Status(project, home); err != nil {
		t.Fatalf("status: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	got := CheckKitStatus(&cfg, "test-source/test-kit", cfg.Installed["test-source/test-kit"], home)
	if got != "up to date" {
		t.Fatalf("want 'up to date', got %q", got)
	}
}

func TestStatusUpdateAvailable(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated.",
	})
	cfg, _ := config.LoadConfig(project)
	got := CheckKitStatus(&cfg, "test-source/test-kit", cfg.Installed["test-source/test-kit"], home)
	if got != "update available" {
		t.Fatalf("want 'update available', got %q", got)
	}
}

func TestStatusNoKits(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := Status(project, home); err != nil {
		t.Fatalf("status empty: %v", err)
	}
}
