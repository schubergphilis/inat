# Plan — `inat update` applies a kit that gives up a name before the kit that takes it

Implements spec [0026](../specs/0026-update-cross-kit-order.md), which amends spec 0017 §4.4 step 5 (`Plan` no longer returns strictly in input order).

## Why

`TestUpdateCrossKitMigration` (`go/cli/update`) fails about 3 runs in 20 and has blocked CI on `main` and the pre-commit hook. The failure is a real bug in `inat update`, not in the test.

When a skill moves from kit-a to kit-b in the source, `inat update` updates both kits in one batch. `Apply` (`go/cli/transaction/apply.go`) runs the plan in target order, and `buildTargets` (`go/cli/update/update.go`) builds the targets by ranging over the `cfg.Installed` map, so the order is random:

- kit-a first: kit-a removes `.claude/skills/shared/`, kit-b delivers its copy. Correct.
- kit-b first: kit-b writes its `shared/SKILL.md` over kit-a's. kit-a's remove step then hashes that file against kit-a's recorded hash, sees a mismatch, reports it as locally modified, and plan 0022 aborts kit-a (`update aborted: test-source/kit-a has 1 locally-modified file(s)`). With `--force` it is worse: kit-a deletes the file kit-b just delivered.

A user whose kit source moves a skill between kits gets a false "needs attention" on some runs.

Acceptance criteria and non-goals (including the swap case) are in spec 0026 §3–§4.

## Decisions

- **Order the plan, not the remove step.** The alternative was to let a kit skip removing (and drift-checking) paths that another kit in the batch takes over. That breaks when the taking kit then fails: the file is left on disk owned by nobody. Ordering keeps each kit's apply self-contained and its snapshot restore unchanged.
- **Order in `transaction.Plan`, after `checkBatchUniqueness`.** Both `update` and `replace` go through `Plan`, so both get the fix, and `--plan` prints the order `Apply` will use. The edge rule: action A must run before action B when a name A records today and does not keep (`cfg.Installed[A].Names − A.NewNames`) is one B takes (`B.NewNames`). Only actionable entries have edges. A stable topological sort (Kahn, ties broken by the incoming position) keeps the incoming order wherever no edge applies; on a cycle the remaining entries are appended in incoming order.
- **New file `plan_order.go`.** `plan.go` is at 367 lines (target 300, max 400).
- **Sort `buildTargets`.** Determinism on its own does not fix the bug (a migration from kit-b to kit-a would then fail every time), but it makes output stable and failures reproducible.

## Changes

1. `go/cli/transaction/plan_order_test.go` — new, written before the fix: `Plan` + `Apply` with targets in taker-first order (`[kit-b, kit-a]` for a kit-a → kit-b migration) succeeds and delivers kit-b's content; unit tests for the ordering (donor moved ahead of taker, no edges keeps the incoming order, a cycle keeps the incoming order, non-actionable entries add no edges).
2. `go/cli/update/update_test.go` — a migration in the other direction (kit-b → kit-a), so with sorted targets the taker comes first and the end-to-end test covers the reorder.
3. `go/cli/transaction/plan_order.go` — `orderDonorsFirst(cfg, plan) []PlannedAction`; `Plan` returns its result.
4. `go/cli/update/update.go` — `buildTargets` sorts the install keys.
5. `CHANGELOG.md` — `Fixed` entry under Unreleased.
6. `.claude/OPEN.md` — replace the flaky-test item with the swap case.

## Verification

1. The new taker-first test fails on `main` with the `update aborted … locally-modified` error and passes with the fix.
2. `go test ./cli/update/ -run TestUpdateCrossKit -count=50` and `go test ./cli/transaction/ -count=20` pass.
3. `gofmt -l .`, `go vet ./...`, `staticcheck ./...`, `go test ./...` under `go/` are clean (the pre-commit hook).
4. Manual run with a built binary against a local source repo: install kit-a and kit-b, move a skill from kit-b to kit-a in the source, `inat update --plan` lists kit-b before kit-a, `inat update` reports `2 updated` and the skill has kit-a's content.
5. The MR pipeline is green.
