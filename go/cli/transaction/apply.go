// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import (
	"fmt"
	"os"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/cli/install"
	removecmd "github.com/schubergphilis/inat/cli/remove"
	"github.com/schubergphilis/inat/cli/rename"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/core/snapshot"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/source"
)

// Apply runs the apply phase of the transaction primitive against an
// in-memory cfg. For every actionable plan entry it performs the
// existing remove + deliver sequence and mutates cfg.Installed in
// memory. Per-kit failures are captured into the corresponding
// ApplyResult and DO NOT stop the loop — Apply runs to completion
// across the whole batch.
//
// Apply does NOT save the config; the caller (cli/update or
// cli/replace) owns the single SaveConfig call after the loop. See
// spec 0017 §4.5.
func Apply(dir string, cfg *config.Config, plan []PlannedAction, force bool, homeOverride string) []ApplyResult {
	results := make([]ApplyResult, 0, len(plan))
	for _, a := range plan {
		results = append(results, applyOne(dir, cfg, a, force, homeOverride))
	}
	return results
}

func applyOne(dir string, cfg *config.Config, a PlannedAction, force bool, homeOverride string) ApplyResult {
	switch a.Kind {
	case ActionUpToDate:
		return ApplyResult{InstallKey: a.InstallKey, Kind: ActionUpToDate}
	case ActionNeedsAttention:
		return ApplyResult{InstallKey: a.InstallKey, Kind: ActionNeedsAttention, Err: a.Err}
	case ActionUpdate:
		return applyUpdate(dir, cfg, a, force, homeOverride)
	case ActionReplace:
		return applyReplace(dir, cfg, a, force, homeOverride)
	case ActionRename:
		return applyRename(dir, cfg, a, homeOverride)
	default:
		return ApplyResult{
			InstallKey: a.InstallKey,
			Kind:       ActionNeedsAttention,
			Err:        errs.NewKitValidation(fmt.Sprintf("apply: unknown action kind %v", a.Kind)),
		}
	}
}

func applyUpdate(dir string, cfg *config.Config, a PlannedAction, force bool, homeOverride string) ApplyResult {
	installed := cfg.Installed[a.InstallKey]
	paths, err := collectSnapshotPaths(cfg, installed.Delivered, homeOverride)
	if err != nil {
		return ApplyResult{InstallKey: a.InstallKey, Kind: ActionNeedsAttention, Err: err}
	}
	req := snapshot.Request{ProjectDir: dir, InstallKey: a.InstallKey, RelPaths: paths, HomeOverride: homeOverride}
	return runWithSnapshot(req, func() (ApplyResult, error) {
		return updateCore(dir, cfg, a, force, homeOverride)
	})
}

func updateCore(dir string, cfg *config.Config, a PlannedAction, force bool, homeOverride string) (ApplyResult, error) {
	sourceName, kitName, _ := splitInstallKey(a.InstallKey)
	installed := cfg.Installed[a.InstallKey]
	delivered, err := removeAndDeliver(
		dir, cfg, a.InstallKey, sourceName, kitName,
		installed.Delivered, a.NewKitCfg, a.NewFiles, force, homeOverride,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	cfg.Installed[a.InstallKey] = config.InstalledKit{
		Tree:      a.NewTreeSHA,
		Names:     a.NewNames,
		Delivered: delivered,
	}
	fmt.Fprintf(os.Stderr, "updated %s\n", a.InstallKey)
	return ApplyResult{InstallKey: a.InstallKey, Kind: ActionUpdate}, nil
}

func applyReplace(dir string, cfg *config.Config, a PlannedAction, force bool, homeOverride string) ApplyResult {
	oldKey := a.InstallKey
	installed := cfg.Installed[oldKey]
	paths, err := collectSnapshotPaths(cfg, installed.Delivered, homeOverride)
	if err != nil {
		return ApplyResult{InstallKey: oldKey, Kind: ActionNeedsAttention, Err: err}
	}
	req := snapshot.Request{ProjectDir: dir, InstallKey: oldKey, RelPaths: paths, HomeOverride: homeOverride}
	return runWithSnapshot(req, func() (ApplyResult, error) {
		return replaceCore(dir, cfg, a, force, homeOverride)
	})
}

func replaceCore(dir string, cfg *config.Config, a PlannedAction, force bool, homeOverride string) (ApplyResult, error) {
	oldKey := a.InstallKey
	newKey := a.NewKey
	newSrcName, newKitName, _ := splitInstallKey(newKey)
	installed := cfg.Installed[oldKey]

	skipped, err := removecmd.RemoveDeliveredFiles(dir, installed.Delivered, force)
	if err != nil {
		return ApplyResult{}, err
	}
	if len(skipped) > 0 && !force {
		return ApplyResult{}, driftAbort("replace", oldKey, len(skipped))
	}

	activeAgents, err := config.CollectActiveAgents(cfg, homeOverride)
	if err != nil {
		return ApplyResult{}, err
	}
	refPaths := make([]string, len(installed.Delivered))
	for i, d := range installed.Delivered {
		refPaths[i] = d.Path
	}
	if err := removecmd.CleanManagedSections(dir, cfg.Scope, activeAgents, oldKey, refPaths); err != nil {
		return ApplyResult{}, err
	}

	delivered, err := deliverAcrossAgents(dir, cfg.Scope, activeAgents, a.NewKitCfg, a.NewFiles, newSrcName, newKitName)
	if err != nil {
		return ApplyResult{}, errs.NewReplaceInstallFailed(oldKey, newKey, err)
	}

	cfg.Installed[newKey] = config.InstalledKit{
		Tree:      a.NewTreeSHA,
		Names:     a.NewNames,
		Delivered: delivered,
	}
	delete(cfg.Installed, oldKey)
	fmt.Fprintf(os.Stderr, "replaced %s → %s\n", oldKey, newKey)
	return ApplyResult{InstallKey: oldKey, Kind: ActionReplace}, nil
}

// driftAbort handles the update- and replace-without-force drift case
// (spec 0016 §6 criterion 11): some old-kit files were locally
// modified, so RemoveDeliveredFiles skipped them. The verb aborts the
// install half; the surrounding runWithSnapshot rolls back any
// non-skipped files that were already removed. Returns the typed error
// both verbs' contracts promise.
func driftAbort(verb, oldKey string, skippedCount int) error {
	fmt.Fprintf(os.Stderr,
		"%s aborted: %s has %d locally-modified file(s); re-run with --force to discard them\n",
		verb, oldKey, skippedCount)
	return errs.NewKitsNeedAttention(1)
}

// applyRename handles ActionRename — the pure-`git mv` auto-detected
// rename case from spec 0018. The kit's tree SHA is unchanged (that's
// what the detection in planUpdate verified), so on-disk delivered
// files are byte-identical at both keys and need no re-deliver. The
// only writes are the always-rule managed-section header rewrites
// across active agents, via the same helper the explicit `inat rename`
// verb uses (spec 0019).
//
// Drops the `force` parameter: drift override has no meaning for a
// header rewrite (line-local; nothing to discard). See spec 0018 §4.4.
//
// Failure during the header rewrite rolls back the in-memory
// cfg.Installed mutation. The caller's gated SaveConfig keeps the disk
// config file unchanged. Multi-agent partial-disk state is acknowledged
// in spec 0018 §3 and §4.4 — recovery via `inat refresh`.
func applyRename(dir string, cfg *config.Config, a PlannedAction, homeOverride string) ApplyResult {
	oldKey := a.InstallKey
	newKey := a.NewKey
	prev := cfg.Installed[oldKey]
	cfg.Installed[newKey] = config.InstalledKit{
		Tree:      prev.Tree,
		Names:     append([]string(nil), prev.Names...),
		Delivered: append([]config.DeliveredFile(nil), prev.Delivered...),
	}
	delete(cfg.Installed, oldKey)
	if err := rename.RewriteAlwaysHeaders(dir, cfg, oldKey, newKey, homeOverride); err != nil {
		delete(cfg.Installed, newKey)
		cfg.Installed[oldKey] = prev
		return ApplyResult{InstallKey: oldKey, Kind: ActionNeedsAttention, Err: err}
	}
	fmt.Fprintf(os.Stderr, "renamed %s → %s\n", oldKey, newKey)
	return ApplyResult{InstallKey: oldKey, Kind: ActionRename}
}

// removeAndDeliver runs the apply-phase disk work for Update: remove
// the old delivered files, clean the kit's managed-section blocks, then
// deliver the new content via the active agents. Drift without force
// aborts before delivery — delivering would overwrite the very files
// RemoveDeliveredFiles just reported as skipped (issue #1). Replace
// runs the same sequence inline in replaceCore. The newSrc / newKit
// pair is what gets embedded in the always-rule managed-section header.
func removeAndDeliver(
	dir string, cfg *config.Config,
	oldKey, newSrcName, newKitName string,
	oldDelivered []config.DeliveredFile,
	newKitCfg *kit.KitConfig,
	newFiles []source.FileBlob,
	force bool, homeOverride string,
) ([]config.DeliveredFile, error) {
	skipped, err := removecmd.RemoveDeliveredFiles(dir, oldDelivered, force)
	if err != nil {
		return nil, err
	}
	if len(skipped) > 0 && !force {
		return nil, driftAbort("update", oldKey, len(skipped))
	}
	activeAgents, err := config.CollectActiveAgents(cfg, homeOverride)
	if err != nil {
		return nil, err
	}
	refPaths := make([]string, len(oldDelivered))
	for i, d := range oldDelivered {
		refPaths[i] = d.Path
	}
	if err := removecmd.CleanManagedSections(dir, cfg.Scope, activeAgents, oldKey, refPaths); err != nil {
		return nil, err
	}
	return deliverAcrossAgents(dir, cfg.Scope, activeAgents, newKitCfg, newFiles, newSrcName, newKitName)
}

// deliverAcrossAgents fans the kit's content out to every active
// agent and returns the merged delivered-files slice. Mirrors
// install.deliverToAllAgents but is duplicated here intentionally —
// keeping that helper unexported in `cli/install` preserves its scope
// boundary.
func deliverAcrossAgents(
	dir string, sc scope.Scope, activeAgents []string, kitCfg *kit.KitConfig,
	allFiles []source.FileBlob, sourceName, kitName string,
) ([]config.DeliveredFile, error) {
	var delivered []config.DeliveredFile
	for _, agentName := range activeAgents {
		profile, err := agents.ProfileFor(agentName, sc)
		if err != nil {
			return nil, err
		}
		files, err := install.DeliverToAgent(dir, &profile.Mapping, kitCfg, allFiles, sourceName, kitName, nil)
		if err != nil {
			return nil, err
		}
		delivered = append(delivered, files...)
	}
	return delivered, nil
}
