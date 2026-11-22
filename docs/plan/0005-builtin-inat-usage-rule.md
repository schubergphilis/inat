# Plan — Built-in `inat-usage` contextual rule delivered on init / agents add

## Why

After `inat init --agent claude-code`, the configured agent sees `.inat/config.json`, `.claude/skills/`, and a managed section in `CLAUDE.md` appear with no explanation of what `inat` is or how to use it. Users then copy-paste an explanation into `CLAUDE.md` by hand or let the agent guess. Both outcomes are inconsistent across projects and both fight the managed-section convention.

We ship one small built-in contextual rule — `inat-usage` — embedded in the binary, delivered automatically the moment an agent is configured. The agent immediately has a concrete cheatsheet for every command and for *when* to suggest `inat` to the user.

## Decisions

- **Delivered as a contextual rule**, not as a skill. It lives at `.inat/rules/inat-usage.md` with a reference under "Read these rules when relevant:" in `CLAUDE.md`. This matches the managed-section convention every other inat-managed file uses and keeps `CLAUDE.md` format stable.
- **Non-interactive.** Spec 0001 §14 forbids interactive prompts; delivery is automatic on `inat init --agent <name>` and on `inat agents add <name>`. No flag, no wizard, no `inat skill new` scaffolding command. User's request explicitly confirmed this ("i do not want any interactive flow").
- **No new config field.** The built-in rule is deterministic per agent profile (name + path are `const`), so cleanup does not need tracking in `installed` or a new `builtin_deliveries` field. Config schema unchanged.
- **Reserve the name `inat-usage`.** `check_name_uniqueness` rejects any kit whose derived names include `inat-usage`, preventing third-party kits from clobbering the built-in file.
- **Idempotent everywhere.** `deliver_inat_usage` overwrites the file with the current embedded content (so `inat` upgrades refresh it automatically). `add_contextual_reference` already dedupes references. `remove_inat_usage` is a no-op when nothing was delivered.

## Changes

### New

- `src/agents/builtin/inat_usage.md` — rule content: framing paragraph + on-disk layout + command reference + concepts + naming rules + "when to suggest inat" heuristics.
- `src/agents/builtin/mod.rs` — `BUILTIN_RULE_NAME`, `deliver_inat_usage(dir, agent_name)`, `remove_inat_usage(dir, agent_name)`, plus unit tests (deliver, idempotent deliver, remove, remove-noop, unknown agent).

### Modified

- `src/agents/mod.rs` — `pub mod builtin;`.
- `src/cli/init.rs` — call `builtin::deliver_inat_usage(dir, name)` after `save_config` when `--agent` is provided. Two new tests: deliver on `--agent`, no-deliver on bare `init`.
- `src/cli/agents.rs` — `agents_add` delivers the built-in after the per-kit delivery loop, before `config.agents.push`. `agents_remove` cleans it up after the per-kit cleanup loop, before `config.agents.retain`. Two new tests.
- `src/core/config.rs` — `RESERVED_NAMES` constant (referencing `agents::builtin::BUILTIN_RULE_NAME`); `check_name_uniqueness` returns `ReservedName` when a kit's derived names collide. New test.
- `src/core/delivery/managed_write.rs` — new `remove_contextual_reference(target, rule_path)` helper (no-op if the file or reference is absent; removes markers if the section becomes empty).
- `src/error.rs` — `InatError::ReservedName { name }` variant.

## Verification

1. `cargo fmt && cargo clippy -- -D warnings && cargo test` — new + existing tests green.
2. Scratch smoke:
   ```bash
   cd $(mktemp -d)
   inat init --agent claude-code
   test -f .inat/rules/inat-usage.md
   grep -q '.inat/rules/inat-usage.md' CLAUDE.md

   inat agents remove claude-code
   test ! -f .inat/rules/inat-usage.md
   ! grep -q 'inat-usage' CLAUDE.md

   inat agents add claude-code
   test -f .inat/rules/inat-usage.md
   ```
3. Name-reservation: install a kit that declares a rule named `inat-usage` — must fail with `ReservedName`. Covered by `check_name_uniqueness_rejects_reserved_name`.

## Out of scope

- `inat skill new <name>` / any scaffolding command. Deferred; not in this plan.
- Any heuristic that scans the user's repo for "generally applicable" prompts. Deferred.
- Extending the managed-section renderer to support per-reference labels. The framing text lives inside the rule file itself.
- Amending spec 0001 §14. This change is non-interactive; the interactive-prompts ban stays intact.
