# inat Demo Walkthrough

A step-by-step walkthrough showing exactly what inat does to your project at each stage.

Built in safe Rust with zero unsafe code, inat stores no credentials and relies entirely on your machine's git credential chain — nothing to rotate, nothing to leak.

---

## The Problem

Your team uses AI coding agents (Claude Code, etc.) across multiple repositories. You have shared coding rules, review checklists, and style guides that every agent should follow — but today you're copy-pasting markdown files between repos, forgetting to update them, and watching them drift out of sync.

**inat** is a kit manager for AI prompts and rules. It installs kits from git repositories and delivers them to the right places for your AI agent.

---

## Concepts in 30 Seconds

| Concept | What it is |
|---------|-----------|
| **Skill** | A directory with a `SKILL.md` prompt — a task the AI agent can perform (e.g., code review) |
| **Rule** | A single markdown file with a persistent instruction (e.g., "never commit secrets") |
| **Kit** | A named group of skills and rules |
| **Source** | A git repository containing kits, indexed by `registry.json` |
| **Agent Profile** | A built-in mapping that knows where to put files for a specific AI agent |

Rules have two priority levels:
- **always** — inlined directly into the agent's config file (e.g., CLAUDE.md)
- **contextual** — copied to disk and referenced, so the agent reads them when relevant

---

## The Source Repository

Before we start, here's what your team maintains — a git repo with kits. **This repository is that example source.** Browse the `kits/` directory to see the real files.

```
inat-demo/
  registry.json
  kits/
    coding-standards/
      kit.json
      skills/
        code-review/
          SKILL.md
      rules/
        no-secrets.md
        style-guide.md
    sbp-brand/
      kit.json
      skills/
        brand-check/
          SKILL.md
      rules/
        brand-voice.md
        naming-conventions.md
    sbp-general-security/
      kit.json
      skills/
        security-scan/
          SKILL.md
      rules/
        no-secrets.md
        dependency-policy.md
        owasp-checklist.md
```

**[registry.json](registry.json)** — indexes available kits:

```json
{
  "version": "1",
  "kits": {
    "coding-standards": {
      "description": "Team coding standards, review skills, and style rules",
      "types": ["skill", "rule"]
    },
    "sbp-brand": {
      "description": "SBP brand voice, tone, and naming conventions for AI-assisted content",
      "types": ["skill", "rule"]
    },
    "sbp-general-security": {
      "description": "SBP baseline security rules and vulnerability scanning skills",
      "types": ["skill", "rule"]
    }
  }
}
```

### Kit: coding-standards

**[kit.json](kits/coding-standards/kit.json)** — declares the kit contents:

```json
{
  "name": "coding-standards",
  "description": "Team coding standards, review skills, and style rules",
  "files": [
    { "path": "skills/code-review", "type": "skill" },
    { "path": "rules/no-secrets.md", "type": "rule", "priority": "always" },
    { "path": "rules/style-guide.md", "type": "rule", "priority": "contextual" }
  ]
}
```

| File | Type | Delivery |
|------|------|----------|
| [skills/code-review/SKILL.md](kits/coding-standards/skills/code-review/SKILL.md) | skill | Copied to `.claude/skills/code-review/` |
| [rules/no-secrets.md](kits/coding-standards/rules/no-secrets.md) | rule (always) | Inlined into CLAUDE.md |
| [rules/style-guide.md](kits/coding-standards/rules/style-guide.md) | rule (contextual) | Copied to `.inat/rules/`, referenced in CLAUDE.md |

### Kit: sbp-brand

**[kit.json](kits/sbp-brand/kit.json)**:

```json
{
  "name": "sbp-brand",
  "description": "SBP brand voice, tone, and naming conventions for AI-assisted content",
  "files": [
    { "path": "skills/brand-check", "type": "skill" },
    { "path": "rules/brand-voice.md", "type": "rule", "priority": "always" },
    { "path": "rules/naming-conventions.md", "type": "rule", "priority": "contextual" }
  ]
}
```

| File | Type | Delivery |
|------|------|----------|
| [skills/brand-check/SKILL.md](kits/sbp-brand/skills/brand-check/SKILL.md) | skill | Copied to `.claude/skills/brand-check/` |
| [rules/brand-voice.md](kits/sbp-brand/rules/brand-voice.md) | rule (always) | Inlined into CLAUDE.md |
| [rules/naming-conventions.md](kits/sbp-brand/rules/naming-conventions.md) | rule (contextual) | Copied to `.inat/rules/`, referenced in CLAUDE.md |

### Kit: sbp-general-security

**[kit.json](kits/sbp-general-security/kit.json)**:

```json
{
  "name": "sbp-general-security",
  "description": "SBP baseline security rules and vulnerability scanning skills",
  "files": [
    { "path": "skills/security-scan", "type": "skill" },
    { "path": "rules/no-secrets.md", "type": "rule", "priority": "always" },
    { "path": "rules/dependency-policy.md", "type": "rule", "priority": "always" },
    { "path": "rules/owasp-checklist.md", "type": "rule", "priority": "contextual" }
  ]
}
```

| File | Type | Delivery |
|------|------|----------|
| [skills/security-scan/SKILL.md](kits/sbp-general-security/skills/security-scan/SKILL.md) | skill | Copied to `.claude/skills/security-scan/` |
| [rules/no-secrets.md](kits/sbp-general-security/rules/no-secrets.md) | rule (always) | Inlined into CLAUDE.md |
| [rules/dependency-policy.md](kits/sbp-general-security/rules/dependency-policy.md) | rule (always) | Inlined into CLAUDE.md |
| [rules/owasp-checklist.md](kits/sbp-general-security/rules/owasp-checklist.md) | rule (contextual) | Copied to `.inat/rules/`, referenced in CLAUDE.md |

---

## Step 1: Initialize inat

```bash
inat init --agent claude-code
```

This creates the `.inat/` directory with an empty config, and registers the `claude-code` agent profile.

**Project after this step:**

```
my-project/
  .inat/
+   config.json        <-- new
  src/
    ...
  CLAUDE.md
```

**.inat/config.json:**

```json
{
  "sources": [],
  "installed": {},
  "agents": ["claude-code"]
}
```

Nothing else changed. CLAUDE.md is untouched.

---

## Step 2: Add a Source

```bash
inat sources add https://git.example.com/team/inat-demo.git --name sbp
```

This clones the repository to a global cache (`~/.inat/cache/`), validates that it has a valid `registry.json`, and registers it in your project config.

**Project after this step:**

```
my-project/
  .inat/
~   config.json        <-- updated
```

**.inat/config.json:**

```json
{
  "sources": [
    {
      "name": "sbp",
      "url": "https://git.example.com/team/inat-demo.git"
    }
  ],
  "installed": {},
  "agents": ["claude-code"]
}
```

The global cache now has a clone:

```
~/.inat/cache/sbp/    <-- bare git clone (not committed, not in your project)
```

---

## Step 3: Install a Kit

```bash
inat install sbp/coding-standards
```

This is where the magic happens. inat reads the kit, derives skill/rule names, checks for naming conflicts, and delivers everything to the right places based on the `claude-code` agent profile.

**Project after this step:**

```
my-project/
  .inat/
~   config.json                         <-- updated with installed kit
    rules/
+     style-guide.md                    <-- contextual rule (copied)
  .claude/
    skills/
+     code-review/
+       SKILL.md                        <-- skill (copied)
~ CLAUDE.md                             <-- managed section added
```

**.inat/config.json:**

```json
{
  "sources": [
    {
      "name": "sbp",
      "url": "https://git.example.com/team/inat-demo.git"
    }
  ],
  "installed": {
    "sbp/coding-standards": {
      "tree": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
      "names": ["code-review", "no-secrets", "style-guide"],
      "delivered": [
        {
          "path": ".claude/skills/code-review/SKILL.md",
          "hash": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
        },
        {
          "path": ".inat/rules/style-guide.md",
          "hash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        }
      ]
    }
  },
  "agents": ["claude-code"]
}
```

Key fields in the installed entry:
- **tree** — the git tree SHA at install time (used to detect upstream changes)
- **names** — all skill/rule names from this kit (used for uniqueness checks)
- **delivered** — every file written to disk with its SHA-256 hash (used to detect local modifications)

**CLAUDE.md** — the managed section is appended:

```markdown
# My Project

(your existing CLAUDE.md content is preserved above)

<!-- inat:managed - DO NOT EDIT THIS SECTION -->

# --- sbp/coding-standards/no-secrets.md ---
Never commit secrets, API keys, tokens, or .env files to the repository.
Scan for hardcoded credentials in every code review.

Read these rules when relevant:
- .inat/rules/style-guide.md

<!-- inat:end -->
```

What happened here:
- The **always** rule (`no-secrets.md`) was inlined directly into CLAUDE.md inside the managed section
- The **contextual** rule (`style-guide.md`) was copied to `.inat/rules/` and a reference was added to the managed section
- The **skill** (`code-review`) was copied to `.claude/skills/code-review/`
- Your existing CLAUDE.md content above the managed section is completely untouched

---

## Step 3b: Install More Kits

```bash
inat install sbp/sbp-general-security
```

**CLAUDE.md** — the managed section grows with content from both kits:

```markdown
# My Project

(your existing CLAUDE.md content is preserved above)

<!-- inat:managed - DO NOT EDIT THIS SECTION -->

# --- sbp/coding-standards/no-secrets.md ---
Never commit secrets, API keys, tokens, or .env files to the repository.
Scan for hardcoded credentials in every code review.

# --- sbp/sbp-general-security/no-secrets.md ---
Never commit secrets, API keys, tokens, passwords, or .env files to the repository.
Connection strings must use environment variables or a secrets manager — never inline.
Flag any hardcoded credential immediately, even in tests — use fixtures or vaults instead.

# --- sbp/sbp-general-security/dependency-policy.md ---
Do not add new dependencies without justification. Every dependency is an attack surface.
Pin exact versions in lock files. Review changelogs before upgrading.
No dependencies with known critical CVEs — check before installing.

Read these rules when relevant:
- .inat/rules/style-guide.md
- .inat/rules/owasp-checklist.md

<!-- inat:end -->
```

**.inat/config.json** — now tracks both kits:

```json
{
  "sources": [
    {
      "name": "sbp",
      "url": "https://git.example.com/team/inat-demo.git"
    }
  ],
  "installed": {
    "sbp/coding-standards": {
      "tree": "a1b2c3d4...",
      "names": ["code-review", "no-secrets", "style-guide"],
      "delivered": [...]
    },
    "sbp/sbp-general-security": {
      "tree": "c3d4e5f6...",
      "names": ["security-scan", "no-secrets", "dependency-policy", "owasp-checklist"],
      "delivered": [
        { "path": ".claude/skills/security-scan/SKILL.md", "hash": "..." },
        { "path": ".inat/rules/owasp-checklist.md", "hash": "..." }
      ]
    }
  },
  "agents": ["claude-code"]
}
```

**Project on disk with both kits installed:**

```
my-project/
  .inat/
    config.json
    rules/
      style-guide.md                    <-- from coding-standards
      owasp-checklist.md                <-- from sbp-general-security
  .claude/
    skills/
      code-review/
        SKILL.md                        <-- from coding-standards
      security-scan/
        SKILL.md                        <-- from sbp-general-security
  CLAUDE.md                             <-- managed section with rules from both kits
```

---

## Step 4: Check Status

```bash
inat status
```

Output:

```
Agents: claude-code

sbp/coding-standards         up to date
sbp/sbp-general-security     up to date
```

If the source repository had newer commits:

```
Agents: claude-code

sbp/coding-standards         up to date
sbp/sbp-general-security     update available
```

---

## Step 5: Update a Kit

```bash
inat update sbp/coding-standards
```

inat fetches the latest from the source, compares the git tree SHA against the recorded one. If the tree changed, it re-delivers all files.

**.inat/config.json** — the `tree` field updates:

```json
"sbp/coding-standards": {
  "tree": "f6e5d4c3b2a1f6e5d4c3b2a1f6e5d4c3b2a1f6e5",
  "names": ["code-review", "no-secrets", "style-guide"],
  "delivered": [...]
}
```

If you've manually edited a delivered file, inat detects the hash mismatch and warns you:

```
warning: .claude/skills/code-review/SKILL.md has been locally modified, skipping
```

Use `--force` to overwrite local modifications:

```bash
inat update --force
```

---

## Step 6: Remove a Kit

```bash
inat remove sbp/coding-standards
```

inat removes all delivered files and cleans up the managed section in CLAUDE.md.

**Project after this step:**

```
my-project/
  .inat/
~   config.json        <-- installed entry removed
  CLAUDE.md            <-- managed section markers removed
```

**.inat/config.json:**

```json
{
  "sources": [
    {
      "name": "sbp",
      "url": "https://git.example.com/team/inat-demo.git"
    }
  ],
  "installed": {},
  "agents": ["claude-code"]
}
```

**CLAUDE.md** — managed section is gone, user content preserved:

```markdown
# My Project

(your existing CLAUDE.md content — exactly as it was before inat)
```

The managed section markers (`<!-- inat:managed -->` ... `<!-- inat:end -->`) are removed entirely when the section is empty. Your content above and below is untouched.

---

## Other Useful Commands

### Refresh (re-deliver without fetching)

```bash
inat refresh
```

Re-delivers all installed kits from the local cache without fetching from remote. Useful after switching branches or if files were accidentally deleted.

### Manage Agents

```bash
inat agents list            # show active and available profiles
inat agents add claude-code # add agent and auto-deliver all installed kits
inat agents remove claude-code  # remove agent and clean up all its delivered files
```

Adding an agent auto-delivers all installed kits to the new agent's targets. Removing an agent cleans up all files it owns.

### Manage Sources

```bash
inat sources list           # show registered sources
inat sources remove sbp     # remove source (fails if kits from it are installed)
```

---

## Key Behaviors

| Behavior | How it works |
|----------|-------------|
| **Atomic operations** | Config is written last — if delivery fails halfway, config isn't updated |
| **Hash-based modification detection** | Every delivered file has a SHA-256 hash recorded. On update/remove, inat checks if you've modified the file locally |
| **`--force` flag** | Overwrite locally modified files on update/remove |
| **Managed section safety** | User content outside `<!-- inat:managed -->` markers is never touched |
| **Name uniqueness** | Skill/rule names must be unique across all installed kits — install fails with a clear error on conflict |
| **Git-native** | Sources are git repos, versions are tree SHAs, cache is bare clones. No lock files, no custom registries |
| **Commit everything** | `.inat/` directory and delivered files are meant to be committed to version control |
| **Safe Rust** | Zero unsafe code, no stored credentials — uses your machine's git credential chain |
