## inat — `inat update --plan` / `inat replace --plan` dry-run (Plan 0020)

Pairs with [`docs/specs/0020-update-plan-dry-run.md`](../specs/0020-update-plan-dry-run.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, TDD test order, and verification.

Pays down the deferral flagged in [`docs/specs/0017-transactional-update.md`](../specs/0017-transactional-update.md) §9 and [`docs/specs/0018-kit-reorg-auto-rename.md`](../specs/0018-kit-reorg-auto-rename.md) §9 (JSON / dry-run plan output). This spec ships the dry-run half only; JSON is left for a future spec.

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Where the printer lives | `cli/transaction`. Both `cli/update` and `cli/replace` already import `transaction` for the Plan/Apply types, so this keeps the import graph clean. `cli/update` would have been the historical choice, but `cli/replace` reusing it would invert the dependency direction. |
| Printer signature | `func PrintPlanDetails(w io.Writer, plan []PlannedAction)`. Takes an `io.Writer` for testability; returns nothing because the only failure mode (write error to `os.Stdout`) is not actionable from the CLI. |
| Output stream | stdout for detail lines. stderr remains the home of the aggregate plan-summary line (unchanged from today) and `Plan`'s fetch trace. |
| Detail-line format | Plain text, one line per action. Action token (`ActionKind.String()`) + space + key + optional `→ newKey` + optional `: <err>`. U+2192 arrow byte sequence — same character spec 0018 already uses in `printResultErrors`. |
| Exit code under `--plan` | Always 0, even when needs-attention actions are in the plan. Spec §2 criterion 5. |
| Pipeline placement | `update`: after `printPlanSummary`, before `transaction.Apply`. Early return; no `SaveConfig` call. `replace`: after `transaction.Plan`, before the `plan[0].Kind == ActionNeedsAttention` short-circuit (so even needs-attention plans print and exit 0 under `--plan`). |
| `--plan --force` collision | Rejected at flag parse in cobra's `RunE` wrapper. New `errs.KitValidation` instance, no new typed-error variant. |
| Signature changes | `Update`, `RunUpdate`, `Replace`, `RunReplace` all gain `planOnly bool`. Positional, between `force` and `homeOverride` to keep the call-site order shape-stable. |
| Test strategy | (a) Pure-format tests for `PrintPlanDetails` in `cli/transaction`. (b) Pipeline tests in `cli/update` and `cli/replace` asserting no-disk-effect under `planOnly=true`. (c) Mutual-exclusion test asserting the typed error short-circuits before `Plan`. |
| Schema change | None. |
| New typed errors | None. Reuses `errs.NewKitValidation`. |
| Module dependencies | None added. |
| README scope | Two flag-table additions (update + replace). One sentence under each command's existing prose. |

---

### 2. Files

#### Modified

- **`go/cli/transaction/action.go`** *or* new sibling **`go/cli/transaction/print.go`** — adds `PrintPlanDetails`. Decision: new file `print.go` since it's a CLI-output concern and `action.go` is a type-definition file. ~25 lines.
- **`go/cli/transaction/print_test.go`** — table-driven tests for `PrintPlanDetails`. ~80 lines.
- **`go/cli/update/update.go`** — `Update` / `RunUpdate` signature update; one new early-return block after `printPlanSummary`. ~10 lines added.
- **`go/cli/update/update_test.go`** — adds `--plan` variants of existing fixtures (no-op, update, rename, needs-attention, mixed multi-kit). ~80 lines added.
- **`go/cli/replace/replace.go`** — `Replace` / `RunReplace` signature update; one new early-return block after `transaction.Plan`. ~10 lines added.
- **`go/cli/replace/replace_test.go`** — adds `--plan` variants (happy-path replace, pre-flight typed-error short-circuit). ~50 lines added.
- **`go/main.go`** — `newUpdateCmd` and `NewReplaceCmd` gain `--plan` flag registration + the mutual-exclusion check before invoking `RunUpdate` / `RunReplace`. ~20 lines added across the two commands.
- **`README.md`** — adds `--plan` to the flag tables for `inat update` and `inat replace`. Adds one sentence under the Reorganization workflows subsection saying `--plan` previews actions before they're applied.
- **`CHANGELOG.md`** — `[Unreleased]` gains a `### Added` entry pointing at spec 0020.
- **`docs/specs/0017-transactional-update.md`** — single closure line on the §9 "Plan serialization or `--dry-run`" follow-up: "Closed by spec [0020](0020-update-plan-dry-run.md)" (plain-text half only; JSON still deferred).
- **`docs/specs/0018-kit-reorg-auto-rename.md`** — same closure line on the §9 "JSON / dry-run plan output" follow-up, noting JSON is still deferred.

#### New

- `docs/specs/0020-update-plan-dry-run.md` — the contract.
- `docs/plan/0020-update-plan-dry-run.md` — this document.
- `go/cli/transaction/print.go` — `PrintPlanDetails` implementation.
- `go/cli/transaction/print_test.go` — table-driven tests.

---

### 3. TDD test order

1. **`cli/transaction/print_test.go` first.** Table-driven against synthesised `PlannedAction` slices:
   - `ActionUpToDate` → `"up-to-date company/foo\n"`
   - `ActionUpdate` → `"update company/foo\n"`
   - `ActionRename` → `"rename company/foo → company/bar\n"`
   - `ActionReplace` → `"replace company/foo → other/bar\n"`
   - `ActionNeedsAttention` → `"needs-attention company/foo: kit not found\n"`
   - Multi-action slice → lines in plan order, no sort.
   - Empty slice → no output.
2. **`cli/update/update_test.go` — `planOnly=true` variants.** For each existing fixture (no-op, update, needs-attention, rename, mixed):
   - Assert stdout buffer matches expected detail lines.
   - Assert `.inat/config.json` byte-identical pre/post.
   - Assert delivered-file mtimes pre/post unchanged.
   - Assert returned error is nil.
3. **`cli/replace/replace_test.go` — `planOnly=true` variants:**
   - Happy-path replace: detail line on stdout, no on-disk effect, nil error.
   - Pre-flight typed error: `KitValidation` returned, no detail line, no `Plan` call.
4. **`main_test.go` (or wherever mutual-exclusion lives):** `--plan --force` returns `KitValidation`, no `Plan` call. (If no existing `main_test.go` covers flag-parse paths, the cleanest home is to test through `Update`/`Replace` directly with both bool flags set true.)

---

### 4. Implementation steps

1. **Write `PrintPlanDetails`** in `go/cli/transaction/print.go`. Switch on `ActionKind`, format per spec §4.5, emit to the provided writer. ~25 lines.
2. **Run `print_test.go`** — should pass before touching anything else.
3. **Update `cli/update/update.go`:**
   - Add `planOnly bool` to `RunUpdate` and `Update`.
   - After `printPlanSummary(plan)`, insert `if planOnly { transaction.PrintPlanDetails(os.Stdout, plan); return nil }`.
   - Re-run `update_test.go` — pre-existing tests should still pass after threading the bool through.
4. **Add `update_test.go` `planOnly=true` cases.** Run to green.
5. **Mirror in `cli/replace/replace.go`:**
   - Add `planOnly bool` to `RunReplace` and `Replace`.
   - After `transaction.Plan(...)`, insert the same `if planOnly { ... }` block (before the needs-attention short-circuit).
6. **Add `replace_test.go` `planOnly=true` cases.** Run to green.
7. **Wire flags in `go/main.go`:**
   - `newUpdateCmd`: add `var planOnly bool`, `cmd.Flags().BoolVar(&planOnly, "plan", false, ...)`, mutual-exclusion check before `update.RunUpdate`.
   - `NewReplaceCmd`: same, but the cobra wiring lives in `cli/replace/replace.go` not `main.go`. Add there.
8. **Mutual-exclusion test.** Decide: `cli/update/update_test.go` and `cli/replace/replace_test.go` can each cover their respective `(planOnly=true, force=true)` case by asserting the typed error. The cobra-level flag-parse plumbing in `main.go` is thin enough to not need its own test.
9. **README + CHANGELOG.**
10. **Spec-cross-reference closure lines** in 0017 and 0018.
11. **Full local test pass.** `go -C go/ test ./...` clean.
12. **Pre-commit hook pass on commit.**

---

### 5. Verification

- **Local unit tests:** `go -C go/ test ./...` exits 0.
- **Local lint:** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...` all exit 0.
- **End-to-end smoke (manual):**
  - Set up a project with two installed kits via the existing `internal/testfixture` source helper.
  - Run `go -C go/ run . update --plan` — confirm stdout has two detail lines, stderr has the planned-summary, config + delivered files unchanged.
  - Run `go -C go/ run . update` — confirm pre-existing behaviour byte-identical to before this change (compare stderr against a recorded golden).
  - Run `go -C go/ run . replace src/foo other/bar --plan` — confirm one detail line, no disk effect.
  - Run `go -C go/ run . update --plan --force` — confirm typed `KitValidation` error, no plan call.
- **CI parity:** GitLab `test` job runs the same `go test ./...` against `go/`; no new job required.

---

### 6. Risks

- **Caller-signature churn.** Three functions gain a `planOnly` parameter. Internal callers update mechanically. No public API surface outside the `inat` binary, so this is a one-time edit. (If a future call wants to opt out without `planOnly`, default-false propagates naturally.)
- **Stdout-stderr split.** Adding a new stdout writer means tests that captured combined stdio output now need to split capture. The existing tests don't do combined capture (they assert against `os.Stderr` redirected to a buffer), so the new stdout assertions are additive. Minor: confirm no test fixture inadvertently asserts stdout is empty.
- **Format stability commitment.** Spec §4.5 commits to a stable line format. Any future `ActionKind` addition will need a matching `PrintPlanDetails` case; the type-switch is exhaustive in Go's switch-on-int idiom but not enforced by the compiler. Mitigation: `print_test.go` covers every `ActionKind`, so adding a new one without updating the printer surfaces a test failure.

---

### 7. Definition of done

All acceptance criteria from spec 0020 §6 met. The CHANGELOG entry is present. The 0017 and 0018 cross-reference closures are present. The MR description names spec 0020 and links the spec/plan pair.
