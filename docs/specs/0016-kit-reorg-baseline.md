## inat — Kit reorganization workflow, baseline (Spec 0016)

---

### 1. Overview

The kit corpus is starting to evolve in shapes the current CLI does not have a clean answer for. Today inat handles changes *inside* a kit perfectly — `inat update` removes every recorded delivered file and re-delivers the new tree (`go/cli/update/update.go:74-173`), so skill renames, deletions, and content edits inside a single kit just work. What it does *not* handle is **kit-level identity change**:

- A kit was renamed in the source repo (`source/old-name` → `source/new-name`). `update` reports `KitNotFound` and leaves the user to manually `remove old + install new`, losing install state and re-incurring drift checks.
- A kit was deleted upstream. Same `KitNotFound` error, with no guidance on what the user should do.
- A skill migrated from one installed kit to another in the same source. `update` walks kits in map iteration order and `CheckNameUniqueness` (`go/core/config/config.go:206-228`) rejects the receiving kit before the donating kit has relinquished the name. Failure is order-dependent and silent.
- A kit moved between sources (`sourceA/kit` → `sourceB/kit`). No path other than full manual remove + install.

This spec establishes the **manual workflow baseline**: three new CLI verbs (`sync`, `rename`, `replace`) and a non-fatal failure mode for `update`. After this spec ships, every reorganization scenario above has a clean, supported workflow even if it requires the user to type one extra command. The "make it automatic" magic — atomic transactional update across kits, plus auto-detected git-rename application — is split into [spec 0017](0017-transactional-update.md) and [spec 0018](0018-kit-reorg-auto-rename.md) so the baseline can ship and bake before the heuristic layer lands on top.

A companion implementation plan lives at [`docs/plan/0016-kit-reorg-baseline.md`](../plan/0016-kit-reorg-baseline.md).

This spec extends [`docs/specs/0001-initial-idea.md`](0001-initial-idea.md) (the kit lifecycle contract). It does not change the on-disk schema (`Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry` are unchanged), it does not change agent profiles, and it does not change the release pipeline.

---

### 2. Goals

1. A new command `inat sync` MUST diff the installed kit set against each configured source's current registry and report every divergence with a suggested remediation. The command is read-only — it MUST NOT mutate `.inat/config.json`, the cache, or any delivered file. Suggestions point at the new `rename` / `replace` verbs or at the existing `remove` / `install` verbs.
2. For each installed kit `cfg.Installed[src/kit]`, `inat sync` MUST classify the kit into exactly one of: (a) present at the same name in `src` registry — no-op; (b) absent from `src` registry but the recorded `Tree` SHA is present at a different name in `src` — suggest `inat rename src/kit src/foundName`; (c) absent from `src` registry but the recorded `Tree` SHA is present in *another* configured source's registry — suggest `inat replace src/kit otherSrc/foundName`; (d) absent from `src` registry and not found by SHA anywhere — suggest `inat remove src/kit` (treat as upstream deletion); (e) `src` itself unreachable / not in `cfg.Sources` — surface as a fetch error, not a divergence.
3. Sync's exit code MUST be 0 when every installed kit classifies as (a), and non-zero when any kit classifies as (b)–(d). (e) is a hard error with a non-zero exit and a separate stderr message. The intent is that CI pipelines can run `inat sync` as a drift gate.
4. A new command `inat rename src/old src/new` MUST rewrite `cfg.Installed`'s map key from `src/old` to `src/new` and save config, without touching any file under `.inat/`, the cache, or the agent target directories. Pre-conditions: `src/old` is in `cfg.Installed`; `src/new` is not in `cfg.Installed`; both refs share the same source name; the new kit exists in `src`'s registry; the new kit's tree SHA equals `cfg.Installed[src/old].Tree` (otherwise content drift means the on-disk delivered files are wrong for the new tree — the user must use `replace` or `update` instead). Each pre-condition violation MUST surface as a typed `errs.Error`.
5. A new command `inat replace src/old newSrc/new` MUST atomically remove the old install and install the new kit. The behaviour MUST equal `inat remove src/old --force=<flag>` followed by `inat install newSrc/new`, except that (a) `cfg.Installed` is mutated only once at the end, (b) `--force` is honoured during the remove half (drift on old delivered files), and (c) on `install` failure the prior `Installed[src/old]` entry is restored from a snapshot — the user's install state is never wedged half-way. Cross-source moves are the primary use case but `replace src/A src/B` is also accepted (no rule that `newSrc` must differ).
6. `inat update` (existing command) MUST change its multi-kit failure mode from "fail on first" to "skip and continue, exit non-zero". For each installed kit it touches, the command captures the per-kit result (`updated`, `up-to-date`, `needs-attention`), prints any per-kit error to stderr, and continues to the next kit. After all kits have been processed, the command saves the config (with whatever updates landed cleanly), prints a summary footer, and returns a typed `KitsNeedAttention` error with a non-zero process exit code if any kit ended in the `needs-attention` bucket. **No transactional / atomic-rollback behaviour is added in this spec** — that is spec 0017.
7. `inat update <single-kit>` (the targeted form) MUST keep its existing fail-fast contract. The skip-and-continue change applies only when iterating multiple kits; a user who explicitly named one kit expects a hard error if that kit fails.
8. The new commands MUST reuse the existing pipeline pieces — no duplication of clone, fetch-tree, walk, parse, derive, deliver, check-uniqueness, remove-files, clean-managed-sections logic. Any helper not already exported MUST be extracted from `cli/install/install.go` or `cli/remove/remove.go` rather than copied into the new packages.
9. No on-disk schema change. `Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry`, the `~/.inat/config.json` shape, the cache layout, and the agent-profile JSON all remain byte-stable. New configs written after this spec ships round-trip identically with older binaries (via the existing additive-compat convention recorded in CLAUDE.md memory).
10. No new module dependencies. The implementation stays within the allowlist (`spf13/cobra`, `go-git/go-git/v5`, standard library) per CLAUDE.md §8.
11. No goroutines and no asynchronous code paths. All I/O is sequential per CLAUDE.md §11.

---

### 3. Non-goals

- **Atomic / transactional `inat update`.** Skip-and-continue is the floor delivered here; turning the multi-kit pipeline into fetch / plan / apply phases is [spec 0017](0017-transactional-update.md). Until that ships, `update` is best-effort: a kit can leave the user in a state where its old delivery has been removed but the new one failed to land. The existing `--force`-aware drift policy and the kit-by-kit error reporting limit the blast radius; they do not eliminate it.
- **Auto-applying detected renames during `update`.** The current spec deliberately leaves all detection in `sync` (suggestion only) and all mutation in the explicit `rename` / `replace` verbs. Auto-application during `update` is gated on the transactional foundation from 0017 and is layered on top in [spec 0018](0018-kit-reorg-auto-rename.md). **Update**: closed by spec [0018](0018-kit-reorg-auto-rename.md).
- **Stable IDs / UUIDs per kit or per skill.** No retrofit of identity onto the kit corpus. `sync` matches by recorded tree SHA, which is content-addressed and bulletproof for the dominant `git mv` rename pattern; non-trivial cases (rename + content edit in the same commit) correctly fall through to the user-driven `replace` workflow.
- **Tombstone redirect markers in the source repo.** Considered and rejected during design grilling: option G in the plan-file decision tree. Authors do nothing extra in their kit repos; the consumer side does the work.
- **Author-side migration manifest.** No `moves.json`, no `aliases` field in `registry.json`. The mechanism is entirely consumer-driven (sync's content-fingerprint match) plus the consumer's manual verb (`rename` / `replace`).
- **A `sync --apply` shortcut.** Sync diagnoses; the user runs `rename` / `replace` / `remove` to act on the suggestions. Conflating diagnosis and execution forces the user to read the diff *after* it was applied, which is the wrong order. Future spec may revisit if multi-kit reorganization becomes a frequent operation.
- **Source-level rename (`inat sources rename`).** Source identity is `cfg.Sources[].Name`; if a user wants to rename it they `sources remove` + `sources add`. That workflow stays unchanged. Source URL changes are likewise out-of-scope for this spec.
- **Within-kit reorganization (skill-content edits, skill renames inside a kit, skill deletions inside a kit).** Already handled by `update` today; no changes needed. The within-kit case is documented here only to explain the scope boundary.
- **Cross-source detection that uses similarity / fuzzy matching.** `sync`'s cross-source classifier matches on tree-SHA equality only. A kit that was content-edited during the move from sourceA to sourceB will not be detected as a move; it will classify as (d) "deleted" with a suggestion to `inat remove`. The user can then `inat install newSrc/new` manually. Strict equality keeps the trust model simple and the false-positive rate at zero.
- **Audit trail / `inat history`.** Print to stderr when `rename` / `replace` mutates state; rely on git history of the source repo for retrospective analysis. No new on-disk record of "this kit used to be called X".
- **Recovery if `replace`'s install half fails after the remove half succeeded.** A snapshot of the `Installed` entry is preserved in memory and restored on install failure, but on-disk delivered files are removed first by the remove phase; restoring those would require a content-replay path that this spec does not add. The `Installed` entry restoration brings the user back to a state where `inat refresh` re-delivers from cache. **Update**: closed by spec [`0021`](0021-apply-snapshot-restore.md) — the apply phase now captures every mutable on-disk file before mutation and restores byte-identically on failure, so delivered files are no longer gone after a failed `replace`.

---

### 4. Design

#### 4.1 Command surface

```
inat sync                                    # diagnostic; exit non-zero on divergence
inat rename <src/old> <src/new>              # config rewrite; same source only
inat replace <src/old> <newSrc/new> [--force] # atomic remove+install; --force passed to remove half
```

All three verbs live at the top level alongside the existing `install`, `update`, `remove`, `refresh`, `status`, `validate`. `sync` takes no arguments and no flags in v1. `rename` requires both refs in `source/kit` form (no bare-name resolution — the verb is precise on purpose). `replace` accepts the same drift override as `remove` does today.

Exit codes: 0 on success or sync-with-no-divergence; non-zero on every error path including sync-with-divergence. Specific exit codes are not part of the contract; users that script around inat read the typed error names from stderr.

#### 4.2 `inat sync` pipeline

Strictly sequential. Each step is an extracted helper function ≤30 lines per CLAUDE.md §4.

1. **Load config.** `config.LoadConfig(dir)`. Empty `Installed` map → print `nothing installed; nothing to sync` and exit 0.
2. **Group installed kits by source name.** Build `map[sourceName][]installKey` for downstream per-source processing.
3. **Fetch every configured source.** For each source in `cfg.Sources`, `source.CloneOrFetch(src.URL, cachePath)` then `source.FetchRegistryAt(repo, subpath)`. Cache the parsed `Registry` plus a tree-SHA index `map[treeSHA]string` (kit-name → tree SHA reverse-map, built via one extra `source.FetchKitTreeAt` call per kit listed in the registry). Fetch errors short-circuit the command with a typed error naming the source.
4. **Build the cross-source SHA index.** Union of every source's tree-SHA map, with the value being `(sourceName, kitName)`. Used in step 5 case (c).
5. **Classify each installed kit.** For each `installKey = src/kit` in `cfg.Installed`:
   - Read `cfg.Installed[installKey].Tree` (the recorded SHA).
   - Look up the kit name in `src`'s parsed registry.
     - Found → case (a). No-op; skip.
     - Not found → continue to SHA matching.
   - Search `src`'s tree-SHA index for the recorded SHA.
     - Exactly one match at name `foundName` → case (b). Suggest `inat rename src/kit src/foundName`.
     - More than one match → ambiguous; emit case (b) suggestion for the alphabetically-first match plus a `multiple candidates: …` note.
     - No match → continue to cross-source SHA matching.
   - Search the cross-source SHA index.
     - Exactly one match at `(otherSrc, otherName)` → case (c). Suggest `inat replace src/kit otherSrc/otherName`.
     - More than one match → emit a "multiple candidates across sources" report; do not pick.
     - No match → case (d). Suggest `inat remove src/kit`.
6. **Print report.** Heading line: `<N> kits installed; <M> need attention.` Then one bullet per non-(a) kit, in the form:
   ```
     company/old-name — renamed in source 'company' to 'new-name'
       suggested: inat rename company/old-name company/new-name
   ```
   Footer: `run \`inat sync\` again after applying suggestions, or \`inat update\` to refresh remaining kits.`
7. **Exit code.** 0 if every kit was case (a); non-zero otherwise. The typed error returned from the cobra `RunE` is `errs.NewSyncDivergence(M)` where `M` is the count.

Sync does not write to `.inat/config.json`. It does write to `~/.inat/cache/` (via `CloneOrFetch`) — that's a side-effect of fetching the source, not a mutation of user state.

#### 4.3 `inat rename` pipeline

1. **Load config.** `config.LoadConfig(dir)`.
2. **Parse refs.** Both args MUST be `source/kit` form. Bare names are rejected with `errs.NewKitValidation("rename requires source/kit form")`. A future spec may grow ambiguity resolution if usage demands.
3. **Validate same source.** `oldSrc, oldKit := split(args[0])`; same for `args[1]`. If `oldSrc != newSrc` → `errs.NewKitValidation("rename is single-source only; use 'replace' for cross-source moves")`.
4. **Validate old install exists.** `cfg.Installed[oldKey]` MUST be present.
5. **Validate new install absent.** `cfg.Installed[newKey]` MUST NOT be present (otherwise the user has both kits installed, which is a different reorganization the user must resolve manually first).
6. **Fetch source and registry.** `source.CloneOrFetch` + `source.FetchRegistryAt` — same as install/update.
7. **Validate new kit in registry.** `reg.Kits[newKit]` MUST be present → `errs.NewKitNotFound`.
8. **Validate tree-SHA equality.** `source.FetchKitTreeAt(repo, subpath, newKit).String()` MUST equal `cfg.Installed[oldKey].Tree`. Mismatch → `errs.NewRenameTreeMismatch(oldKey, newKey, oldSHA, newSHA)`. The user is told to use `replace` or to `update` first.
9. **Mutate config.** `cfg.Installed[newKey] = cfg.Installed[oldKey]; delete(cfg.Installed, oldKey)`.
10. **Save config.** `config.SaveConfig`.
11. **Print result.** `renamed src/old → src/new` to stderr.

Rename does NOT touch any delivered file, any agent target file, or any managed-section header. The header in the always-rule managed section embeds `source/kit/filename` (`go/cli/install/install.go:219`); after a rename the header still refers to the old `kit` name. This is acceptable in v1 because (a) the header is informational, (b) re-running `inat update src/new` after the rename will re-deliver the always rule with the corrected header. A future spec may add header rewriting to `rename` if the staleness becomes user-visible. **Update**: closed by spec [0019](0019-rename-header-rewrite.md) — rename now rewrites the headers in place as a final step before saving config.

#### 4.4 `inat replace` pipeline

1. **Load config.** `config.LoadConfig(dir)`.
2. **Parse refs.** Both args MUST be `source/kit` form. Cross-source is the typical use; same-source is allowed.
3. **Validate old install exists.** `cfg.Installed[oldKey]` MUST be present.
4. **Validate new install absent.** `cfg.Installed[newKey]` MUST NOT be present.
5. **Snapshot.** Capture `oldEntry := cfg.Installed[oldKey]` (deep copy of the slice fields) for rollback.
6. **Remove old.** Run the existing remove pipeline against `oldKey`: `removecmd.RemoveDeliveredFiles(dir, oldEntry.Delivered, force)` then `removecmd.CleanManagedSections(dir, activeAgents, oldKey, refPaths)`. Drift skips honour `--force`. Mutate `cfg.Installed`: `delete(cfg.Installed, oldKey)`. **Do not save config yet.**
7. **Install new.** Run the existing install pipeline against `newKey`: cache fetch → registry validate → kit-tree fetch → walk → parse → derive names → check uniqueness → deliver. Mutate `cfg.Installed[newKey] = newEntry`.
8. **On install failure**: restore the snapshot. `cfg.Installed[oldKey] = oldEntry`. Wrap and return the original install error with a typed `errs.NewReplaceInstallFailed(oldKey, newKey, cause)`. **Note:** the on-disk delivered files for `oldKey` were already removed in step 6; the restored config now describes a kit whose files are gone. The user's recovery path is `inat refresh` (re-delivers from cache). Step 6's drift policy means user-modified files have not been touched, so the loss is bounded.
9. **Save config.** `config.SaveConfig` — single write, contains either the post-install state (success) or the snapshot-restored state (failure).
10. **Print result.** `replaced src/old → newSrc/new` to stderr on success.

#### 4.5 `inat update` skip-and-continue change

Existing structure (`go/cli/update/update.go:32-57`) iterates `keys` from `resolveTargets` and calls `updateSingle` per key. The current loop returns on the first error. The change is local: convert the loop to collect `(key, result, error)` tuples, continue on per-key error, and return a typed multi-kit error after the loop.

```go
type kitOutcome struct {
    Key    string
    State  string  // "updated" | "up-to-date" | "needs-attention"
    Err    error   // nil unless State == "needs-attention"
}
```

Targeted form (`kitRef != ""`) keeps fail-fast: a single key in `keys`, single iteration, return error directly — no behavioural change for `inat update src/kit`.

The post-loop reporting writes one stderr line per `needs-attention` outcome (`<key>: <error>`), then the summary `<U> updated, <S> up-to-date, <N> need attention` and (if `N > 0`) the footer `run \`inat sync\` for guidance`. The function returns `errs.NewKitsNeedAttention(N)` if `N > 0`, otherwise nil.

Config is saved exactly once (existing logic), at the end, capturing whatever updates landed cleanly. This means the user's `.inat/config.json` after a partial-failure run reflects: the new state for kits that updated cleanly, the unchanged state for kits that errored (including kits whose old delivery was successfully removed but whose new delivery failed mid-way — this is the partial-state risk the non-goal in §3 calls out).

#### 4.6 New typed errors in `errs`

Each new failure mode gets a typed `errs.Kind` variant with a constructor mirroring the existing pattern:

- `SyncDivergence(count int)` — sync ran cleanly but found `count` kits needing attention.
- `KitsNeedAttention(count int)` — multi-kit `update` finished with `count` kits in the needs-attention bucket.
- `RenameTreeMismatch(oldKey, newKey, oldSHA, newSHA string)` — rename's tree-SHA equality check failed.
- `RenameCrossSource(oldSrc, newSrc string)` — rename was given two different source names.
- `ReplaceInstallFailed(oldKey, newKey string, cause error)` — replace rolled back its install half; wraps the underlying cause.

`KitNotFound`, `KitNotInstalled`, `KitAlreadyInstalled`, `SourceNotFound`, `KitValidation` are reused — no new variants for cases the existing kinds already cover.

#### 4.7 File layout

Three new packages plus a small set of edits to existing ones.

```
go/cli/sync/sync.go         (~150 lines)  RunSync entry point + classify helper
go/cli/sync/sync_test.go    (~250 lines)  table-driven classification tests + e2e fake-source test
go/cli/rename/rename.go     (~120 lines)  RunRename + per-step helpers
go/cli/rename/rename_test.go (~180 lines) happy path + each pre-condition violation
go/cli/replace/replace.go   (~140 lines)  RunReplace + snapshot + rollback
go/cli/replace/replace_test.go (~220 lines) happy path + drift on old + install-failure rollback
```

Existing files modified:

```
go/cli/install/install.go   (refactor: extract pipeline steps used by replace)
go/cli/remove/remove.go     (refactor: extract pipeline steps used by replace)
go/cli/update/update.go     (skip-and-continue conversion + summary footer)
go/main.go                  (register sync, rename, replace cobra commands)
go/errs/errs.go             (add 5 new Kind variants + constructors + Error cases)
```

Each new file under the 300-line target; each new function under 30 lines.

#### 4.8 Why split rename and replace into two verbs

`rename` is a pure `cfg.Installed` map-key change, no I/O on delivered files, no re-fetch beyond a single tree-SHA validation. The user's working tree is untouched. This is the right behaviour when upstream did a pure `git mv` of the kit directory: on-disk delivered files are still correct because the tree content is byte-identical at the new location.

`replace` is a destructive remove + install, re-fetching the new kit's full tree, re-running delivery, re-checking name uniqueness against the post-remove state. This is the right behaviour when (a) the kit moved across sources (different repo, possibly different content), or (b) the user wants a full re-deliver for any reason.

Collapsing both into a single verb would force one side or the other into the wrong contract. Keeping them split lets `sync`'s suggestion be precise about which one applies, and lets users audit the correct level of churn before they run it.

#### 4.9 Why `sync` does not run during normal commands

`inat install`, `inat update <kit>`, `inat remove`, and `inat refresh` keep their current scopes. None of them gain a "while I'm here, also check if anything else is out of sync" side effect. Reasons:

- **Latency.** `sync` fetches every configured source. Adding it to a per-kit `inat update` would multiply the network cost of every operation by the source count.
- **Predictability.** A user who runs `inat install company/foo` expects exactly that: one fetch, one install. They don't want a multi-source drift scan on every command.
- **Debuggability.** Failures surface against the verb the user invoked. A "you ran install but sync failed" cross-talk would muddy the error model.

The user's mental model is: `update` for routine refresh (skip-and-continue tells them when something needs attention); `sync` for explicit drift audit; `rename` / `replace` for explicit remediation.

---

### 5. Compatibility

- **`go/cli/install/install.go` / `go/cli/remove/remove.go`** — refactored to expose smaller helpers but no externally-observable behaviour change. `RunInstall` and `RunRemove` keep their entry-point signatures.
- **`go/cli/update/update.go`** — `RunUpdate` and `Update` keep their signatures. Multi-kit failure mode is the only behavioural change; targeted `update <kit>` is byte-identical to today.
- **`.inat/config.json`** — schema unchanged. Configs written before / after this spec round-trip identically.
- **`~/.inat/config.json`** — schema unchanged.
- **`~/.inat/cache/`** — layout unchanged; sync uses the same `CloneOrFetch` paths everything else does.
- **Agent profiles** — unchanged.
- **Built-in rules** — unchanged. `inat-usage` and `inat-kits` reservation is enforced by the same `CheckNameUniqueness` the new commands all call through.
- **`go/go.mod` / `go/go.sum`** — byte-identical pre/post change. No new modules; `errors.Is` / `errors.As` semantics over the new typed errors stay in the existing `errs` package.
- **Existing CLI commands** — unchanged. New verbs (`sync`, `rename`, `replace`) do not shadow any existing top-level command.
- **`README.md`** — gains a "Reorganization" section with one paragraph each on `sync`, `rename`, `replace`, and the new `update` summary footer. Cross-link to spec 0016.
- **`CHANGELOG.md`** — `[Unreleased]` gains an `### Added` entry for sync / rename / replace, and a `### Changed` entry for `update`'s skip-and-continue.
- **Pre-commit hook + CI** — unchanged. The hook runs `gofmt`, `go vet`, `staticcheck`, `go test ./...`; the new packages are covered by all four. The spec-presence check is satisfied by this spec/plan pair.

---

### 6. Acceptance criteria

1. **Build clean.** `go -C go/ build ./...` exits 0; the binary's `--help` output lists `sync`, `rename`, `replace` alongside the existing top-level commands.
2. **Sync against a clean install.** Project with two installed kits, both present at the same names in their source registries: `inat sync` prints `2 kits installed; 0 need attention.`, exits 0. No mutation to `.inat/config.json`.
3. **Sync detects in-source rename.** Local fake source repo with kit `old-name`; install it; `git mv kits/old-name kits/new-name` in the source; commit + push; `inat sync` reports case (b) with the `inat rename company/old-name company/new-name` suggestion. Exits non-zero. `.inat/config.json` unchanged.
4. **Sync detects cross-source move.** Two configured local fake sources, kit installed from sourceA, kit deleted from sourceA, identical tree pushed under a (possibly different) name in sourceB: `inat sync` reports case (c) with the `inat replace ...` suggestion. Exits non-zero.
5. **Sync detects deletion.** Installed kit, deleted from source registry without a same-tree match anywhere: `inat sync` reports case (d) with the `inat remove ...` suggestion. Exits non-zero.
6. **Rename happy path.** After (3), `inat rename company/old-name company/new-name` succeeds; `cfg.Installed` contains `company/new-name` and not `company/old-name`; on-disk delivered files are byte-identical to before the rename; `inat sync` after the rename reports zero divergences.
7. **Rename rejects cross-source.** `inat rename company/foo vendor/foo` returns `RenameCrossSource`; config unchanged.
8. **Rename rejects tree-SHA mismatch.** Set up a state where `cfg.Installed[company/old].Tree` does not match the registry's current `kits/new` tree (e.g. content was edited between rename and the user's update): `inat rename company/old company/new` returns `RenameTreeMismatch`; config unchanged.
9. **Replace happy path.** After (4), `inat replace company/foo vendor/foo` removes `company/foo`'s delivered files (drift policy applied), installs `vendor/foo`, mutates config in one save. After: `cfg.Installed` has `vendor/foo` only; no `company/foo` directory; agent target managed sections cleaned of `company/foo` references and re-populated for `vendor/foo`.
10. **Replace rolls back on install failure.** Synthesise an install failure (e.g. `vendor/foo` declares a name that collides with a third installed kit): replace's remove half ran (old delivered files gone, managed-section entries cleaned), install half failed, `cfg.Installed[company/foo]` is restored to its pre-call value via the snapshot path; the typed error is `ReplaceInstallFailed(company/foo, vendor/foo, cause: DuplicateName)`. The user can `inat refresh` to re-deliver `company/foo` from cache.
11. **Replace honours `--force`.** Locally modify a delivered file from `company/foo`; run `inat replace company/foo vendor/foo` (no `--force`): the modified file is skipped during remove and the install fails with a `KitsNeedAttention`-shaped error suggesting `--force`. Re-run with `--force`: succeeds.
12. **Update skip-and-continue (multi-kit).** Three installed kits A, B, C; B's source registry has dropped B (renamed to B' with no tree-SHA match — beyond the simple-rename case). `inat update` (no args) updates A and C cleanly, reports B as needs-attention, exits non-zero with `KitsNeedAttention(1)`. `cfg.Installed` reflects the post-update state for A and C and the unchanged state for B.
13. **Update fail-fast (single-kit).** `inat update bogus/kit` returns the existing `KitNotInstalled` typed error with non-zero exit; behaviour identical to before this spec.
14. **No on-disk schema change.** Diff `cfg` JSON pre/post a sync run, pre/post a rename, pre/post a successful replace: only the keys the spec mutates change; no new fields, no reordering.
15. **No new module dependencies.** `git diff go/go.mod go/go.sum` between the pre-change and post-change tree is empty.
16. **No `unsafe` package, no goroutines.** `grep -n "unsafe\|go func\|go " go/cli/sync/ go/cli/rename/ go/cli/replace/` returns nothing; `staticcheck` and `go vet` pass.
17. **File / function size limits.** Every new file under 300 lines; every new function under 30 lines.
18. **Pre-commit hook clean.** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all exit 0.

---

### 7. Testing notes

- **Fake sources via local bare repos.** Sync, rename, replace tests stage two or three local bare git repos in `t.TempDir()`, populate them with `kit.json` + `registry.json`, and configure the inat project under test to point at them via `cfg.Sources[].URL = file://…`. No real GitLab calls in CI; the existing test harness for `cli/install` and `cli/update` already does this and is the template.
- **Tree-SHA construction in tests.** Tree SHAs are observed via `source.FetchKitTreeAt`; tests do not hard-code SHAs (they would change if the test fixtures change byte-identically). Instead tests capture the SHA after install and assert against the captured value.
- **Cross-source detection test.** Fixture: sourceA registry has kit `foo`; sourceB registry has kit `foo` with the same tree SHA (achieved by `cp -r` then `git add` + `git commit`). Install from sourceA; delete `foo` from sourceA's registry + commit; `inat sync` should report case (c) with `inat replace company/foo vendor/foo`.
- **Replace rollback test.** Inject install failure by configuring the new kit's `kit.json` with a derived name that collides with a third installed kit. The `CheckNameUniqueness` step fails after the remove half completed; the test asserts (a) `cfg.Installed[oldKey]` was restored, (b) on-disk delivered files for oldKey are absent (the spec accepts this as a known limitation), (c) the returned error is `ReplaceInstallFailed`.
- **Drift-during-replace test.** Stage a project with delivered files; locally modify one; run `inat replace` without `--force`. Assert the remove half short-circuits with a typed drift error before the install half runs (replace inherits the existing `RemoveDeliveredFiles` drift contract). Re-run with `--force`; assert success.
- **Sync returns non-zero on divergence.** Verified via `RunSync()` returning a non-nil typed error in the table-driven test cases for (b), (c), (d).
- **Update partial-state test.** Three-kit project where the middle kit's source becomes unreachable mid-update (test rigs this by removing the cache entry and pointing `URL` at a non-existent local path between fetches). Update updates the first kit, fails the second, updates the third, returns `KitsNeedAttention(1)` with the second kit named in the report.
- **No live network in any test.** Same constraint as spec 0015 §7.
- **No goroutines in any new test.** Confirmed by `grep` on the new files.

---

### 8. Documentation

- **`README.md`** — new "Reorganization" subsection under §Usage (between §Update and §Status), with one paragraph on each verb plus a short example showing the `sync → rename` flow on an in-source git mv.
- **`CHANGELOG.md`** — `[Unreleased]` gains `### Added`: "`inat sync`, `inat rename`, `inat replace` for kit reorganization workflows. Spec / plan [`0016-kit-reorg-baseline`](docs/specs/0016-kit-reorg-baseline.md)." Plus `### Changed`: "`inat update` (multi-kit form) now skips kits that need attention and continues, returning a non-zero exit with a summary footer pointing at `inat sync`."
- **`docs/specs/0016-kit-reorg-baseline.md`** — this document.
- **`docs/plan/0016-kit-reorg-baseline.md`** — companion rollout document.

---

### 9. Out-of-scope follow-ups

- **Spec 0017 — transactional update.** Make multi-kit `update` atomic via fetch / plan / apply phases; fixes the partial-state risk this spec accepts.
- **Spec 0018 — auto-rename detection.** Layer trivial-rename auto-application onto the transactional update from 0017; eliminates the manual `sync → rename` step for the dominant pure-`git mv` case. **Closed**: see spec [0018](0018-kit-reorg-auto-rename.md).
- **`sync --apply` shortcut.** Punted on §3 grounds; revisit if multi-kit reorganization becomes routine.
- **Header-rewrite during rename.** ~~Always-rule managed-section headers embed `source/kit/filename`; today they go stale after a rename until the next `update` re-delivers. Adding header rewriting to `rename` is a small follow-up if the staleness becomes user-visible.~~ Closed by spec [0019](0019-rename-header-rewrite.md).
- **`inat history`.** Audit trail of past rename / replace operations; would record the pre-rename `installKey` somewhere on the new entry. Not motivated yet.
- **Stable IDs / UUIDs.** Considered and rejected in design; revisit only if the strict tree-SHA classifier in `sync` proves insufficient in practice.
- **Cross-source detection by content similarity.** Currently strict tree-SHA match only; fuzzy matching could surface "this kit is 80% similar to one in another source" but the false-positive risk is high enough that explicit user action is preferable.
