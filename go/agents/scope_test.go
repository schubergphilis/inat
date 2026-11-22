// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package agents

import (
	"testing"

	"github.com/schubergphilis/inat/core/scope"
)

// Spec 0025 §5: the claude-code mapping per scope. Paths are relative to
// the scope root (project dir, or home for the user scope).
func TestClaudeCodeMappingPerScope(t *testing.T) {
	cases := []struct {
		sc                                scope.Scope
		skill, always, ctxPath, reference string
		refText                           string
	}{
		{scope.Project, ".claude/skills/", "CLAUDE.md", ".inat/rules/", "CLAUDE.md", ".inat/rules/x.md"},
		{scope.User, ".claude/skills/", ".claude/CLAUDE.md", ".inat/rules/", ".claude/CLAUDE.md", "~/.inat/rules/x.md"},
	}
	for _, c := range cases {
		p, err := ProfileFor("claude-code", c.sc)
		if err != nil {
			t.Fatalf("%v: %v", c.sc, err)
		}
		m := p.Mapping
		if m.Skill.Path != c.skill || m.Rule.Always.Path != c.always ||
			m.Rule.Contextual.Path != c.ctxPath || m.Rule.Contextual.Reference != c.reference {
			t.Errorf("%v: mapping = %+v", c.sc, m)
		}
		if got := m.Rule.Contextual.RuleFile("x"); got != ".inat/rules/x.md" {
			t.Errorf("%v: RuleFile = %q", c.sc, got)
		}
		if got := m.Rule.Contextual.RefText("x"); got != c.refText {
			t.Errorf("%v: RefText = %q, want %q", c.sc, got, c.refText)
		}
	}
}

// Project scope keeps v0.7.2 behaviour: GetProfile is the project mapping
// and the reference text is exactly the recorded file path.
func TestGetProfileIsProjectScope(t *testing.T) {
	a, _ := GetProfile("claude-code")
	b, _ := ProfileFor("claude-code", scope.Project)
	if a.Mapping != b.Mapping {
		t.Errorf("GetProfile mapping %+v != project mapping %+v", a.Mapping, b.Mapping)
	}
	c := a.Mapping.Rule.Contextual
	if c.RefTextForFile(".inat/rules/y.md") != ".inat/rules/y.md" {
		t.Errorf("project RefTextForFile must be identity")
	}
}

func TestProfileForUnknownAgent(t *testing.T) {
	if _, err := ProfileFor("nope", scope.User); err == nil {
		t.Error("want AgentNotFound")
	}
}
