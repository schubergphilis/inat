// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import (
	"fmt"
	"os"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/snapshot"
	"github.com/schubergphilis/inat/errs"
)

// collectSnapshotPaths returns the deduplicated project-relative paths
// that an applyUpdate or applyReplace could mutate: every delivered file
// of the installed kit, plus each active agent's always-rule and
// contextual-reference target files. Deduplication matters because for
// the canonical claude-code profile, the always-rule and
// contextual-reference targets are the same file (CLAUDE.md). See
// spec 0021 §4.2.
func collectSnapshotPaths(cfg *config.Config, delivered []config.DeliveredFile, homeOverride string) ([]string, error) {
	seen := map[string]struct{}{}
	var paths []string
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	for _, d := range delivered {
		add(d.Path)
	}
	activeAgents, err := config.CollectActiveAgents(cfg, homeOverride)
	if err != nil {
		return nil, err
	}
	for _, agentName := range activeAgents {
		profile, err := agents.ProfileFor(agentName, cfg.Scope)
		if err != nil {
			return nil, err
		}
		add(profile.Mapping.Rule.Always.Path)
		add(profile.Mapping.Rule.Contextual.Reference)
	}
	return paths, nil
}

// runWithSnapshot captures via the given request, runs the work
// closure, and either discards the snapshot on success or restores from
// it on failure. On a double-fault (apply fails AND restore fails) it
// returns ApplyRestoreFailed wrapping both errors. See spec 0021 §4.4.
func runWithSnapshot(req snapshot.Request, work func() (ApplyResult, error)) ApplyResult {
	snap, err := snapshot.Capture(req)
	if err != nil {
		return ApplyResult{InstallKey: req.InstallKey, Kind: ActionNeedsAttention, Err: err}
	}
	result, workErr := work()
	if workErr != nil {
		if rerr := snap.Restore(); rerr != nil {
			return ApplyResult{
				InstallKey: req.InstallKey,
				Kind:       ActionNeedsAttention,
				Err:        errs.NewApplyRestoreFailed(req.InstallKey, snap.Dir, workErr, rerr),
			}
		}
		return ApplyResult{InstallKey: req.InstallKey, Kind: ActionNeedsAttention, Err: workErr}
	}
	if err := snap.Discard(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: snapshot cleanup failed for %s: %v\n", req.InstallKey, err)
	}
	return result
}
