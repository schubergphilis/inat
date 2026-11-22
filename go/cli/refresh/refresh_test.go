// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package refresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentscmd "github.com/schubergphilis/inat/cli/agents"
	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/hash"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func TestRefreshRestoresDeleted(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(project, ".claude/skills/review/SKILL.md")
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("skill not restored: %v", err)
	}
	if !strings.Contains(string(got), "code review") {
		t.Fatalf("content mismatch: %s", got)
	}
}

func TestRefreshForceOverwrites(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	style := filepath.Join(project, ".inat/rules/style.md")
	if err := os.WriteFile(style, []byte("User customization."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(project, true, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, _ := os.ReadFile(style)
	if !strings.Contains(string(got), "style guidelines") {
		t.Fatalf("content not restored: %s", got)
	}
}

func TestRefreshRestoresManagedSection(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	if err := agentscmd.AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatalf("agents add: %v", err)
	}
	claudeMd := filepath.Join(project, "CLAUDE.md")
	if err := os.WriteFile(claudeMd, []byte("# Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, _ := os.ReadFile(claudeMd)
	s := string(got)
	if !strings.Contains(s, "Never commit secrets.") || !strings.Contains(s, ".inat/rules/style.md") {
		t.Fatalf("kit section not restored: %s", s)
	}
	if !strings.Contains(s, "- .inat/rules/inat-usage.md") {
		t.Fatalf("built-in inat-usage bullet not restored: %s", s)
	}
}

func TestRefreshNoKits(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh empty: %v", err)
	}
}

func TestRefreshNoKitsButAgentsRecordsBuiltins(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := agentscmd.AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatalf("agents add: %v", err)
	}
	// Drop the BuiltinDelivered map to simulate a v0.4.0 → next upgrade
	// where the field doesn't exist yet but the on-disk content matches
	// the embedded content (silent backfill case).
	cfg, _ := config.LoadConfig(project)
	cfg.BuiltinDelivered = map[string][]config.DeliveredFile{}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	loaded, _ := config.LoadConfig(project)
	entries := loaded.BuiltinDelivered["claude-code"]
	if len(entries) != 1 {
		t.Fatalf("BuiltinDelivered[claude-code] = %d entries, want 1 after silent backfill", len(entries))
	}
}

func TestRefreshNothingToDo(t *testing.T) {
	dir := t.TempDir()
	if err := config.EnsureConfig(dir); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := Refresh(dir, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// Successful no-op — no panic, no crash. The "nothing to refresh"
	// message goes to stderr; we don't capture it here, but the test
	// asserts the function returns without error.
}

func TestRefreshKitsRepoFalseIgnoresKitsRule(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := agentscmd.AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatalf("agents add: %v", err)
	}
	// Inject a stale recorded entry for inat-kits even though KitsRepo is false.
	// Refresh must skip it (Q5 strict gate: false → no touch).
	cfg, _ := config.LoadConfig(project)
	cfg.BuiltinDelivered["claude-code"] = append(
		cfg.BuiltinDelivered["claude-code"],
		config.DeliveredFile{Path: ".inat/rules/inat-kits.md", Hash: "stale-hash"},
	)
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	loaded, _ := config.LoadConfig(project)
	for _, e := range loaded.BuiltinDelivered["claude-code"] {
		if strings.HasSuffix(e.Path, "/inat-kits.md") {
			t.Errorf("inat-kits entry should be dropped when KitsRepo=false; got %+v", e)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".inat/rules/inat-kits.md")); !os.IsNotExist(err) {
		t.Errorf("inat-kits.md should not exist when KitsRepo=false")
	}
}

func TestRefreshBuiltinUpgradeFromV03(t *testing.T) {
	// Simulate a v0.3.x → next upgrade: BuiltinDelivered missing AND
	// on-disk content differs from the (now-frontmattered) embedded content.
	// First refresh without --force must emit a modified-file warning and
	// leave the file alone. Refresh with --force must re-deliver and
	// populate BuiltinDelivered.
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := agentscmd.AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatalf("agents add: %v", err)
	}
	// Strip BuiltinDelivered (simulate older version's config).
	cfg, _ := config.LoadConfig(project)
	cfg.BuiltinDelivered = map[string][]config.DeliveredFile{}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}
	// Overwrite on-disk inat-usage.md with v0.3-style content (no frontmatter).
	rulePath := filepath.Join(project, ".inat/rules/inat-usage.md")
	staleContent := []byte("v0.3 inat-usage with no frontmatter")
	if err := os.WriteFile(rulePath, staleContent, 0o644); err != nil {
		t.Fatal(err)
	}
	// Refresh without --force: warning, no change, no record.
	if err := Refresh(project, false, home); err != nil {
		t.Fatalf("refresh (no force): %v", err)
	}
	body, _ := os.ReadFile(rulePath)
	if string(body) != string(staleContent) {
		t.Fatalf("file overwritten without --force: %q", body)
	}
	loaded, _ := config.LoadConfig(project)
	if len(loaded.BuiltinDelivered["claude-code"]) != 0 {
		t.Fatalf("BuiltinDelivered[claude-code] should be empty after warn; got %v",
			loaded.BuiltinDelivered["claude-code"])
	}
	// Now refresh --force: file replaced, hash recorded.
	if err := Refresh(project, true, home); err != nil {
		t.Fatalf("refresh --force: %v", err)
	}
	post, _ := os.ReadFile(rulePath)
	expectedHash := hash.HashBytes(post)
	loaded2, _ := config.LoadConfig(project)
	entries := loaded2.BuiltinDelivered["claude-code"]
	if len(entries) != 1 {
		t.Fatalf("BuiltinDelivered[claude-code] = %d entries after --force, want 1", len(entries))
	}
	if entries[0].Hash != expectedHash {
		t.Errorf("recorded hash = %s, want hash of on-disk content %s", entries[0].Hash, expectedHash)
	}
}

// TestRefreshCRLFCheckoutIsNotModified: issue #2. A CRLF-only change is
// not a local edit, so refresh must not count the file as modified.
func TestRefreshCRLFCheckoutIsNotModified(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	testfixture.ConvertToCRLF(t, filepath.Join(project, ".claude/skills/review/SKILL.md"))

	cfg, _ := config.LoadConfig(project)
	modified, err := collectModified(project, cfg.Installed["test-source/test-kit"].Delivered, false)
	if err != nil {
		t.Fatalf("collectModified: %v", err)
	}
	if len(modified) != 0 {
		t.Fatalf("CRLF checkout reported as modified: %v", modified)
	}
}
