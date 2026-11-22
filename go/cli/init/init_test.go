// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package initcmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

func TestInitFreshProject(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "", false); err != nil {
		t.Fatalf("init: %v", err)
	}
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Sources) != 0 || len(cfg.Installed) != 0 || len(cfg.Agents) != 0 || cfg.KitsRepo {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestInitWithAgent(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "claude-code", false); err != nil {
		t.Fatalf("init: %v", err)
	}
	cfg, _ := config.LoadConfig(dir)
	if len(cfg.Agents) != 1 || cfg.Agents[0] != "claude-code" {
		t.Fatalf("agents: %+v", cfg.Agents)
	}
	if cfg.KitsRepo {
		t.Fatalf("kits_repo should be false")
	}
}

func TestInitWithAgentDeliversBuiltinUsage(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "claude-code", false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".inat/rules/inat-usage.md"))
	if err != nil {
		t.Fatalf("rule missing: %v", err)
	}
	if !strings.Contains(string(body), "Using `inat` in this project") {
		t.Fatalf("rule body: %s", body)
	}
	md, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md missing: %v", err)
	}
	if !strings.Contains(string(md), "- .inat/rules/inat-usage.md") {
		t.Fatalf("ref missing")
	}
}

func TestInitWithoutAgentNoRules(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-usage.md")); !os.IsNotExist(err) {
		t.Fatalf("rule should not exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("CLAUDE.md should not exist")
	}
}

func TestInitUnknownAgent(t *testing.T) {
	dir := t.TempDir()
	err := Init(dir, "unknown-agent", false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrAgentNotFound) {
		t.Fatalf("want AgentNotFound, got %v", err)
	}
	if _, err := config.LoadConfig(dir); err == nil {
		t.Fatalf("config should not have been written")
	}
}

func TestInitAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "", false); err != nil {
		t.Fatal(err)
	}
	err := Init(dir, "", false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrConfigAlreadyExists) {
		t.Fatalf("want ConfigAlreadyExists, got %v", err)
	}
}

func TestInitWithKitsRepoFlag(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "", true); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.LoadConfig(dir)
	if !cfg.KitsRepo {
		t.Fatalf("kits_repo should be true")
	}
	if len(cfg.Agents) != 0 {
		t.Fatalf("agents should be empty")
	}
}

func TestInitKitsRepoAloneNoRules(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-usage.md")); !os.IsNotExist(err) {
		t.Fatalf("inat-usage should not exist")
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-kits.md")); !os.IsNotExist(err) {
		t.Fatalf("inat-kits should not exist")
	}
}

func TestInitKitsRepoAndAgentDeliversBoth(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "claude-code", true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".inat/rules/inat-usage.md", ".inat/rules/inat-kits.md"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(md), "- .inat/rules/inat-usage.md") || !strings.Contains(string(md), "- .inat/rules/inat-kits.md") {
		t.Fatalf("refs missing: %s", md)
	}
	cfg, _ := config.LoadConfig(dir)
	if !cfg.KitsRepo || len(cfg.Agents) != 1 {
		t.Fatalf("cfg: %+v", cfg)
	}
}

func TestInitWithAgentRecordsBuiltinDelivered(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "claude-code", false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.LoadConfig(dir)
	entries, ok := cfg.BuiltinDelivered["claude-code"]
	if !ok {
		t.Fatalf("BuiltinDelivered[claude-code] missing, got %v", cfg.BuiltinDelivered)
	}
	if len(entries) != 1 {
		t.Fatalf("BuiltinDelivered[claude-code] = %d entries, want 1", len(entries))
	}
	if entries[0].Path != ".inat/rules/inat-usage.md" || entries[0].Hash == "" {
		t.Errorf("entry = %+v, want non-empty hash for inat-usage.md", entries[0])
	}
}

func TestInitKitsRepoAndAgentRecordsBoth(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "claude-code", true); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.LoadConfig(dir)
	entries := cfg.BuiltinDelivered["claude-code"]
	if len(entries) != 2 {
		t.Fatalf("BuiltinDelivered[claude-code] = %d entries, want 2", len(entries))
	}
	wantPaths := []string{".inat/rules/inat-usage.md", ".inat/rules/inat-kits.md"}
	for i, p := range wantPaths {
		if entries[i].Path != p {
			t.Errorf("entry[%d].Path = %s, want %s", i, entries[i].Path, p)
		}
		if entries[i].Hash == "" {
			t.Errorf("entry[%d] hash empty", i)
		}
	}
}

func TestInitAgentWithoutKitsRepoSkipsKitsRule(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "claude-code", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-usage.md")); err != nil {
		t.Fatalf("usage missing")
	}
	if _, err := os.Stat(filepath.Join(dir, ".inat/rules/inat-kits.md")); !os.IsNotExist(err) {
		t.Fatalf("kits should not exist")
	}
}
