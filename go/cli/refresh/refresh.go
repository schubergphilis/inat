// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package refresh implements `inat refresh [--force]`.
//
// Mirrors the Rust implementation's `src/cli/refresh.rs`. Re-delivers every installed kit
// from its recorded tree SHA in the global cache, without fetching new
// versions from the remote.
package refresh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/agents/builtin"
	"github.com/schubergphilis/inat/cli/install"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/hash"
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/core/reserved"
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/source"
)

// RunRefresh handles `inat refresh [--force]` against cwd.
func RunRefresh(force bool, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Refresh(dir, force, "")
}

// Refresh re-delivers every installed kit in dir, then refreshes built-in
// rules (inat-usage; inat-kits when cfg.KitsRepo) for each active agent.
// Built-in re-delivery follows spec 0011 §5: a recorded hash matching the
// embedded content's hash is a no-op; an on-disk hash matching the recorded
// hash but differing from embedded is a safe upgrade (criterion 7); a
// modified-file warning is emitted otherwise unless force overrides.
func Refresh(dir string, force bool, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	activeAgents, err := config.CollectActiveAgents(&cfg, homeOverride)
	if err != nil {
		return err
	}
	if len(cfg.Installed) == 0 && len(activeAgents) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to refresh")
		return nil
	}
	for installKey, installed := range cfg.Installed {
		if err := refreshSingle(dir, cfg.Scope, installKey, installed, force, homeOverride, activeAgents); err != nil {
			return err
		}
	}
	newBuiltins, err := refreshBuiltins(dir, &cfg, activeAgents, force)
	if err != nil {
		return err
	}
	cfg.BuiltinDelivered = newBuiltins
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	return nil
}

// refreshBuiltins iterates active agents and applies the spec 0011 §5
// state machine for each built-in rule. Built-in deliveries are gated on
// cfg.KitsRepo for inat-kits (per spec 0011 §7); when KitsRepo is false,
// inat-kits is skipped entirely and any stale record for it is dropped.
func refreshBuiltins(dir string, cfg *config.Config, activeAgents []string, force bool) (map[string][]config.DeliveredFile, error) {
	out := make(map[string][]config.DeliveredFile)
	for _, agentName := range activeAgents {
		var entries []config.DeliveredFile
		var skipped int

		recorded := lookupBuiltin(cfg.BuiltinDelivered[agentName], reserved.BuiltinRuleName)
		delivered, modified, err := builtin.RefreshInatUsage(dir, agentName, recorded, force, cfg.Scope)
		if err != nil {
			return nil, err
		}
		if delivered != nil {
			entries = append(entries, *delivered)
		}
		if modified != "" {
			fmt.Fprintf(os.Stderr,
				"warning: %s modified locally, skipping (use --force to override)\n", modified)
			skipped++
		}

		if cfg.KitsRepo {
			recordedKits := lookupBuiltin(cfg.BuiltinDelivered[agentName], reserved.BuiltinKitsRuleName)
			deliveredKits, modifiedKits, err := builtin.RefreshInatKits(dir, agentName, recordedKits, force)
			if err != nil {
				return nil, err
			}
			if deliveredKits != nil {
				entries = append(entries, *deliveredKits)
			}
			if modifiedKits != "" {
				fmt.Fprintf(os.Stderr,
					"warning: %s modified locally, skipping (use --force to override)\n", modifiedKits)
				skipped++
			}
		}

		if len(entries) > 0 {
			out[agentName] = entries
		}

		if skipped == 0 {
			fmt.Fprintf(os.Stderr, "refreshed built-in rules for %s\n", agentName)
		} else {
			fmt.Fprintf(os.Stderr,
				"refreshed built-in rules for %s (%d file(s) skipped, locally modified)\n", agentName, skipped)
		}
	}
	return out, nil
}

// lookupBuiltin finds the entry in entries whose Path ends with
// `/<ruleName>.md`. Returns nil if no entry matches. Suffix-match is
// robust to agent profiles whose contextual paths differ.
func lookupBuiltin(entries []config.DeliveredFile, ruleName string) *config.DeliveredFile {
	suffix := "/" + ruleName + ".md"
	for i := range entries {
		if strings.HasSuffix(entries[i].Path, suffix) {
			return &entries[i]
		}
	}
	return nil
}

func refreshSingle(
	dir string,
	sc scope.Scope,
	installKey string,
	installed config.InstalledKit,
	force bool,
	homeOverride string,
	activeAgents []string,
) error {
	idx := strings.Index(installKey, "/")
	if idx == -1 {
		return errs.NewKitValidation(fmt.Sprintf("invalid install key '%s'", installKey))
	}
	sourceName, kitName := installKey[:idx], installKey[idx+1:]

	repo, hashRef, err := install.OpenCachedTree(sourceName, installed.Tree, homeOverride)
	if err != nil {
		return err
	}
	tree, err := repo.TreeObject(*hashRef)
	if err != nil {
		return errs.NewKitValidation(fmt.Sprintf(
			"tree '%s' not found in cache for '%s', run update to refresh cache",
			installed.Tree, installKey))
	}
	allFiles, err := source.WalkKitFiles(tree)
	if err != nil {
		return err
	}
	kitCfg, warnings, err := readKit(allFiles)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	skipped, err := collectModified(dir, installed.Delivered, force)
	if err != nil {
		return err
	}
	for _, agentName := range activeAgents {
		profile, err := agents.ProfileFor(agentName, sc)
		if err != nil {
			return err
		}
		if _, err := install.DeliverToAgent(dir, &profile.Mapping, &kitCfg, allFiles, sourceName, kitName, skipped); err != nil {
			return err
		}
	}
	if len(skipped) == 0 {
		fmt.Fprintf(os.Stderr, "refreshed %s\n", installKey)
	} else {
		fmt.Fprintf(os.Stderr, "refreshed %s (%d file(s) skipped, locally modified)\n", installKey, len(skipped))
	}
	return nil
}

// collectModified returns the delivered paths whose on-disk content no
// longer matches the recorded hash, warning for each. Refresh passes the
// set to DeliverToAgent so those files are kept, not overwritten. Empty
// under force.
func collectModified(dir string, delivered []config.DeliveredFile, force bool) (map[string]bool, error) {
	modified := make(map[string]bool)
	if force {
		return modified, nil
	}
	for _, file := range delivered {
		path := filepath.Join(dir, file.Path)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		mod, err := hash.IsModified(path, file.Hash)
		if err != nil {
			return nil, err
		}
		if mod {
			fmt.Fprintf(os.Stderr,
				"warning: %s modified locally, skipping (use --force to override)\n",
				file.Path)
			modified[file.Path] = true
		}
	}
	return modified, nil
}

func readKit(allFiles []source.FileBlob) (kit.KitConfig, []string, error) {
	conv := make([]kit.FileBlob, len(allFiles))
	for i, f := range allFiles {
		conv[i] = kit.FileBlob{Path: f.Path, Content: f.Content}
	}
	return kit.ReadAndValidateKit(conv)
}
