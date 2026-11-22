## inat — `agents add/remove --global` Spec

---

### 1. Overview

Spec [`0001-initial-idea.md`](0001-initial-idea.md) §6 defines `~/.inat/config.json` as the global agent list, and §9 lists the `inat agents add/remove` commands. The existing implementation **reads** the global file — `collect_active_agents` unions project + global, and `agents list` displays both — but **no command writes it**. Users who wanted a machine-wide agent had to hand-edit the file.

This spec extends §9 with a single opt-in flag on two existing commands. The base CLI surface and the union semantics from `0001 §6` are unchanged.

A companion implementation plan lives at `docs/plan/0006-agents-global-flag.md`.

---

### 2. Goals

1. `inat agents add <name> [--global]` MUST support a `--global` flag that targets `~/.inat/config.json` instead of `.inat/config.json`.
2. `inat agents remove <name> [--global]` MUST support the symmetric flag.
3. `--global` operations MUST be **write-only**: they MUST NOT deliver files, clean files, or otherwise touch any project on disk. Other projects pick up the new agent on their next `install`, `update`, or `refresh`.
4. Duplicate detection MUST be scoped to the target list. An agent may legitimately appear in both the project and the global list; `collect_active_agents` already dedupes the union (`0001 §6`).
5. `--global remove` MUST print a human-readable warning that delivered files in other projects will not be cleaned automatically.
6. Profile validation (`agents::get_profile`) MUST run before any write on `--global add`, so unknown agent names fail fast with `AgentNotFound`.

---

### 3. Config schema delta

None. `~/.inat/config.json` (`GlobalConfig`) and `.inat/config.json` (`Config`) schemas are unchanged.

---

### 4. CLI surface

```
inat agents add    <name> [--global]
inat agents remove <name> [--global]
```

- Omitting `--global` keeps the `0001 §9` project-scoped behaviour unchanged.
- `init --agent <name>` gains **no** `--global` flag; `init` creates a project, and initialising a project is not the right shape for a machine-wide default.
- `agents list` is unchanged; it already prints both lists separately.

Examples:

```
# Machine-wide default
inat agents add --global claude-code

# Project override (still works)
inat agents add claude-code

# Remove the machine-wide default
inat agents remove --global claude-code
```

---

### 5. Semantics

#### 5.1 `agents add --global <name>`

1. Validate profile via `agents::get_profile(name)`. Unknown profile → `AgentNotFound`.
2. Load `~/.inat/config.json` via `load_global_config` (returns default empty config if absent).
3. If `name` is already in `global.agents` → `AgentAlreadyActive { name }`.
4. Push `name`, write via `save_global_config`. Creates `~/.inat/` if missing.
5. Print a note directing the user to run `inat refresh` per project if they want immediate delivery.

No files are delivered. The project config is not loaded or written.

#### 5.2 `agents remove --global <name>`

1. Load `~/.inat/config.json`.
2. If `name` is not in `global.agents` → `AgentNotActive { name }`.
3. Remove from the list, write via `save_global_config`.
4. Print a warning: projects that previously delivered files to this agent will not be cleaned automatically; the user must remove files manually or re-install affected kits.

No files are touched on disk anywhere outside `~/.inat/config.json`.

#### 5.3 Duplicate-check scoping

`agents add --global X` errors only if `X` is already in the **global** list. `agents add X` (no flag) errors only if `X` is already in the **project** list. An agent that appears in both lists is a supported state; `collect_active_agents` dedupes the union per `0001 §6`.

---

### 6. Error model

| Condition | Error variant | Where raised |
|---|---|---|
| Unknown agent profile on `--global add` | `InatError::AgentNotFound { name }` | `agents_add_global`, before any write |
| Agent already in `~/.inat/config.json` on `--global add` | `InatError::AgentAlreadyActive { name }` | `agents_add_global` |
| Agent absent from `~/.inat/config.json` on `--global remove` | `InatError::AgentNotActive { name }` | `agents_remove_global` |
| Home directory cannot be resolved | `InatError::Io` | via `global_config_path` |
| `~/.inat/config.json` exists but is malformed | `InatError::Json` | `load_global_config_from` |

All error variants already exist; no new variants introduced.

---

### 7. Non-goals

- **No cross-project cleanup on `--global remove`.** Walking the filesystem to find every `.inat/config.json` and deleting files is not atomic and not safe. Orphan cleanup is the user's responsibility; `inat refresh` in a project will not delete orphaned files, it only re-delivers recorded state.
- **No `init --global`.** `init` is project-scoped; a global default is configured separately.
- **No `agents list --global` / `--project` filter.** The existing grouped output already distinguishes the two lists.
- **No auto-promotion.** `agents add X` does not also write to the global file, and vice versa. Each command targets exactly one list.
- **No spec 0001 text rewrite.** §6 and §9 remain correct; this spec is additive.

---

### 8. References

- [`0001-initial-idea.md`](0001-initial-idea.md) §6 (global config), §9 (CLI).
- `src/core/config.rs` — `GlobalConfig`, `load_global_config`, `save_global_config`, `collect_active_agents`.
- `src/cli/agents.rs` — `agents_add_global`, `agents_remove_global`.
- `docs/plan/0006-agents-global-flag.md` — implementation plan.

---

### 9. Acceptance

The spec is satisfied when:

1. `inat agents add --global claude-code` writes `{"agents":["claude-code"]}` to `~/.inat/config.json`, leaves the current project's `.inat/config.json` unchanged, and delivers no files.
2. A second `inat agents add --global claude-code` fails with `AgentAlreadyActive`.
3. `inat agents add --global unknown-agent` fails with `AgentNotFound` and does not create or modify `~/.inat/config.json`.
4. With `claude-code` in the project's `config.agents`, `inat agents add --global claude-code` succeeds (confirms scoped duplicate rule).
5. `inat agents remove --global claude-code` removes the entry, writes the file, and prints a warning mentioning orphaned files.
6. `inat agents remove --global claude-code` on an empty global list fails with `AgentNotActive`.
7. After `--global add` followed by `inat install <kit>` in a fresh project, the kit is delivered to the global agent's targets (lazy pickup via the §6 union).
8. `inat agents remove --global X` does not delete any file on disk outside `~/.inat/config.json`.
