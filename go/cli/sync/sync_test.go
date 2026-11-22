// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package sync

import (
	"errors"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func TestGroupInstalledBySourceBuckets(t *testing.T) {
	cfg := &config.Config{
		Installed: map[string]config.InstalledKit{
			"alpha/one":  {Tree: "t1"},
			"alpha/two":  {Tree: "t2"},
			"beta/three": {Tree: "t3"},
			"gamma/four": {Tree: "t4"},
			"malformed":  {Tree: "t5"}, // no slash → skipped
		},
	}
	got := groupInstalledBySource(cfg)
	if len(got) != 3 {
		t.Fatalf("want 3 buckets, got %d (%v)", len(got), got)
	}
	for _, src := range []string{"alpha", "beta", "gamma"} {
		if _, ok := got[src]; !ok {
			t.Fatalf("missing bucket for %q: %v", src, got)
		}
	}
	if len(got["alpha"]) != 2 {
		t.Fatalf("alpha bucket: want 2, got %v", got["alpha"])
	}
	sort.Strings(got["alpha"])
	if got["alpha"][0] != "alpha/one" || got["alpha"][1] != "alpha/two" {
		t.Fatalf("alpha contents: %v", got["alpha"])
	}
}

func TestGroupInstalledBySourceEmptyConfig(t *testing.T) {
	got := groupInstalledBySource(&config.Config{Installed: map[string]config.InstalledKit{}})
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestFetchAllSourcesParsesRegistry(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	home := t.TempDir()
	cfg := &config.Config{
		Sources: []config.Source{{Name: "test-source", URL: ctx.URL}},
	}

	got, err := fetchAllSources(cfg, home)
	if err != nil {
		t.Fatalf("fetchAllSources: %v", err)
	}
	idx, ok := got["test-source"]
	if !ok {
		t.Fatalf("test-source missing from result: %v", got)
	}
	if idx.repo == nil {
		t.Fatalf("repo handle missing")
	}
	if _, ok := idx.registry.Kits["test-kit"]; !ok {
		t.Fatalf("test-kit missing from parsed registry: %+v", idx.registry)
	}
	if idx.treeIndex != nil {
		t.Fatalf("treeIndex should be nil at this stage: %v", idx.treeIndex)
	}
}

func TestFetchAllSourcesShortCircuitsOnUnreachable(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{
		Sources: []config.Source{{Name: "broken", URL: "/path/that/does/not/exist"}},
	}

	_, err := fetchAllSources(cfg, home)
	if err == nil {
		t.Fatalf("want error from unreachable source, got nil")
	}
}

func TestFetchAllSourcesNoSources(t *testing.T) {
	got, err := fetchAllSources(&config.Config{}, t.TempDir())
	if err != nil {
		t.Fatalf("fetchAllSources: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty map, got %v", got)
	}
}

func TestBuildTreeSHAIndexSingleKit(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	home := t.TempDir()
	cfg := &config.Config{Sources: []config.Source{{Name: "test-source", URL: ctx.URL}}}
	sources, err := fetchAllSources(cfg, home)
	if err != nil {
		t.Fatal(err)
	}

	if err := buildTreeSHAIndex(sources); err != nil {
		t.Fatalf("buildTreeSHAIndex: %v", err)
	}

	idx := sources["test-source"]
	if len(idx.treeIndex) != 1 {
		t.Fatalf("want 1 tree-SHA entry, got %d (%v)", len(idx.treeIndex), idx.treeIndex)
	}
	for sha, kits := range idx.treeIndex {
		if sha == "" {
			t.Fatalf("empty SHA key")
		}
		if len(kits) != 1 || kits[0] != "test-kit" {
			t.Fatalf("unexpected kit list under %s: %v", sha, kits)
		}
	}
}

func TestBuildCrossSourceIndexUnion(t *testing.T) {
	// Two identical remotes: same files, same tree SHA at kits/test-kit.
	// The cross-source index should flag the SHA as ambiguous (two refs).
	ctxA := testfixture.CreateTestRemote(t)
	ctxB := testfixture.CreateTestRemote(t)
	home := t.TempDir()
	cfg := &config.Config{Sources: []config.Source{
		{Name: "src-a", URL: ctxA.URL},
		{Name: "src-b", URL: ctxB.URL},
	}}
	sources, err := fetchAllSources(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := buildTreeSHAIndex(sources); err != nil {
		t.Fatal(err)
	}

	cross := buildCrossSourceIndex(sources)
	if len(cross) != 1 {
		t.Fatalf("want 1 SHA entry across sources (byte-identical), got %d (%v)", len(cross), cross)
	}
	for _, refs := range cross {
		if len(refs) != 2 {
			t.Fatalf("want 2 cross-source refs (ambiguous), got %d (%v)", len(refs), refs)
		}
		seen := map[string]bool{}
		for _, r := range refs {
			seen[r.Source+"/"+r.Kit] = true
		}
		if !seen["src-a/test-kit"] || !seen["src-b/test-kit"] {
			t.Fatalf("missing expected refs: %v", refs)
		}
	}
}

func TestBuildCrossSourceIndexEmpty(t *testing.T) {
	got := buildCrossSourceIndex(map[string]*sourceIndex{})
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

// classifyTestSources builds in-memory source indexes for the
// classifier table-driven tests. No fetching, no fixtures — just
// hand-crafted maps.
func classifyTestSources(t *testing.T) (map[string]*sourceIndex, map[string][]sourceKitRef) {
	t.Helper()
	src1 := &sourceIndex{
		name: "src1",
		registry: registryShape("kit-still-here", "kit-renamed-target",
			"kit-collision-1", "kit-collision-2"),
		treeIndex: map[string][]string{
			"sha-still-here":    {"kit-still-here"},
			"sha-renamed":       {"kit-renamed-target"},
			"sha-ambiguous-src": {"kit-collision-1", "kit-collision-2"},
		},
	}
	src2 := &sourceIndex{
		name:     "src2",
		registry: registryShape("kit-cross-target", "kit-cross-amb-1"),
		treeIndex: map[string][]string{
			"sha-cross-source": {"kit-cross-target"},
			"sha-cross-ambig":  {"kit-cross-amb-1"},
		},
	}
	src3 := &sourceIndex{
		name:     "src3",
		registry: registryShape("kit-cross-amb-2"),
		treeIndex: map[string][]string{
			"sha-cross-ambig": {"kit-cross-amb-2"},
		},
	}
	sources := map[string]*sourceIndex{"src1": src1, "src2": src2, "src3": src3}
	cross := buildCrossSourceIndex(sources)
	return sources, cross
}

func registryShape(kits ...string) registry.Registry {
	out := registry.Registry{Version: "1", Kits: map[string]registry.RegistryEntry{}}
	for _, k := range kits {
		out.Kits[k] = registry.RegistryEntry{Description: "x"}
	}
	return out
}

func TestClassifyKitCaseAStillInRegistry(t *testing.T) {
	sources, cross := classifyTestSources(t)
	got := classifyKit("src1/kit-still-here", config.InstalledKit{Tree: "sha-still-here"}, sources, cross)
	if got.verdict != caseA {
		t.Fatalf("verdict: %v", got.verdict)
	}
}

func TestClassifyKitCaseBRenamedInSource(t *testing.T) {
	sources, cross := classifyTestSources(t)
	// Recorded SHA "sha-renamed" is at "kit-renamed-target" in src1.
	// The installed key uses the OLD name "kit-old".
	got := classifyKit("src1/kit-old", config.InstalledKit{Tree: "sha-renamed"}, sources, cross)
	if got.verdict != caseB {
		t.Fatalf("verdict: %v want caseB", got.verdict)
	}
	if len(got.sameSourceCandidates) != 1 || got.sameSourceCandidates[0] != "kit-renamed-target" {
		t.Fatalf("candidates: %v", got.sameSourceCandidates)
	}
}

func TestClassifyKitCaseBAmbiguousSorted(t *testing.T) {
	sources, cross := classifyTestSources(t)
	got := classifyKit("src1/kit-old", config.InstalledKit{Tree: "sha-ambiguous-src"}, sources, cross)
	if got.verdict != caseB {
		t.Fatalf("verdict: %v want caseB", got.verdict)
	}
	if len(got.sameSourceCandidates) != 2 {
		t.Fatalf("want 2 candidates, got %v", got.sameSourceCandidates)
	}
	if got.sameSourceCandidates[0] != "kit-collision-1" || got.sameSourceCandidates[1] != "kit-collision-2" {
		t.Fatalf("not sorted: %v", got.sameSourceCandidates)
	}
}

func TestClassifyKitCaseCMovedAcrossSources(t *testing.T) {
	sources, cross := classifyTestSources(t)
	// Recorded SHA "sha-cross-source" is in src2 only.
	got := classifyKit("src1/kit-departed", config.InstalledKit{Tree: "sha-cross-source"}, sources, cross)
	if got.verdict != caseC {
		t.Fatalf("verdict: %v want caseC", got.verdict)
	}
	if len(got.crossSourceCandidates) != 1 {
		t.Fatalf("candidates: %v", got.crossSourceCandidates)
	}
	if got.crossSourceCandidates[0] != (sourceKitRef{Source: "src2", Kit: "kit-cross-target"}) {
		t.Fatalf("ref: %+v", got.crossSourceCandidates[0])
	}
}

func TestClassifyKitCaseCAmbiguousSorted(t *testing.T) {
	sources, cross := classifyTestSources(t)
	got := classifyKit("src1/kit-departed", config.InstalledKit{Tree: "sha-cross-ambig"}, sources, cross)
	if got.verdict != caseC {
		t.Fatalf("verdict: %v want caseC", got.verdict)
	}
	if len(got.crossSourceCandidates) != 2 {
		t.Fatalf("candidates: %v", got.crossSourceCandidates)
	}
	first, second := got.crossSourceCandidates[0], got.crossSourceCandidates[1]
	if first.Source != "src2" || first.Kit != "kit-cross-amb-1" {
		t.Fatalf("first ref: %+v", first)
	}
	if second.Source != "src3" || second.Kit != "kit-cross-amb-2" {
		t.Fatalf("second ref: %+v", second)
	}
}

func TestClassifyKitCaseDDeletedAnywhere(t *testing.T) {
	sources, cross := classifyTestSources(t)
	got := classifyKit("src1/kit-departed", config.InstalledKit{Tree: "sha-not-found-anywhere"}, sources, cross)
	if got.verdict != caseD {
		t.Fatalf("verdict: %v want caseD", got.verdict)
	}
	if len(got.sameSourceCandidates) != 0 || len(got.crossSourceCandidates) != 0 {
		t.Fatalf("unexpected candidates: %+v", got)
	}
}

func TestClassifyKitMissingSourceFallsThrough(t *testing.T) {
	sources, cross := classifyTestSources(t)
	got := classifyKit("unknown-source/foo", config.InstalledKit{Tree: "irrelevant"}, sources, cross)
	if got.verdict != caseD {
		t.Fatalf("verdict: %v want caseD", got.verdict)
	}
}

func TestClassifyKitMalformedKey(t *testing.T) {
	sources, cross := classifyTestSources(t)
	got := classifyKit("no-slash-key", config.InstalledKit{Tree: "irrelevant"}, sources, cross)
	if got.verdict != caseD {
		t.Fatalf("verdict: %v want caseD", got.verdict)
	}
}

// TestFormatReportCleanState covers spec criterion 2: every install is
// caseA, the report says "0 need attention", and no footer is emitted.
func TestFormatReportCleanState(t *testing.T) {
	report, divergence := formatReport([]kitClassification{
		{installKey: "src/a", verdict: caseA},
		{installKey: "src/b", verdict: caseA},
	})
	if divergence != 0 {
		t.Fatalf("divergence: %d want 0", divergence)
	}
	if !strings.Contains(report, "2 kits installed; 0 need attention.") {
		t.Fatalf("heading wrong: %q", report)
	}
	if strings.Contains(report, "suggested:") {
		t.Fatalf("clean report should not have suggestions: %q", report)
	}
	if strings.Contains(report, "run `inat sync` again") {
		t.Fatalf("clean report should not have footer: %q", report)
	}
}

// TestFormatReportRenderEachCase covers the report shape for caseB,
// caseC, and caseD bullets — including the ambiguous-cross-source
// branch and the suggestion lines.
func TestFormatReportRenderEachCase(t *testing.T) {
	classifications := []kitClassification{
		{installKey: "src/a", verdict: caseA},
		{installKey: "src/b", verdict: caseB, sameSourceCandidates: []string{"b-renamed"}},
		{installKey: "src/c", verdict: caseB, sameSourceCandidates: []string{"c-1", "c-2"}},
		{installKey: "src/d", verdict: caseC, crossSourceCandidates: []sourceKitRef{{Source: "alt", Kit: "d-target"}}},
		{installKey: "src/e", verdict: caseC, crossSourceCandidates: []sourceKitRef{
			{Source: "alt", Kit: "e1"}, {Source: "other", Kit: "e2"},
		}},
		{installKey: "src/f", verdict: caseD},
	}
	report, divergence := formatReport(classifications)
	if divergence != 5 {
		t.Fatalf("divergence: %d want 5", divergence)
	}
	wants := []string{
		"6 kits installed; 5 need attention.",
		"src/b — renamed in source 'src' to 'b-renamed'",
		"suggested: inat rename src/b src/b-renamed",
		"src/c — renamed in source 'src' to 'c-1'",
		"multiple candidates: c-1, c-2",
		"suggested: inat rename src/c src/c-1",
		"src/d — moved to source 'alt' as 'd-target'",
		"suggested: inat replace src/d alt/d-target",
		"src/e — multiple candidates across sources",
		"alt/e1",
		"other/e2",
		"src/f — deleted from source 'src'",
		"suggested: inat remove src/f",
		"run `inat sync` again",
	}
	for _, w := range wants {
		if !strings.Contains(report, w) {
			t.Fatalf("missing %q in report:\n%s", w, report)
		}
	}
}

// TestSyncCleanInstallReturnsZero covers the spec acceptance criterion
// 2: project with one cleanly installed kit, sync runs end-to-end and
// returns nil.
func TestSyncCleanInstallReturnsZero(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	if err := Sync(project, home); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// TestSyncReportsDeletion covers spec criterion 5: kit removed from
// source registry without a SHA match anywhere → caseD with remove
// suggestion, exit non-zero, SyncDivergence error.
func TestSyncReportsDeletion(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	// Replace the registry with a different kit (registry must be
	// non-empty per ValidateRegistry). The old test-kit dir stays on
	// disk in HEAD but the registry no longer advertises it, so
	// buildTreeSHAIndex doesn't enumerate it. The new kit's tree SHA
	// differs from the recorded test-kit SHA, so the SHA lookup also
	// misses → caseD with `inat remove` suggestion.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json":                      `{"version":"1","kits":{"unrelated":{"description":"u","types":["skill"]}}}`,
		"kits/unrelated/kit.json":            `{"name":"unrelated","description":"u","files":[{"path":"skills/foo","type":"skill"}]}`,
		"kits/unrelated/skills/foo/SKILL.md": "# Foo\nDifferent content.",
	})

	err := Sync(project, home)
	if !errors.Is(err, errs.ErrSyncDivergence) {
		t.Fatalf("want SyncDivergence, got %v", err)
	}
	var typed *errs.Error
	if !errors.As(err, &typed) {
		t.Fatalf("not a typed *errs.Error: %v", err)
	}
	if typed.Count != 1 {
		t.Fatalf("count: %d want 1", typed.Count)
	}
}

// TestSyncReportsInSourceRename covers spec criterion 3: source
// renames a kit (same content under a new name) → caseB with rename
// suggestion, SyncDivergence(1).
func TestSyncReportsInSourceRename(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	home := t.TempDir()
	if err := install.Install(project, "test-source/test-kit", home); err != nil {
		t.Fatal(err)
	}
	// Stage `kits/renamed-kit/` with byte-identical content and update
	// the registry so test-kit is gone but renamed-kit is present.
	testfixture.UpdateRemote(t, ctx, map[string]string{
		"registry.json":                           `{"version":"1","kits":{"renamed-kit":{"description":"Test","types":["skill","rule"]}}}`,
		"kits/renamed-kit/kit.json":               `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"kits/renamed-kit/skills/review/SKILL.md": "# Review\nDo code review.",
		"kits/renamed-kit/rules/no-secrets.md":    "Never commit secrets.",
		"kits/renamed-kit/rules/style.md":         "Follow style guidelines.",
	})

	err := Sync(project, home)
	if !errors.Is(err, errs.ErrSyncDivergence) {
		t.Fatalf("want SyncDivergence, got %v", err)
	}
}

// TestSyncEmptyInstalledExitsZero covers spec §4.2 step 1: empty
// Installed map → "nothing installed; nothing to sync", return nil.
func TestSyncEmptyInstalledExitsZero(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	if err := Sync(project, t.TempDir()); err != nil {
		t.Fatalf("sync on empty install: %v", err)
	}
}

// TestSyncRejectsInstalledFromUnconfiguredSource covers spec §2 (e):
// cfg.Installed has a kit whose source is not in cfg.Sources → return
// SourceNotFound.
func TestSyncCmdRejectsArgs(t *testing.T) {
	cmd := NewSyncCmd()
	cmd.SetArgs([]string{"unexpected"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected arg-count error, got nil")
	}
}

func TestSyncCmdShape(t *testing.T) {
	cmd := NewSyncCmd()
	if cmd.Use == "" {
		t.Fatalf("Use empty")
	}
	if cmd.Short == "" {
		t.Fatalf("Short empty")
	}
	if cmd.RunE == nil {
		t.Fatalf("RunE nil")
	}
}

func TestSyncRejectsInstalledFromUnconfiguredSource(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	project := testfixture.SetupProject(t, ctx.URL)
	cfg, _ := config.LoadConfig(project)
	cfg.Installed["ghost-source/foo"] = config.InstalledKit{Tree: "fake"}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatal(err)
	}

	err := Sync(project, t.TempDir())
	if !errors.Is(err, errs.ErrSourceNotFound) {
		t.Fatalf("want SourceNotFound, got %v", err)
	}
}
