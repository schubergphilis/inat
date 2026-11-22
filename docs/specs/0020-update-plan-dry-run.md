## inat — `inat update --plan` / `inat replace --plan` dry-run (Spec 0020)

---

### 1. Overview

Spec [0017](0017-transactional-update.md) split `inat update` and `inat replace` into explicit plan and apply phases. The plan output lives only in memory between the two phases — `transaction.Plan` returns `[]PlannedAction`, `transaction.Apply` consumes it. Spec 0017 §3 listed JSON / dry-run output as out-of-scope; spec [0018](0018-kit-reorg-auto-rename.md) §9 reiterated the same deferral after layering auto-rename detection onto the plan, noting that auto-applied renames make a dry-run more valuable since users may want to inspect the detected renames before they're applied.

This spec closes that gap. It adds a `--plan` boolean flag to both `inat update` and `inat replace` that prints the planned actions to stdout and returns without invoking `transaction.Apply`. No JSON output, no on-disk artefact, no other behavioural change.

A companion implementation plan lives at [`docs/plan/0020-update-plan-dry-run.md`](../plan/0020-update-plan-dry-run.md).

This spec extends [`docs/specs/0017-transactional-update.md`](0017-transactional-update.md) (the plan/apply substrate) and pays down a deferral flagged there and in [`docs/specs/0018-kit-reorg-auto-rename.md`](0018-kit-reorg-auto-rename.md) §9.

---

### 2. Goals

1. `inat update --plan` MUST print one detail line per `PlannedAction` to stdout, then return success (exit 0). No `transaction.Apply` call, no `config.SaveConfig` call, no filesystem writes outside of cache I/O performed by `transaction.Plan` (which already happens today during plan).
2. `inat replace <old> <new> --plan` MUST behave the same way — single `TargetReplace`, single detail line, exit 0.
3. The detail-line format MUST be stable enough for scripted consumers to parse positionally on the action token. Field order: `<action> <key>[ → <newKey>][ <annotation>]`. Action tokens come from `ActionKind.String()` (`up-to-date`, `update`, `replace`, `rename`, `needs-attention`).
4. The existing aggregate plan-summary line (`planned: N update, ... need attention`) MUST continue to go to stderr in `--plan` mode, byte-identical to its non-`--plan` form. This preserves parity with the non-dry-run flow's stderr stream so wrappers that already grep the summary need no changes.
5. `--plan` MUST exit 0 even when one or more planned actions are `ActionNeedsAttention`. The point of a dry-run is to surface what would have been attempted; whether it would succeed is reported in-band via the `needs-attention` action token on the relevant detail line. (Without `--plan`, `update` still exits non-zero on per-kit attention as today.)
6. `--plan` MUST be additive — when absent, the behaviour of `inat update` and `inat replace` is byte-identical to today's stderr stream + exit code + on-disk effect.
7. `--plan` MUST NOT combine with `--force`. `--force` is an apply-phase concern (overwrite locally-modified delivered files); it is meaningless when no apply happens. Passing both is rejected at flag parse with a typed error.
8. No new module dependencies, no new typed errors (one repurposed validation error for `--plan --force`; details in §4.4).

---

### 3. Non-goals

- **JSON output.** `--plan=json` / `--plan-format=json` / `inat update --plan-json` are all explicitly deferred. The plain-text format defined here is the only output mode in this spec. A future spec may add a structured form if a CI consumer materialises.
- **`inat update --plan` triggering side-effect-free planning across more than the targeted kits.** Plan today already fetches every relevant source as a precondition; `--plan` does not widen or narrow that scope.
- **Tree-SHA diffs / per-file diffs in the detail line.** The detail line names the action and the kit (and, for replace/rename, the new key). It does not enumerate the delivered files that would change. That belongs in a future `inat diff` verb.
- **`--plan` on other commands.** `inat install`, `inat remove`, `inat refresh`, `inat rename`, `inat sync`, and `inat status` do not gain `--plan`. Install/remove/rename are single-kit single-step operations with no fetch-then-mutate gap. Refresh is a re-delivery from cache. Sync is already read-only with its own output. Status is offline.
- **Suppressing `transaction.Plan`'s own stderr output.** `Plan` already prints `fetching <source>...` lines and per-kit warnings during the fetch phase. `--plan` does not silence them — the user sees the same fetch trace whether or not they ran in dry-run mode. (Suppressing the trace is a different concern, owned by a future `--quiet` flag.)
- **Validating plan output across both `update` and `replace` runs in the same invocation.** They remain separate commands. `--plan` does not introduce batch-across-commands behaviour.

---

### 4. Design

#### 4.1 Flag

`--plan` (boolean, default `false`) registered on both `newUpdateCmd` (`go/main.go`) and `NewReplaceCmd` (`go/cli/replace/replace.go`). No short form; the long form is the public CLI contract.

`-Plan` PowerShell-style aliases are not introduced; cobra's standard `--plan` works under PowerShell too.

#### 4.2 Update pipeline integration

`update.Update` (`go/cli/update/update.go:36`) gains one parameter:

```go
func Update(dir, kitRef string, force, planOnly bool, homeOverride string) error
```

Pipeline (changes prefixed `+`):

```
1. LoadConfig
2. buildTargets
3. plan := transaction.Plan(...)
4. printPlanSummary(plan)              ← stderr (unchanged)
+5. if planOnly { printPlanDetails(plan); return nil }
6. results := transaction.Apply(...)
7. printResultErrors(results)
8. if anyApplied { SaveConfig }
9. printFinalSummary(results)
10. return needs-attention status
```

`printPlanDetails` is a new private function in `go/cli/update/update.go` that writes one line per `PlannedAction` to **stdout**. Format defined in §4.5.

#### 4.3 Replace pipeline integration

`replace.Replace` (`go/cli/replace/replace.go:55`) gains the same parameter. The pre-flight `parseAndValidate` still runs (so the user sees the same fail-fast typed errors for malformed refs or already-installed targets, exactly as today). After `Plan`, when `planOnly` is set, print one detail line and return:

```go
plan, _ := transaction.Plan(...)
if planOnly {
    printPlanDetails(plan)  // shared helper
    return nil
}
if plan[0].Kind == transaction.ActionNeedsAttention {
    return plan[0].Err
}
// ... Apply, SaveConfig as today
```

`printPlanDetails` is shared between update and replace. Decision (§4.7): live in `cli/transaction` so both callers consume it through a single import.

#### 4.4 `--plan --force` mutual exclusion

`--force` controls overwrite of locally-modified delivered files during `Apply`. With `--plan`, no `Apply` runs. Allowing both would be silently nonsensical. The flag-parse layer rejects the combination with `errs.NewKitValidation("--plan and --force are mutually exclusive")`. The check lives in the cobra `RunE` wrapper (before `RunUpdate` / `RunReplace`) so the error path doesn't reach `transaction.Plan`.

This is the only new "user-visible" error path. No new typed-error variant.

#### 4.5 Detail-line format

One line per `PlannedAction`, lowercase action token first (`ActionKind.String()`), then one space, then the kit identifier(s):

| Action            | Line                                                                       |
| ----------------- | -------------------------------------------------------------------------- |
| `ActionUpToDate`  | `up-to-date <key>`                                                         |
| `ActionUpdate`    | `update <key>`                                                             |
| `ActionRename`    | `rename <key> → <newKey>`                                                  |
| `ActionReplace`   | `replace <key> → <newKey>`                                                 |
| `ActionNeedsAttention` | `needs-attention <key>: <err>`                                        |

Examples:

```
update company/coding-standards
rename company/foo → company/foo-renamed
replace company/foo → other/foo
needs-attention company/zzz: kit not found in registry: zzz
up-to-date company/bar
```

Stability commitments:
- Action tokens are byte-identical to `ActionKind.String()` (today: `up-to-date`, `update`, `rename`, `replace`, `needs-attention`). Adding a new `ActionKind` will add a new token; existing tokens won't change spelling.
- `→` separator (U+2192) for the from/to form on `rename`/`replace`. Single space on either side.
- `<err>` for `needs-attention` is rendered by `fmt.Sprintf("%v", a.Err)` — same shape as the per-kit error line that `printResultErrors` writes to stderr today, so existing greps for typed-error strings keep working.
- Lines are emitted in the order `transaction.Plan` returned them. Plan's ordering (built from the target slice, then any auto-detected renames) is preserved; `--plan` does not sort.

#### 4.6 Output streams

- Detail lines → **stdout**. New consumer; stdout is the natural choice for grep/awk pipelines, and the existing `update` / `replace` commands don't write to stdout today, so claiming it for `--plan` doesn't collide.
- Aggregate plan-summary line → **stderr** (unchanged). The line `printPlanSummary` emits today fires regardless of `--plan`.
- `transaction.Plan`'s fetch/warning trace → **stderr** (unchanged).
- Final-summary line + per-result error lines → **NOT emitted in `--plan` mode**, because `Apply` never runs. Their information is already on the per-action detail lines (`needs-attention <key>: <err>`) for `--plan` callers.

#### 4.7 Where the printer lives

`printPlanDetails` could live in `cli/update` (the original caller), but `cli/replace` would have to import `cli/update` to reuse it — an upward dependency that's awkward. Instead, the printer ships as an exported helper in `cli/transaction`:

```go
// PrintPlanDetails writes one detail line per planned action to w.
// Format is stable per spec 0020 §4.5. Returns no error: w is expected
// to be os.Stdout or an in-memory buffer; I/O failures are not handled
// here (the caller is the CLI exit code authority).
func PrintPlanDetails(w io.Writer, plan []PlannedAction)
```

This matches where `ActionKind.String()` already lives (`cli/transaction/action.go`), so the printer and the token formatter share a package. Test fixtures already in `cli/transaction` can verify the format.

`cli/update` and `cli/replace` both import it via the existing `transaction` import.

---

### 5. Compatibility

- **`go/cli/update/update.go`** — `Update` signature gains a `planOnly bool` parameter (positional, between `force` and `homeOverride`). `RunUpdate` gains the same parameter and threads it through. Callers in `go/main.go` updated.
- **`go/cli/replace/replace.go`** — same: `Replace` and `RunReplace` gain `planOnly bool`. `NewReplaceCmd` registers `--plan` and routes it to `RunReplace`.
- **`go/main.go`** — `newUpdateCmd` gains `var planOnly bool` + flag registration + `--plan --force` mutual-exclusion check.
- **`go/cli/transaction/action.go`** (or a new sibling file): adds `PrintPlanDetails(w io.Writer, plan []PlannedAction)`.
- **`go/cli/transaction/transaction.go`** — no change. Plan / Apply / ActionKind unchanged.
- **`PlannedAction` schema** — unchanged. Read-only consumer added.
- **`.inat/config.json`** — schema unchanged. `--plan` runs do not write config.
- **Other CLI commands** — `install`, `remove`, `refresh`, `rename`, `sync`, `status`, `validate`, `self` all unchanged.
- **Pre-commit hook + CI** — unchanged. New tests run under the existing `go test ./...` gate.
- **`go/go.mod` / `go/go.sum`** — no new modules.
- **README.md** — `--plan` row added to the flag tables for `inat update` and `inat replace`. One-paragraph mention in the "Reorganization workflows" subsection.
- **CHANGELOG.md** — `[Unreleased]` gains a `### Added` entry pointing at spec 0020.

---

### 6. Acceptance criteria

1. **Build clean.** `go -C go/ build ./...` exits 0.
2. **`update --plan` prints details and exits 0.** Project with two installed kits whose upstream is unchanged: `inat update --plan` writes two `up-to-date <key>` lines to stdout, the `planned: ... up-to-date ...` aggregate to stderr, exits 0. `.inat/config.json` mtime unchanged. No delivered files touched.
3. **`update --plan` with an actionable update.** Project with one installed kit whose source has a new commit changing the kit tree: `inat update --plan` writes `update <key>` to stdout, exits 0, `.inat/config.json` unchanged, no delivered files touched. Running `inat update` (without `--plan`) after that produces the same kit state mutation it would have produced absent the prior `--plan` call — i.e. `--plan` is genuinely side-effect-free against config + delivered files.
4. **`update --plan` shows auto-rename.** Project with one installed kit whose source has the kit moved by a pure `git mv` (spec 0018 case): `inat update --plan` writes `rename <oldKey> → <newKey>` to stdout, exits 0, no on-disk effect. Running the same command without `--plan` performs the rename.
5. **`update --plan` shows needs-attention without exiting non-zero.** Project with one installed kit whose source upstream deleted the kit: `inat update --plan` writes `needs-attention <key>: <err>` to stdout, exits 0. The same command without `--plan` (single-kit form) exits non-zero with the typed error.
6. **`update --plan` (multi-kit) mixes actions.** Project with three kits — one up-to-date, one updated, one needs-attention: `inat update --plan` writes three detail lines (in plan order), exits 0. The non-`--plan` form exits non-zero with `KitsNeedAttention(1)`.
7. **`replace --plan` prints replace detail and exits 0.** Project with kit `src/old` installed, source `other` has kit `foo`: `inat replace src/old other/foo --plan` writes `replace src/old → other/foo` to stdout, exits 0, no on-disk effect.
8. **`replace --plan` fails fast on pre-flight typed errors.** `inat replace bad-ref other/foo --plan` returns `KitValidation`, exits non-zero, prints no plan detail. (The pre-flight `parseAndValidate` runs before `Plan`, so its typed errors short-circuit `--plan` the same way they short-circuit a real run.)
9. **`--plan --force` rejected.** `inat update --plan --force` exits non-zero with `KitValidation: --plan and --force are mutually exclusive`. No fetch, no plan. Same for `inat replace ... --plan --force`.
10. **Detail-line format stability.** Test asserts byte-for-byte format per §4.5 for each action kind, including the U+2192 arrow byte sequence.
11. **Plan order preserved.** Detail lines appear in the same order as `transaction.Plan`'s returned slice. The printer is `for _, a := range plan { ... }`.
12. **No new dependencies.** `git diff go/go.mod go/go.sum` between pre-change and post-change tree is empty.
13. **File / function size limits.** `PrintPlanDetails` ≤30 lines. No file crosses the 300-line target.
14. **Pre-commit hook clean.** `gofmt -l go/`, `go -C go/ vet ./...`, `staticcheck ./...`, `go -C go/ test ./...` all exit 0.

---

### 7. Testing notes

- **`cli/transaction/print_plan_test.go`** (new) — table-driven test asserting `PrintPlanDetails` output for synthesised `PlannedAction` slices covering each `ActionKind`. Buffer-based, no filesystem.
- **`cli/update/update_test.go`** — extend the existing multi-kit / single-kit fixtures with a `planOnly=true` variant; assert (a) detail lines on stdout, (b) `.inat/config.json` byte-identical to pre-call, (c) delivered file mtimes byte-identical, (d) exit nil.
- **`cli/replace/replace_test.go`** — analogous: add a `planOnly=true` variant; assert detail line + no on-disk effect.
- **`cli/update/update_test.go`** — add a `planOnly=true, force=true` test asserting the typed error from the mutual-exclusion check (no plan call).
- The existing `cli/transaction` test substrate provides synthesised sources (`internal/testfixture`) — reuse for the auto-rename `--plan` case (acceptance criterion 4).

---

### 8. Rollout

1. Spec + plan (this pair) committed first as a sketch — the plan numbers them as the first commit.
2. New `PrintPlanDetails` in `cli/transaction` with table-driven tests.
3. `Update` signature update + flag wiring in `cli/update` and `main.go`, with tests.
4. `Replace` signature update + flag wiring in `cli/replace`, with tests.
5. `--plan --force` mutual exclusion + tests.
6. README + CHANGELOG.
7. One MR off `main`, squash-on-merge.

---

### 9. Out-of-scope follow-ups

- **JSON output.** `--plan` writes plain text only. A `--plan-format=json` follow-up could emit a JSON array of `PlannedAction`-shaped objects; substrate is unaffected. Defer until a CI consumer asks.
- **`inat diff`.** A per-file diff between current and would-be-delivered state across a planned action. Bigger scope (needs a renderable diff format, file-level granularity), separate spec.
- **`--plan` on `inat refresh`.** Refresh re-delivers from cache. A dry-run would enumerate files that would be rewritten; useful, but the action set is "deliver" only (no replace / rename), and refresh's contract is different enough to deserve its own spec.
- **`--quiet` to suppress `Plan`'s fetch trace under `--plan`.** Useful if `--plan` output is piped into a tool that wants only the plan lines. Adds a flag; weigh the value before adding.
- **Sync ↔ plan integration.** `inat sync` today walks every installed kit and prints suggestion lines; conceptually `update --plan` is a strict subset of sync minus the fetch every source. A future spec may consolidate the two outputs. Out of scope here.
