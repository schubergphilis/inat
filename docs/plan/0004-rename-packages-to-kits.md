# Plan — Rename packages → kits (spec 0004)

Companion to [`docs/specs/0004-rename-packages-to-kits.md`](../specs/0004-rename-packages-to-kits.md). Records the concrete rollout order.

## Why

Pre-release terminology cleanup. `package/packages` collided with npm/cargo package-manager jargon and hit `.gitignore` rules that exclude `packages/` directories in host repositories. `kit` is short, domain-free, and has zero existing occurrences in the repo at the time of this plan.

## Decisions (from the approval conversation)

- **Full rename**, including on-disk artifacts: `packages/` → `kits/`, `package.json` → `kit.json`, `registry.json` key `"packages"` → `"kits"`.
- **Clean break** — no dual-read, no migration code, no config-version bump. v0.1.0 is not released yet.
- **Spec-first** — new `docs/specs/0004-rename-packages-to-kits.md` is the decision record; specs 0001 and 0003 are rewritten in place so the canonical specs read kit-terminology.
- `CHANGELOG.md` is rewritten in place inside its existing `[0.1.0]` entry. No new "breaking change" entry, because nothing is released.

## Canonical mapping

See [`docs/specs/0004-rename-packages-to-kits.md`](../specs/0004-rename-packages-to-kits.md) §3. That table is the single source of truth.

Scope exclusions are in §4 of the spec and are not repeated here.

## Phases

### Phase 1 — Specs
- Create `docs/specs/0004-rename-packages-to-kits.md`.
- Rewrite `docs/specs/0001-initial-idea.md` in place (42 occurrences) — prose, directory examples, JSON-key examples, filename examples (`package.json` → `kit.json`). Add a forward-pointing note at the top.
- Rewrite `docs/specs/0003-source-subpath.md` (11 occurrences). It references `fetch_package_tree_at` which is renamed in Phase 2.
- Leave `docs/specs/0002-ci-cd-and-install.md` alone (refers to GitLab's Generic Package Registry, not inat).
- Mirror this plan into `docs/plan/0004-rename-packages-to-kits.md`.

### Phase 2 — Rust code
Order matters so the crate compiles step by step:

1. `src/core/package.rs` → `src/core/kit.rs`; update `src/core/mod.rs` (`pub mod package;` → `pub mod kit;`).
2. Inside `kit.rs`: `PackageConfig` → `KitConfig`; `parse_package` / `validate_package` / `read_and_validate_package` → `parse_kit` / `validate_kit` / `read_and_validate_kit`. Rename the associated tests.
3. `src/core/config.rs`: `InstalledPackage` → `InstalledKit`. Leave the `installed` field name alone — it is not package-flavoured.
4. `src/core/registry.rs`: field `packages` → `kits`. Update test JSON fixtures to use `"kits"` as the key.
5. `src/error.rs`: variants `PackageNotFound` / `PackageAlreadyInstalled` / `PackageNotInstalled` / `PackageValidation` → `KitNotFound` / `KitAlreadyInstalled` / `KitNotInstalled` / `KitValidation`. Update `#[error("...")]` strings and rename the `existing_package` field in `DuplicateName` to `existing_kit`.
6. `src/git/source/tree.rs`: `fetch_package_tree[_at]` / `walk_package_files` renamed; hardcoded path strings `"packages/..."` → `"kits/..."`; test renames.
7. `src/cli/install.rs`: `parse_package_ref` → `parse_kit_ref`; vars and error strings.
8. `src/cli/update.rs`, `remove.rs`, `refresh.rs`, `status.rs`, `agents.rs`: function/var renames and user-facing strings (`no packages installed` → `no kits installed`, `eprintln!("\npackages:")` → `eprintln!("\nkits:")`).
9. `src/main.rs`: clap positional arg `package: String` → `kit: String` on `Install`/`Update`/`Remove`; help text rewrites including the `sources add --path` help text that mentions `packages/`.
10. `src/core/delivery/managed_write.rs`: `remove_package_content` → `remove_kit_content`.
11. Test fixtures in `src/test_helpers.rs`, `src/git/source/test_support.rs`, `src/core/delivery/edge_case_tests.rs`, and `src/cli/*.rs` test blocks: literal path strings and manifest filenames.
12. `spike/git2-spike/src/main.rs`: align for consistency.

### Phase 3 — Non-spec docs
- `README.md` (17), `CLAUDE.md` (8), `docs/guidelines/coding-standards.md` (10), `docs/plan/0003-source-subpath.md` (8), `docs/pitch-narrative.md` (15), `docs/demo-walkthrough.md` (34).
- `CHANGELOG.md` — rewrite the three `package(s)` occurrences inside the existing `[0.1.0] - 2026-04-21` entry. No new entry.
- `Cargo.toml` — change `description = "AI prompt and rule package manager"` → `"AI prompt and rule kit manager"`.
- Leave `docs/plan/0002-ci-cd-and-install.md` alone.

### Phase 4 — Verification
1. Repo-wide grep for whole-word `package` / `packages`. Only allowlisted hits: `Cargo.toml` `[package]` table, `Cargo.lock`, `docs/specs/0002-*`, `docs/plan/0002-*`.
2. `cargo fmt --all`.
3. `cargo clippy --all-targets --all-features -- -D warnings`.
4. `cargo test` — 153 tests pass.
5. Smoke-install: hand-author a local git repo with `registry.json` (`"kits": {...}`) + `kits/<name>/kit.json`. Register with `inat sources add`, install with `inat install <name>`, remove with `inat remove <name>`.
6. `inat install --help` / `update --help` / `remove --help` — confirm the positional is labelled `<kit>` and help text reads "kit".
7. Pre-commit hook passes on the commit bundle (the 0004 spec satisfies the spec-presence check).

## Commit shape

One commit per phase keeps the diff reviewable:
- `feat(0004): spec 0004 — rename package → kit; update specs 0001, 0003`
- `feat(0004): rust rename package → kit (module, types, fns, fields, strings)`
- `feat(0004): docs + CHANGELOG + Cargo.toml description — package → kit`
- `chore(0004): residual rename sweep post-verify` (only if Phase 4 finds stragglers)
