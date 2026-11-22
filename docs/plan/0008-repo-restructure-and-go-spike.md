# Plan — Repo restructure + go-git spike

No matching spec. Step 1 is a mechanical rename; Step 2 is an evaluation spike.
Neither extends spec 0001's contract, so this ships plan-only per the
`CLAUDE.md` "spec or plan alone is acceptable" exception. A future Go port
(if greenlit by the spike's outcome) will get its own spec 0009 + plan 0009 pair.

## Why

`inat` is Rust + `git2`. Cross-platform CI is the active pain point:
- The Windows binary build runs on a self-hosted runner on a developer laptop
  that self-disables for ~1h after DNS/403 cascades.
- macOS is deferred for v0.2.0; users `cargo install --path .` from source.
- Rust cross-compile from Linux is workable but ugly: Windows wants either
  `cargo-xwin` or `mingw-w64` + vendored libgit2 + vendored OpenSSL; macOS wants
  `osxcross` + Apple's SDK (EULA gray area).

Go closes both gaps in one shot:
- `GOOS=windows GOARCH=amd64 go build` and `GOOS=darwin GOARCH={amd64,arm64} go build`
  all run from a Linux runner with zero extra toolchain.
- `go-git` (`github.com/go-git/go-git/v5`) is a pure-Go libgit2 replacement
  covering the operations `inat` actually uses (clone, fetch, tree-walk,
  blob-read).
- darwin-arm64 binaries cross-compiled from Linux still need ad-hoc signing
  before they'll launch (Apple Silicon kernel rejects unsigned ARM64 binaries).
  `rcodesign` does that from Linux, no Mac required. Notarization for clean
  Gatekeeper still wants a Mac, but that's distribution polish, not a build
  blocker.

Decision: spike-first. Don't commit to a port until `go-git` is proven against
the actual operations `inat` depends on. The Rust tree stays in the repo; both
languages coexist until the gate flips.

## Decisions (locked)

1. **Restructure**: `git mv` `src/` → `rust/src/`, `tests/` → `rust/tests/`,
   `spike/` → `rust/spike/`, `Cargo.toml` and `Cargo.lock` → `rust/`. No Rust
   behavior change.
2. **Pipeline cutover**: copy the current GitLab CI config to a
   `.rust-backup` sibling (committed to repo as a literal preservation
   file), then rewrite the GitLab CI config to a Go-only pipeline (spike +
   cross-compile sanity). Rust CI suspended for the duration of the spike.
3. **Pre-commit hook keeps running cargo**: `.githooks/pre-commit` updates to
   `cd rust && cargo fmt/clippy/test`; spec-presence regex `^src/` →
   `^rust/src/`. Local-only floor that prevents Rust regressions during the
   spike phase even though CI is silent.
4. **`go.mod` module name**: `inat` (unqualified placeholder). Finalize at
   port time, not now.
5. **Spike form**: Go-test style — `go test ./spike/...`, one `Test*` per
   operation.

## Step 1 — Restructure

### Renames (preserve history)

```
git mv src rust/src
git mv tests rust/tests
git mv spike rust/spike
git mv Cargo.toml rust/Cargo.toml
git mv Cargo.lock rust/Cargo.lock
```

### Files to update (verified inventory)

| File | What changes |
|---|---|
| `.githooks/pre-commit` | Each `cargo …` runs as `(cd rust && cargo …)`; spec-presence regex `^src/` → `^rust/src/`; error message updated |
| `ci/build-release.sh` | `cd rust` before `cargo build`; bin path → `rust/target/${TARGET}/release/inat` |
| `ci/build-release.ps1` | Symmetric: `cd rust`; `$bin = "rust\target\$Target\release\inat.exe"` |
| `ci/verify-release-prereqs.sh` | All 5 `Cargo.toml` reads → `rust/Cargo.toml` |
| `ci/windows-lint.ps1` | `.\tests\installer_tests.ps1` → `.\rust\tests\installer_tests.ps1` |
| `README.md` | `cargo install --path .` → `cargo install --path ./rust`; `cargo build` → `cd rust && cargo build` |
| `docs/guidelines/coding-standards.md` | Project-structure tree references `src/` — update to `rust/src/` |
| `docs/demo-walkthrough.md` | Example file tree references `src/` — update |
| `CLAUDE.md` | Add "Dual-tree layout" section; status-table row for plan 0008 |

### Pipeline cutover

1. Copy the GitLab CI config to its `.rust-backup` sibling — committed alongside the
   rest. Restoration path is `mv` plus a one-line regex update.
2. Rewrite the GitLab CI config to be Go-only:

   ```yaml
   image: golang:1.22-alpine
   stages: [test, cross-compile]

   go:spike:linux:
     stage: test
     script:
       - cd go
       - go test -v ./spike/...

   go:cross-compile:windows:
     stage: cross-compile
     script:
       - cd go
       - GOOS=windows GOARCH=amd64 go build -o /tmp/spike-win.exe ./spike

   go:cross-compile:darwin-arm64:
     stage: cross-compile
     script:
       - cd go
       - GOOS=darwin GOARCH=arm64 go build -o /tmp/spike-darwin ./spike
   ```

   Cross-compile jobs are compile-only — proves the toolchain premise itself.

### Verification for Step 1

1. `(cd rust && cargo fmt --all -- --check && cargo clippy --all-targets -- -D warnings && cargo test)` — 224 tests pass.
2. Local `git commit` of the restructure — pre-commit hook fires correctly in
   the new layout.
3. Push branch — Go-only GitLab CI config runs (spike job may fail until
   Step 2's code lands; cross-compile jobs run on whatever's in `go/`).

## Step 2 — Go-git spike

### Layout

```
go/
├── go.mod                      # module inat
├── go.sum
└── spike/
    ├── spike_test.go           # one TestX per operation
    ├── fixture.go              # in-process bare-git fixture builder
    └── RESULTS.md              # written after spike runs
```

### Tests in `spike_test.go`

1. `TestClone` — `git.PlainClone(target, false, &git.CloneOptions{URL: fixtureURL})`; assert HEAD readable, `.git/` exists.
2. `TestFetchIdempotent` — `git.PlainOpen(target).Fetch(...)`; expect `git.NoErrAlreadyUpToDate`.
3. `TestFetchPicksUpNewCommit` — append commit to fixture, refetch, assert tree SHA differs.
4. `TestReadBlob` — `Head() → CommitObject → Tree → File("registry.json") → Contents()`; byte-equal to fixture.
5. `TestWalkSubtree` — `tree.Tree("kits/test-kit").Files().ForEach(...)`; collect `map[string][]byte`; assert exact match.
6. `TestSubpathFixture` — second fixture under `ai-resources/`; walk
   `ai-resources/kits/test-kit/`; mirror of Rust's
   `fetch_kit_tree_at_finds_kit_under_subpath`.

Spike succeeds iff `go test ./spike/...` exits 0.

### Fixture (`fixture.go`)

Mirror `rust/src/git/source/test_support.rs::create_test_source` exactly:

```
registry.json                                 → {"version":"1","kits":{"test-kit":{"description":"Test kit","types":["skill","rule"]}}}
kits/test-kit/kit.json                        → minimal kit: 1 skill + 1 rule
kits/test-kit/skills/review/SKILL.md          → "# Review skill"
kits/test-kit/rules/no-secrets.md             → "Never commit secrets."
```

Build the bare repo with `go-git` itself (`git.PlainInit(..., true)` plus a
worktree clone, write blobs, commit, push). No external `git` binary
dependency.

### Out of scope for the spike

- HTTPS credential helper / SSH key auth — real porting question, but not a
  go/no-go blocker on basic operations.
- Performance benchmarking.
- Error-mapping fidelity (matching `InatError::Git` shapes).

### Verification for Step 2

1. `(cd go && go test -v ./spike/...)` locally — 6/6 pass.
2. CI `go:spike:linux` job green.
3. CI cross-compile jobs green.
4. `go/spike/RESULTS.md` committed with: go-git version, six results, caveats,
   recommendation.

## Step 3 — Decision gate

After Step 2 lands and CI is green:

- **6/6 + cross-compile work**: open `docs/specs/0009-go-port.md` for the full
  port. Port module-by-module; Rust tree stays authoritative until Go proves
  parity in production.
- **Any test fails or needs ugly workarounds**: document in `RESULTS.md`.
  Restore Rust CI by moving the `.rust-backup` copy back over the GitLab CI config plus the
  regex update for `rust/src/`. Pursue `cargo-xwin` for Windows-cross only;
  accept macOS-from-source.

## Files changed (summary)

**New**
- `docs/plan/0008-repo-restructure-and-go-spike.md` (this plan)
- `go/go.mod`, `go/go.sum`
- `go/spike/spike_test.go`
- `go/spike/fixture.go`
- `go/spike/RESULTS.md`
- the GitLab CI config's `.rust-backup` copy

**Renamed (`git mv`)**
- `src/` → `rust/src/`
- `tests/` → `rust/tests/`
- `spike/` → `rust/spike/`
- `Cargo.toml` → `rust/Cargo.toml`
- `Cargo.lock` → `rust/Cargo.lock`

**Rewritten**
- the GitLab CI config

**Modified (path-only updates)**
- `.githooks/pre-commit`
- `ci/build-release.sh`, `ci/build-release.ps1`
- `ci/verify-release-prereqs.sh`, `ci/windows-lint.ps1`
- `README.md`
- `docs/guidelines/coding-standards.md`
- `docs/demo-walkthrough.md`
- `CLAUDE.md`

## Out of scope

- Rust behavior changes — pure rename + CI cutover.
- Full Go port — separate spec, gated on this spike's outcome.
- HTTPS credential helper / SSH key auth in the spike.
- Performance benchmarking.
- Codesigning / notarization.
- Removing the Rust tree.
