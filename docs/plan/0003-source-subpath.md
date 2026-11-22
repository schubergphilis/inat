# Implementation Plan — Source Subpath (spec 0003)

Concretises [`docs/specs/0003-source-subpath.md`](../specs/0003-source-subpath.md). This plan is retrospective: the code landed before the spec was written (the authoring instance did not have `CLAUDE.md` in context). The purpose of this document is to bring governance in line with the code that is already on `main` — and to record the test inventory so future work on the subpath feature has a single reference point.

---

## 1. Status

- Code landed out-of-order, on branch `ci/0002-windows-runner-probe`, by an off-repo Claude instance.
- Spec 0003 was backfilled in a separate commit.
- This plan describes the shape of the change as it landed; it is not a forward-looking design document.
- Follow-up: tighten process so this cannot recur — see `docs/plan/0002-ci-cd-and-install.md §8` (spec-presence gate) and the native pre-commit hook at `.githooks/pre-commit`.

---

## 2. Change surface

| Path | Change |
|---|---|
| `src/core/config.rs` | `Source` gains `path: Option<String>` with `serde(default, skip_serializing_if = "Option::is_none")`. Backwards-compatible on read and write. |
| `src/cli/sources.rs` | New `normalize_source_path` validator (spec 0003 §5). `sources_add` wires `--path` through; stores normalised value on the `Source` record. |
| `src/git/source.rs` | New `fetch_registry_at(repo, subpath)` and `fetch_kit_tree_at(repo, subpath, kit)`. Existing `fetch_registry` / `fetch_kit_tree` kept as thin wrappers that pass `None` — no downstream call-site churn beyond the add-path flow. |
| `src/main.rs` | Adds `--path` to the `sources add` clap subcommand; forwards to `run_sources_add`. |
| `src/cli/install.rs` | Threads the source's stored subpath into `fetch_registry_at` / `fetch_kit_tree_at` when installing from a subpath-configured source. |
| `src/cli/update.rs` | Same, for update-detection tree-SHA diffs. |
| `src/cli/status.rs` | Same, for up-to-date checks. |
| `src/cli/agents.rs` | No functional change; uses the updated `Source` struct for agent auto-deliver. |
| `src/test_helpers.rs` | Test-support `Source` construction gains explicit `path: None`. |
| `src/edge_case_tests.rs` | Test-support `Source` construction gains explicit `path: None`. |
| `README.md` | Documents `--path` in the CLI overview and adds a subpath-usage paragraph pointing at spec 0001 §4 for the base schema. |

---

## 3. Test coverage

15 new unit tests, in-file with the code they exercise (per coding-standards §7):

**`src/cli/sources.rs` — 10 tests for `normalize_source_path`:**

- `normalize_none_returns_none`
- `normalize_simple_relative_path`
- `normalize_strips_leading_and_trailing_slashes`
- `normalize_nested_relative_path`
- `normalize_rejects_absolute_path`
- `normalize_rejects_parent_segment`
- `normalize_rejects_current_segment`
- `normalize_rejects_backslashes`
- `normalize_rejects_empty_string`
- `normalize_rejects_only_slashes`

**`src/git/source.rs` — 5 tests for the subpath tree lookups:**

- `fetch_registry_at_finds_subpath_registry`
- `fetch_registry_at_with_none_uses_repo_root`
- `fetch_registry_at_missing_subpath_returns_validation_error`
- `fetch_kit_tree_at_finds_kit_under_subpath`
- `fetch_kit_tree_at_with_none_matches_legacy`

The subpath fetch tests share a new helper, `create_subpath_source`, that builds a bare remote with `ai-resources/registry.json` and `ai-resources/kits/test-kit/` to exercise the spec 0003 §6 semantics end-to-end against a real git repo (no mocking, consistent with existing `src/git/source.rs` tests).

End-to-end smoke test (not automated — noted in spec 0003 §10 acceptance): register a real subpath source, install a kit, confirm the tree SHA recorded in `installed` matches `git ls-tree HEAD <subpath>/kits/<kit>`.

---

## 4. Backwards-compatibility check

Two invariants must hold after this change lands:

1. **Read-compat:** a `.inat/config.json` written by the pre-feature binary loads under the post-feature binary without error and without introducing a `path` key on any source.
2. **Write-compat:** the post-feature binary, re-serialising a config that was read from a pre-feature file and not mutated, produces byte-identical JSON.

Both are enforced by `#[serde(default, skip_serializing_if = "Option::is_none")]` on the `path` field. The existing round-trip test in `src/core/config.rs::tests` (`test_config_roundtrip_preserves_fields`) covers the write side; the `serde(default)` attribute covers the read side.

No migration code. No warning on open. No prompt.

---

## 5. Reused project conventions

- **Spec numbering and header style** copied from `docs/specs/0001-initial-idea.md` and `docs/specs/0002-ci-cd-and-install.md`.
- **Error family:** `InatError::KitValidation` already exists for name / layout rejections in `sources add`; subpath validation reuses it rather than introducing a new variant. (Originally `PackageValidation`; renamed under spec 0004.)
- **Forward-slash path hygiene** in config storage matches the pattern used for kit-internal paths throughout the codebase (see `core/kit.rs` file-path handling).
- **Test location:** new unit tests live in the same file as the code under test, per coding-standards §7. No integration-test reorganisation.
- **Tree-lookup wrapper pattern:** `fetch_registry` and `fetch_kit_tree` are preserved as `None`-forwarding wrappers around the new `_at` variants, so call sites that do not care about the subpath concept stay ignorant of it.

---

## 6. Follow-ups

None required for the feature itself. Process follow-ups (separate from spec 0003 scope) are tracked in the enforcement commit that accompanies this plan:

- Native git pre-commit hook at `.githooks/pre-commit` (spec-presence check + fmt/clippy/test).
- `CLAUDE.md` cross-instance banner documenting the walk-up loading model.
- GitLab CI `check:spec-presence` job gating `src/` changes on a `docs/specs/` or `docs/plan/` touch.

See `docs/plan/0002-ci-cd-and-install.md §8` for the CI-side record.
