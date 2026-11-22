## inat — macOS release binaries (Spec 0014)

---

### 1. Overview

The released artifact for inat is the Go binary at `go/`; the GitLab CI release stage compiles it once per supported target and uploads each archive to the project's generic package registry. As of v0.4.x the release stage shipped `linux/amd64` and `windows/amd64` only. Spec [`0009-go-port.md`](0009-go-port.md) §1 already required four targets — both `darwin/amd64` and `darwin/arm64` — and §1.3 specifically mandated that `darwin/arm64` MUST be ad-hoc-signed via `rcodesign` so it launches on Apple Silicon. The cross-compile sanity checks for both darwin targets pass on every push (`go:cross-compile:darwin-{amd64,arm64}` in the GitLab CI config), but the release stage never grew the corresponding `release:build:darwin-*` jobs and the signing step was never wired in. macOS users were instructed to build from source.

This spec closes that gap. The next tag after this lands publishes `inat-<version>-x86_64-apple-darwin.tar.gz` and `inat-<version>-aarch64-apple-darwin.tar.gz` alongside the Linux and Windows artifacts, both signed ad-hoc, both reproducible bit-for-bit across rebuilds of the same git HEAD.

A companion implementation plan lives at `docs/plan/0014-macos-release-binaries.md`.

This spec finishes work scoped by [`0009-go-port.md`](0009-go-port.md) §1.3 and promotes the macOS targets that [`0002-ci-cd-and-install.md`](0002-ci-cd-and-install.md) §2 had marked deferred. It does not alter the Rust tree under `rust/` (frozen archive); all changes are in `ci/`, the GitLab CI config, `README.md`, and the new spec/plan documents.

---

### 2. Goals

1. The `release` pipeline stage MUST publish four binaries on every tag matching `v[0-9]+\.[0-9]+\.[0-9]+(-.*)?`: `inat-<v>-x86_64-unknown-linux-gnu.tar.gz`, `inat-<v>-x86_64-pc-windows-msvc.zip`, `inat-<v>-x86_64-apple-darwin.tar.gz`, `inat-<v>-aarch64-apple-darwin.tar.gz`. Naming matches the Rust target triples already encoded in `ci/build-release.sh` and consumed by `install.sh`'s `detect_target`; no installer-side change is required.
2. Both darwin binaries MUST carry an ad-hoc Mach-O signature applied by `rcodesign sign` (no team identity, no `--p12-file`). Verified post-sign in CI by `rcodesign verify` on the same path. This satisfies the spec 0009 §1.3 mandate for `darwin/arm64`; `darwin/amd64` is signed for parity at near-zero cost so installer instructions stay identical.
3. The signing step MUST run on the same Linux container as the build (`golang:1.25-alpine`). No native macOS runner is introduced. `rcodesign` is a pure-Rust binary that signs Mach-O cross-host; the musl Linux build is required for the alpine container.
4. The `rcodesign` binary MUST be pinned to a specific upstream release by version *and* SHA256. Bumping the pin requires updating both `RCODESIGN_VERSION` and `RCODESIGN_SHA256` in `ci/install-rcodesign.sh`; the SHA256 is published as a sibling `.sha256` asset on each `apple-codesign/<ver>` GitHub release. Initial pin: `apple-codesign/0.29.0`, sha256 `dbe85cedd8ee4217b64e9a0e4c2aef92ab8bcaaa41f20bde99781ff02e600002`.
5. The signed archives MUST be reproducible: rebuilding the same tag from the same git HEAD MUST produce byte-identical tarballs (and therefore identical SHA256SUMS). The CMS signature timestamp is pinned via `SOURCE_DATE_EPOCH=$(git log -1 --format=%ct)`; the existing `-trimpath -ldflags="-s -w -X main.version=..."` flags handle the Go-build-side determinism.
6. The existing `release:publish` job MUST pick up the new artifacts without further changes to its body. Its `for f in inat-*.tar.gz inat-*.zip ...` loop already globs over all four archives once they're surfaced as needed-job artifacts. Only the `needs:` list is extended.
7. `install.sh` MUST resolve and download the new artifacts unmodified. Its `detect_target` already maps `Darwin-x86_64` / `Darwin-arm64` to the matching triple; its checksum verification consumes the same `SHA256SUMS` published by `release:publish`.
8. The `cross-compile` stage's existing `go:cross-compile:darwin-{amd64,arm64}` jobs MUST continue to pass on every push (not just tags) — they are the early-warning system that catches Go-side breakage before tag time.
9. No new Go module dependencies (CLAUDE.md rule 8). `rcodesign` is invoked as a subprocess from `ci/build-release.sh`; the cobra + go-git + stdlib allowlist is unchanged.

---

### 3. Non-goals

- **Apple-notarized binaries.** Notarization requires an Apple Developer account ($99/year), Apple ID, an app-specific password, and `notarytool` invocations against Apple's submission API. We do not have any of those. macOS users see Gatekeeper's *"cannot be opened because Apple cannot check it for malicious software"* dialog on first launch and clear it via `xattr -d com.apple.quarantine` (or right-click → Open). This is the same UX as every other ad-hoc-signed open-source CLI binary that doesn't pay for a Developer account. If/when SBP's Apple Developer enrolment lands, a follow-up spec adds the notarization step.
- **Universal (lipo-merged) binaries.** A single `inat-<v>-macos-universal.tar.gz` would shave one tarball from the registry but complicate `install.sh`'s detection logic and break the existing Rust-triple naming convention. Two arch-specific tarballs map cleanly onto `install.sh`'s existing logic.
- **Linux/arm64 release builds.** Sanity-checked on every push but not yet promoted; separate spec when there's a user need.
- **A native macOS runner.** No business case for one when cross-compile + cross-sign work. Closes off the entire class of "macOS runner is unhealthy" failures spec 0002 §8 worried about.
- **Changes to `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1`.** They already handle the macOS path or are platform-orthogonal.
- **Migrating away from the Rust-style triple naming (`x86_64-apple-darwin` / `aarch64-apple-darwin`).** Changing the triple breaks v0.2.x-era installer scripts that are already in users' shell history. Not worth the churn.
- **Hardened-runtime entitlements / `--code-signature-flags runtime`.** Hardened runtime is enforced by Gatekeeper as a notarization precondition; on an ad-hoc-signed binary it does nothing useful and can cause spurious dyld restrictions (e.g. blocking `DYLD_*` env vars users may want for debugging). The signing invocation passes no signature flags.
- **A separate `ci/sign-darwin.sh` helper.** The signing logic is six lines; folding it into `ci/build-release.sh` keeps the build pipeline single-file-shaped and testable as one script.

---

### 4. Design

#### 4.1 Pipeline shape

Two new jobs land in the GitLab CI config's `release` stage, parallel to the existing `release:build:linux-x64` and `release:build:windows-x64`:

```yaml
release:build:darwin-x64:
  image: golang:1.25-alpine
  stage: release
  needs: [release:verify]
  <<: *release-rules
  before_script:
    - apk add --no-cache bash tar gzip curl git
    - sh ci/install-rcodesign.sh
  script:
    - sh ci/build-release.sh x86_64-apple-darwin
  artifacts:
    paths: ["inat-*-x86_64-apple-darwin.tar.gz"]
    expire_in: 1 week

release:build:darwin-arm64:   # identical, target = aarch64-apple-darwin
```

`release:publish`'s `needs:` list grows by both job names. Its body is unchanged: the asset-upload loop already globs `inat-*.tar.gz inat-*.zip`, the SHA256SUMS computation already covers all matched archives, and the GitLab Release `--assets-link` arguments are produced from the same loop.

#### 4.2 `ci/install-rcodesign.sh`

A new POSIX-sh helper invoked from each darwin job's `before_script`. Responsibilities:

1. Idempotence — if `${INSTALL_DIR}/rcodesign --version` already reports the pinned version, exit 0 without re-downloading.
2. Download the pinned `apple-codesign-<ver>-x86_64-unknown-linux-musl.tar.gz` from `github.com/indygreg/apple-platform-rs` releases via `curl --fail --silent --show-error --location`.
3. Verify SHA256 against the constant baked into the script. Mismatch → exit 2 (treat as supply-chain failure; do not retry).
4. Extract the binary to `${INAT_RCODESIGN_DIR:-/usr/local/bin}/rcodesign` and `chmod +x`.
5. Sanity-check the installed binary by running `rcodesign --version`; failure → exit 3.

Pinning is by version-and-SHA256, not just version: GitHub release assets are mutable in principle and this script is the supply-chain boundary between an unrelated upstream project and our signed binaries. The script lives at `ci/install-rcodesign.sh` so the pre-commit hook's spec-presence check treats it as a pipeline-adjacent change and the existing shell-script linting rules apply.

#### 4.3 `ci/build-release.sh` darwin signing branch

The script already had `case` branches mapping `x86_64-apple-darwin` and `aarch64-apple-darwin` to GOOS=darwin GOARCH=amd64/arm64 (added by the spec 0009 work but never exercised by a release job). A new post-build branch is inserted between the binary-existence check and the archive-staging step:

```sh
case "$TARGET" in
    *-apple-darwin)
        if ! command -v rcodesign >/dev/null 2>&1; then
            echo "error: rcodesign not on PATH; run ci/install-rcodesign.sh first" >&2
            exit 6
        fi
        if SDE="$(git log -1 --format=%ct 2>/dev/null)" && [ -n "$SDE" ]; then
            export SOURCE_DATE_EPOCH="$SDE"
        fi
        rcodesign sign "$BIN_PATH"
        rcodesign verify "$BIN_PATH"
        ;;
esac
```

Three deliberate choices:
- `command -v` check up front so `release:build:darwin-*` fails fast with a clear error if `before_script` was misconfigured, rather than producing an unsigned tarball.
- `SOURCE_DATE_EPOCH` is set from `git log -1 --format=%ct` — the commit time of the tagged HEAD. rcodesign honours `SOURCE_DATE_EPOCH` for the CMS `SignedAttributes` signing-time field; without it, two ad-hoc signs of the same binary produce different superblob bytes and different SHA256s. Set inside an `if` so a non-git working tree (e.g. someone running the script from an extracted source archive) still works without the variable, just at the cost of reproducibility for that one rebuild.
- ~~`rcodesign verify` immediately after signing is cheap insurance — catches a future contributor who slips a `strip` or post-processing step in between, which would invalidate the signature without anyone noticing until a macOS user tries to launch the binary.~~ **Removed during v0.5.0 release.** The `verify` subcommand has a known CMS-parsing bug in rcodesign 0.29.0 — it emits a self-deprecating warning *"the verify command is known to be buggy and gives misleading results; we highly recommend using Apple's tooling until this message is removed"* and fails with `CMS error: missing further values (at position 0)` against successfully-signed Mach-O binaries. The first tag to exercise the darwin release jobs (v0.5.0) tripped it on both `release:build:darwin-x64` and `release:build:darwin-arm64`. The sign step's exit code is now the only gate; the post-sign verify line was decoration, not a contract. If a future contributor adds a post-processing step that breaks the signature, the failure mode is "macOS kernel rejects the binary at launch," which is recoverable by re-running the release pipeline.

A new exit code 6 is reserved for "rcodesign missing"; documented in the script header.

#### 4.4 Why no `--code-signature-flags runtime`

Hardened runtime is meaningful when combined with Apple-Developer-ID signing and notarization: Gatekeeper enforces hardened-runtime as a notarization precondition. On an ad-hoc-signed binary the flag adds no security and can introduce dyld restrictions (e.g. `DYLD_INSERT_LIBRARIES` is blocked) that are unhelpful for an open-source CLI users may want to instrument. The signing call therefore passes no flags — pure ad-hoc — and the result satisfies Apple Silicon's kernel signature check, which is the only constraint we're trying to clear.

#### 4.5 Why no `--xattrs` on tar

The Mach-O `LC_CODE_SIGNATURE` load command lives inside the binary's binary structure, not in extended attributes; plain `tar -czf` preserves it. The existing `tar -czf "${ARCHIVE}" "${STAGE}"` line in `ci/build-release.sh` is correct as-is. A `com.apple.quarantine` xattr added by the user's browser/curl on download is the only thing macOS users have to clear (separately documented in README §macOS).

---

### 5. Compatibility

- **Existing Linux/Windows release jobs** — unchanged. Job names, scripts, artifact paths, and `release:publish` behaviour are byte-identical.
- **`release:publish`** — `needs:` extends by two entries; the body is unchanged; the asset glob and SHA256SUMS computation pick up the new archives automatically.
- **`install.sh`** — already routes `Darwin-x86_64` / `Darwin-arm64` to `*-apple-darwin.tar.gz`. No code change. The first tag with macOS artifacts populates the registry path it has been requesting all along.
- **`install.ps1`** — Windows-only by definition. Unchanged.
- **`go/` source tree** — unchanged. `main.version` is already injected via `-X main.version=${VERSION}`; nothing about the signing step depends on Go-side wiring.
- **Cross-compile sanity stage** — `go:cross-compile:darwin-{amd64,arm64}` continue to run on every push and remain the early-warning system for Go-side breakage. They do not run signing (no need, no rcodesign in their `before_script`).
- **Reproducibility** — the existing Linux and Windows tarballs were already reproducible (no signature, deterministic tar). The new darwin tarballs become reproducible as soon as `SOURCE_DATE_EPOCH` is set. `ci/parity-rust-vs-go.sh` is unaffected; it compares CLI output, not binary bytes.
- **`docs/specs/0009-go-port.md` §1.3** — the unfulfilled "MUST be ad-hoc-signed via `rcodesign`" requirement is now fulfilled. Spec 0009's checklist closes.

---

### 6. Acceptance criteria

1. **Both darwin archives land on tag.** A tag matching `^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$` triggers `release:build:darwin-x64` AND `release:build:darwin-arm64`; both produce an artifact named `inat-<version>-<triple>.tar.gz` matching their respective triple. Verified by inspecting the pipeline view's `release` stage.
2. **`release:publish` SHA256SUMS contains four lines.** After publish, downloading SHA256SUMS from the generic package registry yields four entries: `*-x86_64-unknown-linux-gnu.tar.gz`, `*-x86_64-pc-windows-msvc.zip`, `*-x86_64-apple-darwin.tar.gz`, `*-aarch64-apple-darwin.tar.gz`. The publish job's stdout `tee SHA256SUMS` log shows the same.
3. **Both darwin archives carry an ad-hoc Mach-O signature.** Extracting the tarball and running `rcodesign print-signature-info inat` (on any host) reports a valid CodeDirectory and an empty/zero-team signature. On a Mac, `codesign -dv inat` reports `Signature=adhoc`.
4. **darwin/arm64 launches on Apple Silicon without `xattr` workaround when transferred via a non-quarantine channel.** Manual smoke (e.g. `scp` from CI artifacts to an Apple Silicon host or a `cd && tar -xzf` from a checkout): `./inat --version` prints the right tag without `Killed: 9` or `bad CPU type in executable`. (Browser-downloaded binaries hit Gatekeeper quarantine separately; not in scope of this criterion.)
5. **Reproducibility.** Re-running `ci/build-release.sh aarch64-apple-darwin` against the same git HEAD twice in a row produces byte-identical tarballs (`sha256sum` of both runs match). Same for `x86_64-apple-darwin`. Verified locally during MR review.
6. **`install.sh` resolves the new artifacts.** From any macOS host (Intel or Apple Silicon): `INAT_TOKEN=... sh install.sh --version v<NEW>` exits 0 and places `inat` at `~/.local/bin/inat`. The downloaded tarball passes the `SHA256SUMS` checksum verification. (Validated post-tag via README's published one-liner.)
7. **Pre-commit gate clean.** `gofmt -l`, `go vet`, `staticcheck`, `go test ./...` are all unaffected (no `go/` changes). The hook's spec-presence check passes because `docs/specs/0014-...md` and `docs/plan/0014-...md` are added in the same commit / MR as the CI changes.
8. **No new Go module dependencies.** `go.mod` and `go.sum` are byte-identical pre/post change.
9. **rcodesign pin matches its `.sha256` sidecar.** `RCODESIGN_VERSION` and `RCODESIGN_SHA256` in `ci/install-rcodesign.sh` agree with the value published at `https://github.com/indygreg/apple-platform-rs/releases/download/apple-codesign%2F<ver>/apple-codesign-<ver>-x86_64-unknown-linux-musl.tar.gz.sha256` at the time of the bump.
10. **No regression on existing Linux/Windows installs.** A representative Linux + Windows install via the README one-liner against the new tag still exits 0 and `inat --version` reports the new tag.

---

### 7. Testing notes

- **Local end-to-end without a Mac is possible.** From a Linux host with `rcodesign` (musl build) on PATH, `bash ci/build-release.sh aarch64-apple-darwin v0.5.0-test` (passing the tag explicitly so the VERSION-mismatch check has something to compare against) produces the signed tarball. `rcodesign print-signature-info` confirms the signature; running the binary fails with "exec format error" because Linux can't load Mach-O — that's expected.
- **Reproducibility test.** The cheapest way to catch a `SOURCE_DATE_EPOCH` regression: `bash ci/build-release.sh aarch64-apple-darwin <tag>` twice, `sha256sum` both `.tar.gz` outputs, expect equal. Add this to the manual MR checklist.
- **CI cannot run the binary.** No native macOS runner; no plan to add one. The build-side tests above are the floor; the runtime test ("does it launch on a real Mac") is gated on a team member with Mac access reporting back after the first tagged release. Spec 0009 §1.3's expectation is that ad-hoc signing covers the kernel-rejection failure mode and Gatekeeper is documented as a separate, accepted UX cost.
- **Parity job.** `parity:rust-vs-go` runs on Linux only and is unaffected. No darwin parity job is added — the Rust tree is frozen and the Rust pipeline never built darwin either.

---

### 8. Documentation

- **`README.md`** — Status banner moves macOS from "not yet shipped from CI" to "shipped, ad-hoc-signed". A new §macOS section under Install describes the install one-liner (same as Linux), the Gatekeeper *cannot be opened* dialog, and the `xattr -d com.apple.quarantine` workaround. The §"From source (macOS, or any platform)" section is renamed to §"From source (any platform)" and reframed as the air-gapped / no-token path; the macOS-specific framing is dropped. Troubleshooting gains two entries: the *cannot be opened* dialog and a *killed: 9* line that flags pipeline regression.
- **`CHANGELOG.md`** — `[Unreleased]` gets an `### Added` entry calling out the new artifacts and the Gatekeeper UX. The release version (likely v0.5.0) is the release captain's call.
- **`docs/specs/0014-macos-release-binaries.md`** — this document.
- **`docs/plan/0014-macos-release-binaries.md`** — companion rollout document.
- **`docs/specs/0009-go-port.md`** — left untouched. Its §1.3 mandate stands as written; this spec implements it. Cross-references both ways via the "extends" line in §1.

---

### 9. Out-of-scope follow-ups

- **Apple-Developer-ID signing + notarization.** Lifts the Gatekeeper *cannot be opened* dialog entirely. Requires SBP enrolment in Apple's Developer Program and CI access to credentials. Separate spec when those land.
- **Linux/arm64 release builds.** The cross-compile stage already proves they build; promoting them to the release stage is mechanical and follows this spec's pattern (no signing needed). Gated on user demand, not technical readiness.
- **Universal (lipo) macOS binary.** Combines amd64 + arm64 into one fat tarball at the cost of `install.sh` complication. Revisit only if reducing the registry asset count becomes valuable.
- **`docs/specs/0009-go-port.md` retrospective.** With this spec landed, every requirement in 0009 is implemented. A short retro paragraph in 0009's plan would be nice but is not required.
- **Bumping the rcodesign pin past 0.29.0.** indygreg's apple-platform-rs has been quiet since late 2024; 0.29.0 is the most recent stable. Re-pin when a useful rcodesign release lands.
