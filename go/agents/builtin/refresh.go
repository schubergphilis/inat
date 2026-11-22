// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package builtin

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/delivery"
	"github.com/schubergphilis/inat/core/frontmatter"
	"github.com/schubergphilis/inat/core/hash"
	"github.com/schubergphilis/inat/core/reserved"
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
)

// RefreshInatUsage applies the spec 0011 §5 state machine for the built-in
// inat-usage rule. It reconciles three pieces of state — the recorded hash
// from cfg.BuiltinDelivered, the on-disk file's hash, and the binary's
// embedded content hash — and either re-delivers the file, records a new
// hash silently, or surfaces a modified-file warning, depending on which
// branch applies.
//
// Returns:
//   - delivered: the DeliveredFile to write into cfg.BuiltinDelivered[agent]
//     for this rule. nil iff there is no record to persist (the migration
//     friction branch — recorded was missing AND on-disk differs from
//     embedded AND force was false).
//   - modifiedPath: relative path of a file the caller should surface in a
//     modified-file warning. Empty when no warning is warranted.
//
// The managed-section bullet for the rule is sync'd unconditionally at the
// end (Q6 invariant from the design grilling), using the *current* binary's
// embedded frontmatter description.
func RefreshInatUsage(
	projectDir, agentName string,
	recorded *config.DeliveredFile,
	force bool,
	sc scope.Scope,
) (*config.DeliveredFile, string, error) {
	return refreshBuiltin(projectDir, agentName, sc, reserved.BuiltinRuleName, inatUsageRule, recorded, force)
}

// RefreshInatKits is the inat-kits mirror of RefreshInatUsage. The caller
// is responsible for gating on cfg.KitsRepo (per spec 0011 §7); this
// function does not consult that field.
func RefreshInatKits(
	projectDir, agentName string,
	recorded *config.DeliveredFile,
	force bool,
) (*config.DeliveredFile, string, error) {
	return refreshBuiltin(projectDir, agentName, scope.Project, reserved.BuiltinKitsRuleName, inatKitsRule, recorded, force)
}

func refreshBuiltin(
	projectDir, agentName string,
	sc scope.Scope,
	ruleName, content string,
	recorded *config.DeliveredFile,
	force bool,
) (*config.DeliveredFile, string, error) {
	profile, err := agents.ProfileFor(agentName, sc)
	if err != nil {
		return nil, "", err
	}
	mapping := profile.Mapping
	rulePath := mapping.Rule.Contextual.RuleFile(ruleName)
	fullRulePath := filepath.Join(projectDir, rulePath)
	refTarget := filepath.Join(projectDir, mapping.Rule.Contextual.Reference)

	fm, _, _, fmErr := frontmatter.Parse(content)
	if fmErr != nil {
		return nil, "", fmErr
	}
	embeddedHash := hash.HashBytes([]byte(content))

	onDisk, statErr := os.ReadFile(fullRulePath)
	fileMissing := false
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			fileMissing = true
		} else {
			return nil, "", errs.WrapIO(statErr)
		}
	}

	delivered, modified, err := decideAndApply(
		projectDir, ruleName, content, mapping.Rule.Contextual.Path,
		rulePath, embeddedHash, onDisk, fileMissing, recorded, force,
	)
	if err != nil {
		return nil, "", err
	}

	if err := delivery.AddContextualReference(refTarget, mapping.Rule.Contextual.RefText(ruleName), fm.Description); err != nil {
		return nil, "", err
	}
	return delivered, modified, nil
}

// decideAndApply runs the state-machine decision and performs the file
// write when the chosen branch calls for one. It returns the entry to
// record (or nil), and the relative path to surface as modified (or "").
func decideAndApply(
	projectDir, ruleName, content, contextualPath string,
	rulePath, embeddedHash string,
	onDisk []byte,
	fileMissing bool,
	recorded *config.DeliveredFile,
	force bool,
) (*config.DeliveredFile, string, error) {
	deliverNow := func() (*config.DeliveredFile, error) {
		d, err := delivery.DeliverRuleContextual(projectDir, ruleName, []byte(content), contextualPath)
		if err != nil {
			return nil, err
		}
		return &d, nil
	}

	switch {
	case fileMissing:
		d, err := deliverNow()
		return d, "", err

	case recorded == nil && hash.IsMatch(onDisk, embeddedHash):
		return &config.DeliveredFile{Path: rulePath, Hash: embeddedHash}, "", nil

	case recorded == nil && force:
		d, err := deliverNow()
		return d, "", err

	case recorded == nil:
		return nil, rulePath, nil

	case recorded.Hash == embeddedHash:
		copyOf := *recorded
		return &copyOf, "", nil

	case hash.IsMatch(onDisk, recorded.Hash):
		d, err := deliverNow()
		return d, "", err

	case force:
		d, err := deliverNow()
		return d, "", err

	default:
		copyOf := *recorded
		return &copyOf, rulePath, nil
	}
}
