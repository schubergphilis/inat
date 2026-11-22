// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/cli/transaction"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/internal/testfixture"
)

// TestPlanApplyTakerFirstMigration is the spec 0026 regression test: a
// skill moving from kit-a to kit-b, with the targets in taker-first
// order. Without the reorder kit-b overwrites shared/SKILL.md, then
// kit-a's remove step sees a hash mismatch and aborts as
// locally-modified.
func TestPlanApplyTakerFirstMigration(t *testing.T) {
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
	for _, k := range []string{"test-source/kit-a", "test-source/kit-b"} {
		if err := install.Install(project, k, home); err != nil {
			t.Fatalf("install %s: %v", k, err)
		}
	}
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/kit-a/kit.json":               `{"name":"kit-a","description":"a","files":[{"path":"skills/kit-a-only","type":"skill"}]}`,
		"kits/kit-b/kit.json":               `{"name":"kit-b","description":"b","files":[{"path":"skills/other","type":"skill"},{"path":"skills/shared","type":"skill"}]}`,
		"kits/kit-b/skills/shared/SKILL.md": "# shared (b)",
	})
	cfg, _ := config.LoadConfig(project)

	plan, err := transaction.Plan(project, &cfg, []transaction.Target{
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/kit-b"},
		{Kind: transaction.TargetUpdate, InstallKey: "test-source/kit-a"},
	}, home)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := planKeys(plan); got[0] != "test-source/kit-a" {
		t.Errorf("plan order = %v, want donor kit-a first", got)
	}
	for _, r := range transaction.Apply(project, &cfg, plan, false, home) {
		if r.Kind != transaction.ActionUpdate || r.Err != nil {
			t.Errorf("%s: kind %v, err %v; want Update", r.InstallKey, r.Kind, r.Err)
		}
	}
	got, err := os.ReadFile(filepath.Join(project, ".claude/skills/shared/SKILL.md"))
	if err != nil || string(got) != "# shared (b)" {
		t.Errorf("shared/SKILL.md = %q (err %v), want kit-b's content", got, err)
	}
}

func planKeys(plan []transaction.PlannedAction) []string {
	keys := make([]string, len(plan))
	for i, a := range plan {
		keys[i] = a.InstallKey
	}
	return keys
}

func orderCfg(names map[string][]string) *config.Config {
	cfg := &config.Config{Installed: map[string]config.InstalledKit{}}
	for k, n := range names {
		cfg.Installed[k] = config.InstalledKit{Names: n}
	}
	return cfg
}

func update(key string, names ...string) transaction.PlannedAction {
	return transaction.PlannedAction{Kind: transaction.ActionUpdate, InstallKey: key, NewNames: names}
}

func assertOrder(t *testing.T, plan []transaction.PlannedAction, want ...string) {
	t.Helper()
	got := planKeys(plan)
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestOrderDonorsFirstMovesDonorAhead(t *testing.T) {
	cfg := orderCfg(map[string][]string{"s/a": {"x", "y"}, "s/b": {"z"}, "s/c": {"w"}})
	plan := []transaction.PlannedAction{update("s/c", "w"), update("s/b", "z", "x"), update("s/a", "y")}
	assertOrder(t, transaction.OrderDonorsFirstForTest(cfg, plan), "s/c", "s/a", "s/b")
}

func TestOrderDonorsFirstNoEdgesKeepsOrder(t *testing.T) {
	cfg := orderCfg(map[string][]string{"s/a": {"x"}, "s/b": {"y"}, "s/c": {"z"}})
	plan := []transaction.PlannedAction{update("s/c", "z"), update("s/a", "x", "q"), update("s/b", "y")}
	assertOrder(t, transaction.OrderDonorsFirstForTest(cfg, plan), "s/c", "s/a", "s/b")
}

func TestOrderDonorsFirstChain(t *testing.T) {
	// c gives x to b, b gives y to a: c, b, a.
	cfg := orderCfg(map[string][]string{"s/a": {}, "s/b": {"y"}, "s/c": {"x"}})
	plan := []transaction.PlannedAction{update("s/a", "y"), update("s/b", "x"), update("s/c")}
	assertOrder(t, transaction.OrderDonorsFirstForTest(cfg, plan), "s/c", "s/b", "s/a")
}

func TestOrderDonorsFirstSwapKeepsOrder(t *testing.T) {
	cfg := orderCfg(map[string][]string{"s/a": {"x"}, "s/b": {"y"}, "s/c": {"z"}})
	plan := []transaction.PlannedAction{update("s/b", "x"), update("s/c", "z"), update("s/a", "y")}
	assertOrder(t, transaction.OrderDonorsFirstForTest(cfg, plan), "s/c", "s/b", "s/a")
}

func TestOrderDonorsFirstIgnoresNonActionable(t *testing.T) {
	cfg := orderCfg(map[string][]string{"s/a": {"x"}, "s/b": {}})
	plan := []transaction.PlannedAction{
		update("s/b", "x"),
		{Kind: transaction.ActionNeedsAttention, InstallKey: "s/a"},
	}
	assertOrder(t, transaction.OrderDonorsFirstForTest(cfg, plan), "s/b", "s/a")
}
