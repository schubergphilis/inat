# inat

A CLI kit manager for AI prompts and rules. Share skills and rules across projects via git — install them into any project with one command, deliver them to the right place for each AI agent, and update them from source without copy-paste drift.

> Status: releases for Linux, Windows and macOS (Intel + Apple Silicon) are published on [GitHub Releases](https://github.com/schubergphilis/inat/releases), with one-liner installers and `inat self update`. The macOS binaries are ad-hoc-signed via `rcodesign` (spec 0014) so they launch on Apple Silicon; first run still requires clearing Gatekeeper's quarantine attribute — see [macOS](#macos) below.

---

## Why

Teams using AI coding agents end up copy-pasting the same rules and skills — "no secrets", code review checklists, style guides — between every repository. They drift. Nobody remembers which version is authoritative. New repos start from stale templates.

`inat` treats those files as kits: authored in a shared git repo, installed per project, updated on demand. Delivered files are committed to the consuming repo, so reproducibility comes from git itself (no lock file, no semver in v1 — kit tree SHAs track changes).

The full narrative: [`docs/pitch-narrative.md`](docs/pitch-narrative.md). A step-by-step tour: [`docs/demo-walkthrough.md`](docs/demo-walkthrough.md).

---

## Install

Releases are published on [GitHub Releases](https://github.com/schubergphilis/inat/releases). The installers download anonymously, with no token.

> **Moved from GitLab.** inat used to be distributed from an internal GitLab instance. Installs from before v0.9.0 cannot update themselves to GitHub: re-install once with the one-liner below, and `inat self update` works from then on.

### Linux (POSIX)

```bash
curl -fsSL https://github.com/schubergphilis/inat/releases/latest/download/install.sh | sh
```

Installs `inat` to `~/.local/bin` (override with `INAT_INSTALL_DIR` or `--install-dir <path>`) and appends a managed section to your shell profile (bash, zsh, or fish) so the binary is on `$PATH` in a new shell. Pass `--no-modify-path` to skip the profile edit. Flags go through `sh -s --`:

```bash
curl -fsSL https://github.com/schubergphilis/inat/releases/latest/download/install.sh | sh -s -- --version v0.9.0
```

### Windows (PowerShell 5.1+)

```powershell
irm https://github.com/schubergphilis/inat/releases/latest/download/install.ps1 | iex
```

Installs `inat.exe` to `%LOCALAPPDATA%\Programs\inat\bin` and appends it to your user `PATH`. `| iex` cannot pass flags; to use `-Version`, `-PreRelease`, `-NoModifyPath` or `-InstallDir`, invoke the script as a scriptblock:

```powershell
$url = 'https://github.com/schubergphilis/inat/releases/latest/download/install.ps1'
& ([scriptblock]::Create((irm $url))) -Version v0.9.0
```

### macOS

The same `install.sh` one-liner shown under [Linux](#linux-posix) works on macOS (Intel + Apple Silicon) — `install.sh`'s `detect_target` resolves `Darwin-x86_64` and `Darwin-arm64` to the matching `*-apple-darwin.tar.gz` artifact.

The macOS binaries are ad-hoc-signed in CI so Apple Silicon's kernel will load them, but they are **not Apple-notarized**. On first launch you'll see a Gatekeeper dialog along the lines of *"inat" cannot be opened because Apple cannot check it for malicious software*. Clear it once with:

```bash
xattr -d com.apple.quarantine "$(command -v inat)"
```

The `com.apple.quarantine` attribute is added by your browser / `curl` on download, not by the build, so notarization is the only way to avoid this dialog entirely. After clearing the attribute once, subsequent runs are silent. Right-click → Open in Finder is the GUI equivalent.

### Common flags

Both installers accept:

| Flag | Purpose |
|---|---|
| `--version v0.9.0` / `-Version v0.9.0` | Install a specific tag instead of the latest. |
| `--pre-release` / `-PreRelease` | Include pre-releases (`vX.Y.Z-rc.N`) when resolving "latest". |
| `--no-modify-path` / `-NoModifyPath` | Do not touch shell profile or user PATH. |
| `--install-dir <path>` / `-InstallDir <path>` | Override the install directory. |

### From source (any platform)

If you'd rather build the binary yourself — air-gapped host, or an unsupported arch such as Linux arm64:

```bash
git clone https://github.com/schubergphilis/inat.git
cd inat
( cd go && go build -ldflags "-X main.version=$(cat ../VERSION)" -o "$HOME/.local/bin/inat" . )
```

Requires Go 1.22 or newer (Go 1.26 or newer to run the pinned `staticcheck`; CI uses 1.27). The built binary is statically linked (no glibc / SSL / libgit2 dependencies); copy it anywhere on your `PATH`.

### Upgrading

`inat self update` upgrades the running binary in place from GitHub Releases, anonymously — no need to re-run the install one-liner. On macOS, self-update sidesteps Gatekeeper's quarantine prompt entirely (Go's `net/http` does not set `com.apple.quarantine`), so the upgrade is silent on every platform.

```bash
inat self update --check                  # Print current vs. latest, no changes.
inat self update                          # Resolve latest stable, download, verify, swap.
inat self update --version v0.9.0         # Pin a specific tag (also works for downgrade).
inat self update --force                  # Re-download even if current = latest.
```

The command verifies the archive's SHA-256 against the published `SHA256SUMS` before touching the installed binary; on mismatch it aborts and leaves the existing binary untouched. On Windows, the previous binary is renamed aside as `inat.exe.old` and removed on the next `inat` invocation (Windows holds an exclusive lock on the running `.exe`); on Linux/macOS the swap is immediate. Spec / plan [`0015-self-update`](docs/specs/0015-self-update.md), amended by [`0027-github-migration`](docs/specs/0027-github-migration.md).

GitHub allows 60 anonymous API requests per hour per IP address. `self update` makes one or two, so this only bites behind a busy shared NAT; wait an hour and retry.

### Uninstall

```bash
curl -fsSL https://github.com/schubergphilis/inat/releases/latest/download/uninstall.sh | sh
```

```powershell
irm https://github.com/schubergphilis/inat/releases/latest/download/uninstall.ps1 | iex
```

The uninstaller removes the binary and the installer's managed PATH section. It does not delete `~/.inat/` or any project-level `.inat/` directories — remove those yourself if you want a fully clean slate.

### Troubleshooting

- **`403` or `rate limit exceeded`** → GitHub's anonymous API limit (60 requests/hour per IP). Wait and retry, or pass `--version vX.Y.Z` / `-Version vX.Y.Z`, which skips the API and downloads directly.
- **`no SHA256 for ... in SHA256SUMS`** → the asked-for `(version, target)` pair isn't published. Check the [Releases page](https://github.com/schubergphilis/inat/releases) and pass `--version vX.Y.Z` explicitly if the default-latest doesn't have your platform.
- **macOS *"cannot be opened because Apple cannot check it for malicious software"*** → expected. The binary is ad-hoc-signed but not Apple-notarized. Run `xattr -d com.apple.quarantine "$(command -v inat)"` once after install, or right-click → Open in Finder. See [macOS](#macos) above.
- **macOS *"killed: 9"* on Apple Silicon** → indicates the binary lacks even an ad-hoc signature, which would mean a release-pipeline regression. [Open an issue](https://github.com/schubergphilis/inat/issues) with the binary's `inat --version` output; in the meantime, [build from source](#from-source-any-platform).

---

## Quick start

```bash
# 1. In your project, create .inat/ and register an agent target
inat init --agent claude-code

# 2. Register a source — a git repo that hosts kits
inat sources add https://github.com/acme/ai-rules.git --name acme

# 3. Install a kit from that source
inat install acme/coding-standards

# 4. See what's installed and whether updates are available
inat status

# 5. Pull the latest from source and re-deliver
inat update
```

After `install`, files are delivered to the locations defined by the active agent profile. For `claude-code`:

| Type | Destination |
|---|---|
| Skills | `.claude/skills/<skill-name>/` |
| Rules (`always`) | Inlined into `CLAUDE.md` inside a managed section |
| Rules (`contextual`) | Copied to `.inat/rules/`, referenced from `CLAUDE.md` |

Commit `.inat/` and the delivered files in your project — that is the reproducibility mechanism.

---

## Concepts

| Term | Definition |
|---|---|
| **Skill** | A directory following the Agent Skills format: `<skill-name>/SKILL.md` plus optional `scripts/`, `references/`, `assets/`. |
| **Rule** | A single markdown file carrying a persistent instruction. Priority `always` is inlined into the agent's rule file; `contextual` is copied and referenced. |
| **Kit** | A named bundle of skills and rules, described by a `kit.json`. |
| **Source** | A remote git repo that hosts kits, indexed by `registry.json` at its root. |
| **Agent profile** | Built-in mapping from file type to delivery target for a specific AI agent. v1 ships `claude-code`. |

Names (sources, kits, skills, rules) match `[a-z0-9]([a-z0-9-]*[a-z0-9])?`, max 64 chars, no consecutive hyphens. Delivered files use the skill/rule name only — no source or kit prefix — so names must be unique across all installed kits in a project.

---

## Commands

```
inat init [--agent <name>]

inat sources add <url> [--name <alias>] [--path <subpath>]
inat sources remove <name>
inat sources list

inat agents add <name>
inat agents remove <name>
inat agents list

inat install <source>/<kit>
inat update  [<source>/<kit>]  [--force] [--plan]
inat remove  <source>/<kit>    [--force]
inat refresh [--force]
inat status

inat sync
inat rename  <source>/<old> <source>/<new>
inat replace <source>/<old> <newSrc>/<new> [--force] [--plan]

inat --user <command> ...   # sources, agents, install, update, remove,
                            # refresh, status, sync, rename, replace
```

Bare kit names (without `<source>/`) are accepted when unambiguous. `--force` overrides modified-file detection on `update`, `remove`, `refresh`, and `replace`. `--plan` (on `update` and `replace`) previews the planned actions on stdout — one line per kit — and returns without applying anything; mutually exclusive with `--force`. Spec [`0020-update-plan-dry-run`](docs/specs/0020-update-plan-dry-run.md).

**Source hosts.** HTTPS URLs against GitLab, GitHub, Azure DevOps (`dev.azure.com/<org>/<project>/_git/<repo>` and the legacy `*.visualstudio.com` form), and other generic git hosts are supported. SSH URLs (`git@host:path` and `ssh://`) work via your SSH agent. `file://` and local paths work for fixtures and self-hosted layouts. Authentication for HTTPS hosts is delegated to `git credential fill` — whatever credential helper your `git` CLI uses (Windows Credential Manager, `git-credential-manager-core`, `osxkeychain`, etc.) provides the PAT.

If you hit an HTTP-layer error against a host that should work, set `INAT_DEBUG_HTTP=1` in the environment — inat will dump method, URL, headers, request body, response status, and 4xx response bodies to stderr so you can see exactly what the host is rejecting (and file a useful bug). Spec [`0012-azure-devops-user-agent`](docs/specs/0012-azure-devops-user-agent.md) walks through this exact diagnostic flow.

`inat refresh` also re-delivers the built-in rules (`inat-usage`, plus `inat-kits` when `kits_repo: true`) for every active agent. **One-time migration for v0.3.x upgraders:** if your project was initialised with v0.3.x, run `inat refresh --force` once after upgrading — refresh will warn that the on-disk built-in content differs from the new embedded content, and `--force` lets it re-deliver. Subsequent upgrades are friction-free. v0.4.0 → next is silent (no warning, no `--force` needed). Spec [`0011-refresh-builtin-rules`](docs/specs/0011-refresh-builtin-rules.md).

All `install`, `update`, `remove`, `refresh`, and `agents add/remove` operations are atomic — planned, then executed; config is written last; any failure rolls back.

### User scope

`--user` runs a command against your home directory instead of the current project, for kits you want in every session regardless of the repo. It works from any directory and needs no `init`: `~/.inat/config.json` is created on the first write.

```
inat --user sources add https://git.example.com/team/kits.git --name company
inat --user agents add claude-code
inat --user install company/personal-habits
```

For `claude-code`, skills go to `~/.claude/skills/`, always rules into the managed section of `~/.claude/CLAUDE.md`, and contextual rules to `~/.inat/rules/`, referenced from `~/.claude/CLAUDE.md` as `~/.inat/rules/<name>.md`. The agents list in `~/.inat/config.json` is shared: an agent added with `--user` is also active in every project, as with `agents add --global`. Names must be unique within a scope; when a project and the user scope deliver the same name, `install` and `update` print a warning and carry on. `init`, `validate` and `self` reject `--user`, and running a project command with your home directory as the cwd is refused. Spec [`0025-user-scope`](docs/specs/0025-user-scope.md).

### Reorganization

Upstream kit corpora evolve: kits get renamed in-source (`git mv kits/foo kits/foo-renamed`), moved between sources, or retired. Three verbs cover the manual workflow:

- **`inat sync`** is the read-only diagnostic. It fetches every configured source, compares each installed kit's recorded tree SHA against the source's current registry, and prints one suggestion per divergent kit (rename / replace / remove). Exit is non-zero on any divergence so CI can gate on drift. Sync never mutates `.inat/config.json` or any delivered file.
- **`inat rename <src/old> <src/new>`** rewrites the install key when upstream did a pure `git mv` that left the kit's tree byte-identical at the new path. Tree-SHA equality is enforced; if content drifted in the same commit as the rename, run `inat replace` instead. Rename touches only `cfg.Installed`'s map key plus the always-rule managed-section headers in your agent files (so they reference the new key) — delivered file content is unchanged because it's already correct for the new tree. **Pure-`git mv` renames are auto-applied during `inat update`** (spec [`0018`](docs/specs/0018-kit-reorg-auto-rename.md)); the explicit verb remains available for users who prefer to audit each move via `inat sync` first.
- **`inat replace <src/old> <newSrc/new> [--force]`** atomically removes the old install and installs the new kit. The typical use is a cross-source move; same-source moves with content drift are also accepted. On install-half failure both the old kit's `cfg.Installed` entry and its delivered files on disk are restored from a snapshot (spec [`0021`](docs/specs/0021-apply-snapshot-restore.md)) — your config and your tree never wedge half-way.

The intended flow is `sync` to diagnose, then run the suggested verb. A typical in-source rename:

```
$ inat sync
2 kits installed; 1 need attention.
  company/foo — renamed in source 'company' to 'foo-renamed'
    suggested: inat rename company/foo company/foo-renamed

run `inat sync` again after applying suggestions, or `inat update` to refresh remaining kits.

$ inat rename company/foo company/foo-renamed
renamed company/foo → company/foo-renamed
```

In addition, `inat update` (multi-kit form) now skips kits that need attention and continues, returning a non-zero exit with a summary footer pointing at `inat sync`. The targeted form `inat update <src/kit>` keeps its fail-fast contract. Spec [`0016-kit-reorg-baseline`](docs/specs/0016-kit-reorg-baseline.md).

### Recovery from a failed apply

Both `inat update` and `inat replace` capture an on-disk snapshot of every file they could mutate before the apply phase begins (spec [`0021`](docs/specs/0021-apply-snapshot-restore.md)). The snapshot lives under `~/.inat/snapshots/<UTC-timestamp>-<sanitized-install-key>/` and is removed on a clean success. If the apply fails, the snapshot is replayed automatically — delivered files and the agent's managed-section target files (CLAUDE.md, contextual rule references) come back byte-identical to pre-apply — and the directory is left on disk for forensics. If the restore itself fails (rare), the typed error names the snapshot directory and `inat refresh` remains the fall-back recovery verb.

---

## Source repository layout

A source repo looks like:

```
registry.json
kits/
  <kit-name>/
    kit.json
    skills/
      <skill-name>/SKILL.md         (+ optional scripts/, references/, assets/)
    rules/
      <rule-name>.md
```

The same layout can also live in a subdirectory of a larger repo — useful when
a general-purpose repo wants to expose kits without colonizing its root.
Register such a source with `--path`:

```
inat sources add https://example.com/my-repo.git --name shared --path ai-resources
```

`inat` will then look up `ai-resources/registry.json` and `ai-resources/kits/<kit>/`
inside that repo. The subpath must be relative, use forward slashes, and contain no `..` or `.` segments.

See [`docs/specs/0001-initial-idea.md`](docs/specs/0001-initial-idea.md) §4 for the full schema.

### Rule frontmatter

Each rule `.md` SHOULD begin with a YAML frontmatter block declaring `name` (matching the filename-derived name) and `description` (a one-sentence "when to read" hint, ≤200 chars, single line):

```markdown
---
name: use-1password-cli
description: Read when the user mentions secrets, env vars, or any task that would otherwise paste a credential into a file.
---

# rule body…
```

When present, `description` is rendered alongside the rule's reference in the inat-managed section, e.g. `- .inat/rules/use-1password-cli.md — Read when …`. When absent, the bullet renders bare and `inat install` / `refresh` / `validate` emit a warning. See [`docs/specs/0010-rule-descriptions.md`](docs/specs/0010-rule-descriptions.md).

---

## Configuration

- `~/.inat/config.json` — global agents list, plus the user scope's sources and installed kits (`--user`).
- `~/.inat/cache/` — cloned source repos, shared across projects. **Not** committed anywhere.
- `.inat/config.json` in each project — sources, installed kits (with tree SHAs + delivered-file hashes), project-level agents. **Is** committed.
- `.inat/rules/` — contextual rule files delivered by the agent profile. Committed.

Authentication is delegated to your existing git setup — SSH keys via `ssh-agent` / `~/.ssh/config`, HTTPS tokens via git credential managers. `inat` stores no credentials.

---

## Development

The released binary is built from `go/`. CI (GitHub Actions, `.github/workflows/ci.yml`) runs the same checks on Linux, Windows and macOS.

```bash
cd go
go build .                # debug build
go test ./...             # run the test suite
go vet ./...              # built-in static analysis
staticcheck ./...         # clippy-equivalent linter (install via go install honnef.co/go/tools/cmd/staticcheck@v0.8.2)
gofmt -l .                # formatting check (must print nothing)
```

Rules of the road are in [`docs/guidelines/coding-standards.md`](docs/guidelines/coding-standards.md). Highlights:

- TDD-first. No untested code.
- No use of the `unsafe` package.
- Files ≤300 lines (hard max 400); functions ≤30 lines (hard max 50).
- Typed errors via `errs.Error` with `errors.Is` / `errors.As` semantics.
- Synchronous I/O only (`os`, `path/filepath`, `go-git`) — no goroutines for the CLI flow.
- Dependency allowlist — new modules require justification. Currently approved: `spf13/cobra`, `go-git/go-git/v5`, plus the standard library.

---

## Contributing

### One-time setup after clone

```bash
bash scripts/install-hooks.sh
```

This points `core.hooksPath` at the committed `.githooks/` directory so every `git commit` in the repo runs `gofmt -l` + `go vet` + `staticcheck` + `go test ./...` against `go/` and enforces the spec-presence check described below. The script also installs `staticcheck` for you (via `go install`) when Go is on `PATH`. The hook is native git, not a Claude Code hook — it fires regardless of who or what invoked the commit, so cross-instance launches (Claude started from a parent directory) cannot bypass it.

### Spec-first / plan-first workflow

Before changing code under `go/`, add or update a spec in [`docs/specs/`](docs/specs/) and, for non-trivial features, a companion plan in [`docs/plan/`](docs/plan/). The pre-commit hook blocks any commit that touches `go/` without also touching `docs/specs/` or `docs/plan/`. Override for mechanical commits (formatting, `go.sum` bumps, renames without behaviour change) with `INAT_SKIP_SPEC_CHECK=1 git commit ...`.

Project-level AI-agent rules live in [`CLAUDE.md`](CLAUDE.md). Launch your AI tooling from the repo root so those rules are in context.

---

## Documentation

- [`docs/specs/0001-initial-idea.md`](docs/specs/0001-initial-idea.md) — authoritative design spec.
- [`docs/specs/0002-ci-cd-and-install.md`](docs/specs/0002-ci-cd-and-install.md) — CI/CD and installer contract.
- [`docs/plan/0002-ci-cd-and-install.md`](docs/plan/0002-ci-cd-and-install.md) — concrete mapping of spec 0002 onto GitLab.
- [`docs/specs/0003-source-subpath.md`](docs/specs/0003-source-subpath.md) — source-subpath layout spec.
- [`docs/plan/0003-source-subpath.md`](docs/plan/0003-source-subpath.md) — retrospective plan for the `--path` feature.
- [`docs/specs/0004-rename-packages-to-kits.md`](docs/specs/0004-rename-packages-to-kits.md) — terminology rename (package → kit).
- [`docs/plan/0004-rename-packages-to-kits.md`](docs/plan/0004-rename-packages-to-kits.md) — rollout plan for the rename.
- [`docs/guidelines/coding-standards.md`](docs/guidelines/coding-standards.md) — coding standards.
- [`docs/pitch-narrative.md`](docs/pitch-narrative.md) — problem/solution story.
- [`docs/demo-walkthrough.md`](docs/demo-walkthrough.md) — end-to-end worked example.
- [`CLAUDE.md`](CLAUDE.md) — guidance for AI agents working in this repo.
