# Plan — Full Go port (spec 0009)

Spec: [`docs/specs/0009-go-port.md`](../specs/0009-go-port.md).

## Why

Plan 0008 validated `go-git` against the six git operations `inat` performs (spike CI pipeline, 6/6) and proved Linux-runner cross-compile to windows/amd64 + darwin/{amd64,arm64} works with zero extra toolchain. Spec 0009 is the contract for the rewrite. This plan is the concrete rollout: per-module port order, branch model, milestones, cutover trigger.

## Decisions (locked via grilling)

1. **Auth model** — mirror Rust: parse `credential.helper` output, hand BasicAuth to `go-git`. SSH agent for SSH URLs.
2. **Module path** — the old GitLab module path in `go.mod` (since moved to `github.com/schubergphilis/inat` by spec 0027).
3. **Port order** — bottom-up: pure logic first (`naming`, `hash`, `registry`, `kit`, `config`), then `validate`, then `delivery` (filesystem), then `git/source` (network + auth), then `agents`, then `cli/*`, then `main.go`.
4. **CLI library** — `spf13/cobra`. Mirrors clap's subcommand nesting; native `--help`.
5. **Layout** — mirror `rust/src/` under `go/`. Skip Go's `cmd/`/`internal/` convention; this is a single-binary CLI without library users.
6. **Test strategy** — rewrite Go-idiomatically; assert behavior parity, not test-count parity.
7. **Branching** — long-lived `feat/0009-go-port` off `main`. Rebase weekly. Rust tree on `main` stays untouched until cutover commit.
8. **Cutover trigger** — M3 (full feature parity + Linux smoke green) on a single PR. Same PR deletes `rust/`, deletes the `.rust-backup` copy of the GitLab CI config, restores release jobs to invoke `go build`. Released as v0.3.0.

## Three milestones

### M1 — pure-logic parity (no CLI yet)

Port the side-effect-free modules. Go binary doesn't build yet — these are library packages with their own tests. CI runs `go test ./core/...` only.

| # | Go package | Rust source | Notes |
|---|---|---|---|
| 1 | `core/error` | `rust/src/error.rs` | `InatError` equivalents. Use typed errors with `errors.Is/As` support; expose `ErrAgentNotFound`, `ErrKitNotFound`, etc. as sentinel values. |
| 2 | `core/naming` | `rust/src/core/naming.rs` | Pure validation; trivial port. ~20 tests. |
| 3 | `core/hash` | `rust/src/core/hash.rs` | SHA-256 of bytes/files. Trivial. |
| 4 | `core/registry` | `rust/src/core/registry.rs` | Parse + validate `registry.json`. JSON schema is identical. |
| 5 | `core/kit` | `rust/src/core/kit.rs` | Parse + validate `kit.json`; `derive_entry_name`. JSON schema identical. |
| 6 | `core/config` | `rust/src/core/config.rs` | `.inat/config.json` + `~/.inat/config.json`. Field names match Rust's serde output exactly (`kits_repo` field included). Round-trip with Rust binary's output is mandatory. |

M1 gate: `go test ./core/...` green; round-trip test loads a Rust-written config without diffs.

### M2 — end-to-end parity (Go binary works against fixtures)

Port the I/O + git + CLI layers. Go binary builds and behaves equivalently to the Rust one for every command. Released artifacts still come from Rust; this is parity-only.

| # | Go package | Rust source | Notes |
|---|---|---|---|
| 7 | `core/validate` | `rust/src/core/validate/{mod,issue,loader}.rs` | Filesystem walker + issue enumeration. Reuses `core/registry` + `core/kit`. |
| 8 | `core/delivery` | `rust/src/core/delivery/{copy,managed_section,managed_write}.rs` | File copy + CLAUDE.md managed-section parser/renderer. Byte-for-byte identical output. |
| 9 | `agents` + `agents/builtin` | `rust/src/agents/{mod,claude}.rs` + `rust/src/agents/builtin/*` | `claude-code` profile; `inat_usage.md` and `inat_kits.md` embedded via `//go:embed`. |
| 10 | `git/cache` | `rust/src/git/cache.rs` | `~/.inat/cache/<source>/` path math. |
| 11 | `git/source/repo` | `rust/src/git/source/repo.rs` | `clone_or_fetch` + auth (`credential.helper` integration). **Largest porting risk** — auth model differs from `git2`. |
| 12 | `git/source/tree` | `rust/src/git/source/tree.rs` | `fetch_registry_at`, `fetch_kit_tree_at`, `walk_kit_files` — already proven in `go/spike/`. |
| 13 | `cli/init` | `rust/src/cli/init.rs` | `inat init [--agent] [--kits-repo]`. |
| 14 | `cli/sources` | `rust/src/cli/sources/{mod,ops,path}.rs` | `inat sources add/remove/list`. |
| 15 | `cli/agents` | `rust/src/cli/agents.rs` | `inat agents add/remove/list [--global]`. |
| 16 | `cli/install` | `rust/src/cli/install.rs` | `inat install <kit>`. |
| 17 | `cli/update` | `rust/src/cli/update.rs` | `inat update [<kit>] [--force]`. |
| 18 | `cli/remove` | `rust/src/cli/remove.rs` | `inat remove <kit> [--force]`. |
| 19 | `cli/refresh` | `rust/src/cli/refresh.rs` | `inat refresh [--force]`. |
| 20 | `cli/status` | `rust/src/cli/status.rs` | `inat status`. |
| 21 | `cli/validate` | `rust/src/cli/validate.rs` | `inat validate [PATH] [--subpath] [--json]`. |
| 22 | `main.go` + cobra wiring | `rust/src/main.rs` | Top-level `cobra.Command` tree. Subcommand routing. |

M2 gate:
- `go build ./...` green for `linux/amd64`, `windows/amd64`, `darwin/{amd64,arm64}`.
- A scripted comparison test (CI job: `parity:rust-vs-go`) initializes a temp project with the Rust binary, then runs the Go binary's `inat status` / `inat agents list` / `inat sources list` and asserts identical output.
- Smoke-test workflow runs Go binary end-to-end (init → install → status → remove) on Linux.
- All Go tests green.

### M3 — cutover

*Revised 2026-04-27.* Originally planned as one MR that deletes `rust/`. Per user instruction, `rust/` is **retained as an unmaintained archive** so we can refer back to the Rust implementation; only the active toolchain swaps to Go. Split into two MRs:

**M3a — pre-commit hook swap (this MR):**
1. Replace cargo fmt/clippy/test in `.githooks/pre-commit` with the Go equivalents: `gofmt -l`, `go vet`, `staticcheck`, `go test`. `staticcheck` is the closest analogue to `clippy::pedantic`; the hook blocks if it's not on PATH and `scripts/install-hooks.sh` installs it via `go install` when available.
2. Retarget the spec-presence regex from `^rust/src/` to `^go/`.
3. Add `*.go text eol=lf` (plus `go.mod` / `go.sum`) to `.gitattributes` so Windows checkouts don't violate `gofmt`.

**M3b — release pipeline cutover:**
1. Switch `ci/build-release.{sh,ps1}` from `cargo build` to `go build -ldflags "-X main.version=$(cat ../VERSION)"`.
2. Switch `ci/verify-release-prereqs.sh` from parsing `rust/Cargo.toml` to reading repo-root `VERSION`.
3. Restore the release/build/sign/package/upload jobs in the GitLab CI config from its `.rust-backup` copy, retargeted at the Go binary.
4. Update `README.md`, `docs/guidelines/coding-standards.md`, `CLAUDE.md` to describe the Go binary with `rust/` retained as unmaintained archive.
5. Bump `VERSION` to `0.3.0` and add a `## [0.3.0]` section to `CHANGELOG.md`.
6. Tag `v0.3.0` after merge to trigger the release pipeline.

`rust/` and the `.rust-backup` CI config are **kept** in both MRs.

Acceptance: tag `v0.3.0`, release pipeline green, `inat install` against a real source from the v0.3.0 binary works, `install.sh` / `install.ps1` keep working unchanged.

## Branch + rebase model

- `feat/0009-go-port` lives off `main`. M1 and M2 work happens on this branch.
- Weekly: `git rebase main` to absorb any Rust-side fixes that ship to `main` (unlikely during the port, but possible).
- M1 lands as one merge commit (small enough to review in one pass); M2 lands as one big merge commit (the rewrite); M3 is its own MR (the cutover).
- After M3 merges, the branch is deleted.

Three MRs total. Each has its own milestone gate; reviewers are expected to verify the gate criteria before approving.

## CI changes during the port

The GitLab CI config (currently Go-only spike + cross-compile sanity) gains a new job at M1:

```yaml
go:test:linux:
  stage: test
  script:
    - cd go
    - go mod tidy
    - go test ./...
```

At M2, add `go:build:cross-compile-all` that produces release-shaped artifacts for all four targets (without the `rcodesign` ad-hoc signing yet — that's an M3 concern).

At M3, replace the Go-only file with the proper release pipeline that combines build + sign + package + upload to GitLab generic packages, mirroring the Rust pipeline preserved in the `.rust-backup` CI config.

## Verification per milestone

### M1
1. `(cd go && go mod tidy && go test ./core/...)` — green.
2. Round-trip test: write a `.inat/config.json` from the Rust binary, load it via Go's `core/config`, assert all fields preserved exactly. Also reverse direction.
3. CI's `go:test:linux` job green on the M1 MR.

### M2
1. `(cd go && go build -o /tmp/inat-go .)` — produces a binary on Linux.
2. `GOOS=windows GOARCH=amd64 go build -o /tmp/inat.exe .` — green; `GOOS=darwin GOARCH=arm64 go build` — green.
3. End-to-end smoke against a temp dir:
   ```
   /tmp/inat-go init --kits-repo --agent claude-code
   /tmp/inat-go agents list
   /tmp/inat-go validate
   ```
4. Parity-test job in CI compares Go vs Rust output for each subcommand against shared fixtures.
5. `(cd go && go test ./...)` — every package green.

### M3
1. `cargo build --release --target x86_64-unknown-linux-gnu` is no longer invoked anywhere.
2. `rust/` is gone from `main`.
3. `git tag v0.3.0 && git push --tags` triggers the new release pipeline; artifacts land in GitLab generic packages.
4. Manual smoke: `install.sh` from a fresh shell pulls the v0.3.0 binary and `inat status` runs.

## Open items / risks

- **Auth on Windows** — *verified 2026-04-27.* Git for Windows 2.51 ships `credential.helper=manager` (in `C:/Program Files/Git/etc/gitconfig`). `git credential fill` returns the stored PAT cleanly. The Go port spawns `git credential fill` directly (`go/git/source/auth.go::credentialFill`) and hands the result to `go-git` as `http.BasicAuth` — exercised end-to-end against a private GitLab HTTPS URL on Windows: clone succeeds, cache populates, only the kit-source registry check (correctly) fails because the repo is not a kits-publishing repo.
- **`go-git` HEAD ref handling** — spike test used `firstOriginRef` to be tolerant of `master` vs `main`; production code in `git/source/repo.go` needs the same robustness.
- **Binary size** — Go binaries strip-after-build are typically 8–15 MB; Rust + git2 currently ~15–20 MB. Likely a wash. Document in M2's `RESULTS.md` equivalent.
- **`ci/verify-release-prereqs.sh`** — *VERSION file landed 2026-04-27.* Repo-root `VERSION` is the Go-side source of truth (M2 follow-up MR). M3 will switch the script to read it instead of `rust/Cargo.toml`. The Go binary already injects the value at link time via `-ldflags "-X main.version=$(cat ../VERSION)"`.
- **macOS notarization** — even with ad-hoc signing, downloaded binaries hit Gatekeeper. Out of this spec; tracked separately when SBP procures an Apple Developer account.

## Out of scope (deferred to follow-up specs)

- Performance benchmarks (`go-git` vs `git2` on large repos).
- Public Go module API (no `go install` story for v0.3.0).
- v1.0.0 promise — separate API-stability spec when ready.
- macOS notarization automation.
- Windows code signing (Authenticode).
- Telemetry / observability.
- Plugin / extension architecture.
