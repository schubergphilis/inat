## inat — Self-update subcommand (Plan 0015)

Pairs with [`docs/specs/0015-self-update.md`](../specs/0015-self-update.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, TDD test order, and verification.

Promotes `inat self update` from the non-goals list in [`docs/specs/0002-ci-cd-and-install.md`](../specs/0002-ci-cd-and-install.md) §10 to a shipping feature. Builds entirely on infrastructure that already exists: the GitLab generic package registry layout (`install.sh:151-182`), the four release artifacts (`ci/build-release.sh:50-65`), the SHA256SUMS contract (`install.sh:163-181`), and the `INAT_TOKEN` env-var auth (`install.sh:84-93`).

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Command shape | `inat self update [--version vX.Y.Z] [--check] [--force]`. Nested under a `self` parent so future siblings (`info`, `uninstall`) have a home; mirrors the `sources add/remove/list` pattern in `go/main.go:77-110`. |
| Auth contract | Reuse `INAT_TOKEN` + `INAT_TOKEN_TYPE` (private/deploy) — same as `install.sh` and `install.ps1`. No `git credential fill` fallback. Single-source trust model; users who can run the install one-liner can run `self update` with the same env. |
| Version discovery | `GET /api/v4/projects/<id>/releases?per_page=50` → take first `tag_name` not containing `-` (mirrors `install.sh:121-140`). String compare against `main.version` (the ldflags-injected value) for the "already on latest" short-circuit. No semver compare; `install.sh` doesn't do one either. |
| Pre-release tags | `--pre-release` flag NOT added in this release. `--version vX.Y.Z-rc.N` covers the targeted-install case; auto-discovering pre-releases is install-script territory. |
| Download URL | `${API_BASE}/projects/<id>/packages/generic/inat/<bare-version>/<archive>` and `<…>/SHA256SUMS`. Identical to `install.sh:155-158`. |
| Target triple table | Mirror `install.sh:95-105` exactly: 5 GOOS×GOARCH combos resolved (linux/amd64, linux/arm64, windows/amd64, darwin/amd64, darwin/arm64). The pipeline only publishes 4 today; linux/arm64 will fail at the `SHA256SUMS` lookup with the same error `install.sh` produces. No special-casing. |
| Archive handling | stdlib only: `archive/tar` + `compress/gzip` for `.tar.gz`, `archive/zip` for `.zip`. Look up the entry by suffix `/inat` or `/inat.exe`. Ignore directory entries, `LICENSE`, `README` — only the binary is needed. |
| SHA256 verification | stdlib `crypto/sha256` streaming hash; compare hex digest against the matching line in `SHA256SUMS` (whitespace-aware, supports the optional `*` GNU-coreutils binary marker that `install.sh:165` already accommodates). |
| Swap pattern | Single uniform "rename-self-aside": rename target → `<target>.old`, write new → target, best-effort `os.Remove` of `.old`. Works on Linux + macOS + Windows without OS-tagged source files. Spec §4.6 documents the alternatives considered. |
| `.old` cleanup | Cobra root `PersistentPreRunE` silently `os.Remove`s `<target>.old` if it exists. ~10 lines inline in `go/main.go`. Required because Windows can't delete the running exe; the leftover lingers until the *next* inat invocation. |
| Error model | Typed `errs.Error` kinds for every failure mode (no token, bad token type, unsupported platform, running via go run, no release found, version not published, download failed, checksum mismatch, archive layout, unrecoverable swap failure). One constructor per kind, mirrors the existing pattern in `go/errs/errs.go:162-200`. |
| HTTP client | Plain `net/http.DefaultClient` with a 5-minute timeout. No custom transport (the existing `git/source/transport.go` UA-injecting transport is install-globally for go-git only and would be a footgun on a non-git request). |
| Where it lives | New package `go/cli/self/`. Four files: `self.go` (cobra), `release.go` (HTTP+sha), `swap.go` (extract+rename), `self_test.go` (table-driven). Mirrors the layout of `go/cli/refresh/`. |
| Token injection in tests | Tests construct an `Auth` struct directly (`{Header: "PRIVATE-TOKEN", Token: "test-token"}`); no `os.Setenv` in tests. Production code reads env via a single `LoadAuth()` call at the entry point. |
| Test fakes | `httptest.NewServer` for the registry; the test rewires `BaseURL` and `ProjectID` to point at the fake. The default values stay private constants matching the README. No public knob to override the URL at runtime — that would be a footgun (a user could be tricked into pointing self-update at a malicious mirror). |

---

### 2. Files

#### New
- `go/cli/self/self.go` — cobra wiring: `NewSelfCmd()` returns the `self` parent; `newSelfUpdateCmd()` returns the `update` child with flags `--version`, `--check`, `--force`. `RunSelfUpdate(opts Options) error` is the testable entry point. Sized to ~80 lines.
- `go/cli/self/release.go` — release discovery, archive download, SHA256 verification, `SHA256SUMS` parsing. `Auth` struct + `LoadAuth()` constructor. `LatestVersion`, `DownloadFile`, `VerifySHA256`, `parseSHA256Sums` helpers. ~150 lines.
- `go/cli/self/swap.go` — `ResolveBinaryPath`, `IsRunningViaGoRun`, `ResolveTriple`, `ExtractBinary` (dispatches to `extractTarGz` / `extractZip`), `SwapBinary` with rollback. ~180 lines.
- `go/cli/self/self_test.go` — table-driven tests for every helper plus an end-to-end fake-server test. ~250 lines.
- `docs/specs/0015-self-update.md` — the contract.
- `docs/plan/0015-self-update.md` — this document.

#### Modified
- `go/main.go`:
  - Imports the new `cli/self` package as `selfcmd`.
  - Adds `selfcmd.NewSelfCmd()` to the `root.AddCommand(...)` call (alphabetic position would be after `refresh`; insert after `validate` to keep it visually grouped with the meta-commands).
  - Adds a `PersistentPreRunE` on the root that runs `selfcmd.CleanupOldBinary()` (silent best-effort `os.Remove` of `<binary>.old`).
- `go/errs/errs.go`:
  - Extends the `Kind` enum with ten new variants (see spec §5).
  - Adds the matching sentinels and constructors.
  - Extends the `Error()` switch with the new format strings.
- `README.md`:
  - Adds §Upgrading between §Install and §Uninstall (one paragraph + flag table reference).
  - One-line cross-reference under §Common flags noting `inat self update --check` for "is there a new version".
- `CHANGELOG.md`:
  - `[Unreleased]` gets an `### Added` entry pointing at spec 0015.
- `docs/specs/0002-ci-cd-and-install.md`:
  - Section 10's bullet "A `inat self update` subcommand" gains "→ implemented in [spec 0015](0015-self-update.md)" inline. Keeps history; clarifies status.

#### Untouched
- `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1` — first-time install path stays canonical.
- `ci/build-release.sh`, `ci/install-rcodesign.sh`, the GitLab CI config — no pipeline change, no new artifact.
- `go/go.mod`, `go/go.sum` — no new modules; archive + http + sha256 are all stdlib.
- `go/git/source/transport.go` — UA-injecting transport stays scoped to go-git's protocol registry; self-update uses `http.DefaultClient` directly.
- `VERSION` — bump is the release captain's call when cutting the next tag (likely v0.5.0 since this also includes spec 0014).
- `rust/` — frozen archive, untouched per CLAUDE.md.

---

### 3. Implementation order

TDD-first per CLAUDE.md §1. Each step writes failing tests, then minimal implementation to pass them, then refactor.

1. **`go/errs/errs.go` extension.** Add the ten new `Kind` variants, sentinels, constructors, and `Error()` cases. Tests in `go/errs/errs_test.go` verify each new kind formats its message correctly and is `errors.Is`-comparable to its sentinel. Land first because the rest of the work imports these.
2. **`go/cli/self/release.go` — `parseSHA256Sums`.** Pure function over an `io.Reader` and a target archive name; returns the hex digest. Tests cover: GNU-coreutils format with `*` marker, plain spaces, BSD format (`SHA256 (file) = digest`), missing entry → typed error. Smallest meaningful unit; landing it first surfaces the parsing edge cases before they're entangled with HTTP.
3. **`go/cli/self/release.go` — `Auth` + `LoadAuth`.** Reads `INAT_TOKEN` and `INAT_TOKEN_TYPE`. Tests inject env via `t.Setenv`; cover empty token, default `private` type, explicit `deploy`, invalid type. Pure-function-ish; no I/O beyond env reads.
4. **`go/cli/self/release.go` — `LatestVersion`.** Takes a configured `*http.Client`, base URL, and `Auth`. Tests use `httptest.NewServer` to return canned `/releases?per_page=50` payloads (one with three tags including a `-rc`, one with only pre-releases, one empty). Verifies the auth header is set on the request.
5. **`go/cli/self/release.go` — `DownloadFile`.** Generic `(client, auth, url, destPath) → error`. Tests cover happy path (200 OK with bytes), 404 → `VersionNotPublished`, network failure → `DownloadFailed`. Streamed via `io.Copy` so memory footprint is constant.
6. **`go/cli/self/release.go` — `VerifySHA256`.** Wraps `parseSHA256Sums` + `crypto/sha256` streaming hash. Tests cover match, mismatch, missing-entry-in-sums.
7. **`go/cli/self/swap.go` — `ResolveTriple`.** Pure function over `(goos, goarch string)`. Tests table-drive every supported combo + an unsupported one. Takes args (not `runtime.GOOS`) so tests are deterministic.
8. **`go/cli/self/swap.go` — `IsRunningViaGoRun`.** Pure function over `(execPath, tempDir string)`. Tests verify the `os.TempDir` heuristic without touching the live filesystem.
9. **`go/cli/self/swap.go` — `ExtractBinary`.** Two paths: `extractTarGz` and `extractZip`. Tests build small in-memory archives via `archive/tar` + `archive/zip` (we can write archives in tests even though we don't ship code that does), feed them through `ExtractBinary`, assert the binary lands at the destination with the correct content + executable bit.
10. **`go/cli/self/swap.go` — `SwapBinary`.** Tests work in `t.TempDir()`: stage a fake "running binary" file, call `SwapBinary(target, replacement)`, assert the new content is at `target`, the old content is at `target.old` (since CI is Linux, the best-effort cleanup deletes it; the test asserts no `.old` remains on Linux). Failure-injection test: pre-create a non-renamable file at `target.new` location to force step 11.2 to fail; assert rollback restored the original.
11. **`go/cli/self/self.go` — `RunSelfUpdate`.** End-to-end orchestration. Tests use `httptest.NewServer` serving the full pipeline (releases listing → SHA256SUMS → archive). The test stages a fake binary in `t.TempDir()` and overrides `os.Executable` via a function-typed package var (set in tests, defaults to `os.Executable` in production). Verify happy path, `--check` no-update, `--check` update-available, `--force` re-download, missing token, unsupported triple.
12. **`go/cli/self/self.go` — cobra wiring.** `NewSelfCmd()`, `newSelfUpdateCmd()`. Tested by exercising the cobra `Execute()` with arg lists; verifies flags reach `RunSelfUpdate`.
13. **`go/main.go` integration.** Add `selfcmd.NewSelfCmd()` to the AddCommand list. Add `PersistentPreRunE` calling `selfcmd.CleanupOldBinary()`. Test in `self_test.go` exercises the cleanup helper in isolation.
14. **README + CHANGELOG.** Documentation per spec §8.
15. **Spec-0002 §10 back-reference.** One-line edit; not a behavioural change.

After step 13, the pre-commit hook is the gate: `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all pass.

---

### 4. Verification

#### Local (test suite)

1. `go -C go/ test ./cli/self/...` — covers every helper from §3 steps 2-12. Should be ~25 test functions.
2. `go -C go/ test ./errs/...` — covers the new error kinds.
3. `gofmt -l go/` — silent.
4. `go -C go/ vet ./...` — silent.
5. `staticcheck ./...` (run from repo root with `-checks=all`) — silent.
6. `go -C go/ test ./...` — full suite remains green.

All five gates are what the pre-commit hook (`.githooks/pre-commit`) and the CI pipeline already enforce; no new tooling.

#### Manual smoke (Linux x86_64, the dev laptop)

7. `go -C go/ build -ldflags "-X main.version=0.4.0" -o /tmp/inat-old .` — stamped-old binary.
8. `INAT_TOKEN=<…> /tmp/inat-old self update --check` — output:
   ```
   current 0.4.0, latest 0.4.1, update available — run `inat self update` to install
   ```
   Exit 0; nothing on disk changes.
9. `INAT_TOKEN=<…> /tmp/inat-old self update` — output ends with:
   ```
   ==> updated inat 0.4.0 → 0.4.1 at /tmp/inat-old
   ```
   `/tmp/inat-old --version` reports `0.4.1`. `ls /tmp/inat-old.old` → no such file (Linux cleans up immediately).
10. Tamper test: temporarily rewrite `release.go`'s SHA expected value to a deliberate wrong digest; rerun step 9. Output ends with a `ChecksumMismatch` error; `/tmp/inat-old --version` still reports the pre-call version (no swap occurred). Revert the tamper.
11. Re-run step 9 with `--force` against current = latest. Verify (via stderr logs) that the download did happen and the binary was re-written even though the version did not change.

#### Manual smoke (Windows amd64)

12. Build a stamped-old binary at `%TEMP%\inat-old.exe`. Run `inat-old.exe self update`. After completion, `%TEMP%\inat-old.exe --version` reports the new tag; `dir %TEMP%\inat-old.exe.old` shows the leftover. Run any `inat-old.exe ...` command (e.g. `inat-old.exe --help`); the leftover is gone afterward (cleaned up by the `PersistentPreRunE`).

#### Manual smoke (macOS Apple Silicon)

13. Build / scp / curl a stamped-old binary to a Mac. `INAT_TOKEN=<…> ./inat-old self update`. The new binary launches without a Gatekeeper dialog (no `xattr` workaround needed). `codesign -dv ./inat-old` reports `Signature=adhoc`.
14. Functional smoke: `./inat-old sources list` and `./inat-old install <kit>` complete normally — same trust path as install.sh, no regression.

The macOS smoke is gated on a team member with Mac access (same as spec 0014 §7); CI runs on Linux only.

---

### 5. Risks & mitigations

- **Trust path divergence between `install.sh` and `inat self update`.** Both must agree byte-for-byte on URLs, auth headers, archive layout, SHA256SUMS format. Mitigation: spec §4 cites the exact `install.sh` line numbers being mirrored; a divergence would surface as a test failure against the same fake-server fixtures the install scripts use. Long-term: if `install.sh` evolves (new URL scheme, different SHA format), `inat self update` must follow in the same MR; the CHANGELOG entry is the audit trail.
- **`os.Executable` returning a `go-build` cache path during dev.** Without the `IsRunningViaGoRun` guard, a developer running `go run . self update` in their checkout would corrupt their Go build cache. Mitigation: the `os.TempDir()` prefix check returns a typed error before any download. Tested in step 10.
- **Read-only install directories.** `~/.local/bin/inat` is user-writable by convention but `/usr/local/bin/inat` (some Linux distros, manual installs) is not. The `os.Rename` call returns `EACCES`/`EPERM`; this surfaces as a typed `WrapIO` error and the user can re-invoke under `sudo`. We don't probe write-permission upfront because the rename is the authoritative check anyway and an upfront probe + race window would be a false security guarantee.
- **Mid-swap interruption.** If the user kills the process between step 11.1 (rename to `.old`) and 11.2 (rename `.new` to target), the binary is gone. The user can recover by manually renaming `.old` back. Mitigation: document this in spec §4.2 step 11.4 (rollback path); the swap window is sub-second for a ~10 MB binary on modern filesystems; risk is acceptable. A defensive "if interrupted, try recovery on next launch" hook would conflict with the `.old` cleanup hook (we don't know if `.old` is a leftover from a successful update or a half-completed one without more state).
- **Registry-side hostname change.** The hardcoded GitLab hostname and project ID could in principle change. Mitigation: same constants live in `install.sh` and `install.ps1`; if they change, all three update together in one MR. No runtime override knob (intentional — see decision in §1).
- **`crypto/sha256` performance.** Negligible — archives are ≤10 MB, stdlib SHA256 hashes ~500 MB/s on modern hardware.
- **`archive/zip` directory traversal.** Standard zip-slip / tar-slip considerations apply: `archive/zip` lets a malicious archive name an entry with `../` segments. Mitigation: the extraction code only writes the *one* entry whose name suffix matches `/inat` or `/inat.exe` to a pre-determined destination path; the entry's name is discarded after the suffix match, so a malicious `../../etc/passwd/inat` entry would still write to the intended dest path (not the entry's path). Tested by a malicious-name fixture.

---

### 6. Out-of-scope follow-ups

Same as spec §9. Re-listed here for the rollout-doc audience:

- **Auto-update channels** (nightly / dev). Pinning via `--version` covers targeted installs.
- **Cosign / minisign / GPG signature verification.** Gated on the release pipeline producing those signatures.
- **`inat self uninstall` / `inat self info`.** Nice-to-have but unmotivated by current user need.
- **Semver-aware compare for "is my version stale".** `--check` covers the explicit case; ambient awareness on every command is a separate UX with separate trust implications.
- **Linux/arm64 release artifacts.** Pipeline-side feature; this command auto-supports them when published.
- **Resumable downloads.** Archives are small; mid-download interruption is a re-run, not a retry.
