// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package agentscmd

import (
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/scope"
)

func TestFormatAgentsListProject(t *testing.T) {
	cfg := config.Config{Agents: []string{"claude-code"}}
	out := formatAgentsList(&cfg, []string{})
	for _, want := range []string{"project agents:\n  claude-code\n", "global agents:\n  (none)\n", "available profiles:\n  claude-code\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// Spec 0025 §6.1: at user scope the shared list is the scope's own list,
// so it is printed once, not as project + global duplicates.
func TestFormatAgentsListUser(t *testing.T) {
	cfg := config.Config{Agents: []string{"claude-code"}, Scope: scope.User}
	out := formatAgentsList(&cfg, cfg.Agents)
	if !strings.Contains(out, "user agents:\n  claude-code\n") {
		t.Errorf("missing user block:\n%s", out)
	}
	if strings.Contains(out, "project agents:") || strings.Contains(out, "global agents:") {
		t.Errorf("user scope must print one list:\n%s", out)
	}
}
