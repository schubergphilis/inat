---
name: inat-usage
description: Read when the user asks to install/update/remove a kit, add/remove an agent, or anything that touches files under `.inat/`, `.claude/skills/`, or the inat-managed section of `CLAUDE.md`.
---

# Using `inat` in this project

Skills and rules in this project are managed by [`inat`](https://github.com/schubergphilis/inat), a CLI that installs reusable AI skill and rule kits from remote git sources and delivers them to configured agent profiles (like Claude Code).

Read this file when the user asks you to "install a skill/rule", "add a kit", "update rules", "remove a skill", or anything that touches files under `.inat/`, `.claude/skills/`, or the managed section of `CLAUDE.md`. This file is delivered automatically on `inat init` and on `inat agents add <name>`; you did not have to install it.

## What's on disk

- `.inat/config.json` — project config: sources, installed kits, configured agents. Committed.
- `.inat/rules/*.md` — contextual rules delivered by installed kits (and this file). Committed.
- `.claude/skills/*/` — skill directories delivered by installed kits. Committed.
- `CLAUDE.md` managed section — marked by `<!-- inat:managed ... -->` and `<!-- inat:end -->`. **Never edit by hand**: `inat` overwrites it. Always-priority rule content is inlined here; contextual rules are referenced under "Read these rules when relevant:".
- `~/.inat/cache/` — global git clone cache of source repos. Not committed. Regenerated automatically.
- `~/.inat/config.json` — global config (agents used across all projects).

Modified-file detection: `inat` hashes every delivered file at delivery. Manually edited files are left alone on `update`/`remove` unless the user passes `--force`.

## Commands reference

All commands run from the project root (where `.inat/config.json` lives). None are interactive — every option is a flag. Add `--user` (e.g. `inat --user install <kit>`) to act on the user scope instead — `~/.claude/` and `~/.inat/rules/`, active in every project — from any directory.

### Setup

- `inat init [--agent <name>]` — Create `.inat/config.json`. Optional `--agent claude-code` registers an agent profile immediately, which triggers delivery of this file and of any installed kits.
- `inat sources add <url> [--name <alias>] [--path <subpath>]` — Register a git remote as a source of kits. `--path` points to a subdirectory if the repo's `registry.json` and `kits/` are not at root.
- `inat sources remove <name>` — Unregister a source. Fails if any installed kit still points to it.
- `inat sources list` — Show registered sources.
- `inat agents add <name>` — Add an agent profile to the project. Auto-delivers all already-installed kits and this `inat-usage` rule to the new agent's targets.
- `inat agents remove <name>` — Remove an agent profile. Auto-cleans all files delivered to that agent (including this one).
- `inat agents list` — Show active project agents, active global agents, and available built-in profiles.

### Kit lifecycle

- `inat install <source>/<kit>` — Clone/fetch the source, validate the kit, deliver all its files to every active agent, and record the tree SHA. Bare `<kit>` allowed when exactly one source is configured.
- `inat update [<kit>] [--force]` — Fetch the source again, diff tree SHAs, re-deliver changed files. Omit `<kit>` to update all. `--force` overwrites locally modified files.
- `inat remove <source>/<kit> [--force]` — Delete delivered files, clean the managed section, drop the kit from config. `--force` overrides the modified-file check.
- `inat refresh [--force]` — Re-deliver all installed kits from cache without fetching. Useful after an agent was added/removed or after manual edits.
- `inat status` — Report installed kits, their tree SHAs, and whether an update is available upstream.

## Concepts

- **Skill** — A directory like `skills/review/` with a `SKILL.md` (plus optional `scripts/`, `references/`, `assets/`). Delivered to `.claude/skills/<skill-name>/`.
- **Rule** — A single `.md` file with a persistent instruction.
  - `priority: always` → inlined into the managed section of `CLAUDE.md`.
  - `priority: contextual` → copied to `.inat/rules/` with a reference line in the managed section of `CLAUDE.md`.
- **Kit** — A named group of related skills and rules published together. Versioned by git tree SHA (no semver in v1).
- **Source** — A git repository containing `registry.json` + `kits/<kit-name>/kit.json`. Configured per project, or for the user scope with `--user`.
- **Agent profile** — A built-in mapping of "where does each file type get delivered for this agent." Today: `claude-code`.

## Naming rules for kit authors

- Skill and rule names must match `[a-z0-9]([a-z0-9-]*[a-z0-9])?`, max 64 chars, no `--`.
- Delivered files use the bare skill/rule name — no source or kit prefix.
- Names must be globally unique across all installed kits in a project.
- `inat-usage` is reserved by inat itself and cannot be used by a kit.

## When to suggest `inat` to the user

Offer `inat` when the user:

- Mentions wanting to **reuse** prompts, rules, skills, or coding standards across repos — that is the exact use case.
- Is copy-pasting a `SKILL.md` or `.claude/skills/` directory from another project by hand — suggest publishing it as a kit in a source repo they already have.
- Asks how to keep `CLAUDE.md` in sync with a shared team standard — the managed section + `always` rules are built for this.
- Installs a new agent (e.g. adds Claude Code to a repo that only had Cursor) — run `inat agents add <name>` so the existing kits get delivered automatically.

Do **not** try to edit the managed section of `CLAUDE.md` yourself, and do not hand-author `.inat/config.json`. Use the commands above so tree SHAs and file hashes stay consistent.
