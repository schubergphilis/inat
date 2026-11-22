// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import (
	git "github.com/go-git/go-git/v5"

	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/git/source"
)

// sourceSHAIndex is the lazy per-source tree-SHA reverse index used by
// auto-rename detection. For a given source's registry, it maps each
// kit's tree SHA to the kit name(s) that resolve to that SHA.
//
// The index is built **lazily** — only when at least one TargetUpdate
// target encounters the `kit-absent-from-registry` branch in planUpdate.
// A Plan invocation over a batch of cleanly-updating kits builds zero
// indices and pays zero extra `FetchKitTreeAt` calls.
//
// See spec 0018 §4.3 for the contract.
type sourceSHAIndex struct {
	repo    *git.Repository
	reg     registry.Registry
	subpath string

	// index maps treeSHA → []kitName. The slice value preserves the
	// ambiguity case: two kits in the same source may share a tree SHA
	// (e.g. `cp -r kits/foo kits/foo-copy` + commit). Both names land
	// in the slice; planUpdate refuses to pick when len > 1.
	index map[string][]string

	// built records that build() has been attempted. A failed build
	// still sets built = true so subsequent candidatesForSHA calls do
	// not re-attempt the failing fetch within the same Plan invocation.
	// A separate Plan invocation rebuilds from scratch.
	built bool
}

// buildHook is a package-private test seam: tests set it to count
// build() invocations. Production code calls it once at the top of
// build() before performing any work. Kept package-private (lowercase)
// so it does not leak to package consumers.
var buildHook func()

// candidatesForSHA returns the kit name(s) whose tree SHA equals the
// argument. Triggers a one-time `build()` on first call; subsequent
// calls reuse the cached index. Returns nil if no kit in the source
// indexed to the given SHA — including the case where the matching
// kit's tree-SHA fetch failed during build (the spec 0018 §4.3
// localised-error policy).
func (i *sourceSHAIndex) candidatesForSHA(treeSHA string) []string {
	if !i.built {
		i.build()
	}
	return i.index[treeSHA]
}

// build walks every kit listed in the source's registry, fetches its
// tree SHA, and populates the reverse index. Per spec 0018 §4.3, a
// mid-build `FetchKitTreeAt` failure is **localised**: the partial
// index built so far is retained; `built` is set true so the failing
// fetch is not retried within the same Plan invocation; targets whose
// recorded SHA happens to match an unindexed kit fall through to
// today's `KitNotFound` from planUpdate. The error is silently
// dropped — the caller has no policy lever it could pull, and the
// downstream behaviour (KitNotFound) is the same as if the corrupt
// kit had been absent from the registry.
func (i *sourceSHAIndex) build() {
	if buildHook != nil {
		buildHook()
	}
	i.index = make(map[string][]string, len(i.reg.Kits))
	for kitName := range i.reg.Kits {
		h, _, err := source.FetchKitTreeAt(i.repo, i.subpath, kitName)
		if err != nil {
			continue
		}
		sha := h.String()
		i.index[sha] = append(i.index[sha], kitName)
	}
	i.built = true
}
