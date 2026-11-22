// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package agents

import (
	"errors"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

func TestGetKnownProfile(t *testing.T) {
	p, err := GetProfile("claude-code")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Name != "claude-code" {
		t.Fatalf("name mismatch")
	}
}

func TestGetUnknownProfile(t *testing.T) {
	_, err := GetProfile("unknown-agent")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrAgentNotFound) {
		t.Fatalf("want AgentNotFound, got %v", err)
	}
}

func TestListProfilesContainsClaude(t *testing.T) {
	found := false
	for _, p := range ListProfiles() {
		if p.Name == "claude-code" {
			found = true
		}
	}
	if !found {
		t.Fatalf("claude-code missing from ListProfiles")
	}
}

func TestClaudeProfileSkillMapping(t *testing.T) {
	p, _ := GetProfile("claude-code")
	if p.Mapping.Skill.Path != ".claude/skills/" || p.Mapping.Skill.Strategy != StrategyCopy {
		t.Fatalf("skill mapping mismatch: %+v", p.Mapping.Skill)
	}
}

func TestClaudeProfileRuleAlways(t *testing.T) {
	p, _ := GetProfile("claude-code")
	if p.Mapping.Rule.Always.Path != "CLAUDE.md" || p.Mapping.Rule.Always.Strategy != StrategyManagedSection {
		t.Fatalf("always mapping mismatch")
	}
}

func TestClaudeProfileRuleContextual(t *testing.T) {
	p, _ := GetProfile("claude-code")
	c := p.Mapping.Rule.Contextual
	if c.Path != ".inat/rules/" || c.Strategy != StrategyCopy || c.Reference != "CLAUDE.md" {
		t.Fatalf("contextual mapping mismatch: %+v", c)
	}
}
