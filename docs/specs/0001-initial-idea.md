## inat — AI Prompt & Rule Kit Manager

> Terminology updated per spec [`0004-rename-packages-to-kits.md`](0004-rename-packages-to-kits.md) (2026-04-22): what earlier drafts of this spec called a "package" is now called a **kit**.

---

### 1. Overview

A CLI tool for managing reusable AI skill and rule kits across projects. Kits are stored in remote git repositories, installed locally per-project, and delivered to configured AI agent targets based on agent profiles.

The tool is called **inat**.

---

### 2. Concepts

**Skill** — A directory containing an AI prompt, instruction, or action for a specific task. Follows the Agent Skills directory format: a directory named after the skill containing a `SKILL.md` file with YAML frontmatter and optional supporting files (`scripts/`, `references/`, `assets/`).

**Rule** — A single file containing a persistent instruction or constraint an AI agent follows. Rules have a priority level that determines how they are delivered.

**Kit** — A named group of related skills and rules.

**Source** — A remote git repository containing kits, indexed by `registry.json`.

**Agent Profile** — A built-in mapping that defines where each file type is delivered for a specific AI agent, including the delivery strategy per type.

**Registry** — An index file at the root of a source listing available kits.

---

### 3. Naming Constraints

All kit, skill, rule, and source names must follow these rules:

- **Pattern:** `[a-z0-9]([a-z0-9-]*[a-z0-9])?` — lowercase alphanumeric and single hyphens only.
- **No consecutive hyphens** (`--`).
- **No leading or trailing hyphens.**
- **Maximum length:** 64 characters per component.

Delivered files use the **skill or rule name only** — no source or kit prefix:

```
<skill-name>/SKILL.md
<rule-name>.md
```

Kits are a delivery mechanism, not a namespace. Once delivered, only the skill/rule name matters to the agent. Skill and rule names must be **unique across all installed kits** within a project. If a kit being installed contains a skill or rule name that is already delivered by another kit, the install fails with a clear error.

---

### 4. Source Repository Structure

```
/registry.json
/kits/
  /<kit-name>/
    /kit.json
    /skills/
      /<skill-name>/
        /SKILL.md
        /scripts/       (optional)
        /references/    (optional)
        /assets/        (optional)
    /rules/
      /<rule-name>.md
```

#### 4.1 Registry (`registry.json`)

Lists all kits available in the source.

```json
{
  "version": "1",
  "kits": {
    "<kit-name>": {
      "description": "<text>",
      "types": ["skill", "rule"]
    }
  }
}
```

The `types` array summarizes which file types the kit contains.

#### 4.2 Kit Metadata (`kit.json`)

```json
{
  "name": "<kit-name>",
  "description": "<text>",
  "author": "<text>",
  "license": "<SPDX identifier>",
  "files": [
    {
      "path": "<relative path>",
      "type": "skill | rule",
      "priority": "always | contextual"
    }
  ]
}
```

- `type` is required for every entry. Valid values: `skill`, `rule`.
- For skills, `path` points to the skill directory (containing `SKILL.md`). For rules, `path` points to the rule file directly.
- `priority` applies only to `rule` type. Defaults to `contextual` if omitted.
  - **`always`** — Content is inlined into the agent's rule file (e.g., CLAUDE.md). Always in agent context.
  - **`contextual`** — File is delivered to a storage path and referenced from the rule file. Loaded by the agent when relevant.

#### 4.3 Skills

Skills follow the [Agent Skills](https://agentskills.io) directory format. Each skill is a directory containing:

- `SKILL.md` (required) — YAML frontmatter (`name`, `description`, and optional `license`, `compatibility`, `metadata`, `allowed-tools`) followed by Markdown instructions.
- `scripts/` (optional) — Executable code the agent can run.
- `references/` (optional) — Additional documentation loaded on demand.
- `assets/` (optional) — Templates, images, data files.

The directory name must match the `name` field in the `SKILL.md` frontmatter.

#### 4.4 Rules

Plaintext files. Markdown recommended. No enforced internal format — content is agent-agnostic by default.

---

### 5. Project Configuration

File: `.inat/config.json` in the project root.

```json
{
  "sources": [
    {
      "name": "<alias>",
      "url": "<git remote URL>"
    }
  ],
  "installed": {
    "<source>/<kit-name>": {
      "tree": "<git tree SHA of kit directory at install time>",
      "delivered": [
        {
          "path": "<relative path of delivered file (skill/rule name only)>",
          "hash": "<SHA-256 hash of the file at delivery time>"
        }
      ]
    }
  },
  "agents": ["<agent profile names>"]
}
```

- **`sources`** — Registered remote repositories.
- **`installed`** — Kits currently installed, keyed by `source/kit-name`. Includes the git tree SHA of the kit directory (for update detection) and delivered file paths with content hashes (for modification detection).
- **`agents`** — Agent profiles active for this project. Extends (union with) the global agent list.

The `.inat/` directory SHOULD be committed to version control.

---

### 6. Global Configuration

File: `~/.inat/config.json`

```json
{
  "agents": ["<agent profile names>"]
}
```

- **`agents`** — Agent profiles active globally across all projects.

Project-level agents extend the global list (union). A project can add agents without repeating the global ones.

#### 6.1 Global Cache

Location: `~/.inat/cache/`

Cloned source repositories are cached globally. Multiple projects sharing the same source use the same cached clone. On install or update, the tool fetches the latest from the remote. If the cache is missing, a fresh clone is performed.

The cache MUST NOT be committed to version control.

---

### 7. Agent Profiles

An agent profile defines how each file type is delivered for a specific AI agent. Profiles are built into the tool.

#### 7.1 Profile Schema

```json
{
  "name": "<agent name>",
  "mapping": {
    "skill": { "path": "<target directory>", "strategy": "copy" },
    "rule": {
      "always": { "path": "<rule file>", "strategy": "managed-section" },
      "contextual": { "path": "<storage directory>", "strategy": "copy", "reference": "<rule file>" }
    }
  }
}
```

#### 7.2 Built-in Profiles (v1)

**claude-code:**

```json
{
  "name": "claude-code",
  "mapping": {
    "skill": { "path": ".claude/skills/", "strategy": "copy" },
    "rule": {
      "always": { "path": "CLAUDE.md", "strategy": "managed-section" },
      "contextual": { "path": ".inat/rules/", "strategy": "copy", "reference": "CLAUDE.md" }
    }
  }
}
```

v1 ships with `claude-code` only. Additional profiles will be added based on demand.

#### 7.3 Built-in `inat-usage` contextual rule

Whenever an agent profile becomes active — on `inat init --agent <name>` or `inat agents add <name>` — inat automatically delivers a built-in contextual rule named `inat-usage` to that agent. The rule file is embedded in the binary, copied to the profile's contextual-rule path (e.g. `.inat/rules/inat-usage.md` for `claude-code`), and a reference to it is added to the managed section (e.g. in `CLAUDE.md`). `inat agents remove <name>` cleans it up symmetrically. The name `inat-usage` is reserved and cannot be used by a third-party kit. The delivery is non-interactive and requires no user action — it is not a carve-out from §14.

---

### 8. Delivery

#### 8.1 Strategies

| Strategy | Behavior |
|---|---|
| **copy** | File is copied to the target path using its original skill/rule name |
| **managed-section** | Content is inlined into a managed section within the target file |

#### 8.2 Naming

Delivered items use the skill or rule name directly, with no source or kit prefix. See section 3 for naming constraints and uniqueness rules.

**Skills** are delivered as directories:

```
<skill-name>/SKILL.md
```

Example: kit `coding-standards` with skill `review` delivers as `review/SKILL.md` (with any supporting files from the source skill directory).

**Rules** are delivered as files:

```
<rule-name>.md
```

Example: kit `coding-standards` with rule `no-secrets.md` delivers as `no-secrets.md`.

#### 8.3 Managed Section

For `always` priority rules, content is inlined into the agent's rule file within a clearly marked managed section:

```markdown
<!-- inat:managed - DO NOT EDIT THIS SECTION -->

# --- company/coding-standards/no-secrets.md ---
Never commit .env files or API keys.

# --- company/security/sql-injection.md ---
Always use parameterized queries.

Read these rules when relevant:
- .inat/rules/python-style.md

<!-- inat:end -->
```

Behavior:

- The tool manages everything between the `<!-- inat:managed -->` and `<!-- inat:end -->` markers.
- User content above and below the markers is never touched.
- On first install, markers are appended to the end of the file (or file is created if it does not exist).
- Each kit's content is delimited by `# --- <source>/<kit-name>/<filename> ---` (internal bookkeeping for the tool). On removal, the specific delimiter block is removed.
- Contextual rule references are listed within the managed section using the delivered file name (rule name only).
- If no managed content remains after removal, the markers are removed entirely.

---

### 9. CLI Commands

```
inat init [--agent <name>]
inat sources add <url> [--name <alias>]
inat sources remove <name>
inat sources list
inat agents add <name>
inat agents remove <name>
inat agents list
inat install <source>/<kit>
inat update [source/kit]
inat remove <source>/<kit>
inat refresh [--force]
inat status
```

Kits are referenced using `source/kit` syntax (e.g., `inat install company/coding-standards`). When only one source is configured, the bare kit name may be used (e.g., `inat install coding-standards`). For `update` and `remove`, bare names are allowed when unambiguous (only one installed instance of that kit name).

| Command | Description |
|---|---|
| `init` | Create `.inat/config.json` in current project with empty defaults. Use `--agent` to set an initial agent (e.g., `inat init --agent claude-code`). |
| `sources add` | Register a new source repository. Fetches and validates the URL immediately (must be a reachable git repo with a valid `registry.json`). |
| `sources remove` | Remove a source. Refuses if any kits from that source are still installed. |
| `sources list` | List configured sources |
| `agents add` | Add an agent profile to the project and auto-deliver all installed kits to the new agent's targets |
| `agents remove` | Remove an agent profile from the project and clean up all files delivered to that agent's targets |
| `agents list` | List active agents (global + project) |
| `install` | Install a kit and deliver files to all active agent targets |
| `update` | Update one or all installed kits. Fetches latest from sources and re-delivers. |
| `remove` | Remove a kit, clean up all delivered files, and update config |
| `refresh` | Re-deliver all installed kits from their recorded state in config. Restores delivered files to match config without fetching new versions. Use `--force` to overwrite locally modified files. |
| `status` | Fetch from all sources and show installed kits with update availability |

If commands are run in a project without `.inat/config.json`, the tool instructs the user to run `inat init` first.

---

### 10. Behavior Rules

1. Installing a kit fetches it from the source (via global cache) and delivers files to all active agent targets using each agent profile's mapping.
2. Delivered files and managed sections in agent rule files are committed to version control. This is the reproducibility mechanism — no lock file is needed.
3. Updating re-fetches from source and re-delivers to all active agent targets.
4. Removing deletes delivered files, cleans up managed section blocks by delimiter, and updates config.
5. Multiple sources may provide kits with the same name. The `source/kit` syntax on install disambiguates. The source is encoded in the `installed` config key.
6. **Skill/rule name uniqueness:** Skill and rule names must be unique across all installed kits within a project. If a kit being installed contains a skill or rule name already delivered by another kit, the install fails with a clear error (e.g., `Skill "review" already delivered by kit "coding-standards". Remove it first or rename the skill.`).
7. Authentication is delegated to the user's existing git configuration (SSH keys via ssh-agent and `~/.ssh/config`, HTTPS tokens via git credential managers). The tool does not manage credentials.
8. **Atomic rollback:** All install, update, remove, refresh, and agent add/remove operations are atomic. File operations are planned first, then executed. If any step fails, all changes are rolled back. Config is written last, only on full success.
9. **Modified file detection:** Delivered files are hashed (SHA-256) at delivery time. Before update, remove, or refresh, the current file hash is compared against the stored hash. If the file was locally modified, the operation warns and skips that file. Use `--force` to overwrite.
10. **No offline fallback:** All install, update, and status operations require network access. The global cache is not used as a fallback for stale data. If a source is unreachable, the operation fails with a clear error.
11. **Sources are project-scoped, or user-scoped under `--user`** (spec [0025](0025-user-scope.md)). There is no machine-wide source list shared across projects.
12. **Scripts are just files.** The tool copies `scripts/` directories as part of skill delivery. It does not set execution permissions or provide sandboxing. Execution semantics are the AI agent's responsibility.
13. **Agent add/remove auto-delivery:** Adding an agent profile immediately delivers all installed kits to the new agent's targets. Removing an agent profile cleans up all files delivered to that agent's targets. Both operations are atomic.
14. **Strict kit validation:** On install, the tool validates the kit structure strictly. `kit.json` must exist, be valid JSON, and contain all required fields (`name`, `files`). All file paths declared in `files` must exist. All names must pass naming constraints. If any check fails, the install fails.
15. **Source validation:** `sources add` fetches the URL immediately and verifies it is a reachable git repo containing a valid `registry.json`. Invalid sources are rejected.
16. **Source removal guard:** `sources remove` refuses if any installed kits originate from that source. The user must remove those kits first.
17. **Managed section integrity:** If the managed section markers (`<!-- inat:managed -->` / `<!-- inat:end -->`) in an agent rule file are missing, malformed, or corrupted, the tool reports the issue and stops. It does not attempt to auto-repair. The user must fix the markers manually before retrying.
18. **Init defaults:** `inat init` creates `.inat/config.json` with empty `sources`, `installed`, and `agents`. No defaults are assumed. Use `--agent` to set an initial agent.

---

### 11. Versioning

v1 uses git-native versioning instead of semantic versioning. Each installed kit records the git tree SHA of its kit directory at install time. The `update` command fetches the latest state from the source and compares per-kit tree SHAs — only kits whose directory contents actually changed are flagged as having updates.

Delivered files are committed to version control, providing reproducibility. Users review changes via `git diff` before committing.

Semantic versioning may be layered on in a future version when kit authors need to signal breaking changes.

---

### 12. Security

- No secrets in project files.
- Credential storage delegated to git's built-in credential system.
- Kit integrity verification is RECOMMENDED (checksums or signed commits).
- Sources should be accessed over encrypted transport (SSH or HTTPS).

---

### 13. Error Handling

- Never fail silently. All errors are reported to the user with a clear message.
- Network operations (fetching sources) may retry up to 2 times before failing.
- If a source is unreachable, report the error and continue with other sources where possible.

---

### 14. Out of Scope (v1)

- Dependency resolution between kits
- Runtime execution of skills
- Kit discovery and browsing (sharing is user-driven)
- Custom agent profile definitions (only built-in profiles)
- Interactive prompts (all options via flags)
- Semantic versioning (git-native tree SHAs used instead)
- Lock file (committed delivered files provide reproducibility)
- Script execution permissions or sandboxing
- JSON output format for CLI (`--json` flag)

---
