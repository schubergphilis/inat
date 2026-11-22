// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package scopecmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentscmd "github.com/schubergphilis/inat/cli/agents"
	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/cli/refresh"
	removecmd "github.com/schubergphilis/inat/cli/remove"
	"github.com/schubergphilis/inat/cli/status"
	"github.com/schubergphilis/inat/cli/update"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/internal/testfixture"
)

const userText = "# My rules\n\nHand-written, never touched.\n"

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// installUserKit seeds ~/.claude/CLAUDE.md with user text and installs
// the standard test kit (skill review, always no-secrets, contextual
// style) at user scope.
func installUserKit(t *testing.T) (home string, ctx *testfixture.RemoteContext) {
	t.Helper()
	ctx = testfixture.CreateTestRemote(t)
	home = testfixture.SetupUserScope(t, ctx.URL, "claude-code")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude/CLAUDE.md"), []byte(userText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := install.Install(home, "test-source/test-kit", home); err != nil {
		t.Fatalf("user install: %v", err)
	}
	return home, ctx
}

// Acceptance 3: user-scope install delivers to the user targets with a
// ~/-form reference, and nothing to the project-scope targets.
func TestUserInstallDeliversToUserTargets(t *testing.T) {
	home, _ := installUserKit(t)
	if !strings.Contains(read(t, filepath.Join(home, ".claude/skills/review/SKILL.md")), "code review") {
		t.Error("skill not delivered")
	}
	if !strings.Contains(read(t, filepath.Join(home, ".inat/rules/style.md")), "style guidelines") {
		t.Error("contextual rule not delivered")
	}
	md := read(t, filepath.Join(home, ".claude/CLAUDE.md"))
	if !strings.Contains(md, "Never commit secrets.") {
		t.Errorf("always rule not inlined:\n%s", md)
	}
	if !strings.Contains(md, "- ~/.inat/rules/style.md") || strings.Contains(md, "- .inat/rules/style.md") {
		t.Errorf("reference not in ~/ form:\n%s", md)
	}
	if exists(filepath.Join(home, "CLAUDE.md")) {
		t.Error("project-scope CLAUDE.md written at home root")
	}
	cfg, err := config.LoadUserConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Installed["test-source/test-kit"]; !ok {
		t.Errorf("kit not recorded in user config: %+v", cfg.Installed)
	}
}

// Acceptance 4 + 5: remove deletes exactly the delivered files, the block
// and the reference; user text in ~/.claude/CLAUDE.md is byte-identical.
func TestUserRemoveRestoresUserText(t *testing.T) {
	home, _ := installUserKit(t)
	if err := removecmd.Remove(home, "test-source/test-kit", false, home); err != nil {
		t.Fatalf("user remove: %v", err)
	}
	for _, rel := range []string{".claude/skills/review/SKILL.md", ".inat/rules/style.md"} {
		if exists(filepath.Join(home, rel)) {
			t.Errorf("%s left behind", rel)
		}
	}
	md := read(t, filepath.Join(home, ".claude/CLAUDE.md"))
	if !strings.HasPrefix(md, userText) || strings.Contains(md, "secrets") || strings.Contains(md, "style.md") {
		t.Errorf("CLAUDE.md after remove:\n%q", md)
	}
}

// Acceptance 6 (refresh): drift skip without --force, restore with it.
func TestUserRefreshDriftSkipAndForce(t *testing.T) {
	home, _ := installUserKit(t)
	skill := filepath.Join(home, ".claude/skills/review/SKILL.md")
	rule := filepath.Join(home, ".inat/rules/style.md")
	if err := os.WriteFile(skill, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rule); err != nil {
		t.Fatal(err)
	}
	if err := refresh.Refresh(home, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if read(t, skill) != "edited" || !exists(rule) {
		t.Error("refresh must keep the edit and restore the deleted rule")
	}
	if !strings.Contains(read(t, filepath.Join(home, ".claude/CLAUDE.md")), "- ~/.inat/rules/style.md") {
		t.Error("refresh lost the ~/ reference")
	}
	if err := refresh.Refresh(home, true, home); err != nil {
		t.Fatalf("refresh --force: %v", err)
	}
	if read(t, skill) == "edited" {
		t.Error("refresh --force kept the edit")
	}
}

// Acceptance 6 (update, status): upstream change lands at user targets.
func TestUserUpdateAndStatus(t *testing.T) {
	home, ctx := installUserKit(t)
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"kits/test-kit/skills/review/SKILL.md": "# Review\nUpdated review.",
	})
	if err := update.Update(home, "", false, false, home); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(read(t, filepath.Join(home, ".claude/skills/review/SKILL.md")), "Updated review.") {
		t.Error("update not delivered to ~/.claude/skills")
	}
	if !strings.Contains(read(t, filepath.Join(home, ".claude/CLAUDE.md")), "- ~/.inat/rules/style.md") {
		t.Error("update lost the ~/ reference")
	}
	if exists(filepath.Join(home, "CLAUDE.md")) {
		t.Error("project-scope CLAUDE.md written at home root")
	}
	if err := status.Status(home, home); err != nil {
		t.Fatalf("status: %v", err)
	}
}

// Acceptance 2 + spec 0025 §6.1: --user agents add delivers installed
// kits and inat-usage to the user targets; remove cleans them.
func TestUserAgentsAddRemove(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	home := testfixture.SetupUserScope(t, ctx.URL)
	if err := install.Install(home, "test-source/test-kit", home); err != nil {
		t.Fatalf("install with no agents: %v", err)
	}
	if err := agentscmd.AgentsAdd(home, "claude-code", home); err != nil {
		t.Fatalf("agents add: %v", err)
	}
	md := read(t, filepath.Join(home, ".claude/CLAUDE.md"))
	for _, want := range []string{"- ~/.inat/rules/inat-usage.md", "- ~/.inat/rules/style.md", "Never commit secrets."} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q:\n%s", want, md)
		}
	}
	if !exists(filepath.Join(home, ".inat/rules/inat-usage.md")) || !exists(filepath.Join(home, ".claude/skills/review/SKILL.md")) {
		t.Error("agents add did not deliver to user targets")
	}
	if exists(filepath.Join(home, ".inat/rules/inat-kits.md")) {
		t.Error("inat-kits must never be delivered at user scope")
	}
	if err := agentscmd.AgentsRemove(home, "claude-code", home); err != nil {
		t.Fatalf("agents remove: %v", err)
	}
	if exists(filepath.Join(home, ".inat/rules/inat-usage.md")) || exists(filepath.Join(home, ".claude/skills/review/SKILL.md")) {
		t.Error("agents remove left user-scope files")
	}
}
