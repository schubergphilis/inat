# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Cross-instance notice.** Claude Code loads `CLAUDE.md` via walk-up from the launch CWD. If Claude was started outside this repo (e.g. at a parent directory) and reaches in to edit files here, this file and `docs/guidelines/coding-standards.md` are NOT in its context. **Before editing inat, `cd` into the repo root and restart Claude** so the project rules activate. The committed native git hook at `.githooks/pre-commit` (install via `bash scripts/install-hooks.sh`) is the single enforcement layer that survives wrong-CWD launches — it fires regardless of how `git commit` was invoked. The hook runs `gofmt -l`, `go vet`, `staticcheck`, and `go test ./...` against `go/`. See `README.md` §Contributing.

## inat — AI Prompt & Rule Kit Manager

A CLI tool for managing reusable AI skill and rule kits across projects. Kits live in remote git repositories, are installed locally per-project, and delivered to AI agent targets via built-in agent profiles.

## Status

The Go binary is the released artifact, published on GitHub Releases from v0.9.0 (spec 0027). inat was first written in Rust; that implementation is retired and not part of this repository.

| Phase | Status |
|---|---|
| Initial design + Rust implementation (specs 0001–0007) | Done — released as v0.2.0 on 2026-04-23. Retired by the Go port. |
| Repo restructure + go-git spike (plan 0008) | Done — go-git spike validates all six git operations + cross-compile to all four release targets. |
| Full Go port (spec 0009) | Done — released as v0.3.0. CLI library `spf13/cobra`, auth via `git credential fill` + go-git BasicAuth. M1: pure-logic parity. M2: end-to-end CLI parity + `parity:rust-vs-go` CI job. M3a: pre-commit hook cargo → go (gofmt + vet + staticcheck + test). M3b: release pipeline cargo → `go build`, all targets cross-compiled from Linux. |
| Move to public GitHub (spec 0027) | Done — v0.9.0. Module path `github.com/schubergphilis/inat`. Releases, installers and `self update` use GitHub Releases anonymously. CI is GitHub Actions on Linux, Windows and macOS. |

## Repo layout

- **`go/`** — authoritative. The Go binary that ships from v0.3.0 onward. Built and tested by the pre-commit hook and CI (`gofmt -l` + `go vet` + `staticcheck` + `go test ./...`). Cross-compiled to all four release targets from the Linux runner.
- **`.github/`** — GitHub Actions: `ci.yml` (lint + test matrix on ubuntu/windows/macos, cross-compile, installer tests), `release.yml` (on `v*` tags: build, `SHA256SUMS`, GitHub Release; `-` tags are pre-releases), and Dependabot.
- **`install.{sh,ps1}`, `uninstall.{sh,ps1}`** — one-liner installers at the repo root, tested by `tests/installer_tests.{sh,ps1}`.

## Spec & Plan

The authoritative design spec lives at `docs/specs/0001-initial-idea.md`. Read it before making architectural decisions.

### Spec + plan pairs

Every non-trivial feature ships **two** documents under matching numbers:

- `docs/specs/NNNN-<slug>.md` — the **contract**: goals, CLI surface, semantics, error model, non-goals, acceptance criteria. Extends spec 0001; does not rewrite it.
- `docs/plan/NNNN-<slug>.md` — the **rollout**: decisions, changes, verification steps. Cross-references the spec in the first line.

Reviewers expect both. The pre-commit hook (`.githooks/pre-commit`) only requires *one* of the two — that's the floor, not the convention. Ship the pair.

**Exceptions** (spec or plan alone is acceptable):
- Mechanical commits — formatting, `go.sum` / dependency version bumps, CI tweaks. Bypass the hook's spec-presence check with `INAT_SKIP_SPEC_CHECK=1`.
- Work that lands purely inside spec 0001's existing contract (e.g. a retrospective plan documenting a change that needed no new goals). Plan 0005 is the reference example.

When starting a feature, pick the next `NNNN` by looking at the highest existing number across both `docs/specs/` and `docs/plan/`, and reserve it for both files.

## Coding Standards

Full coding standards: `docs/guidelines/coding-standards.md` — read before writing any implementation code.

### Critical Rules (always follow)

Apply to the Go tree at `go/`.

1. **TDD-first** — Write tests before implementation. No untested code.
2. **No `unsafe` package** — Plain Go only. If a use case requires `unsafe`, surface it for explicit review.
3. **File size** — ≤300 lines target, 400 hard max. Split by responsibility when exceeded.
4. **Function size** — ≤30 lines target, 50 hard max. Extract helpers for testability.
5. **Error handling** — Typed errors via `errs.Error` with `errors.Is` / `errors.As` semantics. Wrap I/O / JSON / git failures via the `WrapIO`, `WrapJSON`, `WrapGit` helpers. Never return raw `error` for domain failures from public APIs.
6. **Naming verbs** — `Load`/`Save` (file I/O), `Fetch` (network), `Parse`/`Validate` (input), `Deliver`/`Remove` (delivery), `Is`/`Has` (bool), `Ensure` (idempotent). Public functions include action + target.
7. **Module structure** — Two-level domain packages: `cli/`, `core/`, `git/`, `agents/`. Avoid catch-all `util` packages; prefer leaf packages like `core/reserved` for small shared constants.
8. **Dependency allowlist** — Only `spf13/cobra`, `go-git/go-git/v5`, and standard library. New deps require justification.
9. **`staticcheck`** — Enabled in the pre-commit hook and CI. CI also runs `go vet ./...` and `gofmt -l .`.
10. **No silent error discard** — Always return errors up the stack; never `_ = err`. Production code should not `panic`; reserve panics for `init`-time invariants that genuinely cannot fail at runtime.
11. **Synchronous only** — No goroutines for the CLI flow. Blocking I/O via `os` / `path/filepath` / `go-git`. Concurrency is reserved for future features that justify it via spec.

## Key Concepts

- **Skill** — A directory containing an AI prompt, instruction, or action for a specific task. Uses the Agent Skills directory format (`skill-name/SKILL.md` with optional `scripts/`, `references/`, `assets/`).
- **Rule** — A single file containing a persistent instruction or constraint. Has `always` (inlined) or `contextual` (referenced) priority.
- **Kit** — A named group of related skills and rules. No semver in v1 — git tree SHAs track changes.
- **Source** — A remote git repository containing kits, indexed by `registry.json`. Configured per project, or in the user scope under `--user`.
- **User scope** — `--user` runs a command against the home directory as if it were a project: config in `~/.inat/config.json`, delivery to the agent profile's user mapping (`~/.claude/…`, `~/.inat/rules/`). Spec 0025.
- **Agent Profile** — A built-in mapping defining where each file type is delivered for a specific AI agent.

## Project Structure

```
go/                  — authoritative Go binary (the released artifact)
go/cli/              — CLI command packages (init, sources, agents, install, …)
go/core/             — pure-logic core (config, naming, hash, kit, registry, …)
go/git/              — go-git source + cache integration
go/agents/           — agent profiles (claude-code) + built-in rules
ci/                  — release scripts (build-release.{sh,ps1}, verify-release-prereqs.sh)
.github/             — GitHub Actions workflows + Dependabot
tests/               — installer test suites
docs/specs/          — design specs (the contract)
docs/plan/           — implementation plans (the rollout); pairs with docs/specs by NNNN
VERSION              — release version (single source of truth)
.inat/config.json    — per-project configuration (sources, installed kits, agents)
.inat/rules/         — contextual rule files (delivered by agent profile)
~/.inat/config.json  — global agents + user-scope config (`--user`)
~/.inat/cache/       — global cache of cloned source repos (not committed)
```

## CLI Commands

`init`, `sources add/remove/list`, `agents add/remove/list`, `install`, `update`, `remove`, `refresh`, `status`, `validate`

Every command except `init`, `validate` and `self` accepts the persistent `--user` flag. Kits are referenced using `source/kit` syntax (e.g., `inat install company/coding-standards`). Bare names allowed when unambiguous.

`init --kits-repo` marks a repo as a kit-authoring source; combined with `--agent`, auto-delivers a second built-in rule (`inat-kits`) that teaches the authoring contract. `validate [PATH] [--subpath DIR] [--json]` runs an offline structural check that reuses the existing parsers; exit 0 on clean.

## Naming

- Names must match `[a-z0-9]([a-z0-9-]*[a-z0-9])?` — no consecutive hyphens (`--`), max 64 chars.
- Delivered files use skill/rule name only — no source or kit prefix. Names must be unique across all installed kits.

## Rules

- No secrets in project files — auth delegated to git's credential system.
- `.inat/` directory should be committed; global cache must not.
- All operations are atomic — rollback on failure, config written last.
- Delivered files are hashed at delivery; modified files trigger warnings on update/remove (`--force` to override).
- Agent rule files (e.g., CLAUDE.md) use a managed section with markers — never edit user content.
- `agents add` auto-delivers all installed kits; `agents remove` auto-cleans.
- No offline fallback — network required for install/update/status. `validate` is the only command that runs offline.
- Dependency resolution between kits is explicitly out of scope.
- `inat-usage` and `inat-kits` are reserved rule names — kits cannot ship entries that derive to either.
<!-- inat:managed - DO NOT EDIT THIS SECTION -->

Read these rules when relevant:
- .inat/rules/inat-usage.md — Read when the user asks to install/update/remove a kit, add/remove an agent, or anything that touches files under `.inat/`, `.claude/skills/`, or the inat-managed section of `CLAUDE.md`.

<!-- inat:end -->
