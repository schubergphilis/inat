## inat — macOS release binaries (Plan 0014)

Pairs with [`docs/specs/0014-macos-release-binaries.md`](../specs/0014-macos-release-binaries.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, and verification.

Closes the `darwin/arm64` ad-hoc-signing requirement that landed in [`docs/specs/0009-go-port.md`](../specs/0009-go-port.md) §1.3 but never made it to the release pipeline. Also promotes the macOS targets that [`docs/specs/0002-ci-cd-and-install.md`](../specs/0002-ci-cd-and-install.md) §2 marked "deferred — promotion is config-only when a runner / signing approach is available". Both halves now satisfied: Linux container + cross-host `rcodesign` removes the runner blocker; ad-hoc signing without notarization is the agreed-cost-not-blocker decision.

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Build platform for darwin | The same `golang:1.25-alpine` Linux container the existing Linux/Windows release jobs use. CGO=0 + Go's cross-compile means no Mac toolchain is needed. No native macOS runner is introduced. |
| Signing tool | `rcodesign` (Rust) from `github.com/indygreg/apple-platform-rs`. Pure Rust → can sign Mach-O cross-host. Already named in spec 0009 §4. |
| Signing scope | Ad-hoc sign **both** `darwin/amd64` and `darwin/arm64`. arm64 strictly requires it (kernel signature check); amd64 is signed for parity at near-zero cost so installer instructions stay identical and there's no class of "Intel binary surprise" failure. |
| Signing flags | None. No `--code-signature-flags runtime`, no `--p12-file`, no team identity. Hardened-runtime is meaningful only for notarized Developer-ID binaries; on ad-hoc it just adds dyld restrictions. |
| Notarization | Out of scope. No Apple Developer account; users hit Gatekeeper's *"cannot be opened"* dialog on first launch and clear via `xattr -d com.apple.quarantine`. README documents this. |
| rcodesign distribution | Pinned to `apple-codesign/0.29.0`, sha256 `dbe85cedd8ee4217b64e9a0e4c2aef92ab8bcaaa41f20bde99781ff02e600002`, **musl** build (alpine container is musl-based; the gnu build would fail to load against musl ld.so). |
| rcodesign install location | `/usr/local/bin/rcodesign` (overridable via `INAT_RCODESIGN_DIR` for local testing without root). New helper `ci/install-rcodesign.sh` handles download + sha256 verify + extract + sanity check; idempotent. |
| Reproducibility | `SOURCE_DATE_EPOCH=$(git log -1 --format=%ct)` set inside the build script's darwin branch before `rcodesign sign`, so the CMS signing-time field is deterministic. Combined with the existing `-trimpath -ldflags="-s -w -X main.version=..."` Go flags, archives are byte-identical across rebuilds of the same tag. |
| Filename / triple | Keep Rust-style triples (`x86_64-apple-darwin`, `aarch64-apple-darwin`). Already encoded in `ci/build-release.sh` and `install.sh`'s `detect_target`; matching them = zero installer-side work. |
| `release:publish` body | Unchanged. The asset upload loop already globs `inat-*.tar.gz inat-*.zip` and SHA256SUMS is computed from the same glob. Only `needs:` extends. |
| `install.sh` / `install.ps1` changes | None. install.sh's `Darwin-x86_64` / `Darwin-arm64` cases already construct the right `*-apple-darwin.tar.gz` URL; install.ps1 is Windows-only by definition. The first tag with macOS artifacts populates the registry path install.sh has been requesting all along. |
| Where the signing logic lives | Inside `ci/build-release.sh`'s existing `case "$TARGET"` branch arm. ~15 lines added. Extracting to a separate `ci/sign-darwin.sh` would split a build script into two files for no clarity gain. |
| Versioning | Adding a platform is a feature; the next release is the release captain's call but is most naturally `v0.5.0`. CHANGELOG `[Unreleased]` gets the entry; the bump itself is not part of this MR. |

---

### 2. Files

#### New
- `ci/install-rcodesign.sh` — POSIX-sh helper invoked from `release:build:darwin-*`'s `before_script`. Downloads pinned rcodesign musl tarball, sha256-verifies, extracts to `/usr/local/bin`. Idempotent. Documented exit codes (0 OK, 1 download failure, 2 sha256 mismatch, 3 sanity-check failure).
- `docs/specs/0014-macos-release-binaries.md` — the contract.
- `docs/plan/0014-macos-release-binaries.md` — this document.

#### Modified
- GitLab CI config:
  - Adds two release jobs `release:build:darwin-x64` and `release:build:darwin-arm64` immediately after `release:build:windows-x64`, mirroring the windows job's shape (golang:1.25-alpine image, `release:verify` need, release-rules anchor) but with a `before_script` that also installs rcodesign.
  - Extends `release:publish.needs:` by both new job names.
  - Updates the "Pipeline shape" comment block at the top of the release section to list all four build jobs.
- `ci/build-release.sh`:
  - Adds an exit-code-6 line to the header comment block ("rcodesign missing").
  - Adds a `case "$TARGET" in *-apple-darwin)` block between the binary-existence check and the archive-staging step. Inside: `command -v rcodesign` guard (exit 6 if missing), `SOURCE_DATE_EPOCH` export from `git log -1 --format=%ct` (skipped silently outside a git tree), `rcodesign sign $BIN_PATH`, `rcodesign verify $BIN_PATH`. Net add ~15 lines.
- `README.md`:
  - Status banner: replace "macOS binaries can be cross-compiled trivially but are not yet shipped from CI" with the shipped story (ad-hoc-signed, Gatekeeper note, link to new §macOS).
  - Insert §macOS between §Windows and §Common flags. Describes that `install.sh` is the same one-liner as Linux, then walks through the *"cannot be opened"* dialog and the `xattr -d com.apple.quarantine` workaround.
  - Rename §"From source (macOS, or any platform)" to §"From source (any platform)" and rewrite the leading paragraph to drop the macOS-specific framing.
  - Troubleshooting: replace the "macOS → no binary yet" line with two new lines — one for the *cannot be opened* Gatekeeper dialog, one for the *killed: 9* signal that would indicate a missing signature (pipeline regression).
- `CHANGELOG.md`:
  - `[Unreleased]` gets an `### Added` entry: "macOS release binaries (Intel + Apple Silicon, ad-hoc-signed via rcodesign). Spec / plan 0014. Closes the spec 0009 §1.3 darwin/arm64 signing requirement." Plus a paragraph noting the Gatekeeper first-launch UX and the `xattr -d com.apple.quarantine` workaround.
  - Version bump to `[0.5.0] - <date>` is **not** part of this MR; the release captain stamps it when cutting the tag.

#### Untouched
- `go/**` — no Go-source changes. The version injection (`-X main.version=...`) was already wired and works identically for darwin.
- `install.sh` — already detects darwin targets and constructs the right URL.
- `install.ps1`, `uninstall.sh`, `uninstall.ps1` — Windows-only or platform-orthogonal.
- `ci/verify-release-prereqs.sh` — gates on VERSION ↔ tag ↔ CHANGELOG agreement; platform-independent.
- `ci/parity-rust-vs-go.sh` — Linux-only; unchanged.
- `ci/build-release.ps1` — Windows-only (used for local dev on Windows); unchanged.
- `go.mod`, `go.sum` — no new dependencies.
- The `.rust-backup` copy of the GitLab CI config — preserved verbatim per spec 0009 §3; Rust pipeline never had macOS and that historical record stays accurate.

---

### 3. Implementation order

1. **Land `ci/install-rcodesign.sh`** — self-contained, testable in isolation. Verify locally on a Linux box: `INAT_RCODESIGN_DIR=/tmp/rc sh ci/install-rcodesign.sh && /tmp/rc/rcodesign --version`.
2. **Update `ci/build-release.sh`** — add the darwin signing branch + exit-code-6 doc. Verify locally on a Linux box with rcodesign on PATH: `bash ci/build-release.sh aarch64-apple-darwin v$(cat VERSION)` produces a signed tarball; `rcodesign print-signature-info` against the extracted binary reports a CodeDirectory.
3. **Update the GitLab CI config** — two new jobs + extended `release:publish.needs:`. Push to a branch; the existing `cross-compile` stage continues to pass on every push (the new release jobs only fire on tags, so the MR doesn't exercise them, which is expected).
4. **Update `README.md`** — banner + §macOS + §"From source" rename + troubleshooting.
5. **Add spec 0014 + plan 0014** — pre-commit hook's spec-presence check requires either a spec or plan touch alongside `go/` changes. This MR doesn't touch `go/`, so the check is satisfied trivially, but shipping the pair is the convention per CLAUDE.md § Spec & Plan and reviewers expect it.
6. **Add CHANGELOG `[Unreleased]` entry** — describes new artifacts + Gatekeeper UX. No version stamp yet.
7. **Open MR.** Reviewers exercise the local-reproducibility check (step 2 twice with `sha256sum`) as part of review.
8. **After merge, release captain cuts a tag** (likely `v0.5.0`). Pipeline produces four archives + SHA256SUMS. Manual smoke from a Mac (or community report) closes acceptance criterion 4 + 6.

---

### 4. Verification

#### Local (no Mac required)

1. `INAT_RCODESIGN_DIR=/tmp/rc sh ci/install-rcodesign.sh` exits 0; `/tmp/rc/rcodesign --version` reports `0.29.0`. Re-running the same command exits 0 instantly (idempotence).
2. With `rcodesign` on PATH and a `VERSION` matching the test tag: `bash ci/build-release.sh aarch64-apple-darwin v$(cat VERSION)` produces `inat-<v>-aarch64-apple-darwin.tar.gz`. Extract: `tar tzf` lists `inat-<v>-aarch64-apple-darwin/{inat,LICENSE,README}`.
3. Same step for `x86_64-apple-darwin`.
4. Run `rcodesign print-signature-info` against each extracted `inat`: reports a valid CodeDirectory, empty team, ad-hoc signature.
5. Re-run step 2 (and 3) immediately. The two runs against the same git HEAD produce byte-identical tarballs (`sha256sum` of both runs match). This is the reproducibility check.

#### CI (per-tag smoke)

6. Push a tag (release captain). Pipeline view shows `release:build:darwin-x64` and `release:build:darwin-arm64` succeeding; `release:publish` succeeds with four archives in its `tee SHA256SUMS` output.
7. Pull SHA256SUMS from the published `latest`-or-versioned package URL: confirm four lines, four matching archive entries.
8. Download one of the darwin tarballs from the registry; locally `rcodesign print-signature-info` against the extracted `inat` reports the same signature shape as step 4.

#### End-to-end (post-tag, depends on Mac access)

9. From a macOS host (Intel or Apple Silicon), the README §Linux one-liner (which install.sh implements identically across POSIX hosts) installs the binary. First `inat --version` invocation triggers Gatekeeper's *cannot be opened* dialog; running `xattr -d com.apple.quarantine "$(command -v inat)"` once clears it; subsequent `inat --version` runs print the tag. — This step is gated on a team member with Mac access (or a community report); it is **not** a CI gate.
10. From the same host, `inat sources add` against the existing `inat-kits` source completes; `inat install` against a kit lands files. End-to-end functional parity with Linux/Windows.

---

### 5. Risks & mitigations

- **rcodesign upstream stagnation.** indygreg's apple-platform-rs has had no release since late 2024. Risk: a Go or Apple change someday breaks 0.29.0 and we have no maintained upstream to patch from. Mitigation: monitor; if it bites, vendor a forked binary into a SBP-internal release. Cost: low until it happens.
- **GitHub release-asset URL changes.** The pin URL pattern is documented at `apple-codesign%2F<ver>/apple-codesign-<ver>-<triple>.tar.gz`. If indygreg restructures release naming, the install script breaks. Mitigation: pin SHA256 means a silent substitution would be caught; the failure mode is "build fails, fix is to bump the pin". Acceptable.
- **rcodesign verify divergence between sign and verify.** Possible if a future contributor reorders the build pipeline (e.g. compresses before signing, or adds a `strip`). Mitigation: `rcodesign verify` runs immediately after signing inside the same script, in CI on every release build.
- **Ad-hoc signature insufficient for some macOS update.** Apple has tightened the launch path for Apple Silicon over the years; an ad-hoc signature has been sufficient since arm64 macOS shipped. Risk that a future macOS release changes this. Mitigation: spec 0009 §3 already accepts that notarization may eventually become mandatory; the response is a separate notarization spec, not a defensive change here.
- **Non-git source build (rare).** A user running `bash ci/build-release.sh` from an extracted source archive (no `.git/`) won't have `git log` available; the `if SDE=...; then` guard means the script proceeds without `SOURCE_DATE_EPOCH` and the resulting tarball is non-deterministic across rebuilds. The CI path always has the git tree (CI checks out from origin), so this only affects ad-hoc local testing. Acceptable.

---

### 6. Out-of-scope follow-ups

- **Apple-Developer-ID signing + notarization.** Lifts the Gatekeeper *cannot be opened* dialog entirely. Separate spec when SBP's Apple Developer enrolment lands.
- **Linux/arm64 release.** The cross-compile sanity job already proves it builds; promoting it to a release job follows this spec's pattern, minus the signing step. Open when there's a user.
- **Universal (lipo) macOS binary.** Halves the macOS asset count at the cost of `install.sh` complication. Revisit only if asset proliferation becomes an issue.
- **rcodesign pin auto-bump.** A small `ci/check-rcodesign-pin.sh` that compares the pinned hash against the current `apple-codesign/<latest>` sidecar would flag stale pins. Speculative; not worth adding until the upstream actually starts releasing again.
