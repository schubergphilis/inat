## inat — User scope (`--user`) Spec

---

### 1. Overview

Spec [`0001-initial-idea.md`](0001-initial-idea.md) makes every install project-scoped: §10 rule 11 says "Sources are project-only. There is no global source configuration", and §14 lists "Global source configuration" as out of scope. Spec [`0006-agents-global-flag.md`](0006-agents-global-flag.md) added a machine-wide **agent list** to `~/.inat/config.json`, but no sources, kits or delivery.

This spec reverses §10 rule 11 and the matching §14 bullet. It adds a **user scope**, where kits are installed once per user and delivered to the agent's user-level targets (for `claude-code`: `~/.claude/CLAUDE.md` and `~/.claude/skills/`). Those targets load into every session regardless of the working directory.

**Motivation.** Some instructions apply to every project a user works in. Today they are hand-maintained in `~/.claude/CLAUDE.md` and loaded in full into every session. As inat kits they can be versioned, shared, and split by priority. `always` rules stay inline. `contextual` rules become a one-line "read when…" reference, which is the token saving: a contextual rule costs one line until it is relevant.

A companion implementation plan lives at `docs/plan/0025-user-scope.md`.

---

### 2. Goals

1. A root-level persistent flag `--user` MUST switch every scope-aware command to the user scope: `sources add/remove/list`, `agents add/remove/list`, `install`, `update`, `remove`, `refresh`, `status`, `rename`, `replace`, `sync`.
2. In user scope the **scope root** is the user's home directory (`config.HomeDir()`), and the scope config is `~/.inat/config.json`.
3. User-scope delivery MUST use a **user variant** of each agent profile (§5). For `claude-code`, always-rules go to `~/.claude/CLAUDE.md`, skills go to `~/.claude/skills/`, and contextual rules go to `~/.inat/rules/`.
4. Contextual-rule references written into a user-scope managed section MUST resolve from any working directory. They MUST be written in `~/`-prefixed form (`~/.inat/rules/<name>.md`) and never as an absolute path. This keeps the reference valid when the same `CLAUDE.md` is bind-mounted into a container whose home differs from the host's.
5. Every behaviour rule in spec 0001 §10 applies unchanged inside the user scope: atomic rollback, modified-file detection, name uniqueness, managed-section integrity, and source validation and removal guard.
6. Project scope (no `--user`) MUST behave exactly as in v0.7.2, apart from the collision warning in §6.3.

---

### 3. Config schema delta

`~/.inat/config.json` grows from the spec 0006 `GlobalConfig` (`agents` only) to the full project `Config` shape:

```json
{
  "sources": [ { "name": "...", "url": "...", "path": "..." } ],
  "installed": { "<source>/<kit>": { "tree": "...", "names": [...], "delivered": [...] } },
  "agents": ["claude-code"],
  "builtinDelivered": { "...": [...] }
}
```

- All four fields are optional when the file is read. A spec-0006 file containing only `agents` loads as a user scope with no sources and no kits.
- `kits_repo` MUST NOT appear in the user config. Kit authoring is a property of a repository, not of a user.
- `agents` keeps its spec 0006 meaning (it is unioned into every project's active agents, §6 of spec 0001). It also becomes the user scope's own active-agent list. See §6.1.
- Delivered-file paths in `installed[*].delivered` are relative to the home directory (`.claude/skills/review/SKILL.md`, `.inat/rules/x.md`), in the same way that project paths are relative to the project root.

---

### 4. CLI surface

```
inat --user sources add <url> [--name N] [--path P]
inat --user sources remove <name>
inat --user sources list
inat --user agents add <name>
inat --user agents remove <name>
inat --user agents list
inat --user install <kit>
inat --user update [<kit>] [--plan] [--force] ...
inat --user remove <kit> [--force]
inat --user refresh [--force]
inat --user status
inat --user rename <old> <new>
inat --user replace <old> <new> [--plan] [--force]
inat --user sync
```

- `--user` is a persistent flag on the root command, so `inat install --user x` is also accepted.
- `init`, `validate` and `self` reject `--user` with a usage error. The user scope needs no `init`: the config file is created on first write.
- Every existing flag of each command works unchanged under `--user`.
- `agents add/remove --global` (spec 0006) is kept. See §6.1 for how it relates to `--user`.

---

### 5. Profile user variant

Each built-in profile gains a user-scope mapping. For `claude-code`:

| Item | Project scope (unchanged) | User scope |
|---|---|---|
| Skills | `.claude/skills/` | `.claude/skills/` (under `~`) |
| Always rules (managed section) | `CLAUDE.md` | `.claude/CLAUDE.md` (under `~`) |
| Contextual rule files | `.inat/rules/` | `.inat/rules/` (under `~`) |
| Contextual reference target | `CLAUDE.md` | `.claude/CLAUDE.md` (under `~`) |
| Reference text written | `.inat/rules/<name>.md` | `~/.inat/rules/<name>.md` |

Contextual rules deliberately do **not** go to `~/.claude/rules/`. Claude Code loads markdown files from that directory automatically, so placing them there would load every contextual rule in full and defeat the purpose of the contextual priority.

The mapping therefore separates two things that are currently the same string: the **file path** (relative to the scope root, recorded in config) and the **reference text** (written into the managed section). In project scope they stay identical.

---

### 6. Semantics

#### 6.1 Agents

- `inat --user agents add <name>` adds `<name>` to `~/.inat/config.json#agents` and delivers every user-scope kit (plus the built-in `inat-usage` rule) to that agent's user targets, atomically. This is spec 0001 §10 rule 13 applied to the user scope.
- `inat --user agents remove <name>` removes it from the list and cleans that agent's user-scope deliveries, atomically.
- `agents add/remove --global` (spec 0006) keeps its write-only behaviour. It edits the same list but delivers and cleans nothing. Running `inat --user refresh` afterwards brings user-scope delivery in line with the list.
- Because the list is shared, an agent that is active at user scope is also active in every project (spec 0001 §6 union). This is accepted: user scope exists for agents the user runs everywhere.

#### 6.2 Built-in rules

`inat-usage` is delivered at user scope like any contextual rule (`~/.inat/rules/inat-usage.md`, referenced from `~/.claude/CLAUDE.md`). `inat-kits` is never delivered at user scope, because it depends on `kits_repo`. Both names stay reserved in the user scope.

#### 6.3 Collisions between scopes

The scopes are independent. Name uniqueness (spec 0001 §10 rule 6) is enforced **within** a scope only.

- A project-scope `install` or `update` that delivers a skill or rule name the user scope already delivers succeeds, and prints one warning line per colliding name that names the other scope. The reverse direction (`--user install` against the current project's config, when the cwd holds one) is handled the same way.
- Detection is best effort. It reads the other scope's config only if it exists and parses. If it doesn't, nothing is reported. It never blocks and never fails the command.
- Which copy the agent actually uses is the agent's own precedence rule, not inat's.

#### 6.4 Home directory as a project

If the cwd is the home directory, `inat init` and every other scope-aware command run without `--user` MUST refuse with a clear error. A project rooted at `~` would share `~/.inat/config.json` with the user scope. Today such a command silently reads the spec 0006 agents file as if it were a project config.

Internally, "the scope root is the home directory" is therefore equivalent to "user scope". The implementation may rely on that equivalence.

#### 6.5 Compatibility notes

- **Older binaries.** Binaries up to v0.7.2 read `~/.inat/config.json` as `GlobalConfig`, so `agents add/remove --global` from an old binary rewrites the file as `agents` only and **drops** user-scope `sources` and `installed`. Delivered files survive, but inat no longer tracks them. This is accepted, since inat has no user base beyond its owner, and the release notes call it out. From this release on, `--global` reads and writes the full user config, so it preserves `sources` and `installed`.
- **Windows home.** User scope writes into `~/.claude/`, which Claude Code locates as `%USERPROFILE%\.claude\` on Windows. inat must resolve the same folder. `config.HomeDir()` MUST become `os.UserHomeDir()` for every scope: `%USERPROFILE%` on Windows and `$HOME` elsewhere. This restores the Rust behaviour (`rust/src/core/config.rs` `dirs_home`). The spec 0009 Go port changed it to `$HOME`-first, citing a CI convention that no longer applies, because `go test` runs on Linux only. Side effect: on a Windows machine where `$HOME` ≠ `%USERPROFILE%`, the cache and the spec 0006 agents list move to `%USERPROFILE%\.inat\` once. The cache re-clones itself, and global agents have to be re-added.

---

### 7. Error model

| Condition | Error | Where |
|---|---|---|
| `--user` on `init`, `validate`, `self` | usage error (cobra) | flag parsing |
| Any scope-aware command without `--user`, or `init`, with cwd == home | new `errs.Kind` `HomeAsProject` | scope resolution |
| `kits_repo: true` found in `~/.inat/config.json` | new `errs.Kind` `ConfigInvalid`, naming the field | user-config load |
| Home cannot be resolved | `errs.WrapIO` | `config.HomeDir` (existing) |

Every other error is the same one the project-scope command already raises.

---

### 8. Non-goals

- **Container mounts.** Making host-side user-scope deliveries visible inside claude-docker boxes is claude-docker's job, and it needs nothing from inat. As of 2026-11-16 (claude-docker 350b19d):
  - **Rules.** A box bind-mounts the host's `~/.inat/rules/` read-only at the same `~/`-relative path, so the `~/.inat/rules/<name>.md` references from §2 goal 4 resolve inside it. Only `rules/` is mounted; `~/.inat/config.json` and the cache stay host-only, so `--user` commands run on the host.
  - **Skills.** No change was needed. At launch claude-docker copies the host's `~/.claude/skills/` (symlinks resolved) into a per-launch directory and mounts it read-only as the box's `~/.claude/skills/`. Boxes ship no skills of their own, so a box sees exactly the host's user-scope skills, without merging. `~/.claude/agents/` and `~/.claude/commands/` are copied the same way.
  - **Snapshot semantics.** Both reach a box at launch: a host-side `inat --user install` or `update` shows up in a box only after it is relaunched.
  - **Precedence.** Project-scope skills (the repo's `.claude/skills/`) are a separate scope; Claude Code, not inat, decides which wins on a name clash.
  - **Accepted exceptions** to "box config comes only from the repo or from host user folders via inat": plugins (on the shared `claude-code-home` volume), claude.ai org-synced skills (`skills/synced/`, carried in the host snapshot), and claude-docker's own hooks.
- **Extracting the existing `~/.claude/CLAUDE.md` rules into a kit.** That is a separate change in a kits repo.
- **Migrating hand-written content.** inat only manages its own marked section. User text in `~/.claude/CLAUDE.md` is never touched (spec 0001 §8.3).
- **A third scope** (enterprise, machine-wide) or per-scope precedence enforced by inat.
- **Cross-project cleanup** on `--user agents remove` or `--global remove`, as in spec 0006 §7.

---

### 9. Spec 0001 amendments

- §10 rule 11 becomes: "Sources are project-scoped, or user-scoped under `--user` (spec 0025). There is no machine-wide source list shared across projects."
- The §14 bullet "Global source configuration" is removed.

---

### 10. Acceptance

The spec is satisfied when, with `HOME` pointed at a temporary directory:

1. `inat --user sources add <url>` writes `sources` to `$HOME/.inat/config.json` and leaves any `agents` entry already in the file untouched. A later `inat agents add --global <name>` leaves `sources` and `installed` untouched.
2. `inat --user agents add claude-code` delivers `inat-usage` to `$HOME/.inat/rules/inat-usage.md`. `$HOME/.claude/CLAUDE.md` gets a managed section whose reference reads `- ~/.inat/rules/inat-usage.md …`.
3. `inat --user install <src>/<kit>`, for a kit with one skill, one always rule and one contextual rule, produces `$HOME/.claude/skills/<skill>/SKILL.md`, an inlined block in `$HOME/.claude/CLAUDE.md`, `$HOME/.inat/rules/<rule>.md`, and a `~/.inat/rules/<rule>.md` reference. Nothing is written under the cwd.
4. User text above and below the managed markers in `$HOME/.claude/CLAUDE.md` is byte-identical before and after install and remove.
5. `inat --user remove <kit>` removes exactly the files from item 3, plus the block and the reference.
6. `inat --user update`, `refresh` and `status` behave as their project counterparts do against the user config, including drift skip and `--force`.
7. A project install of a kit that delivers a name the user scope already has succeeds and prints a collision warning.
8. `inat init` and `inat status` (no `--user`) with cwd == `$HOME` fail with `HomeAsProject`.
9. `inat --user init` fails with a usage error.
10. `config.HomeDir()` returns `os.UserHomeDir()`. A unit test pins that on non-Windows it follows `$HOME`. Windows behaviour is the standard library's and is checked manually in item 12.
11. Every existing test passes unchanged (project scope did not regress).
12. Run manually on the Windows host: after `inat --user install` of a test kit, a fresh Claude Code session started in an unrelated directory shows the inlined rule and the `~/`-form reference in its loaded user memory.

---

### 11. References

- [`0001-initial-idea.md`](0001-initial-idea.md) §5, §6, §7, §8.3, §10, §14.
- [`0006-agents-global-flag.md`](0006-agents-global-flag.md): global agent list.
- `go/agents/agents.go`: `ClaudeCodeProfile`.
- `go/core/config/config.go`: `Config`, `GlobalConfig`, `HomeDir`.
- `go/cli/install/deliver.go`: `deliverContextualRule` (file path vs reference text).
- `go/agents/builtin/rules.go`: built-in rule delivery.
