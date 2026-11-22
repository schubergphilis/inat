## inat — Auto-rename detection during update (Plan 0018)

Pairs with [`docs/specs/0018-kit-reorg-auto-rename.md`](../specs/0018-kit-reorg-auto-rename.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, TDD test order, and verification.

Lands the localized addition the spec-0017 substrate was designed for: during `inat update`, the plan phase auto-detects pure-`git mv` kit renames in the same source (recorded `Installed.Tree` SHA appears at exactly one different name in the source's registry) and the apply phase rewrites the install key + always-rule managed-section headers — same semantics as the explicit `inat rename` verb (spec 0019). Strict tree-SHA equality only; cross-source moves and content-drift cases keep their existing routes (`inat sync` + `inat replace` / `inat rename`). The manual `inat sync → inat rename` round-trip is eliminated for the dominant pure-`git mv` case.

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Detection classifier | Strict tree-SHA equality. Same classifier `inat sync` already uses for case (b) and `inat rename` enforces. No fuzzy / similarity / `git --follow` heuristic. |
| Detection scope | In-source only — auto-detect renames where the recorded SHA appears at a different name in the **same** source's registry. Cross-source moves stay manual via `inat sync` + `inat replace`. |
| Default behaviour | Auto-detection is **on by default** for `inat update`. No `--auto-rename` / `--no-auto-rename` flag. Escape hatch (audit-before-apply) is the existing read-only `inat sync`. |
| Detection trigger | Runs only on the `kit-absent-from-registry` branch inside `planUpdate`. Every other plan path is unchanged (zero extra cost). |
| Detection during replace | Never. `planReplace` is unchanged — replace targets are explicit cross-source mutations, not candidates for auto-detection. |
| Index strategy | Lazy per-source. The tree-SHA index for a given source is built only when at least one target from that source encounters the kit-absent-from-registry branch. Scoped to a single `Plan` invocation. |
| Index-build error policy | A `FetchKitTreeAt` failure mid-build is localised to auto-detection — the partial index is retained, `built` is set true (no retry within the invocation), and targets whose recorded SHA happens to match the unindexed kit fall through to today's `KitNotFound`. The error does not poison every target from the source. Acceptance criterion §6 #11c covers it. |
| Ambiguity handling | When the index shows the recorded SHA at 2+ different names: refuse to pick. New typed error `RenameAmbiguous(installKey, candidates)` directs the user to `inat sync` and `inat rename`. |
| File-size discipline | `plan.go` is currently 333 lines (over the 300-line target, under the 400-line hard max per CLAUDE.md §3). The lazy-index logic lives in a new `plan_index.go` (~80 lines); `plan.go` gains only the small dispatch edit (~20 lines). Keeps both files within the soft target. |
| Single-kit update preservation | `inat update <src/old-name>` against a kit that was renamed upstream succeeds end-to-end (rename auto-applied, command exits zero). `KitNotFound` continues to fire only for genuine cases (deletion, ambiguity). |
| Apply integration | New `ActionRename` variant in `ActionKind`; new `applyRename` arm in `applyOne`. No call to `RemoveDeliveredFiles` / `DeliverToAgent` (on-disk delivered files are byte-identical at both keys — same tree). |
| Header rewrite reuse | Export `cli/rename`'s private `rewriteAlwaysHeaders` helper as `RewriteAlwaysHeaders`. Transaction package imports it. One-way dependency: `cli/transaction → cli/rename`. |
| Rollback on apply-phase header failure | In-memory rollback (restore the snapshotted `cfg.Installed[oldKey]`); caller's gated `SaveConfig` keeps the disk file unchanged. Same end-state as spec 0017's replace failure path. |
| Reporting format | Plan-summary line and final summary gain a `rename` term (between `update` and `up-to-date`). Per-apply line: `renamed <oldKey> → <newKey>` (mirrors spec 0019's wording). |
| New typed errors | One: `RenameAmbiguous(installKey, candidates []string)`. Constructor + `Error()` case + `errs.Is` semantics mirror the existing pattern. |
| On-disk schema | Unchanged. `Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry` byte-stable. |
| Module dependencies | Unchanged. No additions to `go.mod`. |
| Concurrency | None added. Strictly sequential per CLAUDE.md §11. |
| Test fakes | Local bare git repos in `t.TempDir()`; same harness as specs 0016 / 0017 / 0019. No live network. |
| Documentation surface | README's Reorganization §rename gains a one-sentence note; CHANGELOG `[Unreleased]` gains a `### Changed` entry; the §3/§9 forward-references in specs 0016 and 0017 close their "spec 0018" placeholders. |

---

### 2. Files

#### New

- `docs/specs/0018-kit-reorg-auto-rename.md` — the contract.
- `docs/plan/0018-kit-reorg-auto-rename.md` — this document.
- `go/cli/transaction/plan_index.go` — `sourceSHAIndex` type + `build` / `candidatesForSHA` methods + lazy-threading helpers + the package-private `buildHook` test seam. ~80 lines. Carved out of `plan.go` to keep both files within the 300-line soft target.

#### Modified

- `go/cli/transaction/action.go`:
  - Add `ActionRename` to the `ActionKind` const block (after `ActionNeedsAttention` to preserve existing iota values).
  - Add `case ActionRename: return "rename"` to `String()`.
  - Add `ActionRename` to `IsActionable`.
  - Net: ~10 lines added.

- `go/cli/transaction/plan.go`:
  - Thread an `indexes map[string]*sourceSHAIndex` (defined in the new `plan_index.go`) through `Plan` → `planSingle` → `planUpdate`. Indexes constructed lazily on first miss per source.
  - Rewrite the `kit-absent-from-registry` branch in `planUpdate` to dispatch on `len(candidates)` — 0 → `KitNotFound` (today), 1 → `ActionRename`, 2+ → `RenameAmbiguous`.
  - Add `ActionRename` arm to `buildPostUpdateNameOwners` (treats the rename like a key change with byte-identical names; same shape as `ActionReplace`'s arm).
  - Add `ActionRename` case to the `ownKey` switch at the collision pass (currently lines 74-77): `ownKey = a.NewKey` for `ActionRename`, mirroring the existing `ActionReplace` arm. Without this, a Rename action self-collides on its own NewNames at the new key.
  - Net: ~20 lines added inside `plan.go` (the type/method-heavy code moves to `plan_index.go`).

- `go/cli/transaction/apply.go`:
  - Add `case ActionRename: return applyRename(dir, cfg, a, homeOverride)` to `applyOne`.
  - New `applyRename` function: snapshot → mutate `cfg.Installed` → call `rename.RewriteAlwaysHeaders` → on failure rollback in memory → emit `renamed <oldKey> → <newKey>` to stderr.
  - Net: ~40 lines added.

- `go/cli/transaction/transaction_test.go`:
  - Table-driven detection tests: tree-SHA-identical rename happy path; ambiguity; tree-SHA mismatch (no detection); cross-source (no detection).
  - Apply-phase tests: rename happy path (cfg + delivered files + header check); header-rewrite-failure rollback (single-agent); multi-agent partial-rewrite failure leaves disk drift, recovered by `inat refresh` (acceptance §6 #11a).
  - Lazy-index instrumentation: clean batch builds zero indices; one missing kit builds exactly one index; `sourceSHAIndex.build` mid-fetch error is localised — partial index retained, unindexed kit's SHA misses fall through to `KitNotFound` (acceptance §6 #11c).
  - Single-kit auto-rename test (`inat update src/old-name` succeeds end-to-end) and single-kit `KitNotFound` preservation (acceptance §6 #11b).
  - Rename self-collision NOT fired (asserts the `ownKey = a.NewKey` fix for `ActionRename`).
  - Cross-action collision test (rename + update naming clash).
  - Net: ~280 lines added.

- `go/cli/rename/rename.go`:
  - Rename the private `rewriteAlwaysHeaders` to exported `RewriteAlwaysHeaders`. Function body unchanged. Update the call site inside `Rename`.
  - Net: ~2 lines changed (rename + call site).

- `go/cli/update/update.go`:
  - Extend the plan-summary formatting helper to include the `rename` count between `update` and `up-to-date`.
  - Extend the final summary helper similarly.
  - Net: ~10 lines changed.

- `go/cli/update/update_test.go`:
  - Three new tests: auto-rename happy path (asserts plan-summary line + per-apply line + final summary + cfg state + header content); `inat update <src/old-name>` single-kit auto-rename; ambiguity surfaces `RenameAmbiguous` and exits non-zero with `KitsNeedAttention(1)`.
  - Existing stderr-format assertions updated to include the new `rename` term in the summary lines.
  - Net: ~150 lines added (mostly new tests).

- `go/errs/errs.go`:
  - Extend the `Kind` iota block with one variant: `RenameAmbiguous`, appended after the existing `ReplaceInstallFailed` entry. No "Kind" prefix on the identifier — the file's convention is bare names (`KitNotFound`, `RenameTreeMismatch`, …).
  - Sentinel variable: `ErrRenameAmbiguous = &Error{Kind: RenameAmbiguous}`, appended to the existing `var (...)` block.
  - Constructor: `NewRenameAmbiguous(installKey string, candidates []string) *Error` — returns `*Error` (matching every other `NewXxx` in the file); sorts candidates alphabetically; packs them into the existing `Reason` field as a comma-separated string. No new struct fields are added to `Error`.
  - `Error()` switch gains a `case RenameAmbiguous` arm with the message format from spec §4.8.
  - Adds `sort` and `strings` to the `errs.go` import block (the current import block has only `errors` and `fmt`).
  - Net: ~15 lines added.

- `go/errs/errs_test.go`:
  - Format-string assertion + `errors.Is` check for `RenameAmbiguous`.
  - Net: ~20 lines added.

- `README.md`:
  - The Reorganization subsection's `rename` paragraph gains a final sentence about auto-application during update.
  - Net: 1 sentence.

- `CHANGELOG.md`:
  - `[Unreleased]` gains the `### Changed` entry from spec §8.
  - Net: ~10 lines (one entry).

- `docs/specs/0016-kit-reorg-baseline.md`:
  - Documentation-only edits to an already-shipped spec: the §3 non-goal "Auto-applying detected renames during `update`" and the §9 out-of-scope follow-up "Spec 0018 — auto-rename detection" each gain a "Closed by spec [0018]..." forward-reference. The text itself stays as historical context.
  - Net: 2 sentences.

- `docs/specs/0017-transactional-update.md`:
  - Same documentation-only forward-reference for the §3 "Auto-applying detected renames during update" non-goal and the §9 out-of-scope follow-up.
  - Net: 2 sentences.

#### Untouched

- `go/cli/install/install.go` — `InstallSteps` / `DeliverToAgent` / `ResolveCachePath` / `OpenCachedTree` all unchanged.
- `go/cli/remove/remove.go` — `RemoveSteps` / `RemoveDeliveredFiles` / `CleanManagedSections` / `ResolveInstalledRef` unchanged.
- `go/cli/replace/replace.go` — single-target replace flow unchanged. `TargetReplace` plans do not auto-detect rename (spec §4.4 explicit).
- `go/cli/refresh/`, `go/cli/sync/`, `go/cli/init/`, `go/cli/agents/`, `go/cli/sources/`, `go/cli/status/`, `go/cli/validate/`, `go/cli/self/` — all unchanged.
- `go/core/*` — all pure-logic packages unchanged. `config.CheckNameUniqueness`, `kit.DeriveNames`, `delivery.RenameKitBlocks`, the managed-section parser / renderer all unchanged.
- `go/git/source/`, `go/git/cache/`, `go/agents/` — unchanged.
- `go/main.go` — no new commands, no new flags. Cobra wiring unchanged.
- `go/go.mod`, `go/go.sum` — no new modules.
- `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1`, `ci/`, the GitLab CI config — release plumbing unchanged.
- `VERSION` — bump is the release captain's call when cutting the next tag.
- `rust/` — frozen archive, untouched per CLAUDE.md.

---

### 3. Implementation order

TDD-first per CLAUDE.md §1. Each step writes failing tests, then minimal implementation to pass them, then refactor. Each commit MUST pass `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...`.

1. **Spec + plan land first.** This commit drops the spec/plan pair plus the documentation-only forward-references in specs 0016 and 0017. The pre-commit hook's spec-presence check is satisfied; no Go change yet. Build remains green.
2. **`go/errs/errs.go` extension.** Add `RenameAmbiguous` kind, sentinel, constructor, `Error()` case. Tests in `errs_test.go` verify the format string and `errors.Is` against the sentinel.
3. **`go/cli/transaction/action.go` extension.** Add `ActionRename` to the const block; extend `String()` and `IsActionable`. Tests in `transaction_test.go` verify the variant's string form and that `IsActionable` returns true.
4. **Export `rename.RewriteAlwaysHeaders`.** Mechanical rename of the private helper. Call site inside `Rename` updated. Existing `cli/rename/rename_test.go` is the regression gate — every test must pass byte-identically.
5. **`sourceSHAIndex` skeleton + `build` method.** New type in `plan_index.go` (new file). Tests: a fake source with N kits; `build` populates the index correctly; tree SHAs are deduped into the `[]string` value for the ambiguity case.
6. **`candidatesForSHA` + lazy build + `buildHook` test seam.** Tests: index built exactly once across repeated lookups; never built when no lookup happens; `buildHook` increments observed via package-private set. Mid-fetch error path: build fails on kit-K; partial index retained for kits 1..K-1; `built = true` so retries do not re-execute the failing fetch; targets whose recorded SHA matches the unindexed kit-K fall through to `KitNotFound`.
7. **`planUpdate` kit-absent branch rewrite.** Thread `indexes map[string]*sourceSHAIndex` through `Plan` → `planSingle` → `planUpdate`. Replace the `KitNotFound` terminator with a `len(candidates)` dispatch. Tests: zero candidates → `KitNotFound` preserved; one candidate → `ActionRename` with the correct `NewKey` / `NewNames`; two candidates → `RenameAmbiguous` with sorted-alphabetically candidates list.
8. **`buildPostUpdateNameOwners` ActionRename arm.** Tests: a rename action contributes its names at the new key (not the old); cross-action collision between a rename and an unrelated update converts the conflict to `DuplicateName` via the existing pass.
9. **`applyRename` skeleton.** Snapshot → mutate cfg → call `rename.RewriteAlwaysHeaders` → success path emits `renamed <oldKey> → <newKey>`. Tests: rename happy path with a single active agent (`claude-code`); assert `cfg.Installed` reflects the new key; assert `CLAUDE.md` headers rewritten; assert no on-disk file under the project was modified other than `CLAUDE.md`.
10. **`applyRename` rollback path.** Inject a header-rewrite failure (chmod 000 on the agent target, or make it a directory). Assert: `ActionNeedsAttention` returned; in-memory `cfg.Installed[oldKey]` restored byte-identically; `cfg.Installed[newKey]` absent.
10a. **Multi-agent partial-disk state.** Two active agents whose always-targets both carry the renamed kit's block. Inject failure on the second agent's rewrite (e.g. chmod 000 the second target before `applyRename` runs). Assert: `ActionNeedsAttention` returned; in-memory `cfg.Installed[oldKey]` restored; **first agent's `CLAUDE.md` on disk references `newKey` (the partial state)**; disk config file unchanged. Then run `inat refresh` from the test; assert the first agent's header rewinds to `oldKey/...` (recovery path). Acceptance §6 #11a.
11. **Lazy-index instrumentation tests.** Wrap `sourceSHAIndex.build` via a testable seam (a package-private hook exposed only to tests). Assertions: clean batch → zero builds; one missing kit → exactly one build for that source; multi-kit batch from a single source with multiple misses → still one build for that source.
12. **`cli/update/update.go` summary-line extension.** Update the plan-summary and final-summary formatters to include the `rename` count. Tests in `update_test.go` capture stderr and assert exact format.
13. **End-to-end `inat update` auto-rename tests.** Three new tests in `update_test.go`:
    - Multi-kit happy path with one rename (asserts the full stderr sequence + cfg + delivered files + headers).
    - Single-kit `inat update <src/old-name>` succeeds end-to-end via auto-rename.
    - Ambiguity exits non-zero with `KitsNeedAttention(1)` and stderr contains the `RenameAmbiguous` message.
14. **Forward-reference doc edits.** Specs 0016 §3 / §9 and 0017 §3 / §9 each gain a "Closed by spec 0018" line. Two-sentence edits to already-shipped specs.
15. **README + CHANGELOG.** README's Reorganization §rename paragraph gains the auto-application sentence; `[Unreleased]` gains the `### Changed` entry per spec §8.

After step 15, the pre-commit hook is the gate: `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all pass.

---

### 4. Verification

#### Local (test suite)

1. `go -C go/ test ./cli/transaction/...` — table-driven detection + apply coverage; ambiguity / cross-source / lazy-index / rollback / cross-action collision.
2. `go -C go/ test ./cli/update/...` — every existing scenario plus the three new auto-rename tests; updated summary-format assertions.
3. `go -C go/ test ./cli/replace/...` — unchanged; existing tests must remain green (no auto-detection on replace targets).
4. `go -C go/ test ./cli/rename/...` — unchanged after the `RewriteAlwaysHeaders` export refactor.
5. `go -C go/ test ./cli/sync/...` — unchanged.
6. `go -C go/ test ./cli/install/...`, `./cli/remove/...` — unchanged.
7. `go -C go/ test ./errs/...` — covers the new `RenameAmbiguous` kind.
8. `gofmt -l go/` — silent.
9. `go -C go/ vet ./...` — silent.
10. `staticcheck ./...` (run from repo root with `-checks=all`) — silent.
11. `go -C go/ test ./...` — full suite green.

All gates are what the pre-commit hook (`.githooks/pre-commit`) and CI already enforce; no new tooling.

#### Manual smoke (Linux x86_64)

12. **In-source rename smoke.** Stage a local bare repo as a fake source with kit `foo`. Stage an inat project; `inat install company/foo`. In the source clone: `git mv kits/foo kits/foo-renamed`, swap the registry entry, commit, push. Run `inat update`: expect the phased reporting with `1 rename` in the plan summary and `renamed company/foo → company/foo-renamed` in the per-apply lines. Inspect `CLAUDE.md`: the always-block header now reads `company/foo-renamed/...`. Inspect `.inat/config.json`: `Installed` has `company/foo-renamed`, not `company/foo`. Re-run `inat update`: no work, `0 update, 0 rename, 1 up-to-date, 0 need attention`.
13. **Single-kit auto-rename smoke.** From the post-12 state, do another in-source rename: `git mv kits/foo-renamed kits/foo-final`, swap registry, commit, push. Run `inat update company/foo-renamed` (typing the old name explicitly). Expect success; cfg reflects `company/foo-final`. Repeat with a kit that's actually been deleted (no SHA match) and assert `KitNotFound` is the typed error, not `KitsNeedAttention`.
14. **Tree-drift smoke.** From the post-13 state, in the source clone: `git mv kits/foo-final kits/foo-drift`, edit one file under the kit, commit, push. Run `inat update`. Expect `0 update, 0 rename, 0 up-to-date, 1 need attention` and the per-apply line shows `KitNotFound` for `company/foo-final`. Verify `.inat/config.json` unchanged and no file on disk was touched other than stderr.
15. **Ambiguity smoke.** From the post-12 state (kit installed as `company/foo-renamed`), in the source: `cp -r kits/foo-renamed kits/foo-copy`, add `foo-copy` to registry, commit, push. Run `inat update`. Expect `1 need attention` with the `RenameAmbiguous` message naming both `foo-copy` and `foo-renamed`. Verify cfg unchanged.
16. **Cross-source-move smoke.** From scratch, stage two sources A and B with the same kit content under different source names. Install from A. Delete from A, push. Run `inat update`: expect `KitNotFound` (no auto-rename; cross-source detection deferred to `inat sync`). Run `inat sync`: case (c) suggestion to `inat replace`. Run `inat replace`: succeeds (unchanged from spec 0017).
17. **Mixed-batch smoke.** Three kits installed: kitA renamed in-source; kitB content-edited (normal update); kitC up-to-date. Run `inat update`. Expect `planned: 1 update, 1 rename, 1 up-to-date, 0 need attention` and per-apply lines in the correct order. Verify cfg + on-disk state for all three.

#### CI smoke

18. The pre-commit hook (`.githooks/pre-commit`) runs all four gates locally before any commit; CI re-runs them on push. No new CI job needed.

---

### 5. Risks & mitigations

- **Surprise from the default-on behaviour.** A user accustomed to the spec 0016 `sync → rename` workflow may not expect `inat update` to silently apply renames. Mitigation: the new plan-summary line (`1 rename`) and per-apply line (`renamed X → Y`) make the activity explicit; the CHANGELOG entry calls out the new behaviour as a `### Changed`; the README is updated with the auto-application note. If the surprise is significant in practice, the spec's §9 follow-up adds `--no-auto-rename` as a one-line conditional.
- **Header-rewrite failure leaves apply half-applied (in-memory only).** The rollback restores `cfg.Installed` byte-identically before returning; the caller's gated `SaveConfig` keeps the disk file unchanged. Same end-state pattern as spec 0017's replace failure. Mitigation: tests assert the rollback explicitly; the manual smoke step 15 (ambiguity) exercises a needs-attention path through a different code branch so a future regression in apply rollback would show up.
- **Lazy-index regression — index built when not needed.** If a refactor accidentally eagerly builds the index for every source on every `Plan` call, the cost is N tree-SHA fetches per source per update. Mitigation: instrumentation tests assert the build count explicitly. The cost regression would be caught by the test rather than silently slowing every update.
- **Cross-action collision victim selection is implementation-defined.** When a rename and an unrelated update both contribute the same name to the post-batch view, `checkBatchUniqueness` converts whichever it visits first to `NeedsAttention`. The visit order is input-order (per the existing implementation). Mitigation: the test asserts that *at least one* of the colliding actions converts; it does not assert which one. The user's recovery path (run `inat sync`, resolve manually) is the same either way.
- **`RewriteAlwaysHeaders` export couples transaction → rename.** A future change to `cli/rename`'s helper signature now affects `cli/transaction`. Mitigation: the function's parameters (`dir, cfg, oldKey, newKey, homeOverride`) are the minimal set that any rename-header-rewriting caller needs. Adding parameters would be a breaking change that warrants its own spec.
- **Ambiguity error obscures the underlying state.** The user sees `auto-rename for X is ambiguous: candidates are A, B` but may not understand why two kits in the same source share a tree SHA. Mitigation: the error message points at `inat sync` (which prints the full per-kit classification) and `inat rename` (the explicit verb with strict validation). The CHANGELOG note for spec 0018 calls out the corner case.
- **Doc drift between spec / plan and implementation.** Mitigation: the pre-commit hook's spec-presence check catches missing spec/plan pairs; CI's `gofmt`/`vet`/`staticcheck`/`test` catches code drift; the spec / plan / code triad is reviewed in the same MR.
- **Multi-agent partial-disk state on header-rewrite failure.** The in-memory `cfg.Installed` rollback restores config-file coherence, but a partially-written agent file is left on disk pointing at `newKey`. Mitigation: documented as inherited from spec 0017's apply-phase residual-disk risk; recovery via `inat refresh` re-aligns headers with the saved config; explicit acceptance criterion (§6 #11a) asserts the recovery path. Long-term: an on-disk snapshot/restore primitive (spec 0017 §9 follow-up) would close this.
- **File-size regression on `plan.go`.** The file is 333 lines today, over the 300-line target. Without `plan_index.go` carving off the lazy-index logic, the auto-detection edits would push it past the 400 hard max. Mitigation: the split is part of the spec (§4.9). Pre-commit hook does not enforce the soft target programmatically, but reviewers should reject patches that push `plan.go` over 400 without an additional split.
- **Two implementations of the same classifier (sync vs transaction).** `inat sync` today has its own tree-SHA classifier; 0018 ships a parallel implementation inside the transaction package. Edge-case fixes will need to be made in both places until a future consolidation refactor (acknowledged in spec §9 follow-ups). Mitigation: keep the two implementations small and well-tested; cross-link via comments at both sites once the new code lands; revisit consolidation as a separate spec if drift becomes an issue.

---

### 6. Out-of-scope follow-ups

Same as spec §9. Re-listed here for the rollout-doc audience:

- **`--no-auto-rename` escape hatch** if the auto-apply default surprises users.
- **Cross-source auto-detection** in addition to in-source — would close the manual `inat replace` step too.
- **`inat update --plan` / `inat update --dry-run`** — useful for review workflows; deferred from spec 0017 §9.
- **Detection during `inat status`** — requires a fetch, conflicts with status's no-network contract; may belong in a new `inat audit` verb.
- **Tree-SHA index caching across `Plan` invocations** — premature for current corpus sizes; revisit if profiling shows the rebuild is hot.
- **Auto-detection in `inat refresh`** — requires refresh to fetch, which it does not today; out of scope unless refresh's contract changes.
- **Unify `inat sync` and `transaction` classifiers** — sync today has its own implementation; consolidation would ensure both stay in lockstep on edge cases. Clean refactor opportunity, no behavioural delta.
