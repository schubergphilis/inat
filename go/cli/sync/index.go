// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package sync

import (
	"sort"
	"strings"

	git "github.com/go-git/go-git/v5"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/git/source"
)

// classification is the bucket sync assigns to each installed kit per
// spec §2 / §4.2 step 5.
type classification int

const (
	caseUnknown classification = iota
	caseA                      // present at same name in src registry — no-op
	caseB                      // absent from src registry, SHA matches a different name in src
	caseC                      // absent from src registry, SHA matches in another configured source
	caseD                      // absent from src registry, no SHA match anywhere
)

// kitClassification carries the verdict and any candidates needed to
// render the report line for one installed kit.
type kitClassification struct {
	installKey string
	verdict    classification

	// sameSourceCandidates is non-empty for caseB. Sorted alphabetically;
	// length > 1 indicates ambiguity that the report flags.
	sameSourceCandidates []string

	// crossSourceCandidates is non-empty for caseC. Sorted by source
	// then kit; length > 1 indicates cross-source ambiguity.
	crossSourceCandidates []sourceKitRef
}

// sourceIndex bundles everything sync needs to know about a single
// source: the parsed registry, the cached repo handle (re-used by
// buildTreeSHAIndex), the registry subpath (if any), and the tree-SHA
// reverse index populated by buildTreeSHAIndex.
type sourceIndex struct {
	name      string
	registry  registry.Registry
	repo      *git.Repository
	subpath   string
	treeIndex map[string][]string // tree SHA → kit names within this source
}

// sourceKitRef names a kit by its source and within-source kit name.
// Used as the value type of the cross-source SHA index.
type sourceKitRef struct {
	Source string
	Kit    string
}

// groupInstalledBySource buckets installed keys by their source-name
// prefix. Keys missing the source/kit separator are skipped — sync's
// only goal is to surface divergence, not to crash on malformed config.
func groupInstalledBySource(cfg *config.Config) map[string][]string {
	out := make(map[string][]string)
	for installKey := range cfg.Installed {
		i := strings.Index(installKey, "/")
		if i < 0 {
			continue
		}
		sourceName := installKey[:i]
		out[sourceName] = append(out[sourceName], installKey)
	}
	return out
}

// fetchAllSources clones-or-updates each configured source's cache and
// parses its registry. Returns the per-source state map keyed by source
// name. The treeIndex field is left nil — populated by buildTreeSHAIndex.
// Fetch errors short-circuit per spec §4.2 step 3.
func fetchAllSources(cfg *config.Config, homeOverride string) (map[string]*sourceIndex, error) {
	out := make(map[string]*sourceIndex)
	for _, src := range cfg.Sources {
		idx, err := loadOneSource(src, homeOverride)
		if err != nil {
			return nil, err
		}
		out[src.Name] = idx
	}
	return out, nil
}

func loadOneSource(src config.Source, homeOverride string) (*sourceIndex, error) {
	cachePath, err := install.ResolveCachePath(src.Name, homeOverride)
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
	reg, err := source.FetchRegistryAt(repo, subpath)
	if err != nil {
		return nil, err
	}
	if err := registry.ValidateRegistry(&reg); err != nil {
		return nil, err
	}
	return &sourceIndex{
		name:     src.Name,
		registry: reg,
		repo:     repo,
		subpath:  subpath,
	}, nil
}

// buildTreeSHAIndex populates the per-source tree-SHA reverse index for
// each loaded source: for every kit advertised in the registry, fetch
// its tree SHA and record the mapping. Sync uses this in case (b)
// classification. Multiple kits within one source can share a tree SHA
// (e.g. duplicate content); the value slice preserves all matching kit
// names so the classifier can flag ambiguity.
func buildTreeSHAIndex(sources map[string]*sourceIndex) error {
	for _, idx := range sources {
		idx.treeIndex = make(map[string][]string)
		for kitName := range idx.registry.Kits {
			hash, _, err := source.FetchKitTreeAt(idx.repo, idx.subpath, kitName)
			if err != nil {
				return err
			}
			sha := hash.String()
			idx.treeIndex[sha] = append(idx.treeIndex[sha], kitName)
		}
	}
	return nil
}

// buildCrossSourceIndex builds the union of every per-source tree-SHA
// index, with values keyed by (sourceName, kitName). Sync uses this in
// case (c) classification. Multiple sources can share a tree SHA; the
// value slice preserves all matching refs.
func buildCrossSourceIndex(sources map[string]*sourceIndex) map[string][]sourceKitRef {
	out := make(map[string][]sourceKitRef)
	for _, idx := range sources {
		for sha, kits := range idx.treeIndex {
			for _, kit := range kits {
				out[sha] = append(out[sha], sourceKitRef{Source: idx.name, Kit: kit})
			}
		}
	}
	return out
}

// classifyKit assigns one of the four sync cases to a single installed
// kit. Pure function over its inputs — no I/O. Strict tree-SHA equality
// only; content-drift cases fall through to caseD.
func classifyKit(
	installKey string, installed config.InstalledKit,
	sources map[string]*sourceIndex, crossSourceIdx map[string][]sourceKitRef,
) kitClassification {
	out := kitClassification{installKey: installKey, verdict: caseD}
	i := strings.Index(installKey, "/")
	if i < 0 {
		return out
	}
	sourceName, kitName := installKey[:i], installKey[i+1:]
	src, ok := sources[sourceName]
	if !ok {
		return out
	}
	if _, ok := src.registry.Kits[kitName]; ok {
		out.verdict = caseA
		return out
	}
	if matches, ok := src.treeIndex[installed.Tree]; ok && len(matches) > 0 {
		sorted := append([]string(nil), matches...)
		sort.Strings(sorted)
		out.verdict = caseB
		out.sameSourceCandidates = sorted
		return out
	}
	if refs, ok := crossSourceIdx[installed.Tree]; ok && len(refs) > 0 {
		sorted := append([]sourceKitRef(nil), refs...)
		sortRefs(sorted)
		out.verdict = caseC
		out.crossSourceCandidates = sorted
		return out
	}
	return out
}

func sortRefs(refs []sourceKitRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Source != refs[j].Source {
			return refs[i].Source < refs[j].Source
		}
		return refs[i].Kit < refs[j].Kit
	})
}
