## inat — Source-Subpath Layout Spec

---

### 1. Overview

Spec [`0001-initial-idea.md`](0001-initial-idea.md) §4 defines the source-repository layout with `registry.json` and `kits/` anchored at the repo root. This spec extends the source model with an opt-in **subpath** variant: the same layout may live under a named subdirectory of an otherwise general-purpose repository.

This unlocks the case where a repo already owns its root for another purpose (documentation, a monorepo, a company-wide resource collection) and wants to expose `inat` kits without colonising the root. The base layout from `0001 §4` remains the default and is untouched.

A companion implementation plan (in `docs/plan/0003-source-subpath.md`) records the concrete rollout.

---

### 2. Goals

1. A source repository MAY opt into a subpath layout by being registered with the `--path <subpath>` option on `inat sources add`.
2. When a source is configured with a subpath, `inat` MUST resolve `<subpath>/registry.json` and `<subpath>/kits/<kit>/...` in place of the repo-root equivalents from `0001 §4`. No other resolution semantics change.
3. The default (subpath-absent) layout from `0001 §4` remains the primary shape and is wire-compatible: an existing `.inat/config.json` that has no `path` field on its sources continues to load and operate unchanged.
4. Subpaths MUST be validated at registration time. Invalid subpaths are rejected before any network or filesystem work begins.
5. The subpath is stored once — on `sources add` — and never re-prompted. Downstream commands (`install`, `update`, `status`, `refresh`) read it from the source record.

---

### 3. Config schema delta

The `Source` record gains one optional field:

```rust
pub struct Source {
    pub name: String,
    pub url: String,
    /// Optional subpath inside the source repo where `registry.json`
    /// and `kits/` live. `None` means the default repo-root layout.
    pub path: Option<String>,
}
```

JSON serialisation rule: the `path` field MUST be omitted when `None`, via `#[serde(default, skip_serializing_if = "Option::is_none")]`. Consequences:

- A `config.json` written before this feature shipped deserialises without migration — serde's `default` fills `path` with `None`.
- A `config.json` written today that has a source without `--path` is byte-identical to one written before this feature shipped. No gratuitous churn in committed `.inat/config.json` files.

Reference: `src/core/config.rs` `Source` struct.

---

### 4. CLI surface

```
inat sources add <url> [--name <alias>] [--path <subpath>]
```

- `--path` is optional. Omitting it keeps the `0001 §4` root-layout behaviour.
- `sources remove`, `sources list`, `install`, `update`, `status`, `refresh` gain no new flags. `sources list` MUST surface the subpath when present so users can see which sources opted in.

Examples:

```
# Default root layout (unchanged from 0001 §4)
inat sources add https://example.com/rules.git --name company

# Subpath layout — kits live under ai-resources/ in the repo
inat sources add https://example.com/shared-repo.git --name shared --path ai-resources
```

---

### 5. Subpath validation rules

A subpath MUST be a relative POSIX-style path under the repo root. On `sources add`, the subpath is normalised (surrounding whitespace trimmed, leading and trailing `/` stripped) and then rejected with `InatError::KitValidation` if any of the following hold:

| Class | Example | Error reason |
|---|---|---|
| Empty after trimming | `""`, `"   "`, `"/"`, `"///"` | `source --path must not be empty` |
| Absolute path | `/ai-resources` | `source --path must be relative, got '<raw>'` |
| Windows-style separators | `foo\bar` | `source --path must use forward slashes` |
| Parent / current segments | `foo/../bar`, `foo/./bar` | `source --path must not contain '.' or '..' segments: '<raw>'` |

A valid subpath is stored as-normalised (e.g. `"ai-resources/"` is stored as `"ai-resources"`). `None` passed through unchanged.

Reference: `src/cli/sources.rs::normalize_source_path`.

---

### 6. Tree-lookup semantics

With a normalised subpath `sp`:

- Registry resolution looks up `<sp>/registry.json` via `Tree::get_path` on the source repo's HEAD tree.
- Kit-tree resolution looks up `<sp>/kits/<kit>` via `Tree::get_path`. The returned tree SHA is stored in `Installed.tree_sha` exactly as it is for root-layout sources — downstream update-detection (spec `0001 §…`) works identically.
- When the subpath is `None`, both lookups fall back to the root-anchored paths, byte-identical to the pre-feature behaviour.

Reference: `src/git/source.rs::fetch_registry_at`, `src/git/source.rs::fetch_kit_tree_at`.

---

### 7. Error model

| Condition | Error variant | Where raised |
|---|---|---|
| Subpath fails validation (see §5) | `InatError::KitValidation { reason }` | `sources add`, before any network work |
| `<subpath>/registry.json` missing or not UTF-8 | `InatError::RegistryValidation { reason }` — reason includes the resolved path | `sources add`, `install`, `update`, `refresh` |
| `<subpath>/registry.json` malformed JSON | `InatError::Json` | same as above |
| `<subpath>/kits/<kit>` missing in HEAD tree | `InatError::Git` | `install`, `update` |

---

### 8. Non-goals

- **No migration tooling** for existing sources. A user who wants to move a source from root-layout to subpath-layout runs `sources remove` followed by `sources add --path <subpath>`.
- **No auto-detection** of layout. The layout is declared at registration, not probed.
- **No multi-subpath sources.** Exactly zero or one subpath per source record; a repo that hosts multiple disjoint inat surfaces is registered once per surface, each under a distinct `--name`.
- **No path rewriting for delivered files.** Kit-internal paths (e.g. `skills/<name>/SKILL.md`) are unchanged; the subpath only affects how `inat` *finds* a kit inside the source repo, not how it lays a kit out on delivery.

---

### 9. References

- [`0001-initial-idea.md`](0001-initial-idea.md) §4 — base source-repository structure.
- `src/core/config.rs` — `Source` struct, `path` field.
- `src/cli/sources.rs::normalize_source_path` — validation rules from §5.
- `src/git/source.rs::fetch_registry_at`, `fetch_kit_tree_at` — tree-lookup semantics from §6.
- `docs/plan/0003-source-subpath.md` — retrospective implementation plan.

---

### 10. Acceptance

The spec is satisfied when:

1. `inat sources add <url> --path ai-resources` against a repo whose `registry.json` lives at `ai-resources/registry.json` succeeds, and `sources list` shows the subpath for that source.
2. `inat install <source>/<kit>` against a subpath-configured source delivers files drawn from `<subpath>/kits/<kit>/` and stores a tree SHA matching that directory's HEAD tree.
3. `inat sources add <url>` (no `--path`) against the same repo fails with `RegistryValidation` because the root `registry.json` does not exist — confirming the opt-in is explicit, not inferred.
4. Each invalid-subpath class from §5 produces the matching `KitValidation` error before any network operation runs.
5. An existing project's `.inat/config.json` from before this feature shipped loads, operates, and saves without gaining a `path` key on its unchanged sources.
