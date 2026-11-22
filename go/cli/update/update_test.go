// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package update

import (
	"bytes"
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

// captureStdout redirects os.Stdout for the duration of fn and returns
// what was written. Used by --plan tests (spec 0020) since plan-detail
// lines go to stdout while existing test fixtures only capture stderr.
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

func TestUpdateWithChanges(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated review skill.",
	})
	if err := Update(project, "test-source/test-kit", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(project, ".claude/skills/review/SKILL.md"))
	if !strings.Contains(string(got), "Review v2") {
		t.Fatalf("not updated: %s", got)
	}
	cfg, _ := config.LoadConfig(project)
	if cfg.Installed["test-source/test-kit"].Tree == "" {
		t.Fatalf("tree empty")
	}
}

func TestUpdateNoChanges(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	cfgBefore, _ := config.LoadConfig(project)
	tree := cfgBefore.Installed["test-source/test-kit"].Tree
	if err := Update(project, "test-source/test-kit", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}
	cfgAfter, _ := config.LoadConfig(project)
	if cfgAfter.Installed["test-source/test-kit"].Tree != tree {
		t.Fatalf("tree changed unexpectedly")
	}
}

func TestUpdateAll(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v3\nAll updated.",
	})
	if err := Update(project, "", false, false, home); err != nil {
		t.Fatalf("update all: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(project, ".claude/skills/review/SKILL.md"))
	if !strings.Contains(string(got), "Review v3") {
		t.Fatalf("not updated: %s", got)
	}
}

func TestUpdateNotInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	err := Update(project, "test-source/test-kit", false, false, home)
	if !errors.Is(err, errs.ErrKitNotInstalled) {
		t.Fatalf("want KitNotInstalled, got %v", err)
	}
	// Spec criterion 13: single-kit form's contract is byte-identical to
	// pre-spec-0016 — NOT wrapped in KitsNeedAttention. The skip-and-
	// continue conversion only applies to the multi-kit form.
	if errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("single-kit form must not wrap in KitsNeedAttention: %v", err)
	}
}

// addExtraKits writes kit-2 and kit-3 alongside the standard test-kit
// in the existing remote. Each kit's derived names are distinct from
// the others so install / update don't trip CheckNameUniqueness.
func addExtraKits(t *testing.T, ctx *testfixture.RemoteContext) {
	t.Helper()
	files := map[string]string{
		"registry.json": `{"version":"1","kits":{` +
			`"test-kit":{"description":"k1","types":["skill","rule"]},` +
			`"kit-2":{"description":"k2","types":["skill","rule"]},` +
			`"kit-3":{"description":"k3","types":["skill","rule"]}` +
			`}}`,
		"kits/kit-2/kit.json":            `{"name":"kit-2","description":"k2","files":[{"path":"skills/foo","type":"skill"},{"path":"rules/no-foo.md","type":"rule","priority":"always"},{"path":"rules/style2.md","type":"rule","priority":"contextual"}]}`,
		"kits/kit-2/skills/foo/SKILL.md": "# Foo\nFoo content.",
		"kits/kit-2/rules/no-foo.md":     "no foo",
		"kits/kit-2/rules/style2.md":     "style2",
		"kits/kit-3/kit.json":            `{"name":"kit-3","description":"k3","files":[{"path":"skills/bar","type":"skill"},{"path":"rules/no-bar.md","type":"rule","priority":"always"},{"path":"rules/style3.md","type":"rule","priority":"contextual"}]}`,
		"kits/kit-3/skills/bar/SKILL.md": "# Bar\nBar content.",
		"kits/kit-3/rules/no-bar.md":     "no bar",
		"kits/kit-3/rules/style3.md":     "style3",
	}
	testfixture.UpdateRemote(t, ctx, files)
}

// dropKitFromRegistry rewrites registry.json to advertise only test-kit
// and kit-3 — kit-2 vanishes upstream while its kit dir remains in
// HEAD. Update for kit-2 will see KitNotFound and (in skip-and-continue
// mode) record the failure without aborting.
func dropKit2FromRegistry(t *testing.T, ctx *testfixture.RemoteContext) {
	t.Helper()
	files := map[string]string{
		"registry.json": `{"version":"1","kits":{` +
			`"test-kit":{"description":"k1","types":["skill","rule"]},` +
			`"kit-3":{"description":"k3","types":["skill","rule"]}` +
			`}}`,
	}
	testfixture.UpdateRemote(t, ctx, files)
}

// installTwoKitsForMigration installs kit-a (skills [shared, kit-a-only])
// and kit-b (skill [other]) on the same source. Returns (project, home,
// ctx) for the test body to drive upstream changes.
func installTwoKitsForMigration(t *testing.T) (string, string, *testfixture.RemoteContext) {
	t.Helper()
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json": `{"version":"1","kits":{` +
			`"kit-a":{"description":"a","types":["skill"]},` +
			`"kit-b":{"description":"b","types":["skill"]}` +
			`}}`,
		"kits/kit-a/kit.json":                   `{"name":"kit-a","description":"a","files":[{"path":"skills/shared","type":"skill"},{"path":"skills/kit-a-only","type":"skill"}]}`,
		"kits/kit-a/skills/shared/SKILL.md":     "# shared (a)",
		"kits/kit-a/skills/kit-a-only/SKILL.md": "# kit-a-only",
		"kits/kit-b/kit.json":                   `{"name":"kit-b","description":"b","files":[{"path":"skills/other","type":"skill"}]}`,
		"kits/kit-b/skills/other/SKILL.md":      "# other",
	})
	if err := install.Install(project, "test-source/kit-a", home); err != nil {
		t.Fatalf("install kit-a: %v", err)
	}
	if err := install.Install(project, "test-source/kit-b", home); err != nil {
		t.Fatalf("install kit-b: %v", err)
	}
	return project, home, ctx
}

// TestUpdateCrossKitMigration is the regression test for spec 0016 §1(c)
// being closed by spec 0017's batch-aware uniqueness check. Skill
// 'shared' migrates from kit-a to kit-b in the same update batch — under
// per-kit uniqueness check (spec 0016) this fails because kit-b's plan
// sees `shared` still owned by kit-a's recorded names. Under the batch
// uniqueness check the post-update set has `shared` exactly once.
func TestUpdateCrossKitMigration(t *testing.T) {
	project, home, ctx := installTwoKitsForMigration(t)

	// Migrate skill 'shared' from kit-a to kit-b.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-a/kit.json":               `{"name":"kit-a","description":"a","files":[{"path":"skills/kit-a-only","type":"skill"}]}`,
		"kits/kit-b/kit.json":               `{"name":"kit-b","description":"b","files":[{"path":"skills/other","type":"skill"},{"path":"skills/shared","type":"skill"}]}`,
		"kits/kit-b/skills/shared/SKILL.md": "# shared (b)",
	})

	if err := Update(project, "", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	aNames := cfg.Installed["test-source/kit-a"].Names
	bNames := cfg.Installed["test-source/kit-b"].Names
	if !equalSet(aNames, []string{"kit-a-only"}) {
		t.Errorf("kit-a Names = %v, want [kit-a-only]", aNames)
	}
	if !equalSet(bNames, []string{"other", "shared"}) {
		t.Errorf("kit-b Names = %v, want [other shared]", bNames)
	}
	// kit-b's new shared skill is delivered; kit-a's shared skill dir
	// is gone.
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/shared/SKILL.md")); err != nil {
		t.Errorf("shared skill not delivered at new location: %v", err)
	}
}

// TestUpdateCrossKitMigrationTakerSortsFirst migrates in the other
// direction: kit-b gives skill 'other' to kit-a. Sorted targets put the
// taker (kit-a) first, so the update only succeeds because Plan moves
// the donor ahead (spec 0026 §4.2).
func TestUpdateCrossKitMigrationTakerSortsFirst(t *testing.T) {
	project, home, ctx := installTwoKitsForMigration(t)

	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-a/kit.json":              `{"name":"kit-a","description":"a","files":[{"path":"skills/shared","type":"skill"},{"path":"skills/kit-a-only","type":"skill"},{"path":"skills/other","type":"skill"}]}`,
		"kits/kit-a/skills/other/SKILL.md": "# other (a)",
		"kits/kit-b/kit.json":              `{"name":"kit-b","description":"b","files":[{"path":"skills/new-b","type":"skill"}]}`,
		"kits/kit-b/skills/new-b/SKILL.md": "# new-b",
	})

	if err := Update(project, "", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	if aNames := cfg.Installed["test-source/kit-a"].Names; !equalSet(aNames, []string{"shared", "kit-a-only", "other"}) {
		t.Errorf("kit-a Names = %v, want [shared kit-a-only other]", aNames)
	}
	got, err := os.ReadFile(filepath.Join(project, ".claude/skills/other/SKILL.md"))
	if err != nil || string(got) != "# other (a)" {
		t.Errorf("other/SKILL.md = %q (err %v), want kit-a's content", got, err)
	}
}

// TestUpdatePlanFailureLeavesDiskUntouched is the central guarantee
// 0017 buys: a kit whose plan fails is reported as needs-attention
// without any of its on-disk delivered files being touched. This
// closes the within-kit partial-state risk that spec 0016 §3
// explicitly accepts.
func TestUpdatePlanFailureLeavesDiskUntouched(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Capture the delivered file content before the failed update.
	skillPath := filepath.Join(project, ".claude/skills/review/SKILL.md")
	preContent, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read pre: %v", err)
	}

	// Drop test-kit from the registry — plan will report KitNotFound.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json": `{"version":"1","kits":{"placeholder":{"description":"p","types":["skill"]}}}`,
	})

	err = Update(project, "test-source/test-kit", false, false, home)
	if !errors.Is(err, errs.ErrKitNotFound) {
		t.Fatalf("want KitNotFound, got %v", err)
	}

	postContent, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read post: %v", err)
	}
	if string(postContent) != string(preContent) {
		t.Errorf("delivered file changed despite plan failure:\nbefore: %q\nafter:  %q",
			preContent, postContent)
	}
}

func equalSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, s := range got {
		seen[s]++
	}
	for _, s := range want {
		if seen[s] == 0 {
			return false
		}
		seen[s]--
	}
	return true
}

// TestUpdateMultiKitSkipAndContinue covers spec criterion 12: three
// installed kits, the middle one's source removes it from the registry,
// `inat update` (no args) should update / leave-up-to-date the other
// two and report kit-2 as needs-attention with KitsNeedAttention(1)
// returned. cfg.Installed retains kit-2's pre-update entry.
func TestUpdateMultiKitSkipAndContinue(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	addExtraKits(t, ctx)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	for _, k := range []string{"test-kit", "kit-2", "kit-3"} {
		if err := install.Install(project, "test-source/"+k, home); err != nil {
			t.Fatalf("install %s: %v", k, err)
		}
	}
	preCfg, _ := config.LoadConfig(project)
	preKit2 := preCfg.Installed["test-source/kit-2"]

	dropKit2FromRegistry(t, ctx)
	// Also bump kit-3's content so update has something to update.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-3/skills/bar/SKILL.md": "# Bar v2\nUpdated bar.",
	})

	err := Update(project, "", false, false, home)
	if !errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("want KitsNeedAttention, got %v", err)
	}
	var typed *errs.Error
	if !errors.As(err, &typed) {
		t.Fatalf("not a typed *errs.Error: %v", err)
	}
	if typed.Count != 1 {
		t.Fatalf("want Count=1, got %d", typed.Count)
	}

	postCfg, _ := config.LoadConfig(project)
	// kit-2 entry remains as it was before update — the failure didn't mutate it.
	postKit2 := postCfg.Installed["test-source/kit-2"]
	if postKit2.Tree != preKit2.Tree {
		t.Fatalf("kit-2 mutated despite needs-attention: pre=%s post=%s", preKit2.Tree, postKit2.Tree)
	}
	// kit-3 was updated (content changed → new tree).
	bar, _ := os.ReadFile(filepath.Join(project, ".claude/skills/bar/SKILL.md"))
	if !strings.Contains(string(bar), "Bar v2") {
		t.Fatalf("kit-3 not re-delivered: %s", bar)
	}
	// All three kits still present in cfg.Installed (no removals from update).
	for _, k := range []string{"test-source/test-kit", "test-source/kit-2", "test-source/kit-3"} {
		if _, ok := postCfg.Installed[k]; !ok {
			t.Fatalf("missing entry: %s in %+v", k, postCfg.Installed)
		}
	}
}

// --- Spec 0018: end-to-end auto-rename through `inat update` ---

// renameTestKitInRemote writes a byte-identical mirror of test-kit at
// newKitName and updates registry.json so it references only newKitName.
// Tree content is byte-identical so the new tree's SHA equals the
// pre-rename SHA — this is the auto-rename trigger condition.
func renameTestKitInRemote(t *testing.T, ctx *testfixture.RemoteContext, newKitName string) {
	t.Helper()
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json":                                  `{"version":"1","kits":{"` + newKitName + `":{"description":"Test","types":["skill","rule"]}}}`,
		"kits/" + newKitName + "/kit.json":               `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"kits/" + newKitName + "/skills/review/SKILL.md": "# Review\nDo code review.",
		"kits/" + newKitName + "/rules/no-secrets.md":    "Never commit secrets.",
		"kits/" + newKitName + "/rules/style.md":         "Follow style guidelines.",
	})
}

func TestUpdateAutoRenameApplied(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	preDelivered, _ := os.ReadFile(filepath.Join(project, ".claude/skills/review/SKILL.md"))

	renameTestKitInRemote(t, ctx, "test-kit-renamed")

	if err := Update(project, "", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Error("old key still in cfg.Installed after auto-rename")
	}
	if _, ok := cfg.Installed["test-source/test-kit-renamed"]; !ok {
		t.Fatal("new key missing from cfg.Installed after auto-rename")
	}
	// Delivered files must be byte-identical — auto-rename does not
	// re-deliver because the tree is unchanged.
	postDelivered, _ := os.ReadFile(filepath.Join(project, ".claude/skills/review/SKILL.md"))
	if string(preDelivered) != string(postDelivered) {
		t.Error("delivered content changed; auto-rename must not touch delivered files")
	}
	// Managed-section header in CLAUDE.md must reference the new key.
	claudeMd, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if !strings.Contains(string(claudeMd), "test-source/test-kit-renamed/") {
		t.Errorf("CLAUDE.md missing new-key header reference:\n%s", claudeMd)
	}
	if strings.Contains(string(claudeMd), "test-source/test-kit/") {
		t.Errorf("CLAUDE.md still references old-key header:\n%s", claudeMd)
	}
}

func TestUpdateSingleKitAutoRenameAgainstOldName(t *testing.T) {
	// Spec 0018 §2 goal 8 / acceptance 3: `inat update <src/old>` against
	// a kit that was renamed upstream MUST succeed via auto-rename.
	// Pre-0018 this returned KitNotFound; under 0018 it returns nil.
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	renameTestKitInRemote(t, ctx, "test-kit-renamed")

	if err := Update(project, "test-source/test-kit", false, false, home); err != nil {
		t.Fatalf("single-kit update against old name should auto-rename, got %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.Installed["test-source/test-kit-renamed"]; !ok {
		t.Fatal("auto-rename did not apply — new key missing")
	}
}

func TestUpdateSingleKitGenuinelyDeleted(t *testing.T) {
	// Spec 0018 §6 acceptance 11b: single-kit fail-fast for genuine
	// KitNotFound cases is preserved. A kit deleted upstream with no
	// tree-SHA match anywhere returns the underlying KitNotFound, not
	// nil and not KitsNeedAttention.
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	// Remove test-kit from registry without writing a tree-SHA mirror
	// anywhere. The registry needs at least one entry to pass validation;
	// add a distinct kit whose tree SHA cannot match test-kit's recorded
	// SHA (different files = different tree).
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json":                        `{"version":"1","kits":{"other-kit":{"description":"o","types":["skill"]}}}`,
		"kits/other-kit/kit.json":              `{"name":"other-kit","description":"o","files":[{"path":"skills/other","type":"skill"}]}`,
		"kits/other-kit/skills/other/SKILL.md": "# Other\nDifferent content.",
	})

	err := Update(project, "test-source/test-kit", false, false, home)
	if !errors.Is(err, errs.ErrKitNotFound) {
		t.Fatalf("want KitNotFound, got %v", err)
	}
	if errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("single-kit form must surface KitNotFound directly, not wrap in KitsNeedAttention: %v", err)
	}
}

func TestUpdateAutoRenameAmbiguousSurfacesError(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	// Two byte-identical mirrors of test-kit. Both share the recorded
	// tree SHA. Auto-detection refuses to pick.
	renameTestKitInRemote(t, ctx, "kit-a")
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json":                     `{"version":"1","kits":{"kit-a":{"description":"T","types":["skill","rule"]},"kit-b":{"description":"T","types":["skill","rule"]}}}`,
		"kits/kit-b/kit.json":               `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"kits/kit-b/skills/review/SKILL.md": "# Review\nDo code review.",
		"kits/kit-b/rules/no-secrets.md":    "Never commit secrets.",
		"kits/kit-b/rules/style.md":         "Follow style guidelines.",
	})

	// Multi-kit form: should exit with KitsNeedAttention(1).
	err := Update(project, "", false, false, home)
	if !errors.Is(err, errs.ErrKitsNeedAttention) {
		t.Fatalf("want KitsNeedAttention, got %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	// cfg.Installed unchanged — auto-rename refused, no apply.
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Error("old key removed despite ambiguity rejection")
	}
}

// --- Spec 0020: --plan dry-run ---

func TestUpdatePlanLeavesDiskUntouched(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(project, ".claude/skills/review/SKILL.md")
	cfgPath := filepath.Join(project, ".inat/config.json")
	preSkill, _ := os.ReadFile(skillPath)
	preCfg, _ := os.ReadFile(cfgPath)

	// Upstream changes; plan should see the update but not apply it.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nWould be applied without --plan.",
	})

	var err error
	stdout := captureStdout(t, func() {
		err = Update(project, "", false, true, home)
	})
	if err != nil {
		t.Fatalf("--plan update returned error: %v", err)
	}
	if want := "update test-source/test-kit\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	postSkill, _ := os.ReadFile(skillPath)
	if string(postSkill) != string(preSkill) {
		t.Errorf("delivered file changed under --plan:\nbefore: %q\nafter:  %q", preSkill, postSkill)
	}
	postCfg, _ := os.ReadFile(cfgPath)
	if string(postCfg) != string(preCfg) {
		t.Error("config.json changed under --plan")
	}
}

func TestUpdatePlanNeedsAttentionExitsZero(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	// Upstream deletes the kit; plan reports KitNotFound. --plan must
	// still exit 0 (the point of dry-run is to surface this).
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json": `{"version":"1","kits":{"placeholder":{"description":"p","types":["skill"]}}}`,
	})

	var err error
	stdout := captureStdout(t, func() {
		err = Update(project, "test-source/test-kit", false, true, home)
	})
	if err != nil {
		t.Fatalf("single-kit --plan must exit 0 even on needs-attention, got %v", err)
	}
	if !strings.HasPrefix(stdout, "needs-attention test-source/test-kit:") {
		t.Errorf("stdout missing needs-attention line:\n%s", stdout)
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Errorf("stdout missing trailing newline: %q", stdout)
	}
}

func TestUpdatePlanShowsAutoRenameWithoutApplying(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	renameTestKitInRemote(t, ctx, "test-kit-renamed")
	cfgPath := filepath.Join(project, ".inat/config.json")
	preCfg, _ := os.ReadFile(cfgPath)
	claudePath := filepath.Join(project, "CLAUDE.md")
	preClaude, _ := os.ReadFile(claudePath)

	var err error
	stdout := captureStdout(t, func() {
		err = Update(project, "", false, true, home)
	})
	if err != nil {
		t.Fatalf("--plan returned error: %v", err)
	}
	want := "rename test-source/test-kit → test-source/test-kit-renamed\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	postCfg, _ := os.ReadFile(cfgPath)
	if string(postCfg) != string(preCfg) {
		t.Error("config.json changed under --plan rename")
	}
	postClaude, _ := os.ReadFile(claudePath)
	if string(postClaude) != string(preClaude) {
		t.Error("CLAUDE.md changed under --plan rename")
	}
}

func TestUpdatePlanMultiKitMixedActions(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	addExtraKits(t, ctx)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	for _, k := range []string{"test-kit", "kit-2", "kit-3"} {
		if err := install.Install(project, "test-source/"+k, home); err != nil {
			t.Fatalf("install %s: %v", k, err)
		}
	}
	// Drop kit-2 from registry; bump kit-3.
	dropKit2FromRegistry(t, ctx)
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-3/skills/bar/SKILL.md": "# Bar v2\nUpdated.",
	})
	cfgPath := filepath.Join(project, ".inat/config.json")
	preCfg, _ := os.ReadFile(cfgPath)

	var err error
	stdout := captureStdout(t, func() {
		err = Update(project, "", false, true, home)
	})
	if err != nil {
		t.Fatalf("multi-kit --plan returned error: %v", err)
	}
	// Three detail lines, one per kit, in sorted install-key order
	// (spec 0026 §2.3: buildTargets sorts, no migrations to reorder).
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 detail lines, got %d:\n%s", len(lines), stdout)
	}
	if !strings.HasPrefix(lines[0], "needs-attention test-source/kit-2:") ||
		lines[1] != "update test-source/kit-3" ||
		lines[2] != "up-to-date test-source/test-kit" {
		t.Errorf("detail lines not in sorted order:\n%s", stdout)
	}
	postCfg, _ := os.ReadFile(cfgPath)
	if string(postCfg) != string(preCfg) {
		t.Error("config.json changed under multi-kit --plan")
	}
}

func TestUpdatePlanForceMutuallyExclusive(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	err := Update(project, "", true, true, home)
	if !errors.Is(err, errs.ErrKitValidation) {
		t.Fatalf("want KitValidation for --plan --force, got %v", err)
	}
}

// --- Spec 0021: apply-phase snapshot/restore ---

// TestUpdateApplyFailureRestoresFiles covers spec 0021 §6 criterion 3:
// an apply-phase failure during DeliverToAgent leaves every previously
// delivered file plus CLAUDE.md byte-identical to pre-apply. The
// failure is injected by extending the upstream kit with a new
// contextual rule whose destination is pre-created as a directory.
func TestUpdateApplyFailureRestoresFiles(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}

	skillPath := filepath.Join(project, ".claude/skills/review/SKILL.md")
	stylePath := filepath.Join(project, ".inat/rules/style.md")
	claudePath := filepath.Join(project, "CLAUDE.md")
	preSkill := mustReadUpdate(t, skillPath)
	preStyle := mustReadUpdate(t, stylePath)
	preClaude := mustReadUpdate(t, claudePath)

	// Upstream change: add a new contextual rule. Pre-create its
	// destination as a directory so DeliverToAgent's WriteFile fails.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/kit.json":          `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"},{"path":"rules/new-rule.md","type":"rule","priority":"contextual"}]}`,
		"kits/test-kit/rules/new-rule.md": "new rule content",
	})
	blocker := filepath.Join(project, ".inat/rules/new-rule.md")
	if err := os.MkdirAll(blocker, 0o755); err != nil {
		t.Fatal(err)
	}

	err := Update(project, "test-source/test-kit", false, false, home)
	if err == nil {
		t.Fatalf("Update should have failed with a deliver error")
	}

	if got := mustReadUpdate(t, skillPath); !bytes.Equal(got, preSkill) {
		t.Errorf("skill not restored byte-identical: got %q, want %q", got, preSkill)
	}
	if got := mustReadUpdate(t, stylePath); !bytes.Equal(got, preStyle) {
		t.Errorf("style.md not restored byte-identical: got %q, want %q", got, preStyle)
	}
	if got := mustReadUpdate(t, claudePath); !bytes.Equal(got, preClaude) {
		t.Errorf("CLAUDE.md not restored byte-identical: got %q, want %q", got, preClaude)
	}

	snaps, err := os.ReadDir(filepath.Join(home, ".inat", "snapshots"))
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("expected 1 snapshot dir after failure, got %d", len(snaps))
	}
}

// TestUpdateHappyPathLeavesNoSnapshot covers spec 0021 §6 criterion 2:
// a successful update discards its snapshot.
func TestUpdateHappyPathLeavesNoSnapshot(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated.",
	})
	if err := Update(project, "test-source/test-kit", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}
	snapDir := filepath.Join(home, ".inat", "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read snapshots dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected snapshots/ empty after success, got %d entries", len(entries))
	}
}

func mustReadUpdate(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestUpdateAutoRenameLeavesNoSnapshot covers spec 0021 §6 criterion 9:
// applyRename does not invoke runWithSnapshot, so an auto-rename apply
// creates no snapshot directory. Structurally guaranteed by apply.go's
// switch on a.Kind (only ActionUpdate and ActionReplace path through
// snapshot lifecycle), but worth an end-to-end assertion.
func TestUpdateAutoRenameLeavesNoSnapshot(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	renameTestKitInRemote(t, ctx, "test-kit-renamed")
	if err := Update(project, "", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}
	snapDir := filepath.Join(home, ".inat", "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read snapshots dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected snapshots/ empty after auto-rename, got %d entries", len(entries))
	}
}

// TestUpdateBatchIsolationOnFailure covers spec 0021 §6 criterion 8:
// in a multi-kit update where one kit's apply fails and another
// succeeds, the successful kit leaves no snapshot behind and the
// failing kit's snapshot survives. Each kit's snapshot lifecycle is
// independent.
func TestUpdateBatchIsolationOnFailure(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	addExtraKits(t, ctx)
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	if err := install.Install(project, "test-source/kit-2", home); err != nil {
		t.Fatal(err)
	}

	// Upstream change: kit-2 gains a new contextual rule whose
	// destination we'll block; test-kit just bumps its skill content
	// and should update cleanly.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated.",
		"kits/kit-2/kit.json":                  `{"name":"kit-2","description":"k2","files":[{"path":"skills/foo","type":"skill"},{"path":"rules/no-foo.md","type":"rule","priority":"always"},{"path":"rules/style2.md","type":"rule","priority":"contextual"},{"path":"rules/blocked2.md","type":"rule","priority":"contextual"}]}`,
		"kits/kit-2/rules/blocked2.md":         "blocked content",
	})
	blocker := filepath.Join(project, ".inat/rules/blocked2.md")
	if err := os.MkdirAll(blocker, 0o755); err != nil {
		t.Fatal(err)
	}

	// Batch update — test-kit should succeed, kit-2 should fail.
	err := Update(project, "", false, false, home)
	if err == nil {
		t.Fatalf("Update should have failed for kit-2")
	}

	snaps, err := os.ReadDir(filepath.Join(home, ".inat", "snapshots"))
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	// kit-2's failing apply leaves its snapshot; test-kit's succeeding
	// apply does not. Exactly one snapshot survives, and it belongs to
	// kit-2.
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot dir after batch failure, got %d: %v", len(snaps), entryNames(snaps))
	}
	if !strings.Contains(snaps[0].Name(), "test-source__kit-2") {
		t.Errorf("surviving snapshot is not kit-2's: %s", snaps[0].Name())
	}

	// test-kit's update did land on disk.
	got, _ := os.ReadFile(filepath.Join(project, ".claude/skills/review/SKILL.md"))
	if !strings.Contains(string(got), "Review v2") {
		t.Errorf("test-kit did not update despite kit-2 failing: %s", got)
	}
}

func entryNames(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}
