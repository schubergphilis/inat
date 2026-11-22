// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package agents holds the built-in agent profile registry.
//
// Mirrors the Rust implementation's `src/agents/mod.rs`. Profiles are static; new agents are
// added by appending to `profiles`.
package agents

import (
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
)

// Strategy is the delivery mechanism for a file type.
type Strategy int

const (
	StrategyCopy Strategy = iota + 1
	StrategyManagedSection
)

// SkillMapping describes how skills are delivered for an agent.
type SkillMapping struct {
	Path     string
	Strategy Strategy
}

// RuleAlwaysMapping describes how always-priority rules land for an agent.
type RuleAlwaysMapping struct {
	Path     string
	Strategy Strategy
}

// RuleContextualMapping describes how contextual-priority rules land,
// including the rule-list reference target. RefPrefix is prepended to
// the rule's file path to form the reference text written into the
// managed section: empty for project scope, "~/" for user scope so the
// reference resolves from any working directory (spec 0025 §5).
type RuleContextualMapping struct {
	Path      string
	Strategy  Strategy
	Reference string
	RefPrefix string
}

// RuleFile returns the rule's file path relative to the scope root, as
// recorded in config `delivered` entries.
func (m RuleContextualMapping) RuleFile(name string) string {
	return m.Path + name + ".md"
}

// RefText returns the reference text written into the managed section
// for the named rule.
func (m RuleContextualMapping) RefText(name string) string {
	return m.RefTextForFile(m.RuleFile(name))
}

// RefTextForFile maps a recorded rule file path to its reference text.
func (m RuleContextualMapping) RefTextForFile(path string) string {
	return m.RefPrefix + path
}

// RuleMapping pairs the always- and contextual-priority mappings.
type RuleMapping struct {
	Always     RuleAlwaysMapping
	Contextual RuleContextualMapping
}

// ProfileMapping describes the full delivery shape for an agent.
type ProfileMapping struct {
	Skill SkillMapping
	Rule  RuleMapping
}

// AgentProfile binds a name to its project-scope Mapping and its
// user-scope UserMapping (spec 0025 §5).
type AgentProfile struct {
	Name        string
	Mapping     ProfileMapping
	UserMapping ProfileMapping
}

var profiles = []AgentProfile{ClaudeCodeProfile}

// GetProfile returns the project-scope profile for the given agent
// name. Equivalent to ProfileFor(name, scope.Project).
//
// Returns *errs.Error with Kind=AgentNotFound when no profile matches.
func GetProfile(name string) (AgentProfile, error) {
	for _, p := range profiles {
		if p.Name == name {
			return p, nil
		}
	}
	return AgentProfile{}, errs.NewAgentNotFound(name)
}

// ProfileFor returns the profile for the given agent name with Mapping
// set to the mapping of scope sc.
//
// Returns *errs.Error with Kind=AgentNotFound when no profile matches.
func ProfileFor(name string, sc scope.Scope) (AgentProfile, error) {
	p, err := GetProfile(name)
	if err != nil {
		return AgentProfile{}, err
	}
	if sc == scope.User {
		p.Mapping = p.UserMapping
	}
	return p, nil
}

// ListProfiles returns every built-in agent profile.
func ListProfiles() []AgentProfile {
	out := make([]AgentProfile, len(profiles))
	copy(out, profiles)
	return out
}

// ClaudeCodeProfile is the canonical claude-code agent profile.
var ClaudeCodeProfile = AgentProfile{
	Name: "claude-code",
	Mapping: ProfileMapping{
		Skill: SkillMapping{
			Path:     ".claude/skills/",
			Strategy: StrategyCopy,
		},
		Rule: RuleMapping{
			Always: RuleAlwaysMapping{
				Path:     "CLAUDE.md",
				Strategy: StrategyManagedSection,
			},
			Contextual: RuleContextualMapping{
				Path:      ".inat/rules/",
				Strategy:  StrategyCopy,
				Reference: "CLAUDE.md",
			},
		},
	},
	UserMapping: ProfileMapping{
		Skill: SkillMapping{
			Path:     ".claude/skills/",
			Strategy: StrategyCopy,
		},
		Rule: RuleMapping{
			Always: RuleAlwaysMapping{
				Path:     ".claude/CLAUDE.md",
				Strategy: StrategyManagedSection,
			},
			// Not `.claude/rules/`: Claude Code auto-loads that folder,
			// which would defeat the contextual priority.
			Contextual: RuleContextualMapping{
				Path:      ".inat/rules/",
				Strategy:  StrategyCopy,
				Reference: ".claude/CLAUDE.md",
				RefPrefix: "~/",
			},
		},
	},
}
