// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package rename implements `inat rename <src/old> <src/new>`.
//
// Rename is a pure `cfg.Installed` map-key rewrite for the in-source
// `git mv` case: the kit's tree content is byte-identical at the new
// path, so on-disk delivered files stay correct and only the install
// key needs to follow the upstream move. Cross-source moves and
// content-drift cases are rejected here and routed to `inat replace`.
//
// See spec 0016 §4.3 for the contract.
package rename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/cli/install"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/delivery"
	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/source"
)

// NewRenameCmd returns the `inat rename` cobra command. Registered in
// main.go's root command tree.
func NewRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <src/old> <src/new>",
		Short: "Rename an installed kit's identity to follow an in-source git mv.",
		Long: "Rewrite the install key from <src/old> to <src/new> when upstream did a pure\n" +
			"git mv that left the kit's tree byte-identical at the new path. Both refs\n" +
			"must use source/kit form and share the same source. Tree-SHA equality is\n" +
			"enforced; for content drift use 'inat replace' instead.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunRename(args[0], args[1], scopecmd.IsUser(cmd))
		},
	}
}

// RunRename handles `inat rename <src/old> <src/new>` against cwd.
func RunRename(oldRef, newRef string, user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Rename(dir, oldRef, newRef, "")
}

// Rename runs the rename pipeline against an explicit project dir.
//
// homeOverride lets tests redirect the cache lookup; pass "" for
// production.
func Rename(dir, oldRef, newRef, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	oldKey, newKey, err := validatePreconditions(&cfg, oldRef, newRef, homeOverride)
	if err != nil {
		return err
	}
	cfg.Installed[newKey] = cfg.Installed[oldKey]
	delete(cfg.Installed, oldKey)
	if err := RewriteAlwaysHeaders(dir, &cfg, oldKey, newKey, homeOverride); err != nil {
		return err
	}
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "renamed %s → %s\n", oldKey, newKey)
	return nil
}

// RewriteAlwaysHeaders walks every active agent's always-rule target
// file and rewrites managed-section block headers so they reflect the
// new install key. Per spec 0019: in the explicit `inat rename` flow,
// it lands between the in-memory cfg.Installed mutation and
// config.SaveConfig — failure aborts the rename before any disk-side
// config is written, leaving on-disk state pre-rename.
//
// Exported per spec 0018 §4.5 so the transaction package's apply phase
// can run the same rewrite for the auto-rename `ActionRename` variant.
func RewriteAlwaysHeaders(dir string, cfg *config.Config, oldKey, newKey, homeOverride string) error {
	activeAgents, err := config.CollectActiveAgents(cfg, homeOverride)
	if err != nil {
		return err
	}
	for _, agentName := range activeAgents {
		profile, err := agents.ProfileFor(agentName, cfg.Scope)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, profile.Mapping.Rule.Always.Path)
		if err := delivery.RenameKitBlocks(target, oldKey, newKey); err != nil {
			return err
		}
	}
	return nil
}

// validatePreconditions runs the spec 0016 §4.3 checks (steps 2-8) and
// returns the resolved old/new install keys. Order matches the spec so
// pre-fetch errors (cross-source, not-installed, already-installed) fire
// before any I/O against the cache or registry.
func validatePreconditions(cfg *config.Config, oldRef, newRef, homeOverride string) (string, string, error) {
	oldSrc, oldKit, newSrc, newKit, err := parseRefs(oldRef, newRef)
	if err != nil {
		return "", "", err
	}
	if err := validateSameSource(oldSrc, newSrc); err != nil {
		return "", "", err
	}
	oldKey, newKey := oldSrc+"/"+oldKit, newSrc+"/"+newKit
	if err := validateInstalledState(cfg, oldKey, newKey); err != nil {
		return "", "", err
	}
	sc, err := loadSource(cfg, oldSrc, homeOverride)
	if err != nil {
		return "", "", err
	}
	if err := ensureKitInRegistry(sc, oldSrc, newKit); err != nil {
		return "", "", err
	}
	if err := ensureTreeMatches(sc, oldKey, newKey, newKit, cfg.Installed[oldKey].Tree); err != nil {
		return "", "", err
	}
	return oldKey, newKey, nil
}

// validateInstalledState rejects the rename if oldKey is not installed
// (nothing to rename) or newKey is already installed (the user has both
// kits and must resolve the duplicate manually first).
func validateInstalledState(cfg *config.Config, oldKey, newKey string) error {
	if _, ok := cfg.Installed[oldKey]; !ok {
		return errs.NewKitNotInstalled(oldKey)
	}
	if _, ok := cfg.Installed[newKey]; ok {
		return errs.NewKitAlreadyInstalled(newKey)
	}
	return nil
}

// sourceCtx bundles the cached repo handle and the registry subpath so
// the registry/tree-SHA helpers can share a single fetch.
type sourceCtx struct {
	repo    *git.Repository
	subpath string
}

func loadSource(cfg *config.Config, sourceName, homeOverride string) (*sourceCtx, error) {
	src, ok := findSource(cfg, sourceName)
	if !ok {
		return nil, errs.NewSourceNotFound(sourceName)
	}
	cachePath, err := install.ResolveCachePath(sourceName, homeOverride)
	if err != nil {
		return nil, err
	}
	repo, err := source.CloneOrFetch(src.URL, cachePath)
	if err != nil {
		return nil, err
	}
	subpath := ""
	if src.Path != nil {
		subpath = *src.Path
	}
	return &sourceCtx{repo: repo, subpath: subpath}, nil
}

func ensureKitInRegistry(sc *sourceCtx, sourceName, kitName string) error {
	reg, err := source.FetchRegistryAt(sc.repo, sc.subpath)
	if err != nil {
		return err
	}
	if err := registry.ValidateRegistry(&reg); err != nil {
		return err
	}
	if _, ok := reg.Kits[kitName]; !ok {
		return errs.NewKitNotFound(kitName, sourceName)
	}
	return nil
}

// ensureTreeMatches enforces strict tree-SHA equality between the
// source's current new-kit tree and the recorded old install. A mismatch
// means upstream edited content in the same commit as the rename — the
// on-disk delivered files are now wrong for the new tree, so the user
// must run `inat replace` or `inat update` first.
func ensureTreeMatches(sc *sourceCtx, oldKey, newKey, newKit, recordedTree string) error {
	newHash, _, err := source.FetchKitTreeAt(sc.repo, sc.subpath, newKit)
	if err != nil {
		return err
	}
	if newHash.String() != recordedTree {
		return errs.NewRenameTreeMismatch(oldKey, newKey, recordedTree, newHash.String())
	}
	return nil
}

func findSource(cfg *config.Config, name string) (config.Source, bool) {
	for _, s := range cfg.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return config.Source{}, false
}

// parseRefs splits both refs on "/". Bare names (no slash) are rejected
// because rename is intentionally precise: a user invoking rename has
// already audited which source/kit pair they want, and silently
// resolving a bare name would invite the wrong move on multi-source
// projects.
func parseRefs(oldRef, newRef string) (string, string, string, string, error) {
	oldSrc, oldKit, ok := splitRef(oldRef)
	if !ok {
		return "", "", "", "", errs.NewKitValidation(
			fmt.Sprintf("rename requires source/kit form, got '%s'", oldRef))
	}
	newSrc, newKit, ok := splitRef(newRef)
	if !ok {
		return "", "", "", "", errs.NewKitValidation(
			fmt.Sprintf("rename requires source/kit form, got '%s'", newRef))
	}
	return oldSrc, oldKit, newSrc, newKit, nil
}

func splitRef(ref string) (string, string, bool) {
	i := strings.Index(ref, "/")
	if i < 0 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}

// validateSameSource enforces the single-source contract. Cross-source
// moves are routed to `inat replace`, which re-fetches and re-delivers.
func validateSameSource(oldSrc, newSrc string) error {
	if oldSrc != newSrc {
		return errs.NewRenameCrossSource(oldSrc, newSrc)
	}
	return nil
}
