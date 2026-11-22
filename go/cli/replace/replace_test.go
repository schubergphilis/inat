// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package replace

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func TestReplaceCmdRequiresExactlyTwoArgs(t *testing.T) {
	cases := [][]string{
		{},
		{"only-one"},
		{"a/b", "a/c", "extra"},
	}
	for _, args := range cases {
		cmd := NewReplaceCmd()
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected arg-count error for %v, got nil", args)
		}
	}
}

func TestReplaceCmdShape(t *testing.T) {
	cmd := NewReplaceCmd()
	if cmd.Use == "" {
		t.Fatalf("Use empty")
	}
	if cmd.Short == "" {
		t.Fatalf("Short empty")
	}
	if cmd.RunE == nil {
		t.Fatalf("RunE nil")
	}
	if cmd.Flags().Lookup("force") == nil {
		t.Fatalf("--force flag missing")
	}
	if cmd.Flags().Lookup("plan") == nil {
		t.Fatalf("--plan flag missing")
	}
}

// secondKitFiles returns a kit definition whose derived names ("lint",
// "no-shouting", "naming") do NOT collide with the standard test-kit's
// names ("review", "no-secrets", "style"). Used to seed cross- and
// same-source replace targets without tripping CheckNameUniqueness.
func secondKitFiles(kitName string) map[string]string {
	return map[string]string{
		"kits/" + kitName + "/kit.json":             `{"name":"` + kitName + `","description":"Second kit","files":[{"path":"skills/lint","type":"skill"},{"path":"rules/no-shouting.md","type":"rule","priority":"always"},{"path":"rules/naming.md","type":"rule","priority":"contextual"}]}`,
		"kits/" + kitName + "/skills/lint/SKILL.md": "# Lint\nRun the linter.",
		"kits/" + kitName + "/rules/no-shouting.md": "Be quiet in code.",
		"kits/" + kitName + "/rules/naming.md":      "Use clear names.",
	}
}

// addSecondKitToRemote writes a second kit into the existing test-source
// remote and updates registry.json to advertise both kits. Used by the
// same-source replace happy-path test.
func addSecondKitToRemote(t *testing.T, ctx *testfixture.RemoteContext, kitName string) {
	t.Helper()
	files := secondKitFiles(kitName)
	files["registry.json"] = `{"version":"1","kits":{"test-kit":{"description":"Test","types":["skill","rule"]},"` +
		kitName + `":{"description":"Second","types":["skill","rule"]}}}`
	testfixture.UpdateRemote(t, ctx, files)
}

// createSecondRemote builds a fresh worktree-style remote populated with
// a single kit named kitName whose names don't collide with the standard
// test-kit. Returns the URL the project should register as a second
// source. Mirrors the layout testfixture.CreateTestRemote uses.
func createSecondRemote(t *testing.T, kitName string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "remote-"+kitName)
	repo, err := git.PlainInit(work, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	files := secondKitFiles(kitName)
	files["registry.json"] = `{"version":"1","kits":{"` + kitName + `":{"description":"Second","types":["skill","rule"]}}}`
	for relPath, content := range files {
		full := filepath.Join(work, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := wt.AddGlob("."); err != nil {
		t.Fatalf("AddGlob: %v", err)
	}
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return work
}

// addSourceToProject registers an additional source on an existing
// project config without disturbing the first source.
func addSourceToProject(t *testing.T, project, name, url string) {
	t.Helper()
	cfg, err := config.LoadConfig(project)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.Sources = append(cfg.Sources, config.Source{Name: name, URL: url})
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
}

// TestReplaceCrossSourceHappyPath covers the dominant `replace` use
// case: a kit moved between sources. After replace, the old source's
// install entry is gone, the new source's install entry exists, the
// new kit's delivered files are on disk, and the old kit's delivered
// files are gone.
func TestReplaceCrossSourceHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	otherURL := createSecondRemote(t, "other-kit")
	addSourceToProject(t, project, "other-source", otherURL)

	if err := Replace(project, "test-source/test-kit", "other-source/other-kit", false, false, home); err != nil {
		t.Fatalf("replace: %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["other-source/other-kit"]; !ok {
		t.Fatalf("new key missing: %+v", cfg.Installed)
	}
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Fatalf("old key still present: %+v", cfg.Installed)
	}

	// New kit's delivered files exist; old kit's are gone.
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/lint/SKILL.md")); err != nil {
		t.Fatalf("new skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("old skill still present: %v", err)
	}
	md, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	body := string(md)
	if !strings.Contains(body, "Be quiet in code.") {
		t.Fatalf("new always rule missing: %s", body)
	}
	if strings.Contains(body, "Never commit secrets.") {
		t.Fatalf("old always rule still present: %s", body)
	}
}

// TestReplaceSameSourceHappyPath covers `replace src/A src/B` where
// both kits live in the same source. The flow is identical to the
// cross-source case but exercises the "newSrc may equal oldSrc" branch
// in parseAndValidate (no enforced same/cross constraint).
func TestReplaceSameSourceHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	addSecondKitToRemote(t, ctx, "lint-kit")

	if err := Replace(project, "test-source/test-kit", "test-source/lint-kit", false, false, home); err != nil {
		t.Fatalf("replace: %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/lint-kit"]; !ok {
		t.Fatalf("new key missing: %+v", cfg.Installed)
	}
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Fatalf("old key still present: %+v", cfg.Installed)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/lint/SKILL.md")); err != nil {
		t.Fatalf("new skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("old skill still present: %v", err)
	}
}

// TestReplaceRejectedAtPlanByNameCollision exercises the spec-0017
// equivalent of the spec-0016 install-half rollback test. Under the new
// transactional pipeline, a name collision is caught at the plan phase
// (transaction.checkBatchUniqueness) BEFORE any disk mutation runs, so
// the typed error is the natural DuplicateName rather than the
// spec-0016 ReplaceInstallFailed wrapper. The on-disk state is
// preserved byte-for-byte — this is the central guarantee 0017 buys.
func TestReplaceRejectedAtPlanByNameCollision(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	addSecondKitToRemote(t, ctx, "lint-kit")

	// Inject a third installed entry whose Names ("lint") will collide
	// with the names lint-kit derives. Plan's batch-aware uniqueness
	// check rejects the Replace before Apply runs.
	cfg, _ := config.LoadConfig(project)
	pre := cfg.Installed["test-source/test-kit"]
	cfg.Installed["test-source/blocker"] = config.InstalledKit{
		Tree: "fakeTreeSha", Names: []string{"lint"}, Delivered: nil,
	}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}

	err := Replace(project, "test-source/test-kit", "test-source/lint-kit", false, false, home)
	if !errors.Is(err, errs.ErrDuplicateName) {
		t.Fatalf("want DuplicateName, got %v", err)
	}
	var typed *errs.Error
	if !errors.As(err, &typed) {
		t.Fatalf("not a typed *errs.Error: %v", err)
	}
	if typed.Name != "lint" || typed.ExistingKit != "test-source/blocker" {
		t.Fatalf("unexpected DuplicateName fields: %+v", typed)
	}

	// On-disk config: gated SaveConfig means oldKey is still present,
	// newKey absent, blocker untouched.
	cfg, _ = config.LoadConfig(project)
	post, ok := cfg.Installed["test-source/test-kit"]
	if !ok {
		t.Fatalf("old key missing from disk cfg: %+v", cfg.Installed)
	}
	if post.Tree != pre.Tree {
		t.Fatalf("disk Tree changed: pre=%s post=%s", pre.Tree, post.Tree)
	}
	if _, present := cfg.Installed["test-source/lint-kit"]; present {
		t.Fatalf("new key materialized: %+v", cfg.Installed)
	}
	if _, present := cfg.Installed["test-source/blocker"]; !present {
		t.Fatalf("blocker entry lost: %+v", cfg.Installed)
	}

	// On-disk delivered files: the central 0017 guarantee. Plan-phase
	// failure means RemoveDeliveredFiles never ran, so test-kit's
	// review skill survives.
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); err != nil {
		t.Errorf("old skill should be preserved on plan-phase failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/lint/SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("new skill should not exist: %v", err)
	}
}

// TestReplaceDriftWithoutForceAborts covers the spec criterion-11
// case: a delivered file from the old kit has been locally modified.
// Without --force the existing RemoveDeliveredFiles contract skips the
// modified file (warns, leaves it on disk). Replace then rolls back
// the config (snapshot restore) and returns KitsNeedAttention so the
// user sees a non-zero exit and the suggestion to re-run with --force.
func TestReplaceDriftWithoutForceAborts(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	otherURL := createSecondRemote(t, "other-kit")
	addSourceToProject(t, project, "other-source", otherURL)

	// Locally modify one of test-kit's delivered skill files.
	skill := filepath.Join(project, ".claude/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Replace(project, "test-source/test-kit", "other-source/other-kit", false, false, home)
	if !errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("want KitsNeedAttention, got %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Fatalf("snapshot restore failed: old key missing: %+v", cfg.Installed)
	}
	if _, ok := cfg.Installed["other-source/other-kit"]; ok {
		t.Fatalf("new key materialized despite drift abort: %+v", cfg.Installed)
	}
	// Modified file is still on disk (the existing skip contract).
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("modified file gone: %v", err)
	}
	if string(got) != "local edit" {
		t.Fatalf("modified file content changed: %q", got)
	}
	// New kit's files were never delivered.
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/lint/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("new skill should not exist after drift abort: %v", err)
	}
}

// TestReplaceDriftWithForceSucceeds covers the override path: --force
// tells the remove half to ignore drift and delete the modified file
// alongside the others. The install half then proceeds normally.
func TestReplaceDriftWithForceSucceeds(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	otherURL := createSecondRemote(t, "other-kit")
	addSourceToProject(t, project, "other-source", otherURL)

	skill := filepath.Join(project, ".claude/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Replace(project, "test-source/test-kit", "other-source/other-kit", true, false, home); err != nil {
		t.Fatalf("replace --force: %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["other-source/other-kit"]; !ok {
		t.Fatalf("new key missing after --force replace: %+v", cfg.Installed)
	}
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Fatalf("old key still present: %+v", cfg.Installed)
	}
	// Modified file was force-removed; old skill dir should be gone.
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Fatalf("modified file should be gone after --force: %v", err)
	}
	// New kit's skill exists.
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/lint/SKILL.md")); err != nil {
		t.Fatalf("new skill missing: %v", err)
	}
}

// --- Spec 0020: --plan dry-run ---

// captureStdout redirects os.Stdout for the duration of fn and returns
// what was written. Mirrors the helper in cli/update/update_test.go.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	_ = w.Close()
	return <-done
}

func TestReplacePlanLeavesDiskUntouched(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	otherURL := createSecondRemote(t, "other-kit")
	addSourceToProject(t, project, "other-source", otherURL)

	skillPath := filepath.Join(project, ".claude/skills/review/SKILL.md")
	cfgPath := filepath.Join(project, ".inat/config.json")
	preSkill, _ := os.ReadFile(skillPath)
	preCfg, _ := os.ReadFile(cfgPath)

	var err error
	stdout := captureStdout(t, func() {
		err = Replace(project, "test-source/test-kit", "other-source/other-kit", false, true, home)
	})
	if err != nil {
		t.Fatalf("--plan replace: %v", err)
	}
	want := "replace test-source/test-kit → other-source/other-kit\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	// Old kit's delivered files still there; new kit not delivered.
	if string(mustRead(t, skillPath)) != string(preSkill) {
		t.Error("old skill file changed under --plan")
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/lint/SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("new kit's skill delivered under --plan: %v", err)
	}
	if string(mustRead(t, cfgPath)) != string(preCfg) {
		t.Error("config.json changed under --plan")
	}
}

func TestReplacePlanPreFlightTypedError(t *testing.T) {
	// parseAndValidate runs before transaction.Plan, so its typed
	// errors must short-circuit --plan the same way they short-circuit
	// a real replace.
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}

	stdout := captureStdout(t, func() {
		err := Replace(project, "bad-ref", "other-source/other-kit", false, true, home)
		if !errors.Is(err, errs.ErrKitValidation) {
			t.Fatalf("want KitValidation, got %v", err)
		}
	})
	if stdout != "" {
		t.Errorf("--plan with bad-ref leaked stdout: %q", stdout)
	}
}

func TestReplacePlanForceMutuallyExclusive(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	err := Replace(project, "test-source/test-kit", "other-source/other-kit", true, true, home)
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation for --plan --force, got %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// --- Spec 0021: apply-phase snapshot/restore ---

// TestReplaceInstallHalfFailureRestoresOldFiles covers spec 0021 §6
// criterion 4 — the central guarantee 0021 adds. Inject an install-half
// failure (the new kit's contextual rule output path pre-created as a
// directory so deliverContextualRule's os.WriteFile fails) and assert
// the OLD kit's delivered files plus CLAUDE.md come back byte-identical
// to pre-apply.
//
// This is the fixture spec 0017 §6 criterion 9 nominally envisioned but
// no test materialised because, post-0017, name-collision failures are
// caught at plan phase (TestReplaceRejectedAtPlanByNameCollision) before
// any disk mutation. A real install-half failure requires an I/O fault
// during deliverAcrossAgents.
func TestReplaceInstallHalfFailureRestoresOldFiles(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	otherURL := createSecondRemote(t, "other-kit")
	addSourceToProject(t, project, "other-source", otherURL)

	skillPath := filepath.Join(project, ".claude/skills/review/SKILL.md")
	stylePath := filepath.Join(project, ".inat/rules/style.md")
	claudePath := filepath.Join(project, "CLAUDE.md")
	preSkill := mustRead(t, skillPath)
	preStyle := mustRead(t, stylePath)
	preClaude := mustRead(t, claudePath)

	// Pre-create the new kit's contextual rule destination as a directory
	// to force a write failure inside deliverContextualRule. Step 1
	// (RemoveDeliveredFiles) and step 3 (CleanManagedSections) don't
	// touch this path; only step 4's os.WriteFile for .inat/rules/naming.md
	// hits it.
	blocker := filepath.Join(project, ".inat/rules/naming.md")
	if err := os.MkdirAll(blocker, 0o755); err != nil {
		t.Fatal(err)
	}

	err := Replace(project, "test-source/test-kit", "other-source/other-kit", false, false, home)
	if !errors.Is(err, errs.ErrReplaceInstallFailed) {
		t.Fatalf("want ReplaceInstallFailed, got %v", err)
	}

	if got := mustRead(t, skillPath); !bytes.Equal(got, preSkill) {
		t.Errorf("review skill not restored byte-identical: got %q, want %q", got, preSkill)
	}
	if got := mustRead(t, stylePath); !bytes.Equal(got, preStyle) {
		t.Errorf("style rule not restored byte-identical: got %q, want %q", got, preStyle)
	}
	if got := mustRead(t, claudePath); !bytes.Equal(got, preClaude) {
		t.Errorf("CLAUDE.md not restored byte-identical: got %q, want %q", got, preClaude)
	}

	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Errorf("old key missing from cfg: %+v", cfg.Installed)
	}
	if _, ok := cfg.Installed["other-source/other-kit"]; ok {
		t.Errorf("new key present in cfg after install-half failure: %+v", cfg.Installed)
	}

	snaps, err := os.ReadDir(filepath.Join(home, ".inat", "snapshots"))
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("expected 1 snapshot dir after failure, got %d", len(snaps))
	}
}

// TestReplaceDriftRestoresNonDriftedFiles tightens
// TestReplaceDriftWithoutForceAborts with spec 0021 §6 criterion 5: the
// non-drifted delivered files (which RemoveDeliveredFiles successfully
// removed before the drift check fired) come back byte-identical, and
// the drifted file stays in its user-modified state.
func TestReplaceDriftRestoresNonDriftedFiles(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	otherURL := createSecondRemote(t, "other-kit")
	addSourceToProject(t, project, "other-source", otherURL)

	skillPath := filepath.Join(project, ".claude/skills/review/SKILL.md")
	stylePath := filepath.Join(project, ".inat/rules/style.md")
	preStyle := mustRead(t, stylePath)

	// Drift the skill so RemoveDeliveredFiles skips it but processes
	// the others (style.md gets removed before the drift check fires).
	if err := os.WriteFile(skillPath, []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Replace(project, "test-source/test-kit", "other-source/other-kit", false, false, home)
	if !errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("want KitsNeedAttention, got %v", err)
	}

	// Drifted file: still user-modified content.
	if got := mustRead(t, skillPath); string(got) != "local edit" {
		t.Errorf("drifted file content changed: %q", got)
	}
	// Non-drifted file: removed by RemoveDeliveredFiles, then restored
	// from snapshot byte-identical to pre-apply.
	if got := mustRead(t, stylePath); !bytes.Equal(got, preStyle) {
		t.Errorf("style.md not restored after drift abort: got %q, want %q", got, preStyle)
	}
}
