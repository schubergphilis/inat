// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package builtin

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/delivery"
	"github.com/schubergphilis/inat/core/frontmatter"
	"github.com/schubergphilis/inat/core/reserved"
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
)

//go:embed inat_usage.md
var inatUsageRule string

//go:embed inat_kits.md
var inatKitsRule string

// DeliverInatUsage writes the built-in inat-usage contextual rule for the
// named agent and returns the delivered file's path + hash so the caller
// can persist it in cfg.BuiltinDelivered. Idempotent.
func DeliverInatUsage(projectDir, agentName string, sc scope.Scope) (config.DeliveredFile, error) {
	return deliverBuiltin(projectDir, agentName, sc, reserved.BuiltinRuleName, inatUsageRule)
}

// RemoveInatUsage deletes the built-in inat-usage rule for the named
// agent and clears the corresponding reference. Safe to call when the
// rule was never delivered.
func RemoveInatUsage(projectDir, agentName string, sc scope.Scope) error {
	return removeBuiltin(projectDir, agentName, sc, reserved.BuiltinRuleName)
}

// DeliverInatKits writes the built-in inat-kits contextual rule for the
// named agent and returns the delivered file's path + hash so the caller
// can persist it in cfg.BuiltinDelivered. Called alongside
// DeliverInatUsage when kits_repo is true. Idempotent.
func DeliverInatKits(projectDir, agentName string) (config.DeliveredFile, error) {
	return deliverBuiltin(projectDir, agentName, scope.Project, reserved.BuiltinKitsRuleName, inatKitsRule)
}

// RemoveInatKits deletes the built-in inat-kits rule for the named
// agent. Safe to call when the rule was never delivered.
func RemoveInatKits(projectDir, agentName string) error {
	return removeBuiltin(projectDir, agentName, scope.Project, reserved.BuiltinKitsRuleName)
}

func deliverBuiltin(projectDir, agentName string, sc scope.Scope, ruleName, content string) (config.DeliveredFile, error) {
	profile, err := agents.ProfileFor(agentName, sc)
	if err != nil {
		return config.DeliveredFile{}, err
	}
	mapping := profile.Mapping
	delivered, err := delivery.DeliverRuleContextual(
		projectDir, ruleName, []byte(content), mapping.Rule.Contextual.Path,
	)
	if err != nil {
		return config.DeliveredFile{}, err
	}
	fm, _, _, fmErr := frontmatter.Parse(content)
	if fmErr != nil {
		return config.DeliveredFile{}, fmErr
	}
	refTarget := filepath.Join(projectDir, mapping.Rule.Contextual.Reference)
	if err := delivery.AddContextualReference(refTarget, mapping.Rule.Contextual.RefText(ruleName), fm.Description); err != nil {
		return config.DeliveredFile{}, err
	}
	return delivered, nil
}

func removeBuiltin(projectDir, agentName string, sc scope.Scope, ruleName string) error {
	profile, err := agents.ProfileFor(agentName, sc)
	if err != nil {
		return err
	}
	mapping := profile.Mapping
	ruleFile := filepath.Join(projectDir, mapping.Rule.Contextual.RuleFile(ruleName))
	if _, statErr := os.Stat(ruleFile); statErr == nil {
		if removeErr := os.Remove(ruleFile); removeErr != nil {
			return errs.WrapIO(removeErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return errs.WrapIO(statErr)
	}
	refTarget := filepath.Join(projectDir, mapping.Rule.Contextual.Reference)
	return delivery.RemoveContextualReference(refTarget, mapping.Rule.Contextual.RefText(ruleName))
}
