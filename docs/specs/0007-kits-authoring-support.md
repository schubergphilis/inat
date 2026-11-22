## inat — Kit-authoring support Spec

---

### 1. Overview

Spec [`0001-initial-idea.md`](0001-initial-idea.md) defines how a consumer project installs and delivers kits. It does not describe **authoring** a kit: the `registry.json` / `kit.json` layout, naming rules, and priority semantics are stated in §5 and §8, but nothing in the tool itself teaches or enforces them at author time.

Today a kit author must read the spec by hand, and structural mistakes are surfaced only on a consumer's first `inat install`, with error messages framed for installers. CI for a kits repo has no inat-native validator.

This spec closes that gap with three connected additions under a single feature:

1. `inat init --kits-repo` — a new flag that marks the project as a kit-authoring repo by persisting `kits_repo: true` in `.inat/config.json`.
2. A second built-in contextual rule, `inat-kits`, embedded in the binary and delivered alongside `inat-usage` whenever `kits_repo: true` in a project that has an agent configured. The rule teaches the authoring contract that `inat validate` enforces.
3. `inat validate [PATH]` — a new top-level command that deterministically checks that a directory is a well-formed kits repository, reusing the existing `parse_registry` / `validate_registry` / `parse_kit` / `validate_kit` / `validate_name` / `derive_entry_name` functions against the local filesystem instead of a git tree.

A companion implementation plan lives at `docs/plan/0007-kits-authoring-support.md`.

---

### 2. Goals

1. `inat init --kits-repo [--agent <name>]` MUST persist `kits_repo: true` in `.inat/config.json`. The flag is independent of `--agent`: either can be supplied without the other.
2. When `--agent <name>` is also supplied, `inat init --kits-repo --agent <name>` MUST deliver **both** `inat-usage` and `inat-kits` contextual rules (file + managed-section reference). Plain `inat init --agent <name>` (no `--kits-repo`) MUST deliver **only** `inat-usage`, unchanged from spec 0005.
3. `inat agents add <name>` in a project whose config has `kits_repo: true` MUST additionally deliver `inat-kits`. `inat agents remove <name>` MUST clean both rules (defensively — regardless of current `kits_repo` state).
4. The name `inat-kits` MUST be reserved by `check_name_uniqueness`, on the same footing as `inat-usage` (spec 0005 §).
5. `inat validate [PATH]` MUST, statelessly and without network access, enumerate every structural defect in the given directory as a kits-publishing repo. Exit 0 iff zero issues.
6. `inat validate` MUST reuse the existing parsers and validators; no parallel validation logic may be introduced.
7. `inat validate --subpath <DIR>` MUST support repos where `registry.json` lives at a subdirectory (matching the `Source.path` field per spec `0003`).
8. `inat validate --json` MUST emit a machine-readable issue list suitable for CI consumption.
9. `inat init` remains one-shot; `ConfigAlreadyExists` stays the error for re-invocation. There is **no** toggle to flip `kits_repo` on an already-initialized project in this feature.

---

### 3. Config schema delta

`Config` in `.inat/config.json` gains one field:

```jsonc
{
  "sources": [...],
  "installed": {...},
  "agents": [...],
  "kits_repo": true              // new, default false
}
```

- Marked `#[serde(default)]` so existing configs without the field continue to load and equal `false`.
- `GlobalConfig` (`~/.inat/config.json`) is unchanged.

---

### 4. CLI surface

```
inat init [--agent <name>] [--kits-repo]
inat validate [PATH] [--subpath <dir>] [--json]
```

- `PATH` defaults to the current working directory when omitted.
- Existing `init` invocations without `--kits-repo` behave exactly as before.
- `validate` is a new top-level subcommand. It sits alongside `init`, `sources`, `agents`, `install`, `update`, `remove`, `refresh`, `status`.

Examples:

```
# Start a new kits repo with Claude Code as the authoring agent
inat init --kits-repo --agent claude-code

# Validate the current directory
inat validate

# Validate a checkout, with registry.json under ./pkg
inat validate ./some-source --subpath pkg

# Validate for CI
inat validate --json
```

---

### 5. Semantics

#### 5.1 `inat init --kits-repo`

1. Create `.inat/config.json` with `kits_repo: true` (via `ensure_config` + an explicit set before `save_config`).
2. If `--agent <name>` is also supplied: validate the profile, push onto `config.agents`, save, then call `deliver_inat_usage(dir, name)` followed by `deliver_inat_kits(dir, name)`.
3. If `--agent` is omitted: persist `kits_repo: true` only. No rule delivery happens until a later `inat agents add`.

#### 5.2 `inat agents add <name>` with `kits_repo: true`

After the per-kit delivery loop and after `deliver_inat_usage`, the command reads `config.kits_repo`. If `true`, it additionally calls `deliver_inat_kits(dir, name)`. The order is: install-loop deliveries, then `inat-usage`, then (conditionally) `inat-kits`.

#### 5.3 `inat agents remove <name>`

After the per-kit cleanup loop and after `remove_inat_usage`, the command unconditionally calls `remove_inat_kits(dir, name)`. This is a no-op if the rule was never delivered — it costs one `std::fs::metadata` check and a dedupe on the managed section. Calling it regardless of `kits_repo` is deliberate: if a project toggled `kits_repo` off by hand and the rule was previously delivered, `agents remove` must still clean it up.

#### 5.4 `inat validate [PATH] [--subpath <DIR>] [--json]`

`validate` runs offline and does not consult `.inat/config.json`. The target root is `PATH` (cwd by default), optionally rooted at `PATH/DIR` when `--subpath` is provided.

Checks, in order (all issues collected, none short-circuit):

1. **Registry exists** — `<root>/registry.json` exists and is readable UTF-8.
2. **Registry parses + validates** — `parse_registry` then `validate_registry` (`version == "1"`, `kits` object non-empty, each kit name key matches `validate_name`, each `types` entry is `"skill"` or `"rule"`).
3. **Per-kit** for each kit name key in the registry's `kits` object:
   3.1. `<root>/kits/<kit-name>/kit.json` exists and parses.
   3.2. `validate_kit` passes (`kit.name` matches regex, `files` non-empty, skills reject priority, each file entry's derived name passes `validate_name`).
   3.3. For each `files[i]` entry:
        - `type: "skill"`: `<root>/kits/<kit-name>/<path>/SKILL.md` exists and is a regular file.
        - `type: "rule"`: `<root>/kits/<kit-name>/<path>` exists, is a regular file, and ends in `.md`.
4. **Cross-kit uniqueness** — derived names MUST be unique across all kits in this registry.
5. **Reserved-name guard** — no derived name may equal `inat-usage` or `inat-kits`.

Note: the registry's `RegistryEntry.types` field is NOT cross-checked against the actual `files` list in `kit.json` — `types` is metadata, not a contract.

Every failing check appends one or more `Issue` records. Exit code is 0 iff the `Issue` list is empty.

#### 5.5 Output

Human mode (default):

```
error: <path>: <message>
error: <path>: <message>
...
validated N kit(s), M issue(s)
```

Writes to stderr; writes nothing else to stdout. Exit 0 on zero issues, non-zero otherwise.

JSON mode (`--json`), on stdout:

```json
{
  "path": "<canonical absolute path>",
  "issues": [
    {"kind": "MissingRegistry",    "path": "registry.json",            "message": "..."},
    {"kind": "InvalidName",        "path": "kits/foo/kit.json",        "message": "..."},
    {"kind": "MissingSkillMd",     "path": "kits/foo/skills/bar",      "message": "..."},
    {"kind": "DuplicateName",      "path": "kits/bar/kit.json",        "message": "derived name 'baz' also declared by 'foo'"},
    {"kind": "ReservedName",       "path": "kits/foo/kit.json",        "message": "derived name 'inat-usage' is reserved"}
  ]
}
```

The JSON object is emitted even on success (`issues: []`).

---

### 6. Error model

`InatError` gains no new variants. All failures outside `validate` reuse existing variants (`ConfigAlreadyExists`, `AgentNotFound`, `ReservedName`, etc.). Internally, `validate` returns `Vec<Issue>` rather than `Result<(), InatError>`; the CLI layer maps a non-empty vec to `std::process::ExitCode::from(1)`.

A separate `Issue` enum (in `src/core/validate/issue.rs`) enumerates the structural defects. `Issue` is not part of `InatError` because a validate run produces many of them per invocation — modeling them as errors would break the `?` early-return pattern used elsewhere.

---

### 7. Non-goals

- **No retro-fit command.** Flipping `kits_repo` on an existing project is deliberately not supported in this feature. Users hand-edit `.inat/config.json` or delete `.inat/` and re-init.
- **No scaffolding.** `init --kits-repo` does not create a starter `registry.json` or `kits/` directory. The rule and `validate` together guide creation.
- **No content-sanity checks in `validate`.** Empty-file detection, SKILL.md heading structure, or markdown-quality checks are out of scope; the checks here are structural only.
- **No git-state checks.** Validate does not care whether the working tree is clean or whether files are tracked. It inspects the filesystem.
- **No build-time rule generation.** `inat-kits.md` is hand-authored, not derived from spec 0001 via a build script. Drift is managed by keeping the rule compact.
- **No reservation of future names.** Only `inat-usage` and `inat-kits` are reserved. The list is not an API; future built-ins will add to it.
- **No `validate` variant that reads `.inat/config.json`.** Validate is stateless by design — it runs on cloned source repos that have never been touched by `inat init`.

---

### 8. References

- [`0001-initial-idea.md`](0001-initial-idea.md) §5 (kit structure), §6 (config), §8 (naming), §9 (CLI).
- [`docs/plan/0005-builtin-inat-usage-rule.md`](../plan/0005-builtin-inat-usage-rule.md) — the built-in `inat-usage` rule this spec mirrors.
- [`0003-source-subpath.md`](0003-source-subpath.md) — `Source.path` behaviour that `--subpath` shadows.
- `src/agents/builtin/mod.rs` — built-in rule delivery infrastructure (shared helper to be introduced).
- `src/core/registry.rs`, `src/core/kit.rs`, `src/core/naming.rs` — parsers/validators to be reused by `validate`.
- `docs/plan/0007-kits-authoring-support.md` — implementation plan.

---

### 9. Acceptance

The spec is satisfied when:

1. `inat init --kits-repo --agent claude-code` in a fresh directory produces `.inat/config.json` containing `"kits_repo": true` and `"agents": ["claude-code"]`, and delivers both `.inat/rules/inat-usage.md` and `.inat/rules/inat-kits.md`. `CLAUDE.md` references both files under the managed contextual section.
2. `inat init --kits-repo` without `--agent` produces `.inat/config.json` with `"kits_repo": true` and `"agents": []`. No rule files are delivered and no `CLAUDE.md` is created.
3. `inat init --agent claude-code` (no `--kits-repo`) continues to deliver only `inat-usage`, unchanged from spec 0005.
4. In a project with `kits_repo: true` and no agent, `inat agents add claude-code` delivers both rules.
5. In the same project, `inat agents remove claude-code` removes both rule files and both managed-section references.
6. Attempting to install a kit whose declared files derive to a name `inat-kits` fails with `InatError::ReservedName`.
7. `inat validate` on a directory with no `registry.json` exits non-zero with a `MissingRegistry` issue.
8. `inat validate` on a directory whose `registry.json` declares a kit `foo` under `kits/foo/`, whose `kit.json` declares a rule `kits/foo/rules/bar.md` that exists on disk, exits 0 and prints `validated 1 kit(s), 0 issue(s)`.
9. `inat validate` on a directory whose `registry.json` declares two kits both exporting a rule named `bar` exits non-zero with a `DuplicateName` issue.
10. `inat validate` on a directory whose `registry.json` declares a kit whose `kit.json` entries derive to `inat-usage` exits non-zero with a `ReservedName` issue.
11. `inat validate ./some-checkout --subpath pkg` validates `./some-checkout/pkg/registry.json`.
12. `inat validate --json` emits a JSON object with `path` and `issues` keys, regardless of success or failure.
13. No new dependencies are added. All validation logic reuses `parse_registry`, `validate_registry`, `parse_kit`, `validate_kit`, `validate_name`, and `derive_entry_name`.
