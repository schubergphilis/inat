## inat — Transactional update (Plan 0017)

Pairs with [`docs/specs/0017-transactional-update.md`](../specs/0017-transactional-update.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, TDD test order, and verification.

Restructures `inat update` and `inat replace` into explicit plan and apply phases via a new `go/cli/transaction/` package. The plan phase fetches every relevant source, parses kit trees, derives names, and validates name uniqueness across the post-update batch — without writing to disk. The apply phase consumes the plan and performs the existing remove + deliver sequence per kit. Failures during the plan phase no longer leave a kit half-applied; failures during apply are isolated to the affected kit (skip-and-continue at the apply level). The within-kit partial-state risk that spec [0016](../specs/0016-kit-reorg-baseline.md) §3 explicitly accepts is closed for every failure mode except disk-write failures during apply — that residual risk is documented as a non-goal in the new spec.

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Atomicity boundary | Per-kit atomic; the batch is skip-and-continue at both the plan and apply phases. A flaky kit does not block routine refresh of others. |
| Rollback mechanism | Plan-then-apply only. No on-disk snapshot of old delivered files. Disk-write failures during apply remain a residual risk (recovery via `inat refresh`). The cost of a true on-disk rollback was rejected during design grilling. |
| Pipeline scope | Both single-kit `inat update <src/kit>` and multi-kit `inat update` flow through the same primitive (single-kit is a 1-element batch). Single-kit's fail-fast contract is preserved by short-circuiting on the first needs-attention outcome. |
| Replace integration | `inat replace` is reimplemented through the new primitive too. Replace becomes one `TargetReplace` target, its apply phase is a single `Replace` action, the snapshot+restore code from spec 0016 is removed (it's no longer needed; config save is gated on success). |
| Plan output shape | Explicit `ActionKind` enum: `UpToDate \| Update \| Replace \| NeedsAttention`. Spec 0018 will add `Rename` as a fifth variant. |
| Apply failure handling | Skip-and-continue at the apply phase: kit B failing mid-write does not stop kit C's apply. The user receives one error line per failing kit and a final summary footer. |
| Package location | New package at `go/cli/transaction/`. Lives under `cli/` because it does I/O (network + disk); peer to the consumer commands (`cli/update`, `cli/replace`) so the dependency direction is clean. |
| Name uniqueness check | Global post-update across the planned batch. Closes 0016 §1(c) (skill migration between kits in the same update). |
| Reporting format | Phased: fetch progress per source → one-line plan summary → per-kit apply progress → per-kit error lines → final summary. |
| Refresh contract | Unchanged. Refresh remains the documented recovery path for the residual disk-write risk. |
| New typed errors | None. The structural change is *when* errors are constructed, not the catalogue. Existing `errs.Kind` variants suffice. |
| On-disk schema | Unchanged. `Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry` all byte-stable. |
| Module dependencies | Unchanged. Stays within the existing allowlist. |
| Concurrency | None added. Strictly sequential per CLAUDE.md §11. |
| Test fakes | Local bare git repos in `t.TempDir()` for sources; same harness as spec 0016. No live network. |
| Documentation surface | CLI help text byte-identical. README §Update may gain a one-paragraph note describing the plan-summary line; not blocking. CHANGELOG `[Unreleased]` gains a `### Changed` entry pointing at this spec. |

---

### 2. Files

#### New

- `go/cli/transaction/transaction.go` — package doc, top-level `Plan` and `Apply` entry points, the small set of helpers that orchestrate phases. ~80 lines.
- `go/cli/transaction/action.go` — `ActionKind`, `PlannedAction`, `Target`, `TargetKind`, plus tiny accessor helpers (`(a *PlannedAction) IsActionable() bool` etc.). ~60 lines.
- `go/cli/transaction/plan.go` — per-target plan helpers: source grouping (`groupTargetsBySource`), batch fetch (`fetchSources`), per-target classification (`planUpdate`, `planReplace`), global uniqueness check (`checkBatchUniqueness`). ~200 lines.
- `go/cli/transaction/apply.go` — per-action apply helpers: `applyUpdate`, `applyReplace`, plus the small `runRemoveAndDeliver` helper that both share. ~150 lines.
- `go/cli/transaction/transaction_test.go` — unit tests for the plan and apply phases, table-driven across the failure modes spec §6 enumerates. ~400 lines.
- `docs/specs/0017-transactional-update.md` — the contract.
- `docs/plan/0017-transactional-update.md` — this document.

#### Modified

- `go/cli/update/update.go`:
  - `RunUpdate(kitRef, force)` and `Update(dir, kitRef, force, homeOverride)` keep their signatures.
  - Internal pipeline replaced: `LoadConfig` → `buildTargets` → `transaction.Plan` → `printPlanSummary` + `printPlanErrors` → `transaction.Apply` → `printApplyErrors` → `SaveConfig` (if anything applied) → `printFinalSummary`.
  - Single-kit fail-fast preserved by `firstNeedsAttentionError` shortcut at the bottom.
  - Net: file shrinks from 241 → ~120 lines. Most existing helpers (`updateSingle`, `findSource`, `readKit`, `resolveTargets`) are deleted in favour of `transaction.Plan` calls.
- `go/cli/replace/replace.go`:
  - `RunReplace(oldRef, newRef, force)` and `Replace(dir, oldRef, newRef, force, homeOverride)` keep their signatures.
  - Internal pipeline replaced: `LoadConfig` → `parseAndValidate` → build a single-element `[]Target{TargetReplace}` → `transaction.Plan` → check `plan[0].Kind` → `transaction.Apply` → check `results[0].Kind` → `SaveConfig`.
  - `snapshotInstalled`, `runRemoveHalf`, `runInstallHalf`, `rollbackOnDrift` are deleted. The snapshot is no longer needed because plan does not mutate config and apply mutates only on success.
  - Net: file shrinks from 181 → ~80 lines.
- `go/cli/update/update_test.go`:
  - Existing happy-path / drift / multi-kit / single-kit-fail-fast tests are preserved.
  - Stderr-format assertions updated to expect the new plan-summary line and the unchanged final summary.
  - One new test added: cross-kit skill migration succeeds (regression test for 0016 §1(c) being closed).
  - One new test added: plan-phase failure leaves zero on-disk files touched (the central guarantee 0017 adds).
- `go/cli/replace/replace_test.go`:
  - Existing happy-path / drift / install-failure-rollback tests are preserved.
  - Snapshot-related assertions changed to "config save was gated on success" — the disk file is never rewritten on failure, achieving the same end-state without an in-memory snapshot.
  - Stderr-format assertions updated to include the plan-summary line.
- `CHANGELOG.md` — `[Unreleased]` gets a `### Changed` entry per spec §8.

#### Untouched

- `go/cli/install/install.go` — `InstallSteps`, `DeliverToAgent`, `OpenCachedTree`, `ResolveCachePath` all unchanged. The transaction's apply phase calls `DeliverToAgent` per agent identically to today.
- `go/cli/remove/remove.go` — `RemoveSteps`, `RemoveDeliveredFiles`, `CleanManagedSections`, `ResolveInstalledRef` all unchanged. Apply's remove half calls them identically.
- `go/cli/refresh/`, `go/cli/sync/`, `go/cli/rename/`, `go/cli/init/`, `go/cli/agents/`, `go/cli/sources/`, `go/cli/status/`, `go/cli/validate/`, `go/cli/self/` — all unchanged.
- `go/core/*` — all pure-logic packages unchanged.
- `go/git/source/`, `go/git/cache/`, `go/agents/` — unchanged.
- `go/errs/errs.go` — no new variants. The constructors `NewKitNotFound`, `NewKitNotInstalled`, `NewSourceNotFound`, `NewKitValidation`, `NewDuplicateName`, `NewKitsNeedAttention`, `NewReplaceInstallFailed` are reused.
- `go/main.go` — no new commands.
- `go/go.mod`, `go/go.sum` — no new modules.
- `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1`, `ci/`, the GitLab CI config — release plumbing unchanged.
- `VERSION` — bump is the release captain's call when cutting the next tag.
- `rust/` — frozen archive, untouched per CLAUDE.md.

---

### 3. Implementation order

TDD-first per CLAUDE.md §1. Each step writes failing tests, then minimal implementation to pass them, then refactor. Each commit MUST pass `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...`.

1. **Spec + plan land first.** This commit drops the spec/plan pair plus a stub `go/cli/transaction/transaction.go` containing only the package declaration and a doc comment. The pre-commit hook is satisfied by the spec/plan presence; the stub keeps `go build ./...` green.
2. **`go/cli/transaction/action.go` + types tests.** Add `ActionKind`, `PlannedAction`, `Target`, `TargetKind`. Tests in `transaction_test.go` verify zero-value behaviour, pretty-printing of the enum, and the `IsActionable` accessor. Pure types — no I/O.
3. **`Plan` skeleton — `UpToDate` and `KitNotInstalled` paths.** Implement enough of `Plan` to handle: empty target list (returns empty plan); single `TargetUpdate` target whose recorded tree SHA matches the upstream tree (returns one `UpToDate` action); single `TargetUpdate` target whose `InstallKey` is not in `cfg.Installed` (returns one `NeedsAttention` action with `KitNotInstalled`). Use the existing fake-source harness from `cli/install_test.go`. After this step `Plan` is callable but does not yet know how to walk + parse trees.
4. **`Plan` — happy-path `Update` action.** Add the walk + parse + derive-names path: when the upstream tree differs from recorded, emit an `Update` action populated with `NewTreeSHA`, `NewKitCfg`, `NewFiles`, `NewNames`. Tests: a fake source where the kit's tree changed; assert the action's fields are correct and `cfg.Installed` is unchanged.
5. **`Plan` — `Replace` action.** Add `TargetReplace` handling: validate old/new install state, fetch new kit's tree from `NewRef`'s source, populate the action's fields. Tests: same-source replace, cross-source replace, missing-old rejection, existing-new rejection.
6. **`Plan` — global uniqueness check.** Add `checkBatchUniqueness`: build the post-update name set for the entire batch (using planned `NewNames` for actions in the batch, recorded `cfg.Installed[k].Names` otherwise), enforce uniqueness, convert collisions to `NeedsAttention(DuplicateName)`. Tests: cross-kit `foo` migration succeeds (the regression test for 0016 §1(c)); two kits adding `foo` in the same batch both convert to needs-attention; a kit colliding with an unrelated installed kit converts to needs-attention.
7. **`Plan` — fetch-failure handling.** When `source.CloneOrFetch` or `FetchRegistryAt` fails for a source, every kit from that source converts to `NeedsAttention` with the typed error. Tests: a fake source pointed at a non-existent path; assert all targets from it become needs-attention with the underlying error preserved.
8. **`Plan` — fetch-progress reporting.** Emit `fetching <source>...` to stderr per source. Tests capture stderr and assert the format. The progress line is the first user-visible signal that work is happening.
9. **`Apply` skeleton.** Implement `Apply` with the trivial paths: `UpToDate` returns success silently; `NeedsAttention` returns the captured error in the `ApplyResult`. Tests: pass a plan composed entirely of `UpToDate` and `NeedsAttention` actions; assert `ApplyResult` shape and that no file under `dir` is touched.
10. **`Apply` — `Update` action happy path.** Wire up `RemoveDeliveredFiles` + `CleanManagedSections` + `DeliverToAgent` per agent + `cfg.Installed[InstallKey] = ...`. Reuse the existing helpers from `cli/install` and `cli/remove` byte-identically. Tests: fake sources, install-then-update flow; assert `cfg.Installed` reflects the new tree, delivered files exist with the new content, agent managed sections updated.
11. **`Apply` — `Replace` action happy path.** Same shape as `Update` but install-key changes from old to new. Cross-source and same-source variants. Tests cover both.
12. **`Apply` — apply-phase failure isolation.** Test injects a failing target (e.g. a target's deliver path is read-only). Assert: that target's `ApplyResult.Kind == ActionNeedsAttention` with the typed error; subsequent targets still apply cleanly; `cfg.Installed` reflects the cleanly-applied targets and the unchanged state for the failing target.
13. **`Apply` — apply-phase progress reporting.** Emit `updated <key>` and `replaced <oldKey> → <newKey>` per success. Tests assert stderr format.
14. **`cli/update/update.go` cutover.** Reimplement `RunUpdate` and `Update` on top of `transaction.Plan` + `transaction.Apply`. Delete `updateSingle`, `findSource`, `readKit`, `resolveTargets`. Single-kit fail-fast preserved by the `firstNeedsAttentionError` shortcut. The existing `cli/update/update_test.go` is the regression gate — every test case must pass with the new pipeline before this step is committed. Update stderr-format assertions to include the plan-summary line.
15. **`cli/update/update_test.go` extensions.** Add the cross-kit migration test (closes 0016 §1(c)) and the plan-phase-failure-leaves-no-disk-state test (the central new guarantee).
16. **`cli/replace/replace.go` cutover.** Reimplement `RunReplace` and `Replace` on top of `transaction.Plan` + `transaction.Apply`. Delete `snapshotInstalled`, `runRemoveHalf`, `runInstallHalf`, `rollbackOnDrift`. The existing tests are the regression gate; update stderr assertions to expect the plan-summary line.
17. **`cli/replace/replace_test.go` adjustments.** Snapshot-related tests change their assertion wording from "snapshot restored" to "config save gated on success — disk file unchanged after failed replace." The end-state behaviour and the typed error class are unchanged.
18. **CHANGELOG entry.** `[Unreleased]` gets the `### Changed` entry from spec §8.
19. **README §Update note (optional, non-blocking).** Single paragraph describing the plan-summary line. Land if it fits the implementation arc; defer if not.

After step 17, the pre-commit hook is the gate: `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all pass.

---

### 4. Verification

#### Local (test suite)

1. `go -C go/ test ./cli/transaction/...` — table-driven plan + apply coverage; failure-mode isolation; cross-kit migration; reporting format.
2. `go -C go/ test ./cli/update/...` — every existing scenario plus the two new ones (cross-kit migration, plan-phase-failure-leaves-no-disk-state).
3. `go -C go/ test ./cli/replace/...` — every existing scenario with updated stderr / config-save assertions.
4. `go -C go/ test ./cli/install/...` — existing tests must remain green; `InstallSteps` is unchanged but it is consumed indirectly via `transaction.Apply`'s `DeliverToAgent` calls.
5. `go -C go/ test ./cli/remove/...` — existing tests must remain green; `RemoveSteps`, `RemoveDeliveredFiles`, `CleanManagedSections` are all unchanged.
6. `go -C go/ test ./errs/...` — no new error variants; existing tests are sufficient.
7. `gofmt -l go/` — silent.
8. `go -C go/ vet ./...` — silent.
9. `staticcheck ./...` (run from repo root with `-checks=all`) — silent.
10. `go -C go/ test ./...` — full suite remains green.

All gates are what the pre-commit hook (`.githooks/pre-commit`) and CI already enforce; no new tooling.

#### Manual smoke (Linux x86_64, the dev laptop)

11. **Multi-kit happy path.** Two configured fake sources, three kits (one up-to-date, two with new content). Run `inat update`. Stderr shows the phased reporting (fetch → plan summary → per-kit apply progress → final summary). Exit 0.
12. **Plan-phase failure isolation.** Three-kit project where one kit's source URL is broken. Run `inat update`. The broken-source kit's plan fails; the other two kits' plans + applies run cleanly. Exit non-zero with `KitsNeedAttention(1)`. **No file under the broken kit's delivery paths is modified** — verify via mtime comparison before/after.
13. **Cross-kit migration.** Two fake sources; install kits A and B; locally edit upstream so that skill `foo` moves from kitA to kitB in the same update batch. Run `inat update`. Today (spec 0016) this fails; under 0017 it succeeds. Verify `cfg.Installed[A].Names` no longer contains `foo` and `cfg.Installed[B].Names` does. The agent's managed sections reflect the migration.
14. **Replace happy path.** Cross-source replace via `inat replace company/foo vendor/foo`. Exit 0. `cfg.Installed[vendor/foo]` exists, `cfg.Installed[company/foo]` deleted, agent files updated.
15. **Replace install-half failure preserves config.** Inject an install-half failure (third kit's name collides with replace target's derived names). Run `inat replace`. Exit non-zero with `ReplaceInstallFailed`. Read `.inat/config.json` from disk: `oldKey` still present (config save was gated). Run `inat refresh` to recover the on-disk delivered files for `oldKey` from cache.
16. **Single-kit fail-fast preserved.** `inat update bogus/kit` returns `KitNotInstalled` byte-identically. `inat update src/<unreachable>` returns the underlying network error. Both exit non-zero with the typed error, not `KitsNeedAttention`.

#### CI smoke

17. The pre-commit hook (`.githooks/pre-commit`) runs all four gates locally before any commit; CI re-runs them on push. No new CI job needed.

---

### 5. Risks & mitigations

- **Behavioural regression in `cli/update` from the cutover.** The reimplementation is structurally a rewrite of the per-kit loop and the single-kit-vs-multi-kit branch. Mitigation: every existing `cli/update/update_test.go` test case is the regression gate. The implementation does not move forward (steps 14+) until each existing test passes against the new pipeline. Stderr-format assertions are the only pre-existing tests that need updating; behavioural assertions (config state, exit code, typed error) remain unchanged.
- **Behavioural regression in `cli/replace` from the cutover.** The replace path is a single-target `transaction.Plan` + `transaction.Apply` invocation. The snapshot+restore code from spec 0016 is removed in favour of "config save gated on success." Mitigation: the existing `cli/replace/replace_test.go` is the regression gate; the test that previously asserted "snapshot restored `cfg.Installed[oldKey]`" is updated to assert "disk config file unchanged after failure," which is the same end-state observed differently. Other tests (drift, force, happy paths) are byte-identical assertions.
- **Apply-phase residual partial-state risk (disk-write failures).** A kit's apply phase can fail mid-write (ENOSPC, permission-denied, partial deliver) and leave the kit half-applied. Mitigation: documented in spec §3 as a non-goal; recovery via `inat refresh` is the user-facing answer; the new `KitsNeedAttention(N)` typed error and per-kit error line guide the user to investigate. A future spec can layer on-disk snapshot/restore if motivated.
- **Replace's install-half-failure still leaves old delivered files removed on disk.** Inherited from spec 0016 §3 and reaffirmed under the new primitive. Mitigation: same as above — `inat refresh` restores the files from cache; the `ReplaceInstallFailed` typed error continues to point at this recovery path.
- **Global uniqueness check edge cases.** The new check considers planned `NewNames` for kits in the batch and recorded `cfg.Installed[k].Names` for kits not in the batch. An edge case: a kit not in the batch has names that collide with a kit in the batch's planned names — the conflict surfaces as a `DuplicateName` on the in-batch kit. This is correct (the in-batch kit is what's changing) and matches the existing single-kit `CheckNameUniqueness` semantics. Mitigation: explicit table-driven test for this edge case.
- **Reporting-format churn affects scripts that grep stderr.** The new plan-summary line is added; the per-kit progress lines are unchanged; the final summary is unchanged. Mitigation: the new line is structured (`planned: N update, M up-to-date, K need attention`) and pre-existing scripts that grep for `updated <key>` or the final summary footer continue to match. The CHANGELOG entry calls out the new line.
- **Source-fetch deduplication regression.** Today's update fetches each source per-kit-iteration (multiple times if multiple kits share a source); the new primitive fetches once per source per `Plan` call. This is a small performance improvement, not a behavioural change, but tests against fixtures that count `CloneOrFetch` calls would observe the difference. Mitigation: the existing tests do not count fetches; if a future test does, it should assert "at most once per source" rather than "exactly N per source."
- **Doc drift between spec / plan and implementation.** Mitigation: the pre-commit hook's spec-presence check catches missing spec/plan pairs; CI's `gofmt`/`vet`/`staticcheck`/`test` catches code drift; the spec / plan / code triad is reviewed in the same MR.

---

### 6. Out-of-scope follow-ups

Same as spec §9. Re-listed here for the rollout-doc audience:

- **Spec 0018 — auto-rename detection.** Layer a `Rename` action variant onto `transaction.PlannedAction`; teach `Plan` to detect tree-SHA matches at different names in the source registry; the apply phase calls `cli/rename` semantics for `Rename` actions.
- **On-disk snapshot/restore for the apply phase.** Closes the disk-write residual risk and replace's install-half-failure files-gone limitation simultaneously.
- **`inat update --plan` / `inat update --dry-run`.** Run `Plan` and emit a JSON or human-readable plan to stdout without applying. Useful for CI gating and review workflows.
- **Migrate `inat refresh` through the transaction primitive.** Refresh's contract is different (no fetch, just re-deliver from cache), but the partial-state recovery story would unify if it shared the apply phase. Out of scope here.
- **Concurrent plan / fetch.** Goroutines for parallel source fetches. CLAUDE.md §11 forbids it for now.
