## inat — On-disk snapshot/restore for the apply phase (Spec 0021)

---

### 1. Overview

Spec [0017](0017-transactional-update.md) §3 acknowledged a residual risk: disk-write failures during the apply phase (`RemoveDeliveredFiles`, `CleanManagedSections`, `DeliverToAgent`) can leave a kit half-applied. Spec 0016 carried an analogous limitation for `inat replace`'s install-half failure — when the remove half succeeds and the install half then fails, the old kit's delivered files are gone (`go/cli/transaction/apply.go:75-131`). Both gaps share one fix: capture the pre-apply on-disk state and replay it on apply failure.

This spec adds an on-disk snapshot/restore primitive scoped to the apply phase of `update` and `replace`. Each kit's apply opens a snapshot, runs the existing remove/clean/deliver sequence, and either deletes the snapshot on success or restores from it on failure. The snapshot is per-kit (the per-kit-atomic batch contract from 0017 is unchanged) and on-disk (survives process crash). `rename`'s apply path (`apply.go:148-165`) does not touch delivered files and is out of scope.

A companion implementation plan lives at [`docs/plan/0021-apply-snapshot-restore.md`](../plan/0021-apply-snapshot-restore.md).

This spec extends [`docs/specs/0017-transactional-update.md`](0017-transactional-update.md) (the plan/apply substrate) and pays down deferrals flagged in 0017 §9 (on-disk snapshot/restore for the apply phase) and 0016 §3 (replace install-half files-gone limitation).

---

### 2. Goals

1. The apply phase of `update` and `replace` MUST, on per-kit failure, leave on-disk delivered files and agent target files byte-identical to their pre-apply state. The kit's `ApplyResult` continues to be `ActionNeedsAttention` with the same underlying typed error class as today (`KitsNeedAttention`, `ReplaceInstallFailed`, etc.) so existing callers and tests stay unchanged at the error-class boundary.
2. The snapshot MUST be captured on disk under `~/.inat/snapshots/<timestamp>-<sanitized-install-key>/` (resolved through `config.HomeDir()`, honouring `homeOverride` for tests). The directory MUST be self-contained: a `manifest.json` plus preserved file copies referenced by relative project paths.
3. On successful apply, the snapshot directory MUST be removed (`os.RemoveAll`). On failed apply with successful restore, the snapshot directory MUST be left in place for operator forensics. On failed apply with failed restore, the snapshot directory MUST also be left in place (it is the manual recovery handle).
4. Snapshot capture failure (e.g., ENOSPC creating the snapshot dir, permission denied) MUST abort the kit cleanly *before* any mutation of project files. The kit's `ApplyResult` is `ActionNeedsAttention` carrying a new typed error `SnapshotCaptureFailed` wrapping the underlying I/O cause. Other kits in the batch proceed independently per the existing skip-and-continue contract.
5. Restore failure (apply failed AND restore failed) MUST surface as a new typed error `ApplyRestoreFailed` wrapping BOTH the original apply error and the restore error. The kit's `ApplyResult` is `ActionNeedsAttention`. The snapshot directory remains on disk; the error message names it.
6. The behaviour MUST be symmetric between `update` and `replace`: both call the same snapshot helper, both observe the same restore semantics on failure. The integration point is the `removeAndDeliver` helper (`apply.go:173-196`) and the `applyReplace` body (`apply.go:75-131`).
7. `rename`'s apply path MUST NOT take a snapshot. Per `apply.go:148-165`, rename mutates no delivered files; its only disk write is the always-rule header rewrite, which already has its own in-memory cfg rollback. Adding snapshot infrastructure here would be cost without benefit (spec 0019's header rewrite is line-local and recoverable via `inat refresh`).
8. Batch isolation MUST hold: in a multi-kit update, each kit gets its own snapshot. Kit A's snapshot is independent of kit B's. Kit A succeeding (snapshot deleted) does not affect kit B's apply or its snapshot lifecycle.
9. The on-disk schema of `Config`, `InstalledKit`, `DeliveredFile`, and `~/.inat/config.json` MUST remain byte-stable. Snapshots live under `~/.inat/snapshots/`, a new sibling directory to `~/.inat/cache/` and `~/.inat/config.json`. No existing schema field changes.
10. No new module dependencies. New typed errors only: `SnapshotCaptureFailed`, `ApplyRestoreFailed`.

---

### 3. Non-goals

- **Concurrency / locking on the snapshots directory.** Per CLAUDE.md §11 the CLI is single-threaded and a single invocation owns its own snapshot directories (timestamp-suffixed). Cross-invocation locking is not in scope.
- **Automated GC / retention policy.** Successful applies clean up their own snapshot. Failed applies leave it. There is no scheduled prune, no `--max-snapshots` flag, no age-based cleanup. A future `inat snapshots prune` verb may add this; out of scope here.
- **A `inat snapshots list` / `inat snapshots restore <id>` UX.** The snapshot directory is forensic and recovery-via-`inat refresh` remains the user-facing answer for the "restore failed too" case. A dedicated verb is a future-spec discussion.
- **Snapshotting the `~/.inat/config.json` or `.inat/config.json`.** Per spec 0017's gated-save invariant, the disk config is only written on whole-batch success — it self-restores on failure without a snapshot. Adding one would be dead weight.
- **Snapshotting `applyRename`'s header rewrites.** Per goal 7. Rename's in-memory cfg rollback (`apply.go:158-161`) is sufficient and the disk-write surface is a single helper call.
- **Snapshot encryption or hashing-for-integrity.** Snapshots are operator-readable forensic artifacts on the same disk as the project. Cryptographic integrity is overkill for the threat model.
- **Cross-platform path handling beyond what `config.HomeDir()` already does.** Snapshots live under `HomeDir()` and use `filepath.Join`. Windows paths work the same way `~/.inat/cache/` already works.
- **Refactoring `applyRename` to share infrastructure.** Future cleanup; not part of this spec.

---

### 4. Design

#### 4.1 Snapshot directory layout

```
~/.inat/snapshots/
  20260519T085400Z-company__coding-standards/
    manifest.json
    files/
      .inat/rules/code-quality.md
      CLAUDE.md
```

- Top-level dir name: `<UTC RFC-3339 timestamp, compact>-<sanitized install key>`. The install key's `/` is replaced with `__` (filesystem-safe, unambiguous against the constrained kit-name alphabet `[a-z0-9-]`). Timestamp uses `20060102T150405Z` to avoid `:` in paths (Windows-unsafe). If the target dir already exists (leftover forensic state from a prior aborted run, or a same-second retry), `Capture` appends a `-2`, `-3`, … suffix and retries; existing forensic dirs are NEVER overwritten.
- `manifest.json` schema:

```json
{
  "schemaVersion": 1,
  "installKey": "company/coding-standards",
  "createdAt": "2026-05-19T08:54:00Z",
  "projectDir": "/abs/path/to/project",
  "files": [
    {"relPath": ".inat/rules/code-quality.md"},
    {"relPath": "CLAUDE.md"},
    {"relPath": ".inat/rules/new-rule.md", "absent": true}
  ]
}
```

- `files/` mirrors project-relative paths verbatim. Each captured file's content is copied under `files/<relPath>`. Directories are created on demand. Entries with `"absent": true` carry no `files/<relPath>` content; restore for those entries deletes the file (defensive against the apply having created it before failure).
- `schemaVersion` is `1`. Bumped only on a backwards-incompatible manifest change; `Restore` re-reads the manifest from disk each call and rejects any other version with `errs.KitValidation`.

#### 4.2 What gets captured

For each `applyUpdate` and `applyReplace` invocation, before any disk mutation, build the set of project-relative paths to snapshot:

1. **Delivered files.** Every `Path` in `installed.Delivered` (`apply.go:58, 79`).
2. **Agent target files.** For each active agent (`config.CollectActiveAgents`), `mapping.Rule.Always.Path` and `mapping.Rule.Contextual.Reference` (`go/cli/remove/remove.go:156, 160`). For the built-in `ClaudeCodeProfile`, both resolve to `CLAUDE.md`; collection MUST dedup by resolved path so each on-disk file is captured exactly once.

Capture reads each path's current on-disk bytes verbatim, including any locally-modified content (drift) that `RemoveDeliveredFiles` would skip — the contract is byte-identity restore to pre-apply state, not restore-to-the-cache. Files that do not exist at capture time are recorded with `"absent": true` so restore re-deletes a file the apply created mid-flight. Files outside these two sources are not captured; the contract of `RemoveDeliveredFiles`, `CleanManagedSections`, and `DeliverToAgent` is narrow enough that no other on-disk state is touched by an apply.

#### 4.3 Capture and restore primitive

New package `go/core/snapshot/snapshot.go`. Public surface:

```go
type Snapshot struct {
    Dir string // ~/.inat/snapshots/<ts>-<key>[-N]
}

type Request struct {
    ProjectDir   string
    InstallKey   string
    RelPaths     []string // project-relative; deduped by caller
    HomeOverride string   // honours config.HomeDir() override semantics
}

func Capture(req Request) (*Snapshot, error)
func (s *Snapshot) Restore() error
func (s *Snapshot) Discard() error
```

- `Capture` resolves the snapshots root via `config.HomeDir(homeOverride)`, creates a unique directory (collision-avoidance suffix per §4.1), copies each path's current bytes into `files/<relPath>` or records `"absent": true`, writes the manifest, and returns the `*Snapshot`. On any failure mid-capture, the partially-created dir is `os.RemoveAll`'d before returning `errs.NewSnapshotCaptureFailed(installKey, cause)`. Capture is atomic from the caller's perspective.
- `Restore` re-reads the manifest from disk (so a stale in-memory copy never silently restores against changed metadata), validates `schemaVersion == 1`, then for each entry copies content back (or deletes the file if `absent`). Returns the first failure encountered; the snapshot dir is **NOT** deleted on restore — the caller decides retention.
- `Discard` is `os.RemoveAll(s.Dir)`. Idempotent; missing dir is not an error.

The previously-considered `Role` tag on each captured path is dropped: capture, restore, and discard treat every entry identically, and the manifest already records the project-relative path verbatim. A future spec can add typing if forensic categorisation needs it.

#### 4.4 Apply-phase integration — Update

`applyUpdate` (`apply.go:56-73`) and its delegate `removeAndDeliver` (`apply.go:173-196`) are reshaped. The capture/restore/discard wrap is extracted into a private helper `runWithSnapshot` so `applyReplace` shares the lifecycle code:

```go
func applyUpdate(...) ApplyResult {
    installed := cfg.Installed[a.InstallKey]
    paths, err := collectSnapshotPaths(dir, cfg, installed.Delivered, homeOverride)
    if err != nil { return needsAttention(a.InstallKey, err) }
    return runWithSnapshot(dir, a.InstallKey, paths, homeOverride, func() (ApplyResult, error) {
        delivered, err := removeAndDeliver(dir, cfg, ...)
        if err != nil { return ApplyResult{}, err }
        cfg.Installed[a.InstallKey] = config.InstalledKit{...}
        fmt.Fprintf(os.Stderr, "updated %s\n", a.InstallKey)
        return ApplyResult{InstallKey: a.InstallKey, Kind: ActionUpdate}, nil
    })
}
```

`runWithSnapshot` captures, calls the closure, and on closure error attempts restore (returning `ApplyRestoreFailed` on double-fault, otherwise the original error). On closure success, `Discard` is called; discard failure is a non-fatal stderr warning.

`collectSnapshotPaths` is a private helper in `apply.go` that walks `installed.Delivered` plus every active agent's `mapping.Rule.Always.Path` and `mapping.Rule.Contextual.Reference`, returning a deduped `[]string` of project-relative paths.

The local `snapshot` variable in `applyRename` (`apply.go:151`) is renamed to `prev` to avoid shadowing the imported `snapshot` package alias.

#### 4.5 Apply-phase integration — Replace

`applyReplace` (`apply.go:75-131`) integrates via the same `runWithSnapshot` helper. The closure body runs the existing `RemoveDeliveredFiles` → drift-check → `CleanManagedSections` → `deliverAcrossAgents` sequence. The drift-detection short-circuit at `apply.go:85-100` returns an error from the closure, which `runWithSnapshot` treats as an apply failure and restores from — bringing back the non-drifted files that `RemoveDeliveredFiles` already removed (it leaves drifted files in place, per `remove.go:127-138`). The user-modified drifted file is restored to its on-disk pre-apply bytes (which the snapshot captured before mutation).

The typed-error contract is preserved: `ReplaceInstallFailed` continues to be the install-half failure error class; it now wraps an apply that was rolled back from disk rather than leaving the disk dirty.

#### 4.6 New typed errors

In `go/errs/errs.go`:

```go
SnapshotCaptureFailed  // new Kind
ApplyRestoreFailed     // new Kind

func NewSnapshotCaptureFailed(installKey string, cause error) *Error {
    return &Error{Kind: SnapshotCaptureFailed, Name: installKey, Cause: cause}
}

func NewApplyRestoreFailed(installKey, snapshotDir string, applyErr, restoreErr error) *Error {
    // Cause holds errors.Join(applyErr, restoreErr) so both error chains
    // survive errors.As / errors.Is traversal. Path holds the snapshot dir.
    return &Error{
        Kind: ApplyRestoreFailed, Name: installKey, Path: snapshotDir,
        Cause: errors.Join(applyErr, restoreErr),
    }
}
```

`Error()` strings:
- `SnapshotCaptureFailed`: `"snapshot capture failed for kit '%s': %v"` (installKey, cause)
- `ApplyRestoreFailed`: `"apply failed for kit '%s' and snapshot restore also failed; snapshot kept at %s: %v"` (installKey, path, joined cause)

Both errors carry `Kind` that callers can `errors.As` / `errors.Is` against — same pattern as `ReplaceInstallFailed`. The joined `Cause` on `ApplyRestoreFailed` means `errors.Is(err, errs.ErrFoo)` walks both the apply and restore chains; callers needing to recover a specific typed error from either side can `errors.As` against the joined chain.

The `ReplaceInstallFailed` error message text (`errs.go:158-160`) currently directs users to "run `inat refresh` to recover" the missing old files. Now that restore brings those files back, the suggestion is misleading. The message text is updated to drop the refresh recommendation; the typed-error class (`Kind == ReplaceInstallFailed`) and wrapped-cause behaviour are unchanged.

#### 4.7 Snapshot location resolver

Internal helper `snapshotsRoot(homeOverride string) (string, error)` honours the same `homeOverride` semantics as `config.CollectActiveAgents` (`go/core/config/config.go:235-254`): if `homeOverride != ""` use it directly, else call `config.HomeDir()`. Returns `filepath.Join(<resolved>, ".inat", "snapshots")`. The directory is created on demand inside `Capture` (which uses `os.MkdirAll` so prior `.inat/` ancestors are tolerated).

---

### 5. Compatibility

- **`go/cli/transaction/apply.go`** — `applyUpdate`, `applyReplace`, `removeAndDeliver` reshaped per §4.4 and §4.5. The exported `Apply` signature is unchanged. `removeAndDeliver` may either grow a `*snapshot.Snapshot` parameter (caller-owned snapshot lifecycle) or stay as-is with snapshot handling pushed up to `applyUpdate`. The plan picks the second shape — `apply.go:173-196` becomes a function whose contract is "given a captured snapshot, do the mutation; caller restores/discards." Decision elaborated in plan §1.
- **`go/core/snapshot/snapshot.go`** — new file. `Capture`, `Restore`, `Discard`, types per §4.3. Precedent: existing leaf packages `core/hash`, `core/reserved`, `core/delivery` already establish the leaf-package pattern.
- **`go/core/snapshot/snapshot_test.go`** — new. Round-trip tests against a temp `homeOverride`.
- **`go/errs/errs.go`** — two new `Kind` constants, two `New...` constructors, two `Error()` cases, two `Err...` sentinels. Following the existing `ReplaceInstallFailed` pattern. The `ReplaceInstallFailed` `Error()` message (currently directs to `inat refresh`) is updated to drop the refresh recommendation, since restore now brings the old files back automatically. Typed-error class unchanged.
- **`go/errs/errs_test.go`** — coverage for the two new error variants.
- **`go/cli/transaction/transaction_test.go`** and **`go/cli/transaction/apply_test.go` (new)** — apply-phase tests asserting snapshot create/restore/discard semantics.
- **`go/cli/update/update_test.go`** and **`go/cli/replace/replace_test.go`** — extend existing fixtures with a failure-injection path; assert post-restore byte-identity of delivered + agent target files.
- **`go/main.go`** — no flag changes; no surface change.
- **`.inat/config.json`** — schema unchanged.
- **`~/.inat/`** — `snapshots/` sibling directory added on demand. Cache layout (`~/.inat/cache/`) and global config (`~/.inat/config.json`) unchanged.
- **`.gitignore`** — N/A. The snapshot directory is global, not per-project.
- **`README.md`** — one paragraph under "Recovery from a failed apply", noting the new snapshot directory location and that successful applies leave nothing behind.
- **`CHANGELOG.md`** — `[Unreleased]` gains a `### Added` entry pointing at spec 0021.
- **`docs/specs/0016-kit-reorg-baseline.md`** and **`docs/specs/0017-transactional-update.md`** — single closure line on the respective §3 / §9 deferral: "Closed by spec [0021](0021-apply-snapshot-restore.md)."

---

### 6. Acceptance criteria

1. **Build clean.** `go -C go/ build ./...` exits 0; `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...` all exit 0.
2. **Update happy path leaves no snapshot.** Project with one installed kit; upstream has a new commit; `inat update` runs to success. After: `~/.inat/snapshots/` is empty (or absent). Behaviour byte-identical to today's stderr trace + exit code.
3. **Update apply failure restores delivered files byte-identically.** Inject a failure inside `DeliverToAgent` (e.g., a synthesised permission error on one agent's `Always.Path`). Run `inat update`. Exit non-zero with `KitsNeedAttention`. Hash every previously-delivered file and the affected agent target file: bytes equal to pre-apply. `~/.inat/snapshots/<...>-<key>/` survives for forensics.
4. **Replace install-half failure restores old-kit files.** Inject a step-4 (deliver-only) failure: pre-create the new kit's contextual rule output path (`.inat/rules/<new-rule>.md`) as a non-empty directory before calling `Replace`. Steps 1-3 (`RemoveDeliveredFiles`, `CleanManagedSections`) don't touch this path; step 4's `delivery.DeliverRuleContextual` fails on the directory-not-file write. (Name-collision failures are caught at plan phase per `replace_test.go:209-275` and are NOT install-half failures; this fixture is new.) Run `inat replace`. Exit non-zero with `ReplaceInstallFailed`. **All of `oldKey`'s previously-delivered files are present on disk byte-identical to pre-apply.** The disk config still has `oldKey` (the save-gated invariant from 0017 carries forward). This is the central improvement vs the spec 0017 contract. Snapshot dir survives.
5. **Replace drift abort restores byte-identically.** Fixture: oldKey has three delivered files; mutate the *third* on disk so drift is detected. `RemoveDeliveredFiles` removes files 1 and 2, leaves file 3 (the drifted one) in place per `remove.go:127-138`, then `applyReplace` short-circuits at `apply.go:85-100`. Snapshot restores files 1 and 2 from their pre-apply bytes; file 3 stays as its pre-apply (user-modified) bytes — the snapshot captured them in that state. Exit non-zero with `KitsNeedAttention(1)`. Post-state assertion: hash of all three files equals pre-apply hashes.
6. **Snapshot capture failure aborts cleanly.** Inject a failure during `snapshot.Capture` by placing a regular file at the path `~/.inat/snapshots` so `os.MkdirAll` fails predictably across platforms. Run `inat update`. Exit non-zero with `SnapshotCaptureFailed`. **Zero on-disk mutation occurred** — delivered files and agent target files are byte-identical to pre-call. No snapshot dir on disk.
7. **Restore failure surfaces ApplyRestoreFailed.** Inject an apply failure (per criterion 4) AND a restore failure (e.g., `chmod 000` on `<snapshot>/files/` after capture, before the closure runs). The returned error is `ApplyRestoreFailed`; `Error()` names the snapshot dir path; `errors.As` against the joined chain recovers both the original apply error and the restore error. Snapshot dir survives.
8. **Batch isolation.** Three-kit project, kit A succeeds, kit B's apply fails+restores, kit C succeeds. After: A's snapshot is gone, B's snapshot survives, C's snapshot is gone. The `KitsNeedAttention(1)` error names B; A and C are visible in config as updated.
9. **Rename apply path takes no snapshot.** Multi-kit update where kit A is auto-detected as a rename (spec 0018 case). Snapshot directory is **not** created for A. (Tested by asserting `~/.inat/snapshots/` is empty after a clean rename run.)
10. **Snapshot dir naming is deterministic enough for tests to find.** A test helper enumerates `~/.inat/snapshots/` and asserts the single surviving directory matches `^\d{8}T\d{6}Z-<sanitized-key>$`.
11. **Manifest schema round-trips.** `Capture` then `Restore` then re-read manifest: JSON byte-identical to the just-written one (modulo trailing newline, which is convention-stable).
12. **No on-disk schema change.** Diff `.inat/config.json` byte-by-byte before/after a clean update run, before/after a needs-attention-with-restore run: only the keys the spec mutates change. `InstalledKit` / `DeliveredFile` JSON fields unchanged.
13. **No new direct dependencies.** `go/go.mod` has no new `require` entries between pre- and post-change tree (`go.sum` may legitimately change from `go mod tidy`).
14. **File / function size budgets.** Every new file ≤300 lines; every new function ≤30 lines (CLAUDE.md §3, §4). Both `applyUpdate`/`applyReplace` reshape via the shared `runWithSnapshot` helper to stay inside the 30-line cap; `apply.go` splits into `apply.go` + `apply_snapshot.go` if it crosses 300 lines.
15. **Multi-path capture round-trip.** Snapshot dir contains entries for both a delivered file and an agent target file (different relPaths); after `Restore`, both are byte-identical to pre-capture. Asserts the capture/restore primitive does not regress on path-set diversity.

---

### 7. Testing notes

- **`go/core/snapshot/snapshot_test.go`** — unit tests against a `t.TempDir()` `homeOverride`:
  - Capture-and-discard a single file: snapshot dir created, manifest correct, after discard the dir is gone.
  - Capture-and-restore: mutate the captured file post-capture, call `Restore`, assert byte-identity.
  - Capture-and-restore with `"absent": true`: snapshot a path that does not exist; let the apply create the file; call `Restore`; assert the file is gone again.
  - Multi-path capture: snapshot a mix of delivered + agent-target paths in one call; round-trip restore brings both back.
  - Capture failure mid-flight: place a regular file at `<home>/.inat/snapshots` so `os.MkdirAll` fails; assert `errs.SnapshotCaptureFailed` and no orphan dir on disk.
  - Leftover-dir collision: pre-create `<home>/.inat/snapshots/<expected-ts-key>/` as a non-empty dir before `Capture`; assert the new snapshot lands at `<...>-2/` and the pre-existing forensic dir is untouched.
  - Manifest schemaVersion rejection: after `Capture`, overwrite `manifest.json` with `schemaVersion: 2`; call `Restore` on the same `*Snapshot`; assert `errs.KitValidation`.
- **`go/cli/transaction/apply_test.go`** (new file) — apply-phase tests using real-filesystem fixtures (per `replace_test.go` precedent). Assert the snapshot-discard-on-success and snapshot-restore-on-failure invariants at the `applyUpdate` / `applyReplace` granularity.
- **`go/cli/update/update_test.go`** — extend existing fixtures: trigger a deliver-phase failure via the same fixture style (e.g. pre-create one of the new delivered paths as a directory). Assert post-failure file byte-identity for all delivered + agent target files.
- **`go/cli/replace/replace_test.go`** — new `TestReplaceInstallHalfFailureRestoresOldFiles` (no precedent; the only pre-existing collision test is plan-phase). Drift-without-force test extended with three-delivered-file fixture per acceptance criterion 5.
- **`go/errs/errs_test.go`** — `Error()` string assertions for `SnapshotCaptureFailed` and `ApplyRestoreFailed`; `errors.As` round-trip across the joined chain.

---

### 8. Rollout

1. Spec + plan (this pair) committed first as a sketch.
2. `errs` additions with tests.
3. `core/snapshot` package with tests.
4. `cli/transaction/apply.go` integration with tests.
5. `cli/update` and `cli/replace` test fixture updates (the visible behavioural assertions for criteria 3, 4, 5).
6. CHANGELOG + README + cross-reference closure lines.
7. One MR off `main`, squash-on-merge.

---

### 9. Out-of-scope follow-ups

- **`inat snapshots list / restore / prune` verbs.** A user-facing way to enumerate, manually replay, or expire snapshot dirs. The current contract leaves the directory for forensics on failure; manual recovery is `inat refresh` (re-deliver from cache) — same as today. A dedicated verb is worthwhile once operators ask, but not before.
- **Snapshot integrity checks (hash / sign).** Detect tampering or corruption in the snapshot dir. Out of scope; the snapshot directory shares the trust boundary of the project directory.
- **Snapshot of `applyRename`'s header rewrites.** Rename's in-memory cfg rollback is already sufficient; the multi-agent partial-disk-state risk acknowledged in spec 0018 §4.4 could be closed by extending the snapshot primitive to header rewrites. Cost is real (different file set, different failure shape); defer until the failure mode is observed.
- **Cross-kit shared-file snapshot dedup.** Agent target files (CLAUDE.md) are shared across kits in a batch; today each kit's snapshot captures its own copy. Dedup would save disk for large batches; cost is a more complex restore protocol. Defer until disk consumption is observed.
- **Automated GC of orphaned snapshot dirs.** Today the operator (or a future `inat snapshots prune`) is responsible. A startup-time prune of snapshots older than N days is a viable addition; left to a follow-up.
