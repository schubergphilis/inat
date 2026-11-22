// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package agentscmd

import (
	"testing"

	"github.com/schubergphilis/inat/core/config"
)

// Spec 0025 §6.5 / acceptance 1: `agents add/remove --global` edits the
// shared agents list without dropping user-scope sources and kits.
func TestGlobalAgentsPreserveUserScope(t *testing.T) {
	home := t.TempDir()
	seed := config.Config{
		Sources:   []config.Source{{Name: "s", URL: "https://example.invalid/s.git"}},
		Installed: map[string]config.InstalledKit{"s/k": {Tree: "abc", Names: []string{"n"}, Delivered: []config.DeliveredFile{}}},
		Agents:    []string{},
	}
	if err := config.SaveConfig(home, &seed); err != nil {
		t.Fatal(err)
	}
	if err := AgentsAddGlobal("claude-code", home); err != nil {
		t.Fatalf("add --global: %v", err)
	}
	assertUserScopeKept(t, home, 1)
	if err := AgentsRemoveGlobal("claude-code", home); err != nil {
		t.Fatalf("remove --global: %v", err)
	}
	assertUserScopeKept(t, home, 0)
}

func assertUserScopeKept(t *testing.T, home string, wantAgents int) {
	t.Helper()
	cfg, err := config.LoadUserConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 1 || len(cfg.Installed) != 1 {
		t.Errorf("user scope dropped: sources=%v installed=%v", cfg.Sources, cfg.Installed)
	}
	if len(cfg.Agents) != wantAgents {
		t.Errorf("agents = %v, want %d", cfg.Agents, wantAgents)
	}
}
