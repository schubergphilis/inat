# Plan — `inat update` aborts on locally-modified files instead of overwriting them

Lands inside spec 0001's existing contract (§ Rules, item 9 "Modified file detection"); no new spec. Fixes an issue on the internal GitLab tracker.

## Why

`inat update` without `--force` printed `warning: <path> modified locally, skipping` and then overwrote that file with the new kit content. The local edit was lost, the run exited 0, and the summary reported the kit as `updated`. In a project folder that is not a git repo the edit was unrecoverable.

Cause: `removeAndDeliver` in `go/cli/transaction/apply.go` discarded the skipped list returned by `removecmd.RemoveDeliveredFiles`, and `deliverAcrossAgents` then wrote every file of the new kit, including the skipped ones. `replaceCore` already checked the same list and aborted via `driftAbort`; the update path did not.

## Decisions

- **Abort the kit, don't skip the file.** Spec 0001 says the operation "warns and skips that file". Keeping the user's file while advancing the recorded tree SHA would leave the kit half-updated, and the next `update` would report it up to date. Aborting the kit matches what `replace` already does (spec 0016 §6 criterion 11, spec 0017 criterion 8): the kit surfaces as `needs attention`, exit is non-zero (`KitsNeedAttention`), and `--force` is the documented override.
- **Rollback via the existing snapshot.** The abort happens after unmodified files have already been removed. Returning the error from inside the `runWithSnapshot` closure restores them (spec 0021), so the kit is left exactly as it was. No new rollback code.
- **Apply-time check, not plan-time.** Detecting drift in `Plan` would let `inat update --plan` show it up front, but `Plan` has no `force` input and `plan.go` is near the 400-line cap. Deferred; the apply-time guard is needed regardless as the backstop.
- **Other kits in the batch are unaffected.** Per-kit isolation from spec 0017 applies: the drifted kit is `needs attention`, the rest of the batch proceeds.

## Changes

- `go/cli/transaction/apply.go` — `removeAndDeliver` keeps the skipped list and returns `driftAbort("update", …)` when it is non-empty and `force` is false. `driftAbort` takes the verb so the stderr line reads `update aborted: …` or `replace aborted: …`.
- `go/cli/update/update_drift_test.go` — new regression tests: drift without `--force` keeps the edit and the recorded tree and returns `KitsNeedAttention`; non-drifted files are restored after the abort; `--force` delivers the new content.
- `CHANGELOG.md` — `Fixed` entry under Unreleased.

## Verification

1. `gofmt -l .`, `go vet ./...`, `staticcheck ./...`, `go test ./...` under `go/` — clean. The new tests fail on `main` (`want drift error, got nil`) and pass with the fix.
2. Manual repro from the issue: install a kit, edit a delivered skill, push an upstream change to that file, `inat update <kit>`. Expect `update aborted: <kit> has 1 locally-modified file(s); re-run with --force to discard them`, `0 updated … 1 need attention`, non-zero exit, and the edit intact. `inat update <kit> --force` then delivers the new content.
