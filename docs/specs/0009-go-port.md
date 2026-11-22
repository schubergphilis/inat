## inat — Full Go port Spec

---

### 1. Overview

Plan [`0008-repo-restructure-and-go-spike.md`](../plan/0008-repo-restructure-and-go-spike.md) restructured the repo into `rust/` + `go/` and validated `go-git` against the six git operations `inat` actually performs. The spike CI pipeline returned 6/6 plus successful cross-compile to `windows/amd64`, `darwin/amd64`, `darwin/arm64` from a Linux runner with zero extra toolchain. `go/spike/RESULTS.md` recommended greenlighting a full port.

This spec is the contract for that port: what gets rewritten, what's preserved, what ships when. The Rust tree under `rust/` stays authoritative throughout; Go grows alongside until parity is reached.

A companion implementation plan lives at `docs/plan/0009-go-port.md`.

---

### 2. Goals

1. The Go binary MUST preserve the existing CLI surface byte-for-byte: every command, flag, positional argument, exit code, and error message format that spec [`0001`](0001-initial-idea.md) §9 (and extensions in specs `0003`, `0006`, `0007`) defines.
2. The Go binary MUST be functionally equivalent for every operation the Rust binary performs against:
   - `.inat/config.json` schema (sources, installed kits, agents, `kits_repo`).
   - `~/.inat/config.json` global schema.
   - `~/.inat/cache/<source>/` cache layout.
   - `registry.json` and `kit.json` parsing/validation rules.
   - Naming regex and reserved names (`inat-usage`, `inat-kits`).
   - `CLAUDE.md` managed-section markers + delivery semantics.
   - File hashing (SHA-256) and modified-file detection.
   - Built-in `inat-usage` and `inat-kits` rule content (delivered byte-equal).
3. The Go binary MUST be cross-compilable from a single Linux runner to `linux/amd64`, `windows/amd64`, `darwin/amd64`, `darwin/arm64`. macOS-arm64 binaries MUST be ad-hoc-signed via `rcodesign` so they launch on Apple Silicon without manual `xattr` workarounds.
4. The Go binary MUST handle git authentication compatibly with the current Rust binary: read the user's git `credential.helper` configuration, invoke it, parse its `username=`/`password=` output, and pass to `go-git` via `transport/http.BasicAuth`. SSH operations use the running ssh-agent.
5. The port MUST mirror the `rust/src/` module structure under `go/` so reviewers can read both trees in parallel during the transition.
6. The port MUST be test-covered: every behavior asserted by the 224 Rust tests MUST have an equivalent Go test asserting the same outcome (not necessarily 1:1 function count).
7. The port MUST land via a long-lived branch off `main`, with periodic rebases. The Rust tree stays unchanged on `main` until the Go cutover commit. CI is Go-only since plan 0008.

---

### 3. Non-goals

- **No CLI surface changes.** No new commands, no renamed flags, no changed exit codes. If a flag was a footgun in Rust, it stays a footgun in Go. Improvements ship under separate specs after parity.
- **No on-disk format changes.** A user's `.inat/config.json` written by the Rust binary MUST be round-trippable by the Go binary and vice versa.
- **No new `InatError` semantics.** Error messages may differ in casing/wording where Go idioms force it (e.g. `errors.Is` chains), but the *kinds* and the user-visible top-level texts stay equivalent.
- **No performance work.** Spike measured `go-git` is fast enough for `inat`'s small kit repos. Benchmarking + perf tuning are out of scope for v1; revisit only if regressions surface in production.
- **No new dependencies beyond the allow-list.** See §6.
- **No new agent profiles.** `claude-code` is the only profile in v1, same as Rust.
- **No public Go module API.** The crate is a CLI binary, not a library. No `go install …/inat` use cases promised.

---

### 4. Module path and layout

`go.mod` declares (module path shown as moved by spec 0027; originally the internal GitLab path):

```go
module github.com/schubergphilis/inat

go 1.22
```

The Go tree mirrors `rust/src/` one-for-one:

```
go/
├── go.mod
├── go.sum
├── main.go                            // entry point (was rust/src/main.rs)
├── cli/                               // command implementations
│   ├── init/init.go
│   ├── sources/{add,remove,list}.go
│   ├── agents/{add,remove,list}.go
│   ├── install/install.go
│   ├── update/update.go
│   ├── remove/remove.go
│   ├── refresh/refresh.go
│   ├── status/status.go
│   └── validate/validate.go
├── core/
│   ├── config/config.go               // .inat/config.json + global
│   ├── kit/kit.go                     // kit.json parsing + validation
│   ├── registry/registry.go           // registry.json parsing + validation
│   ├── delivery/{copy.go,managed_write.go,managed_section.go}
│   ├── naming/naming.go               // [a-z0-9]([a-z0-9-]*[a-z0-9])? regex
│   ├── hash/hash.go                   // SHA-256
│   └── validate/{validate.go,issue.go,loader.go}
├── agents/
│   ├── agents.go                      // profile registry
│   ├── claude.go                      // claude-code profile
│   └── builtin/
│       ├── builtin.go                 // shared deliver/remove helpers
│       ├── inat_usage.md              // embedded via go:embed
│       └── inat_kits.md               // embedded via go:embed
├── git/
│   ├── cache/cache.go                 // ~/.inat/cache path math
│   └── source/{repo.go,tree.go}
├── error/error.go                     // InatError equivalents
└── spike/                             // kept as a reference; deleted at cutover
```

The `go/spike/` tree from plan 0008 stays during the port (useful regression check), gets deleted in the same commit that retires `rust/`.

Tests live alongside the code they test: `core/config/config_test.go`, etc. — Go's standard layout.

---

### 5. Auth model (port spec)

Mirrors `rust/src/git/source/repo.rs::fetch_options`:

1. Read git config via `go-git`'s config reader or by shelling out to `git config` (whichever is more reliable on Windows — TBD per plan).
2. For each clone/fetch, build a `transport/http.AuthMethod`:
   - If the URL is HTTPS, look up `credential.helper` for that origin, invoke it, parse `username=` and `password=` from stdout, return `&http.BasicAuth{Username, Password}`.
   - If the URL is SSH, use `transport/ssh.NewSSHAgentAuth(user)`; the user defaults to "git".
   - If neither helper nor agent yields credentials, fall through with no auth (lets public repos work).
3. Capture authentication failures and return an `InatError::Git`-equivalent typed error.

Out of scope for v1: OS keychain integration beyond what the user's `credential.helper` already wires (Windows Credential Manager, macOS Keychain, libsecret on Linux all become reachable via standard git config).

---

### 6. Dependency allow-list

| Purpose | Crate (Go) | Justification |
|---|---|---|
| git operations | `github.com/go-git/go-git/v5` | The whole reason for the port. v5.13.x pinned. |
| CLI parsing | `github.com/spf13/cobra` | Native subcommand nesting matches `inat sources add`. Generates `--help` automatically. Pulls `spf13/pflag`. |
| Embedded files | stdlib `embed` | Built-in rule content (`inat_usage.md`, `inat_kits.md`). |
| Logging | stdlib `log/slog` | Stable since Go 1.21. Stderr structured logging. |
| JSON | stdlib `encoding/json` | Config, registry, kit, validate output. No third-party JSON. |
| Hashing | stdlib `crypto/sha256` | File hashing for delivery integrity. |
| Filesystem | stdlib `os`, `io/fs`, `path/filepath` | All filesystem ops. |
| Testing | stdlib `testing` | No third-party test framework. |
| macOS code-signing | `github.com/indygreg/apple-platform-rs` (rcodesign binary) | Used at release time to ad-hoc-sign `darwin/arm64`. Not a Go dep — invoked as a subprocess in CI. |

Anything else requires a spec amendment.

---

### 7. Test strategy

Every behavior asserted in the 224 Rust tests MUST have an equivalent Go test. Strategy:

- **Rewrite Go-idiomatically.** Don't try to map `#[cfg(test)] mod tests` modules 1:1 — Go's `*_test.go` next-to-file convention is the target.
- **Table-driven where it fits** (naming validation, kit validation, error display strings).
- **Golden files for fixtures.** The `core/registry`, `core/kit`, and `core/validate` packages each get a `testdata/` directory with the same inputs the Rust tests use.
- **`testing.T.TempDir()`** for filesystem fixtures (replaces `tempfile::tempdir()`).
- **No mocks for git.** Reuse the spike's `NewFixture` pattern (in-process bare-clone-from-worktree) for `git/source/*` tests. Keep `go/spike/fixture.go` available as the canonical example.
- **Integration tests** (`go/integration_test.go`?) cover end-to-end flows: `init --kits-repo --agent claude-code` followed by `validate`, `install`, `agents add/remove`. Mirrors the smoke-tests at the bottom of the plan-0007 verification.

CI must report at least one Go test per behavioral parity claim. Per-module ports do not merge without their tests passing.

---

### 8. Release / CI cutover

Three milestones, each gated:

1. **Pure-logic parity** (M1) — `core/{naming,hash,registry,kit,config}` ported with green tests. No CLI yet. Go binary doesn't exist yet.
2. **End-to-end parity** (M2) — every `inat <subcommand>` invocation that the Rust binary supports works in Go against the same fixtures. CI cross-compiles to all four targets, smoke-tests on Linux. The Rust binary is still what ships.
3. **Cutover** (M3) — release engineering switches `ci/build-release.{sh,ps1}` to invoke Go (`go build`) instead of Rust (`cargo build`). The `rust/` tree and the `.rust-backup` copy of the GitLab CI config are deleted in the same commit. Released as v0.3.0 (no user-visible CLI changes — just a new binary).

`v1.0.0` is **not** part of this spec. A version bump to 1.x signals API stability promises that go beyond "the rewrite shipped"; defer to a separate spec.

---

### 9. References

- [`0001-initial-idea.md`](0001-initial-idea.md) — original CLI surface contract; preserved verbatim.
- [`0003-source-subpath.md`](0003-source-subpath.md) — subpath behaviour preserved.
- [`0006-agents-global-flag.md`](0006-agents-global-flag.md) — `--global` semantics preserved.
- [`0007-kits-authoring-support.md`](0007-kits-authoring-support.md) — `init --kits-repo`, `inat-kits` rule, `inat validate` preserved.
- [`docs/plan/0008-repo-restructure-and-go-spike.md`](../plan/0008-repo-restructure-and-go-spike.md) — restructure + spike that this port builds on.
- [`go/spike/RESULTS.md`](../../go/spike/RESULTS.md) — spike's evidence base.
- [`docs/plan/0009-go-port.md`](../plan/0009-go-port.md) — implementation plan.

---

### 10. Acceptance

The spec is satisfied when:

1. The Go binary's `--help` output for every command + subcommand byte-equals the Rust binary's, modulo cobra's built-in formatting differences (which are stable across `inat` versions).
2. Round-trip: a `.inat/config.json` written by the Rust binary loads cleanly into the Go binary and vice versa, with no field loss or reordering.
3. `inat init --kits-repo --agent claude-code` from the Go binary produces the same files (with byte-equal content) as the Rust binary did under spec 0007 acceptance test 1.
4. `inat install <source>/<kit>` against a fixture source delivers files with identical paths, identical hashes, and identical managed-section content.
5. `inat validate` against the same fixtures emits the same exit codes and (modulo wording) the same human/JSON output.
6. `inat agents add/remove` (project + `--global`) writes the same config files and delivers/removes the same managed-section references.
7. The Go binary cross-compiled from Linux for `darwin/arm64` launches on Apple Silicon (with `rcodesign` ad-hoc signing) without the `com.apple.quarantine` workaround being needed by the user *on the build host*. (Downloaded binaries still hit Gatekeeper without notarization — out of scope, see §3.)
8. Go binary size is documented in `RESULTS.md`-equivalent format. (No hard cap — informational only.)
9. Every test category in the Rust tree has a corresponding Go test category passing on `linux/amd64` CI. Test count parity not required; behavior parity is.
10. Release artifacts (`inat-<version>-<target>.tar.gz` / `.zip`) preserve their current naming so `install.sh` / `install.ps1` keep working.
