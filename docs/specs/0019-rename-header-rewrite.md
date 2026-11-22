## inat — Rename rewrites always-rule managed-section headers (Spec 0019)

---

### 1. Overview

Spec [0016](0016-kit-reorg-baseline.md) shipped `inat rename src/old src/new` as a pure `cfg.Installed` map-key rewrite — no on-disk files touched, because the kit's tree content is byte-identical at the new path so delivered files are still correct. One thing the spec explicitly accepted as a known limitation (§4.3, §9): always-rule managed-section blocks embed `source/kit/filename` in their header, e.g. `<!-- inat:block source=company/foo/no-secrets.md -->`. After `inat rename company/foo company/foo-renamed`, that header still says `company/foo/...` — informational, not behavioural, but stale.

The recovery path today is `inat refresh`, which re-delivers everything and rewrites the headers in passing. This spec removes the round-trip: `inat rename` itself rewrites the affected headers as a final step in its own pipeline, before saving config. No new commands, no new flags, no schema change.

A companion implementation plan lives at [`docs/plan/0019-rename-header-rewrite.md`](../plan/0019-rename-header-rewrite.md).

This spec extends [`docs/specs/0001-initial-idea.md`](0001-initial-idea.md) (the kit lifecycle contract) and pays down a known limitation flagged in [`docs/specs/0016-kit-reorg-baseline.md`](0016-kit-reorg-baseline.md) §4.3.

---

### 2. Goals

1. After `inat rename src/old src/new` succeeds, every active agent's always-rule target file (e.g. `CLAUDE.md` for `claude-code`) MUST contain managed-section block headers that use the new install key. Specifically: every block whose header begins with `src/old/` MUST have its header rewritten to begin with `src/new/`, with the trailing filename component preserved byte-identically.
2. Block content MUST NOT change. Only the header line changes.
3. Block ordering within the managed section MUST be preserved.
4. The rewrite MUST happen as part of `rename`'s existing pipeline, before `config.SaveConfig`. If the rewrite fails, `cfg.Installed` MUST NOT be mutated and config MUST NOT be saved — the user sees the original error and their state is unchanged.
5. Files without a managed section, or with a managed section that contains no blocks matching the old key, MUST be a no-op (no error, no file write).
6. The rewrite MUST iterate every active agent's always-target. If `claude-code` is the only active agent, only `CLAUDE.md` is touched. If multiple agents are active, each agent's configured always-target is processed independently.
7. No on-disk schema change. `Config`, `InstalledKit`, agent profiles, managed-section markers all unchanged.
8. No new module dependencies; no new typed errors. Reuses the existing `delivery` package's parse / render helpers.

---

### 3. Non-goals

- **Contextual references.** The reference path `.inat/rules/<entryName>.md` doesn't embed the kit name, so contextual references survive rename intact and need no rewrite. Outside the scope of this spec.
- **Replace.** `inat replace` re-runs the install pipeline as its install half, which already writes fresh headers using the new key. No change needed.
- **Update.** Same — `update` removes and re-delivers, which writes fresh headers.
- **Cross-agent migration.** If a user added `claude-code` *after* a kit was installed, the always-rule block was never written there. Rename has nothing to fix on that agent. Recovery is `inat refresh`, unchanged from today.
- **Header format change.** The existing `source/kit/filename` shape stays. Spec only rewrites the prefix; doesn't change the encoding.

---

### 4. Design

#### 4.1 Pipeline addition

`inat rename` today runs (per spec 0016 §4.3): load config → parse refs → validate same source → validate installed state → fetch source → ensure kit in registry → ensure tree-SHA matches → mutate `cfg.Installed` → save config → print result.

The new step lands between "mutate `cfg.Installed`" and "save config":

```
8.  Validate tree-SHA equality.
9.  Mutate config: cfg.Installed[newKey] = cfg.Installed[oldKey]; delete oldKey.
9a. Rewrite always-rule headers in every active agent's target.   ← NEW
10. Save config.
11. Print result.
```

Step 9a fails closed: any error from the rewrite returns before the config save, so the on-disk config never reflects a half-renamed state. (The in-memory `cfg.Installed` was mutated in step 9 but is discarded when the function returns without saving.)

#### 4.2 New helper: `delivery.RenameKitBlocks`

A new exported function in `go/core/delivery/managed_write.go`, mirroring the shape of `RemoveKitContent`:

```go
// RenameKitBlocks rewrites every block whose header begins with
// `oldKey/` to use `newKey/` instead, preserving the trailing
// filename component and the block content. No-op when the file has
// no managed section or no blocks match. Idempotent.
func RenameKitBlocks(targetFile, oldKey, newKey string) error
```

Implementation:
1. Read file; if not exist, return nil.
2. Parse managed section; if absent, return nil.
3. For each block whose `Header` starts with `oldKey + "/"`, set `Header = newKey + "/" + strings.TrimPrefix(b.Header, oldKey+"/")`.
4. Write the file back via the existing `writeManagedFile` helper.

If no block matched, the function still goes through the read-write cycle once. Acceptable: idempotent, low cost. (Optional optimization: skip the write when no header changed. Decided in §4.4.)

#### 4.3 Rename pipeline integration

`go/cli/rename/rename.go`'s `Rename` function gains one call after the in-memory mutation:

```go
cfg.Installed[newKey] = cfg.Installed[oldKey]
delete(cfg.Installed, oldKey)
if err := rewriteAlwaysHeaders(dir, &cfg, oldKey, newKey, homeOverride); err != nil {
    return err
}
if err := config.SaveConfig(dir, &cfg); err != nil {
    return err
}
```

`rewriteAlwaysHeaders` is a small private helper that:
1. Calls `config.CollectActiveAgents(cfg, homeOverride)`.
2. For each agent, looks up the profile via `agents.GetProfile`.
3. Calls `delivery.RenameKitBlocks(<dir>/<profile.Mapping.Rule.Always.Path>, oldKey, newKey)`.

Errors from any step propagate up. The mutation in step 9 is fine to discard: the function returns without saving, so on-disk config still reflects the pre-rename state.

#### 4.4 Skip-write when no header changed

`RenameKitBlocks` tracks whether any block header was rewritten. When zero blocks match, the file write is skipped — preserves mtime for files that didn't actually change, and avoids needless I/O on agents that never carried the renamed kit's blocks.

Pseudocode:

```go
changed := 0
for i := range parts.Section.Blocks {
    if strings.HasPrefix(parts.Section.Blocks[i].Header, oldPrefix) {
        parts.Section.Blocks[i].Header = newPrefix + strings.TrimPrefix(parts.Section.Blocks[i].Header, oldPrefix)
        changed++
    }
}
if changed == 0 {
    return nil
}
return writeManagedFile(targetFile, parts)
```

#### 4.5 Why not also fix `replace` / `update`?

Replace's install half writes fresh always-rule headers via `install.InstallSteps` → `deliverAlwaysRule` using the new key — already correct. Update is the same: remove old, install new, fresh headers. Only rename, which deliberately *doesn't* re-deliver, has the staleness problem.

---

### 5. Compatibility

- **`go/cli/rename/rename.go`** — gains a private helper plus one call site; `RunRename` signature unchanged.
- **`go/core/delivery/managed_write.go`** — new exported `RenameKitBlocks` function. No change to existing helpers.
- **Managed-section markers** — unchanged. `ParseManagedSection` and `RenderManagedSection` consume the same shape pre- and post-spec.
- **`.inat/config.json`** — schema unchanged. Configs written before / after this spec round-trip identically.
- **Agent profiles** — unchanged.
- **Existing CLI commands** — `install`, `update`, `remove`, `refresh`, `replace`, `sync` all unchanged.
- **Pre-commit hook + CI** — unchanged. The new test files are covered by the existing `gofmt`/`vet`/`staticcheck`/`test` gates.
- **`go/go.mod` / `go/go.sum`** — no new modules.
- **README.md** — one-line clarification under the rename paragraph in the Reorganization subsection: header staleness is no longer a known limitation.
- **CHANGELOG.md** — `[Unreleased]` gains a `### Changed` entry pointing at spec 0019.

---

### 6. Acceptance criteria

1. **Build clean.** `go -C go/ build ./...` exits 0.
2. **Header rewritten for active agent.** Project with `claude-code` agent + an installed kit with an always rule. After `inat rename`, `CLAUDE.md`'s managed section has a block whose header references `src/new/...` (not `src/old/...`). Block content is byte-identical to before the rename.
3. **No header for non-matching kits is touched.** Project with two kits installed (kitA, kitB); both have always rules. After `inat rename src/kitA src/kitA-renamed`, kitB's block header is unchanged byte-for-byte.
4. **Block ordering preserved.** Project with three always rules from the renamed kit, in some specific order in the managed section. After rename, the order is identical.
5. **No managed section, no error.** Project where `CLAUDE.md` exists but has no managed section: rename succeeds; `CLAUDE.md` is not modified (mtime unchanged).
6. **No active agents, no error.** Project with `cfg.Agents` empty (and no global agents): rename succeeds; no agent files touched.
7. **Multiple active agents.** Project with two agents whose always-targets differ: each gets its headers rewritten independently.
8. **Failure closes the loop.** If `RenameKitBlocks` returns an error (synthesized in tests via a permission failure or the file being a directory), `cfg.Installed` on disk reflects the *pre-rename* state. The error propagates to the caller.
9. **`inat refresh` is no longer needed.** A user who renames + then runs no other command should see correct headers; no `refresh` step required. Verified by criterion 2.
10. **No on-disk schema change.** Diff `cfg` JSON pre/post a rename: only `cfg.Installed`'s key change.
11. **No new module dependencies.** `git diff go/go.mod go/go.sum` between pre-change and post-change tree is empty.
12. **File / function size limits.** New `RenameKitBlocks` and `rewriteAlwaysHeaders` each under 30 lines. No file crosses the 300-line target.
13. **Pre-commit hook clean.** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all exit 0.

---

### 7. Testing notes

- **Unit tests for `RenameKitBlocks`.** Table-driven in `go/core/delivery/managed_write_test.go`: file with one matching block, file with mixed blocks, file with no managed section, file that doesn't exist, file with no matching blocks. Assert content and (where relevant) mtime preservation.
- **Integration tests in `cli/rename`.** Extend `rename_test.go`: install-with-always-rule then rename, parse `CLAUDE.md`, assert header. Mirror existing `TestRenameHappyPath` setup.
- **Multi-agent test.** Add a second agent profile to the project (or use a fixture that exercises one). Assert both agents' always-targets get rewritten.
- **No live network.** Same constraint as the rest of the test suite.
- **No goroutines.** Same constraint.

---

### 8. Documentation

- **`README.md`** — under the Reorganization subsection, the rename paragraph today says headers go stale until the next `update`/`refresh`. Replace with: headers are rewritten in place by rename itself.
- **`CHANGELOG.md`** — `[Unreleased]` gains `### Changed`: "`inat rename` now rewrites stale always-rule managed-section headers in place; `inat refresh` is no longer required to clean them up. Spec / plan [`0019-rename-header-rewrite`](docs/specs/0019-rename-header-rewrite.md)."
- **`docs/specs/0016-kit-reorg-baseline.md`** — the §4.3 "header staleness is a known limitation" sentence stays as historical context (the limitation existed in 0016) but should add a forward reference to spec 0019. This is a documentation-only change to an already-shipped spec.

---

### 9. Out-of-scope follow-ups

- **Header format reshape.** The existing `source/kit/filename` shape might want a richer encoding (e.g. embedded tree SHA for traceability). Out of scope here; would be a separate spec if motivated.
- **`inat refresh` short-circuit.** Today refresh re-delivers everything; it could detect that headers are already correct and skip the write. Possible perf optimisation, no correctness need.
