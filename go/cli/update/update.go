// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package update implements `inat update [<kit>] [--force]`.
//
// Reimplemented in spec 0017 on top of the cli/transaction primitive:
// LoadConfig → buildTargets → transaction.Plan → transaction.Apply →
// SaveConfig. Plan-phase failures (network, parse, name-uniqueness)
// no longer leave a kit half-applied; apply-phase failures are
// isolated to the affected kit per spec 0017 §4.5.
package update

import (
	"fmt"
	"os"
	"slices"

	removecmd "github.com/schubergphilis/inat/cli/remove"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/cli/transaction"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

// RunUpdate handles `inat update [<kit>] [--force] [--plan]`. kitRef
// may be "" to update every installed kit. When planOnly is true the
// pipeline stops after Plan and prints per-action detail lines to
// stdout (spec 0020).
func RunUpdate(kitRef string, force, planOnly bool, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Update(dir, kitRef, force, planOnly, "")
}

// Update runs the update pipeline against an explicit project dir.
//
// Single-kit (kitRef != "") preserves the spec-0016 fail-fast contract:
// the underlying typed error is returned byte-identically. Multi-kit
// (kitRef == "") returns KitsNeedAttention(N) when at least one kit
// failed plan or apply, after running every other kit to completion.
//
// planOnly (spec 0020) short-circuits after Plan: per-action detail
// lines go to stdout, no Apply or SaveConfig runs, and the function
// returns nil regardless of needs-attention entries in the plan.
func Update(dir, kitRef string, force, planOnly bool, homeOverride string) error {
	if planOnly && force {
		return errs.NewKitValidation("--plan and --force are mutually exclusive")
	}
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	targets, err := buildTargets(kitRef, &cfg)
	if err != nil {
		return err
	}

	plan, _ := transaction.Plan(dir, &cfg, targets, homeOverride)
	printPlanSummary(plan)
	if planOnly {
		transaction.PrintPlanDetails(os.Stdout, plan)
		return nil
	}
	results := transaction.Apply(dir, &cfg, plan, force, homeOverride)
	printResultErrors(results)

	if anyApplied(results) {
		if err := config.SaveConfig(dir, &cfg); err != nil {
			return err
		}
	}
	printFinalSummary(results)

	if kitRef != "" {
		return firstNeedsAttentionError(results)
	}
	if n := countNeedsAttention(results); n > 0 {
		return errs.NewKitsNeedAttention(n)
	}
	return nil
}

// buildTargets converts the user's kitRef into a transaction.Target
// slice. Empty kitRef → one TargetUpdate per installed kit, in sorted
// install-key order so plans are reproducible (spec 0026). Non-empty
// → one TargetUpdate after resolving via ResolveInstalledRef (so bare
// "kit" names resolve to "src/kit").
func buildTargets(kitRef string, cfg *config.Config) ([]transaction.Target, error) {
	if kitRef != "" {
		key, err := removecmd.ResolveInstalledRef(kitRef, cfg)
		if err != nil {
			return nil, err
		}
		return []transaction.Target{{Kind: transaction.TargetUpdate, InstallKey: key}}, nil
	}
	keys := make([]string, 0, len(cfg.Installed))
	for k := range cfg.Installed {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	targets := make([]transaction.Target, 0, len(keys))
	for _, k := range keys {
		targets = append(targets, transaction.Target{Kind: transaction.TargetUpdate, InstallKey: k})
	}
	return targets, nil
}

func printPlanSummary(plan []transaction.PlannedAction) {
	if len(plan) == 0 {
		return
	}
	var update, rename, upToDate, needs int
	for _, a := range plan {
		switch a.Kind {
		case transaction.ActionUpdate, transaction.ActionReplace:
			update++
		case transaction.ActionRename:
			rename++
		case transaction.ActionUpToDate:
			upToDate++
		case transaction.ActionNeedsAttention:
			needs++
		}
	}
	fmt.Fprintf(os.Stderr, "planned: %d update, %d rename, %d up-to-date, %d need attention\n",
		update, rename, upToDate, needs)
}

func printResultErrors(results []transaction.ApplyResult) {
	for _, r := range results {
		if r.Kind == transaction.ActionNeedsAttention && r.Err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", r.InstallKey, r.Err)
		}
	}
}

func printFinalSummary(results []transaction.ApplyResult) {
	if len(results) == 0 {
		return
	}
	var updated, renamed, upToDate, needs int
	for _, r := range results {
		switch r.Kind {
		case transaction.ActionUpdate, transaction.ActionReplace:
			updated++
		case transaction.ActionRename:
			renamed++
		case transaction.ActionUpToDate:
			upToDate++
		case transaction.ActionNeedsAttention:
			needs++
		}
	}
	fmt.Fprintf(os.Stderr, "%d updated, %d renamed, %d up-to-date, %d need attention\n",
		updated, renamed, upToDate, needs)
	if needs > 0 {
		fmt.Fprintln(os.Stderr, "run `inat sync` for guidance")
	}
}

func anyApplied(results []transaction.ApplyResult) bool {
	for _, r := range results {
		if r.Kind == transaction.ActionUpdate || r.Kind == transaction.ActionReplace ||
			r.Kind == transaction.ActionRename {
			return true
		}
	}
	return false
}

func countNeedsAttention(results []transaction.ApplyResult) int {
	n := 0
	for _, r := range results {
		if r.Kind == transaction.ActionNeedsAttention {
			n++
		}
	}
	return n
}

// firstNeedsAttentionError returns the typed error of the first
// NeedsAttention result, or nil if every result is clean. Used by
// single-kit Update to honour the fail-fast contract.
func firstNeedsAttentionError(results []transaction.ApplyResult) error {
	for _, r := range results {
		if r.Kind == transaction.ActionNeedsAttention {
			return r.Err
		}
	}
	return nil
}
