// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package agentscmd

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

func TestAddAgentAutoDeliversInstalled(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()

	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("CLAUDE.md should not exist before agents add")
	}
	if err := AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatalf("AgentsAdd: %v", err)
	}
	skill := filepath.Join(project, ".claude/skills/review/SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("skill missing: %v", err)
	}
	md, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if !strings.Contains(string(md), "Never commit secrets.") {
		t.Fatalf("always rule missing")
	}
	if _, err := os.Stat(filepath.Join(project, ".inat/rules/style.md")); err != nil {
		t.Fatalf("contextual rule missing")
	}
	cfg, _ := config.LoadConfig(project)
	if !contains(cfg.Agents, "claude-code") {
		t.Fatalf("agent not in config")
	}
	rec := cfg.Installed["test-source/test-kit"]
	if len(rec.Delivered) == 0 {
		t.Fatalf("delivered empty")
	}
}

func TestAddUnknownAgent(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	err := AgentsAdd(project, "unknown-agent", home)
	if !errors.Is(err, errs.ErrAgentNotFound) {
		t.Fatalf("want AgentNotFound, got %v", err)
	}
}

func TestAddAlreadyActive(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	err := AgentsAdd(project, "claude-code", home)
	if !errors.Is(err, errs.ErrAgentAlreadyActive) {
		t.Fatalf("want AgentAlreadyActive, got %v", err)
	}
}

func TestRemoveAgentAutoCleans(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); err != nil {
		t.Fatalf("pre: skill missing")
	}
	if err := AgentsRemove(project, "claude-code", home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude/skills/review/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("skill should be gone")
	}
	if _, err := os.Stat(filepath.Join(project, ".inat/rules/style.md")); !os.IsNotExist(err) {
		t.Fatalf("style should be gone")
	}
	claudeMd := filepath.Join(project, "CLAUDE.md")
	if data, err := os.ReadFile(claudeMd); err == nil {
		if strings.Contains(string(data), "Never commit secrets.") || strings.Contains(string(data), ".inat/rules/style.md") {
			t.Fatalf("managed section not cleaned: %s", data)
		}
	}
	cfg, _ := config.LoadConfig(project)
	if contains(cfg.Agents, "claude-code") {
		t.Fatalf("agent still present")
	}
	rec := cfg.Installed["test-source/test-kit"]
	if len(rec.Delivered) != 0 {
		t.Fatalf("delivered not cleared: %+v", rec.Delivered)
	}
}

func TestRemoveAgentNotActive(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	err := AgentsRemove(project, "claude-code", home)
	if !errors.Is(err, errs.ErrAgentNotActive) {
		t.Fatalf("want AgentNotActive, got %v", err)
	}
}

func TestListSucceeds(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	globalPath := filepath.Join(home, ".inat", "config.json")
	if err := config.SaveGlobalConfigTo(globalPath, &config.GlobalConfig{Agents: []string{"claude-code"}}); err != nil {
		t.Fatal(err)
	}
	if err := AgentsList(project, home); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestAddAgentWithoutKitsRepo(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".inat/rules/inat-usage.md")); err != nil {
		t.Fatalf("inat-usage missing")
	}
	if _, err := os.Stat(filepath.Join(project, ".inat/rules/inat-kits.md")); !os.IsNotExist(err) {
		t.Fatalf("inat-kits should not be delivered without kits_repo")
	}
}

func TestAddAgentInKitsRepo(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	cfg, _ := config.LoadConfig(project)
	cfg.KitsRepo = true
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".inat/rules/inat-usage.md", ".inat/rules/inat-kits.md"} {
		if _, err := os.Stat(filepath.Join(project, p)); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}
	md, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if !strings.Contains(string(md), "- .inat/rules/inat-usage.md") || !strings.Contains(string(md), "- .inat/rules/inat-kits.md") {
		t.Fatalf("refs missing: %s", md)
	}
}

func TestAddAgentRecordsBuiltinDelivered(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.LoadConfig(project)
	entries, ok := cfg.BuiltinDelivered["claude-code"]
	if !ok {
		t.Fatalf("BuiltinDelivered[claude-code] missing, got %v", cfg.BuiltinDelivered)
	}
	if len(entries) != 1 {
		t.Fatalf("BuiltinDelivered[claude-code] = %d entries, want 1 (inat-usage only)", len(entries))
	}
	if entries[0].Path != ".inat/rules/inat-usage.md" || entries[0].Hash == "" {
		t.Errorf("entry = %+v, want non-empty hash for inat-usage.md", entries[0])
	}
}

func TestAddAgentInKitsRepoRecordsBothBuiltins(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	cfg, _ := config.LoadConfig(project)
	cfg.KitsRepo = true
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatal(err)
	}
	loaded, _ := config.LoadConfig(project)
	entries := loaded.BuiltinDelivered["claude-code"]
	if len(entries) != 2 {
		t.Fatalf("BuiltinDelivered[claude-code] = %d entries, want 2", len(entries))
	}
	paths := []string{entries[0].Path, entries[1].Path}
	wantPaths := []string{".inat/rules/inat-usage.md", ".inat/rules/inat-kits.md"}
	for i, p := range wantPaths {
		if paths[i] != p {
			t.Errorf("entry[%d].Path = %s, want %s", i, paths[i], p)
		}
	}
	for _, e := range entries {
		if e.Hash == "" {
			t.Errorf("entry %+v has empty hash", e)
		}
	}
}

func TestRemoveAgentClearsBuiltinDelivered(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := AgentsAdd(project, "claude-code", home); err != nil {
		t.Fatalf("add: %v", err)
	}
	pre, _ := config.LoadConfig(project)
	if _, ok := pre.BuiltinDelivered["claude-code"]; !ok {
		t.Fatalf("pre-state: BuiltinDelivered[claude-code] should exist after AgentsAdd")
	}
	if err := AgentsRemove(project, "claude-code", home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cfg, _ := config.LoadConfig(project)
	if _, ok := cfg.BuiltinDelivered["claude-code"]; ok {
		t.Errorf("BuiltinDelivered[claude-code] should be cleared, got %v", cfg.BuiltinDelivered)
	}
}

func TestAddGlobalWritesHome(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProjectNoAgents(t, ctx.URL)
	home := t.TempDir()
	if err := AgentsAddGlobal("claude-code", home); err != nil {
		t.Fatalf("add global: %v", err)
	}
	gcfg, _ := config.LoadGlobalConfigFrom(filepath.Join(home, ".inat", "config.json"))
	if len(gcfg.Agents) != 1 || gcfg.Agents[0] != "claude-code" {
		t.Fatalf("global config: %+v", gcfg)
	}
	cfg, _ := config.LoadConfig(project)
	if len(cfg.Agents) != 0 {
		t.Fatalf("project should be untouched")
	}
	if _, err := os.Stat(filepath.Join(project, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("CLAUDE.md should not exist")
	}
}

func TestAddGlobalAlreadyActive(t *testing.T) {
	home := t.TempDir()
	gcfgPath := filepath.Join(home, ".inat", "config.json")
	if err := config.SaveGlobalConfigTo(gcfgPath, &config.GlobalConfig{Agents: []string{"claude-code"}}); err != nil {
		t.Fatal(err)
	}
	err := AgentsAddGlobal("claude-code", home)
	if !errors.Is(err, errs.ErrAgentAlreadyActive) {
		t.Fatalf("want AgentAlreadyActive, got %v", err)
	}
}

func TestAddGlobalUnknownAgent(t *testing.T) {
	home := t.TempDir()
	err := AgentsAddGlobal("unknown-agent", home)
	if !errors.Is(err, errs.ErrAgentNotFound) {
		t.Fatalf("want AgentNotFound, got %v", err)
	}
}

func TestRemoveGlobalWritesHome(t *testing.T) {
	home := t.TempDir()
	gcfgPath := filepath.Join(home, ".inat", "config.json")
	if err := config.SaveGlobalConfigTo(gcfgPath, &config.GlobalConfig{Agents: []string{"claude-code"}}); err != nil {
		t.Fatal(err)
	}
	if err := AgentsRemoveGlobal("claude-code", home); err != nil {
		t.Fatal(err)
	}
	gcfg, _ := config.LoadGlobalConfigFrom(gcfgPath)
	if len(gcfg.Agents) != 0 {
		t.Fatalf("agents not cleared: %+v", gcfg)
	}
}

func TestRemoveGlobalNotActive(t *testing.T) {
	home := t.TempDir()
	err := AgentsRemoveGlobal("claude-code", home)
	if !errors.Is(err, errs.ErrAgentNotActive) {
		t.Fatalf("want AgentNotActive, got %v", err)
	}
}
