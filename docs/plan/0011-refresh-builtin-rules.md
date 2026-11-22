## inat — `inat refresh` re-delivers built-in rules (Plan 0011)

Pairs with [`docs/specs/0011-refresh-builtin-rules.md`](../specs/0011-refresh-builtin-rules.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, and verification.

Closes the implementation gap left by spec [`0010-rule-descriptions.md`](../specs/0010-rule-descriptions.md) §8 criterion 7. Flagged in the spec 0010 MR's "Related potential follow-up". Spec + plan landed via their own MR (2026-04-29). Implementation landed via a follow-up MR (this commit's branch).

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Locus of the fix | Extend `inat refresh`. No new command surface. |
| Tracking field | Additive sidecar map: `Config.BuiltinDelivered map[string][]DeliveredFile` with `json:"builtinDelivered,omitempty"`. `Config.Agents` stays `[]string`. |
| State machine ownership | Encapsulated in `agents/builtin/`. New `RefreshInatUsage` / `RefreshInatKits` exported. Existing `Deliver*` signatures change to return `(config.DeliveredFile, error)`. |
| Upgrade migration when `BuiltinDelivered` is absent | On-disk ≠ embedded → modified-file warning; require `inat refresh --force` once (v0.3.x case). On-disk == embedded → silent backfill, no warning (v0.4.0 case). No prior-hash list. Documented in `CHANGELOG.md`. |
| KitsRepo gating | Strict gate on `cfg.KitsRepo`. False → skip `inat-kits` entirely, even if a stale record exists. |
| Managed-section bullet | Always sync'd, independent of file-state branch. Description text sourced from the *current* binary's embedded frontmatter via `delivery.AddContextualReference`. |
| Empty-state guard | Early-return only when `len(cfg.Installed) == 0 && len(activeAgents) == 0`. `CollectActiveAgents` moves above the guard. Message: "nothing to refresh". |
| Failure handling | Fail-fast on per-agent error. Single config save at end (atomicity rule). Self-healing on retry via state machine. |

---

### 2. Files

#### New
- `go/agents/builtin/refresh.go` — `RefreshInatUsage(projectDir, agentName, recorded *config.DeliveredFile, force bool) (delivered *config.DeliveredFile, modifiedPath string, err error)` and the `RefreshInatKits` mirror. Houses the §5 state machine. Split out of `rules.go` to keep that file under the 300-line target.
- `go/agents/builtin/refresh_test.go` — table-driven coverage of the state machine: missing record + on-disk-match-embedded, missing record + on-disk-mismatch + force/no-force, recorded match (no-op), recorded mismatch + on-disk-match-recorded (criterion 7), recorded mismatch + on-disk-mismatch + force/no-force, missing file (always restores), bullet-always-sync invariant. Smaller mirror suite for `RefreshInatKits`.
- `docs/specs/0011-refresh-builtin-rules.md` — the contract.
- `docs/plan/0011-refresh-builtin-rules.md` — this document.

#### Modified
- `go/core/config/config.go` — add `BuiltinDelivered map[string][]DeliveredFile` with `json:"builtinDelivered,omitempty"` to `Config`.
- `go/core/config/config_test.go` — add `TestConfigBuiltinDeliveredRoundTrips` and `TestConfigOmitsBuiltinDeliveredFieldWhenEmpty`.
- `go/agents/builtin/rules.go` — `DeliverInatUsage(projectDir, agentName) (config.DeliveredFile, error)` and `DeliverInatKits(...)` (was `error`-only). Stop discarding the return from `delivery.DeliverRuleContextual` in `deliverBuiltin`; thread it back to callers.
- `go/agents/builtin/rules_test.go` — adjust existing assertions to the new signatures; add a check that the returned `DeliveredFile.Hash` equals `hash.HashBytes(embeddedContent)`.
- `go/cli/agents/agents.go` — `AgentsAdd` captures the returned `DeliveredFile`s, populates `cfg.BuiltinDelivered[name]`, saves config. `AgentsRemove` runs `delete(cfg.BuiltinDelivered, name)`.
- `go/cli/agents/agents_test.go` — `TestAddAgentRecordsBuiltinDelivered`, `TestRemoveAgentClearsBuiltinDelivered`.
- `go/cli/init/init.go` — `Init` captures returns from `DeliverInatUsage` / `DeliverInatKits` (lines 58, 62), populates `cfg.BuiltinDelivered`, saves config.
- `go/cli/init/init_test.go` — `TestInitWithAgentRecordsBuiltinDelivered`, `TestInitKitsRepoAndAgentRecordsBoth`.
- `go/cli/refresh/refresh.go` — move `CollectActiveAgents` above the empty guard; relax guard to `len(cfg.Installed) == 0 && len(activeAgents) == 0`; new "nothing to refresh" message; after the existing kit loop, add a built-in loop over `activeAgents` calling `builtin.RefreshInatUsage` and (gated on `cfg.KitsRepo`) `builtin.RefreshInatKits`; accumulate into `newBuiltins map[string][]config.DeliveredFile`; replace `cfg.BuiltinDelivered = newBuiltins` and `config.SaveConfig` once at end.
- `go/cli/refresh/refresh_test.go` — `TestRefreshNoKitsButAgents`, `TestRefreshNothingToDo`, `TestRefreshKitsRepoFalseIgnoresKitsRule`, `TestRefreshBuiltinUpgradeFromV03`, `TestRefreshMultiAgentBuiltins`. Modify `TestRefreshRestoresManagedSection` to also assert `inat-usage` bullet is restored.
- `CHANGELOG.md` — `[Unreleased]` entry: "`inat refresh` now re-delivers built-in rules (`inat-usage`, `inat-kits`) and tracks their hashes in `.inat/config.json`. **One-time migration for v0.3.x upgraders:** run `inat refresh --force` once to refresh the built-in rule files; the modified-file guard will warn until you do. v0.4.0 upgraders need nothing — refresh silently backfills the new tracking field. Subsequent upgrades are friction-free."
- `README.md` — short note in the §Refresh-or-equivalent section pointing at the v0.3 → v0.4 migration step.

---

### 3. Implementation order (TDD-first)

1. **Config schema** — `BuiltinDelivered` field + round-trip tests. Smallest unit, no dependents.
2. **Built-in deliverer signatures** — change `DeliverInatUsage` / `DeliverInatKits` to return `(DeliveredFile, error)`. Update callers in `agents.go` and `init.go` to capture and persist. Keeps tests green; CLI behaviour unchanged at this point.
3. **`agents add` / `agents remove` / `init` config writes** — populate / clear `BuiltinDelivered` at the four call sites. Tests for each.
4. **State machine + tests in `agents/builtin/refresh.go`** — pure logic against a tmpdir, no orchestration. TDD all branches before wiring into refresh.
5. **`refresh.go` orchestrator** — relax guard; loop active agents; call new helpers; accumulate; save. Update tests.
6. **End-to-end smoke** — manual scenarios from §4 below.
7. **Docs** — `CHANGELOG.md`, `README.md` migration note.

---

### 4. Verification

- `go test ./...` green.
- `gofmt -l .` empty; `go vet ./...` clean; `staticcheck ./...` clean.
- Pre-commit hook (`.githooks/pre-commit`) passes (the spec+plan pair satisfies the spec-presence check).
- End-to-end manual smoke (in a scratch project under `~/.inat/cache/smoke` or similar):
  1. `inat init --agent claude-code` → inspect `.inat/config.json`: `builtinDelivered.claude-code` has one entry for `inat-usage.md` with a non-empty hash.
  2. `inat refresh` → re-delivered without warnings; hash unchanged.
  3. **Upgrade-from-v0.3 simulation:** hand-edit `.inat/config.json` to delete the `builtinDelivered` field; hand-edit `.inat/rules/inat-usage.md` to remove its frontmatter (simulate v0.3 content). `inat refresh` → modified-file warning, no change. `inat refresh --force` → file re-delivered, `builtinDelivered.claude-code` repopulated.
  4. **Wiped `CLAUDE.md`:** `> CLAUDE.md` then `inat refresh` → managed section restored *with* the `inat-usage` bullet (regression guard for the gap that started this plan).
  5. **KitsRepo gating:** `inat init --agent claude-code --kits-repo` in a fresh dir; `inat refresh`; both built-ins recorded. Compare to a non-`KitsRepo` project: only `inat-usage`.
  6. **Empty edge case:** `inat init` (no agent), then `inat refresh` → "nothing to refresh", exit 0.
  7. **Multi-agent:** with both `claude-code` and a hypothetical second profile (or two project agents), refresh records both `BuiltinDelivered` entries.
- The local dev shim (`~/.local/bin/inat`) runs `go -C go/ run .`, so all smoke runs hit the working tree without rebuilding.

---

### 5. Risks

- **Existing `TestRefreshRestoresManagedSection` must still pass** after the early-return relaxation. The test installs a kit (so `cfg.Installed` is non-empty); the existing path is unchanged. Modification adds a built-in-bullet assertion only.
- **Refresh's per-agent built-in loop is the first place refresh writes to `cfg`.** Today refresh is read-only on `cfg`. The new write path needs the existing `config.SaveConfig` atomic-write helper (write-temp + rename) — already used by other commands; no new infra.
- **`agents add` ordering** — today the order is: append to `cfg.Agents`, save config, deliver built-ins. The new flow needs delivery to happen *before* the save so the hashes can be persisted in the same write. If delivery fails, `cfg.Agents` shouldn't already be appended on disk. Re-order: deliver built-ins (capture returns), then append to `cfg.Agents`, then `cfg.BuiltinDelivered[name] = …`, then save once. Same atomicity rule as refresh.
- **Built-in content with no `description` frontmatter** — the embedded `inat_usage.md` and `inat_kits.md` from v0.4 onward both ship descriptions (per spec 0010). If a future contributor strips one, `delivery.AddContextualReference` will be called with an empty description and render a bare bullet. That's a separate quality issue, not a refresh correctness issue.
- **Hash drift if embedded content changes mid-MR.** Tests should hash the embedded content via `hash.HashBytes(builtin.InatUsageContent)` rather than hardcoding hex strings, so editing the embedded `.md` doesn't require updating fixtures.
