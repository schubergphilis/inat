// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package rename

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

// TestRenameHappyPath covers the smallest meaningful rename: install a kit,
// stage a source-side `git mv` that puts byte-identical content under a new
// directory, then rename and assert the map key was rewritten while the
// stored entry survived byte-identically. Same blobs at same relative paths
// produce the same tree SHA, satisfying rename's tree-SHA equality check.
func TestRenameHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	pre := cfg.Installed["test-source/test-kit"]

	stageRenameInRemote(t, ctx, "renamed-kit", sameContentAsTestKit())

	if err := Rename(project, "test-source/test-kit", "test-source/renamed-kit", home); err != nil {
		t.Fatalf("rename: %v", err)
	}
	cfg, _ = config.LoadConfig(project)
	post, ok := cfg.Installed["test-source/renamed-kit"]
	if !ok {
		t.Fatalf("entry not at new key: %+v", cfg.Installed)
	}
	if _, stillThere := cfg.Installed["test-source/test-kit"]; stillThere {
		t.Fatalf("entry still at old key: %+v", cfg.Installed)
	}
	if post.Tree != pre.Tree {
		t.Fatalf("tree mutated: pre=%s post=%s", pre.Tree, post.Tree)
	}
	if len(post.Names) != len(pre.Names) || len(post.Delivered) != len(pre.Delivered) {
		t.Fatalf("entry shape changed: pre=%+v post=%+v", pre, post)
	}
}

// TestRenameRewritesAlwaysHeaders covers spec 0019: after rename the
// always-rule managed-section block in CLAUDE.md must reference the
// new install key, not the old one. Block content is unchanged.
func TestRenameRewritesAlwaysHeaders(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	claudeMd := filepath.Join(project, "CLAUDE.md")
	pre, err := os.ReadFile(claudeMd)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(pre), "test-source/test-kit/no-secrets.md") {
		t.Fatalf("pre-condition: header missing in CLAUDE.md: %s", pre)
	}

	stageRenameInRemote(t, ctx, "renamed-kit", sameContentAsTestKit())
	if err := Rename(project, "test-source/test-kit", "test-source/renamed-kit", home); err != nil {
		t.Fatalf("rename: %v", err)
	}

	post, _ := os.ReadFile(claudeMd)
	s := string(post)
	if strings.Contains(s, "test-source/test-kit/no-secrets.md") {
		t.Fatalf("old header still present in CLAUDE.md: %s", s)
	}
	if !strings.Contains(s, "test-source/renamed-kit/no-secrets.md") {
		t.Fatalf("new header missing in CLAUDE.md: %s", s)
	}
	// Block body is unchanged.
	if !strings.Contains(s, "Never commit secrets.") {
		t.Fatalf("block content lost: %s", s)
	}
}

// TestRenameHeaderRewriteFailureLeavesConfigUnchanged covers spec 0019
// criterion 8: if the header rewrite fails, on-disk cfg.Installed must
// still reflect the pre-rename state (the in-memory mutation is
// discarded because SaveConfig is never reached).
//
// Failure is synthesised by replacing CLAUDE.md with a directory at the
// same path; os.ReadFile then returns a non-IsNotExist error and
// RenameKitBlocks propagates errs.WrapIO(err).
func TestRenameHeaderRewriteFailureLeavesConfigUnchanged(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	stageRenameInRemote(t, ctx, "renamed-kit", sameContentAsTestKit())

	// Replace CLAUDE.md with a directory at the same path so the read
	// fails with a non-IsNotExist error.
	claudeMd := filepath.Join(project, "CLAUDE.md")
	if err := os.Remove(claudeMd); err != nil {
		t.Fatalf("remove CLAUDE.md: %v", err)
	}
	if err := os.Mkdir(claudeMd, 0o755); err != nil {
		t.Fatalf("mkdir CLAUDE.md: %v", err)
	}

	if err := Rename(project, "test-source/test-kit", "test-source/renamed-kit", home); err == nil {
		t.Fatalf("expected error from header-rewrite failure, got nil")
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("on-disk cfg should still have old key: %+v", cfg.Installed)
	}
	if _, ok := cfg.Installed["test-source/renamed-kit"]; ok {
		t.Fatalf("on-disk cfg should not have new key after failure: %+v", cfg.Installed)
	}
}

// TestRenameRejectsTreeShaMismatch covers the content-drift case: source
// gained a new kit at the target name but its tree differs from the
// recorded tree (someone edited content in the same commit as the rename).
// Rename must reject with RenameTreeMismatch — the user should run
// `inat replace` or `inat update` instead.
func TestRenameRejectsTreeShaMismatch(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}

	stageRenameInRemote(t, ctx, "renamed-kit", driftedContent())

	err := Rename(project, "test-source/test-kit", "test-source/renamed-kit", home)
	if !errors.Is(err, errs.ErrRenameTreeMismatch) {
		t.Fatalf("want RenameTreeMismatch, got %v", err)
	}
	var typed *errs.Error
	if !errors.As(err, &typed) {
		t.Fatalf("not a typed *errs.Error: %v", err)
	}
	if typed.Name != "test-source/test-kit" || typed.ExistingKit != "test-source/renamed-kit" {
		t.Fatalf("unexpected old/new keys in error: %+v", typed)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("config mutated on rejected rename: %+v", cfg.Installed)
	}
	if _, ok := cfg.Installed["test-source/renamed-kit"]; ok {
		t.Fatalf("new key materialized on rejected rename: %+v", cfg.Installed)
	}
}

func TestRenameRejectsBareNameOld(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}

	err := Rename(project, "test-kit", "test-source/renamed-kit", home)
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation, got %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("install entry mutated on rejected rename: %+v", cfg.Installed)
	}
}

func TestRenameRejectsBareNameNew(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}

	err := Rename(project, "test-source/test-kit", "renamed-kit", home)
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation, got %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("install entry mutated on rejected rename: %+v", cfg.Installed)
	}
}

// stageRenameInRemote writes the given file map under kits/<newKitDir>/ in
// the remote worktree, updates registry.json to advertise the new kit, and
// commits. Used by rename tests that need a source-side `git mv`-shaped
// move (with or without content drift).
func stageRenameInRemote(t *testing.T, ctx *testfixture.RemoteContext, newKitDir string, files map[string]string) {
	t.Helper()
	staged := make(map[string]string, len(files)+1)
	for relPath, content := range files {
		staged["kits/"+newKitDir+"/"+relPath] = content
	}
	staged["registry.json"] = `{"version":"1","kits":{"test-kit":{"description":"Test","types":["skill","rule"]},"` +
		newKitDir + `":{"description":"Test","types":["skill","rule"]}}}`
	testfixture.UpdateRemote(t, ctx, staged)
}

// sameContentAsTestKit returns the byte-identical files of the standard
// test-kit. Reproducing the source helper rather than importing it because
// testfixture's standard set is unexported.
func sameContentAsTestKit() map[string]string {
	return map[string]string{
		"kit.json":               `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"skills/review/SKILL.md": "# Review\nDo code review.",
		"rules/no-secrets.md":    "Never commit secrets.",
		"rules/style.md":         "Follow style guidelines.",
	}
}

// driftedContent returns the same file set as the standard kit but with one
// byte changed in a skill so the tree SHA differs from the recorded one.
func driftedContent() map[string]string {
	files := sameContentAsTestKit()
	files["skills/review/SKILL.md"] = "# Review\nDo MUCH BETTER code review."
	return files
}

func TestRenameRejectsKitNotInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()

	err := Rename(project, "test-source/never-installed", "test-source/renamed-kit", home)
	if !errors.Is(err, errs.ErrKitNotInstalled) {
		t.Fatalf("want KitNotInstalled, got %v", err)
	}
}

func TestRenameRejectsAlreadyInstalledAtNewKey(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	cfg.Installed["test-source/already-here"] = config.InstalledKit{Tree: "fake", Names: nil, Delivered: nil}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}

	err := Rename(project, "test-source/test-kit", "test-source/already-here", home)
	if !errors.Is(err, errs.ErrKitAlreadyInstalled) {
		t.Fatalf("want KitAlreadyInstalled, got %v", err)
	}
	cfg, _ = config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("old key removed on rejected rename: %+v", cfg.Installed)
	}
}

func TestRenameRejectsKitNotInRegistry(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}

	// No source-side rename is staged — the registry only advertises
	// `test-kit`, so the new ref must be rejected as not-in-registry
	// before the tree-SHA check runs.
	err := Rename(project, "test-source/test-kit", "test-source/never-existed", home)
	if !errors.Is(err, errs.ErrKitNotFound) {
		t.Fatalf("want KitNotFound, got %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("old key removed on rejected rename: %+v", cfg.Installed)
	}
}

func TestRenameCmdRequiresExactlyTwoArgs(t *testing.T) {
	cases := [][]string{
		{},
		{"only-one"},
		{"a/b", "a/c", "extra"},
	}
	for _, args := range cases {
		cmd := NewRenameCmd()
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected arg-count error for %v, got nil", args)
		}
	}
}

func TestRenameCmdShape(t *testing.T) {
	cmd := NewRenameCmd()
	if cmd.Use == "" {
		t.Fatalf("Use empty")
	}
	if cmd.Short == "" {
		t.Fatalf("Short empty")
	}
	if cmd.RunE == nil {
		t.Fatalf("RunE nil")
	}
}

func TestRenameRejectsCrossSource(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}

	err := Rename(project, "test-source/test-kit", "other-source/test-kit", home)
	if !errors.Is(err, errs.ErrRenameCrossSource) {
		t.Fatalf("want RenameCrossSource, got %v", err)
	}
	var typed *errs.Error
	if !errors.As(err, &typed) {
		t.Fatalf("not a typed *errs.Error: %v", err)
	}
	if typed.Name != "test-source" || typed.SourceName != "other-source" {
		t.Fatalf("unexpected old/new source names: %+v", typed)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("install entry mutated on rejected rename: %+v", cfg.Installed)
	}
}
