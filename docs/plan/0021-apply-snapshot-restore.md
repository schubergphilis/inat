## inat — On-disk snapshot/restore for the apply phase (Plan 0021)

Pairs with [`docs/specs/0021-apply-snapshot-restore.md`](../specs/0021-apply-snapshot-restore.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, TDD test order, and verification.

Pays down deferrals from [`docs/specs/0017-transactional-update.md`](../specs/0017-transactional-update.md) §9 (on-disk snapshot/restore for the apply phase) and [`docs/specs/0016-kit-reorg-baseline.md`](../specs/0016-kit-reorg-baseline.md) §3 (replace install-half files-gone limitation).

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Snapshot location | `~/.inat/snapshots/<UTC-compact-timestamp>-<sanitized-install-key>/`. Sibling of `~/.inat/cache/` and `~/.inat/config.json`. Honours `homeOverride` for tests via `config.HomeDir()`. |
| Snapshot directory naming | Timestamp format `20060102T150405Z` (no colons; Windows-safe). Install key `/` replaced with `__` (filesystem-safe; install-key alphabet is `[a-z0-9-]` so `__` is unambiguous). If the target dir already exists (leftover forensic state from a prior aborted run or same-second retry), `Capture` appends `-2`, `-3`, … and retries; pre-existing forensic dirs are never overwritten. |
| Snapshot scope per apply | One snapshot per kit, captured at the entry of `applyUpdate` and `applyReplace`. Per-kit-atomic batch contract (spec 0017) is unchanged: each kit's snapshot lifecycle is independent of every other kit in the batch. |
| What gets captured | (a) Every `Path` in `installed.Delivered`. (b) Each active agent's `mapping.Rule.Always.Path` and `mapping.Rule.Contextual.Reference`. (c) Paths deduped by resolved value — for the canonical `ClaudeCodeProfile`, Always.Path and Contextual.Reference both resolve to `CLAUDE.md` and must produce a single manifest entry. Files that don't exist on disk are recorded with `"absent": true` so restore deletes any file the apply created mid-flight. Bytes are captured from the current on-disk state (including any drift), since the contract is byte-identity restore to pre-apply, not restore-to-the-cache. |
| Snapshot retention | Delete on success (`os.RemoveAll`). Keep on failure (apply failure with restored disk → forensic; apply failure with failed restore → recovery handle). No automated GC. |
| Snapshot-capture failure handling | New typed error `SnapshotCaptureFailed`. Returned as the kit's `ActionNeedsAttention` cause. Capture is atomic from the caller's perspective: a partial capture cleans itself up before returning the error. No on-disk project mutation has occurred. Other kits in the batch proceed independently. |
| Restore-failure handling | New typed error `ApplyRestoreFailed` wrapping `errors.Join(applyErr, restoreErr)` as a single `Cause` chain (so both error sides survive `errors.Is` / `errors.As` traversal). The kit's `ApplyResult` is `ActionNeedsAttention`. Snapshot dir survives on disk. `Error()` string names the snapshot dir path. |
| Discard-failure handling | Non-fatal warning to stderr. The orphaned snapshot dir does not affect correctness; surface it to the operator so they can clean up. |
| Rename apply path | No snapshot. `applyRename` (`apply.go:148-165`) does not touch delivered files; its only disk write is the always-rule header rewrite, which already has its own in-memory cfg rollback. Spec 0019's multi-agent header-rewrite partial-state risk is acknowledged in spec 0018 §4.4 and explicitly out of scope here. |
| Snapshot package location | `go/core/snapshot/`. Precedent: existing leaf packages `core/hash`, `core/reserved`, `core/delivery`. The `core/` rule is about avoiding catch-all `util` packages, not about restricting I/O — `core/delivery` already does file I/O. |
| `removeAndDeliver` reshape | Keep its current signature (no snapshot parameter); push snapshot lifecycle up to `applyUpdate`. `removeAndDeliver` continues to be "do the mutation, return any error." This keeps the helper testable in isolation and avoids threading lifecycle objects through pure-logic helpers. |
| Shared lifecycle helper | New private `runWithSnapshot(dir, installKey string, paths []string, homeOverride string, work func() (ApplyResult, error)) ApplyResult` in `apply.go`. Both `applyUpdate` and `applyReplace` invoke it. Forced refactor (not conditional): `applyReplace` is already 57 lines today and any in-place wrap would bust the 30-line function budget. |
| `applyRename` identifier rename | Local variable `snapshot := cfg.Installed[oldKey]` (`apply.go:151`) renamed to `prev` to avoid shadowing the imported `snapshot` package alias once `apply.go` imports `core/snapshot`. |
| Public API surface | `snapshot.Capture(Request) (*Snapshot, error)`, `(*Snapshot).Restore() error`, `(*Snapshot).Discard() error`, `type Snapshot struct{ Dir string }`, `type Request struct{ ... }`. No `Role` tag — capture, restore, discard treat every entry identically, so the type-tag would be dead distinction. Errors via the typed `errs` package. |
| Manifest schema versioning | `schemaVersion: 1`. Loader rejects any other value with `errs.KitValidation`. Future schema-incompatible changes bump the version. |
| New typed errors | `SnapshotCaptureFailed`, `ApplyRestoreFailed`. Follow the existing `ReplaceInstallFailed` pattern in `errs.go` (Kind constant, sentinel `Err...`, constructor `New...`, `Error()` case). |
| Module dependencies | None added. Standard library + existing `errs`, `core/config`, `core/hash`, `agents` only. |
| Tests against real filesystem | Yes — `t.TempDir()`-rooted `homeOverride` is the existing convention (see `update_test.go`, `replace_test.go`). |
| Failure-injection seam | Apply tests need a way to make `RemoveDeliveredFiles` / `CleanManagedSections` / `DeliverToAgent` fail mid-flight. Approach: hand-crafted fixtures (read-only target file, conflict on a third kit's name) rather than a runtime hook — keeps the production code clean of test-only branches. |
| README scope | One paragraph under a new (or existing) "Recovery from a failed apply" subsection. Notes the snapshot dir location, the keep-on-failure-delete-on-success behaviour, and that `inat refresh` remains the user-facing recovery path. |

---

### 2. Files

#### Modified

- **`go/cli/transaction/apply.go`** — `applyUpdate` and `applyReplace` reshape to delegate to a new `runWithSnapshot` helper. New private helpers `collectSnapshotPaths` and `runWithSnapshot`. `applyRename` local var `snapshot` renamed to `prev`. Realistic estimate ~80-100 added lines once the function-size split is honoured; if the file crosses 300 lines (currently 220), split out a sibling `apply_snapshot.go` carrying `runWithSnapshot` + `collectSnapshotPaths` to stay inside the budget.
- **`go/cli/transaction/apply_test.go`** *(new — split from `transaction_test.go` for size)* — apply-phase tests asserting snapshot lifecycle invariants. ~150 lines.
- **`go/cli/update/update_test.go`** — extend existing failure fixtures with file-byte-identity assertions post-restore. ~40 lines added.
- **`go/cli/replace/replace_test.go`** — no install-half-failure fixture exists today (the only collision fixture, `TestReplaceRejectedAtPlanByNameCollision` at line 209, is caught at plan phase). New fixture `TestReplaceInstallHalfFailureRestoresOldFiles`: shape the agent's always-rule target as a directory before `Replace` so `delivery.AddAlwaysBlock` fails inside the install half; assert `ReplaceInstallFailed` + byte-identity restoration of oldKey's delivered files. Drift-abort fixture (`TestReplaceDriftWithoutForceAborts`, line 283) gains restore-of-partially-removed-files assertion. ~80 lines added.
- **`go/errs/errs.go`** — two new `Kind` constants (`SnapshotCaptureFailed`, `ApplyRestoreFailed`), two `Error()` cases, two `New...` constructors, two `Err...` sentinels. `ApplyRestoreFailed.Cause` holds `errors.Join(applyErr, restoreErr)`. Update the `Error` struct field-purpose comment to mention the new variants. **Also**: tweak `ReplaceInstallFailed`'s `Error()` text at `errs.go:158-160` to drop the "run `inat refresh`" suggestion — restore now brings old files back automatically. Existing typed-error class unchanged. ~35 lines added/changed.
- **`go/errs/errs_test.go`** — table-driven assertions for the two new variants. ~30 lines added.
- **`README.md`** — one paragraph (≤120 words) on snapshot recovery semantics under a new H3 "Recovery from a failed apply" inside the existing transactional-update section.
- **`CHANGELOG.md`** — `[Unreleased]` gains a `### Added` entry pointing at spec 0021 and an `### Changed` entry noting the replace-install-half files-restored upgrade.
- **`docs/specs/0016-kit-reorg-baseline.md`** — single closure line on §3 "Replace's install-half-failure leaves old files removed on disk" follow-up: "Closed by spec [0021](0021-apply-snapshot-restore.md)."
- **`docs/specs/0017-transactional-update.md`** — single closure line on §9 "On-disk snapshot/restore for the apply phase" follow-up: same closure pointer.

#### New

- **`docs/specs/0021-apply-snapshot-restore.md`** — the contract.
- **`docs/plan/0021-apply-snapshot-restore.md`** — this document.
- **`go/core/snapshot/snapshot.go`** — `Capture` / `Restore` / `Discard` + types. Realistic budget ≥250 lines given the responsibilities (dir tree creation, file copy, manifest read/write, collision retry, schema-version check); plan for a split into `snapshot.go` + `manifest.go` if the single-file count crosses 300.
- **`go/core/snapshot/snapshot_test.go`** — unit tests. ~180 lines.

---

### 3. TDD test order

1. **`errs/errs_test.go` first.** Add table-driven assertions for `SnapshotCaptureFailed` and `ApplyRestoreFailed`: `Error()` string format, `errors.As` round-trip, sentinel `errors.Is` match. Write the `errs.go` additions to make them green.
2. **`core/snapshot/snapshot_test.go`** — write before the implementation. Order:
   - Capture-and-discard: snapshot dir created with timestamp-key naming, manifest schema correct, after `Discard()` dir is gone.
   - Capture-and-restore: capture a file, mutate it, restore, assert byte-identity.
   - Capture-and-restore with absent file: snapshot a path that doesn't exist (records `"absent": true`); create the file post-capture; restore; assert file is gone.
   - Multi-path capture: snapshot a mix of paths (delivered file + agent target file with different relPaths) in one call; round-trip restore brings both back.
   - Capture-failure cleanup: place a regular file at `<home>/.inat/snapshots` so `os.MkdirAll` fails predictably across platforms; assert `errs.SnapshotCaptureFailed`, assert no orphan dir.
   - Leftover-dir collision: pre-create `<home>/.inat/snapshots/<expected-ts-key>/` containing a marker file; invoke `Capture` with the matching ts+key; assert the new snapshot lands at `<...>-2/` and the marker file in the original dir is unchanged.
   - Manifest schemaVersion validation: after `Capture`, overwrite the snapshot's `manifest.json` with `schemaVersion: 2`; call `Restore` on the same `*Snapshot`; assert `errs.KitValidation`. `Restore` re-reads the manifest from disk each call.
3. **Snapshot implementation in `core/snapshot/snapshot.go`.** Run `snapshot_test.go` to green.
4. **`cli/transaction/apply_test.go`** — new file. Tests:
   - Update happy path: snapshot dir created, deleted on success.
   - Update apply-failure: failure injected, snapshot dir survives, on-disk files byte-identical to pre-apply.
   - Replace install-half failure: same, plus assertion that the *old* kit's delivered files are byte-identical (the central improvement).
   - Replace drift-abort restore: drift triggers after partial remove, snapshot restores fully.
   - Snapshot capture failure: capture itself fails, no on-disk mutation, no snapshot dir.
   - Restore failure: apply and restore both fail, `ApplyRestoreFailed` is the error class, snapshot dir survives.
   - Batch isolation: three-kit batch (A success, B fails+restores, C success). Snapshot dir state asserted per-kit.
   - Rename apply: no snapshot dir created.
5. **`cli/transaction/apply.go`** — integrate snapshot lifecycle. Run `apply_test.go` to green.
6. **Update existing tests + add the new install-half-failure fixture:**
   - `update_test.go` failure cases: existing assertions about post-failure disk state get tightened from "config unchanged" to "config AND delivered files AND agent target files all byte-identical."
   - `replace_test.go` — add `TestReplaceInstallHalfFailureRestoresOldFiles` (no precedent today). Fixture: install oldKey, then before calling `Replace`, replace the active agent's `mapping.Rule.Always.Path` file with a directory of the same name so `delivery.AddAlwaysBlock` fails. Assert `ReplaceInstallFailed`, byte-identity of oldKey's delivered files, snapshot dir survives. This is the central new behaviour the spec promises (CHANGELOG `### Changed`).
   - `replace_test.go` drift-abort test (`TestReplaceDriftWithoutForceAborts`, line 283): extend with the partially-removed-files-restored assertion.
7. **Full local test pass.** `go -C go/ test ./...` clean.

---

### 4. Implementation steps

1. **`errs` additions.** Add Kind constants, sentinels, constructors, `Error()` cases per spec §4.6. Update the struct-field comment. Run `errs_test.go` to green.
2. **`core/snapshot/snapshot.go`** — public API per spec §4.3. Internal helpers: `sanitizeInstallKey`, `timestampDir`, `copyFileIntoSnapshot`, `restoreFileFromSnapshot`, manifest read/write. Watch the 30-line function limit: `Capture` will need to split into "create dir + write manifest" and "copy files" helpers. Run `snapshot_test.go` to green.
3. **`collectSnapshotPaths` helper** in `apply.go`. Walk `installed.Delivered` (RoleDelivered) and every active agent's two target files (RoleAgentTarget). Return `[]FileSpec`.
4. **`runWithSnapshot` helper** in `apply.go`: `runWithSnapshot(dir, installKey string, paths []string, homeOverride string, work func() (ApplyResult, error)) ApplyResult`. Captures, runs the closure, restores on closure error (or `ApplyRestoreFailed` on double-fault), discards on success.
5. **`applyUpdate` reshape** (`apply.go:56-73`): collect paths, delegate to `runWithSnapshot` with the closure being the current `removeAndDeliver` + cfg mutation + success-stderr line. Stays inside the 30-line function budget.
6. **`applyReplace` reshape** (`apply.go:75-131`): same shape. The closure body holds the existing `RemoveDeliveredFiles` → drift-check → `CleanManagedSections` → `deliverAcrossAgents` sequence. Drift short-circuit and `deliverAcrossAgents` failure both surface as closure errors, both restore from snapshot. Typed-error contract (`KitsNeedAttention(1)` for drift, `ReplaceInstallFailed` for install half) preserved.
7. **`applyRename` local variable rename**: `snapshot := cfg.Installed[oldKey]` → `prev := cfg.Installed[oldKey]` (`apply.go:151, 153-155, 160`). Required because `apply.go` now imports `core/snapshot` and the package alias `snapshot` would shadow the local.
8. **Wire `homeOverride` through.** `applyUpdate` and `applyReplace` already receive it (`apply.go:27`); pass to `snapshot.Capture` via the `Request`.
9. **Run `apply_test.go`** — should pass.
10. **Update `update_test.go`** failure fixtures: trigger deliver-phase failure via the directory-in-place-of-file fixture style; tighten post-failure assertions to byte-identity for all delivered + agent target files.
11. **Update `replace_test.go`**: add the new install-half-failure fixture (`TestReplaceInstallHalfFailureRestoresOldFiles`) using the contextual-rule-path-as-directory injection; extend the drift fixture to three delivered files per acceptance criterion 5.
12. **CHANGELOG + README.**
13. **Cross-reference closure lines** in spec 0016 and 0017.
14. **Full local test pass + pre-commit hook gate.**

---

### 5. Verification

- **Local unit tests:** `go -C go/ test ./...` exits 0.
- **Local lint:** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...` all exit 0.
- **End-to-end smoke (manual):**
  - Set up a project with two installed kits via the `internal/testfixture` source helper.
  - Run a happy-path `inat update` and confirm `~/.inat/snapshots/` is empty after.
  - Inject a failure (e.g. `chmod -w` an agent target file) and run `inat update`; confirm exit non-zero, snapshot dir present, and `diff` of the project tree pre/post is empty.
  - Reproduce the replace install-half-failure fixture and confirm the old key's files are still on disk.
- **CI parity:** GitLab `test` job runs the same `go test ./...` against `go/`; no new job required.

---

### 6. Risks

- **Disk usage growth on operators who routinely hit apply failures.** No automated GC. Mitigation: document the snapshot location in README, mention it in the failure error message (`ApplyRestoreFailed`'s `Error()` names the dir path), and surface "inat snapshots prune" as a future-spec follow-up.
- **Restore atomicity.** `Restore` walks the manifest sequentially. If a write fails mid-restore, the disk is in an intermediate state (some files restored, some not). Mitigation: this is the `ApplyRestoreFailed` case — the snapshot dir survives, the operator (or `inat refresh`) has a recovery handle. A more sophisticated two-phase restore (stage to temp, atomic rename) is out of scope; the cost outweighs the marginal additional safety given that we're already in a double-failure scenario.
- **Snapshot capture cost for large kits.** Copying every delivered file + every agent target file is O(N + K) syscalls per apply. For typical kit sizes (5–20 files) this is unmeasurable; for pathological kits (hundreds of files) it's still bounded by the apply's own read+write work. Not optimising in v1.
- **Behavioural change to `replace` install-half failure.** Today, `replace`'s install-half failure leaves old-kit files gone (`apply.go:114-121`). After this spec, they are restored. This is a documented improvement, called out as a `CHANGED` entry in CHANGELOG. Any consumer that relied on the old-files-gone behaviour (unlikely but possible) needs to know. The `ReplaceInstallFailed` error class is byte-identical.
- **Drift abort restore semantics.** Before this spec, a drift-aborted `replace` left the drift-skipped files in place and the non-skipped files removed (per `apply.go:81-100` — `RemoveDeliveredFiles` returned with `skipped` non-empty, the skipped file stayed on disk, the others were already removed). After: the non-skipped removed files come back, the drifted file stays in its user-modified state (the snapshot captured it that way). The operator-facing "your modifications survived" promise is unchanged for the drift-skipped files specifically. CHANGED entry in CHANGELOG.
- **`homeOverride` plumbing.** Snapshot dir must use `homeOverride` for tests to be hermetic. Existing test fixtures already pass `homeOverride` to `update.Update` / `replace.Replace`; the apply layer threads it down to `snapshot.Capture` via the `Request` struct. Risk: forgetting to pass it in one path would write to the real `~/.inat/snapshots/`. Mitigation: snapshot's test helper enumerates `<tempdir>/.inat/snapshots/` and would fail loudly if anything leaked.
- **schemaVersion future-proofing.** Setting `schemaVersion: 1` today commits to recognising it forever (or rejecting it explicitly). Mitigation: cheap; one switch case. The upside is that future-spec snapshot-format changes don't silently corrupt operator forensic state.
- **`errors.Join` traversal.** `ApplyRestoreFailed.Cause = errors.Join(applyErr, restoreErr)`. Callers that did `errors.Is(err, errs.ErrFoo)` against a `ReplaceInstallFailed`-wrapped error continue to work because `errors.Is` walks joined chains. New behaviour: `errors.As` on a joined chain returns the FIRST matching typed-error in walk order — restore errors may now precede or follow apply errors in that walk. Callers that need to distinguish "which side raised this" must inspect both via `errors.As` explicitly or compare `Kind`. Test asserts both sides recoverable.
- **`ReplaceInstallFailed` message text change.** `errs.go:158-160` currently directs operators to "run `inat refresh` to recover" the missing old files. After this spec the refresh is redundant. The message is updated; any test asserting against the exact string needs to update. The `Kind` constant and constructor signature are unchanged.

---

### 7. Definition of done

All acceptance criteria from spec 0021 §6 met. CHANGELOG entries (Added + Changed) present. Cross-reference closure lines in spec 0016 and 0017 present. README "Recovery from a failed apply" paragraph present. MR description names spec 0021 and links the spec/plan pair.
