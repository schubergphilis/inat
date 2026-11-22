## inat — Kit reorganization workflow, baseline (Plan 0016)

Pairs with [`docs/specs/0016-kit-reorg-baseline.md`](../specs/0016-kit-reorg-baseline.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, TDD test order, and verification.

Establishes the manual workflow baseline for upstream kit reorganization: three new CLI verbs (`sync`, `rename`, `replace`) and a non-fatal failure mode for multi-kit `update`. After this milestone every reorganization scenario (in-source rename, upstream deletion, skill move between kits, cross-source kit move) has a clean supported workflow even if it requires the user to type one extra command. The "make it automatic" magic — atomic transactional update plus auto-detected rename application — is split into spec 0017 and 0018 so the baseline can ship and bake before the heuristic layer lands on top.

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Detection model | Author does nothing extra. inat reads the cache's git tree SHAs to detect in-source moves; consumer commands handle everything else. |
| Trigger split | `update` keeps current per-kit logic but switches to skip-and-continue for the multi-kit form; all detection lives in `sync`. Mutation lives in explicit `rename` / `replace` verbs. Auto-application during `update` is gated to spec 0018. |
| Rename threshold | Strict tree-SHA equality only. Recorded `Installed.Tree` must reappear at exactly one new path in the source's registry to surface a rename suggestion. No similarity / fuzzy / `git --follow` heuristic. |
| Cross-source match | Tree-SHA fingerprint across all configured sources. If a registry-orphaned kit's tree SHA exists in another configured source, sync suggests `replace`. Strict equality only. |
| Update failure mode (multi-kit) | Skip & continue, exit non-zero. Per-kit error printed; summary footer points at `sync`. CI catches the non-zero exit. |
| Update failure mode (single-kit) | Unchanged. `inat update src/kit` keeps fail-fast contract. |
| Update ordering | Map iteration order. NOT changed in this spec — transactional ordering is spec 0017. |
| New CLI verbs | `sync` (read-only diagnostic), `rename src/old src/new` (config rewrite, tree-SHA equality required), `replace src/old newSrc/new` (atomic remove+install, snapshot-restore on install failure). No `--apply` flag on `sync`. |
| State / audit trail | None. Print to stderr when mutating; rely on git history for retrospective analysis. |
| Tombstone files | Explicitly rejected (option G in the design grilling). |
| On-disk schema change | None. `Config`, `InstalledKit`, `DeliveredFile`, `Source`, `RegistryEntry` all unchanged. |
| Module dependencies | None added. Stays within the existing allowlist. |
| Where new code lives | Three new packages: `go/cli/sync/`, `go/cli/rename/`, `go/cli/replace/`. Each mirrors the layout of `go/cli/refresh/` and `go/cli/status/`. |
| Reusable helpers source | Refactor `go/cli/install/install.go` and `go/cli/remove/remove.go` to extract per-step helpers. No copy-paste of pipeline steps into the new packages. |
| Test fakes | Local bare git repos in `t.TempDir()` for sources. Same harness as existing `cli/install_test.go` / `cli/update_test.go`. No live GitLab calls. |
| Error model | Five new typed `errs.Kind` variants: `SyncDivergence`, `KitsNeedAttention`, `RenameTreeMismatch`, `RenameCrossSource`, `ReplaceInstallFailed`. One constructor per kind, mirrors existing pattern in `go/errs/errs.go`. |
| Concurrency | None added. Strictly sequential per CLAUDE.md §11. |

---

### 2. Files

#### New
- `go/cli/sync/sync.go` — cobra wiring (`NewSyncCmd()`) and `RunSync(opts)` entry point. Per-step helpers: `groupInstalledBySource`, `fetchAllSources`, `buildTreeSHAIndex`, `classifyKit`, `formatReport`. ~150 lines.
- `go/cli/sync/sync_test.go` — table-driven classification tests (cases a–d) + end-to-end fake-source tests covering in-source rename, cross-source move, deletion, ambiguous match. ~250 lines.
- `go/cli/rename/rename.go` — cobra wiring (`NewRenameCmd()`) and `RunRename(args)`. Helpers: `parseRefs`, `validateSameSource`, `validateInstalledState`, `validateTreeMatch`, `applyRename`. ~120 lines.
- `go/cli/rename/rename_test.go` — happy path, cross-source rejection, missing-old rejection, existing-new rejection, tree-SHA mismatch rejection, registry-not-found rejection. ~180 lines.
- `go/cli/replace/replace.go` — cobra wiring (`NewReplaceCmd()`) and `RunReplace(args, force)`. Helpers: `snapshotInstalled`, `runRemoveHalf`, `runInstallHalf`, `restoreSnapshot`. ~140 lines.
- `go/cli/replace/replace_test.go` — happy path (cross-source + same-source), drift on old kit without `--force`, drift with `--force`, install-failure rollback (name collision injection), config-save-once invariant. ~220 lines.
- `docs/specs/0016-kit-reorg-baseline.md` — the contract.
- `docs/plan/0016-kit-reorg-baseline.md` — this document.

#### Modified
- `go/cli/install/install.go`:
  - Extract a new exported `InstallSteps` orchestration that takes a pre-loaded `*config.Config` and does NOT save config; the existing `Install` becomes a thin wrapper that calls `InstallSteps` and then `config.SaveConfig`. `replace` calls `InstallSteps` directly so its single config save can include both halves of the operation.
  - The extraction is mechanical: lift lines 41-126 of `Install` into a new `InstallSteps(dir, kitRef, homeOverride, cfg *config.Config) error`. The existing entry point becomes ~10 lines (`LoadConfig` → `InstallSteps` → `SaveConfig`).
- `go/cli/remove/remove.go`:
  - Same shape: extract `RemoveSteps(dir, installKey string, force bool, homeOverride string, cfg *config.Config) error` that does the file-removal, managed-section cleanup, and `delete(cfg.Installed, installKey)` mutation but does NOT save config.
  - `RunRemove` becomes a thin wrapper: `LoadConfig` → `RemoveSteps` → `SaveConfig`.
  - Existing `RemoveDeliveredFiles`, `CleanManagedSections`, `ResolveInstalledRef` stay public (already used by `update`).
- `go/cli/update/update.go`:
  - Lines 41-50 (the `for _, key := range keys` loop) restructured into a `processOutcomes` collector. Per-iteration error is captured into a `kitOutcome{State: "needs-attention", Err: err}` instead of returned. After the loop, a `formatSummary` helper writes the stderr report and returns `nil` or `errs.NewKitsNeedAttention(N)`.
  - Targeted form (`kitRef != ""`) keeps fail-fast: if `keys` has a single element, skip the new collector path and call `updateSingle` directly with hard-error semantics. The branch is ~5 lines and lives at the top of `Update`.
  - Net add: ~40 lines, all inside the existing function or extracted helpers; the file remains under the 300-line target (current size: 191 lines).
- `go/main.go`:
  - Imports the three new packages.
  - Registers `sync.NewSyncCmd()`, `rename.NewRenameCmd()`, `replace.NewReplaceCmd()` via `root.AddCommand(...)` (alphabetic position: `refresh` → `remove` → `rename` → `replace` → `sources` → `status` → `sync` → `update` → `validate`).
- `go/errs/errs.go`:
  - Extends the `Kind` enum with five new variants in spec §4.6.
  - Adds matching sentinel constants and constructor functions.
  - Extends the `Error()` switch with five new format strings.
- `README.md`:
  - New "Reorganization" subsection under §Usage with paragraphs on `sync`, `rename`, `replace`, and the new `update` summary footer.
  - One example showing the `sync → rename` flow.
- `CHANGELOG.md`:
  - `[Unreleased]` gets `### Added` and `### Changed` entries pointing at spec 0016.

#### Untouched
- `go/core/config/config.go` — `CheckNameUniqueness`, `LoadConfig`, `SaveConfig`, `CollectActiveAgents`, schema all unchanged.
- `go/core/registry/registry.go` — registry parser unchanged.
- `go/core/hash/hash.go` — hash semantics unchanged.
- `go/core/kit/`, `go/core/delivery/`, `go/core/frontmatter/`, `go/core/naming/`, `go/core/reserved/`, `go/core/validate/` — all unchanged.
- `go/git/source/` — `CloneOrFetch`, `FetchRegistryAt`, `FetchKitTreeAt`, `WalkKitFiles` all unchanged. New code calls them as-is.
- `go/git/cache/` — cache layout unchanged.
- `go/agents/` — agent profiles unchanged.
- `go/cli/refresh/`, `go/cli/status/`, `go/cli/init/`, `go/cli/agents/`, `go/cli/sources/`, `go/cli/validate/`, `go/cli/self/` — unaffected.
- `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1` — first-time install path unchanged.
- `ci/build-release.sh`, `ci/install-rcodesign.sh`, the GitLab CI config — no pipeline change, no new artifact.
- `go/go.mod`, `go/go.sum` — no new modules.
- `VERSION` — bump is the release captain's call when cutting the next tag.
- `rust/` — frozen archive, untouched per CLAUDE.md.

---

### 3. Implementation order

TDD-first per CLAUDE.md §1. Each step writes failing tests, then minimal implementation to pass them, then refactor.

1. **`go/errs/errs.go` extension.** Add the five new `Kind` variants, sentinels, constructors, and `Error()` cases. Tests in `go/errs/errs_test.go` verify each new kind formats its message correctly and is `errors.Is`-comparable to its sentinel. Land first because the rest of the work imports these.
2. **`go/cli/install/install.go` refactor — extract `InstallSteps`.** Move lines 41-126 of the current `Install` into a new exported `InstallSteps(dir, kitRef, homeOverride string, cfg *config.Config) error`. `Install` becomes a 10-line wrapper. Existing `cli/install_test.go` (and the `cli/update` tests that import `install.DeliverToAgent` etc.) MUST continue to pass byte-identically. No new test added for the extraction itself; the existing tests are the regression gate.
3. **`go/cli/remove/remove.go` refactor — extract `RemoveSteps`.** Same shape: lift the file-removal + managed-section cleanup + `delete(cfg.Installed, ...)` into `RemoveSteps(dir, installKey string, force bool, homeOverride string, cfg *config.Config) error`. `RunRemove` becomes a thin wrapper. Existing tests are the regression gate.
4. **`go/cli/rename/rename.go` — `parseRefs` + `validateSameSource` + happy path.** Smallest meaningful unit. Tests cover both refs in `source/kit` form, bare-name rejection, cross-source rejection. After this step `inat rename` works for the same-source case modulo the tree-SHA validation.
5. **`go/cli/rename/rename.go` — tree-SHA validation.** Add the `source.FetchKitTreeAt` call and the equality check. Tests stage a fake source where the new kit's tree differs from the recorded one and assert `RenameTreeMismatch`.
6. **`go/cli/rename/rename.go` — registry / installed-state validation.** Add the remaining guards (kit-not-installed, kit-already-at-new-key, kit-not-in-registry). Tests cover each rejection path.
7. **`go/cli/rename/rename.go` — cobra wiring.** `NewRenameCmd()` returns the cobra command; tested by exercising `Execute()` with arg lists.
8. **`go/cli/replace/replace.go` — `snapshotInstalled` + `runRemoveHalf`.** Reuse `removecmd.RemoveSteps` (extracted in step 3). Tests verify the snapshot is a deep copy (mutating the snapshot does not affect `cfg.Installed[oldKey]`) and that `runRemoveHalf` correctly mutates `cfg` in-memory without saving.
9. **`go/cli/replace/replace.go` — `runInstallHalf` + happy path.** Reuse `install.InstallSteps`. Tests cover the cross-source happy path (sourceA → sourceB), same-source happy path (`replace src/A src/B` where new kit has different content from old).
10. **`go/cli/replace/replace.go` — `restoreSnapshot` rollback path.** Test injects an install failure (name collision against a third installed kit) and asserts: `cfg.Installed[oldKey]` restored to snapshot value, returned error is `ReplaceInstallFailed`, single config save at the end (counted via wrapping `config.SaveConfig` in tests).
11. **`go/cli/replace/replace.go` — `--force` plumbing.** Tests cover drift-without-force (rejected at remove half) and drift-with-force (succeeds). Reuses the existing drift contract from `RemoveDeliveredFiles`.
12. **`go/cli/replace/replace.go` — cobra wiring.** `NewReplaceCmd()` with `--force` flag.
13. **`go/cli/sync/sync.go` — `groupInstalledBySource` + `fetchAllSources`.** Pure helpers over `*config.Config`. Tests use fake sources to verify the registry / tree-SHA index is built correctly.
14. **`go/cli/sync/sync.go` — `buildTreeSHAIndex`.** For each source, walk every kit listed in its registry, fetch its tree SHA, build `map[treeSHA](sourceName, kitName)`. Tests cover registries with one, several, and zero kits.
15. **`go/cli/sync/sync.go` — `classifyKit`.** Pure function over `(installKey, installed config.InstalledKit, perSourceIndex map[string]Index, crossSourceIndex Index)`. Returns one of `case_a / case_b / case_c / case_d`. Tests table-drive every combination.
16. **`go/cli/sync/sync.go` — `formatReport` + `RunSync`.** End-to-end orchestration. Tests exercise a multi-kit fake project with kits in each of the four classification buckets and assert the stderr report and the returned `SyncDivergence(M)` error.
17. **`go/cli/sync/sync.go` — cobra wiring.** `NewSyncCmd()` with no flags or args.
18. **`go/cli/update/update.go` — skip-and-continue conversion.** Restructure the multi-kit loop. Tests extend `cli/update/update_test.go` with a three-kit scenario where the middle kit fails; assert the first and third update cleanly, the failure is reported, and the returned error is `KitsNeedAttention(1)`.
19. **`go/cli/update/update.go` — single-kit fail-fast preservation.** Test asserts `inat update src/missing` returns the original `KitNotInstalled` error byte-identically to today.
20. **`go/main.go` integration.** Register the three new commands. Run `go test ./...` to confirm nothing regressed.
21. **README + CHANGELOG.** Documentation per spec §8.

After step 20, the pre-commit hook is the gate: `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all pass.

---

### 4. Verification

#### Local (test suite)

1. `go -C go/ test ./cli/sync/...` — table-driven classifier coverage + e2e.
2. `go -C go/ test ./cli/rename/...` — every pre-condition violation + happy path.
3. `go -C go/ test ./cli/replace/...` — happy paths + rollback + drift policy.
4. `go -C go/ test ./cli/update/...` — extended multi-kit failure scenarios + single-kit fail-fast preserved.
5. `go -C go/ test ./cli/install/...` — existing tests must remain green after the `InstallSteps` extraction.
6. `go -C go/ test ./cli/remove/...` — existing tests must remain green after the `RemoveSteps` extraction.
7. `go -C go/ test ./errs/...` — covers the five new error kinds.
8. `gofmt -l go/` — silent.
9. `go -C go/ vet ./...` — silent.
10. `staticcheck ./...` (run from repo root with `-checks=all`) — silent.
11. `go -C go/ test ./...` — full suite remains green.

All gates are what the pre-commit hook (`.githooks/pre-commit`) and the CI pipeline already enforce; no new tooling.

#### Manual smoke (Linux x86_64, the dev laptop)

12. Stage two local bare repos as fake sources (`/tmp/inat-smoke/sourceA.git`, `/tmp/inat-smoke/sourceB.git`), each with a `registry.json` and one kit. Stage an inat project under `/tmp/inat-smoke/proj/` with both sources configured. `inat install company/foo`.
13. **In-source rename smoke.** In `sourceA`'s working clone, `git mv kits/foo kits/foo-renamed`, update `registry.json`, commit, push to bare repo. Run `inat sync` from `/tmp/inat-smoke/proj/`: expect a case (b) report with the suggestion `inat rename company/foo company/foo-renamed`. Run the suggestion: expect `renamed company/foo → company/foo-renamed` and `cfg.Installed` reflects the new key. On-disk delivered files unchanged. Run `inat sync` again: zero divergences.
14. **Cross-source move smoke.** From the post-13 state, in sourceB's clone create a kit `bar` with the same tree as sourceA's `foo-renamed` (e.g. `cp -r ../sourceA-clone/kits/foo-renamed kits/bar`, fix `registry.json`, commit, push). Then in sourceA's clone delete `kits/foo-renamed` and its registry entry, commit, push. Run `inat sync`: expect a case (c) report suggesting `inat replace company/foo-renamed vendor/bar`. Run the suggestion: expect the `company/foo-renamed` delivered files to disappear, `vendor/bar`'s files to land, `cfg.Installed` to have `vendor/bar` only.
15. **Deletion smoke.** From a fresh project, install a kit; delete it from source registry without a matching tree elsewhere. `inat sync`: case (d) suggestion to `inat remove`. Run `inat remove`: kit gone.
16. **Update skip-and-continue smoke.** Three-kit project; rename one kit upstream beyond what the spec auto-handles (tree content edited in same commit as the rename, so no SHA match). Run `inat update`: expect two kits updated, one needs-attention; non-zero exit. Run `inat sync`: expect the case (b) suggestion (which will fail tree-SHA equality if you try `rename`, requiring `replace` instead — verifying the spec's "user is told to use replace if SHA mismatches" path).
17. **Replace rollback smoke.** Inject a name collision: install kitA from sourceA whose skill name is `foo`; install kitB from sourceA whose skill name is `bar`. Now in sourceA modify kitB's content so that the new tree's derived names include `foo`. Run `inat update`: kitB will fail name-uniqueness; verify the typed error and that `cfg.Installed[company/kitB]` is unchanged. (This is the existing `update` behaviour, preserved.) Now stage a `replace company/kitA company/kitC` where `kitC` also derives `foo`: run `inat replace`; verify the remove half of `kitA` ran (its delivered files are gone), the install half failed with `DuplicateName`, the snapshot was restored (`cfg.Installed[company/kitA]` is back), and the error is `ReplaceInstallFailed`. Run `inat refresh` to recover `kitA`'s on-disk files from cache.

#### CI smoke

18. The pre-commit hook (`.githooks/pre-commit`) runs all four gates locally before any commit; CI re-runs them on push. No new CI job needed.

---

### 5. Risks & mitigations

- **Refactor risk in `install.go` / `remove.go`.** The `InstallSteps` / `RemoveSteps` extraction is mechanical but the call sites in `update.go` already use individual exported helpers (`DeliverToAgent`, `RemoveDeliveredFiles`, `CleanManagedSections`); a partial extraction could break the existing update flow. Mitigation: keep the existing exported helpers untouched (they stay public and behavioural). The new `*Steps` functions are additional entry points layered on top, not replacements. The existing `cli/install_test.go` and `cli/update_test.go` are the regression gate; they MUST stay green at every commit during the refactor.
- **Replace's partial-state on install failure.** If the install half fails, on-disk delivered files for the old kit are already gone (the remove half ran first). The snapshot restores the config but not the files. Mitigation: spec acknowledges this in §3 non-goals; user recovery path is `inat refresh` from cache; the error message points at this. Long-term: spec 0017's transactional approach can extend to cover replace too; out of scope here.
- **Sync's tree-SHA enumeration cost.** Each source's `buildTreeSHAIndex` calls `source.FetchKitTreeAt` once per kit listed in the registry. For a source with N kits, that's N tree-SHA fetches per `inat sync` invocation against an already-cached repo (zero network cost beyond the initial `CloneOrFetch`). At N=20 the wall-clock cost is negligible. Mitigation: if profiling later shows the loop is hot, batch via `go-git`'s tree iteration directly. Premature for this spec.
- **Cross-source false positive.** Two unrelated sources happen to contain the same tree SHA (e.g. an empty kit or a copy-pasted `kit.json`). Sync would suggest a replace that isn't a real move. Mitigation: tree-SHA equality at this granularity (whole-kit tree) makes coincidental matches astronomically unlikely. The user reads the suggestion before running `replace` — sync never auto-applies.
- **Rename header staleness.** Always-rule managed-section headers embed `source/kit/filename` (set at delivery time). After `inat rename`, headers in `CLAUDE.md` (or whichever agent file) still say the old kit name. Mitigation: spec accepts this as a known limitation; documented in §4.3. Re-running `inat update` (or `refresh`) after the rename re-delivers the always rule with the corrected header.
- **Update's partial state in skip-and-continue mode.** A kit whose old delivery was removed but whose new delivery failed mid-loop is left with the old `Installed` entry pointing at files that are no longer on disk. Mitigation: this is exactly the partial-state risk spec 0017 fixes via transactional update; in this spec the user can `inat refresh` to re-deliver from cache, or the explicit `KitsNeedAttention` error guides them to investigate.
- **Cobra command name collisions.** `sync`, `rename`, `replace` are short, generic verbs. Mitigation: verified none shadow an existing top-level command in `go/main.go` at spec time.
- **Doc drift between spec / plan and implementation.** Mitigation: the pre-commit hook's spec-presence check catches missing spec/plan pairs; CI's `gofmt`/`vet`/`staticcheck`/`test` catches code drift; the spec / plan / code triad is reviewed in the same MR.

---

### 6. Out-of-scope follow-ups

Same as spec §9. Re-listed here for the rollout-doc audience:

- **Spec 0017 — transactional update.** Make multi-kit `update` atomic via fetch / plan / apply phases.
- **Spec 0018 — auto-rename detection.** Auto-apply trivial `git mv` renames during `update` once 0017's transactional foundation is in place.
- **`sync --apply`.** Conflate diagnosis and execution; punted on UX grounds.
- **Header-rewrite during rename.** Staleness mitigation; small follow-up if it becomes user-visible.
- **`inat history`.** Audit trail of past rename / replace operations.
- **Stable IDs / UUIDs.** Revisit only if strict tree-SHA classifier proves insufficient.
- **Cross-source detection by content similarity.** Strict equality only for now.
