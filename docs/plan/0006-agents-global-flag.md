# Plan — `agents add/remove --global` writes `~/.inat/config.json`

Spec: [`docs/specs/0006-agents-global-flag.md`](../specs/0006-agents-global-flag.md).

## Why

Spec 0001 §6 defines `~/.inat/config.json` as the global agent list, and §9 lists `inat agents add/remove`. The CLI **reads** the global file — `collect_active_agents` unions project + global, and `agents list` displays both — but **never writes it**. `save_global_config_to` at `src/core/config.rs:189` was `pub(crate)` + `#[allow(dead_code)]` with a code comment "Currently used only by tests; no production command writes `~/.inat/config.json` yet." Users who wanted a machine-wide agent had to hand-edit the file.

This closes that gap with a minimal `--global` flag on `agents add` / `agents remove`. Spec 0006 records the contract; this plan records the rollout.

## Decisions

- **Flag, not new command.** `inat agents add --global <name>` and `inat agents remove --global <name>`. No flag on `init`: `init` creates a project, not a global default.
- **Write-only semantics.** `--global add` updates `~/.inat/config.json` and prints a note. No file delivery anywhere — other projects pick up the new agent on their next `install`/`update`/`refresh`. Walking the filesystem to deliver everywhere is not atomic and explicitly rejected.
- **Remove is also write-only, with a warning.** `--global remove` prints that projects that delivered files to the agent while it was globally active will not be cleaned automatically. Orphan files are the user's responsibility (clean manually or re-install affected kits).
- **Duplicate check is scoped to the target list.** `agents add --global X` errors (`AgentAlreadyActive`) only if `X` is already in `~/.inat/config.json`. Project-level `X` is orthogonal — the union model allows an agent to appear in both lists; dedup happens in `collect_active_agents`.
- **Profile validation reused.** `agents::get_profile(name)?` runs for `--global add` so unknown agents fail fast before any file write.

## Changes

### Modified

- `src/main.rs` — `AgentsAction::Add` and `::Remove` gain `#[arg(long)] global: bool`; threaded through the match arms.
- `src/cli/agents.rs` — `run_agents_add` / `run_agents_remove` accept `global: bool` and dispatch. New `agents_add_global(name, home)` and `agents_remove_global(name, home)` with `home: Option<&Path>` for tests. Private `load_global_cfg` / `save_global_cfg` helpers pick the home-override path vs. the real `global_config_path()`.
- `src/core/config.rs` — removed `#[allow(dead_code)]` from `save_global_config_to`; added `pub fn save_global_config()` wrapper over `global_config_path()`; trimmed the stale doc comment.

### Tests added (7, all in `src/cli/agents.rs`)

- `add_global_writes_home_config` — round-trips through `~/.inat/config.json`; project config untouched; no files delivered.
- `add_global_when_already_global_returns_error` — `AgentAlreadyActive`.
- `add_global_allows_when_already_in_project` — confirms the scoped duplicate rule.
- `add_global_unknown_agent_returns_error` — `AgentNotFound`.
- `remove_global_writes_home_config` — agent removed from the file.
- `remove_global_not_active_returns_error` — `AgentNotActive`.
- `remove_global_does_not_clean_project_files` — install a kit while the agent is only global, then `--global remove`; delivered files remain on disk, documenting the orphan trade-off.

## Verification

1. `cargo fmt && cargo clippy --all-targets -- -D warnings && cargo test` — 186 tests pass (179 existing + 7 new).
2. Scratch smoke:
   ```bash
   rm -f ~/.inat/config.json
   inat agents add --global claude-code
   cat ~/.inat/config.json                  # {"agents":["claude-code"]}
   inat agents list                         # claude-code under "global agents"
   inat agents add --global claude-code     # AgentAlreadyActive
   inat agents remove --global claude-code
   cat ~/.inat/config.json                  # {"agents":[]}
   ```

## Out of scope

- Cross-project cleanup on `--global remove` (requires filesystem walking, not atomic).
- `init --global --agent <name>` — `init` is project-scoped.
- Changes to `agents list` output (already displays both lists).
- Spec update — §6 and §9 are still correct; the new flag is a natural extension.
