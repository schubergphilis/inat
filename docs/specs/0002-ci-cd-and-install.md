## inat — CI/CD and Install Spec

---

### 1. Overview

This spec defines the contract for continuous integration, release publishing, and end-user installation of the `inat` CLI. It is deliberately independent of any specific CI system, hosting product, or package manager. Any choice of CI system or artifact host that satisfies every requirement below is a valid implementation.

A companion implementation plan (in `docs/plan/`) maps each requirement here onto concrete tooling and hosting for the current deployment.

---

### 2. Goals

1. An engineer on a stock laptop with a standard shell can install `inat` with a single copy-paste command, without opening a browser, without configuring credentials interactively, and without running a privileged command.
2. Every merge to `main` has passed automated lint and tests on every supported platform.
3. Every published release is reproducible from the tagged source and can be verified for integrity by the installer before the binary is executed.
4. The release process is driven by git tags; there is no dependency on a specific CI system's web UI for correct operation.

---

### 3. Supported platform matrix

**Tier-1 — every release must produce and verify artifacts for all of these:**

| Operating system | CPU architecture | Target identifier | CI enforcement |
|---|---|---|---|
| Windows | x86_64 | `x86_64-pc-windows-msvc` | Enforced via self-hosted runner (see note). |
| macOS | x86_64 (Intel) | `x86_64-apple-darwin` | **Deferred** — no macOS runner available; see note below. |
| macOS | aarch64 (Apple Silicon) | `aarch64-apple-darwin` | **Deferred** — no macOS runner available; see note below. |
| Linux | x86_64 (glibc) | `x86_64-unknown-linux-gnu` | Enforced. |

**CI enforcement on Windows** uses a self-hosted project-scoped GitLab Runner (tagged `windows`) running on the maintainer's development machine. This is a convenience arrangement, not a production-grade host — the runner is online only when the maintainer's laptop is powered on, and all Windows jobs block on it. Acceptable for a solo-maintained tool; if usage or team size grows, migrate to an SBP-provisioned shared Windows runner. See `docs/plan/0002-ci-cd-and-install.md` §8 for operational details.

**CI enforcement is deferred on macOS** until the hosting instance provisions runners or a self-hosted Mac becomes available. The tier-1 commitment stands — the tool is designed to work on all four platforms. Until macOS runners exist:

- macOS engineers install by building from source (`cargo install --path .` in a fresh clone).
- The `README.md` `Install` section states this gap explicitly; it is not a silent exclusion.
- Promotion to enforced tier-1 is a config-only task: add the platform's test and release jobs to the GitLab CI config, publish the resulting archive, no spec change needed.

**Tier-2 — ship if cheap, do not block release on failure:** Linux `aarch64-unknown-linux-gnu`.

**Explicitly out of scope for v1:** Windows-on-ARM, Linux musl/static, FreeBSD, 32-bit targets, universal macOS binaries.

**Linux binary compatibility floor:** the Linux x86_64 build is produced against a glibc version old enough to run on currently-supported enterprise distributions (reference floor: glibc 2.31 / Ubuntu 20.04 vintage).

---

### 4. Versioning and release trigger

- Releases use semantic versioning: `MAJOR.MINOR.PATCH`. Tags are prefixed `v`, e.g. `v0.2.0`.
- Pre-release tags are permitted: `v0.2.0-rc.1`, `v0.2.0-beta.1`, etc. They go through the same release pipeline and are published as pre-releases, not latest.
- The act of pushing a matching tag is the authorisation to release. There is no manual "publish" button in a web UI, no auto-release on merge to `main`, and no nightly release channel.
- The version encoded in the source tree (`Cargo.toml`) must exactly equal the version in the tag that triggers the release. The release pipeline must refuse to publish on mismatch.
- The git tag is the canonical source of the release version.

---

### 5. Pre-merge quality gate

Every push to a branch and every merge request must satisfy:

1. **Lint** — runs once, on one Linux environment:
   - Source formatting is checked (no reformatting allowed).
   - A strict static analyser pass runs with warnings treated as errors (the codebase already enables `clippy::pedantic`).
2. **Tests** — the full test suite runs natively on every tier-1 platform for which CI enforcement is active (see §3). Cross-compiled test runs are not acceptable; each target runs tests on its own native environment.
3. The toolchain used is the current stable Rust release. There is no minimum-supported-Rust-version commitment and no beta/nightly toolchain jobs.

A merge request that fails any of the above is not mergeable.

---

### 6. Release pipeline

On receipt of a tag matching `v*.*.*`:

1. **Pre-flight verification** — before any build work:
   - The `Cargo.toml` version matches the tag version.
   - `CHANGELOG.md` contains a section for this version (see §10).
   - Any mismatch fails the pipeline here, before any build runs.
2. **Build** — for every tier-1 target for which CI enforcement is active per §3 (and best-effort for tier-2):
   - Build a release-optimised binary on a native environment for that target.
   - Strip debug information from the binary.
   - Package the binary plus `LICENSE` and a short `README` into an archive (see §7).
3. **Integrity manifest** — after all builds complete, produce a single `SHA256SUMS` file containing the SHA-256 of every archive.
4. **Publish** — attach all archives, the `SHA256SUMS` file, the POSIX install script, the PowerShell install script, the POSIX uninstall script, and the PowerShell uninstall script to the release associated with the tag. Release notes are the text of the matching `CHANGELOG.md` section (see §10).

The pipeline is atomic from the user's perspective: either all tier-1 artifacts are published together or none are (a partial publish is a failed release).

---

### 7. Artifact naming and packaging

**Naming — deterministic and installer-discoverable:**

- `inat-<version>-<target-triple>.tar.gz` for Unix targets (`<version>` is the tag without the leading `v`).
- `inat-<version>-<target-triple>.zip` for Windows targets.

**Archive contents:**

- The `inat` executable (`inat.exe` on Windows).
- `LICENSE`.
- A short `README` with a pointer to online docs.

**Archive layout:** a single top-level directory `inat-<version>-<target-triple>/` containing the above. No other files, no nested structure.

---

### 8. Artifact integrity

- Every release includes a `SHA256SUMS` file listing the SHA-256 of every archive in the release.
- Both installer scripts MUST download `SHA256SUMS` and verify the archive's checksum before extracting or executing any code from the archive. A checksum mismatch aborts the installation with a non-zero exit status and leaves the target filesystem unmodified.
- Signatures (GPG, cosign, minisign), SBOM generation, and reproducible-build guarantees are explicitly out of scope for v1. These belong in a dedicated supply-chain-hardening spec if later required.

---

### 9. Install UX

#### 9.1 First-class install commands

**POSIX (macOS, Linux):**

```
curl -fsSL <install.sh URL> | sh
```

**Windows (PowerShell):**

```
irm <install.ps1 URL> | iex
```

Both commands are prominently documented in the repository `README.md` and in the release notes.

#### 9.2 Behaviour required of both installers

1. Detect the host operating system and CPU architecture. Fail with a clear message listing supported targets if the host is not tier-1 (or tier-2, if a tier-2 artifact was published for the selected version).
2. Resolve the version to install:
   - If the user supplied a version, use it verbatim.
   - Otherwise, query the release channel for the latest applicable version — the latest stable by default, or the latest including pre-releases if the user opted in (§9.4).
3. Download the correct archive for the resolved `(version, target)` pair.
4. Download `SHA256SUMS`, compute the local hash of the downloaded archive, and compare. Abort on mismatch without touching the install directory.
5. Extract the binary into the install directory, creating the directory if necessary.
6. Ensure the install directory is on the user's `PATH` (see §9.3).
7. Print the installed version, the install directory, and — if PATH was mutated — a reminder to open a new shell.

#### 9.3 Install directory and PATH handling

**Install directory (per-user, never requires sudo/UAC):**

- Unix: `~/.local/bin` (XDG convention).
- Windows: `%LOCALAPPDATA%\Programs\inat\bin`.

**Overridable** via the environment variable `INAT_INSTALL_DIR`.

**PATH mutation:**

- If the install directory is already on `PATH`, the installer makes no changes to any shell profile or environment variable.
- Otherwise, the installer appends the install directory to the user's `PATH` by modifying the user's shell profile (Unix: the appropriate profile file for `$SHELL` — `.bashrc`, `.zshrc`, or fish config) or the user-scope `PATH` environment variable (Windows).
- The installer must print exactly which file or environment variable it modified.
- A `--no-modify-path` flag disables step 7 and prints the PATH snippet the user should add manually.

#### 9.4 Installer flag surface (v1)

In scope:

- `--version <v>` — install a specific release instead of the latest.
- `--pre-release` — include pre-release tags when resolving "latest". Without this flag, pre-releases are invisible to the installer.
- `--no-modify-path` — skip automatic PATH mutation.
- `--install-dir <dir>` — override the install directory (alternative to `INAT_INSTALL_DIR`).

The installer is idempotent: running it a second time over an existing install performs a clean upgrade or reinstall without duplicating PATH entries or leaving stale files.

#### 9.5 Uninstall

Companion scripts `uninstall.sh` and `uninstall.ps1` are published alongside the install scripts. They:

1. Remove the `inat` binary from the install directory.
2. Remove any PATH entry added by the installer.
3. Leave user configuration and data (the `.inat/` directories in projects, the global `~/.inat/`) untouched — removing user data is not the uninstaller's job.

#### 9.6 Error contract

The installer must exit with a non-zero status and a clear human-readable message in each of these cases, and must not leave a partial install on disk:

| Condition | Behaviour |
|---|---|
| Unsupported OS or architecture | Print supported targets list, exit non-zero. |
| Checksum mismatch | Print expected vs actual hash, do not extract, exit non-zero. |
| Transient network failure | Retry once; if it still fails, print the failing URL and exit non-zero. |
| Requested version not found | Print the tag that was requested and the location queried, exit non-zero. |

---

### 10. Release notes and changelog

- A `CHANGELOG.md` file at the repository root follows the Keep-a-Changelog format.
- The release pipeline fails if a tag `vX.Y.Z` is pushed without a corresponding `## [X.Y.Z]` section in `CHANGELOG.md`.
- Release notes attached to the published release are the extracted text of that section, unmodified.
- There is no auto-generation from commit messages; the human-written section is canonical.

---

### 11. Auth and network assumption

The spec places one requirement on the hosting chosen by the implementation:

> The installer scripts and release archives MUST be fetchable over HTTPS without an interactive login, given that the user is on the corporate network (or, when the tool is later made public, from any network).

The spec does not prescribe how. The implementation must validate, on a real host, that this property holds. If it does not — i.e. the only available hosting requires per-user authentication — the implementer chooses between (a) moving to a different host that allows anonymous reads, or (b) falling back to an environment-variable-based authentication path in the installer (`INAT_TOKEN` or equivalent) and documenting the token-provisioning step in the README. Interactive browser login as part of `curl | sh` is not acceptable.

---

### 12. Explicitly out of scope for v1

Enumerated so the scope of v1 is unambiguous. Each item below may become the subject of its own future spec.

- A `inat self update` subcommand. → implemented in [spec 0015](0015-self-update.md).
- A nightly release channel.
- Offline / air-gapped install mode (installing from a pre-downloaded archive).
- Package-manager distribution channels: Homebrew tap, Scoop bucket, winget manifest, MSI, `.deb`, `.rpm`, Snap, Flatpak.
- Code signing beyond SHA-256 checksums: GPG, cosign, minisign.
- SBOM publication.
- Reproducible-build guarantees.
- Windows-on-ARM, Linux musl/static, FreeBSD, 32-bit targets.
- MSRV testing, beta/nightly Rust toolchain CI jobs.

---

### 13. Acceptance

The spec is satisfied when all of the following can be demonstrated end to end:

1. A brand-new engineer on a stock laptop runs the documented install one-liner from the repository `README.md` and, within sixty seconds, has a working `inat --version` on `PATH` — without opening a browser, without configuring credentials interactively, and without running a privileged command.
2. A deliberately tampered archive published to the release channel causes the installer to abort with a clear error before any file is extracted.
3. A merge request that fails lint or a platform test on any tier-1 target for which CI enforcement is active (see §3) cannot be merged.
4. A `v*.*.*` tag pushed without a corresponding `CHANGELOG.md` section or with a mismatched `Cargo.toml` version fails before any build work starts.
