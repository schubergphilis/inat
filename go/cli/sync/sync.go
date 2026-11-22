// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package sync implements `inat sync`.
//
// Sync is the read-only diagnostic verb for kit-level divergence. It
// fetches every configured source, classifies each installed kit into
// one of four buckets (in-registry / in-source-renamed / cross-source-
// moved / deleted), prints a report with suggested remediation, and
// returns SyncDivergence(N) when N>0. Sync never mutates
// .inat/config.json or any delivered file.
//
// See spec 0016 §4.2 for the contract.
package sync

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

// NewSyncCmd returns the `inat sync` cobra command. Registered in
// main.go's root command tree.
func NewSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Diagnose divergence between installed kits and source registries.",
		Long: "Read-only check that fetches every configured source and reports each\n" +
			"installed kit that has been renamed, moved between sources, or deleted\n" +
			"upstream. Exits non-zero on any divergence so CI can gate on drift. Sync\n" +
			"never mutates project config or delivered files; remediation is left to\n" +
			"explicit `inat rename` / `inat replace` / `inat remove` commands.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunSync(scopecmd.IsUser(cmd))
		},
	}
}

// RunSync handles `inat sync` against cwd.
func RunSync(user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return Sync(dir, "")
}

// Sync runs the read-only divergence diagnostic against an explicit
// project dir. homeOverride lets tests redirect cache lookups.
//
// Pipeline order matches spec 0016 §4.2: load config → group by source
// → validate every installed source is configured → fetch all sources
// → build per-source + cross-source SHA indexes → classify every
// installed kit → render report → return SyncDivergence(M) when M > 0.
func Sync(dir, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	if len(cfg.Installed) == 0 {
		fmt.Fprintln(os.Stderr, "nothing installed; nothing to sync")
		return nil
	}
	grouped := groupInstalledBySource(&cfg)
	if err := ensureSourcesConfigured(&cfg, grouped); err != nil {
		return err
	}
	sources, err := fetchAllSources(&cfg, homeOverride)
	if err != nil {
		return err
	}
	if err := buildTreeSHAIndex(sources); err != nil {
		return err
	}
	crossIdx := buildCrossSourceIndex(sources)
	classifications := classifyAll(&cfg, sources, crossIdx)
	report, divergence := formatReport(classifications)
	fmt.Fprint(os.Stderr, report)
	if divergence > 0 {
		return errs.NewSyncDivergence(divergence)
	}
	return nil
}

// ensureSourcesConfigured surfaces spec §2 (e): an installed kit whose
// source isn't in cfg.Sources is a fetch-error-shaped failure, not a
// divergence. Returns SourceNotFound naming the missing source.
func ensureSourcesConfigured(cfg *config.Config, grouped map[string][]string) error {
	configured := make(map[string]bool, len(cfg.Sources))
	for _, src := range cfg.Sources {
		configured[src.Name] = true
	}
	for sourceName := range grouped {
		if !configured[sourceName] {
			return errs.NewSourceNotFound(sourceName)
		}
	}
	return nil
}

// classifyAll runs classifyKit for every installed entry and returns
// the classifications sorted by install key for stable reporting.
func classifyAll(
	cfg *config.Config, sources map[string]*sourceIndex, crossIdx map[string][]sourceKitRef,
) []kitClassification {
	out := make([]kitClassification, 0, len(cfg.Installed))
	for installKey, installed := range cfg.Installed {
		out = append(out, classifyKit(installKey, installed, sources, crossIdx))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].installKey < out[j].installKey
	})
	return out
}

// formatReport renders the stderr report described in spec §4.2 step 6
// and returns it as a string plus the count of classifications that
// were not caseA. Pure function for ease of testing.
func formatReport(classifications []kitClassification) (string, int) {
	var b strings.Builder
	total := len(classifications)
	needAttention := 0
	for _, c := range classifications {
		if c.verdict != caseA {
			needAttention++
		}
	}
	fmt.Fprintf(&b, "%d %s installed; %d need attention.\n", total, kitWord(total), needAttention)
	for _, c := range classifications {
		if c.verdict == caseA {
			continue
		}
		writeReportEntry(&b, c)
	}
	if needAttention > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "run `inat sync` again after applying suggestions, or `inat update` to refresh remaining kits.")
	}
	return b.String(), needAttention
}

// writeReportEntry renders one per-kit bullet for the report.
func writeReportEntry(b *strings.Builder, c kitClassification) {
	sourceName := ""
	if i := strings.Index(c.installKey, "/"); i >= 0 {
		sourceName = c.installKey[:i]
	}
	switch c.verdict {
	case caseB:
		target := c.sameSourceCandidates[0]
		fmt.Fprintf(b, "  %s — renamed in source '%s' to '%s'\n", c.installKey, sourceName, target)
		if len(c.sameSourceCandidates) > 1 {
			fmt.Fprintf(b, "    multiple candidates: %s\n", strings.Join(c.sameSourceCandidates, ", "))
		}
		fmt.Fprintf(b, "    suggested: inat rename %s %s/%s\n", c.installKey, sourceName, target)
	case caseC:
		if len(c.crossSourceCandidates) > 1 {
			fmt.Fprintf(b, "  %s — multiple candidates across sources\n", c.installKey)
			for _, r := range c.crossSourceCandidates {
				fmt.Fprintf(b, "    %s/%s\n", r.Source, r.Kit)
			}
			return
		}
		ref := c.crossSourceCandidates[0]
		fmt.Fprintf(b, "  %s — moved to source '%s' as '%s'\n", c.installKey, ref.Source, ref.Kit)
		fmt.Fprintf(b, "    suggested: inat replace %s %s/%s\n", c.installKey, ref.Source, ref.Kit)
	case caseD:
		fmt.Fprintf(b, "  %s — deleted from source '%s'\n", c.installKey, sourceName)
		fmt.Fprintf(b, "    suggested: inat remove %s\n", c.installKey)
	}
}

func kitWord(n int) string {
	if n == 1 {
		return "kit"
	}
	return "kits"
}
