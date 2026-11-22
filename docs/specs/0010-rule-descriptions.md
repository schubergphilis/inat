## inat — Per-rule "when to read" descriptions in the managed section (Spec 0010)

---

### 1. Overview

Today, contextual rules render in agent rule files (e.g. `CLAUDE.md`) as bare path bullets:

```
Read these rules when relevant:
- .inat/rules/use-1password-cli.md
```

Unless the filename is self-explanatory, the agent has no signal for *when* opening the file is worth its time, so it usually doesn't. This spec adds a per-rule "when to read" hint, sourced from each rule's own YAML frontmatter, that renders alongside the path:

```
Read these rules when relevant:
- .inat/rules/inat-usage.md — Read when the user asks to install/update/remove a kit, …
- .inat/rules/skill-frontmatter.md — Read when creating or modifying any `SKILL.md` …
```

The frontmatter shape mirrors Agent Skills' `SKILL.md` convention (`name` + `description`), so kit authors only have to learn one schema.

A companion implementation plan lives at `docs/plan/0010-rule-descriptions.md`.

This spec extends spec [`0001-initial-idea.md`](0001-initial-idea.md). It does not alter the Rust tree under `rust/` (frozen archive); all changes are in `go/`.

---

### 2. Goals

1. Every contextual-rule reference in an inat-managed section MUST be able to carry a one-sentence hint that the agent uses to decide whether the file is relevant.
2. The hint MUST be authored alongside the rule body, in the rule `.md` file itself — no second source of truth.
3. The frontmatter shape MUST be compatible with Agent Skills' `SKILL.md` convention (a YAML block fenced by `---` containing `name` + `description`), so authors can write rules and skills with the same mental model.
4. Existing kits MUST keep working unchanged: a rule without frontmatter MUST install successfully and render as a bare bullet (current behavior), accompanied by a stderr warning that nudges the author to add frontmatter.
5. Validation MUST distinguish soft issues (missing description ⇒ warning, advisory) from hard violations (name mismatch, oversize, multi-line, malformed YAML ⇒ error, blocks).
6. `inat refresh` and `inat update` MUST rewrite the managed section to pick up new or changed descriptions from the rule files in the cache, so authors can iterate on hints without reinstalling.
7. Always-priority rules MUST also support frontmatter; the frontmatter block MUST be stripped from the rule body when the body is inlined into a managed-section content block, so the agent never sees the YAML twice.
8. The implementation MUST add no new Go module dependencies (CLAUDE.md rule 8): cobra + go-git + stdlib remain the entire allowlist. The frontmatter parser is hand-rolled.

---

### 3. Non-goals

- Full YAML support. The parser handles only flat `key: value` pairs between `---` fences. Anchors, nested maps, lists, folded scalars are out of scope.
- Per-skill `description` rendering changes. Skills already ship `SKILL.md` frontmatter; the agent already reads the description from there. No managed-section change applies to skills.
- Localisation / multi-language hints.
- Hint inheritance from `kit.json` or registry-level metadata.
- Backwards-compatibility shims for older inat releases reading newer kits — the new format is purely additive (existing readers see a bare bullet because they ignore extra text after the path).

---

### 4. Frontmatter shape

A rule `.md` MAY begin with a YAML frontmatter block. When present, the block:

- Starts on line 1 (BOM-tolerant) with a line containing only `---` (trailing whitespace allowed).
- Ends with a matching `---` line.
- Contains zero or more `key: value` lines between the fences.
- Allows blank lines and `# …` comment lines anywhere inside.
- Recognised keys: `name`, `description`. Other keys are tolerated and ignored.
- Quoted values: surrounding single or double quotes are stripped. Quote escaping is not supported (descriptions don't need it).

When the block is present, both `name` and `description` SHOULD be set:

- `name` is REQUIRED and MUST equal the filename-derived name (e.g. `rules/foo.md` → `foo`). Mismatch is a hard error at validate / install.
- `description` is REQUIRED for the bullet to render with a hint; missing it is a warning, not an error. The renderer falls back to a bare bullet.
- `description` MUST be single-line (no `\n` / `\r`) and MUST NOT exceed 200 characters. Both are hard errors.

Example:

```markdown
---
name: inat-usage
description: Read when the user asks to install/update/remove a kit, add/remove an agent, or anything that touches files under `.inat/`.
---

# Using `inat` in this project

…
```

---

### 5. Render format

Each contextual rule reference renders as one bullet line under the `Read these rules when relevant:` header:

- With description: `- <path> — <description>` (using U+2014 em-dash with surrounding ASCII spaces).
- Without description: `- <path>` (current behavior).

The em-dash separator is parsed back to `(path, description)` by splitting on the first occurrence of ` — `; embedded em-dashes inside the description body are preserved.

For always-priority rules, the renderer strips the frontmatter block and inlines only the body into the managed-section content block. The bullet-list rendering does not apply.

---

### 6. Severity model

| Condition | Severity | Surface |
|---|---|---|
| No frontmatter at all | Warning | stderr (install/refresh/update) + `inat validate` warning |
| Frontmatter present, no `description` | Warning | stderr + validate warning |
| Frontmatter present, no `name` | Error | install/refresh/update fail; validate error |
| `name` present but mismatches derived name | Error | same |
| `description` > 200 chars | Error | same |
| `description` contains newline | Error | same |
| Malformed YAML (no closing fence, malformed line, duplicate `name`/`description`) | Error | same |

Errors block the operation. Warnings never affect exit codes; they're advisory.

`inat validate` `--json` output gains a top-level `warnings` array alongside `issues`. Existing consumers that read `issues` are unaffected.

---

### 7. Migration

- `inat update` fetches new commits from the source, advances the recorded tree SHA, and re-renders the managed section using the new frontmatter. **This is the path authors and consumers use to pick up new or changed hints** (commit the rule, run `update`).
- `inat refresh` re-renders the managed section against the **already-recorded** tree SHA — it does not fetch and does not pick up working-tree edits in the cache directory. Use it when an inat upgrade changes how the managed section is rendered (this release is one such case: any project whose recorded tree already carries frontmatter on its rules will gain hints on the next refresh, no `update` required).
- Other commands (`install`, `remove`, `status`) are pass-through with respect to the managed section.
- Built-in rules `inat-usage` and `inat-kits` ship with frontmatter from this release onward.
- An existing project's first run of `inat refresh` (or `update`) after upgrade rewrites bullets to include hints where the recorded tree has frontmatter, and emits warnings where it doesn't. The diff is intentional and documented in `CHANGELOG`.

---

### 8. Acceptance criteria

1. A rule with valid frontmatter installs cleanly and its bullet renders with the em-dash hint.
2. A rule without frontmatter installs successfully; bullet renders bare; stderr emits a warning naming the rule.
3. A rule with a name mismatch fails install with a clear error message naming the rule and both names.
4. A rule with a 250-char description fails validate / install with the actual length and the cap.
5. An always-priority rule with frontmatter has its frontmatter stripped from the inlined block (no `---` fences in the rendered managed section).
6. After authoring frontmatter on a previously-bare rule and committing it to the source, the consumer's `inat update` rewrites the managed section to include the new hint. (`inat refresh` re-renders against the recorded tree SHA only, so it picks up new hints only when the recorded tree already carries the frontmatter — typical of the first refresh after an inat binary upgrade.)
7. `inat refresh --force` does not require modified-file overrides for built-in rules whose embedded content gained frontmatter — the on-disk hash matches the recorded hash, so the modified-file guard does not trip.
8. `inat validate --json` output contains both `issues` (errors) and `warnings` (advisory) arrays. Exit code is 0 when `issues` is empty regardless of warning count.

---

### 9. Reserved-name interaction

The reserved names `inat-usage` and `inat-kits` (per spec 0001 §6) continue to apply. The built-in rules satisfy their own frontmatter contract. Kits MUST NOT ship rules deriving to either name (existing rule, unchanged).
