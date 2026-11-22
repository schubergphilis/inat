# inat Coding Standards

This document defines coding standards for the inat project. Read before writing any implementation code.

The standards apply to the Go tree at `go/`. The Rust tree at `rust/` is **unmaintained** (kept as a historical archive only) and is exempt from these rules.

## 1. Stack & Toolchain

- **Language:** Go
- **Module path:** `github.com/schubergphilis/inat`
- **Go directive in `go.mod`:** `go 1.22` (the source-compatibility floor)
- **Toolchain in CI:** Go 1.27 (`golang:1.27-alpine`), with `staticcheck` pinned to v0.8.2 (needs Go 1.26+). Bump the image and the pin together.
- **Execution model:** Synchronous — no goroutines for the CLI flow. All I/O is blocking via `os`, `path/filepath`, and `go-git`.
- **Project type:** Single-binary CLI (`go build .` from `go/` produces `inat`)
- **Formatting:** Default `gofmt` — no custom formatter config. Run `gofmt -w .` before every commit.

## 2. Project Structure

Single Go module under `go/` with two-level domain packages. Each top-level package groups a domain concern; subpackages handle distinct responsibilities within that domain.

```
go/
  main.go              — entry point, cobra command tree, dispatch
  cli/
    init/init.go       — inat init
    sources/sources.go — inat sources add/remove/list
    agents/agents.go   — inat agents add/remove/list
    install/install.go — inat install
    update/update.go   — inat update
    remove/remove.go   — inat remove
    refresh/refresh.go — inat refresh
    status/status.go   — inat status
    validate/validate.go — inat validate
  core/
    config/config.go   — read/write .inat/config.json
    kit/kit.go         — kit.json parsing, validation
    registry/registry.go — registry.json parsing
    delivery/copy.go,
            managed_section.go,
            managed_write.go — file copy + managed-section strategies
    naming/naming.go   — name validation rules
    hash/hash.go       — SHA-256 file hashing
    validate/validate.go — offline kits-repo validator
    reserved/reserved.go — reserved rule names (leaf, no deps)
  git/
    cache/cache.go     — ~/.inat/cache/ management
    source/source.go,
           tree.go,
           auth.go     — clone, fetch, tree reads, credential.helper
  agents/
    agents.go          — agent profile registry
    builtin/rules.go   — built-in inat-usage / inat-kits delivery
  errs/errs.go         — typed error type (mirrors Rust's InatError)
  internal/testfixture/testfixture.go — shared CLI test fixtures
```

**Rules:**
- Each package directory contains files with the package's domain logic. Avoid catch-all `util` packages.
- Use lowercase, short package names (Go convention). Exported identifiers carry the domain + action (e.g. `delivery.DeliverSkill`, not `delivery.Skill`).
- When a file exceeds the file size limit, split by responsibility — see `core/delivery/` (split into `copy.go`, `managed_section.go`, `managed_write.go`) for the canonical example.
- Tiny shared-constant leaves (e.g. `core/reserved/`) are encouraged when a constant is referenced from packages that would otherwise form an import cycle.

## 3. Approved Dependencies

| Module | Purpose | Notes |
|---|---|---|
| `github.com/spf13/cobra` | CLI command tree | clap analogue; subcommand nesting + `--help` |
| `github.com/go-git/go-git/v5` | Git operations | Pure-Go libgit2 replacement; cross-compiles trivially |
| Standard library | Everything else | `encoding/json`, `os`, `path/filepath`, `crypto/sha256`, `errors`, … |

Test-only:
- `testing` (standard library) — `t.TempDir`, table-driven tests, etc.
- The `internal/testfixture` package builds disposable git remotes for CLI integration tests via `go-git`.

### Adding a New Dependency

Any module not on this list requires justification before use:

1. **Why can't an approved module handle this?** — Explain what's missing.
2. **Maintenance status** — Last release date, number of maintainers, bus factor.
3. **Transitive dependency count** — `go mod why`/`go mod graph` review.
4. **Security audit status** — Check the [Go vulnerability database](https://pkg.go.dev/vuln/) (`govulncheck`) for known issues.

## 4. Error Handling

Use a single typed error (`errs.Error`) for the domain error model. Wrap underlying errors via the `WrapIO`, `WrapJSON`, `WrapGit` helpers. Inspect with `errors.Is` (kind-only) and `errors.As` (kind + parameters).

```go
// Sentinel-style match (kind only):
if errors.Is(err, errs.ErrAgentNotFound) {
    // ...
}

// Detail-style match (kind + parameters):
var e *errs.Error
if errors.As(err, &e) && e.Kind == errs.AgentNotFound && e.Name == "claude-code" {
    // ...
}
```

**Rules:**
- Never return raw `error` values for domain failures from public APIs — return `*errs.Error` (or wrap on the way out).
- Never `_ = err` to silently discard. If an error genuinely doesn't matter, document why with one comment line.
- Never `panic` in production code paths; reserve `panic` for `init`-time invariants that genuinely cannot fail at runtime. The CLI surface should always return errors up the stack.
- Error messages are lowercase, no trailing punctuation, and include relevant values (paths, names). The texts in `errs.Error.Error()` mirror Rust's `#[error(...)]` attributes verbatim so historical assertions still match.

## 5. Code Size Limits

| Metric | Target | Hard Maximum | Exceptions |
|---|---|---|---|
| File length | ≤300 lines | 400 lines | `errs/errs.go` and pure type/struct definition files |
| Function length | ≤30 lines | 50 lines | `switch`/dispatch where each case is 1–3 lines |

**Rules:**
- When splitting a file, extract by **responsibility**, not by arbitrary line count.
- Closures (function literals) count toward the enclosing function's line count.
- If a function needs an inline comment explaining a section, that section should be extracted into its own function.

## 6. Naming Conventions

### Verb Prefixes

Standardize function names with these verb prefixes to make behavior predictable across the codebase:

| Prefix | Domain | Example |
|---|---|---|
| `Load*` / `Save*` | Local file I/O | `LoadConfig()`, `SaveConfig()` |
| `Fetch*` | Network / git operations | `FetchSource()`, `FetchRegistry()` |
| `Parse*` / `Validate*` | Input processing | `ParseKit()`, `ValidateName()` |
| `Deliver*` / `Remove*` | File delivery operations | `DeliverSkill()`, `RemoveDeliveredFiles()` |
| `Is*` / `Has*` | Boolean queries | `IsModified()`, `Has…()` |
| `Ensure*` | Idempotent setup | `EnsureCacheDir()`, `EnsureConfig()` |

### General Rules

- Public functions include **action + target**: `DeliverSkill()` not `Deliver()`.
- No abbreviations in public APIs: `LoadConfig()` not `LoadCfg()`.
- `MixedCaps` for exported identifiers and unexported multi-word identifiers (Go convention — never `snake_case`, never `under_score`).
- Type names are singular nouns: `KitConfig`, `Source`, `DeliveredFile`.
- Package names are lowercase, short, single-word where possible: `config`, `delivery`, `naming`.
- Constants use `MixedCaps` too — `BuiltinRuleName`, not `BUILTIN_RULE_NAME`.

## 7. Linting Configuration

Three tools, all enforced by both the local pre-commit hook and CI:

```sh
gofmt -l .          # formatting drift; non-empty list = fail
go vet ./...        # built-in static analysis
staticcheck ./...   # clippy-equivalent — install with:
                    #   go install honnef.co/go/tools/cmd/staticcheck@v0.8.2
go test ./...       # full test suite
```

`scripts/install-hooks.sh` installs `staticcheck` for you on first run when Go is on PATH.

**Rules:**
- The `unsafe` package is **forbidden**. There are no low-level operations in inat that require it; if a use case appears, surface it for review and update this rule.
- `staticcheck` is enabled at the project's default profile. Address findings as they arise — do not blanket-allow them without justification.
- CI's `go:test:linux` job runs the same four-tool gate as the hook; the hook and CI cannot disagree.

## 8. Go Pattern Rules

### Do

- **Return errors with `errs.New…`/`errs.Wrap…` constructors** — keeps the domain error model uniform.
- **Prefer plain types in function parameters** — `string`, `[]byte`, `*Config`. Avoid `interface{}` / `any` unless you genuinely need polymorphism.
- **Prefer `for ... := range` over manual index loops** when iterating slices or maps.
- **Use `t.TempDir()`** in tests — automatic cleanup, no leftover state on test failure.
- **Use `t.Helper()`** in test fixture helpers so failure lines point at the test, not the helper.
- **Use `errors.Is` / `errors.As`** to inspect errors — never compare error strings.

### Don't

- **No `panic` in CLI code paths** — return errors up the stack. `panic` is reserved for `init`-time invariant violations.
- **No silent error discard** — `_ = err` is not allowed. If you genuinely don't care about an error, document why with a single comment line.
- **No `interface{}` / `any` parameters** unless polymorphism is actually required.
- **No goroutines / channels in the CLI flow** — see §1. Concurrency is a future-spec concern.
- **No `fmt.Println` for warnings or progress** — use `fmt.Fprintln(os.Stderr, …)` for messages, and reserve `fmt.Println` for data output to stdout that can be piped.
- **No `init()` functions for application logic** — use explicit constructors. `init()` is only acceptable for things like registering a flag default.

## 9. Testing (TDD-First)

**Write tests before implementation.** Every feature or function should start with a failing test that defines the expected behavior, followed by the minimal implementation to make it pass.

### Unit Tests

Inline in each package as `<file>_test.go`:

```go
// In go/core/naming/naming_test.go
package naming

import "testing"

func TestValidNameAccepted(t *testing.T) {
    if err := ValidateName("my-kit"); err != nil {
        t.Fatalf("expected ok, got %v", err)
    }
}

func TestConsecutiveHyphensRejected(t *testing.T) {
    if err := ValidateName("my--kit"); err == nil {
        t.Fatalf("expected rejection")
    }
}
```

Best suited for: name validation, config serialization/deserialization, hash comparison, managed section parsing.

### Integration Tests

CLI integration tests live alongside the package they exercise (e.g. `cli/install/install_test.go`). They use the shared fixture builder:

```go
import "github.com/schubergphilis/inat/internal/testfixture"

func TestInstallHappyPath(t *testing.T) {
    ctx := testfixture.CreateTestRemote(t)
    project := testfixture.SetupProject(t, ctx.URL)
    home := t.TempDir()
    if err := Install(project, "test-source/test-kit", home); err != nil {
        t.Fatalf("install: %v", err)
    }
    // …assertions
}
```

The fixture builder spins up a real go-git worktree as the "remote", populated with a known-good registry + kit, so tests assert against the same shape that production code reads.

### Rules

- **No mocking** of git, filesystem, or config. Test against real temp directories and real go-git repos.
- Every public function in `core/` should have unit tests.
- Every CLI command should have at least one integration test covering the happy path.
- `internal/testfixture` is a test-only package — never imported by production code.

## 10. Documentation

- `// …` doc comments are **required on all exported items** — functions, types, constants, package-level vars.
- Skip doc comments on unexported items unless the logic is non-obvious.
- The first line of a doc comment starts with the identifier name (Go convention): `// LoadConfig reads …`, not `// Loads the config …`.
- Doc comments describe **what** and **why**, not **how** (the code shows how).
- Use `// Returns *errs.Error with Kind=ConfigNotFound when …` to document failure modes.

```go
// LoadConfig reads and parses `<dir>/.inat/config.json`.
//
// Returns *errs.Error with Kind=ConfigNotFound when the file does not
// exist, Kind=JSON on parse failure, Kind=IO on read failure.
func LoadConfig(dir string) (Config, error) {
    // …
}
```

## 11. Enforcement

Three tiers of enforcement, from hardest to softest:

### Tier 1: Toolchain (blocks build/test)

| Rule | Mechanism |
|---|---|
| Type safety | Go type system |
| Unused code/imports | `go build` errors |
| `gofmt` drift | `gofmt -l .` |
| Static analysis | `go vet ./...` |
| `staticcheck` findings | `staticcheck ./...` |

### Tier 2: CI Pipeline (blocks merge)

| Check | Command |
|---|---|
| All tests pass | `go test ./...` |
| No `staticcheck` findings | `staticcheck ./...` |
| Code formatted | `gofmt -l .` (must print nothing) |
| `go vet` clean | `go vet ./...` |
| File length >400 lines | Reviewer eyeballs (no automated check yet) |
| CLI output parity with v0.2 Rust | `parity:rust-vs-go` job (until intentionally diverged) |

### Tier 3: CLAUDE.md Instructions (guides AI agent)

| Guideline | Where Documented |
|---|---|
| Naming verb conventions | This document, section 6 |
| TDD-first workflow | This document, section 9 |
| Dependency allowlist | This document, section 3 |
| Code organization patterns | This document, section 2 |
| 300-line soft file limit | This document, section 5 |
| Go pattern do/don't rules | This document, section 8 |
