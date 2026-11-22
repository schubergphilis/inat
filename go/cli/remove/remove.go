// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package removecmd implements `inat remove <kit> [--force]` plus the
// shared cleanup helpers used by update and agents.
//
// Mirrors the Rust implementation's `src/cli/remove.rs`.
package removecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schubergphilis/inat/agents"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/delivery"
	"github.com/schubergphilis/inat/core/hash"
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
)

// RunRemove handles `inat remove <kit_ref> [--force]` against cwd.
func RunRemove(kitRef string, force bool, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Remove(dir, kitRef, force, "")
}

// Remove runs the full removal pipeline against an explicit project dir.
func Remove(dir, kitRef string, force bool, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	installKey, skipped, err := RemoveSteps(dir, kitRef, force, homeOverride, &cfg)
	if err != nil {
		return err
	}
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	if len(skipped) == 0 {
		fmt.Fprintf(os.Stderr, "removed %s\n", installKey)
	} else {
		fmt.Fprintf(os.Stderr, "removed %s (%d file(s) skipped, locally modified)\n", installKey, len(skipped))
	}
	return nil
}

// RemoveSteps runs the remove pipeline against an in-memory cfg. It does
// NOT load or save config — the caller owns those. Returns the resolved
// install key, the list of skipped (locally-modified, --force not set)
// file paths, and any error. Used by `Remove` (saves after) and by
// `cli/replace` so a single config save covers both halves of the
// operation.
func RemoveSteps(
	dir, kitRef string, force bool, homeOverride string, cfg *config.Config,
) (string, []string, error) {
	installKey, err := ResolveInstalledRef(kitRef, cfg)
	if err != nil {
		return "", nil, err
	}
	installed, ok := cfg.Installed[installKey]
	if !ok {
		return "", nil, errs.NewKitNotInstalled(installKey)
	}
	skipped, err := RemoveDeliveredFiles(dir, installed.Delivered, force)
	if err != nil {
		return "", nil, err
	}
	activeAgents, err := config.CollectActiveAgents(cfg, homeOverride)
	if err != nil {
		return "", nil, err
	}
	refPaths := make([]string, len(installed.Delivered))
	for i, d := range installed.Delivered {
		refPaths[i] = d.Path
	}
	if err := CleanManagedSections(dir, cfg.Scope, activeAgents, installKey, refPaths); err != nil {
		return "", nil, err
	}
	delete(cfg.Installed, installKey)
	return installKey, skipped, nil
}

// ResolveInstalledRef maps a `source/kit` or bare `kit` reference to an
// installed-kit key. Returns *errs.Error on miss or ambiguity.
func ResolveInstalledRef(reference string, cfg *config.Config) (string, error) {
	if strings.Contains(reference, "/") {
		if _, ok := cfg.Installed[reference]; ok {
			return reference, nil
		}
		return "", errs.NewKitNotInstalled(reference)
	}
	var matches []string
	for k := range cfg.Installed {
		i := strings.Index(k, "/")
		if i >= 0 && k[i+1:] == reference {
			matches = append(matches, k)
		}
	}
	switch len(matches) {
	case 0:
		return "", errs.NewKitNotInstalled(reference)
	case 1:
		return matches[0], nil
	default:
		return "", errs.NewKitValidation(fmt.Sprintf(
			"ambiguous kit name '%s': installed from multiple sources, use source/kit syntax",
			reference))
	}
}

// RemoveDeliveredFiles deletes each delivered file from disk, skipping
// any that have been locally modified (unless force=true). Returns the
// list of skipped paths so the caller can adjust its summary message.
func RemoveDeliveredFiles(dir string, delivered []config.DeliveredFile, force bool) ([]string, error) {
	var skipped []string
	for _, file := range delivered {
		path := filepath.Join(dir, file.Path)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, errs.WrapIO(err)
		}
		if !force {
			modified, err := hash.IsModified(path, file.Hash)
			if err != nil {
				return nil, err
			}
			if modified {
				fmt.Fprintf(os.Stderr,
					"warning: %s modified locally, skipping (use --force to override)\n",
					file.Path)
				skipped = append(skipped, file.Path)
				continue
			}
		}
		if err := os.Remove(path); err != nil {
			return nil, errs.WrapIO(err)
		}
		cleanupEmptyParents(path, dir)
	}
	return skipped, nil
}

// CleanManagedSections strips a kit's blocks and references from every
// active agent's managed-section file. refPaths are recorded delivered
// paths; each agent's mapping turns them into its reference text.
// Idempotent.
func CleanManagedSections(dir string, sc scope.Scope, activeAgents []string, installKey string, refPaths []string) error {
	for _, agentName := range activeAgents {
		profile, err := agents.ProfileFor(agentName, sc)
		if err != nil {
			return err
		}
		refTexts := make([]string, len(refPaths))
		for i, p := range refPaths {
			refTexts[i] = profile.Mapping.Rule.Contextual.RefTextForFile(p)
		}
		alwaysTarget := filepath.Join(dir, profile.Mapping.Rule.Always.Path)
		if err := delivery.RemoveKitContent(alwaysTarget, installKey, refTexts); err != nil {
			return err
		}
		refTarget := filepath.Join(dir, profile.Mapping.Rule.Contextual.Reference)
		if refTarget != alwaysTarget {
			if err := delivery.RemoveKitContent(refTarget, installKey, refTexts); err != nil {
				return err
			}
		}
	}
	return nil
}

func cleanupEmptyParents(filePath, projectRoot string) {
	dir := filepath.Dir(filePath)
	root, _ := filepath.Abs(projectRoot)
	for {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		if !strings.HasPrefix(absDir, root) || absDir == root {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
