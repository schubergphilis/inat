## inat — Rename rewrites always-rule managed-section headers (Plan 0019)

Pairs with [`docs/specs/0019-rename-header-rewrite.md`](../specs/0019-rename-header-rewrite.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, TDD test order, and verification.

Pays down the known limitation flagged in spec 0016 §4.3: always-rule managed-section block headers embed `source/kit/filename` and go stale after `inat rename` until the next `inat refresh`. This spec moves the rewrite into rename itself.

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Where the rewrite lives | New exported function in the existing `delivery` package, mirroring `RemoveKitContent`. Avoids leaking managed-section parsing into `cli/rename`. |
| Pipeline placement | Step 9a: between `cfg.Installed` mutation (step 9) and `config.SaveConfig` (step 10). Failure short-circuits the save so on-disk config never reflects a half-renamed state. |
| Skip-write optimisation | When zero blocks match, return without writing. Preserves mtime; avoids needless I/O on agents that never carried the renamed kit's blocks. |
| New typed errors | None. Existing `errs.WrapIO` covers any I/O failure path. |
| Scope of header rewrite | Only `Block.Header`. Block content untouched, contextual references untouched, render order untouched. |
| Multi-agent handling | Iterate `config.CollectActiveAgents`; for each agent, rewrite its always-target file (`profile.Mapping.Rule.Always.Path`). Sequential, no goroutines per CLAUDE.md §11. |
| Test strategy | Unit tests for `RenameKitBlocks` (table-driven against synthesised file contents); integration tests for the rename pipeline (install + rename + assert `CLAUDE.md`). |
| Schema change | None. |
| Module dependencies | None added. |

---

### 2. Files

#### Modified

- **`go/core/delivery/managed_write.go`** — adds `RenameKitBlocks(targetFile, oldKey, newKey string) error`. ~25 lines.
- **`go/core/delivery/delivery_test.go`** (or `managed_write_test.go` if that's the established naming) — table-driven tests for the new function. ~80 lines.
- **`go/cli/rename/rename.go`** — inserts one call to a private `rewriteAlwaysHeaders` helper between the in-memory mutation and `config.SaveConfig`. The helper iterates active agents and calls `delivery.RenameKitBlocks` per agent. ~25 lines added.
- **`go/cli/rename/rename_test.go`** — extends `TestRenameHappyPath` (or adds a focused test) to assert the post-rename `CLAUDE.md` has the correct header. Adds a multi-agent test if the existing fixture doesn't already cover one. ~40 lines added.
- **`README.md`** — updates the rename paragraph under the Reorganization subsection. The "stale until next update/refresh" sentence is replaced with the new behaviour.
- **`CHANGELOG.md`** — `[Unreleased]` gets a `### Changed` entry pointing at spec 0019.
- **`docs/specs/0016-kit-reorg-baseline.md`** — single sentence pointing the §4.3 known-limitation note at spec 0019 (says "fixed by 0019" while leaving the historical note intact).

#### New

- `docs/specs/0019-rename-header-rewrite.md` — the contract.
- `docs/plan/0019-rename-header-rewrite.md` — this document.

#### Untouched

- `go/cli/replace/`, `go/cli/sync/`, `go/cli/update/`, `go/cli/install/`, `go/cli/remove/`, `go/cli/refresh/` — none are affected.
- `go/core/delivery/managed_section.go`, `managed_parse.go`, `managed_render.go` — managed-section markers, parser, and renderer all unchanged.
- `go/core/config/`, `go/core/registry/`, `go/core/kit/`, `go/core/hash/`, `go/core/naming/`, `go/core/reserved/`, `go/core/validate/`, `go/core/frontmatter/` — all unchanged.
- `go/git/`, `go/agents/`, `go/errs/` — all unchanged.
- `go/main.go` — no new commands; no change.
- Release pipeline, install scripts, `VERSION` — no change.

---

### 3. Implementation order

TDD-first per CLAUDE.md §1. Each step writes failing tests, then minimal implementation to pass them, then refactor.

1. **`go/core/delivery/RenameKitBlocks`.** Add the new helper. Tests:
   - File doesn't exist → returns nil, no write.
   - File exists, no managed section → returns nil, no write (mtime preserved).
   - File has managed section with no matching block → returns nil, no write (mtime preserved).
   - File has managed section with one matching block → header rewritten, content preserved, file mtime updated.
   - File has managed section with multiple matching blocks plus non-matching blocks → all matching rewritten, non-matching unchanged, ordering preserved.
   - File has managed section with block whose header is exactly `oldKey` (no trailing slash) → not rewritten (the prefix match requires `oldKey + "/"`).
2. **`go/cli/rename/rename.go` integration.** Add the private `rewriteAlwaysHeaders(dir, cfg, oldKey, newKey, homeOverride)` helper and the one-line call from `Rename`. Place after the in-memory mutation, before `SaveConfig`.
3. **`cli/rename/rename_test.go` extension.** Extend `TestRenameHappyPath` with assertions on `CLAUDE.md`'s post-rename content. Add `TestRenameRewritesHeadersAcrossMultipleAgents` if the test fixture supports it (or add a smaller focused test if not).
4. **Failure-path test.** Synthesise an `RenameKitBlocks` failure (e.g. `CLAUDE.md` is a directory rather than a file) and assert: rename returns the error, `cfg.Installed` on disk still has the old key.
5. **README + CHANGELOG.** Replace the rename paragraph's known-limitation sentence; add `[Unreleased]` `### Changed` entry.
6. **Forward reference in 0016 spec.** Add a single sentence to spec 0016 §4.3 pointing at 0019: "Spec 0019 closes this gap."

After step 4, the pre-commit hook is the gate.

---

### 4. Verification

#### Local (test suite)

1. `go -C go/ test ./core/delivery/...` — new `RenameKitBlocks` table-driven coverage.
2. `go -C go/ test ./cli/rename/...` — existing tests + extended header assertion + multi-agent test.
3. `gofmt -l go/` — silent.
4. `go -C go/ vet ./...` — silent.
5. `staticcheck ./...` — silent.
6. `go -C go/ test ./...` — full suite remains green.

All gates are what the pre-commit hook (`.githooks/pre-commit`) and CI already enforce.

#### Manual smoke (Linux x86_64)

7. Stage a project with `claude-code` configured and a kit installed that has an always rule. Run `inat rename src/old src/new`. Inspect `CLAUDE.md`: the managed-section block previously headed `src/old/...` now reads `src/new/...`. Block content byte-identical. Other blocks (if any) untouched.
8. Run `inat sync` after the rename: zero divergences.

#### CI smoke

9. The pre-commit hook runs all four gates locally; CI re-runs them on push. No new CI job needed.

---

### 5. Risks & mitigations

- **Failure mid-rewrite leaves a partially-rewritten file.** `RenameKitBlocks` reads, mutates in memory, then writes once at the end. The write itself is atomic at the syscall level on Linux (`os.WriteFile` truncates + writes), so partial states are bounded to "old content" or "new content", not "half-and-half". Mitigation: spec acknowledges this is the same atomicity guarantee `delivery.AddAlwaysBlock` already provides. No new risk.
- **Multi-agent partial rewrite.** If two agents are active and the second agent's rewrite fails, the first agent's file is already updated but config didn't save — so `cfg.Installed` still has the old key. The first agent's `CLAUDE.md` now references `src/new/...` while config says `src/old/...`. Mitigation: this is the existing partial-state risk that `inat refresh` recovers from (running refresh against `src/old` re-delivers headers using the old key, restoring consistency). Documented in §3 non-goals as the same recovery path.
- **Block whose header is exactly `oldKey`.** The prefix match deliberately requires `oldKey + "/"` to avoid matching unrelated headers (e.g. blocks from a kit named `foo` shouldn't match a rename of `f`). Tests cover this.
- **Idempotency.** Running rename twice for the same key pair (the second time after the rewrite has already happened) — the second invocation finds no blocks matching `oldKey/` (because they all start with `newKey/` now), returns no-op. Safe.
- **Doc drift between spec / plan and implementation.** Mitigation: the pre-commit hook's spec-presence check catches missing pairs; CI's `gofmt`/`vet`/`staticcheck`/`test` catches code drift; spec / plan / code are reviewed in the same MR.

---

### 6. Out-of-scope follow-ups

Same as spec §9.

- Header format reshape (richer encoding).
- `inat refresh` short-circuit when headers are already correct.
