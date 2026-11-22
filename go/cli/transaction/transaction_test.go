// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/cli/transaction"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func TestActionKindString(t *testing.T) {
	cases := []struct {
		kind transaction.ActionKind
		want string
	}{
		{transaction.ActionUpToDate, "up-to-date"},
		{transaction.ActionUpdate, "update"},
		{transaction.ActionReplace, "replace"},
		{transaction.ActionNeedsAttention, "needs-attention"},
		{transaction.ActionRename, "rename"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("ActionKind(%d).String() = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestActionKindStringUnknown(t *testing.T) {
	if got := transaction.ActionKind(99).String(); got != "unknown" {
		t.Errorf("ActionKind(99).String() = %q, want %q", got, "unknown")
	}
}

func TestTargetKindString(t *testing.T) {
	cases := []struct {
		kind transaction.TargetKind
		want string
	}{
		{transaction.TargetUpdate, "update"},
		{transaction.TargetReplace, "replace"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("TargetKind(%d).String() = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestTargetKindStringUnknown(t *testing.T) {
	if got := transaction.TargetKind(99).String(); got != "unknown" {
		t.Errorf("TargetKind(99).String() = %q, want %q", got, "unknown")
	}
}

func TestPlannedActionIsActionable(t *testing.T) {
	cases := []struct {
		name string
		kind transaction.ActionKind
		want bool
	}{
		{"up-to-date", transaction.ActionUpToDate, false},
		{"update", transaction.ActionUpdate, true},
		{"replace", transaction.ActionReplace, true},
		{"needs-attention", transaction.ActionNeedsAttention, false},
		{"rename", transaction.ActionRename, true},
	}
	for _, c := range cases {
		a := transaction.PlannedAction{Kind: c.kind}
		if got := a.IsActionable(); got != c.want {
			t.Errorf("%s: IsActionable() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPlannedActionZeroValue(t *testing.T) {
	var a transaction.PlannedAction
	if a.Kind != transaction.ActionUpToDate {
		t.Errorf("zero PlannedAction.Kind = %v, want ActionUpToDate", a.Kind)
	}
	if a.IsActionable() {
		t.Error("zero PlannedAction should not be actionable")
	}
	if a.InstallKey != "" || a.NewKey != "" || a.NewTreeSHA != "" {
		t.Error("zero PlannedAction strings should be empty")
	}
	if a.NewKitCfg != nil || a.NewFiles != nil || a.NewNames != nil || a.Err != nil {
		t.Error("zero PlannedAction reference fields should be nil")
	}
}

func TestPlannedActionCarriesError(t *testing.T) {
	want := errors.New("boom")
	a := transaction.PlannedAction{
		Kind:       transaction.ActionNeedsAttention,
		InstallKey: "src/kit",
		Err:        want,
	}
	if !errors.Is(a.Err, want) {
		t.Errorf("PlannedAction.Err = %v, want errors.Is %v", a.Err, want)
	}
	if a.IsActionable() {
		t.Error("NeedsAttention action must not be actionable")
	}
}

func TestApplyResultZeroValue(t *testing.T) {
	var r transaction.ApplyResult
	if r.Kind != transaction.ActionUpToDate {
		t.Errorf("zero ApplyResult.Kind = %v, want ActionUpToDate", r.Kind)
	}
	if r.InstallKey != "" {
		t.Errorf("zero ApplyResult.InstallKey = %q, want empty", r.InstallKey)
	}
	if r.Err != nil {
		t.Errorf("zero ApplyResult.Err = %v, want nil", r.Err)
	}
}

func TestTargetZeroValue(t *testing.T) {
	var tgt transaction.Target
	if tgt.Kind != transaction.TargetUpdate {
		t.Errorf("zero Target.Kind = %v, want TargetUpdate", tgt.Kind)
	}
	if tgt.InstallKey != "" || tgt.NewRef != "" {
		t.Error("zero Target strings should be empty")
	}
}

// --- Plan: TargetUpdate paths ---

func TestPlanEmpty(t *testing.T) {
	project := testfixture.SetupProject(t, "ignored")
	cfg, _ := config.LoadConfig(project)
	plan, err := transaction.Plan(project, &cfg, nil, t.TempDir())
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 0 {
		t.Fatalf("plan len = %d, want 0", len(plan))
	}
}

func TestPlanUpdateKitNotInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/missing"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want one NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrKitNotInstalled) {
		t.Fatalf("plan[0].Err = %v, want KitNotInstalled", plan[0].Err)
	}
	if plan[0].InstallKey != "test-source/missing" {
		t.Errorf("plan[0].InstallKey = %q", plan[0].InstallKey)
	}
}

func TestPlanUpdateUpToDate(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionUpToDate {
		t.Fatalf("plan = %+v, want one UpToDate", plan)
	}
	if plan[0].Err != nil {
		t.Errorf("UpToDate must not carry Err: %v", plan[0].Err)
	}
}

func TestPlanUpdateTreeDiffers(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	preCfg, _ := config.LoadConfig(project)
	oldTree := preCfg.Installed["test-source/test-kit"].Tree

	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated.",
	})

	plan, err := transaction.Plan(project, &preCfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionUpdate {
		t.Fatalf("plan = %+v, want one Update", plan)
	}
	a := plan[0]
	if a.NewTreeSHA == "" || a.NewTreeSHA == oldTree {
		t.Fatalf("NewTreeSHA = %q (old %q), want a new non-empty SHA", a.NewTreeSHA, oldTree)
	}
	if a.NewKitCfg == nil {
		t.Error("NewKitCfg = nil, want populated")
	}
	if len(a.NewFiles) == 0 {
		t.Error("NewFiles empty, want populated")
	}
	if len(a.NewNames) == 0 {
		t.Error("NewNames empty, want populated")
	}
	// cfg must NOT have been mutated.
	if preCfg.Installed["test-source/test-kit"].Tree != oldTree {
		t.Error("Plan mutated cfg.Installed; spec §2 forbids it")
	}
}

func TestPlanUpdateKitNotFound(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Drop test-kit from the registry (leave one other entry so the
	// registry itself still validates).
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json": `{"version":"1","kits":{"other-kit":{"description":"o","types":["skill"]}}}`,
	})
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want one NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrKitNotFound) {
		t.Fatalf("plan[0].Err = %v, want KitNotFound", plan[0].Err)
	}
}

func TestPlanUpdateSourceNotFound(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	// Inject an Installed entry referencing a source that doesn't exist
	// in cfg.Sources.
	cfg.Installed["ghost/orphan-kit"] = config.InstalledKit{
		Tree:      "00000000",
		Names:     []string{"orphan"},
		Delivered: nil,
	}

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "ghost/orphan-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want one NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrSourceNotFound) {
		t.Fatalf("plan[0].Err = %v, want SourceNotFound", plan[0].Err)
	}
}

// addSecondKitToRemote extends the standard test remote with a second
// kit (kit-2) whose derived names are distinct from test-kit so they
// can coexist in cfg.Installed.
func addSecondKitToRemote(t *testing.T, ctx *testfixture.RemoteContext) {
	t.Helper()
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json": `{"version":"1","kits":{` +
			`"test-kit":{"description":"k1","types":["skill","rule"]},` +
			`"kit-2":{"description":"k2","types":["skill"]}` +
			`}}`,
		"kits/kit-2/kit.json":            `{"name":"kit-2","description":"k2","files":[{"path":"skills/foo","type":"skill"}]}`,
		"kits/kit-2/skills/foo/SKILL.md": "# Foo\nFoo content.",
	})
}

// --- Plan: TargetReplace paths ---

func TestPlanReplaceSameSourceHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	addSecondKitToRemote(t, ctx)
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{{
		Kind:       transaction.TargetReplace,
		InstallKey: "test-source/test-kit",
		NewRef:     "test-source/kit-2",
	}}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionReplace {
		t.Fatalf("plan = %+v, want one Replace", plan)
	}
	a := plan[0]
	if a.InstallKey != "test-source/test-kit" {
		t.Errorf("InstallKey = %q", a.InstallKey)
	}
	if a.NewKey != "test-source/kit-2" {
		t.Errorf("NewKey = %q", a.NewKey)
	}
	if a.NewTreeSHA == "" {
		t.Error("NewTreeSHA empty")
	}
	if a.NewKitCfg == nil {
		t.Error("NewKitCfg nil")
	}
	if len(a.NewFiles) == 0 {
		t.Error("NewFiles empty")
	}
	if len(a.NewNames) != 1 || a.NewNames[0] != "foo" {
		t.Errorf("NewNames = %v, want [foo]", a.NewNames)
	}
}

func TestPlanReplaceOldNotInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	cfg, _ := config.LoadConfig(project)

	plan, _ := transaction.Plan(project, &cfg, []transaction.Target{{
		Kind:       transaction.TargetReplace,
		InstallKey: "test-source/missing",
		NewRef:     "test-source/test-kit",
	}}, t.TempDir())
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrKitNotInstalled) {
		t.Errorf("Err = %v, want KitNotInstalled", plan[0].Err)
	}
}

func TestPlanReplaceNewAlreadyInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	cfg.Installed["test-source/already"] = config.InstalledKit{
		Tree: "deadbeef", Names: []string{"already"},
	}

	plan, _ := transaction.Plan(project, &cfg, []transaction.Target{{
		Kind:       transaction.TargetReplace,
		InstallKey: "test-source/test-kit",
		NewRef:     "test-source/already",
	}}, home)
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrKitAlreadyInstalled) {
		t.Errorf("Err = %v, want KitAlreadyInstalled", plan[0].Err)
	}
}

func TestPlanReplaceNewRefMalformed(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)

	plan, _ := transaction.Plan(project, &cfg, []transaction.Target{{
		Kind:       transaction.TargetReplace,
		InstallKey: "test-source/test-kit",
		NewRef:     "no-slash-here",
	}}, home)
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrKitValidation) {
		t.Errorf("Err = %v, want KitValidation", plan[0].Err)
	}
}

func TestPlanReplaceNewSourceNotFound(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)

	plan, _ := transaction.Plan(project, &cfg, []transaction.Target{{
		Kind:       transaction.TargetReplace,
		InstallKey: "test-source/test-kit",
		NewRef:     "ghost-source/some-kit",
	}}, home)
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrSourceNotFound) {
		t.Errorf("Err = %v, want SourceNotFound", plan[0].Err)
	}
}

func TestPlanReplaceNewKitNotInRegistry(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)

	plan, _ := transaction.Plan(project, &cfg, []transaction.Target{{
		Kind:       transaction.TargetReplace,
		InstallKey: "test-source/test-kit",
		NewRef:     "test-source/no-such-kit",
	}}, home)
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrKitNotFound) {
		t.Errorf("Err = %v, want KitNotFound", plan[0].Err)
	}
}

// --- Apply ---

func TestApplyEmpty(t *testing.T) {
	project := testfixture.SetupProject(t, "ignored")
	cfg, _ := config.LoadConfig(project)
	results := transaction.Apply(project, &cfg, nil, false, t.TempDir())
	if len(results) != 0 {
		t.Fatalf("results len = %d, want 0", len(results))
	}
}

func TestApplyUpToDate(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	preTree := cfg.Installed["test-source/test-kit"].Tree

	plan := []transaction.PlannedAction{
		{Kind: transaction.ActionUpToDate, InstallKey: "test-source/test-kit"},
	}
	results := transaction.Apply(project, &cfg, plan, false, home)
	if len(results) != 1 || results[0].Kind != transaction.ActionUpToDate {
		t.Fatalf("results = %+v, want one UpToDate", results)
	}
	if results[0].Err != nil {
		t.Errorf("Err = %v, want nil", results[0].Err)
	}
	if cfg.Installed["test-source/test-kit"].Tree != preTree {
		t.Error("UpToDate apply mutated cfg")
	}
}

func TestApplyNeedsAttention(t *testing.T) {
	project := testfixture.SetupProject(t, "ignored")
	cfg, _ := config.LoadConfig(project)
	wantErr := errs.NewKitNotInstalled("ghost/orphan")
	plan := []transaction.PlannedAction{
		{Kind: transaction.ActionNeedsAttention, InstallKey: "ghost/orphan", Err: wantErr},
	}
	results := transaction.Apply(project, &cfg, plan, false, t.TempDir())
	if len(results) != 1 || results[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("results = %+v, want one NeedsAttention", results)
	}
	if !errors.Is(results[0].Err, errs.ErrKitNotInstalled) {
		t.Errorf("Err = %v, want KitNotInstalled", results[0].Err)
	}
}

func TestApplyUpdateHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review v2\nUpdated.",
	})
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	results := transaction.Apply(project, &cfg, plan, false, home)
	if len(results) != 1 || results[0].Kind != transaction.ActionUpdate {
		t.Fatalf("results = %+v, want one Update", results)
	}
	got, _ := os.ReadFile(filepath.Join(project, ".claude/skills/review/SKILL.md"))
	if !strings.Contains(string(got), "Review v2") {
		t.Fatalf("file not updated: %s", got)
	}
	if cfg.Installed["test-source/test-kit"].Tree != plan[0].NewTreeSHA {
		t.Errorf("cfg.Installed.Tree = %q, want %q",
			cfg.Installed["test-source/test-kit"].Tree, plan[0].NewTreeSHA)
	}
}

func TestApplyReplaceHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	addSecondKitToRemote(t, ctx)
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetReplace, InstallKey: "test-source/test-kit", NewRef: "test-source/kit-2"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	results := transaction.Apply(project, &cfg, plan, false, home)
	if len(results) != 1 || results[0].Kind != transaction.ActionReplace {
		t.Fatalf("results = %+v, want one Replace; err: %v", results, results[0].Err)
	}

	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Error("old key still in cfg.Installed")
	}
	if _, ok := cfg.Installed["test-source/kit-2"]; !ok {
		t.Error("new key missing from cfg.Installed")
	}
	// kit-2's only delivered file is the foo skill — verify it landed.
	got, err := os.ReadFile(filepath.Join(project, ".claude/skills/foo/SKILL.md"))
	if err != nil {
		t.Fatalf("foo skill not delivered: %v", err)
	}
	if !strings.Contains(string(got), "Foo") {
		t.Errorf("foo skill content unexpected: %s", got)
	}
	// test-kit's review skill should be gone.
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("review skill should be removed, stat err = %v", err)
	}
}

func TestApplyMixedFailureIsolation(t *testing.T) {
	// One UpToDate, one Update happy path, one NeedsAttention. Apply
	// must honour all three independently.
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := config.LoadConfig(project)

	wantErr := errs.NewKitNotInstalled("ghost/orphan")
	plan := []transaction.PlannedAction{
		{Kind: transaction.ActionUpToDate, InstallKey: "test-source/test-kit"},
		{Kind: transaction.ActionNeedsAttention, InstallKey: "ghost/orphan", Err: wantErr},
	}
	results := transaction.Apply(project, &cfg, plan, false, home)
	if len(results) != 2 {
		t.Fatalf("results len = %d", len(results))
	}
	if results[0].Kind != transaction.ActionUpToDate {
		t.Errorf("results[0].Kind = %v", results[0].Kind)
	}
	if results[1].Kind != transaction.ActionNeedsAttention {
		t.Errorf("results[1].Kind = %v", results[1].Kind)
	}
	if !errors.Is(results[1].Err, errs.ErrKitNotInstalled) {
		t.Errorf("results[1].Err = %v", results[1].Err)
	}
}

// --- Plan: global post-update name uniqueness ---

// setupTwoKitFixture installs kit-a (skill 'shared', skill 'kit-a-only')
// and kit-b (skill 'other') in the same source. Returns (project, home,
// ctx) for the test body.
func setupTwoKitFixture(t *testing.T) (string, string, *testfixture.RemoteContext) {
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

func TestPlanGlobalUniquenessAllowsCrossKitMigration(t *testing.T) {
	project, home, ctx := setupTwoKitFixture(t)
	// Migrate skill 'shared' from kit-a to kit-b in a single batch.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-a/kit.json":               `{"name":"kit-a","description":"a","files":[{"path":"skills/kit-a-only","type":"skill"}]}`,
		"kits/kit-b/kit.json":               `{"name":"kit-b","description":"b","files":[{"path":"skills/other","type":"skill"},{"path":"skills/shared","type":"skill"}]}`,
		"kits/kit-b/skills/shared/SKILL.md": "# shared (b)",
	})
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/kit-a"},
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/kit-b"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("plan len = %d", len(plan))
	}
	for _, a := range plan {
		if a.Kind != transaction.ActionUpdate {
			t.Errorf("%s: kind = %v, want Update; err: %v",
				a.InstallKey, a.Kind, a.Err)
		}
	}
}

func TestPlanGlobalUniquenessRejectsBothAddSameName(t *testing.T) {
	project, home, ctx := setupTwoKitFixture(t)
	// Both kits add skill 'collision'.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-a/kit.json":                  `{"name":"kit-a","description":"a","files":[{"path":"skills/kit-a-only","type":"skill"},{"path":"skills/collision","type":"skill"}]}`,
		"kits/kit-a/skills/collision/SKILL.md": "# collision (a)",
		"kits/kit-b/kit.json":                  `{"name":"kit-b","description":"b","files":[{"path":"skills/other","type":"skill"},{"path":"skills/collision","type":"skill"}]}`,
		"kits/kit-b/skills/collision/SKILL.md": "# collision (b)",
	})
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/kit-a"},
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/kit-b"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	for _, a := range plan {
		if a.Kind != transaction.ActionNeedsAttention {
			t.Errorf("%s: kind = %v, want NeedsAttention", a.InstallKey, a.Kind)
		}
		if !errors.Is(a.Err, errs.ErrDuplicateName) {
			t.Errorf("%s: err = %v, want DuplicateName", a.InstallKey, a.Err)
		}
	}
}

func TestPlanGlobalUniquenessRejectsCollisionWithNonBatchKit(t *testing.T) {
	project, home, ctx := setupTwoKitFixture(t)
	// kit-a's update adds skill 'other' which kit-b already owns
	// (kit-b is NOT in the plan).
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-a/kit.json":              `{"name":"kit-a","description":"a","files":[{"path":"skills/kit-a-only","type":"skill"},{"path":"skills/other","type":"skill"}]}`,
		"kits/kit-a/skills/other/SKILL.md": "# other (a)",
	})
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/kit-a"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrDuplicateName) {
		t.Errorf("Err = %v, want DuplicateName", plan[0].Err)
	}
}

func TestPlanGlobalUniquenessRejectsReservedName(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Upstream renames a rule path to derive 'inat-usage' (reserved).
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/kit.json":            `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/inat-usage.md","type":"rule","priority":"contextual"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"kits/test-kit/rules/inat-usage.md": "trying to grab the reserved name",
	})
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrReservedName) {
		t.Errorf("Err = %v, want ReservedName", plan[0].Err)
	}
}

func TestPlanUpdateFetchFailure(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Corrupt the source URL so fetch fails. Must use a brand new home so
	// no cache exists to fall back on.
	cfg, _ := config.LoadConfig(project)
	cfg.Sources[0].URL = "/path/that/does/not/exist/anywhere"

	freshHome := t.TempDir()
	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, freshHome)
	if err != nil {
		t.Fatalf("Plan err: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want one NeedsAttention", plan)
	}
	if plan[0].Err == nil {
		t.Error("plan[0].Err = nil, want a non-nil fetch error")
	}
}

// --- Spec 0018: auto-rename detection ---

// renameKitInRemote writes a byte-identical mirror of `oldKitName` under
// `newKitName` and updates registry.json so it references only the new
// name. The old kit directory is left in place (harmless — registry no
// longer points at it; detection scans the registry, not the
// filesystem). The new tree's SHA equals the old tree's SHA because
// content is identical, which is exactly the auto-rename trigger.
func renameKitInRemote(t *testing.T, ctx *testfixture.RemoteContext, newKitName, registryJSON string) {
	t.Helper()
	updates := map[string]string{
		"registry.json": registryJSON,
		// Mirror of testfixture.standardFiles() under the new kit name.
		"kits/" + newKitName + "/kit.json":               `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"kits/" + newKitName + "/skills/review/SKILL.md": "# Review\nDo code review.",
		"kits/" + newKitName + "/rules/no-secrets.md":    "Never commit secrets.",
		"kits/" + newKitName + "/rules/style.md":         "Follow style guidelines.",
	}
	testfixture.UpdateRemote(t, ctx, updates)
}

func TestPlanUpdateAutoRenameHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	preCfg, _ := config.LoadConfig(project)
	preTree := preCfg.Installed["test-source/test-kit"].Tree
	preNames := append([]string(nil), preCfg.Installed["test-source/test-kit"].Names...)

	renameKitInRemote(t, ctx, "test-kit-renamed",
		`{"version":"1","kits":{"test-kit-renamed":{"description":"Test","types":["skill","rule"]}}}`)

	cfg, _ := config.LoadConfig(project)
	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionRename {
		t.Fatalf("plan = %+v, want one ActionRename", plan)
	}
	a := plan[0]
	if a.InstallKey != "test-source/test-kit" {
		t.Errorf("InstallKey = %q", a.InstallKey)
	}
	if a.NewKey != "test-source/test-kit-renamed" {
		t.Errorf("NewKey = %q, want test-source/test-kit-renamed", a.NewKey)
	}
	if a.NewTreeSHA != preTree {
		t.Errorf("NewTreeSHA = %q, want unchanged %q", a.NewTreeSHA, preTree)
	}
	if !equalStringSlices(a.NewNames, preNames) {
		t.Errorf("NewNames = %v, want unchanged %v", a.NewNames, preNames)
	}
	if a.NewKitCfg != nil || a.NewFiles != nil {
		t.Error("ActionRename must not populate NewKitCfg / NewFiles")
	}
}

func TestPlanUpdateAutoRenameAmbiguous(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Two byte-identical copies of test-kit at two new paths. Both share
	// the same tree SHA as the installed kit. Auto-detection MUST refuse
	// to pick.
	renameKitInRemote(t, ctx, "kit-a",
		`{"version":"1","kits":{"kit-a":{"description":"T","types":["skill","rule"]},"kit-b":{"description":"T","types":["skill","rule"]}}}`)
	renameKitInRemote(t, ctx, "kit-b",
		`{"version":"1","kits":{"kit-a":{"description":"T","types":["skill","rule"]},"kit-b":{"description":"T","types":["skill","rule"]}}}`)

	cfg, _ := config.LoadConfig(project)
	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want one NeedsAttention", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrRenameAmbiguous) {
		t.Fatalf("plan[0].Err = %v, want RenameAmbiguous", plan[0].Err)
	}
	// Message must mention both candidates (sorted alphabetically).
	msg := plan[0].Err.Error()
	if !strings.Contains(msg, "kit-a") || !strings.Contains(msg, "kit-b") {
		t.Errorf("ambiguous err missing candidates: %s", msg)
	}
}

func TestPlanUpdateAutoRenameTreeMismatchFallsThrough(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Rename AND edit content: the new tree's SHA differs from the
	// recorded one, so auto-detection finds no candidates and falls
	// through to KitNotFound (today's behaviour preserved).
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json":                                `{"version":"1","kits":{"test-kit-renamed":{"description":"T","types":["skill","rule"]}}}`,
		"kits/test-kit-renamed/kit.json":               `{"name":"test-kit","description":"T","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"kits/test-kit-renamed/skills/review/SKILL.md": "# Review v2\nDIFFERENT.", // content edit
		"kits/test-kit-renamed/rules/no-secrets.md":    "Never commit secrets.",
		"kits/test-kit-renamed/rules/style.md":         "Follow style guidelines.",
	})

	cfg, _ := config.LoadConfig(project)
	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("plan = %+v, want NeedsAttention (no auto-rename when tree differs)", plan)
	}
	if !errors.Is(plan[0].Err, errs.ErrKitNotFound) {
		t.Fatalf("plan[0].Err = %v, want KitNotFound (auto-rename declined, fell through)", plan[0].Err)
	}
}

func TestPlanRenameDoesNotSelfCollide(t *testing.T) {
	// Verifies the ownKey fix in checkBatchUniqueness: a single
	// ActionRename's names must NOT be reported as colliding with
	// themselves at the new key. Without ownKey = a.NewKey for
	// ActionRename, this test would fail.
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	renameKitInRemote(t, ctx, "test-kit-renamed",
		`{"version":"1","kits":{"test-kit-renamed":{"description":"T","types":["skill","rule"]}}}`)

	cfg, _ := config.LoadConfig(project)
	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionRename {
		t.Fatalf("plan = %+v, want ActionRename (no self-collision after ownKey fix)", plan)
	}
	if plan[0].Err != nil {
		t.Errorf("ActionRename carries Err = %v, want nil", plan[0].Err)
	}
}

func TestPlanLazyIndexZeroBuildsCleanBatch(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	// No upstream changes — every target resolves cleanly.
	var builds int
	restore := transaction.SetBuildHookForTest(func() { builds++ })
	defer restore()

	cfg, _ := config.LoadConfig(project)
	if _, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if builds != 0 {
		t.Errorf("builds = %d, want 0 (clean batch must not build index)", builds)
	}
}

func TestPlanLazyIndexOneBuildPerSource(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	renameKitInRemote(t, ctx, "test-kit-renamed",
		`{"version":"1","kits":{"test-kit-renamed":{"description":"T","types":["skill","rule"]}}}`)

	var builds int
	restore := transaction.SetBuildHookForTest(func() { builds++ })
	defer restore()

	cfg, _ := config.LoadConfig(project)
	if _, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if builds != 1 {
		t.Errorf("builds = %d, want 1 (one source needed detection)", builds)
	}
}

func TestPlanLazyIndexLocalisedError(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Registry lists both a working renamed-kit AND a phantom entry whose
	// directory does not exist on disk. FetchKitTreeAt for the phantom
	// errors mid-build; per spec 0018 §4.3 the partial index is retained
	// and the phantom is dropped silently — detection for the working
	// kit still succeeds.
	renameKitInRemote(t, ctx, "test-kit-renamed",
		`{"version":"1","kits":{"test-kit-renamed":{"description":"T","types":["skill","rule"]},"phantom":{"description":"missing","types":["skill"]}}}`)

	cfg, _ := config.LoadConfig(project)
	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Kind != transaction.ActionRename {
		t.Fatalf("plan = %+v, want ActionRename despite phantom registry entry", plan)
	}
	if plan[0].NewKey != "test-source/test-kit-renamed" {
		t.Errorf("NewKey = %q, want test-source/test-kit-renamed", plan[0].NewKey)
	}
}

func TestApplyRenameHappyPath(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	preDeliveredSHA := mustReadFile(t, filepath.Join(project, ".claude/skills/review/SKILL.md"))

	renameKitInRemote(t, ctx, "test-kit-renamed",
		`{"version":"1","kits":{"test-kit-renamed":{"description":"T","types":["skill","rule"]}}}`)

	cfg, _ := config.LoadConfig(project)
	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	results := transaction.Apply(project, &cfg, plan, false, home)
	if len(results) != 1 || results[0].Kind != transaction.ActionRename {
		t.Fatalf("results = %+v (err: %v), want ActionRename", results, results[0].Err)
	}
	if _, ok := cfg.Installed["test-source/test-kit"]; ok {
		t.Error("old key still in cfg.Installed after rename")
	}
	newEntry, ok := cfg.Installed["test-source/test-kit-renamed"]
	if !ok {
		t.Fatal("new key missing from cfg.Installed")
	}
	if newEntry.Tree == "" {
		t.Error("new entry.Tree empty — should carry pre-rename SHA byte-identically")
	}
	// On-disk delivered file content must be byte-identical — auto-rename
	// does not re-deliver. This is the whole reason auto-rename is safe.
	postDeliveredSHA := mustReadFile(t, filepath.Join(project, ".claude/skills/review/SKILL.md"))
	if preDeliveredSHA != postDeliveredSHA {
		t.Error("delivered file content changed across rename; auto-rename must not touch delivered files")
	}
	// CLAUDE.md's managed-section block header must now reference the new key.
	claudeMd, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if !strings.Contains(string(claudeMd), "test-source/test-kit-renamed/") {
		t.Errorf("CLAUDE.md missing new-key header reference; content:\n%s", claudeMd)
	}
	if strings.Contains(string(claudeMd), "test-source/test-kit/") {
		t.Errorf("CLAUDE.md still references old-key header; content:\n%s", claudeMd)
	}
}

func TestApplyRenameHeaderRewriteFailureRollback(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatalf("install: %v", err)
	}
	renameKitInRemote(t, ctx, "test-kit-renamed",
		`{"version":"1","kits":{"test-kit-renamed":{"description":"T","types":["skill","rule"]}}}`)

	// Force a header-rewrite failure: replace CLAUDE.md with a directory
	// so delivery.RenameKitBlocks's read returns EISDIR.
	claudeMd := filepath.Join(project, "CLAUDE.md")
	if err := os.Remove(claudeMd); err != nil {
		t.Fatalf("remove CLAUDE.md: %v", err)
	}
	if err := os.Mkdir(claudeMd, 0o755); err != nil {
		t.Fatalf("mkdir CLAUDE.md: %v", err)
	}

	cfg, _ := config.LoadConfig(project)
	preEntry := cfg.Installed["test-source/test-kit"]

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/test-kit"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan[0].Kind != transaction.ActionRename {
		t.Fatalf("Plan: expected ActionRename, got %v", plan[0].Kind)
	}
	results := transaction.Apply(project, &cfg, plan, false, home)
	if results[0].Kind != transaction.ActionNeedsAttention {
		t.Fatalf("results[0].Kind = %v, want NeedsAttention (rewrite must fail)", results[0].Kind)
	}
	if results[0].Err == nil {
		t.Error("ActionNeedsAttention without Err")
	}
	// In-memory rollback: old key restored, new key absent.
	postEntry, ok := cfg.Installed["test-source/test-kit"]
	if !ok {
		t.Fatal("old key missing after rollback")
	}
	if postEntry.Tree != preEntry.Tree {
		t.Errorf("old entry Tree changed across rollback: got %q, want %q", postEntry.Tree, preEntry.Tree)
	}
	if _, ok := cfg.Installed["test-source/test-kit-renamed"]; ok {
		t.Error("new key present after rollback — should have been deleted")
	}
}

// mustReadFile reads the entire file or fails the test.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
