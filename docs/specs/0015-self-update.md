## inat — Self-update subcommand (Spec 0015)

---

### 1. Overview

Today, upgrading the `inat` binary is a manual operation: the user re-runs the README install one-liner (POSIX `install.sh` or PowerShell decode-then-scriptblock for Windows). [`docs/specs/0002-ci-cd-and-install.md`](0002-ci-cd-and-install.md) §10 explicitly listed `inat self update` as v1 out-of-scope; the same install/update infrastructure has now been live for four releases, all four release targets ship from the same Linux container ([`docs/specs/0014-macos-release-binaries.md`](0014-macos-release-binaries.md)), and the install scripts have settled on a stable contract — version discovery via the GitLab releases API, archive download from the generic package registry, SHA256 verification against a published sibling `SHA256SUMS`. A built-in updater is now the natural next step.

This spec defines `inat self update` as a single new top-level command, nested under a new `self` parent (so `inat self info`, `inat self uninstall`, etc. have a home if/when they're added). The command is a thin wrapper around the same protocol `install.sh` and `install.ps1` already speak: discover the latest tag, download the archive matching the host triple, verify SHA256, swap the running binary in place. No new module dependencies, no new CI jobs, no new release-side artifacts.

A companion implementation plan lives at [`docs/plan/0015-self-update.md`](../plan/0015-self-update.md).

This spec extends [`docs/specs/0002-ci-cd-and-install.md`](0002-ci-cd-and-install.md) §10 (promotes one of its enumerated non-goals to a goal) and [`docs/specs/0009-go-port.md`](0009-go-port.md) (adds one CLI surface). It does not change the release pipeline, the installer scripts, or the registry layout.

---

### 2. Goals

1. A new command `inat self update [--version vX.Y.Z] [--check] [--force]` MUST install the latest published release of `inat` over the running binary, on each of the four supported platforms: Linux x86_64, Windows x86_64, macOS Intel, macOS Apple Silicon. After a successful run the user MUST be able to invoke the binary at the same path and see the new `--version` output.
2. With no flags, the command MUST resolve "latest" the same way `install.sh`'s `resolve_version` does: hit `${API_BASE}/projects/${PROJECT_ID}/releases?per_page=50`, take the first entry whose `tag_name` does not contain `-` (i.e. exclude pre-releases). `--version vX.Y.Z` MUST install that exact tag (also used for downgrade); `--pre-release` is NOT added in this spec — pinning via `--version` covers that need without growing surface.
3. With `--check`, the command MUST print "current X, latest Y, update available" or "current X, already on latest" to stderr and exit 0 without touching the binary or the network beyond the version-discovery call.
4. Without `--force`, when the resolved version equals the running version (i.e. `main.version` injected by ldflags), the command MUST short-circuit before download with "already on vX.Y.Z" on stderr and exit 0. With `--force`, the command MUST proceed through download + verify + swap regardless. `--force` is the explicit re-install / re-download lever; it does not change verification behaviour.
5. The download URL pattern MUST match the install scripts byte-for-byte: `${API_BASE}/projects/${PROJECT_ID}/packages/generic/inat/${VERSION}/inat-${VERSION}-${TARGET}.{tar.gz,zip}` plus a sibling `SHA256SUMS`. `VERSION` is the bare tag (no leading `v`) per `ci/build-release.sh:46`. `TARGET` mirrors `install.sh`'s `detect_target` table verbatim (Rust-style triples kept by spec 0014 §3).
6. The archive's SHA256 MUST be verified against the matching line in `SHA256SUMS` BEFORE any filesystem mutation in the binary's directory. A mismatch MUST abort with a typed error and leave the existing binary untouched. This mirrors `install.sh`'s `download_and_verify` and `install.ps1`'s `Test-Sha256` exactly so the trust model is uniform.
7. The binary swap MUST be safe to perform on the currently running process on all three OS families. The "rename-self-aside" pattern (rename the running binary to `<path>.old`, write the new binary to the original path, best-effort `os.Remove` of `.old`) works on Linux, macOS, and Windows without OS branching beyond the `.old` cleanup, which is best-effort because Windows holds an exclusive lock on the running `.exe`.
8. A leftover `<binary>.old` from a previous Windows update MUST be cleaned up on the next `inat` invocation. The cleanup runs from the cobra root `PersistentPreRunE` and is silent on success and on failure both — failure means the file vanished or the user lacks permission, neither of which warrants a message on every command.
9. Authentication MUST reuse the same env-var contract the install scripts established: `INAT_TOKEN` (required), `INAT_TOKEN_TYPE=private|deploy` (default `private`). Missing token → typed error pointing at the README. The token is sent on every HTTP request as `PRIVATE-TOKEN` or `Deploy-Token` per `INAT_TOKEN_TYPE`. No fallback to `git credential fill`: the install scripts don't do it either, and adding a second auth path here would diverge the trust contract for no user-visible win.
10. No new Go module dependencies. The implementation MUST stay within the existing allowlist: `spf13/cobra`, `go-git/go-git/v5`, and the standard library. Archive handling uses `archive/tar` + `compress/gzip` and `archive/zip`; HTTP uses `net/http`; SHA256 uses `crypto/sha256`. (None of these are imported in the codebase today; all are stdlib and need no allowlist exception per CLAUDE.md §8.)
11. No new CI jobs and no release-pipeline changes. The command consumes artifacts the existing pipeline has been publishing since v0.4.0. `release:publish` is unchanged; the install scripts remain the canonical first-time-install path.
12. No goroutines and no asynchronous code paths. The pipeline is strictly sequential per CLAUDE.md §11.

---

### 3. Non-goals

- **Auto-update on launch.** `inat` will never check for updates without an explicit `inat self update[ --check]` invocation. Background update checks would either silently spend the user's network budget or leak information about which user runs which inat version against which registry; neither is desirable for a CLI a user runs hundreds of times a day.
- **Notifying about new versions on every command.** Same reason: the user invoked `inat install`, not `inat install + please-tell-me-about-updates`. `inat self update --check` is the supported way to learn about a new version.
- **Replacing the install snippets.** `install.sh` and `install.ps1` remain the canonical first-time install path. They handle PATH wiring, shell-profile editing, and managed-section markers — concerns `inat self update` does not have because it operates on an already-installed binary at a path the user chose.
- **Updating any companion file.** The command touches *exactly one path*: `os.Executable()` (resolved through `filepath.EvalSymlinks`). It does not touch `~/.inat/`, `.inat/`, the cache, or any other inat-managed file. There is no "update inat config schema" step; on-disk schemas stay additive per the existing additive-compat convention (memory `feedback_additive_schema_compat`).
- **Rollback to an arbitrary historical version beyond `--version`.** Listing every prior tag and offering interactive choice is a UI feature without a clear use case; `--version vX.Y.Z` is sufficient for the targeted-downgrade case. Users who need historical archaeology already have `git log` on the repo.
- **Replacing the `git credential fill` auth used by go-git for source clones.** `inat self update` is a separate trust path (HTTPS GET against the package registry, not git's smart-HTTP); reusing the env-var contract from `install.sh` keeps the two paths comparable but does not couple them.
- **Cosign / GPG / minisign signature verification** beyond SHA256. The release pipeline does not produce these signatures (`docs/specs/0002-ci-cd-and-install.md` §10 explicit non-goal); self-update can not verify what was never produced. If signatures land in a future release-pipeline spec, this command grows verification trivially.
- **Handling read-only install directories** (e.g. `/usr/local/bin/inat` on a Linux host where the user installed via sudo). Self-update probes write permission early and surfaces a typed `read-only-install-dir` error. Re-acquiring privileges is the user's responsibility — `sudo inat self update` is the operator-level escape hatch.
- **Linux/arm64 release artifacts.** `install.sh`'s `detect_target` accepts `Linux-aarch64` but the release pipeline does not (yet) publish `aarch64-unknown-linux-gnu`. Self-update inherits the same shape: it constructs the URL, the SHA256SUMS lookup fails, the user gets the same `release version X.Y.Z not published for aarch64-unknown-linux-gnu` error `install.sh` would produce. No special-casing in this spec — when the pipeline grows the target, self-update gets it for free.
- **A separate progress UI.** Standard stderr logging (`==> downloading inat-…`, `==> checksum OK`, `==> swapping in place`) matches the install scripts. No spinner, no progress bar.

---

### 4. Design

#### 4.1 Command surface

```
inat self update [--version vX.Y.Z] [--check] [--force]
```

- Nested under a new `self` parent with no children other than `update` initially. Pattern mirrors `inat sources add/remove/list` and `inat agents add/remove/list` in `go/main.go:77-147`.
- Flags are positional-free; all behaviour is flag-driven.
- Exit code 0 on success or `--check` with no update available; non-zero on every error path. Specific exit codes are an internal implementation detail (no shell scripts depend on them; the install scripts' codes are documented because users source them, but `inat self update` errors surface as typed `errs.Error` strings).

#### 4.2 Pipeline

The command runs strictly sequentially through these steps, each an extracted helper function ≤30 lines per CLAUDE.md §4:

1. **Resolve current binary path.** `path, err := os.Executable()` then `filepath.EvalSymlinks(path)` to follow any `~/.local/bin/inat → /opt/inat/bin/inat` shim. If the resolved path lives under `os.TempDir()` (heuristic for `go run`'s cache directory), abort with a typed `running-via-go-run` error: there is no installed binary to update.
2. **Resolve target triple.** Pure function over `runtime.GOOS` × `runtime.GOARCH`. Table mirrors `install.sh`'s `detect_target`:

   | GOOS | GOARCH | Triple | Archive |
   |---|---|---|---|
   | linux | amd64 | x86_64-unknown-linux-gnu | `.tar.gz` |
   | linux | arm64 | aarch64-unknown-linux-gnu | `.tar.gz` |
   | windows | amd64 | x86_64-pc-windows-msvc | `.zip` |
   | darwin | amd64 | x86_64-apple-darwin | `.tar.gz` |
   | darwin | arm64 | aarch64-apple-darwin | `.tar.gz` |

   Anything else (e.g. `linux/386`, `freebsd`) → typed `unsupported-platform` error naming the GOOS/GOARCH pair.
3. **Resolve auth header.** Read `INAT_TOKEN` from the environment; if empty, typed `no-token` error pointing at README §"Provisioning an INAT_TOKEN". Read `INAT_TOKEN_TYPE` (default `private`); accept `private` or `deploy`; anything else → typed `bad-token-type` error.
4. **Discover latest version.** Skipped when `--version` is set. Single `GET https://${HOST}/api/v4/projects/${PROJECT_ID}/releases?per_page=50` with the auth header. Parse the JSON response (stdlib `encoding/json` against a minimal `[]struct{TagName string \`json:"tag_name"\`}`); take the first entry whose `tag_name` does not contain `-`. Empty result → typed `no-release-found` error.
5. **Compare versions.** String compare (not full semver): both `main.version` (set by ldflags) and the resolved tag's bare form are byte-equal when they refer to the same release. If equal and `--check` is set → print the no-update line and exit 0; if equal and `--force` is unset → print the already-on-latest line and exit 0; if equal and `--force` is set → continue. A future spec can graduate to semver compare if a use case appears (e.g. "warn if pinned version is older than latest"); the byte-equality floor is sufficient for "is the running binary the same release as latest".
6. **`--check` happy path with update available.** Print "current X, latest Y, update available — run `inat self update` to install" and exit 0 without touching the network or filesystem further. Step 7 onwards is skipped.
7. **Download `SHA256SUMS`.** `GET ${REGISTRY_BASE}/${VERSION}/SHA256SUMS` with auth. 404 → typed `version-not-published` error naming the version (matches `install.sh`'s `EXIT_NOT_FOUND`).
8. **Download archive.** `GET ${REGISTRY_BASE}/${VERSION}/inat-${VERSION}-${TRIPLE}.{tar.gz|zip}` streamed into `<binary-dir>/.inat-update-<pid>-<rand>.partial`. Same directory as the target binary so the eventual `os.Rename` is on the same filesystem. Network failure → typed `download-failed` error. The temp file is cleaned up on every error path via `defer`.
9. **Verify SHA256.** Stream the temp file through `crypto/sha256.New()`; compare hex digest against the matching line in `SHA256SUMS`. Mismatch → typed `checksum-mismatch` error, temp file deleted, target binary untouched.
10. **Extract the binary.** `archive/tar` + `compress/gzip` for `.tar.gz`; `archive/zip` for `.zip`. Look up the entry whose name suffix is `/inat` (Unix/macOS) or `/inat.exe` (Windows). No file in the archive → typed `archive-layout` error (mirrors `install.sh` exit 6). Write the entry to `<binary-dir>/.inat-update-<pid>-<rand>.new`; on Unix, set `0755`. The archive's directory entries and other files (`LICENSE`, `README`) are ignored.
11. **Atomic swap.**
    1. `os.Rename(target, target + ".old")` — moves the running binary aside. Works on all three OSes (Windows allows rename of a locked exe, Unix kernels keep the inode mapped via the open-on-exec fd).
    2. `os.Rename(target + ".new", target)` — move the new binary into place.
    3. Best-effort `os.Remove(target + ".old")`. Windows will refuse while the parent process is running; the failure is silent because the next `inat` invocation cleans it up via `PersistentPreRunE` (see §4.3). Unix succeeds.
    4. On any error during 11.1 or 11.2: rollback. Try `os.Rename(target + ".old", target)` to restore. If even rollback fails, surface a typed `unrecoverable-swap-failure` error with both the original and the rollback errors wrapped in the cause — at that point the target directory is in an inconsistent state and the user must intervene.
12. **Print success.** `==> updated inat vX → vY at <PATH>` to stderr, exit 0.

#### 4.3 `.old` cleanup on next launch

Cobra's root command grows a `PersistentPreRunE` (a closure constructed in `newRootCmd`) that:

1. Resolves the current binary path the same way as step 1 above (`os.Executable` + `EvalSymlinks`); if the lookup fails, returns nil (don't fail every inat command on a Go runtime quirk).
2. Computes `<binary>.old` and runs `os.Remove`. Errors are silenced — the file may not exist (no prior update), may have been removed by an external tool, or may still be locked (which means the *previous* process is still running, which is fine and we don't want to interrupt the user's other terminal).

The cleanup is ~10 lines, lives inline in `go/main.go`, and is the only piece of self-update code that fires on every command. It is untested for performance reasons (one `os.Stat` + at most one `os.Remove`) but covered by an integration-style test that runs `update` then asserts a synthesised `.old` is gone after a second invocation.

#### 4.4 Why no `git credential fill` fallback

`install.sh` reads `INAT_TOKEN` only — no fallback. Self-update reuses that contract verbatim so a user who can run the install snippet can run `inat self update` with the same env. Adding a `git credential fill` fallback would (a) mean `inat self update` sometimes works without `INAT_TOKEN` and sometimes doesn't, depending on whether the user previously ran `inat sources add` against the GitLab host, (b) couple this command's auth to the go-git transport's Azure DevOps workarounds (irrelevant here), and (c) blur the README's promise that token provisioning is a one-time setup with a single env var. Single auth path keeps the trust model identical to install.sh.

#### 4.5 Why string equality, not semver

Comparing `main.version` to the resolved tag for "are these the same release" is a byte-equality check: the release pipeline guarantees `main.version` (injected via `-ldflags "-X main.version=${VERSION}"` per `ci/build-release.sh:90`) equals the bare form of the tag (`VERSION="${TAG#v}"` per `ci/build-release.sh:46`). The discovery step normalises the tag the same way (`strings.TrimPrefix(tag, "v")`), so the comparison is well-defined.

A semver-aware compare (e.g. "warn if pinned to vX.Y.Z while latest is vX.Y.Z+1") would require either a new dependency (forbidden by CLAUDE.md §8) or a hand-rolled parser (~50 lines). The byte-equality floor delivers "is my running binary the latest release" with no semver complexity; future "warn-if-stale" UX is a separate feature with separate scope.

#### 4.6 Why all OSes use the same swap pattern

Three options were considered:

- **Direct overwrite.** Linux and macOS allow `os.Rename` over a running binary; Windows does not. Two code paths — one for POSIX, one for Windows — would mean OS-tagged files (`swap_unix.go` / `swap_windows.go`), which the codebase has zero of today (memory: no existing `_windows.go` / `_unix.go` files, no `runtime.GOOS` switches in `go/`).
- **Rename-self-aside.** Works on all three OSes uniformly: rename the running file to a sibling, write the new file to the original path, best-effort cleanup of the sibling. Single code path; `runtime.GOOS` is consulted only to pick the archive extension and the target-triple table.
- **Spawn a helper.** Write the new binary to a temp path, exec a small shell snippet (or `cmd /c`) that waits for the parent to exit then performs the move. Adds an exec dependency, complicates the rollback story, and is the standard hack for systems older than Win32's rename-locked-exe support — gratuitous on modern Windows.

Option 2 wins. The swap step is ~40 lines, single file, no build tags. The Windows-leftover story (the `.old` file lingers until the next inat run) is annoying but quiet; the cleanup hook closes the loop.

#### 4.7 macOS Gatekeeper interaction

Spec 0014 §3 documents that browser- and curl-downloaded macOS binaries get a `com.apple.quarantine` xattr and trigger Gatekeeper's "cannot be opened" dialog on first launch. **Self-update does not hit this path:** Go's `net/http` client is not LaunchServices-aware and does not set `com.apple.quarantine` on writes. The new binary inherits no xattrs from its temp file (the archive entry is extracted with `os.Create` + `io.Copy`, not via a quarantine-aware API), and the rename-aside swap preserves no xattrs from the prior binary either. The ad-hoc `rcodesign` signature embedded in the Mach-O is what Apple Silicon's kernel checks; that survives the archive round-trip intact. `inat self update` is therefore a *quieter* upgrade UX on macOS than the install one-liner — no `xattr -d com.apple.quarantine` step required.

#### 4.8 File layout

New package `go/cli/self/`, four files:

- `self.go` — cobra wiring (`NewSelfCmd()` / `newSelfUpdateCmd()`), `RunSelfUpdate(args)`. Mirrors the shape of `go/cli/status/status.go` and `go/cli/refresh/refresh.go`. ~80 lines.
- `release.go` — release discovery and download: `LatestVersion(client *http.Client, auth Auth) (string, error)`, `DownloadArchive(client *http.Client, auth Auth, version, triple string, ext string, dest string) error`, `VerifySHA256(archivePath, sumsPath, archiveName string) error`, `parseSHA256Sums(io.Reader, name) (string, error)`. ~150 lines, all sub-functions ≤30 lines.
- `swap.go` — `ResolveBinaryPath() (string, error)`, `ExtractBinary(archivePath, ext, dest string) error` (one entry point that dispatches to `extractTarGz` / `extractZip` based on `ext`), `SwapBinary(target, replacement string) error` with rollback. ~180 lines.
- `self_test.go` — table-driven tests using `httptest.NewServer` for the registry and a temp-dir fake binary for the swap. ~250 lines.

Each file under the 300-line target; each function under 30 lines.

#### 4.9 Authentication shape

`Auth` is a small struct in `go/cli/self/release.go`:

```go
type Auth struct {
    Header string // "PRIVATE-TOKEN" or "Deploy-Token"
    Token  string
}
```

A constructor `LoadAuth() (Auth, error)` reads `INAT_TOKEN` and `INAT_TOKEN_TYPE` and returns a typed `no-token` / `bad-token-type` error on failure. Tests inject a fake `Auth` struct; no env munging in tests.

Auth is never logged. The HTTP request constructor (`newAuthedRequest(method, url, auth)`) sets the header on a single point of contact so a future audit can confirm token-leak surface area.

---

### 5. Compatibility

- **`install.sh` / `install.ps1`** — unchanged. Self-update is a sibling code path that consumes the same artifacts; the install scripts remain canonical for first-time install (PATH wiring, shell-profile management).
- **`ci/build-release.sh` / `ci/install-rcodesign.sh` / the GitLab CI config** — unchanged. No new build target, no new artifact, no new pipeline stage.
- **`go/main.go`** — adds `newSelfCmd()` to the `AddCommand` list and a `PersistentPreRunE` for the `.old` cleanup (~12 lines net add).
- **`go/errs/errs.go`** — adds typed errors for the new failure modes (`NoToken`, `BadTokenType`, `UnsupportedPlatform`, `RunningViaGoRun`, `NoReleaseFound`, `VersionNotPublished`, `DownloadFailed`, `ChecksumMismatch`, `ArchiveLayout`, `UnrecoverableSwapFailure`). Errors stay in the same enum / sentinel / constructor pattern as the existing kinds. Net add ~70 lines.
- **`go/go.mod` / `go/go.sum`** — byte-identical pre/post change. No new modules.
- **Existing CLI commands** — unchanged. The new `self` parent does not shadow any existing command name.
- **`README.md`** — gains an "Upgrading" section pointing at `inat self update` and noting the `INAT_TOKEN` requirement matches the install one. The install snippets remain the documented first-time path.
- **`CHANGELOG.md`** — `[Unreleased]` gains an `### Added` entry for the new command.
- **Pre-commit hook + CI** — unchanged. The hook runs `gofmt`, `go vet`, `staticcheck`, `go test ./...` against `go/`; the new package is covered by all four. The spec-presence check is satisfied by this spec/plan pair.

---

### 6. Acceptance criteria

1. **Build clean.** `go -C go/ build ./...` exits 0; the binary's `--help` output lists `self` alongside the existing top-level commands; `inat self --help` shows `update` as the only sub-command.
2. **`inat self update --check` against the registry.** With `INAT_TOKEN` set and the running binary stamped to the latest release, output reads `current X.Y.Z, already on latest`. With the running binary stamped to an older release (e.g. `0.4.0` while latest is `0.4.1`), output reads `current 0.4.0, latest 0.4.1, update available …` and exits 0. No filesystem mutation.
3. **`inat self update` happy path on Linux x86_64.** Build a stamped-old binary at `/tmp/inat-old`; run `INAT_TOKEN=... /tmp/inat-old self update`. After completion, `/tmp/inat-old --version` reports the new tag, `/tmp/inat-old.old` does not exist.
4. **`inat self update` happy path on Windows amd64.** Same as 3, but expect `inat.exe.old` to linger after the run; the next invocation of `/tmp/inat-old --version` (or any inat command) silently removes it. Verified by `dir` between runs.
5. **`inat self update` happy path on macOS (Apple Silicon).** Same as 3. Additionally: the new binary launches without a Gatekeeper dialog (no `xattr -d com.apple.quarantine` workaround needed). `codesign -dv` against the new binary reports `Signature=adhoc`.
6. **Checksum mismatch aborts cleanly.** Local test with a `httptest.NewServer` returning a corrupted archive: `RunSelfUpdate` returns a typed `ChecksumMismatch` error; the target binary on disk is byte-identical to its pre-call state; no `.old` or `.new` siblings remain.
7. **`--version vX.Y.Z` pins to an exact tag.** With `--version v0.4.0` against a registry where the current binary is v0.4.1, the binary downgrades to v0.4.0; `--version vX.Y.Z` for a non-existent tag → typed `VersionNotPublished` error.
8. **`--force` re-downloads on already-current.** With current = latest, `inat self update` exits 0 without download; `inat self update --force` proceeds through download + verify + swap (verified by an httptest counter on the server side).
9. **Unsupported platform fails fast.** A test that fakes `runtime.GOOS=js` (via a small `resolveTriple(goos, goarch string)` helper that takes the values as arguments) returns a typed `UnsupportedPlatform` error before any network call.
10. **`go run`-detected refusal.** A test that stages `os.Executable()` returning a path under `os.TempDir()` returns a typed `RunningViaGoRun` error; no network call.
11. **Missing token fails fast.** With `INAT_TOKEN` unset, `RunSelfUpdate` returns a typed `NoToken` error before any network call. The error message points at README §"Provisioning an INAT_TOKEN".
12. **No new dependencies.** `git diff go/go.mod go/go.sum` between the pre-change and post-change tree is empty.
13. **No `unsafe` package, no goroutines.** `grep -n "unsafe\|go func\|go " go/cli/self/` returns nothing; `staticcheck` and `go vet` pass.
14. **File / function size limits.** Every new file under 300 lines; every new function under 30 lines (per `gofmt -l` and a hand-check during MR review).
15. **Pre-commit hook clean.** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all exit 0.

---

### 7. Testing notes

- **Network mocking via `httptest.NewServer`.** All tests that exercise discovery / download / verify use an in-process HTTP server. No real GitLab calls in CI.
- **Swap test on the test host.** `TestSwapBinary` writes a fake binary to a temp dir, exercises `SwapBinary` with a known replacement, asserts the swap landed and that the post-state matches expectations. On Windows the test verifies the `.old` lingers (CI runs on Linux only, so this assertion is gated on `runtime.GOOS == "windows"` — captured but not exercised in CI; the manual smoke in §6.4 is the floor).
- **Version-comparison test.** Unit tests for the byte-equality short-circuit, the `vX.Y.Z` vs `X.Y.Z` normalisation, and the `--force` override.
- **No live registry probe in tests.** Eliminates an entire class of CI flakiness; the integration test that hits the real registry is the manual smoke covered by §6.2.
- **Concurrency tests not added.** The pipeline is sequential per CLAUDE.md §11; there are no concurrent code paths to test.
- **Failure-injection tests.** The swap step's rollback path is tested by injecting a forced rename failure on step 11.2 (e.g. by removing write permission on the parent dir between rename calls); the test asserts the original binary is restored bit-for-bit.

---

### 8. Documentation

- **`README.md`** — new §Upgrading between §Install and §Uninstall, with one short paragraph: "From v0.5.0 onward, run `inat self update` to upgrade in place. Same `INAT_TOKEN` env var as the install one-liner. Use `--check` to preview, `--version vX.Y.Z` to pin." Cross-links to §Provisioning an INAT_TOKEN.
- **`CHANGELOG.md`** — `[Unreleased]` gets an `### Added` entry: "`inat self update` upgrades the running binary in place. Resolves the latest release from the package registry, downloads the platform-matched archive, verifies SHA256 against the published `SHA256SUMS`, and swaps the binary atomically. Same `INAT_TOKEN` env contract as `install.sh`. Spec / plan [`0015-self-update`](docs/specs/0015-self-update.md)."
- **`docs/specs/0015-self-update.md`** — this document.
- **`docs/plan/0015-self-update.md`** — companion rollout document.
- **`docs/specs/0002-ci-cd-and-install.md` §10** — the bullet "A `inat self update` subcommand" is moved out of the non-goals list with a back-pointer to spec 0015 ("→ implemented in spec 0015"). All other non-goals in that list stand.

---

### 9. Out-of-scope follow-ups

- **Auto-update channels** (nightly / dev / pre-release). Pinning via `--version` covers the targeted-install case; an automated channel selector is a separate UX with separate trust implications.
- **Cosign / minisign / GPG signature verification.** Gated on the release pipeline producing those signatures; spec 0002 §10 lists them explicitly as out-of-scope. When and if they land, this command grows verification trivially (one more file in the temp dir, one more verify call before the swap).
- **`inat self uninstall`** — would invoke `uninstall.sh` / `uninstall.ps1` from inside the running binary. Niche; users who installed via the one-liner can always re-fetch the matching uninstall script.
- **`inat self info`** — print the currently running binary's path, version, target triple, and install source. Pure-info, ~30 lines, but not motivated by an immediate user need.
- **Semver-aware comparison.** "Warn if a newer version is available" UX; `inat self update --check` already covers the explicit-check case, so this only matters for ambient awareness which is a separate scope (see Non-goals: "notifying about new versions on every command").
- **Linux/arm64 release artifacts.** Pipeline-side feature; this command auto-supports them when published.
- **Resumable downloads.** Archives are ≤10 MB; mid-download interruption is a re-run, not a retry. Not worth the complexity until artifacts grow.
