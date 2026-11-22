# Plan — Kit-authoring support (`init --kits-repo`, `inat-kits` rule, `inat validate`)

Spec: [`docs/specs/0007-kits-authoring-support.md`](../specs/0007-kits-authoring-support.md).

## Why

`inat` today is entirely consumer-facing. Authoring a kit means reading spec 0001 §5 and §8 by hand, eyeballing the existing sources in another tab, and hoping the first `inat install` from a consumer repo doesn't explode. The error messages that surface on install are framed for the person *pulling* the kit, not the person *writing* it. CI for a kits repo has no inat-native check.

This plan lands three tightly-related pieces under a single feature: an authoring flag on `init`, a second built-in contextual rule that teaches the authoring contract in-context for the agent, and a `validate` command that enforces it offline. Together: **the rule teaches, the command enforces.**

## Decisions

- **Flag on `init`, not a new command.** `inat init --kits-repo` persists `kits_repo: true` in `.inat/config.json`, parallel to how `--agent` threads into the same config. Matches the existing one-shot init model; no new top-level verb for turning a project into a kits repo.
- **Rule name `inat-kits`, not `kits`.** Parallel with `inat-usage`: both built-ins share the `inat-` prefix and sit in `RESERVED_NAMES` together. `kits` alone collides semantically with the `kits/` directory inside a source repo.
- **Additive delivery, not alternative.** When `kits_repo: true` and an agent is configured, **both** `inat-usage` and `inat-kits` are delivered. Authors are also consumers of their tooling — they still benefit from the generic inat-usage rule.
- **Shared `deliver_builtin` / `remove_builtin` helpers.** The existing `deliver_inat_usage` / `remove_inat_usage` logic becomes the first caller of a generic helper; `deliver_inat_kits` / `remove_inat_kits` become the second. Avoids two near-identical branches.
- **`validate` is stateless and offline.** It does not read `.inat/config.json`. It walks the filesystem under a positional `PATH` (cwd default) with optional `--subpath DIR` to match spec `0003`'s source subpath convention. No git operations.
- **Reuse every existing parser and validator.** `parse_registry`, `validate_registry`, `parse_kit`, `validate_kit`, `validate_name`, `derive_entry_name` — no parallel implementation. The only new code is the filesystem walker that feeds them and the `Issue` enum that aggregates failures.
- **`Issue` is not an `InatError`.** A validate run produces many issues per invocation; collecting them in `Vec<Issue>` beats the `?`-propagating single-error model used elsewhere. CLI maps non-empty `Vec<Issue>` to a non-zero `ExitCode`.
- **Human output by default, `--json` for CI.** Human mode writes to stderr (errors) + summary; JSON goes to stdout with a stable `{path, issues: [...]}` shape emitted even on success.
- **Retro-fit and scaffolding are out of scope.** `inat init` stays one-shot; users flipping `kits_repo` post-hoc hand-edit the config. No starter `registry.json`/`kits/` creation; the rule + `validate` guide the author.

## Changes

### New

- `src/agents/builtin/inat_kits.md` — the rule content (~100–150 lines). Covers naming regex, `registry.json` and `kit.json` examples, skill/rule layout, `always` vs `contextual` priority, reserved names, `inat validate` invocation, pointer to spec 0001.
- `src/cli/validate.rs` — `run_validate(path, subpath, json)`: drives `core::validate::run`, formats `Vec<Issue>` as human or JSON, maps to `ExitCode`.
- `src/core/validate/mod.rs` — pure logic: `run(root: &Path, subpath: Option<&str>) -> Vec<Issue>`. Reads + walks the filesystem; delegates schema checks to existing parsers.
- `src/core/validate/issue.rs` — `Issue` struct with `kind: IssueKind`, `path: String`, `message: String`; `IssueKind` enum; `serde::Serialize` for JSON output.

### Modified

- `src/core/config.rs`
  - Add `#[serde(default)] pub kits_repo: bool` field to `Config`.
  - Extend `RESERVED_NAMES` with the `inat-kits` name (alongside `inat-usage`).
  - Test: serialization round-trip with the field set/unset; `check_name_uniqueness` rejects `inat-kits`.
- `src/agents/builtin/mod.rs`
  - Extract `deliver_builtin(rule_name: &str, content: &str, dir: &Path, agent_name: &str)` and `remove_builtin(rule_name: &str, dir: &Path, agent_name: &str)`.
  - Re-express `deliver_inat_usage` / `remove_inat_usage` as thin wrappers.
  - Add `BUILTIN_KITS_RULE_NAME = "inat-kits"`, `BUILTIN_KITS_RULE_CONTENT = include_str!("inat_kits.md")`, `deliver_inat_kits`, `remove_inat_kits`.
  - Update the `RESERVED_NAMES` pointer in `config.rs` to reference both constants.
  - Tests mirror the existing `inat-usage` coverage: deliver/idempotent-deliver/remove/remove-noop/unknown-agent for the new rule.
- `src/cli/init.rs`
  - `run_init(agent, kits_repo)`, `init(dir, agent, kits_repo)`.
  - Persist `kits_repo` into `Config` after `ensure_config`.
  - When `--agent` and `kits_repo` are both true, call `deliver_inat_kits` after `deliver_inat_usage`.
  - Tests: flag sets config field; with agent, both rules delivered; without agent, neither delivered; flag alone persists but delivers nothing.
- `src/cli/agents.rs`
  - `run_add`: after `deliver_inat_usage`, read `config.kits_repo`; if true, call `deliver_inat_kits`.
  - `run_remove`: after `remove_inat_usage`, always call `remove_inat_kits` (defensive).
  - Tests: `agents add` delivers both when `kits_repo: true`; delivers only `inat-usage` when `kits_repo: false`; `agents remove` cleans both in the kits-repo case.
- `src/main.rs`
  - `Init { agent, kits_repo }` — add `#[arg(long)] kits_repo: bool`.
  - New `Validate { path, subpath, json }` variant; dispatch to `cli::validate::run_validate`.
  - Map `run_validate`'s returned `ExitCode` (or equivalent) to the process exit.

### Reused (no change)

- `src/core/naming.rs::validate_name`
- `src/core/registry.rs::{parse_registry, validate_registry}`
- `src/core/kit.rs::{parse_kit, validate_kit, derive_entry_name}`
- `src/core/delivery/copy::deliver_rule_contextual`
- `src/core/delivery/managed_write::{add_contextual_reference, remove_contextual_reference}`

## Implementation order (TDD-first)

Every step: failing test → code → green → commit.

1. **Spec + plan** — this file and its sibling. Pre-commit hook passes once either exists.
2. **Config schema** — `kits_repo` field + reserved name extension, with tests.
3. **Builtin refactor + `inat-kits` hooks** — helper extraction, new rule constants, new deliver/remove functions, unit tests.
4. **Rule content** — author `inat_kits.md` (the `include_str!` target must exist for the module to compile).
5. **`init` flag** — thread `--kits-repo` through `run_init` / `init`; conditional dual delivery; tests.
6. **`agents add/remove`** — conditional delivery/always-clean of `inat-kits`; tests.
7. **Pure validate logic** — walker + `Issue` list, per-check TDD. Each `IssueKind` (MissingRegistry, InvalidRegistry, MissingKitJson, InvalidKit, MissingFile, InvalidName, DuplicateName, ReservedName) gets its own test.
8. **Validate CLI** — output formatting (human stderr + summary, JSON stdout), exit code mapping.
9. **`main.rs` wiring** — `Init` flag, `Validate` subcommand, dispatch.
10. **CLAUDE.md status table + smoke** — update the project table with spec 0007 row and the CLI command list, then run `cargo fmt && cargo clippy --all-targets -- -D warnings && cargo test`.

## Verification

1. `cargo fmt --all && cargo clippy --all-targets -- -D warnings && cargo test` — all green.
2. Temp-dir smoke:
   ```bash
   d=$(mktemp -d); cd "$d"
   inat init --kits-repo --agent claude-code
   test -f .inat/rules/inat-usage.md
   test -f .inat/rules/inat-kits.md
   grep -q '.inat/rules/inat-usage.md' CLAUDE.md
   grep -q '.inat/rules/inat-kits.md' CLAUDE.md
   jq '.kits_repo' .inat/config.json   # → true
   ```
3. Build a minimal valid kits repo (one kit, one rule), then run `inat validate` — expect exit 0, `validated 1 kit(s), 0 issue(s)`.
4. Break the fixture in three ways (delete the rule file, add a kit declaring a `inat-kits` entry, add a second kit exporting the same derived name), run `inat validate` — expect exit 1 and all three issue kinds reported.
5. Move `registry.json` and `kits/` under `pkg/`, run `inat validate --subpath pkg` — expect pass.
6. `inat validate --json` — expect a valid JSON document parseable by `jq '.issues | length'`.

## Out of scope

- Retro-fit command to toggle `kits_repo` without re-init.
- `inat scaffold kit <name>` scaffold command.
- Content-sanity warnings in `validate` (SKILL.md has heading, rule non-empty, etc.).
- CI workflow template / reusable GitHub Action for kits repos.
