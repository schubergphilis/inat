// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import (
	"fmt"
	"os"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/source"
)

// Plan runs the plan phase of the transaction primitive against an
// in-memory cfg. It performs the network + parse work needed to
// describe each target's apply work without writing anything to disk
// or mutating cfg. See spec 0017 §4.4. The actions come back in apply
// order: input order, except that a kit giving up a name precedes the
// kit taking it (spec 0026).
//
// The returned error is reserved for callers that want to short-circuit
// catastrophic failures; in practice it is always nil — fatal failures
// are encoded as ActionNeedsAttention entries inside the slice.
//
// dir is currently unused at the plan phase (Plan never writes under
// dir) but is part of the signature so callers can pass it through to
// Apply unchanged.
func Plan(dir string, cfg *config.Config, targets []Target, homeOverride string) ([]PlannedAction, error) {
	_ = dir
	announced := make(map[string]bool)
	indexes := make(map[string]*sourceSHAIndex)
	actions := make([]PlannedAction, 0, len(targets))
	for _, t := range targets {
		actions = append(actions, planSingle(cfg, t, homeOverride, announced, indexes))
	}
	checkBatchUniqueness(cfg, actions)
	return orderDonorsFirst(cfg, actions), nil
}

// checkBatchUniqueness enforces the post-update name-uniqueness invariant
// across the entire plan. Two passes:
//
//   - Reserved-name pass: any actionable plan entry whose NewNames
//     contains an inat-reserved name (`inat-usage`, `inat-kits`) is
//     converted in place to ActionNeedsAttention(ReservedName).
//   - Collision pass: build the post-update name → owning install-key
//     map (recorded names for kits not in the batch; planned NewNames
//     for Update / Replace actions; old key removed for Replace). Any
//     actionable plan entry whose NewNames collides with another owner
//     is converted to ActionNeedsAttention(DuplicateName).
//
// Closes spec 0016 §1(c): a skill migrating from kit-a to kit-b in the
// same batch resolves cleanly because the post-update set has the name
// exactly once.
func checkBatchUniqueness(cfg *config.Config, plan []PlannedAction) {
	reserved := config.ReservedNames()
	for i := range plan {
		a := &plan[i]
		if !a.IsActionable() {
			continue
		}
		if r := firstReserved(a.NewNames, reserved); r != "" {
			convertToNeedsAttention(a, errs.NewReservedName(r))
		}
	}
	owners := buildPostUpdateNameOwners(cfg, plan)
	for i := range plan {
		a := &plan[i]
		if !a.IsActionable() {
			continue
		}
		ownKey := a.InstallKey
		if a.Kind == ActionReplace || a.Kind == ActionRename {
			ownKey = a.NewKey
		}
		for _, n := range a.NewNames {
			if other := otherOwner(owners[n], ownKey); other != "" {
				convertToNeedsAttention(a, errs.NewDuplicateName(n, other))
				break
			}
		}
	}
}

func firstReserved(names, reserved []string) string {
	for _, n := range names {
		for _, r := range reserved {
			if n == r {
				return n
			}
		}
	}
	return ""
}

func otherOwner(owners []string, self string) string {
	for _, o := range owners {
		if o != self {
			return o
		}
	}
	return ""
}

func buildPostUpdateNameOwners(cfg *config.Config, plan []PlannedAction) map[string][]string {
	keyToNames := make(map[string][]string, len(cfg.Installed))
	for k, ik := range cfg.Installed {
		keyToNames[k] = append([]string(nil), ik.Names...)
	}
	for _, a := range plan {
		switch a.Kind {
		case ActionUpdate:
			keyToNames[a.InstallKey] = a.NewNames
		case ActionReplace, ActionRename:
			delete(keyToNames, a.InstallKey)
			keyToNames[a.NewKey] = a.NewNames
		}
	}
	nameToKeys := make(map[string][]string)
	for k, names := range keyToNames {
		for _, n := range names {
			nameToKeys[n] = append(nameToKeys[n], k)
		}
	}
	return nameToKeys
}

// convertToNeedsAttention rewrites an actionable PlannedAction in place
// so it carries the typed error and no longer holds the planned data
// (which the apply phase would otherwise consume).
func convertToNeedsAttention(a *PlannedAction, err error) {
	a.Kind = ActionNeedsAttention
	a.Err = err
	a.NewKey = ""
	a.NewTreeSHA = ""
	a.NewKitCfg = nil
	a.NewFiles = nil
	a.NewNames = nil
}

func planSingle(
	cfg *config.Config, target Target, homeOverride string,
	announced map[string]bool, indexes map[string]*sourceSHAIndex,
) PlannedAction {
	switch target.Kind {
	case TargetUpdate:
		return planUpdate(cfg, target, homeOverride, announced, indexes)
	case TargetReplace:
		return planReplace(cfg, target, homeOverride, announced)
	default:
		return needsAttention(target.InstallKey,
			errs.NewKitValidation(fmt.Sprintf("unknown target kind: %v", target.Kind)))
	}
}

func planReplace(cfg *config.Config, target Target, homeOverride string, announced map[string]bool) PlannedAction {
	if _, ok := cfg.Installed[target.InstallKey]; !ok {
		return needsAttention(target.InstallKey, errs.NewKitNotInstalled(target.InstallKey))
	}
	newSrcName, newKitName, ok := splitInstallKey(target.NewRef)
	if !ok {
		return needsAttention(target.InstallKey,
			errs.NewKitValidation(fmt.Sprintf("invalid new ref '%s'", target.NewRef)))
	}
	newKey := newSrcName + "/" + newKitName
	if _, ok := cfg.Installed[newKey]; ok {
		return needsAttention(target.InstallKey, errs.NewKitAlreadyInstalled(newKey))
	}
	newSrc, ok := findSource(cfg, newSrcName)
	if !ok {
		return needsAttention(target.InstallKey, errs.NewSourceNotFound(newSrcName))
	}
	repo, reg, err := fetchSource(newSrc, newSrcName, homeOverride, announced)
	if err != nil {
		return needsAttention(target.InstallKey, err)
	}
	if _, ok := reg.Kits[newKitName]; !ok {
		return needsAttention(target.InstallKey, errs.NewKitNotFound(newKitName, newSrcName))
	}
	subpath := ""
	if newSrc.Path != nil {
		subpath = *newSrc.Path
	}
	treeHash, tree, err := source.FetchKitTreeAt(repo, subpath, newKitName)
	if err != nil {
		return needsAttention(target.InstallKey, err)
	}
	kitCfg, allFiles, names, err := walkAndParseKit(tree)
	if err != nil {
		return needsAttention(target.InstallKey, err)
	}
	return PlannedAction{
		Kind:       ActionReplace,
		InstallKey: target.InstallKey,
		NewKey:     newKey,
		NewTreeSHA: treeHash.String(),
		NewKitCfg:  kitCfg,
		NewFiles:   allFiles,
		NewNames:   names,
	}
}

func planUpdate(
	cfg *config.Config, target Target, homeOverride string,
	announced map[string]bool, indexes map[string]*sourceSHAIndex,
) PlannedAction {
	installed, ok := cfg.Installed[target.InstallKey]
	if !ok {
		return needsAttention(target.InstallKey, errs.NewKitNotInstalled(target.InstallKey))
	}
	sourceName, kitName, ok := splitInstallKey(target.InstallKey)
	if !ok {
		return needsAttention(target.InstallKey,
			errs.NewKitValidation(fmt.Sprintf("invalid install key '%s'", target.InstallKey)))
	}
	src, ok := findSource(cfg, sourceName)
	if !ok {
		return needsAttention(target.InstallKey, errs.NewSourceNotFound(sourceName))
	}
	repo, reg, err := fetchSource(src, sourceName, homeOverride, announced)
	if err != nil {
		return needsAttention(target.InstallKey, err)
	}
	subpath := ""
	if src.Path != nil {
		subpath = *src.Path
	}
	if _, ok := reg.Kits[kitName]; !ok {
		// Kit absent from the source's current registry. Auto-rename
		// detection per spec 0018 §4.2: search the source's tree-SHA
		// index for the recorded Installed.Tree. Index is constructed
		// lazily — first miss in this source triggers the registry
		// walk; subsequent misses reuse the cached index.
		idx, ok := indexes[sourceName]
		if !ok {
			idx = &sourceSHAIndex{repo: repo, reg: reg, subpath: subpath}
			indexes[sourceName] = idx
		}
		candidates := idx.candidatesForSHA(installed.Tree)
		switch len(candidates) {
		case 0:
			return needsAttention(target.InstallKey, errs.NewKitNotFound(kitName, sourceName))
		case 1:
			return PlannedAction{
				Kind:       ActionRename,
				InstallKey: target.InstallKey,
				NewKey:     sourceName + "/" + candidates[0],
				NewTreeSHA: installed.Tree,
				NewNames:   append([]string(nil), installed.Names...),
			}
		default:
			return needsAttention(target.InstallKey,
				errs.NewRenameAmbiguous(target.InstallKey, candidates))
		}
	}
	treeHash, tree, err := source.FetchKitTreeAt(repo, subpath, kitName)
	if err != nil {
		return needsAttention(target.InstallKey, err)
	}
	if treeHash.String() == installed.Tree {
		return PlannedAction{Kind: ActionUpToDate, InstallKey: target.InstallKey}
	}
	kitCfg, allFiles, names, err := walkAndParseKit(tree)
	if err != nil {
		return needsAttention(target.InstallKey, err)
	}
	return PlannedAction{
		Kind:       ActionUpdate,
		InstallKey: target.InstallKey,
		NewTreeSHA: treeHash.String(),
		NewKitCfg:  kitCfg,
		NewFiles:   allFiles,
		NewNames:   names,
	}
}

// walkAndParseKit walks the kit tree, parses kit.json, derives the
// public-name set, and returns the four pieces planUpdate / planReplace
// need to populate the action. Soft-warning strings emitted during
// kit.ReadAndValidateKit are printed to stderr — same behaviour as
// install.loadKitFromSource.
func walkAndParseKit(tree *object.Tree) (*kit.KitConfig, []source.FileBlob, []string, error) {
	allFiles, err := source.WalkKitFiles(tree)
	if err != nil {
		return nil, nil, nil, err
	}
	kitCfg, warnings, err := readKit(allFiles)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	return &kitCfg, allFiles, kit.DeriveNames(&kitCfg), nil
}

// fetchSource clones-or-fetches the source repo and parses + validates
// its registry. The first call per Plan invocation for a given source
// emits a `fetching <source>...` progress line; subsequent calls reuse
// the cache silently.
func fetchSource(
	src config.Source, sourceName, homeOverride string, announced map[string]bool,
) (*git.Repository, registry.Registry, error) {
	cachePath, err := install.ResolveCachePath(sourceName, homeOverride)
	if err != nil {
		return nil, registry.Registry{}, err
	}
	if !announced[sourceName] {
		fmt.Fprintf(os.Stderr, "fetching %s...\n", sourceName)
		announced[sourceName] = true
	}
	repo, err := source.CloneOrFetch(src.URL, cachePath)
	if err != nil {
		return nil, registry.Registry{}, err
	}
	subpath := ""
	if src.Path != nil {
		subpath = *src.Path
	}
	parsed, err := source.FetchRegistryAt(repo, subpath)
	if err != nil {
		return nil, registry.Registry{}, err
	}
	if err := registry.ValidateRegistry(&parsed); err != nil {
		return nil, registry.Registry{}, err
	}
	return repo, parsed, nil
}

func splitInstallKey(installKey string) (string, string, bool) {
	i := strings.Index(installKey, "/")
	if i < 0 {
		return "", "", false
	}
	return installKey[:i], installKey[i+1:], true
}

func findSource(cfg *config.Config, name string) (config.Source, bool) {
	for _, s := range cfg.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return config.Source{}, false
}

func needsAttention(installKey string, err error) PlannedAction {
	return PlannedAction{
		Kind:       ActionNeedsAttention,
		InstallKey: installKey,
		Err:        err,
	}
}

func readKit(allFiles []source.FileBlob) (kit.KitConfig, []string, error) {
	conv := make([]kit.FileBlob, len(allFiles))
	for i, f := range allFiles {
		conv[i] = kit.FileBlob{Path: f.Path, Content: f.Content}
	}
	return kit.ReadAndValidateKit(conv)
}
