## inat — Transactional update (Spec 0017)

---

### 1. Overview

Spec [0016](0016-kit-reorg-baseline.md) shipped the kit-reorganization baseline: `sync`, `rename`, `replace`, and a skip-and-continue failure mode for multi-kit `inat update`. The skip-and-continue change made `update` resilient to kit-level errors — one failing kit no longer aborts the others — but it left the **within-a-kit** partial-state window open: a kit whose old delivered files were already removed but whose new delivered files failed mid-loop is left with the old `Installed` entry pointing at files that are no longer on disk. The user's recovery path is `inat refresh`, which works but is documented in [`0016-kit-reorg-baseline.md`](0016-kit-reorg-baseline.md) §3 as a known limitation.

This spec closes that window for the failure modes that today's interleaved-per-kit pipeline exposes — network errors, parse errors, name-collision errors, registry-missing-kit errors — by restructuring the multi-kit (and single-kit) `inat update` pipeline into two explicit phases:

- **Plan** — for every targeted kit, fetch the source repo, parse the registry, fetch the new kit tree, walk + parse + derive names, and check name uniqueness across the **post-update** name set of the entire batch. No disk writes. The output is a list of `PlannedAction` values keyed by install-key.
- **Apply** — for every kit whose plan succeeded, run the existing remove-old + deliver-new sequence. Apply walks the plan output sequentially; per-kit apply errors do not stop other kits.

After this spec ships, the only failure modes that can leave a kit half-applied are **disk-write failures during the apply phase** (ENOSPC, permission-denied, partial deliver). Every pre-disk-write failure mode is converted from "partial state" to "kit was skipped, nothing on disk changed." The disk-write residual risk is acknowledged as a non-goal in §3 — eliminating it requires an on-disk snapshot/restore primitive that this spec deliberately does not build.

A companion implementation plan lives at [`docs/plan/0017-transactional-update.md`](../plan/0017-transactional-update.md).

This spec extends [`docs/specs/0001-initial-idea.md`](0001-initial-idea.md) (the kit lifecycle contract). It does not change the on-disk schema (`Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry` are unchanged), it does not change agent profiles, and it does not change the release pipeline.

---

### 2. Goals

1. A new package `go/cli/transaction/` MUST host two entry points and one data type:
   - `Plan(dir string, cfg *config.Config, targets []Target, homeOverride string) ([]PlannedAction, error)` — pure plan phase. Performs network + parse + name-uniqueness work for every target. Returns one `PlannedAction` per target. Side effects: cache is touched (via `source.CloneOrFetch`); `cfg` is **not** mutated; nothing under `dir` is written.
   - `Apply(dir string, cfg *config.Config, plan []PlannedAction, force bool, homeOverride string) []ApplyResult` — pure apply phase. For every actionable plan entry, runs the existing remove + deliver sequence and mutates `cfg.Installed` in memory. Returns one `ApplyResult` per plan entry. Per-kit apply errors are captured into the result, not returned; the loop continues to the next entry.
   - `PlannedAction` — sum type over `{UpToDate, Update, Replace, NeedsAttention}`. Variant fields carry the data the apply phase needs (new tree SHA, parsed kit, file blobs, derived names, source-change for replace, error for needs-attention).
2. `inat update` (both targeted and multi-kit forms) MUST be reimplemented as `Plan → Apply → Save` against `transaction.Plan` and `transaction.Apply`. Targeted `inat update <src/kit>` MUST keep its existing fail-fast contract: a plan-phase failure for the single target returns immediately with the typed error byte-identically to today; an apply-phase failure for the single target does the same.
3. `inat replace` (existing command) MUST be reimplemented as `Plan(targets=[Target{Replace, oldKey, newRef}]) → Apply` against the same primitive. The `Replace` plan-action variant MUST encode the source-change (oldSrc != newSrc allowed; oldSrc == newSrc allowed). Replace's `--force` flag continues to govern the remove-old half during apply. The on-disk-files-not-restored limitation from [spec 0016](0016-kit-reorg-baseline.md) §3 carries forward unchanged — Q2 of the design grilling deliberately did not add an on-disk snapshot.
4. The plan phase's name-uniqueness check MUST validate the **post-update name set across the entire batch**, not per-kit against `cfg.Installed`. Concretely: compute, for each planned `Update`/`Replace`, the kit's new derived-name set; for every other planned `Update`/`Replace` in the batch, also use the new name set; for every kit not in the batch (or planned as `UpToDate`), use the recorded `cfg.Installed[key].Names`; then enforce uniqueness across the union. This change closes [spec 0016](0016-kit-reorg-baseline.md) §1 case (c) — a skill that migrates from kitA to kitB in the same batch passes uniqueness because the donating kit's plan removes the name from the post-update set in the same batch the receiving kit adds it.
5. Failures during the plan phase MUST result in a `NeedsAttention` `PlannedAction` for the affected kit, not a returned error from `transaction.Plan`. The plan phase MUST run to completion across all targets even if some fail. Skip-and-continue is the contract at both phases.
6. Failures during the apply phase MUST result in an `ApplyResult` with the kit's status set to `needs-attention` and the typed error captured. The apply phase MUST run to completion; per-kit failures do not stop other kits.
7. After Apply returns, the caller (the cobra command) MUST save `cfg` exactly once — same as today's behaviour. The save captures whatever applied cleanly; kits that errored leave their pre-call `cfg.Installed` entry unchanged. There is no per-kit save during the apply loop.
8. Reporting to stderr MUST follow a phased structure visible to the user:
   - During fetch (inside `Plan`): one progress line per source — `fetching <source> ...`
   - After plan completes: a one-line plan summary — `planned: N update, M up-to-date, K need attention`
   - During apply: one progress line per applied kit — `updated <key>` / `replaced <oldKey> → <newKey>` / etc.
   - After apply completes: per-kit error lines for needs-attention outcomes followed by a final summary — `<U> updated, <S> up-to-date, <N> need attention` and (if `N > 0`) the spec-0016 footer pointing at `inat sync`.
9. The cobra command surface MUST stay byte-identical: `inat update`, `inat update <src/kit>`, `inat update --force`, `inat replace <oldRef> <newRef> [--force]`. No new flags, no new positional arguments. The exit codes from spec 0016 (`KitsNeedAttention(N)` for multi-kit failures, typed error pass-through for single-kit) are unchanged.
10. `inat refresh` is **not** modified by this spec. Refresh remains the documented recovery path for the residual partial-state risk that the apply phase still exposes (disk-write failures). Any change to refresh would be a separate spec.
11. No on-disk schema change. `Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry`, the `~/.inat/config.json` shape, the cache layout, and the agent-profile JSON all remain byte-stable. Configs written before / after this spec round-trip identically.
12. No new module dependencies. The implementation stays within the allowlist (`spf13/cobra`, `go-git/go-git/v5`, standard library) per CLAUDE.md §8.
13. No goroutines and no asynchronous code paths. All I/O is sequential per CLAUDE.md §11.

---

### 3. Non-goals

- **Eliminating the disk-write partial-state window.** When `RemoveDeliveredFiles` or `DeliverToAgent` fails mid-write during the apply phase, the affected kit is left half-applied: some delivered files removed but new ones not yet on disk; or some agent's managed-section was cleaned but the new section's bullets were not yet written. The plan/apply restructure shrinks the failure window from "anywhere in the pipeline" to "during the apply phase's disk writes." Closing it completely requires an on-disk snapshot/restore primitive — the failing kit would need its old files captured before remove and replayed on apply failure. The design grilling for this spec deliberately rejected that primitive (Q2): the cost is real (memory snapshot per kit, replay path that needs its own tests, parity with replace's failure mode) and the residual risk is tolerable because (a) `inat refresh` is the documented recovery path and (b) plan-phase failure rates dominate disk-failure rates in practice. A future spec may revisit if disk failures during apply become user-visible.
- **Managed-section atomicity within a kit's apply.** A kit's apply phase calls `CleanManagedSections` (removes the kit's existing managed-section block from each agent file) followed by `DeliverToAgent` per active agent (which re-adds the block with the new content). If the deliver step fails after clean succeeded, the agent file has the kit's old managed-section block removed but the new block not yet added. This is the same residual-disk-failure class as the bullet above; same recovery path; same future-spec scope.
- **Replace's install-half-failure leaves old files removed on disk.** Inherited from spec 0016 §3. The plan/apply restructure refactors replace through the same primitive, but without an on-disk snapshot the failure mode is structurally identical: if the apply phase's install half fails after the remove half succeeded, the old kit's files are gone. The `cfg.Installed[oldKey]` snapshot still restores config coherence (same as today); the user still recovers via `inat refresh`. Replace's `ReplaceInstallFailed` typed error continues to point at this recovery path.
- **Batch-wide atomicity.** Either-all-or-none batch update is **not** the contract. Per-kit atomic with skip-and-continue at the batch level is the contract: kit A succeeds, kit B fails+stops at its kit boundary, kit C succeeds independently. A flaky source affecting one kit does not block routine refresh of unrelated kits. Batch-wide rollback would require undoing successful applies (re-delivering the *previous* tree of every successful kit) and would amplify the failure surface; rejected during design.
- **Parallelism.** All plan and apply work is strictly sequential per CLAUDE.md §11. Network fetches are reused within a batch (each source is `CloneOrFetch`-ed exactly once even if multiple kits target it) but the loop itself is single-threaded. Goroutines are out of scope.
- **Plan serialization or `--dry-run`.** The plan output lives in memory between Plan and Apply. There is no JSON/text serialization, no `inat update --plan` flag, no on-disk plan artefact. Adding a dry-run mode is a viable future spec but is not part of this contract. **Update**: the human-readable `--plan` half is closed by spec [0020](0020-update-plan-dry-run.md); JSON output remains deferred.
- **Auto-applying detected renames during update.** Spec [0018](0018-kit-reorg-auto-rename.md) layers a `Rename` action variant onto the plan output and teaches `Plan` to detect tree-SHA matches at different names in the source registry. This spec deliberately does not implement that detection — it ships the substrate (the explicit `Action` enum, the plan/apply boundary, the global uniqueness check) so 0018 is a localized addition rather than a structural change. **Update**: closed by spec [0018](0018-kit-reorg-auto-rename.md).
- **Refactoring `inat refresh`, `inat status`, `inat install`, `inat remove`, `inat sync`, `inat rename` through the new primitive.** Only `update` and `replace` are migrated in this spec. The other commands have either single-kit semantics that don't benefit from batch planning (`install`, `remove`, `rename`) or read-only contracts (`status`, `sync`) that don't touch disk. Wider migration is out of scope.
- **Changes to `inat refresh`.** Refresh's drift contract (re-deliver from cache, skip locally-modified files without `--force`) is unchanged. Refresh remains the documented recovery path for the residual disk-write risk.
- **Changes to error message wording for the existing typed errors.** The new package emits the existing `KitsNeedAttention`, `KitNotFound`, `KitNotInstalled`, `RenameTreeMismatch`, etc. variants byte-identically to today. No new error variants are needed; the structural change is in *when* the error is constructed, not in what it says.

---

### 4. Design

#### 4.1 The `transaction` package

```
go/cli/transaction/
├── transaction.go      // Plan, Apply, top-level types
├── plan.go             // per-target plan-phase helpers (fetch, parse, derive)
├── apply.go            // per-action apply-phase helpers (remove + deliver)
├── action.go           // PlannedAction enum + Target input type
└── transaction_test.go // unit tests for plan + apply phases
```

#### 4.2 Input types

```go
// Target is the input to Plan: which kit to operate on, and what kind
// of operation. The caller (cli/update or cli/replace) constructs a
// slice of Targets and hands it to Plan.
type Target struct {
    Kind     TargetKind
    InstallKey string  // "<src>/<kit>" — required for both kinds
    NewRef     string  // Replace only: "<newSrc>/<newKit>"; ignored otherwise
}

type TargetKind int
const (
    TargetUpdate TargetKind = iota  // refresh kit at its current install key
    TargetReplace                   // remove InstallKey + install NewRef
)
```

`update` constructs targets with `Kind=TargetUpdate` and `InstallKey` set; `NewRef` is empty. `replace` constructs a single target with `Kind=TargetReplace`, `InstallKey=oldKey`, `NewRef=newRef`.

#### 4.3 Output types

```go
// ActionKind identifies the variant of a PlannedAction.
type ActionKind int
const (
    ActionUpToDate ActionKind = iota
    ActionUpdate
    ActionReplace
    ActionNeedsAttention
)

// PlannedAction is the per-target output of Plan. Variant fields are
// populated based on Kind; the apply phase switches on Kind.
type PlannedAction struct {
    Kind        ActionKind
    InstallKey  string             // identifies the kit; always set
    NewKey      string             // ActionReplace only (oldKey is InstallKey)

    // ActionUpdate / ActionReplace plan data (populated by Plan):
    NewTreeSHA  string
    NewKitCfg   *kit.KitConfig     // pointer so zero-value omits cleanly
    NewFiles    []source.FileBlob
    NewNames    []string

    // ActionNeedsAttention only:
    Err         error
}

// ApplyResult is the per-action output of Apply.
type ApplyResult struct {
    InstallKey  string
    Kind        ActionKind  // ActionUpToDate | ActionUpdate | ActionReplace | ActionNeedsAttention
    Err         error       // non-nil iff Kind == ActionNeedsAttention
}
```

#### 4.4 `Plan` pipeline

Strictly sequential. Each step ≤30 lines per CLAUDE.md §4.

1. **Group targets by source.** For each `Target`, derive the source name(s) it touches (one for `Update`, two for `Replace`). Build a dedupe set of source names that need fetching.
2. **Fetch every needed source.** For each source name in the set: emit `fetching <source>...` to stderr; call `source.CloneOrFetch(src.URL, cachePath)`; call `source.FetchRegistryAt(repo, subpath)`. Cache the (repo, registry) pair under the source name. A fetch failure for a source is **not** a hard error — it converts every `Target` from that source into a `NeedsAttention` action for the rest of the pipeline.
3. **Per-target plan.** For each `Target`:
   - Look up the source's (repo, registry). If the source failed to fetch → emit `NeedsAttention`.
   - For `TargetUpdate`: validate kit-in-registry; fetch kit tree; if `treeSHA == cfg.Installed[InstallKey].Tree`, emit `UpToDate`. Otherwise walk + parse + derive names; emit `Update` with the new tree data.
   - For `TargetReplace`: validate that `cfg.Installed[InstallKey]` exists and `cfg.Installed[NewKey]` does NOT exist; validate kit-in-registry for NewRef's source; fetch the new kit tree; walk + parse + derive names; emit `Replace` with the new tree data and the old key.
   - Any error in this per-target step → `NeedsAttention` with the typed error captured.
4. **Global post-update name-uniqueness check.** Build the post-update name set: for every `cfg.Installed[k]`, take `cfg.Installed[k].Names` unless the kit is in the plan as `Update`/`Replace`, in which case take the planned `NewNames`. For each `Update` / `Replace` action, check that its `NewNames` are unique against the rest of the post-update name set (excluding itself). Collisions convert that action into `NeedsAttention(DuplicateName)`. Reserved-name checks (`inat-usage`, `inat-kits`) re-run against the planned names; they were already checked at the kit level by `kit.DeriveNames`, but the post-update batch view catches a collision the per-kit view cannot see.
5. **Return.** Plan emits one `PlannedAction` per input `Target`, in input order. The function never returns a non-nil `error` — fatal failures are encoded as `NeedsAttention` actions. (The signature returns `error` for symmetry with the rest of the codebase but in practice it is always nil. Guarded by tests.)

After `Plan` returns, the caller emits the plan summary line: `planned: N update, M up-to-date, K need attention` (a `Replace` is counted under `update` for the summary). Plan-phase `NeedsAttention` errors are printed to stderr immediately after the summary, one per kit, with the kit's typed error message — same format as today's skip-and-continue summary.

#### 4.5 `Apply` pipeline

Strictly sequential. Per `PlannedAction`:

1. **`UpToDate`** → emit nothing to stderr (the plan summary already counts it); return `ApplyResult{Kind: ActionUpToDate}`.
2. **`NeedsAttention`** → already reported by the plan summary; return the captured error in `ApplyResult{Kind: ActionNeedsAttention, Err: action.Err}`.
3. **`Update`** → run the existing per-kit apply pipeline:
   - `removecmd.RemoveDeliveredFiles(dir, cfg.Installed[InstallKey].Delivered, force)`
   - `removecmd.CleanManagedSections(dir, activeAgents, InstallKey, refPaths)`
   - For each active agent: `install.DeliverToAgent(dir, &profile.Mapping, NewKitCfg, NewFiles, src, kit)`
   - Mutate `cfg.Installed[InstallKey] = config.InstalledKit{Tree, Names, Delivered}` with the new values.
   - Emit `updated <key>` to stderr.
   - Any error → `ApplyResult{Kind: ActionNeedsAttention, Err: ...}`. The kit is left half-applied per §3; subsequent actions still run.
4. **`Replace`** → similar to Update but the install key changes:
   - `removecmd.RemoveDeliveredFiles(dir, cfg.Installed[oldKey].Delivered, force)` against the OLD key.
   - `removecmd.CleanManagedSections(dir, activeAgents, oldKey, refPaths)` against the OLD key's refPaths.
   - `delete(cfg.Installed, oldKey)`.
   - For each active agent: `install.DeliverToAgent(...)` with the new kit data — same as Update but the destination install-key is `NewKey`.
   - Mutate `cfg.Installed[NewKey] = ...` with the new values.
   - Emit `replaced <oldKey> → <newKey>` to stderr.
   - Any error during the install half → `ApplyResult{Kind: ActionNeedsAttention, Err: ReplaceInstallFailed(...)}`. The OLD key's `cfg.Installed` entry is **not** restored from a snapshot; the per-spec-0016 limitation carries forward.

Apply runs to completion regardless of per-action errors. The caller (the cobra command) is responsible for the post-loop work: per-action error lines, final summary, `KitsNeedAttention(N)` if any action needs attention, and `config.SaveConfig` exactly once.

#### 4.6 `cli/update` after this spec

```go
func Update(dir, kitRef string, force bool, homeOverride string) error {
    cfg, err := config.LoadConfig(dir)
    if err != nil { return err }

    targets, err := buildTargets(kitRef, &cfg)
    if err != nil { return err }

    plan, _ := transaction.Plan(dir, &cfg, targets, homeOverride)
    printPlanSummary(plan)
    printPlanErrors(plan)  // NeedsAttention from plan phase

    results := transaction.Apply(dir, &cfg, plan, force, homeOverride)
    printApplyErrors(results)

    if anyApplied(results) {
        if err := config.SaveConfig(dir, &cfg); err != nil { return err }
    }
    printFinalSummary(plan, results)

    if kitRef != "" {
        // Single-kit fail-fast: surface the underlying typed error.
        return firstNeedsAttentionError(plan, results)
    }
    n := countNeedsAttention(plan, results)
    if n > 0 { return errs.NewKitsNeedAttention(n) }
    return nil
}
```

`buildTargets` is the only branch on `kitRef`: empty → one `Target` per kit in `cfg.Installed`; non-empty → one `Target` for the resolved key. Single-kit fail-fast is preserved by the `firstNeedsAttentionError` shortcut at the bottom.

#### 4.7 `cli/replace` after this spec

```go
func Replace(dir, oldRef, newRef string, force bool, homeOverride string) error {
    cfg, err := config.LoadConfig(dir)
    if err != nil { return err }

    oldKey, newKey, err := parseAndValidate(&cfg, oldRef, newRef)
    if err != nil { return err }

    targets := []transaction.Target{{
        Kind: transaction.TargetReplace,
        InstallKey: oldKey,
        NewRef: newRef,
    }}

    plan, _ := transaction.Plan(dir, &cfg, targets, homeOverride)
    if a := plan[0]; a.Kind == transaction.ActionNeedsAttention {
        return a.Err
    }

    results := transaction.Apply(dir, &cfg, plan, force, homeOverride)
    if r := results[0]; r.Kind == transaction.ActionNeedsAttention {
        return r.Err
    }

    if err := config.SaveConfig(dir, &cfg); err != nil { return err }
    fmt.Fprintf(os.Stderr, "replaced %s → %s\n", oldKey, newKey)
    return nil
}
```

The existing `parseAndValidate` (loaded refs in `source/kit` form, oldKey installed, newKey not installed) stays unchanged. The snapshot/restore code that was in spec 0016's replace is **removed** — the new primitive doesn't need it because plan-phase failures never leave config dirty, and apply-phase failures emit a typed error without partial-config-restoration. Replace becomes ~30 lines, almost all wiring.

The `--force` semantics: `force=true` is passed straight through to `transaction.Apply`, which forwards it to `RemoveDeliveredFiles` per kit. Drift on the old kit's files behaves identically to today.

The `ReplaceInstallFailed` typed error is constructed inside the apply phase when the install half of a `Replace` action fails. The wrapped cause is the underlying error (e.g. `KitValidation`, `IO`).

#### 4.8 The `rollbackOnDrift` removal

The current replace.go has a `rollbackOnDrift` helper that handles "remove half short-circuited because of drift; restore config snapshot." Under the new primitive, drift on the old kit's files surfaces as an error *inside* `RemoveDeliveredFiles` (when `force=false` and a file's recorded hash mismatches its on-disk hash). That error propagates up as a `NeedsAttention` action. The caller's existing single-kit-replace error path returns it directly; no separate snapshot/restore is needed.

#### 4.9 No new typed errors

Every error this spec emits already exists: `KitNotFound`, `KitNotInstalled`, `KitAlreadyInstalled`, `SourceNotFound`, `KitValidation`, `RegistryValidation`, `DuplicateName`, `ReservedName`, `Git`, `IO`, `JSON`, `KitsNeedAttention`, `ReplaceInstallFailed`. The structural change is *when* and *how* these are constructed, not the catalogue. The `errs` package gets no new variants.

#### 4.10 File layout

New files:

```
go/cli/transaction/transaction.go      (~80 lines)   Plan, Apply, package doc
go/cli/transaction/action.go           (~60 lines)   ActionKind, PlannedAction, Target, TargetKind
go/cli/transaction/plan.go             (~200 lines)  fetch + per-target plan helpers + uniqueness check
go/cli/transaction/apply.go            (~150 lines)  per-action apply helpers
go/cli/transaction/transaction_test.go (~400 lines)  table-driven plan + apply tests
```

Modified:

```
go/cli/update/update.go      Reimplemented on transaction.Plan/Apply (~120 lines after, was 241).
go/cli/replace/replace.go    Reimplemented on transaction.Plan/Apply (~80 lines after, was 181).
go/cli/update/update_test.go Existing assertions updated to match new stderr format; behavioural tests preserved.
go/cli/replace/replace_test.go Existing assertions updated similarly; rollback tests adjusted (snapshot path removed; error class unchanged).
```

Untouched:

```
go/cli/install/install.go    InstallSteps stays the entry point for install + transaction.Apply's deliver helpers.
go/cli/remove/remove.go      RemoveSteps unchanged; RemoveDeliveredFiles + CleanManagedSections unchanged.
go/cli/refresh/, go/cli/sync/, go/cli/rename/, go/cli/init/, go/cli/agents/, go/cli/sources/, go/cli/status/, go/cli/validate/, go/cli/self/    All unchanged.
go/core/*                    All pure-logic packages unchanged.
go/git/source/, go/git/cache/, go/agents/    Unchanged.
go/errs/errs.go              No new variants; existing constructors reused.
go/main.go                   No new commands; cobra wiring unchanged.
go/go.mod, go/go.sum         No new modules.
```

#### 4.11 Why the plan/apply primitive lives at `cli/transaction/`

Two readings were considered during design:

- **Inside `cli/update/`** — replace would import from `cli/update`. Backwards-feeling dependency direction; replace today imports from `cli/install` + `cli/remove`, not from `cli/update`.
- **New top-level `go/transaction/`** — peer to `cli/`, `core/`, `git/`, `agents/`. Stronger separation but introduces a new architectural layer for one consumer pair. Existing layering is `cli/` for command-driven I/O, `core/` for pure logic, `git/` for git plumbing, `agents/` for profiles; the plan/apply primitive does I/O so it belongs under `cli/`, but it's not itself a command.

`cli/transaction/` is the chosen home: it lives in the same dependency layer as its consumers (`cli/update`, `cli/replace`), it's named for the operation it represents (a transactional kit operation across one or more kits), and the package boundary is explicit so 0018's `Rename` variant lands as a localized addition without forcing a layout change.

---

### 5. Compatibility

- **`go/cli/update/update.go`** — `RunUpdate` and `Update` keep their entry-point signatures. The internal pipeline restructure is a behavioural change in failure semantics (network/parse/uniqueness errors no longer leave kits half-applied), and a stderr format change (one-line plan summary added between fetch progress and apply progress); both are documented in §8.
- **`go/cli/replace/replace.go`** — `RunReplace` and `Replace` keep their entry-point signatures. The snapshot+restore code path is removed; functional behaviour for the snapshot's-only-customer (apply-half failure) is unchanged because the underlying failure mode is the same. Tests verify byte-identical typed-error returns for the existing failure scenarios.
- **`.inat/config.json`** — schema unchanged. Configs written before / after this spec round-trip identically.
- **`~/.inat/config.json`** — schema unchanged.
- **`~/.inat/cache/`** — layout unchanged. The plan phase reuses `source.CloneOrFetch`, which itself is unchanged.
- **Agent profiles** — unchanged.
- **Built-in rules** — unchanged. `inat-usage` and `inat-kits` reservation enforcement now runs inside the plan phase's global uniqueness check rather than per-kit, but the user-visible error is byte-identical.
- **`go/go.mod` / `go/go.sum`** — byte-identical pre/post change. No new modules.
- **Existing CLI commands** — unchanged surface. No new commands, no new flags, no new positional arguments.
- **`go/main.go`** — no change. The transaction package is internal infrastructure; users do not address it directly.
- **`README.md`** — no new section is required. The `update` and `replace` paragraphs already describe the user-visible behaviour, and the plan/apply restructure does not change that surface. A note may be added under §Update describing the plan summary line, but it is not contractually required.
- **`CHANGELOG.md`** — `[Unreleased]` gains a `### Changed` entry summarizing the plan/apply restructure and its closure of the within-kit partial-state risk.
- **Pre-commit hook + CI** — unchanged. The hook runs `gofmt`, `go vet`, `staticcheck`, `go test ./...`; the new `cli/transaction/` package is covered by all four. The spec-presence check is satisfied by this spec/plan pair.

---

### 6. Acceptance criteria

1. **Build clean.** `go -C go/ build ./...` exits 0; `inat update --help` and `inat replace --help` are byte-identical to pre-change output.
2. **Single-kit update fail-fast preserved.** `inat update bogus/kit` returns `KitNotInstalled` byte-identically to spec 0016. `inat update src/kit` where `src` is unreachable returns the underlying network error directly. The cobra exit code is unchanged.
3. **Multi-kit update plan summary visible.** Three-kit project; run `inat update`. Stderr contains, in order: `fetching <src1>...` `fetching <src2>...` (one per source touched), then `planned: 2 update, 1 up-to-date, 0 need attention`, then `updated <kitA>` `updated <kitB>`, then the final summary `2 updated, 1 up-to-date, 0 need attention`. No `KitsNeedAttention` exit on this clean path.
4. **Plan-phase failure isolated.** Three-kit project where kit B's source is unreachable. Stderr: fetch progress per source (with the failing source emitting an error line), `planned: 2 update, 0 up-to-date, 1 need attention`, the kit-B error line, apply progress for A and C, final summary `2 updated, 0 up-to-date, 1 need attention`. `cfg.Installed` reflects A and C's new state and B's unchanged state. Exit non-zero with `KitsNeedAttention(1)`. **No on-disk delivered file for kit B has been touched.** This is the central guarantee 0017 adds.
5. **Apply-phase failure leaves other kits intact.** Three-kit project; inject a disk-write failure mid-apply for kit B (e.g. read-only target directory for kit B's contextual rule). Stderr: plan summary `planned: 3 update`, apply progress `updated <A>`, kit-B error line, `updated <C>`, final summary `2 updated, 0 up-to-date, 1 need attention`. `cfg.Installed[A]` and `cfg.Installed[C]` reflect new state; `cfg.Installed[B]` reflects pre-call state (the delivered files for kit B may be partial — the residual disk-write risk per §3 — but its config entry is coherent with the pre-call state).
6. **Cross-kit name migration succeeds.** Two-kit project where the upstream change moves skill `foo` from kitA to kitB. Today (spec 0016) this fails with `DuplicateName` because the loop's per-kit uniqueness check sees the post-A-removed-but-pre-B-added state in iteration order. Under 0017, `inat update` succeeds: the global post-update name-uniqueness check sees that the post-update set has `foo` exactly once. `cfg.Installed[A].Names` no longer contains `foo`; `cfg.Installed[B].Names` does. Both kits' delivered files are updated.
7. **Replace happy path.** Two configured fake sources with kits `company/foo` and `vendor/foo` (different content). Install `company/foo`. Run `inat replace company/foo vendor/foo`. Stderr: fetch progress, `planned: 1 update, 0 up-to-date, 0 need attention`, `replaced company/foo → vendor/foo`, no needs-attention. Result: `cfg.Installed[vendor/foo]` exists, `cfg.Installed[company/foo]` deleted, agent target's managed sections cleaned of `company/foo` and re-populated for `vendor/foo`.
8. **Replace honours `--force`.** Locally modify a delivered file from `company/foo`; run `inat replace company/foo vendor/foo` (no `--force`): the modified file is detected as drift inside `RemoveDeliveredFiles`, the apply step fails, the typed error returned is the existing drift class (`KitsNeedAttention(1)` or equivalent — same wording as today's replace). Re-run with `--force`: succeeds.
9. **Replace rolls back appropriately on apply-half failure.** Synthesize an install-half failure (e.g. the new kit's derived names collide with a third installed kit). Replace's apply phase emits `ReplaceInstallFailed` for the action; the caller returns it byte-identically to today. **Note**: per §3, on-disk files for the old kit are gone (no on-disk snapshot). The user's recovery is `inat refresh`. The test asserts the typed error class is unchanged from spec 0016; the spec 0016 snapshot of `cfg.Installed[oldKey]` is no longer needed because `Plan` did not mutate config and `Apply` mutates config only on success — `cfg.Installed[oldKey]` is still present for the caller to leave alone, plus `delete(cfg.Installed, oldKey)` is rolled back implicitly by the early-return without `SaveConfig`.

   Wait — this is subtle. In the `Replace` apply path, the remove half does `delete(cfg.Installed, oldKey)` *before* the install half attempts to `cfg.Installed[newKey] = ...`. If the install half fails, the in-memory cfg has neither `oldKey` nor `newKey`. But because `SaveConfig` is gated on `r.Kind != ActionNeedsAttention` for replace, the config file on disk is **not** rewritten — the unchanged disk config still has `oldKey`. From the user's perspective, after a failed replace, `inat status` (which reads the disk config) shows `oldKey` present, exactly as before the call. This is the same end-state behaviour as spec 0016's snapshot path, achieved without an in-memory snapshot.
10. **No on-disk schema change.** Diff `.inat/config.json` byte-by-byte before/after a clean update run, before/after a clean replace run, before/after a needs-attention run: only the keys the spec mutates change; no new fields, no reordering.
11. **No new module dependencies.** `git diff origin/main..HEAD -- go/go.mod go/go.sum` is empty.
12. **No `unsafe` package, no goroutines.** `grep -rn "unsafe\|go func\|go " go/cli/transaction/` returns nothing; `staticcheck` and `go vet` pass.
13. **File / function size limits.** Every new file under 300 lines; every new function under 50 lines (target 30).
14. **Pre-commit hook clean.** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all exit 0.

---

### 7. Testing notes

- **Fake sources via local bare git repos.** Same harness as spec 0016: `t.TempDir()`-backed bare repos with hand-rolled `kit.json` + `registry.json`, configured via `cfg.Sources[].URL = file://...`. No live network in any test.
- **Plan-phase isolation tests.** Each plan-phase failure mode (source unreachable, kit not in registry, parse error in kit.json, derived-name reserved-collision, derived-name cross-kit-collision) gets a table-driven test that asserts: (a) the affected kit's `PlannedAction.Kind == ActionNeedsAttention` with the expected typed error; (b) `cfg.Installed` is byte-identical to the pre-Plan state; (c) **no file under the project dir has been touched**. The third assertion is the central one — it's what 0017 buys over 0016.
- **Apply-phase failure injection.** Disk-write failures are simulated via filesystem permission tweaks: chmod a target directory to read-only before Apply runs, run Apply, assert the typed error and the survival of other kits' applies. On Windows the test harness uses `os.Chmod` with appropriate-mode bits; the test gates on `runtime.GOOS == "windows"` only where the UNIX permission semantics don't translate.
- **Cross-kit name-migration test.** Two-kit fixture where kitA's new tree drops skill `foo` and kitB's new tree adds skill `foo`. Run `inat update`. Assert: success; both kits updated; final names in `cfg.Installed` reflect the migration. This is the regression test for 0016 §1(c) being closed.
- **Replace rollback test.** Inject install-half failure (third kit's name collides with the new replace target's derived names — this is the same fixture as spec 0016's replace_test.go's rollback test). Assert: the typed error returned is `ReplaceInstallFailed`; the on-disk config file has the OLD key present (not the new); the on-disk delivered files for the old key are gone (per §3 limitation). The previous spec's "snapshot restored cfg.Installed[oldKey]" assertion is replaced with "config save was gated on success" — the disk file was never rewritten, so its `oldKey` entry survives.
- **Single-kit update fail-fast test.** `inat update bogus/kit` and `inat update src/<unreachable>` both return the underlying typed error (not `KitsNeedAttention`). Assert via `errors.As` against the expected error variant.
- **Plan-summary stderr test.** Capture stderr; assert the exact format `planned: %d update, %d up-to-date, %d need attention\n` and the per-kit progress lines.
- **Order-independence test.** Run a multi-kit plan with two different `cfg.Installed` map iteration orders (forced via test harness). Assert the resulting `cfg.Installed` after `Plan + Apply` is identical regardless of order. This catches regressions in the global uniqueness check.
- **No live network in any test.** Same constraint as spec 0015 §7 and 0016 §7.
- **No goroutines in any new test.** Verified by `grep` on the new files.

---

### 8. Documentation

- **`README.md`** — no contractually-required addition; the user-visible CLI surface is unchanged. A short note may be added under §Update describing the plan-summary line as informational output. Not blocking for spec acceptance.
- **`CHANGELOG.md`** — `[Unreleased]` gains a `### Changed` entry:
  > **`inat update` and `inat replace` now run in two explicit phases — plan and apply.** The plan phase fetches every relevant source, validates name uniqueness across the post-update batch, and produces an in-memory plan; the apply phase performs disk writes against that plan. The user-visible CLI surface is unchanged, but the failure semantics improve: a kit whose plan fails (source unreachable, kit deleted upstream, parse error, derived-name collision) is now reported as needs-attention without any of its delivered files being touched. Cross-kit name migration (skill `foo` moving from kitA to kitB in the same update batch) now succeeds where it previously failed. The disk-write residual risk during apply is documented in the spec's non-goals; recovery via `inat refresh` is unchanged. Spec / plan [`0017-transactional-update`](docs/specs/0017-transactional-update.md).
- **`docs/specs/0017-transactional-update.md`** — this document.
- **`docs/plan/0017-transactional-update.md`** — companion rollout document.

---

### 9. Out-of-scope follow-ups

- **Spec 0018 — auto-rename detection.** Layer a `Rename` action variant onto `transaction.PlannedAction`; teach `Plan` to detect tree-SHA matches at different names in the source registry; the apply phase calls `cli/rename` semantics for `Rename` actions. With 0017's substrate in place, 0018 is a localized addition. **Closed**: see spec [0018](0018-kit-reorg-auto-rename.md).
- **On-disk snapshot/restore for the apply phase.** Capture old delivered files' content before remove; replay on apply failure. Closes the disk-write residual risk (and replace's install-half-failure files-gone limitation in one stroke). Cost: memory snapshot per kit, a replay code path with its own tests, parity required between update and replace. **Closed**: see spec [0021](0021-apply-snapshot-restore.md) — snapshot lives on disk under `~/.inat/snapshots/`, captures delivered + agent target files, discarded on success and kept on failure.
- **`inat update --plan` / `inat update --dry-run`.** Run `Plan` and emit a JSON or human-readable plan to stdout without applying. Useful for CI gating ("show me what `update` would do") and for review workflows. Out of scope here. **Update**: human-readable half closed by spec [0020](0020-update-plan-dry-run.md); JSON remains deferred.
- **Migrate the rest of the commands through the transaction primitive.** `install`, `remove`, `rename` are all single-kit operations that don't benefit from batch planning. `refresh` arguably could — the partial-state recovery story would be uniform — but it's a different contract (no fetch, just re-deliver from cache) and is out of scope.
- **Concurrent plan / fetch.** Goroutines for parallel source fetches would speed up multi-source `inat update`. CLAUDE.md §11 forbids it for now; revisit if a future spec justifies it.
