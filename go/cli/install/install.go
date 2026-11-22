// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package install implements `inat install <source/kit>`. It also exposes
// the shared delivery helpers used by update, refresh, and agents.
//
// Mirrors the Rust implementation's `src/cli/install.rs`.
package install

import (
	"fmt"
	"os"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/schubergphilis/inat/agents"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/cache"
	"github.com/schubergphilis/inat/git/source"
)

// RunInstall handles `inat install <kit_ref>` against the current working
// directory.
func RunInstall(kitRef string, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Install(dir, kitRef, "")
}

// Install runs the install pipeline against an explicit project dir.
//
// homeOverride lets tests redirect the cache + global-config lookups; pass
// "" for production.
func Install(dir, kitRef, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	installKey, err := InstallSteps(dir, kitRef, homeOverride, &cfg)
	if err != nil {
		return err
	}
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "installed %s\n", installKey)
	return nil
}

// InstallSteps runs the install pipeline against an in-memory cfg. It
// does NOT load or save config — the caller owns those. Returns the
// install key on success so the caller can compose its own success
// message. Used by `Install` (saves after) and by `cli/replace` so a
// single config save covers both halves of the operation.
func InstallSteps(dir, kitRef, homeOverride string, cfg *config.Config) (string, error) {
	sourceName, kitName, err := parseKitRef(kitRef, cfg)
	if err != nil {
		return "", err
	}
	installKey := sourceName + "/" + kitName
	if _, ok := cfg.Installed[installKey]; ok {
		return "", errs.NewKitAlreadyInstalled(installKey)
	}
	src, ok := findSource(cfg, sourceName)
	if !ok {
		return "", errs.NewSourceNotFound(sourceName)
	}
	treeHash, kitCfg, allFiles, err := loadKitFromSource(src, sourceName, kitName, homeOverride)
	if err != nil {
		return "", err
	}
	names := kit.DeriveNames(&kitCfg)
	if err := config.CheckNameUniqueness(cfg, names, installKey); err != nil {
		return "", err
	}
	delivered, err := deliverToAllAgents(dir, cfg, &kitCfg, allFiles, sourceName, kitName, homeOverride)
	if err != nil {
		return "", err
	}
	cfg.Installed[installKey] = config.InstalledKit{
		Tree:      treeHash.String(),
		Names:     names,
		Delivered: delivered,
	}
	return installKey, nil
}

// loadKitFromSource clones / fetches the source repo, validates the
// registry, fetches the kit tree, walks its files, and parses the kit.
// Returns the tree SHA, the parsed kit, and the file blobs for delivery.
// Soft-warning strings from kit parsing are printed to stderr.
func loadKitFromSource(
	src config.Source, sourceName, kitName, homeOverride string,
) (plumbing.Hash, kit.KitConfig, []source.FileBlob, error) {
	cachePath, err := ResolveCachePath(sourceName, homeOverride)
	if err != nil {
		return plumbing.Hash{}, kit.KitConfig{}, nil, err
	}
	repo, err := source.CloneOrFetch(src.URL, cachePath)
	if err != nil {
		return plumbing.Hash{}, kit.KitConfig{}, nil, err
	}
	subpath := ""
	if src.Path != nil {
		subpath = *src.Path
	}
	reg, err := source.FetchRegistryAt(repo, subpath)
	if err != nil {
		return plumbing.Hash{}, kit.KitConfig{}, nil, err
	}
	if err := registry.ValidateRegistry(&reg); err != nil {
		return plumbing.Hash{}, kit.KitConfig{}, nil, err
	}
	if _, ok := reg.Kits[kitName]; !ok {
		return plumbing.Hash{}, kit.KitConfig{}, nil, errs.NewKitNotFound(kitName, sourceName)
	}
	treeHash, tree, err := source.FetchKitTreeAt(repo, subpath, kitName)
	if err != nil {
		return plumbing.Hash{}, kit.KitConfig{}, nil, err
	}
	allFiles, err := source.WalkKitFiles(tree)
	if err != nil {
		return plumbing.Hash{}, kit.KitConfig{}, nil, err
	}
	kitCfg, warnings, err := kitFromBlobs(allFiles)
	if err != nil {
		return plumbing.Hash{}, kit.KitConfig{}, nil, err
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	return treeHash, kitCfg, allFiles, nil
}

// deliverToAllAgents collects every active agent profile, runs
// DeliverToAgent against each, and returns the merged delivered-files
// list. Used by Install and (in the future) by replace.
func deliverToAllAgents(
	dir string, cfg *config.Config, kitCfg *kit.KitConfig,
	allFiles []source.FileBlob, sourceName, kitName, homeOverride string,
) ([]config.DeliveredFile, error) {
	activeAgents, err := config.CollectActiveAgents(cfg, homeOverride)
	if err != nil {
		return nil, err
	}
	var delivered []config.DeliveredFile
	for _, agentName := range activeAgents {
		profile, err := agents.ProfileFor(agentName, cfg.Scope)
		if err != nil {
			return nil, err
		}
		files, err := DeliverToAgent(dir, &profile.Mapping, kitCfg, allFiles, sourceName, kitName, nil)
		if err != nil {
			return nil, err
		}
		delivered = append(delivered, files...)
	}
	return delivered, nil
}

// ResolveCachePath returns the cache path for a given source. When
// homeOverride is non-empty it's used in place of the real home — this
// is the test entry point.
func ResolveCachePath(sourceName, homeOverride string) (string, error) {
	if homeOverride != "" {
		return cache.PathForSourceUnder(homeOverride, sourceName), nil
	}
	return cache.PathForSource(sourceName)
}

// OpenCachedTree opens the cached repo for a source and returns the
// stored tree object for treeHash. Used by update/refresh/agents to
// re-read kit content without re-fetching the network.
func OpenCachedTree(sourceName, treeHash, homeOverride string) (*git.Repository, *plumbing.Hash, error) {
	cachePath, err := ResolveCachePath(sourceName, homeOverride)
	if err != nil {
		return nil, nil, err
	}
	repo, err := git.PlainOpen(cachePath)
	if err != nil {
		return nil, nil, errs.NewKitValidation(fmt.Sprintf(
			"cache not found for source '%s', run install or update first", sourceName))
	}
	hash := plumbing.NewHash(treeHash)
	if hash.IsZero() {
		return nil, nil, errs.NewKitValidation(fmt.Sprintf("invalid tree SHA '%s'", treeHash))
	}
	return repo, &hash, nil
}

func parseKitRef(reference string, cfg *config.Config) (string, string, error) {
	if i := strings.Index(reference, "/"); i >= 0 {
		return reference[:i], reference[i+1:], nil
	}
	switch len(cfg.Sources) {
	case 1:
		return cfg.Sources[0].Name, reference, nil
	case 0:
		return "", "", errs.NewSourceNotFound(reference)
	default:
		return "", "", errs.NewKitValidation(fmt.Sprintf(
			"ambiguous kit name '%s': multiple sources configured, use source/kit syntax",
			reference))
	}
}

func findSource(cfg *config.Config, name string) (config.Source, bool) {
	for _, s := range cfg.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return config.Source{}, false
}

// kitFromBlobs converts source.FileBlob slices to kit.FileBlob and
// invokes ReadAndValidateKit. Returns the parsed kit, any soft warnings
// (e.g. rules without frontmatter — the caller is expected to print
// these to stderr), and any hard validation error.
func kitFromBlobs(in []source.FileBlob) (kit.KitConfig, []string, error) {
	conv := make([]kit.FileBlob, len(in))
	for i, b := range in {
		conv[i] = kit.FileBlob{Path: b.Path, Content: b.Content}
	}
	return kit.ReadAndValidateKit(conv)
}
