## inat — Per-rule "when to read" descriptions in the managed section (Plan 0010)

Pairs with [`docs/specs/0010-rule-descriptions.md`](../specs/0010-rule-descriptions.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, and verification.

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Source of truth | YAML frontmatter at the top of each rule `.md`, fields `name` + `description`. |
| Required fields | Both `name` and `description`. `name` MUST equal the filename-derived name (`core/kit.DeriveEntryName`). |
| Missing / empty frontmatter at install / refresh | Soft warn to stderr; render bare bullet. |
| Shape violations (name mismatch, malformed YAML, oversize, multi-line description) | Hard error at install / validate. |
| Description length | Soft cap **200 chars**, single-line only. |
| Always-priority rules | Adopt frontmatter; renderer strips the YAML block before inlining the body. |
| Render format | `- <path> — <description>`, U+2014 em-dash with surrounding spaces. Bare `- <path>` when description is empty. |
| YAML parser | Hand-rolled 2-key flat extractor in new `go/core/frontmatter`. No new dependencies. |
| Migration | `inat refresh` and `inat update` rewrite existing managed sections; other commands are pass-through. |
| Parser model | `ManagedSection.References` is `[]Reference{Path, Description}`. `AddContextualReference` takes a `description` parameter. `RemoveContextualReference` keeps its single-arg signature, matching by Path. |
| `inat validate` | Warns on missing description; errors on shape violations. `--json` output adds a `warnings` array. |
| Built-in rules | `inat-usage` and `inat-kits` adopt frontmatter (dogfood). |

---

### 2. Files

#### New
- `go/core/frontmatter/frontmatter.go` — `Parse(content) (Frontmatter, body, found, err)` over a `---`-fenced flat key-value block.
- `go/core/frontmatter/frontmatter_test.go` — table-driven coverage: present+complete, missing description, missing name, no closing fence, no frontmatter, CRLF, BOM, leading blank lines, quoted values, value containing `:` and ` — `, comments, duplicate keys, malformed lines.
- `docs/specs/0010-rule-descriptions.md` — the contract (this plan's pair).
- `docs/plan/0010-rule-descriptions.md` — this document.

#### Modified
- `go/core/delivery/managed_section.go` — `Reference{Path, Description}` type; `parseSectionContent` splits each bullet on the first ` — `; `RenderManagedSection` emits `- <path>` or `- <path> — <description>`.
- `go/core/delivery/managed_write.go` — `AddContextualReference(targetFile, rulePath, description string)` (new param). On dedupe-hit, the existing entry's description is updated, so `refresh` corrects stale hints. `RemoveKitContent` filters by `Reference.Path`.
- `go/core/delivery/delivery_test.go` — round-trip with descriptions, em-dash split/render, dedupe-updates-description, clears-on-empty.
- `go/core/kit/kit.go` — `ReadAndValidateKit` returns `(KitConfig, []string warnings, error)`. `MaxDescriptionLength = 200`. New `validateRuleFrontmatters` helper applies the severity model from spec §6.
- `go/core/kit/kit_test.go` — coverage for missing fm (warn), missing description (warn), missing name (error), name mismatch (error), oversize (error), malformed fm (error), clean (no warnings).
- `go/core/validate/validate.go` — new IssueKinds `InvalidRuleFrontmatter`, `MissingRuleDescription`. `Issue` gains `Severity`. `Issue.IsError()` reports whether the issue blocks. `checkRuleEntry` reads the rule body and runs frontmatter checks via the new `checkRuleFrontmatter` helper.
- `go/cli/install/install.go` — propagate the new `ReadAndValidateKit` signature; print warnings to stderr. `deliverContextualRule` parses frontmatter to extract description (now passed to `AddContextualReference`). `deliverAlwaysRule` strips frontmatter and inlines only the body.
- `go/cli/refresh/refresh.go` — propagate new signature; warnings to stderr.
- `go/cli/update/update.go` — same.
- `go/cli/agents/agents.go` — same.
- `go/cli/validate/validate.go` — `Outcome` gains `WarningCount`; `IsOK()` ignores warnings. Human and JSON output split errors from warnings.
- `go/agents/builtin/rules.go` — `deliverBuiltin` parses embedded content's frontmatter and forwards the description into `AddContextualReference`.
- `go/agents/builtin/inat_usage.md` — frontmatter prepended (`name: inat-usage`, description sentence).
- `go/agents/builtin/inat_kits.md` — frontmatter prepended (`name: inat-kits`, description sentence). New `## Rule frontmatter (recommended)` section so the body matches the description's claim that this file covers "the rule-frontmatter requirement for contextual rules."
- `go/agents/builtin/rules_test.go` — assertions: both embedded rules parse to valid frontmatter, names match `reserved.BuiltinRuleName` / `reserved.BuiltinKitsRuleName`, delivered references render with em-dash hints.
- `README.md` — short § "Rule frontmatter" with the shape and a one-line example.
- `CHANGELOG.md` — `[Unreleased]` entry covering the additive frontmatter feature, the new `--json` `warnings` array, and the intentional first-refresh diff.
- `.gitignore` — ignore `.claude/scheduled_tasks.lock`, `go/CLAUDE.md`, `go/.inat/` (local smoke-test artifacts when running `inat init` inside `go/`).

---

### 3. Implementation order (TDD-first)

1. `frontmatter` package — tests, then `Parse`.
2. `delivery` package — round-trip and em-dash tests, then `Reference` type and parser/render updates.
3. `kit.ReadAndValidateKit` — warnings tests, then signature change and frontmatter validation helper.
4. CLI plumbing (install → refresh → update → agents → validate) — propagate new signatures; emit warnings.
5. Built-in rules — frontmatter prepended; `rules.go` updated; assertions added.
6. Docs — spec 0010, plan 0010, README.

---

### 4. Verification

- `go test ./...` green.
- `gofmt -l .` empty; `go vet ./...` clean; `staticcheck ./...` clean.
- Pre-commit hook (`.githooks/pre-commit`) passes.
- End-to-end manual smoke (in a scratch project):
  1. `inat init --agent claude-code` → `CLAUDE.md` shows `- .inat/rules/inat-usage.md — Read when …`.
  2. Install a kit with one frontmatter-bearing contextual rule and one without → first renders with em-dash hint; second renders bare and stderr emits a warning naming the rule.
  3. Author commits frontmatter for a previously-bare rule in the source repo → consumer runs `inat update` → bullet now carries the hint. (`inat refresh` re-renders against the recorded tree SHA only; it does not fetch new commits, so it would not pick up the new frontmatter until `update` advances the SHA. See spec §7 for the refresh-vs-update contract.)
  4. `inat validate` against a kit with a name mismatch / 250-char description → exits non-zero with the precise error.
  5. Always-priority rule with frontmatter → managed-section content block contains body only, no `---` fences leaking through.

---

### 5. Risks

- **Hash drift on built-ins.** Embedding frontmatter into `inat_usage.md` / `inat_kits.md` changes their delivered hashes. On the user's next `inat refresh` after upgrade, the on-disk file is still the previous version (unchanged hash matches recorded), so the modified-file guard does not trip — refresh overwrites cleanly and records the new hash. No `--force` required.
- **First-refresh diff is large.** Users upgrading from v0.3.x see their managed section gain hints on every contextual rule that has frontmatter. Document in CHANGELOG.
- **Em-dash inside description.** Splitting on the *first* ` — ` is fine; the parser only needs to recover the Path. The description body is opaque after that.
- **`name` redundancy.** `name` duplicates filename-derived data, but validation enforces equality so drift can't survive validate. The redundancy buys 1:1 parity with `SKILL.md`.
