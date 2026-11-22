## inat — Auto-rename detection during update (Spec 0018)

---

### 1. Overview

Spec [0016](0016-kit-reorg-baseline.md) shipped `inat sync` (suggest-only) and the explicit `inat rename` verb for the pure-`git mv` reorganization case. Spec [0017](0017-transactional-update.md) restructured `inat update` and `inat replace` into a plan/apply primitive and explicitly ships the substrate that "0018 is a localized addition rather than a structural change." This spec lands the localized addition: during `inat update`, the plan phase now **auto-detects** that an installed kit was renamed in its source (the recorded `Installed.Tree` SHA appears at a different name in the same source's registry), and the apply phase auto-applies the rename — same semantics as the explicit `inat rename` verb, including the [spec 0019](0019-rename-header-rewrite.md) always-rule header rewrite. The manual `inat sync → inat rename` round-trip is eliminated for the dominant pure-`git mv` case.

Detection is **strict tree-SHA equality only** (same classifier `inat sync` already uses for case (b) in spec 0016 §4.2), **in-source only** (cross-source moves still route through the explicit `inat replace` verb), and **default-on** (no opt-in flag — see §4.6 for rationale). Content-drift cases (rename + content edit in the same upstream commit) are unaffected: they fail tree-SHA equality, do not auto-rename, and surface as today's `KitNotFound` needs-attention with the user-driven `inat replace` workflow available.

A companion implementation plan lives at [`docs/plan/0018-kit-reorg-auto-rename.md`](../plan/0018-kit-reorg-auto-rename.md).

This spec extends [`docs/specs/0001-initial-idea.md`](0001-initial-idea.md) (the kit lifecycle contract) and pays down the "automation gap" called out in [`docs/specs/0016-kit-reorg-baseline.md`](0016-kit-reorg-baseline.md) §3 and [`docs/specs/0017-transactional-update.md`](0017-transactional-update.md) §3. It does not change the on-disk schema (`Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry` are unchanged), it does not change agent profiles, and it does not change the release pipeline.

---

### 2. Goals

1. A new `ActionKind` variant `ActionRename` MUST be added to `go/cli/transaction/action.go`. The variant's invariants: `InstallKey` is the pre-rename key, `NewKey` is the post-rename key (same source, different kit name), `NewTreeSHA` equals `cfg.Installed[InstallKey].Tree` (unchanged — that is the whole point of auto-rename), `NewNames` equals `cfg.Installed[InstallKey].Names` (unchanged for the same reason), `NewKitCfg` and `NewFiles` are nil (apply does not re-deliver). `IsActionable` MUST return `true` for `ActionRename`.
2. `transaction.Plan` MUST detect a `TargetUpdate` whose kit name is absent from its source's current registry but whose recorded `Installed.Tree` SHA appears at exactly one different name in the same source's registry, and MUST emit `ActionRename` for that target. The detection is run only when the kit is absent from its source's registry — every other plan path is unchanged. Detection MUST NOT run for `TargetReplace` (replace is explicit cross-source mutation; no auto-detection layered on top).
3. `transaction.Plan` MUST build the per-source tree-SHA index **lazily** — only when at least one target from that source missed its kit name in the registry. Sources where every target was found at its current name pay zero extra cost (zero additional `FetchKitTreeAt` calls). The index is scoped per `Plan` invocation, not cached across invocations.
4. When the per-source tree-SHA index shows the recorded SHA at **two or more** different names, auto-rename MUST refuse to pick a candidate. The target converts to `ActionNeedsAttention` with a new typed error `RenameAmbiguous(installKey string, candidates []string)`. The user is directed to `inat sync` for diagnosis and `inat rename` for explicit resolution.
5. When the per-source tree-SHA index shows the recorded SHA at **zero** different names, the target converts to `ActionNeedsAttention` with the existing `KitNotFound` typed error — same as today. No behaviour change for this case.
6. `transaction.Apply` MUST handle `ActionRename` by:
   - rewriting the in-memory `cfg.Installed` map key from `InstallKey` to `NewKey` (carrying the existing `InstalledKit{Tree, Names, Delivered}` byte-identically — no field is recomputed);
   - calling the same always-rule header rewrite the explicit `inat rename` command runs today (spec 0019), against every active agent's always-target;
   - emitting `renamed <oldKey> → <newKey>` to stderr (matching the explicit-rename verb's wording).
   On header-rewrite failure, the action converts to `ActionNeedsAttention` with the underlying typed error; `cfg.Installed` is **rolled back in memory** before returning (the same in-memory rollback the explicit `inat rename` already does — failure leaves config coherent because the caller's gated `SaveConfig` will not rewrite the disk file).
7. The post-update name-uniqueness check (`checkBatchUniqueness`) MUST treat `ActionRename` identically to the pre-batch state for the kit's names but at the new key: `keyToNames[oldKey]` is deleted; `keyToNames[newKey] = cfg.Installed[oldKey].Names`. Per `plan.go:74-77`, the self-comparison guard MUST use the post-batch owning key, so the collision pass MUST set `ownKey = a.NewKey` for `ActionRename` (mirroring `ActionReplace`'s arm) — without this, a Rename action would compare its NewNames against its own entry at `NewKey` and self-collide via `DuplicateName`. No new collisions can be introduced by a pure rename (the names are byte-identical), but the batch view correctly catches cross-action conflicts (e.g. an `Update` action elsewhere in the batch that would land a colliding name).
8. Single-kit `inat update <src/old-name>` against a kit that was renamed upstream MUST succeed end-to-end: the user typed the old name, `Plan` detected the in-source rename, `Apply` rewrote the install key, the cobra command returns nil. Single-kit fail-fast (today: a single-kit `update` for a moved kit fails with `KitNotFound`) is replaced with success in this case; `KitNotFound` continues to fire only for the genuine cases (kit deleted upstream, or ambiguous tree-SHA matches).
9. The stderr plan-summary line MUST include the rename count: `planned: N update, R rename, M up-to-date, K need attention`. `R` is printed even when zero (consistent with the existing format's treatment of `N`/`M`/`K`). The per-apply line is `renamed <oldKey> → <newKey>`, distinct from `updated <key>` and `replaced <oldKey> → <newKey>`. The final summary line gains a rename term: `<U> updated, <R> renamed, <S> up-to-date, <N> need attention`.
10. The detection runs **only against `TargetUpdate` plans inside `transaction.Plan`**. The explicit `inat rename` and `inat sync` commands are not refactored to share this code path — `inat rename` keeps its precise single-source contract (already covered by spec 0016 §4.3 and spec 0019), and `inat sync` keeps its broader read-only diagnostic (already covers cross-source case (c) which auto-detection deliberately omits). The auto-detection is one new helper inside the transaction package; no command-surface refactor.
11. The auto-detection is **default-on** for `inat update`. No `--auto-rename` / `--no-auto-rename` flag. The escape hatch (if a user wants to inspect changes before applying) is `inat sync`, which today reports the same case (b) classification read-only. Rationale in §4.6.
12. On-disk schema is unchanged. `Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry`, the `~/.inat/config.json` shape, the cache layout, and the agent-profile JSON all remain byte-stable. Configs written before / after this spec round-trip identically.
13. No new module dependencies. The implementation stays within the allowlist (`spf13/cobra`, `go-git/go-git/v5`, standard library) per CLAUDE.md §8.
14. No goroutines and no asynchronous code paths. All I/O is sequential per CLAUDE.md §11.

---

### 3. Non-goals

- **Default-off / opt-in via flag.** No `--auto-rename` switch. The detection's classifier is strict tree-SHA equality — the same equality test `inat sync` already uses to suggest the same rename today, and the same one `inat rename` enforces before mutating config. There is no semantic gap between "sync's case (b) suggestion" and "update's auto-rename" beyond removing the round-trip; gating the auto-apply behind a flag would re-introduce the manual step this spec exists to eliminate. The escape hatch — for users who want to audit before applying — is `inat sync`, unchanged and still read-only. If the auto-apply behaviour proves surprising in practice, a follow-up spec may introduce `--no-auto-rename`; the substrate added here makes that a one-line change.
- **Cross-source auto-detection.** A kit whose tree SHA appears in *another* configured source (spec 0016 §4.2 case (c)) is left alone by auto-detection. `inat sync` continues to suggest `inat replace src/old otherSrc/new` for these; the user runs the explicit `replace`. Cross-source moves carry implicit trust judgement (a kit's provenance has changed) that the auto-path deliberately does not make on the user's behalf.
- **Fuzzy / similarity matching.** Strict tree-SHA equality only. A kit whose content was edited *during* the rename (rename + edit in the same upstream commit) does not auto-rename — it falls through to today's `KitNotFound` needs-attention. The user-driven `inat replace` workflow handles this case explicitly and is unchanged.
- **Auto-detection during `inat refresh`.** Refresh does not fetch sources; it re-delivers from cache. No upstream-state reading happens, so there is nothing to detect against. Refresh's contract is unchanged.
- **Auto-detection during `inat replace`.** Replace is an explicit mutation with both refs provided by the user. Layering detection on top would conflate diagnosis (sync) with explicit action (replace) — the exact muddle spec 0016 §3 rejected.
- **Multi-kit auto-rename within a single source via "git --follow"-style heuristic.** No, strict tree-SHA equality. If two kits in the same source happen to share a tree SHA (e.g. two empty kits, or a hand-rolled `cp -r` copy), auto-detection refuses to pick (per goal 4) and the user resolves via `inat sync` + `inat rename`.
- **JSON / dry-run plan output.** Still out of scope (deferred from spec 0017 §3). Auto-detection runs inside `transaction.Plan` and produces the same in-memory plan structure; serialization is a separate follow-up.
- **Auto-rename feedback during `inat status`.** Status reads the local config and prints what's installed; it does not fetch sources. Auto-detection is gated on a source fetch (the registry contents drive it), so status is unchanged. Users who want a fetched diagnostic run `inat sync`.
- **Restoring the partial on-disk state when multi-agent header rewrite fails mid-loop.** `RewriteAlwaysHeaders` walks active agents sequentially (per `cli/rename/rename.go:92-101`); the first agent's `CLAUDE.md` is already written to disk before a later agent's rewrite fails. The in-memory `cfg.Installed` rollback restores config coherence with respect to the disk *config file*, but the already-rewritten agent file is left pointing at the new key. This is structurally identical to spec 0017 §3's "managed-section atomicity within a kit's apply" residual risk; the same recovery path applies: `inat refresh` re-delivers headers from cache against the saved (rolled-back) config. The acceptance criteria (§6 criterion 11) cover the in-memory rollback; the disk-side partial state is acknowledged here as inherited from the apply-phase residual-disk risk that the project already accepts.
- **Rewriting contextual-rule references during rename.** Contextual rule paths under `.inat/rules/<entryName>.md` are keyed on the derived entry name, not the install key, so they are untouched by rename — confirmed by spec 0019 §3. Spec 0018 inherits this assumption byte-identically: `applyRename` only invokes the always-rule header rewrite. Contextual references survive auto-rename intact.

---

### 4. Design

#### 4.1 `ActionRename` and the action enum

`go/cli/transaction/action.go` gains one variant:

```go
const (
    ActionUpToDate ActionKind = iota
    ActionUpdate
    ActionReplace
    ActionNeedsAttention
    ActionRename  // NEW
)

func (k ActionKind) String() string {
    switch k {
    case ActionUpToDate:       return "up-to-date"
    case ActionUpdate:          return "update"
    case ActionReplace:         return "replace"
    case ActionNeedsAttention:  return "needs-attention"
    case ActionRename:          return "rename"  // NEW
    default:                    return "unknown"
    }
}
```

`IsActionable` returns `true` for `ActionRename` in addition to `ActionUpdate` and `ActionReplace`. `convertToNeedsAttention` is unchanged — it already clears every variant field, which is the correct behaviour for downgrading a `Rename` to `NeedsAttention`.

A `PlannedAction` of kind `ActionRename` populates:
- `InstallKey` — the pre-rename key (the user's currently-recorded `src/old`).
- `NewKey` — the post-rename key (`src/new`).
- `NewTreeSHA` — equal to `cfg.Installed[InstallKey].Tree` byte-identically. Carried for symmetry with `ActionUpdate` / `ActionReplace` but never actually changes any state.
- `NewNames` — equal to `cfg.Installed[InstallKey].Names`. Carried so `checkBatchUniqueness` can use the same code path for all actionable kinds.
- `NewKitCfg`, `NewFiles` — nil. Apply does not re-deliver files.

#### 4.2 Plan-phase detection

`planUpdate` (in `plan.go`) currently terminates with `KitNotFound` when the kit is absent from the source's registry. This terminator is replaced with a fallback:

```go
if _, ok := reg.Kits[kitName]; !ok {
    candidates, err := indexForSource.candidatesForSHA(installed.Tree)
    if err != nil {
        return needsAttention(target.InstallKey, err)
    }
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
```

`indexForSource` is the lazy per-source tree-SHA index described in §4.3. The detection adds at most one map lookup to the happy `KitNotFound` path; the first miss within a source triggers the registry walk that builds the index.

#### 4.3 The lazy per-source tree-SHA index

A new helper inside the `transaction` package:

```go
type sourceSHAIndex struct {
    repo    *git.Repository
    reg     registry.Registry
    subpath string
    index   map[string][]string  // treeSHA → []kitName
    built   bool
}

func (i *sourceSHAIndex) candidatesForSHA(treeSHA string) ([]string, error) {
    if !i.built {
        if err := i.build(); err != nil {
            return nil, err
        }
    }
    return i.index[treeSHA], nil
}

func (i *sourceSHAIndex) build() error {
    i.index = make(map[string][]string, len(i.reg.Kits))
    for name := range i.reg.Kits {
        h, _, err := source.FetchKitTreeAt(i.repo, i.subpath, name)
        if err != nil {
            return err
        }
        i.index[h.String()] = append(i.index[h.String()], name)
    }
    i.built = true
    return nil
}
```

The `[]string` value (rather than a single name) preserves ambiguity detection: when two kits in the same source share a tree SHA, both names are recorded and the caller (planUpdate) returns `RenameAmbiguous`. Map iteration order is non-deterministic; for the ambiguity-error message, candidates are sorted alphabetically before being included in the error.

`Plan` builds one `sourceSHAIndex` per source name involved in the batch, **lazily**: the index is constructed only when at least one target from that source encounters the `kit-absent-from-registry` branch. A `Plan` over a batch of cleanly-updating kits (every kit still present at its registered name) builds zero indices and pays zero extra tree-SHA fetches — the auto-detection path is on a slow-path branch.

`Plan`'s outer loop now needs to thread the per-source index map through `planSingle` / `planUpdate`. The simplest threading: a `planCtx` struct or an additional `map[string]*sourceSHAIndex` parameter. The implementation should keep `Plan`'s public signature unchanged.

**Index-build error handling.** `build()` calls `source.FetchKitTreeAt` for every kit in the registry. If one of those calls errors (e.g. a malformed kit entry's tree object cannot be opened), the build fails partway through. The contract:

- The error is **localised** to auto-detection only. It MUST NOT poison every target from that source. Concretely: `build()` records the error on the `sourceSHAIndex` and sets `built = true` (so subsequent `candidatesForSHA` calls do not re-attempt the failing kit). `candidatesForSHA(sha)` returns the (possibly partial) index it managed to build, ignoring the build error — callers that need to know the build was incomplete inspect a separate `BuildErr` field for telemetry / future use, but the spec does NOT propagate `BuildErr` to the calling target's `NeedsAttention`. The target sees its current `kit-absent-from-registry` path resolve based on the kits the index did succeed in indexing.
- This means: a corrupt kit in the registry that prevents its own SHA from being indexed will NOT trigger auto-rename even if the recorded `Installed.Tree` matches it. The target falls through to `KitNotFound` exactly as today.
- The kits successfully indexed before the error remain available for auto-rename detection. A single corrupt registry entry does not block rename detection for the rest of the source's kits.
- The partial-build state is set `built = true` so retries within the same `Plan` invocation do not re-execute the failing fetch. A subsequent `Plan` invocation (separate command) rebuilds from scratch.

#### 4.4 Apply-phase semantics

`applyOne` gains a new switch arm:

```go
case ActionRename:
    return applyRename(dir, cfg, a, homeOverride)
```

`applyRename` does:

1. Snapshot `cfg.Installed[oldKey]` (in memory only — for rollback if header rewrite fails). The snapshot is a value-copy of `InstalledKit{Tree, Names, Delivered}`; slice fields are deep-copied so rollback is safe.
2. `cfg.Installed[newKey] = cfg.Installed[oldKey]`.
3. `delete(cfg.Installed, oldKey)`.
4. Call the helper that rewrites always-rule managed-section headers across every active agent's always-target (the same helper `inat rename` uses today; see §4.5).
5. On rewrite failure: restore the snapshot (`delete(cfg.Installed, newKey)`; `cfg.Installed[oldKey] = snapshot`); return `ApplyResult{Kind: ActionNeedsAttention, Err: <wrapped>}`. The caller's gated `SaveConfig` then keeps the disk file unchanged.
6. On success: emit `renamed <oldKey> → <newKey>` to stderr; return `ApplyResult{InstallKey: oldKey, Kind: ActionRename}`.

`applyRename` does **not** call `RemoveDeliveredFiles` and does **not** call `DeliverToAgent`. On-disk delivered files are byte-identical at both keys because the tree SHA is identical — exactly the reason auto-rename is safe to apply.

**`applyRename` signature.** `applyRename(dir, cfg, a, homeOverride)` — drops the `force` parameter that `applyUpdate` / `applyReplace` accept. Rationale: drift on a delivered file is content drift on the user's working copy, which `applyRename` does not touch (no `RemoveDeliveredFiles`, no `DeliverToAgent`); the helper's only disk write is the line-local header rewrite in the agent's always-target, where the concept of "drift override" does not apply. The dispatch in `applyOne` discards `force` for the `ActionRename` arm explicitly: `case ActionRename: return applyRename(dir, cfg, a, homeOverride)`. Reviewers expecting parameter parity across action arms should look for this asymmetry.

**Multi-agent partial-rewrite acknowledgment.** When `RewriteAlwaysHeaders` walks active agents and a later agent's rewrite fails after an earlier agent's already succeeded, the earlier agent's `CLAUDE.md` (or equivalent) is on disk with the new key while the in-memory rollback restores `cfg.Installed[oldKey]`. The caller's gated `SaveConfig` keeps the disk config file unchanged. End state: disk config says `oldKey`; one or more disk agent files say `newKey/...` in their managed-section headers. This is the same residual-disk class spec 0017 §3 already accepts ("managed-section atomicity within a kit's apply"); recovery via `inat refresh` re-delivers headers from cache against the saved config and reconciles the drift. The non-goal bullet in §3 documents this; acceptance criterion 11 in §6 covers the in-memory rollback explicitly; an additional acceptance criterion (§6 criterion 11a) asserts the multi-agent partial-disk state and the documented recovery path.

#### 4.5 Header rewrite reuse

Spec 0019 added a private helper `rewriteAlwaysHeaders(dir, cfg, oldKey, newKey, homeOverride) error` to `go/cli/rename/rename.go`. The transaction package needs the same behaviour. Two options:

- **Export it from `cli/rename`.** Rename the helper to `RewriteAlwaysHeaders` and have `cli/transaction/apply.go` import `cli/rename`. Dependency direction: `cli/transaction → cli/rename`. Clean and one-way (rename does not depend on transaction).
- **Duplicate inside the transaction package.** Mirror the spec 0017 pattern of duplicating `deliverAcrossAgents` to keep cross-package helpers small. ~15 lines of duplication.

The spec prefers **export**: the helper is self-contained, has a clear name, and the call site needs the exact same behaviour byte-for-byte. Duplication invites silent divergence if spec 0019's contract changes (e.g. a future spec extends rename header semantics to cover contextual rules). The dependency direction is sensible — `cli/transaction` is the consumer; `cli/rename` is the canonical home of the rewrite logic.

#### 4.6 Why default-on (no opt-in flag)

The detection's classifier is the same one `inat sync` already uses to surface the same rename suggestion to the user today, and the same one `inat rename` enforces before mutating config. False positives are bounded by tree-SHA equality — a value calculated by `go-git` over the kit's entire directory tree, byte-identical on rename + zero edits. The set of conditions that would auto-rename incorrectly is exactly the set of conditions that would make `inat sync` suggest the wrong rename — and we accepted that risk profile in spec 0016 because the strict equality eliminates it.

The default-on choice has one **deliberately accepted cost**: a CI pipeline today can grep for `inat update`'s non-zero exit (driven by `KitNotFound` when a kit moved upstream) as a signal that "kit moved, action needed." Under 0018 the same multi-kit batch returns `0 updated, 1 renamed, 0 up-to-date, 0 need attention` and exits zero. A script that today reads `KitNotFound` to trigger an alert will go silent. The spec accepts this because:

- The *symptom* the CI script detected (the kit needs reorganization) was already automatable; making the automation default-on is the point of this spec.
- The new plan-summary line + per-apply `renamed <oldKey> → <newKey>` line are structured stderr output that scripts can grep instead of exit codes. A script that wants to alert on auto-renames adds `inat update 2>&1 | grep -q '^renamed '` rather than `inat update || alert`.
- The CHANGELOG entry calls this out as a `### Changed` for scripted consumers (see §8).
- Genuine `KitNotFound` cases (kit deleted upstream, ambiguity, content drift) continue to fire with non-zero exit — scripts that retry on those signals still trigger.

Layering a flag (default-on or default-off) on top of the same classifier would not change the underlying safety: a default-off flag re-introduces the manual sync → rename round-trip this spec exists to eliminate; a default-on flag exists only as an escape hatch for users who want to audit, but `inat sync` is already that escape hatch and is read-only. The flag adds command surface area without changing what auto-apply does.

If the auto-apply behaviour proves surprising in practice — for example, a user setup where upstream kit moves happen on a schedule the user wants to inspect — a follow-up spec may add `--no-auto-rename`. The substrate this spec lands makes that a one-line conditional in `planUpdate`. The default this spec ships is **on**, no flag.

#### 4.7 Reporting changes

The plan summary line gains a rename term:

```
planned: 2 update, 1 rename, 3 up-to-date, 0 need attention
```

The position is between `update` and `up-to-date` (logical: rename is a no-op-on-disk variant of update, less disruptive than up-to-date is informative). Zero counts are still printed for format stability (the existing format prints `0 need attention` when there are no needs-attention actions).

The per-apply progress line for an `ActionRename` is:

```
renamed company/old-name → company/new-name
```

Mirroring spec 0019's `inat rename` output verbatim. Distinct from `updated <key>` and `replaced <oldKey> → <newKey>` so a user grepping stderr for rename activity catches both the explicit verb and the auto-applied case.

The final summary line gains a rename term:

```
2 updated, 1 renamed, 3 up-to-date, 0 need attention
```

Same position rule.

**Note on positional parsing.** Today's summary format is `N update, M up-to-date, K need attention` — three terms. The new format inserts `rename` between `update` and `up-to-date`, becoming four terms. Field positions shift: today's field-2 was the update count; under 0018, field-2 is still update but field-4 is up-to-date (was field-3) and field-6 is need-attention (was field-5). Scripts parsing the summary by field position (`awk '{print $2}'`-style) on the current format will silently read the new `rename` term as if it were the second update count. The CHANGELOG entry (§8) flags this for downstream consumers. Scripts that match by token name (`grep -oE '[0-9]+ rename'`) are robust to the change.

#### 4.8 New typed error

`go/errs/errs.go` uses a single `Error` struct with a `Kind` discriminator and shared scalar fields (`Name`, `SourceName`, `ExistingKit`, `Reason`, `Count`, `Cause`). New variants extend the `Kind` enum and reuse the existing fields — they do **not** add per-variant struct types. The new variant follows that pattern:

```go
// In the Kind iota block, append after ReplaceInstallFailed:
RenameAmbiguous

// Sentinel (matches the file's ErrXxx convention):
ErrRenameAmbiguous = &Error{Kind: RenameAmbiguous}

// Constructor — candidates are sorted alphabetically and packed into
// the existing Reason field as a comma-separated string, consistent
// with NewRenameTreeMismatch's use of Reason for composite text.
// No new field is added to the Error struct.
func NewRenameAmbiguous(installKey string, candidates []string) *Error {
    sorted := append([]string(nil), candidates...)
    sort.Strings(sorted)
    return &Error{
        Kind:   RenameAmbiguous,
        Name:   installKey,
        Reason: strings.Join(sorted, ", "),
    }
}

// Error() case:
case RenameAmbiguous:
    return fmt.Sprintf("auto-rename for '%s' is ambiguous: tree SHA matches %s; "+
        "run `inat sync` for guidance and `inat rename` for explicit resolution",
        e.Name, e.Reason)
```

The sort happens inside the constructor so test assertions over the error message are stable regardless of map-iteration order at the call site.

No other new typed errors. The existing `KitNotFound`, `SourceNotFound`, `RenameCrossSource`, `RenameTreeMismatch`, etc. are reused as-is.

#### 4.9 File layout

```
go/cli/transaction/action.go            edit  add ActionRename, update String() + IsActionable.
go/cli/transaction/plan.go              edit  extend planUpdate to dispatch to detection on the kit-absent path; +20 lines.
go/cli/transaction/plan_index.go        new   sourceSHAIndex type + build/candidatesForSHA methods + lazy threading helpers; ~80 lines.
go/cli/transaction/apply.go             edit  add applyRename switch arm + helper.
go/cli/transaction/transaction_test.go  edit  table-driven detection + apply tests; ambiguity / cross-source / drift / single-kit / index-build-error.
go/cli/rename/rename.go                 edit  rename rewriteAlwaysHeaders → exported RewriteAlwaysHeaders.
go/cli/update/update.go                 edit  extend summary-line formatting (R rename term).
go/cli/update/update_test.go            edit  new tests: auto-rename happy, ambiguous, single-kit-with-old-name.
go/errs/errs.go                         edit  add RenameAmbiguous Kind + constructor + Error() case (per §4.8 — single Error struct, fields packed into Reason).
go/errs/errs_test.go                    edit  RenameAmbiguous formatting + errors.Is.
README.md                               edit  update Reorganization §rename: note auto-application during update.
CHANGELOG.md                            edit  [Unreleased] ### Changed entry.
```

One new file (`plan_index.go`). Rationale: `go/cli/transaction/plan.go` is already 333 lines (close to CLAUDE.md §3's 300-line target, well under the 400 hard max). Adding the `sourceSHAIndex` type, its two methods, and the lazy-threading helpers as inline additions would push `plan.go` past the 400 hard max. Splitting the index logic into its own file keeps `plan.go` near its current size, isolates the auto-detection mechanism in a file named for its job, and follows the project's "split by responsibility when exceeded" guidance (CLAUDE.md §3). The new file is ~80 lines; the planUpdate edit inside `plan.go` is small (~20 lines).

---

### 5. Compatibility

- **`go/cli/transaction/`** — public `Plan` and `Apply` signatures unchanged. Internal: one new actionable variant in the action enum. Downstream consumers (`cli/update`, `cli/replace`) already switch on `ActionKind` exhaustively in the current code (the default case returns `KitValidation`); the new variant is handled in `cli/update` (auto-rename participates in update) but not in `cli/replace` (auto-rename never appears in replace's single-target plan — replace targets are `TargetReplace`, not `TargetUpdate`). The default case in `cli/replace` would emit `KitValidation` if the variant somehow appeared, which is unreachable but safe.
- **`go/cli/rename/rename.go`** — `rewriteAlwaysHeaders` is exported as `RewriteAlwaysHeaders`; the function body and call site inside `Rename` are unchanged. No external behaviour change for the `inat rename` command.
- **`go/cli/update/update.go`** — `RunUpdate` and `Update` signatures unchanged. Summary-line text changes (added rename term); no API change.
- **`go/cli/replace/`, `go/cli/refresh/`, `go/cli/sync/`, `go/cli/init/`, `go/cli/agents/`, `go/cli/sources/`, `go/cli/status/`, `go/cli/validate/`, `go/cli/self/`** — unchanged.
- **`.inat/config.json`** — schema unchanged. Configs written before / after this spec round-trip identically.
- **`~/.inat/config.json`** — schema unchanged.
- **`~/.inat/cache/`** — layout unchanged. Auto-detection uses `source.FetchKitTreeAt` exactly as today's `Plan` does; one additional call per registered kit in a source, gated on a registry-miss for at least one target from that source.
- **Agent profiles** — unchanged. Auto-rename writes through the same agent always-targets that `inat rename` already writes through.
- **`go/go.mod` / `go/go.sum`** — byte-identical pre/post change. No new modules.
- **Existing CLI commands** — surfaces unchanged. No new commands, no new flags, no new positional arguments. `inat update` gains auto-rename behaviour silently from a user perspective except for the new plan-summary line (rename count) and the per-rename progress line.
- **Exit-code change for scripted consumers.** Single-kit `inat update <src/old-name>` that today returns the `KitNotFound` typed error with non-zero exit will, under 0018, return nil with exit 0 when the kit was renamed in-source upstream (the auto-rename succeeds). Multi-kit `inat update` that today exits non-zero with `KitsNeedAttention(N)` for `N` moved kits will exit 0 with `0 need attention` when every moved kit is unambiguously auto-renamed. Pipelines that previously relied on the non-zero exit to detect "kit moved, action required" should migrate to grepping the stderr `^renamed ` lines (see §4.6 for the rationale and the suggested replacement). Genuine `KitNotFound` (upstream deletion), `RenameAmbiguous` (multi-candidate), and content-drift cases continue to surface as `needs-attention` with non-zero exit, so scripts that retry on those signals still trigger.
- **Positional summary-line parsing for scripted consumers.** The plan-summary line and final summary insert a `rename` term between `update` and `up-to-date`, shifting later term positions (see §4.7 for the exact format change). Scripts that parse the summary by field index will silently misread the new format; scripts that match by token name (`grep -oE '[0-9]+ rename'`) are robust. The CHANGELOG entry (§8) flags this.
- **`README.md`** — the Reorganization subsection's `rename` paragraph adds a one-sentence note that `inat update` now auto-applies trivial in-source renames; the manual `sync → rename` round-trip is preserved as the documented audit path.
- **`CHANGELOG.md`** — `[Unreleased]` gains a `### Changed` entry: "`inat update` now auto-detects and applies pure-`git mv` kit renames during the plan phase. Strict tree-SHA equality only; cross-source moves continue to route through `inat sync` + `inat replace`. Spec / plan [`0018-kit-reorg-auto-rename`](docs/specs/0018-kit-reorg-auto-rename.md)."
- **Pre-commit hook + CI** — unchanged. The hook's `gofmt`/`vet`/`staticcheck`/`test` gates cover every new and modified file. The spec-presence check is satisfied by this spec/plan pair.

---

### 6. Acceptance criteria

1. **Build clean.** `go -C go/ build ./...` exits 0; the `inat update --help` output is byte-identical to pre-change.
2. **In-source rename auto-applied.** Fake source with kit `foo`; install `company/foo`; in the source, `git mv kits/foo kits/foo-renamed` + update `registry.json` + commit + push (tree content byte-identical). Run `inat update`. Stderr contains, in order: `fetching company...`, `planned: 0 update, 1 rename, 0 up-to-date, 0 need attention`, `renamed company/foo → company/foo-renamed`, `0 updated, 1 renamed, 0 up-to-date, 0 need attention`. Exit 0. `cfg.Installed` has `company/foo-renamed` and not `company/foo`. On-disk delivered files byte-identical to pre-update. `CLAUDE.md`'s managed section's always-block header reads `company/foo-renamed/...` (rewritten per spec 0019).
3. **Single-kit update against old name auto-renames.** State as in (2) but the user runs `inat update company/foo` (typing the old name). Same end state as (2): success, rename applied, headers rewritten. The single-kit fail-fast contract is preserved for genuine `KitNotFound` cases (kit deleted upstream with no SHA match); rename is auto-applied so the user does not see the failure.
4. **Tree-SHA mismatch does NOT auto-rename.** Fake source where kit was renamed AND content was edited in the same commit. The renamed kit's tree SHA differs from the recorded `Installed.Tree`. Run `inat update`. Auto-detection's SHA lookup returns zero candidates. Result: `ActionNeedsAttention` with `KitNotFound`. Per-apply line shows the error; final summary: `0 updated, 0 renamed, 0 up-to-date, 1 need attention`. Non-zero exit. `cfg.Installed` unchanged. **No file on disk modified.**
5. **Ambiguity emits `RenameAmbiguous`.** Fake source where two kits share the same tree SHA as the recorded install (e.g. one is a `cp -r` of the other plus a renamed registry entry). Run `inat update`. Auto-detection's SHA lookup returns two candidates. Result: `ActionNeedsAttention` with `RenameAmbiguous(installKey, [a, b])`. Per-apply line names both candidates and points at `inat sync` / `inat rename`. Non-zero exit. `cfg.Installed` unchanged.
6. **Cross-source move does NOT auto-rename.** Fake sources A and B; kit moved from sourceA to sourceB (tree-SHA-identical, but in a different source). Run `inat update`. Auto-detection runs only against sourceA's registry (where the kit was missing) and finds zero matches there. Result: `ActionNeedsAttention` with `KitNotFound`. The user runs `inat sync` to see case (c) and `inat replace` to apply.
7. **Multi-kit batch with mixed actions.** Three kits: kitA renamed in-source, kitB content-updated normally, kitC up-to-date. Run `inat update`. Stderr summary: `planned: 1 update, 1 rename, 1 up-to-date, 0 need attention`. Apply order: rename for kitA, update for kitB, up-to-date silence for kitC. `cfg.Installed` reflects all three actions; both `kitA-renamed` and `kitB`'s new tree are on disk; kitC unchanged.
8. **Cross-action name collision caught.** Two kits in batch: kitX renamed in-source from `src/X-old` to `src/X-new` (names: `[foo]`); kitY content-updated, post-update names include `foo`. `checkBatchUniqueness` sees both kits owning `foo` in the post-batch view. At least one of the two actions converts to `ActionNeedsAttention` with `DuplicateName`. Test asserts the typed error fires; exact victim selection is implementation-defined (the existing logic visits actions in input order).
9. **Lazy index — clean batch builds zero indices.** Three-kit batch where every kit is present at its registered name (no auto-detection needed). Assert via test instrumentation: `sourceSHAIndex.build()` is called zero times across the whole `Plan` invocation. Confirms the auto-detection adds no cost when nothing has moved.
10. **Lazy index — one missing kit builds one index.** Three-kit batch from two sources; kitA in sourceA was renamed (registry miss → detection runs). kitB in sourceA still present (no detection). kitC in sourceB still present (no detection). Assert: `sourceSHAIndex.build()` called exactly once, against sourceA only.
11. **Header rewrite failure rolls back in memory.** Inject a header-rewrite failure (synthesize via permission tweak on `CLAUDE.md`). `applyRename` returns `ActionNeedsAttention`; in-memory `cfg.Installed` reflects pre-rename state (no `newKey` entry, `oldKey` still present); the caller's gated `SaveConfig` does not rewrite the disk file. The user's on-disk config file shows `oldKey` byte-identically to before the call.
11a. **Multi-agent partial-disk state on header-rewrite failure.** Two active agents, both with always-targets carrying the renamed kit's block. Inject a failure on the second agent's rewrite only (e.g. chmod 000 the second target before `applyRename` runs). The first agent's header is already on disk pointing at `newKey`; the in-memory `cfg.Installed` is rolled back to `oldKey`; the disk config file says `oldKey`. The test asserts: returned `ActionNeedsAttention` with the underlying error; first agent's `CLAUDE.md` (or equivalent) on disk references `newKey` (the partial state); `cfg.Installed[oldKey]` is restored in memory; disk config file unchanged. The test ALSO asserts the documented recovery path: after running `inat refresh`, the first agent's header is rewritten back to `oldKey/...` to match the saved config.
11b. **Single-kit fail-fast for `KitNotFound` is preserved for genuine cases.** `inat update src/genuinely-deleted-kit` (kit deleted upstream, no tree-SHA match anywhere) returns the underlying `KitNotFound` error byte-identically to today. Exit non-zero. The single-kit auto-rename path of goal 8 fires only when the SHA match succeeds; deletion cases still surface.
11c. **`sourceSHAIndex.build` mid-fetch error is localised.** Fake source with N=5 kits in the registry; kit-3's tree object is corrupted (or its `kit.json` cannot be fetched). The remaining four kits' tree SHAs are indexed correctly; auto-detection succeeds for those targets; the target whose recorded SHA happens to match kit-3 (the corrupt entry) falls through to `KitNotFound` (today's behaviour). No other targets from the source are affected. Test asserts: build runs once; the index contains four entries; `KitNotFound` is the only needs-attention for the affected target.
12. **Replace targets never auto-rename.** A `TargetReplace` whose `InstallKey` is in `cfg.Installed` and whose `NewRef` points at a kit not in the new source's registry: the kit-absent-from-registry path inside `planReplace` is unchanged from spec 0017 — it returns `KitNotFound`, not a rename. Auto-detection runs only inside `planUpdate`.
13. **`inat rename` (explicit verb) unchanged.** All existing `cli/rename/` tests pass byte-identically after the `RewriteAlwaysHeaders` export refactor.
14. **`inat sync` unchanged.** All existing `cli/sync/` tests pass byte-identically. Sync continues to surface case (b) suggestions read-only — its independent classifier is not refactored to share `transaction`'s detection code.
15. **No on-disk schema change.** Diff `.inat/config.json` byte-by-byte before/after an auto-rename run: only `cfg.Installed`'s key changes (`old` removed, `new` added with the same `InstalledKit` value).
16. **No new module dependencies.** `git diff` of `go/go.mod` and `go/go.sum` between pre-change and post-change tree is empty.
17. **No `unsafe`, no goroutines.** `grep -n "unsafe\|go func\|go " go/cli/transaction/` returns nothing new (the package-level pattern from spec 0017 is preserved). `staticcheck` and `go vet` pass.
18. **File / function size limits.** Every modified file under 300 lines; every new function under 30 lines.
19. **Pre-commit hook clean.** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all exit 0.

---

### 7. Testing notes

- **Fake sources via local bare git repos.** Same harness as specs 0016 / 0017 / 0019: `t.TempDir()`-backed bare repos with `kit.json` + `registry.json`, configured via `cfg.Sources[].URL = file://...`. No live network.
- **Tree-SHA-identical rename fixture.** The canonical fixture: `git mv kits/foo kits/foo-renamed` in the source's working clone (file content untouched), update `registry.json` to swap the entry's name (key change only, content byte-identical), commit, push to the bare repo. `source.FetchKitTreeAt` against either name in any commit that contains the kit returns the same hash. Tests capture the hash post-install and assert it is unchanged across the rename.
- **Ambiguity fixture.** `cp -r kits/foo kits/foo-copy` in the source's clone, add `foo-copy` to `registry.json`, commit, push. Both `foo` and `foo-copy` now resolve to the same tree SHA. Run `inat update` against an install of `src/foo`; assert `RenameAmbiguous` fires with `Candidates: [foo, foo-copy]` (alphabetical) and `cfg.Installed` is unchanged.
- **Drift-during-rename fixture.** `git mv kits/foo kits/foo-renamed` plus a content edit to one file under the kit, commit, push. Tree SHA differs. Run `inat update`; assert `KitNotFound` (today's behaviour preserved) and no auto-rename.
- **Cross-source fixture.** Two configured sources. Kit `foo` installed from sourceA. In sourceA: delete `kits/foo` and its registry entry, commit, push. In sourceB: `cp -r` the same tree into `kits/foo`, add registry entry, commit, push. Run `inat update`; assert `KitNotFound` against sourceA (auto-detection scopes per-source); the user discovers the cross-source move via `inat sync` and runs `inat replace`.
- **Lazy-index instrumentation.** Tests assert build counts via a package-private hook in `plan_index.go`: `var buildHook func() = nil`. Production code calls `if buildHook != nil { buildHook() }` once at the top of `build()`. Tests set the hook to a counter-incrementer; the hook is unset after each test. The hook is package-internal (lowercase `buildHook`) so it does not leak to package consumers. Choice rationale: alternative would be exposing a build-counter field on `sourceSHAIndex`, but that pollutes the production type with test-only state; the package-private hook keeps the production type clean. Assertions: build called zero times for a clean batch; once per source that needs detection; never more than once per source per `Plan` invocation.
- **Tree-SHA test fixtures.** Auto-rename requires byte-identical tree content at two paths. The `cp -r` pattern (`cp -r kits/foo kits/foo-renamed` + commit) produces matching SHAs because go-git's tree object is computed over the recursive (name, mode, blob-sha) triples and `cp -r` preserves modes. The fixture should be alert to one trap: explicitly setting file permissions to `0644` after copy is necessary on systems where the umask might produce different bits; otherwise the trees differ on mode and the SHAs diverge. Existing inat test helpers in `internal/testfixture/` (referenced in spec 0016 §7) handle this.
- **In-memory rollback test.** Synthesize a header-rewrite failure by making the target file a directory (or chmod 000). Assert `applyRename` returns `ActionNeedsAttention`, `cfg.Installed[oldKey]` is present with byte-identical value, `cfg.Installed[newKey]` is absent. The disk-file check (config save was gated) is unchanged from spec 0017's pattern.
- **Single-kit-with-old-name test.** `inat update src/old-name` where the kit was renamed upstream. Assert: success (nil error), the cobra command exits zero, the rename is applied. Today this returns `KitNotFound` directly to the caller; under 0018 it succeeds.
- **Cross-action collision test.** Two-kit batch where kitX's rename (no names change) and kitY's update (NewNames adds a name kitX has) collide on the post-batch view. Assert the typed error is `DuplicateName` (not `RenameAmbiguous` — that's a different failure class). The victim is whichever action `checkBatchUniqueness` visits first in input order.
- **Plan-summary stderr format test.** Capture stderr; assert the exact format including the rename term: `planned: %d update, %d rename, %d up-to-date, %d need attention\n`. Same for the final summary.
- **No live network in any test.** Same constraint as the existing test suite.
- **No goroutines in any new test.** Verified by `grep` on the new code.

---

### 8. Documentation

- **`README.md`** — the Reorganization subsection's `rename` paragraph today describes `inat sync` suggesting a rename and the user running `inat rename` to apply. Add a final sentence: "Pure-`git mv` renames (kit's tree byte-identical at the new path) are auto-applied during `inat update`; the explicit `inat rename` verb remains available for users who prefer to audit each move and for the `inat sync`-then-act workflow." Cross-link to spec 0018.
- **`CHANGELOG.md`** — `[Unreleased]` gains a `### Changed` entry:
  > **`inat update` now auto-detects and applies pure-`git mv` kit renames.** When `Plan` finds a target kit absent from its source's current registry but its recorded tree SHA is present at exactly one different name in the same source, the action plans as `Rename` and the apply phase rewrites the install key plus the always-rule managed-section headers — same semantics as the explicit `inat rename` verb (spec 0019). Strict tree-SHA equality only; cross-source moves and content-drift cases continue to route through `inat sync` and the explicit `inat rename` / `inat replace` verbs. The plan-summary line and final summary gain a `rename` term — field positions shift; scripts that parse positionally should migrate to token-name matching (`grep -oE '[0-9]+ rename'`). **Exit-code change for scripted consumers:** `inat update` against a kit that was renamed in-source upstream now exits 0 with `renamed src/old → src/new` on stderr, where today it exits non-zero with `KitNotFound`. Scripts relying on the non-zero exit to alert on "kit moved" should grep stderr `^renamed ` lines instead. Genuine `KitNotFound` (upstream deletion), `RenameAmbiguous` (multi-candidate), and content-drift cases continue to exit non-zero. Spec / plan [`0018-kit-reorg-auto-rename`](docs/specs/0018-kit-reorg-auto-rename.md).
- **`docs/specs/0016-kit-reorg-baseline.md`** — the "Auto-applying detected renames during `update`" non-goal at §3 gains a closing forward-reference: "Closed by spec [0018](0018-kit-reorg-auto-rename.md)." Same for the §9 follow-up bullet. Documentation-only edits to an already-shipped spec.
- **`docs/specs/0017-transactional-update.md`** — the "Auto-applying detected renames during update" non-goal at §3 gains the same forward-reference closure.
- **`docs/specs/0018-kit-reorg-auto-rename.md`** — this document.
- **`docs/plan/0018-kit-reorg-auto-rename.md`** — companion rollout document.

---

### 9. Out-of-scope follow-ups

- **`--no-auto-rename` escape hatch.** If the auto-apply behaviour proves surprising, add a flag to `inat update`. One-line conditional in `planUpdate`; substrate is ready.
- **Cross-source auto-detection.** Extend the detection to consider every configured source's tree-SHA index (mirroring `inat sync` case (c)). Would auto-apply cross-source moves during update. The judgement call about kit provenance changing is significant enough to be a deliberate user action today; revisit if the manual replace step becomes a routine bottleneck.
- **JSON / dry-run plan output (`inat update --plan`).** Still deferred from spec 0017 §9. The auto-rename addition makes the value of a dry-run higher — users may want to inspect detected renames before applying. Substrate is unaffected. **Update**: the human-readable `--plan` half is closed by spec [0020](0020-update-plan-dry-run.md); JSON output remains deferred.
- **Detection during `inat status`.** Status today is offline; adding detection would require fetching every source. Could be useful as a pre-update audit but conflicts with status's no-network contract. A separate `inat audit` verb may be a better home.
- **Tree-SHA index caching across invocations.** Today the index is rebuilt every `Plan` call that hits the slow path. For very large kit corpora this could be cached on disk (under `~/.inat/cache/<source>/`) and refreshed when the source's HEAD changes. Premature for the current corpus sizes; revisit if profiling shows the rebuild is hot.
- **Refresh integration.** `inat refresh` does not fetch sources, so auto-detection cannot run there. If a future spec adds a fetch to refresh (or introduces a separate `inat audit` verb), the detection logic from 0018 is reusable byte-identically.
- **Migration to use `transaction`'s detection from `inat sync`.** Sync today has its own classifier; consolidation would unify the two and ensure they stay in lockstep on edge cases. A clean refactor opportunity; out of scope here.
