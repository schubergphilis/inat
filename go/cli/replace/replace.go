// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package replace implements `inat replace <src/old> <newSrc/new>
// [--force]`.
//
// Reimplemented in spec 0017 on top of the cli/transaction primitive:
// LoadConfig → parseAndValidate → transaction.Plan (one TargetReplace)
// → transaction.Apply → SaveConfig. The plan-then-apply structure
// removes the spec-0016 in-memory snapshot — Plan never mutates cfg,
// Apply mutates only on success, and SaveConfig is gated on a clean
// result, so the on-disk config retains the old key on every failure
// path automatically.
package replace

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/cli/transaction"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

// NewReplaceCmd returns the `inat replace` cobra command.
func NewReplaceCmd() *cobra.Command {
	var force, planOnly bool
	cmd := &cobra.Command{
		Use:   "replace <src/old> <newSrc/new>",
		Short: "Atomically remove an installed kit and install another in its place.",
		Long: "Remove the installed kit identified by <src/old> and install <newSrc/new>\n" +
			"as a single operation; the on-disk config keeps the old key on every\n" +
			"failure path so a partial replace can be recovered with `inat refresh`.\n" +
			"--force discards locally-modified files in the old kit.\n" +
			"--plan previews the planned action on stdout without applying anything.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunReplace(args[0], args[1], force, planOnly, scopecmd.IsUser(cmd))
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Discard locally-modified files when removing the old kit.")
	cmd.Flags().BoolVar(&planOnly, "plan", false, "Preview the planned action on stdout without applying anything (spec 0020).")
	return cmd
}

// RunReplace handles `inat replace <src/old> <newSrc/new> [--force]
// [--plan]` against cwd.
func RunReplace(oldRef, newRef string, force, planOnly bool, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Replace(dir, oldRef, newRef, force, planOnly, "")
}

// Replace runs the replace pipeline against an explicit project dir.
// homeOverride lets tests redirect cache + global-config lookups.
// planOnly (spec 0020) short-circuits after Plan: a single detail line
// is written to stdout, no Apply or SaveConfig runs, and the function
// returns nil even if the plan is needs-attention.
func Replace(dir, oldRef, newRef string, force, planOnly bool, homeOverride string) error {
	if planOnly && force {
		return errs.NewKitValidation("--plan and --force are mutually exclusive")
	}
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	oldKey, _, err := parseAndValidate(&cfg, oldRef, newRef)
	if err != nil {
		return err
	}

	plan, _ := transaction.Plan(dir, &cfg, []transaction.Target{{
		Kind:       transaction.TargetReplace,
		InstallKey: oldKey,
		NewRef:     newRef,
	}}, homeOverride)
	if planOnly {
		transaction.PrintPlanDetails(os.Stdout, plan)
		return nil
	}
	if plan[0].Kind == transaction.ActionNeedsAttention {
		return plan[0].Err
	}

	results := transaction.Apply(dir, &cfg, plan, force, homeOverride)
	if results[0].Kind == transaction.ActionNeedsAttention {
		return results[0].Err
	}

	return config.SaveConfig(dir, &cfg)
}

// parseAndValidate enforces the pre-flight checks the user expects to
// fail fast with a typed error: both refs in source/kit form, oldKey
// installed, newKey not installed. Plan re-validates these (it has to
// for batch use), but routing them through here keeps single-target
// replace's error class direct (KitNotInstalled / KitAlreadyInstalled
// / KitValidation) rather than detoured through ActionNeedsAttention.
func parseAndValidate(cfg *config.Config, oldRef, newRef string) (string, string, error) {
	oldSrc, oldKit, ok := splitRef(oldRef)
	if !ok {
		return "", "", errs.NewKitValidation(
			"replace requires source/kit form, got '" + oldRef + "'")
	}
	newSrc, newKit, ok := splitRef(newRef)
	if !ok {
		return "", "", errs.NewKitValidation(
			"replace requires source/kit form, got '" + newRef + "'")
	}
	oldKey := oldSrc + "/" + oldKit
	newKey := newSrc + "/" + newKit
	if _, ok := cfg.Installed[oldKey]; !ok {
		return "", "", errs.NewKitNotInstalled(oldKey)
	}
	if _, ok := cfg.Installed[newKey]; ok {
		return "", "", errs.NewKitAlreadyInstalled(newKey)
	}
	return oldKey, newKey, nil
}

func splitRef(ref string) (string, string, bool) {
	i := strings.Index(ref, "/")
	if i < 0 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}
