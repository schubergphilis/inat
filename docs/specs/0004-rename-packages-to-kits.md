## inat — Terminology Rename: Packages → Kits

---

### 1. Context

Spec [`0001-initial-idea.md`](0001-initial-idea.md) introduces a "package" as the unit of delivery for skills and rules. As `inat` began to be used in real repositories, that term proved to be a poor fit:

- **Collision with existing package managers.** "`inat` is a package manager for packages" overloads a word that already means something specific in npm/cargo/pip/apt contexts, and confuses the pitch.
- **Repository-hygiene clashes.** Many projects ship `.gitignore` entries that exclude `packages/` directories (a common npm/cargo workspace artifact). Source repos authored for `inat` hit these ignores by accident.
- **Weaker identity.** "Package" does not evoke the actual shape of what `inat` delivers — a small, named bundle of prompts/rules meant to be picked up as a unit.

The chosen replacement is **kit**. It is short, domain-free, and non-overloaded inside this project today (grep confirms zero existing occurrences of `kit`/`kits` in the repo at the time of writing).

This spec supersedes the "package" terminology everywhere it appears in [`0001-initial-idea.md`](0001-initial-idea.md) and [`0003-source-subpath.md`](0003-source-subpath.md). Spec 0001 has been rewritten in place under this change; 0003 has been updated likewise. This spec is the decision record.

---

### 2. Decision

Rename the concept from **package / packages** to **kit / kits** across the entire project surface: code, CLI, on-disk artifacts, and documentation.

This is a **clean break**. `inat` is pre-release (v0.1.0 not yet published). No dual-read shim, no version-bump migration layer, no back-compat code path is shipped. Any source repository authored against the pre-rename layout is expected to be migrated manually by renaming its `packages/` directory to `kits/` and its `package.json` manifests to `kit.json`; any project's `.inat/config.json` authored against the pre-rename registry key is expected to be discarded (`.inat/` deleted and `inat init` re-run).

---

### 3. Canonical mapping

| Surface | Before | After |
|---|---|---|
| Concept noun | package / packages | kit / kits |
| Directory in source repo | `packages/` | `kits/` |
| Per-kit manifest filename | `package.json` | `kit.json` |
| Registry JSON key | `"packages": { ... }` | `"kits": { ... }` |
| CLI positional arg (`install`/`update`/`remove`) | `<package>` | `<kit>` |
| Rust module file | `src/core/package.rs` | `src/core/kit.rs` |
| Rust type (manifest) | `PackageConfig` | `KitConfig` |
| Rust type (config row) | `InstalledPackage` | `InstalledKit` |
| Rust error variants | `PackageNotFound`, `PackageAlreadyInstalled`, `PackageNotInstalled`, `PackageValidation` | `KitNotFound`, `KitAlreadyInstalled`, `KitNotInstalled`, `KitValidation` |
| Registry struct field | `pub packages: HashMap<...>` | `pub kits: HashMap<...>` |
| Core functions | `parse_package`, `validate_package`, `read_and_validate_package` | `parse_kit`, `validate_kit`, `read_and_validate_kit` |
| Git-source functions | `fetch_package_tree`, `fetch_package_tree_at`, `walk_package_files` | `fetch_kit_tree`, `fetch_kit_tree_at`, `walk_kit_files` |
| CLI helpers | `parse_package_ref`, `deliver_package_to_agent`, `remove_package_content`, `check_package_status` | `parse_kit_ref`, `deliver_kit_to_agent`, `remove_kit_content`, `check_kit_status` |
| Local variable / parameter names | `package_ref`, `package_name`, `package_json` | `kit_ref`, `kit_name`, `kit_json` |
| User-facing CLI output | `no packages installed`, `packages:`, `ambiguous package name` | `no kits installed`, `kits:`, `ambiguous kit name` |
| Reference syntax | `source/package` | `source/kit` |

The install-key string format inside `.inat/config.json` — `"<source>/<name>": { ... }` — is **syntactically** unchanged; it was never literally the word "package", only a source/name pair. The second segment is now called a kit-name instead of a package-name.

---

### 4. Scope exclusions (intentionally unchanged)

The following surfaces are left as-is because they are not package-flavoured terminology:

- **`.inat/config.json` key `installed`.** This field name describes what the map contains (installed things), not what kind of thing, and is preserved.
- **Managed-section markers** in delivered rule files: `<!-- inat:managed - DO NOT EDIT THIS SECTION -->` and `<!-- inat:end -->`. Neither contains "package".
- **Managed-section block header prefix** `# --- <source>/<name>/<filename> ---`. The `<name>` slot has always been a name, never the literal word "package".
- **`Cargo.toml` `[package]` table.** Rust manifest syntax, not inat-terminology.
- **Cargo crate name** `inat`. Unchanged.
- **CLI command names** (`install`, `update`, `remove`, `refresh`, `status`, etc.). They describe actions, not objects, and are already kit-agnostic.
- **Rust `[package]` references in `Cargo.lock`.** Package-manager metadata belonging to cargo itself.
- **`docs/specs/0002-ci-cd-and-install.md`** and **`docs/plan/0002-ci-cd-and-install.md`.** Their single occurrences of "package" refer to GitLab's Generic Package Registry feature and are correct as written.

---

### 5. Clean-break consequences

1. Source repositories authored against the pre-rename layout fail to load on install: `registry.json` missing the `kits` key deserializes to an empty registry. Authors must rename `packages/` → `kits/`, `package.json` → `kit.json`, and the JSON key `"packages"` → `"kits"` before `inat install` succeeds.
2. Existing `.inat/config.json` files in the wild — there are none known at time of writing — are not automatically migrated. `inat` will attempt to refresh kits referenced under a pre-rename install key format; because that format is unchanged, existing configs technically still parse, but tracked on-disk layouts of already-delivered content may be stale. The supported upgrade path is: `rm -rf .inat/ && inat init`.
3. `CHANGELOG.md` is rewritten in place inside its existing `[0.1.0] - 2026-04-21` section rather than gaining a new entry. No release has shipped, so there is nothing to announce a breaking change for.

---

### 6. Collision check

At the time this spec is written (2026-04-22), the strings `kit` and `kits` do not appear anywhere in the repository: not in source, tests, docs, specs, CI config, or fixtures. The rename therefore introduces no terminology ambiguity.

---

### 7. Acceptance

The rename is complete when:

1. A repo-wide grep for the whole words `package` and `packages` returns only these allowlisted hits:
   - `Cargo.toml` — the `[package]` table and dependency references.
   - `Cargo.lock` — cargo-owned metadata.
   - `docs/specs/0002-ci-cd-and-install.md` and `docs/plan/0002-ci-cd-and-install.md` — references to GitLab's Generic Package Registry.
2. `cargo fmt --all`, `cargo clippy --all-targets --all-features -- -D warnings`, and `cargo test` all pass, with the test count at least matching the pre-rename count.
3. A hand-crafted local kit source with `registry.json` containing `{"version":"1","kits":{...}}` and a `kits/<name>/kit.json` tree can be registered via `inat sources add`, installed via `inat install <name>`, and removed via `inat remove <name>` end-to-end.
4. `inat install --help`, `inat update --help`, and `inat remove --help` all show `<kit>` as the positional argument and phrase their help text in kit-terminology.
5. The pre-commit hook passes on a commit that includes this spec plus the matching code and documentation changes (the presence of spec `0004-rename-packages-to-kits.md` satisfies the spec-presence check).

---

### 8. References

- [`0001-initial-idea.md`](0001-initial-idea.md) — rewritten in place as part of this change.
- [`0003-source-subpath.md`](0003-source-subpath.md) — rewritten in place as part of this change.
- `docs/plan/0004-rename-packages-to-kits.md` — companion implementation plan recording the concrete rollout order.
