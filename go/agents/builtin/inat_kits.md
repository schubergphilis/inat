---
name: inat-kits
description: Read when authoring or editing a kit — covers kit.json schema, file types, contextual vs always priority, naming rules, and the rule-frontmatter requirement for contextual rules.
---

# Authoring `inat` kits in this repo

This repository is a **kits-publishing source** for [`inat`](https://github.com/schubergphilis/inat): it exposes a `registry.json` at the root (or under a subpath) plus kit directories under `kits/`, to be cloned and installed by consumer projects. This file is delivered automatically because `kits_repo: true` is set in `.inat/config.json`; you did not have to install it.

Read this file when the user asks you to "add a kit", "add a skill", "add a rule", edit `registry.json`, edit a `kit.json`, or touch anything under `kits/`. Pair it with `inat validate` (run it before every commit).

## On-disk layout

```
<repo-root>/              # or <repo-root>/<subpath>/ if registered with --path
├── registry.json         # index of all kits this source exposes
└── kits/
    ├── <kit-a>/
    │   ├── kit.json      # entries (skills + rules) this kit delivers
    │   ├── skills/
    │   │   └── <skill-name>/
    │   │       ├── SKILL.md
    │   │       ├── scripts/       # optional
    │   │       ├── references/    # optional
    │   │       └── assets/        # optional
    │   └── rules/
    │       └── <rule-name>.md
    └── <kit-b>/
        └── ...
```

## Naming rules (enforced)

All kit, skill, and rule names must match:

```
[a-z0-9]([a-z0-9-]*[a-z0-9])?
```

- Lowercase letters, digits, single hyphens only.
- Max 64 characters.
- No leading or trailing hyphen. No `--` (double hyphen).
- No uppercase, underscores, dots, spaces.
- The derived name of a skill is its **directory name**. The derived name of a rule is its **filename minus `.md`**.

Reserved names (built-in, cannot be used by a kit): `inat-usage`, `inat-kits`.

Delivered files use the bare skill/rule name — no source or kit prefix. Names must be globally unique across all installed kits in any consumer project, so two kits in this repo must not export the same derived name.

## `registry.json` — the source index

Minimal shape:

```json
{
  "version": "1",
  "kits": {
    "coding-standards": {
      "description": "Common coding rules and skills",
      "types": ["skill", "rule"]
    },
    "review": {
      "description": "Code review helpers",
      "types": ["skill"]
    }
  }
}
```

- `version` MUST be the string `"1"`. This is the schema version, not a semver.
- `kits` is a JSON object keyed by **kit name** (each key obeys the naming regex). MUST be non-empty.
- Each kit entry has a `description` string and a `types` array listing the file types the kit ships. `types` values MUST each be `"skill"` or `"rule"`.
- The kit's directory on disk is always `kits/<kit-name>/`, relative to the registry root. There is no explicit path mapping.

## `kit.json` — the per-kit manifest

Minimal shape for a kit with one skill and one rule:

```json
{
  "name": "review",
  "files": [
    {
      "type": "skill",
      "path": "skills/code-review"
    },
    {
      "type": "rule",
      "path": "rules/no-secrets.md",
      "priority": "contextual"
    }
  ]
}
```

- `name` MUST match the kit's directory name (`kits/<name>/kit.json`).
- `files` MUST be non-empty.
- Each entry has `type: "skill"` or `type: "rule"` and a `path` **relative to the kit directory**.
- For `type: "skill"`: `path` points to a **directory** that contains `SKILL.md`.
- For `type: "rule"`: `path` points to an `.md` **file**. `priority` is required and MUST be either `"always"` or `"contextual"`.
- `priority` MUST NOT be set on skill entries.

## Skill vs rule — what ships where

- **Skill** — a directory. Delivered into the agent's skills location (for Claude Code: `.claude/skills/<skill-name>/`). Optional subfolders `scripts/`, `references/`, `assets/` travel with it.
- **Rule, `priority: always`** — inlined into the managed section of the agent's rules file (for Claude Code: `CLAUDE.md`). Use for short, load-bearing instructions the agent must always see.
- **Rule, `priority: contextual`** — copied into `.inat/rules/<rule-name>.md` with a reference line in the managed section. Use for longer guidance that should only be pulled in when relevant.

Skills have no priority field. Rules require one.

## Rule frontmatter (recommended)

Each rule `.md` SHOULD begin with a YAML frontmatter block declaring `name` and `description`:

```markdown
---
name: <rule-name>
description: Read when …
---

# rule body…
```

- `name` MUST equal the filename-derived name (e.g. `rules/no-secrets.md` → `no-secrets`). Mismatch is a hard error at install / validate.
- `description` is a single-line "when to read" hint, ≤200 chars. The consuming agent uses it to decide whether to open the file, so make it a real reason — not a restatement of the title.
- Missing frontmatter, or frontmatter without `description`, is a warning (the bullet renders bare and existing kits keep working). Multi-line description, oversize description, missing `name`, name mismatch, and malformed YAML are hard errors.
- Always-priority rules also support frontmatter; the renderer strips the block before inlining the body, so the agent never sees the YAML.

See `docs/specs/0010-rule-descriptions.md` for the full contract.

## Before committing, run `inat validate`

```
inat validate            # validates the current directory
inat validate --subpath pkg     # if registry.json lives under ./pkg
inat validate --json     # machine-readable output (use this in CI)
```

Exit code is 0 iff zero issues. Every missing file, bad name, duplicate derived name, reserved-name collision, and malformed `kit.json` is reported with its path. Fix all issues before pushing — consumers of the source will hit the same errors on their first `inat install` otherwise.

## When editing this repo

- Never hand-edit `.inat/config.json`. Use `inat` commands.
- Never edit the managed section of `CLAUDE.md` (markers `<!-- inat:managed -->` / `<!-- inat:end -->`).
- When adding a kit: create `kits/<name>/` with `kit.json`, add its entry to `registry.json`, run `inat validate`.
- When renaming a skill or rule: the **derived name** changes (it comes from the directory/filename). Update any consumer-visible docs, and warn the user — consumers must uninstall the old name and install the new one; there is no auto-rename.
- When removing a kit: delete it from `registry.json` AND delete `kits/<name>/`. Leaving one without the other leaves an orphaned directory or a dangling registry entry.

See `docs/specs/0001-initial-idea.md` for the full contract.
