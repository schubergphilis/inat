## inat — `inat refresh` re-delivers built-in rules (Spec 0011)

---

### 1. Overview

`inat refresh` today iterates only `cfg.Installed` and never touches the two built-in rules `inat-usage` and `inat-kits`. The built-ins are delivered fire-and-forget by `inat agents add` and `inat init --agent`: the `DeliveredFile{Path, Hash}` returned by the underlying delivery helper is discarded, and the rule's existence is implicit in the agent's registration rather than tracked in config.

The consequence — surfaced after spec 0010 shipped — is that any binary upgrade that changes built-in content (the v0.3 → v0.4 frontmatter addition is the first concrete instance) leaves stale content on disk forever. The only manual remedy is `inat agents remove <name> && inat agents add <name>`, which is friction users won't discover and isn't documented.

Spec 0010 §8 acceptance criterion 7 *describes* the desired behaviour:

> *"`inat refresh --force` does not require modified-file overrides for built-in rules whose embedded content gained frontmatter — the on-disk hash matches the recorded hash, so the modified-file guard does not trip."*

The implementation does not exist; this spec adds it.

Two adjacent gaps surfaced during exploration are also in scope:

- `refresh.go:38-41` early-returns when `cfg.Installed` is empty, so a user with one agent and zero installed kits gets nothing from refresh — even though built-ins exist.
- The existing `TestRefreshRestoresManagedSection` proves the kit half of managed-section restoration works after a `CLAUDE.md` wipe; built-in bullets are silently lost in the same scenario because no code path re-adds them during refresh.

A companion implementation plan lives at `docs/plan/0011-refresh-builtin-rules.md`.

This spec extends spec [`0001-initial-idea.md`](0001-initial-idea.md) and closes the implementation gap left by spec [`0010-rule-descriptions.md`](0010-rule-descriptions.md) §8 criterion 7. It does not alter the Rust tree under `rust/` (frozen archive); all changes are in `go/`.

---

### 2. Goals

1. `inat refresh` MUST re-deliver `inat-usage` (and `inat-kits` when `cfg.KitsRepo`) for every active agent, with the same hash-guarded semantics that already protect installed-kit files.
2. Built-in rule deliveries MUST be tracked in `.inat/config.json` so subsequent refreshes can distinguish "user edited the file" from "the binary changed the embedded content."
3. The schema change MUST be additive: configs written by older inat binaries MUST round-trip through the new binary without loss, and configs written by the new binary that have no built-ins recorded MUST not gain a noisy field on disk.
4. The state machine MUST satisfy spec 0010 §8 criterion 7: when an upgrade ships new built-in content and the on-disk hash matches the recorded hash from the prior version, refresh re-delivers without surfacing a modified-file warning and without requiring `--force`.
5. The managed-section bullet for each built-in (path + frontmatter description) MUST be sync'd on every refresh, regardless of whether the file body needed re-delivery, so a wiped or hand-edited `CLAUDE.md` recovers all bullets — installed and built-in — in one pass.
6. `inat refresh` MUST run when there are active agents but zero installed kits (the early-return guard MUST relax accordingly).
7. `inat agents add`, `inat agents remove`, and `inat init --agent` MUST keep the new tracking field consistent: `add`/`init` populate it, `remove` clears it.
8. The implementation MUST add no new Go module dependencies (CLAUDE.md rule 8): cobra + go-git + stdlib remain the entire allowlist.

---

### 3. Non-goals

- An automatic backfill of `BuiltinDelivered` from on-disk hashes on first config load. The migration friction (one `--force` invocation for v0.3 → v0.4 upgraders, documented in release notes) is preferable to silently blessing potentially user-edited content as the recorded baseline.
- Embedding a list of "known prior content hashes" inside the binary so the migration `--force` is unnecessary. Maintaining such a list per release is real complexity for a one-time benefit.
- A new top-level command (`inat upgrade`, `inat refresh --builtins`). Refresh is the entry point per spec 0010 §8 criterion 7; one command, one mental model.
- Restructuring `Config.Agents` from `[]string` to a struct array. The new tracking is a sidecar map keyed by agent name; existing tests and callers stay untouched.
- Defensive removal of `inat-kits.md` when `cfg.KitsRepo` flips from `true` to `false`. No CLI path exposes that flip today; the cell is reachable only by hand-edit, which is the user signalling intent. Cleanup is the user's call via `agents remove` + `agents add`.
- Auto-delivery of `inat-kits` to existing agents when `inat init --kits-repo` is re-run after `agents add`. Pre-existing edge case, not in this spec's scope.

---

### 4. Config schema

A new optional field on `Config`:

```go
type Config struct {
    Sources          []Source
    Installed        map[string]InstalledKit
    Agents           []string
    KitsRepo         bool
    BuiltinDelivered map[string][]DeliveredFile `json:"builtinDelivered,omitempty"`
}
```

- Keys are agent names (matching entries in `Config.Agents`).
- Values are slices of `DeliveredFile{Path, Hash}` — same struct already used by `InstalledKit.Delivered`.
- `omitempty` ensures configs without any tracked built-ins serialise without the field, matching the v0.3 wire format byte-for-byte. Existing tests that compare round-tripped JSON byte-strings (e.g. `TestConfigOmitsKitsRepoFieldWhenFalse`) keep passing.

The field is populated by `inat init --agent`, `inat agents add`, and `inat refresh`. It is cleared (per agent) by `inat agents remove`.

---

### 5. Refresh state machine

For each active agent (project + global, via `config.CollectActiveAgents`), and for each applicable built-in (`inat-usage` always; `inat-kits` only when `cfg.KitsRepo`):

```
recorded     := lookup(cfg.BuiltinDelivered[agent], embeddedPath)
embeddedHash := hash(embedded content)
onDiskHash, statErr := hash(file on disk)

if file missing (statErr is "not exist"):
    deliver embedded content
    record(embeddedHash)

else if recorded == nil:
    if onDiskHash == embeddedHash:
        record(embeddedHash)            # silent backfill — same content, no write
    elif force:
        deliver + record(embeddedHash)
    else:
        modified-file warning           # migration friction (v0.3 → v0.4 case)
        no change

else if recorded.Hash == embeddedHash:
    no-op on file                       # already current

else if onDiskHash == recorded.Hash:    # criterion 7 path
    deliver + record(embeddedHash)      # safe upgrade, no warning

else:                                   # user edited under the recorded baseline
    if force: deliver + record(embeddedHash)
    else:     modified-file warning, no change
```

Independent of every branch above, refresh MUST call `delivery.AddContextualReference(refTarget, refPath, fm.Description)` for the built-in. The frontmatter description comes from the **current binary's embedded content**, so the bullet's text always reflects the running version even when the file body wasn't re-delivered.

`refTarget` and `refPath` are derived from the agent profile's `Mapping.Rule.Contextual` entries — the same source of truth used by `agents add`.

---

### 6. Migration

Three scenarios cover the upgrade transition for any binary that lacked `BuiltinDelivered`:

| State at first refresh after upgrade | Behaviour | Typical source version |
|---|---|---|
| `BuiltinDelivered` missing AND on-disk content == new embedded content | Silent backfill: record hash, no write, no warning. | Coming from v0.4.0 (frontmatter content already on disk; just missing the tracking field). |
| `BuiltinDelivered` missing AND on-disk content != new embedded content | Modified-file warning. No change. User runs `inat refresh --force` once; file is re-delivered, hash recorded. From then on, refreshes work without `--force`. | Coming from v0.3.x (no frontmatter on disk yet). |
| `BuiltinDelivered` present | Spec 0010 §8 criterion 7 path: on-disk == recorded → silent re-delivery on next embedded-content change; on-disk != recorded → user-edit warning unless `--force`. | Coming from any version that shipped this spec (v0.4.1 onward). |

The release notes / `CHANGELOG.md` for the version shipping this change MUST document the one-time `inat refresh --force` step for v0.3.x upgraders, and call out that v0.4.0 upgraders need nothing (silent backfill). From v0.4.1 onward, future binary upgrades that change embedded built-in content require nothing extra: criterion 7 covers them.

---

### 7. KitsRepo semantics

`cfg.KitsRepo` is the single source of truth for whether `inat-kits` should be present in a project:

- `KitsRepo == true` → ensure `inat-kits.md` exists per the §5 state machine; record its hash; sync its bullet.
- `KitsRepo == false` → skip `inat-kits` entirely. Do not check on-disk state, do not modify the file, do not remove the bullet. Stale `BuiltinDelivered[agent]` entries for `inat-kits` (reachable only via hand-edit of config) are left as-is; refresh does not "clean up" them.

This matches the project's general posture toward hand-edited config: respect the user's intent rather than second-guess it. The legitimate cleanup path remains `inat agents remove <name> && inat agents add <name>`.

---

### 8. Empty-state handling

`inat refresh` MUST NOT early-return when `cfg.Installed` is empty if `activeAgents` is non-empty. The new guard:

```go
activeAgents, err := config.CollectActiveAgents(&cfg, homeOverride)
if err != nil {
    return err
}
if len(cfg.Installed) == 0 && len(activeAgents) == 0 {
    fmt.Fprintln(os.Stderr, "nothing to refresh")
    return nil
}
```

`CollectActiveAgents` moves above the guard so its result informs the decision. The "nothing to refresh" message replaces "no kits installed" only in the truly-empty case; when there are agents, refresh proceeds and emits its usual per-kit / per-built-in messages.

---

### 9. Atomicity

Refresh MUST follow the same write-config-last contract that other inat commands use:

1. Read `cfg` once.
2. Iterate kits, then built-ins, accumulating the new `BuiltinDelivered` map in memory.
3. Fail fast on per-agent error: any error from `RefreshInatUsage` / `RefreshInatKits` aborts the loop, no config write.
4. On success, replace `cfg.BuiltinDelivered` wholesale with the new map and call `config.SaveConfig` once.

If refresh aborts partway after some file writes have completed but before the config save, the next refresh is self-healing: the §5 state machine sees "no recorded hash, on-disk == embedded" → silent backfill, then continues from there.

---

### 10. Acceptance criteria

1. After `inat init --agent claude-code` on a fresh project, `.inat/config.json` contains `builtinDelivered.claude-code` with one entry whose `Path` ends in `inat-usage.md` and whose `Hash` is the SHA-256 of the embedded content.
2. After `inat init --agent claude-code --kits-repo`, `builtinDelivered.claude-code` has two entries (`inat-usage.md`, `inat-kits.md`).
3. `inat refresh` on a fresh project re-delivers without warnings and without changing recorded hashes.
4. **Spec 0010 §8 criterion 7 path:** with `BuiltinDelivered.claude-code` recorded against an old hash and the binary now embedding new content, `inat refresh` (without `--force`) re-delivers the file when on-disk == recorded; the modified-file guard does not trip.
5. **Migration path:** with `BuiltinDelivered` deleted from config and on-disk content != embedded content, `inat refresh` emits a modified-file warning and makes no change. `inat refresh --force` then re-delivers the file and populates `BuiltinDelivered`.
6. After a user wipes `CLAUDE.md` (or its managed section), `inat refresh` restores the managed section with both installed-kit bullets *and* the `inat-usage` bullet (and `inat-kits` when `KitsRepo`). The bullet description text comes from the binary's current embedded frontmatter.
7. `inat refresh` on a project with `cfg.Installed` empty and one active agent re-delivers built-ins. On a project with both empty, refresh prints "nothing to refresh" and exits 0.
8. `inat agents remove claude-code` deletes the `claude-code` entry from `cfg.BuiltinDelivered`. `inat agents add claude-code` re-populates it.
9. With `cfg.KitsRepo == false`, `inat-kits.md` is never delivered or modified by refresh, even if a stale `BuiltinDelivered` entry for it exists in config (reachable only via hand-edit).
10. The full pre-commit gate (`gofmt -l .` empty, `go vet ./...` clean, `staticcheck ./...` clean, `go test ./...` green) passes on the implementation MR.

---

### 11. Reserved-name interaction

The reserved names `inat-usage` and `inat-kits` (per spec 0001 §6) continue to apply unchanged. Kits MUST NOT ship rules deriving to either name; this spec adds tracking and refresh semantics for the two reserved built-ins but does not change what is reserved.
