# Implementation Plan — CI/CD and Install (spec 0002)

Concretises [`docs/specs/0002-ci-cd-and-install.md`](../specs/0002-ci-cd-and-install.md) onto the current hosting and CI stack: GitLab (the internal GitLab instance), GitLab CI, GitLab Releases.

This plan is the *how*. The spec is the *what*. If a future migration moves inat off GitLab, only this document needs to change; the spec stays.

---

## 1. Summary of concrete choices

| Spec concept | GitLab mapping |
|---|---|
| CI system | GitLab CI (the GitLab CI config at repo root) |
| Release channel | GitLab Releases on the inat project |
| Pre-merge trigger | MR pipelines and branch push pipelines |
| Release trigger | Tag pipelines matching `v*.*.*` |
| Artifact storage | GitLab Release assets (generic package registry as fallback if asset URLs require auth — see §5) |
| Install script hosting | Same URL as release assets; canonical location is the permalink on the latest Release |

---

## 2. Files introduced by this work

| Path | Purpose |
|---|---|
| GitLab CI config | Top-level pipeline: `lint`, `test`, `release` stages |
| `ci/build-release.sh` | POSIX build + archive helper (Linux + macOS release jobs) |
| `ci/build-release.ps1` | Windows build + archive helper |
| `ci/verify-release-prereqs.sh` | Checks Cargo.toml-vs-tag version match and CHANGELOG section presence |
| `ci/extract-changelog-section.sh` | Extracts `[X.Y.Z]` section from CHANGELOG.md for release notes |
| `install.sh` | POSIX installer, committed at repo root, also published as a release asset |
| `install.ps1` | Windows installer, committed at repo root, also published as a release asset |
| `uninstall.sh` | POSIX uninstaller |
| `uninstall.ps1` | Windows uninstaller |
| `CHANGELOG.md` | Keep-a-Changelog format, seeded with `[0.1.0]` for current state |
| `LICENSE` | Full licence text at repo root. Required by spec §7 — bundled into every release archive. Repo currently has no LICENSE file; it must land before step 6's first tagged build. |
| Updates to `README.md` | Install one-liners + troubleshooting pointer |

All shell / PowerShell scripts respect the repo-wide ≤300 line soft cap from `docs/guidelines/coding-standards.md` (400 hard max). Split by responsibility when exceeded.

---

## 3. Pipeline structure

### 3.1 Stages

```
stages:
  - lint
  - test
  - release
```

### 3.2 `lint` stage — always, Linux only

- Image: pinned Rust stable image (e.g. `rust:1.94-bookworm` — reference only, updated when the repo upgrades).
- Steps: `cargo fmt --check`, `cargo clippy --all-targets --all-features -- -D warnings`.
- Uses a `cargo` cache keyed on `Cargo.lock`.

### 3.3 `test` stage — matrix, one job per tier-1 target

Runs on MR pipelines and branch pipelines. Four parallel jobs:

| Job | Runner | Command |
|---|---|---|
| `test:linux-x64` | Linux runner (shared or SBP self-hosted) | `cargo test` |
| `test:windows-x64` | Windows runner | `cargo test` |
| `test:macos-x64` | macOS Intel runner | `cargo test` |
| `test:macos-arm64` | macOS Apple Silicon runner | `cargo test` |

**Runner availability is the biggest unknown.** Before step 5 lands the full matrix, the runner-spike MR (§6 step 4) must confirm which runners the SBP GitLab instance exposes and what tag selectors they use. If macOS runners are unavailable, two options: (a) temporarily downgrade macOS to tier-2 and flag for follow-up, or (b) self-host a macOS runner. Decide after the spike, not before.

### 3.4 `release` stage — tag pipeline only, `if: $CI_COMMIT_TAG =~ /^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$/`

Jobs in this stage:

1. **`release:verify`** — Linux, fast. Runs `ci/verify-release-prereqs.sh`:
   - Parses `$CI_COMMIT_TAG`, strips the leading `v`.
   - Compares against `version` in `Cargo.toml`. Fails on mismatch.
   - Greps `CHANGELOG.md` for `## \[<version>\]`. Fails if absent.
   - No other job in this stage runs if this fails.
2. **`release:build:<target>`** — one job per tier-1 target, plus tier-2 `linux-aarch64` as `allow_failure: true`. Each:
   - Builds `cargo build --release --target <triple>` on the native runner for that target.
   - Strips the binary.
   - Packages `inat(.exe)`, `LICENSE`, and a short `README` into `inat-<version>-<triple>.(tar.gz|zip)`.
   - Uploads the archive as a pipeline artifact.

   **Linux builder image is pinned to honour spec §3's glibc 2.31 floor.** The `release:build:linux-x64` job runs in a Rust image based on Debian bullseye (glibc 2.31) — reference: `rust:1.94-bullseye`. Bookworm-based images (glibc 2.36) must not be used for the release builder or the resulting binary will fail to run on Ubuntu 20.04 / RHEL 8. The lint and test jobs may use a newer base image; only `release:build:linux-x64` has the floor constraint. Step 4's spike (§8) must confirm what glibc the available Linux runner itself provides — if it's newer than the builder image, the Docker-in-Docker / native choice shifts accordingly.
3. **`release:publish`** — Linux. Depends on all `release:build:*` jobs:
   - Downloads all build artifacts.
   - Generates `SHA256SUMS` covering every archive (`sha256sum inat-*.{tar.gz,zip} > SHA256SUMS`).
   - Uses `release-cli` (GitLab's official CLI) to create a Release on the tag, attaching all archives, `SHA256SUMS`, `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1`.
   - Release description set to output of `ci/extract-changelog-section.sh <version>`.

**Cross-compilation is deliberately avoided** — every target builds on its native OS. This removes a class of `libgit2` linking headaches and keeps build config simple.

---

## 4. Installer scripts — contract and structure

Both scripts implement the same state machine; the spec's §9 is the source of truth for observable behaviour. Implementation notes only below.

### 4.1 Shared logical flow

1. Parse flags (`--version`, `--pre-release`, `--no-modify-path`, `--install-dir`) and env vars (`INAT_INSTALL_DIR`, `INAT_TOKEN`, `INAT_TOKEN_TYPE`).
2. **Require `INAT_TOKEN`** (§5 spike outcome — recorded in §8). Build auth header: `PRIVATE-TOKEN: $INAT_TOKEN` by default, or `Deploy-Token: $INAT_TOKEN` when `INAT_TOKEN_TYPE=deploy`. Missing token → exit 2 with a precise message pointing at the README token-provisioning section.
3. Resolve `(os, arch)` → `target-triple` using a fixed lookup table. Published targets are `x86_64-unknown-linux-gnu` and `x86_64-pc-windows-msvc` (for the PowerShell installer); macOS and Linux aarch64 are accepted by the triple table but will fall through to a "no SHA256 for archive" error at checksum time until those artifacts are produced.
4. Resolve version:
   - If `--version` supplied → use verbatim.
   - Else fetch `https://<host>/api/v4/projects/<id>/releases?per_page=50` (GitLab REST), extract `tag_name` fields with `grep -oE '"tag_name":"[^"]+"'` (no `jq` dependency), filter by pre-release flag (stable = no hyphen in the tag body), pick the first.
5. Build archive URL + `SHA256SUMS` URL from `https://<host>/api/v4/projects/<id>/packages/generic/inat/<version>/<file>`. Download both to a temp directory (`mktemp -d`) using a single `fetch` helper with one automatic retry on transient failure (exit 4 after retry).
6. Verify: extract expected hash from `SHA256SUMS` via `awk`, compute local hash with `sha256sum` if present (Linux) or `shasum -a 256` (macOS); string-compare. Exit 3 on mismatch.
7. Extract: POSIX `tar -xzf "$archive" -C "$stage"` then move `$stage/inat-<version>-<target>/inat` to the install dir. Exit 6 on missing expected path. PowerShell path (step 8): `Expand-Archive`.
8. Move binary into the install dir, `chmod +x` on POSIX.
9. PATH handling:
   - POSIX: detect `$SHELL`, pick profile file (`.bashrc`/`.zshrc`/fish config), check if INSTALL_DIR is already on `$PATH` (no-op), else check for existing managed-section markers (`# >>> inat install >>>` / `# <<< inat install <<<`), append the managed block if absent. Never edit user content outside the markers. Unknown shell → print the snippet to stderr and exit 0.
   - PowerShell (step 8): read user `PATH` via `[Environment]::GetEnvironmentVariable('PATH','User')`, append install dir if missing, write back.
10. Print version, install dir, PATH action taken (if any), and the "open a new shell" hint.

The one-liner documented in README for the POSIX installer is therefore:

```
curl -fsSL -H "PRIVATE-TOKEN: $INAT_TOKEN" \
  https://<host>/api/v4/projects/<project-id>/packages/generic/inat/latest/install.sh | sh
```

The `/latest/install.sh` path is overwritten on every tagged release by the `release:publish` job, in addition to the versioned `/…/<version>/install.sh` path that the release-cli asset links point at.

### 4.2 Why managed-section markers

Matches the pattern already used by inat itself for agent rule files (see spec 0001 §"Rules"). Same concept, same safety guarantee: the installer never mutates user content outside its own markers, so uninstall is a clean removal.

### 4.3 Exit codes

| Code | Condition |
|---|---|
| 0 | Success |
| 1 | Unknown error (unexpected) |
| 2 | Unsupported OS or architecture |
| 3 | Checksum mismatch |
| 4 | Network failure after one retry |
| 5 | Requested version not found |
| 6 | Archive layout unexpected after extraction |

### 4.4 Testability

Rust-style unit tests aren't available for shell/PowerShell, but the purely-local helpers (flag parsing, token header construction, tag filtering, managed-block stripping) are testable by sourcing the scripts in a test-mode where `main` is skipped and calling functions directly.

- `shellcheck install.sh uninstall.sh ci/*.sh` runs in the `lint` stage on the Linux runner.
- `sh tests/installer_tests.sh` runs in the `lint` stage (sources `install.sh` with `INAT_INSTALL_TEST=1` to suppress `main`, asserts each helper).
- `Invoke-ScriptAnalyzer install.ps1 uninstall.ps1 -Settings PSScriptAnalyzerSettings.psd1 -Severity Error,Warning` runs in the `lint:windows` job on the self-hosted Windows runner. The settings file excludes `PSAvoidUsingWriteHost` (installer UX deliberately uses Write-Host for user-visible output) and `PSUseShouldProcessForStateChangingFunctions` (not applicable to user-invoked scripts, only to module cmdlets).
- `tests\installer_tests.ps1` runs in the same `lint:windows` job (sources `install.ps1` with `$env:INAT_INSTALL_TEST = '1'`, uses a no-Pester inline `Assert-Equal` / `Assert-Throws` harness). `Resolve-Version` is exercised via a `-Fetcher` scriptblock override so unit tests never hit the network.
- End-to-end smoke tests: on installer-touching MRs, a reviewer runs both installers in a clean container/VM against the most recent pre-release tag. A GitLab CI job that runs the installer against a prior release is a stretch goal for later — blocked on provisioning a CI-job-scoped deploy token and is not a v1 requirement.

---

## 5. Auth / hosting validation spike (addresses spec §11)

> **Spike closed 2026-04-22 — result: gated.** Summary and decision
> recorded in §8 ("Anonymous release-asset fetch — GATED"). The
> remainder of this section is retained for historical context; the
> procedure below is the one that was executed.

Before step 7 wires the installers, run a small spike to determine whether the release-asset URLs on the inat project are fetchable anonymously inside the corporate network.

**Procedure:**

1. Manually create a draft release on GitLab with the tag `v0.0.1-spike` (or similar) and attach a dummy text file as an asset.
2. From a machine on the corp network that does not have cached GitLab credentials (a fresh container works):
   - `curl -fI <asset permalink URL>` — observe status code.
   - If 200 OK: anonymous asset access works; the spec's recommended `curl | sh` path is viable unchanged.
   - If 302 → SSO / 401 / 403: anonymous access is gated.
3. If gated, try the Generic Package Registry (`POST /projects/:id/packages/generic/...`) and observe anonymous read behaviour.
4. If still gated, the installer must gain an auth path: add `INAT_TOKEN` (GitLab Personal Access Token with `read_api` scope) env-var support to both installers, document token provisioning in the README.

**Output of the spike:** a one-paragraph note appended to this plan under §8 recording the observed hosting behaviour and the decision taken.

---

## 6. Execution order (reviewable MRs)

Each step is a separate MR against `main`, reviewed and merged before the next begins.

| # | MR | Contents | Depends on | Status |
|---|---|---|---|---|
| 1 | Spec | `docs/specs/0002-ci-cd-and-install.md` | — | Merged (commit `a810a78`) |
| 2 | Impl plan | `docs/plan/0002-ci-cd-and-install.md` (this file) | 1 | Merged (commit `a810a78`) |
| 3 | CHANGELOG seed | `CHANGELOG.md` with `## [0.1.0]` summarising current state | — (can land in parallel) | Merged |
| 3a | LICENSE | `LICENSE` file at repo root (Apache-2.0). Separate small MR; hard prerequisite of step 6 because the release pipeline bundles it into every archive. | — (can land in parallel) | Merged |
| 4 | Runner spike | Minimal GitLab CI config with only the Linux lint job. Confirm pipeline runs on the SBP instance. Append findings (runner inventory, macOS availability) to this plan under a §8 appendix. | 2 | Merged (lint green, inventory in §8) |
| 5 | Pre-merge CI | Linux test stage, then Windows test stage via self-hosted runner. macOS still deferred. | 4 | Merged |
| 6 | Release pipeline | `release` stage + `ci/build-release.sh` + `ci/build-release.ps1` + `ci/verify-release-prereqs.sh` + `ci/extract-changelog-section.sh`. Linux x86_64 + Windows x86_64 archives; macOS still absent. Smoke-tag `v0.2.0-rc.0` after merge to exercise end-to-end. | 3, 3a, 5 | Merged — smoke-tag `v0.2.0-rc.0` queued as step 6a |
| 6a | Release-prep + smoke tag | Cargo.toml bumped to `0.2.0-rc.0`, CHANGELOG `[0.2.0-rc.0]` section added, step 6 status marked merged. Tag `v0.2.0-rc.0` cut on main 2026-04-22, release pipeline green (verify + linux-x64 + windows-x64 + publish), GitLab Release created with three assets. | 6 | Merged |
| 6b | Auth-spike finding | Record §5 spike result in §8: the inat project is private, package-registry assets return 401, adopt spec §11 option (b) — `INAT_TOKEN` env var on installers. Locks design for steps 7 + 8. | 6a | Merged |
| 7 | POSIX installer | `install.sh` + `uninstall.sh` at repo root (auto-attached to the versioned release path and to `/latest/install.sh` overwritten on each tag). Requires `INAT_TOKEN` (PAT or deploy token) per §8 auth-spike finding. `tests/installer_tests.sh` added and wired into the `lint` job. | 5, 6, 6b | Merged |
| 8 | Windows installer | `install.ps1` + `uninstall.ps1`, attached by release pipeline alongside the POSIX pair. New `lint:windows` stage job runs PSScriptAnalyzer (with `PSScriptAnalyzerSettings.psd1` suppressing `PSAvoidUsingWriteHost` + `PSUseShouldProcessForStateChangingFunctions`) and `tests/installer_tests.ps1`. | 5, 6, 6b | Merged |
| 9 | README + announce | Install one-liners in `README.md` for POSIX + PowerShell, "Provisioning an INAT_TOKEN" section documenting PAT vs deploy-token flow, uninstall one-liners, troubleshooting pointer. Status banner updated for v0.2 RCs. Internal announcement is a separate comms step, not a file change. After this merges, tag `v0.2.0-rc.1` on main to smoke-test both installers end-to-end; once accepted, tag `v0.2.0` as the first stable release. | 7, 8 | Merged |
| 9a | Release-prep + RC.1 tag | Cargo.toml bumped to `0.2.0-rc.1`, CHANGELOG `[0.2.0-rc.1]` section added covering steps 6b/7/8/9, plan statuses refreshed. Tag `v0.2.0-rc.1` cut on main 2026-04-23, release pipeline green (verify + linux-x64 + windows-x64 + publish), GitLab Release created with 7 assets (archives + SHA256SUMS + install/uninstall scripts). | 9 | Merged |
| 9c | Fix: README PS one-liner on Windows PS 5.1 | First rc.1 smoke test on 2026-04-23 revealed the published `iwr \| iex` snippet fails on Windows PowerShell 5.1: GitLab's generic package registry serves `.ps1` as `application/octet-stream`, so `Invoke-WebRequest` returns `.Content` as `byte[]`, and `iex` stringifies each byte to its ASCII code — parser then chokes with `Unexpected token '114'` / `'101'` etc. Replace the install + uninstall snippets with a decode-then-`[scriptblock]::Create` pattern that also lets `-PreRelease` / `-Version` reach the installer's `param` block (which `\| iex` cannot do). Add a troubleshooting entry pointing users with the old symptom at the supported snippet. README-only change; no `install.ps1` change and no re-tag needed — `/latest/install.ps1` in the registry is correct. | 9a | Merged |
| 9d | CI hardening: windows retry on runner-side failure | Self-hosted windows runner observed going "unhealthy" mid-job on step 9c's MR (2026-04-23): trace-upload 5xx → 403 on job poll → runner self-disabled, 1h wall-clock timeout. Add `retry: max 2` to `test:windows-x64` and `lint:windows` jobs, scoped to `runner_system_failure`, `stuck_or_timeout_failure`, and `script_failure` narrowed via `exit_codes: [128]` (git fetch network blips only). Real regressions produce different exit codes (cargo test → 101, clippy → 1, pwsh → 1), so flakes can't mask them. GitLab-CI-config-only change. | 9c | Merged |
| 9e | CI load: avoid dual pipelines on branch push + MR open | Per-job `$CI_OPEN_MERGE_REQUESTS == null` rules don't help when a branch is pushed *before* the MR exists (the normal push-then-open-MR workflow): the branch-push pipeline fires, then MR creation fires a second one — both consume the self-hosted Windows runner and double the developer-laptop load. Add a top-level `workflow.rules` block that only allows pipelines for `merge_request_event`, default-branch commits, and `v*` tags. Consequence: pushing a feature branch with no open MR runs no pipeline — open the MR to get CI feedback. Halves Windows runner load per change set. GitLab-CI-config-only. | 9d | Merged |
| 9b | Stable v0.2.0 tag | `v0.2.0-rc.1` (with the 9c README fix) passed the spec §13 acceptance test end-to-end on clean Windows + clean POSIX on 2026-04-23 (README one-liner → working `inat --version` in under 60 s). Release-prep MR bumped `Cargo.toml` to `0.2.0`, added `[0.2.0]` CHANGELOG entry, refreshed this plan. Tag `v0.2.0` cut on main 2026-04-23 17:42Z, release pipeline green (verify + linux-x64 + windows-x64 + publish), GitLab Release `inat v0.2.0` created with Linux + Windows archives, `SHA256SUMS`, and install/uninstall scripts on `/latest`. Pipeline stalled ~35 min mid-way on the self-hosted Windows runner's 1h self-disable backoff (same cooldown class step 9d's job-level retry was meant for, but 9d protects the *job*, not the runner daemon); runner recovered on its own and the windows build completed in 162 s once scheduled. First stable release of the v0.2 line — Linux x86_64 + Windows x86_64 only (macOS still deferred per plan §8). | 9a, 9c | Merged + tag `v0.2.0` |

---

## 7. Reused project conventions

- `Cargo.toml` version field is where `ci/verify-release-prereqs.sh` reads the source-of-truth version.
- `docs/specs/0001-initial-idea.md` — spec numbering (`0002-...md`) and section-header style are copied from here.
- `docs/guidelines/coding-standards.md` — file and function size caps apply to shell/PowerShell scripts too.
- Managed-section marker pattern (comment-fenced region the tool owns) is borrowed from how `inat` already manages agent rule files; the installers reuse this idea for `PATH` additions in shell profiles.

No existing CI, installer, or release code exists in the repo — this is entirely new surface area.

---

## 8. Spike findings

Observed from the first green lint pipeline on `ci/0002-pipeline-bootstrap`
(a follow-up pipeline adds a runner-diag step).

### Linux runner — resolved

- **Type:** SBP-managed SaaS shared runners. `CI_RUNNER_DESCRIPTION`
  names the instance's production SaaS pool; `CI_RUNNER_TAGS` is
  `["shared", "services", "saas"]`. Jobs without an explicit `tags:`
  directive pick these up automatically, so the current lint job has
  no tag selector.
- **Host OS:** RHEL 8 (kernel `4.18.0-553.111.1.el8_10.x86_64`). Runs
  Docker executor; jobs run inside the image declared in the GitLab CI config.
- **Container glibc:** 2.36 (from `rust:1.94-bookworm`), above the spec
  §3 2.31 floor. The release builder for step 6 must still pin
  `rust:1.94-bullseye` (glibc 2.31) to produce binaries runnable on the
  enterprise-distro floor — the runner's container glibc determines build
  compatibility, not runtime compatibility for end users.
- **Docker executor + cache:** image pulls, Cargo cache upload to
  the instance's S3 object store, and ~1s warm-cache clippy
  all working.

### Full runner inventory

Queried via `GET /projects/<project-id>/runners` + `GET /runners/<id>`.

Instance (SBP-wide, Linux amd64):

| Runner | Tags | `run_untagged` |
|---|---|---|
| Production SaaS shared pool | `shared`, `services`, `saas` | **yes** |
| Newer SaaS pool | `shared`, `services`, `saas`, `saas-patching` | no |
| Acceptance-environment runner | (environment-specific tag) | no |
| Docker-build runner | (Docker-socket tags) | no |

Project (self-hosted, project-scoped):

| Runner | Platform / Arch | Tags | `run_untagged` |
|---|---|---|---|
| Maintainer's laptop (Windows) | windows / amd64 | `windows`, `local` | no |

### Windows runner — SELF-HOSTED

SBP does not provide a shared Windows runner (request sent, no ETA).
Running a self-hosted GitLab Runner 18.11.1 on the maintainer's dev
laptop as a Windows service using the `powershell`
shell executor. Toolchain is system-wide Rust 1.95.0 at `C:\Rust\`
(CARGO_HOME / RUSTUP_HOME set via `Machine` scope, `C:\Rust\cargo\bin`
on the SYSTEM PATH) plus Visual Studio Build Tools for the
`libgit2-sys` / `openssl-sys` C compile.

First green Windows pipeline came from branch `ci/0002-windows-runner-probe`.
`cargo test --all-targets --all-features` passes 154 tests in ~10s warm.

Operational caveat: the runner is online only while the maintainer's
laptop is powered on. Jobs queue when the laptop is off and pick up
when it returns. Acceptable for a solo-maintained tool; would need
rethink if usage or team size grows.

### macOS runner — NOT AVAILABLE

No macOS runner on the SBP instance and no self-hosted Mac available.
Spec §3's tier-1 macOS commitment remains "CI enforcement deferred"
per the §3 amendment. `install.sh` covers macOS on the install side
(POSIX), so macOS engineers can still use the tool once a Mac binary
exists — but no Mac binary will be produced until a runner exists.

### Consequence for step 6 (release pipeline)

Tier-1 release artifacts now achievable by CI:

- Linux x86_64 (via the instance's shared SaaS runners)
- Windows x86_64 (via the self-hosted Windows runner)

Still blocked on runner provisioning:

- macOS x86_64 + aarch64

First stable release (`v0.2.0`) therefore ships Linux + Windows
archives. macOS is deferred per the spec §3 amendment.

### Quota / cost — not available

No per-minute budget surfaced via CI env vars. Ask SBP infra if a budget
constraint is likely (concurrent-runner count seems to be >= 1, which is
all step 4 strictly needs).

### Anonymous release-asset fetch (spec §11 / plan §5) — GATED

Smoke-tag `v0.2.0-rc.0` was cut 2026-04-22; its GitLab Release was
published by the release pipeline with three assets
(`SHA256SUMS`, `inat-0.2.0-rc.0-x86_64-unknown-linux-gnu.tar.gz`,
`inat-0.2.0-rc.0-x86_64-pc-windows-msvc.zip`) uploaded to the project's
generic package registry.

Anonymous HEAD probe from the maintainer's workstation (no token, no
netrc) against each asset permalink:

```
$ curl -sI --netrc-file /dev/null \
    https://<host>/api/v4/projects/<project-id>/packages/generic/inat/0.2.0-rc.0/SHA256SUMS
HTTP/1.1 401 Unauthorized
Www-Authenticate: Basic realm="GitLab Packages Registry"
```

Same result for the `.tar.gz` and `.zip`. Adjacent endpoints:

- `/api/v4/projects/<project-id>/releases` → **404** (GitLab masks private
  projects as 404 rather than 401).
- `/<group>/inat/-/archive/v0.2.0-rc.0/*.tar.gz` → **302** (redirects to
  SSO).
- `/api/v4/projects/<project-id>` → **404**.

The inat project is private on the SBP GitLab instance and
the infra team has not enabled anonymous read on either the repo or
the package registry. Spec §11 option (a) ("move to a different host
that allows anonymous reads") is not on the table — the project stays
on SBP GitLab.

**Decision: take spec §11 option (b) — installer carries an auth token.**

The step 7 POSIX installer and step 8 PowerShell installer must:

1. Read `INAT_TOKEN` from the environment (required; no interactive
   prompt). PAT with `read_api` scope, or a project/group deploy token
   with `read_package_registry`.
2. Send it on every GitLab request as `PRIVATE-TOKEN: $INAT_TOKEN`
   (for PATs) or the `Deploy-Token` header (for deploy tokens). The
   installer picks the header based on a presence heuristic or accepts
   `INAT_TOKEN_TYPE=private|deploy` to be explicit.
3. Fail fast with a precise message if `INAT_TOKEN` is absent or
   rejected, pointing the user at the README token-provisioning
   section.
4. Never echo the token or write it to disk.

README (step 9) must add a short "Provisioning an INAT_TOKEN" section:
PAT creation URL on the SBP instance, scope, one-liner exporting it
for `bash`/`zsh`/PowerShell profiles, rotation guidance.

Consequence for spec §13 acceptance: the "without configuring
credentials interactively" clause still holds — `INAT_TOKEN` is set
once in the shell profile, not prompted by the installer. The "brand
new engineer, stock laptop, sixty seconds" test stretches to "…after
a one-time PAT creation step documented in the README". Not ideal,
but unavoidable on a private-GitLab host.

Follow-up to consider (not in v1 scope): once inat itself is cleared
for public release, publish to a public hosting (GitHub Releases,
Cloudflare R2, or an unauthenticated S3 bucket) in parallel to the
SBP-internal channel, and swap the default install-source. That's a
later spec, not a blocker for v0.2.0.

### Spec-presence gate (added post-hoc)

Added `check:spec-presence` to the `lint` stage after a cross-instance
incident: a Claude Code session launched outside this repo shipped a
273-line Rust feature without loading the project `CLAUDE.md`, so the
spec/plan documents never accompanied the code. The gate fails MR
pipelines that touch `src/` without also touching `docs/specs/` or
`docs/plan/`, and mirrors the committed `.githooks/pre-commit` hook so
enforcement exists on both sides of the push. Bypass for mechanical
MRs with `[skip-spec-check]` in the title or CI var
`INAT_SKIP_SPEC_CHECK=1`.

Starts as `allow_failure: true` while we observe the signal on real
MRs. Promote to required in a follow-up once it fires cleanly for a
week of real traffic. The native git hook at `.githooks/pre-commit`
(install via `bash scripts/install-hooks.sh`) is the primary layer;
this CI job is the belt-and-braces backstop for contributors who did
not install the hook.

---

## 9. Verification (end-to-end)

These are the acceptance checks for each MR, corresponding to spec §13.

### Step 4 (runner spike)
- Lint pipeline runs green on the internal GitLab instance.
- §8 populated.

### Step 5 (pre-merge CI)
- Open an MR that introduces a `#[allow(unused)]` → still passes.
- Open an MR that introduces a clippy::pedantic violation (e.g. a public function returning `Result` without `# Errors` docs triggering `missing_errors_doc`, or a `fn f(v: Vec<T>)` triggering `needless_pass_by_value`) → lint job fails, merge blocked.
- Open an MR that breaks a test only on Windows → `test:windows-x64` red, others green, merge blocked.

### Step 6 (release pipeline)
- Tag `v0.2.0-rc.0` on a branch with matching Cargo.toml + CHANGELOG entry → 4 archives + `SHA256SUMS` attached to the Release, notes populated from CHANGELOG.
- Delete and re-tag with CHANGELOG section removed → `release:verify` fails before any build.
- Delete and re-tag with Cargo.toml version mismatched → `release:verify` fails before any build.

### Step 7 (POSIX installer)
- Fresh Ubuntu container: `curl -fsSL <URL>/install.sh | sh` → `inat --version` works in a new shell.
- macOS VM (Apple Silicon): same one-liner works.
- `curl ... | sh -s -- --version v0.1.0` installs the pinned version.
- `curl ... | sh -s -- --no-modify-path` installs but does not touch `.bashrc`.
- Tamper with `SHA256SUMS` on a test release → installer exits non-zero, no files left behind.
- Run installer twice → second run is clean, no duplicated `.bashrc` block.
- `uninstall.sh` → binary removed, `.bashrc` managed block removed, user config untouched.

### Step 8 (Windows installer)
- Fresh Windows 11 VM: `irm <URL>/install.ps1 | iex` in PowerShell → `inat --version` works in a new PowerShell session.
- `iwr <URL>/install.ps1 -OutFile install.ps1; .\install.ps1 --version v0.1.0` installs pinned version.
- `.\install.ps1 --no-modify-path` leaves user PATH env untouched.
- Checksum tamper → installer aborts, no files on disk.
- `uninstall.ps1` → binary removed, user PATH entry removed.

### Step 9 (stable release)
- Brand-new engineer, stock laptop, copy-pastes the README one-liner → working `inat --version` in under 60 seconds, no browser, no credentials prompt, no privileged command. (Spec §13 acceptance test.)
