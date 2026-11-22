// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package status implements `inat status`. Mirrors the Rust implementation's `src/cli/status.rs`.
package status

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/schubergphilis/inat/cli/install"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/git/source"
)

// RunStatus handles `inat status` against cwd.
func RunStatus(user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Status(dir, "")
}

// Status fetches each source and reports per-kit update availability.
func Status(dir, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	if len(cfg.Installed) == 0 {
		fmt.Fprintln(os.Stderr, "no kits installed")
		return nil
	}
	activeAgents, err := config.CollectActiveAgents(&cfg, homeOverride)
	if err != nil {
		return err
	}
	agentList := "(none)"
	if len(activeAgents) > 0 {
		agentList = strings.Join(activeAgents, ", ")
	}
	fmt.Fprintf(os.Stderr, "agents: %s\n", agentList)
	fmt.Fprintln(os.Stderr, "\nkits:")
	keys := make([]string, 0, len(cfg.Installed))
	for k := range cfg.Installed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(os.Stderr, "  %s: %s\n", key, CheckKitStatus(&cfg, key, cfg.Installed[key], homeOverride))
	}
	return nil
}

// CheckKitStatus probes a single kit's update availability and returns a
// human-readable status string. Errors are converted to descriptive
// statuses so the caller can keep iterating.
func CheckKitStatus(cfg *config.Config, installKey string, installed config.InstalledKit, homeOverride string) string {
	idx := strings.Index(installKey, "/")
	if idx == -1 {
		return "invalid install key"
	}
	sourceName, kitName := installKey[:idx], installKey[idx+1:]
	src, ok := findSource(cfg, sourceName)
	if !ok {
		return "source not configured"
	}
	cachePath, err := install.ResolveCachePath(sourceName, homeOverride)
	if err != nil {
		return "cache error"
	}
	repo, err := source.CloneOrFetch(src.URL, cachePath)
	if err != nil {
		return "source unreachable"
	}
	subpath := ""
	if src.Path != nil {
		subpath = *src.Path
	}
	reg, err := source.FetchRegistryAt(repo, subpath)
	if err != nil {
		return "failed to read registry"
	}
	if _, ok := reg.Kits[kitName]; !ok {
		return "kit removed from source"
	}
	hash, _, err := source.FetchKitTreeAt(repo, subpath, kitName)
	if err != nil {
		return "kit tree not found"
	}
	if hash.String() == installed.Tree {
		return "up to date"
	}
	return "update available"
}

func findSource(cfg *config.Config, name string) (config.Source, bool) {
	for _, s := range cfg.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return config.Source{}, false
}
