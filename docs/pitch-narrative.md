# inat: A Story About Taming AI Agents at Scale

---

## The Situation

You're a tech lead at Acme Corp. Your team of eight engineers uses Claude Code across twelve repositories — backend services, frontend apps, internal tools. Claude is great, but there's a growing problem.

Last week, Sarah pushed an `.env` file with a staging API key. Claude didn't catch it because her repo's `CLAUDE.md` doesn't have the "no secrets" rule — someone forgot to copy it over when the repo was created.

Meanwhile, Jake has been getting inconsistent code reviews. The review skill in his repo is three months out of date — the team improved it in the main backend repo, but nobody propagated the changes.

And the new developer, Alex? She initialized her project from the template, but the template's rules were last updated in January. She's been working with stale instructions for weeks.

**The root cause:** your team's AI rules and skills are copy-pasted between repositories, tracked by nothing, updated by nobody.

---

## Before inat

Here's what the team does today:

1. Someone writes a good rule or skill in one repo
2. They post it in Slack: "hey, new code review skill, copy this to your repos"
3. A few people do it. A few don't. A few modify it slightly.
4. Three months later, nobody remembers which version is authoritative
5. A new repo gets created — someone copies rules from whichever repo they were last working in

The result: twelve repos, twelve different versions of the same rules, no way to update them, no way to know they've drifted.

---

## Enter inat

inat is a kit manager for AI prompts and rules. Your team publishes kits to a shared git repository, and every project installs them with a single command. Updates flow from the source to all projects.

Here's how Acme Corp sets it up.

### Step 1: Create the Source Repository

The team creates a git repo for their shared AI kits — `github.com/acme/ai-rules`. It has a simple structure:

```
registry.json                          <-- index of available kits
kits/
  coding-standards/
    kit.json                           <-- declares what's in the kit
    skills/
      code-review/
        SKILL.md                       <-- the review skill Claude uses
    rules/
      no-secrets.md                    <-- "always" rule: inlined into CLAUDE.md
      style-guide.md                   <-- "contextual" rule: referenced when relevant
```

The `registry.json` lists what's available:

```json
{
  "version": "1",
  "kits": {
    "coding-standards": {
      "description": "Team coding standards, review skills, and style rules",
      "types": ["skill", "rule"]
    }
  }
}
```

The `kit.json` declares the contents and how each file should be treated:

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

The `priority` field matters:
- **always** — this rule is so important it gets inlined directly into CLAUDE.md, so Claude always sees it
- **contextual** — this rule is stored as a file and referenced, so Claude reads it when it's relevant

### Step 2: Set Up a Project

Sarah opens her backend repo and runs:

```bash
inat init --agent claude-code
inat sources add https://github.com/acme/ai-rules.git --name acme
```

That's it. The project now knows about the team's shared source. Her `.inat/config.json`:

```json
{
  "sources": [
    { "name": "acme", "url": "https://github.com/acme/ai-rules.git" }
  ],
  "installed": {},
  "agents": ["claude-code"]
}
```

### Step 3: Install the Kit

```bash
inat install acme/coding-standards
```

inat fetches the kit, reads the manifest, and delivers every file to the right place based on the `claude-code` agent profile:

- **The skill** (`code-review`) is copied to `.claude/skills/code-review/SKILL.md` — exactly where Claude Code looks for skills
- **The always rule** (`no-secrets.md`) is inlined into `CLAUDE.md` inside a managed section — Claude sees it on every interaction
- **The contextual rule** (`style-guide.md`) is copied to `.inat/rules/style-guide.md` and a reference is added to `CLAUDE.md` — Claude reads it when style questions come up

Here's what her `CLAUDE.md` looks like now:

```markdown
# Backend Service

Our API service built with Rust and Actix.

## Development

Run `cargo test` for tests, `cargo run` for the dev server.

<!-- inat:managed - DO NOT EDIT THIS SECTION -->

# --- acme/coding-standards/no-secrets.md ---
Never commit secrets, API keys, tokens, or .env files to the repository.
Scan for hardcoded credentials in every code review.

Read these rules when relevant:
- .inat/rules/style-guide.md

<!-- inat:end -->
```

Everything above the `<!-- inat:managed -->` marker is Sarah's own content — inat never touches it. The managed section is inat's territory, clearly marked, containing the inlined rules and references.

The config now tracks exactly what's installed:

```json
{
  "sources": [
    { "name": "acme", "url": "https://github.com/acme/ai-rules.git" }
  ],
  "installed": {
    "acme/coding-standards": {
      "tree": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
      "names": ["code-review", "no-secrets", "style-guide"],
      "delivered": [
        { "path": ".claude/skills/code-review/SKILL.md", "hash": "b94d27b..." },
        { "path": ".inat/rules/style-guide.md", "hash": "e3b0c44..." }
      ]
    }
  },
  "agents": ["claude-code"]
}
```

The `tree` SHA is from git — it's how inat knows whether the upstream kit has changed. The `hash` on each delivered file is a SHA-256 of the content — it's how inat knows if you've edited a file locally.

Sarah commits everything: `.inat/config.json`, the delivered skill and rule files, and the updated `CLAUDE.md`. It's all version-tracked.

### Step 4: Roll It Out

Sarah posts in Slack: "Run these three commands in your repo." Jake and Alex do the same:

```bash
inat init --agent claude-code
inat sources add https://github.com/acme/ai-rules.git --name acme
inat install acme/coding-standards
```

Now all twelve repos have the same rules. Same review skill. Same "no secrets" mandate. Same style guide.

---

## A Month Later

The team improves the code review skill — they add a security checklist section. Someone pushes the update to `github.com/acme/ai-rules`.

Jake wants to check if his repo is current:

```bash
inat status
```

```
Agents: claude-code

acme/coding-standards    update available
```

He updates:

```bash
inat update
```

inat fetches the latest, sees the tree SHA has changed, removes the old deliveries, and re-delivers the updated files. His CLAUDE.md's managed section gets the latest rules. His skill directory gets the latest review prompt.

But wait — Jake had tweaked the review skill locally to add a database-specific check. inat notices:

```
warning: .claude/skills/code-review/SKILL.md has been locally modified, skipping
```

Jake has a choice: keep his local changes (the default) or overwrite with the upstream version (`inat update --force`). Either way, he knows exactly what's happening.

---

## When Someone Leaves a Project

Alex's project is decommissioned. She removes the kit:

```bash
inat remove acme/coding-standards
```

inat deletes all delivered files, removes the blocks from CLAUDE.md's managed section, and cleans the config. If the managed section is now empty, even the markers are removed — CLAUDE.md looks like inat was never there.

---

## Why This Matters

**Without inat**, AI rules are tribal knowledge. They live in Slack messages, wiki pages, and template repos. They drift, they go stale, and your agents give inconsistent guidance across projects.

**With inat:**

- **One source of truth.** Rules and skills live in a git repo your team already knows how to manage. PRs, reviews, blame — the full git workflow applies.

- **Install, don't copy.** One command installs a kit. One command updates it. No manual copy-paste, no "did you remember to update repo X?"

- **Version-tracked delivery.** Everything inat delivers is committed to your project. `git diff` shows exactly what changed. `git blame` shows when it was installed or updated.

- **Agent-aware delivery.** inat knows where Claude Code expects skills (`.claude/skills/`), where rules go (`CLAUDE.md`), and how to manage them (inlined vs. referenced). Future agent profiles can target different tools.

- **Safe by design.** Managed sections are clearly marked. Local modifications are detected. Operations are atomic — if something fails halfway, your config stays consistent.

- **No lock files, no custom registries, no runtime dependencies.** Just git repos, markdown files, and a CLI that puts them in the right place.

---

## Quick Reference

```bash
# Set up a project
inat init --agent claude-code
inat sources add <url> --name <alias>

# Install and manage kits
inat install <source>/<kit>
inat update [<source>/<kit>] [--force]
inat remove <source>/<kit> [--force]

# Day-to-day
inat status                    # check for updates
inat refresh [--force]         # re-deliver from cache (no fetch)

# Manage agents and sources
inat agents add <name>         # add agent + auto-deliver
inat agents remove <name>      # remove agent + clean up
inat sources list
inat sources remove <name>
```
