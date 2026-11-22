# Plan — `inat refresh` keeps locally-modified files it reports as skipped

Lands inside spec 0001's existing contract (§ Rules, item 9 "Modified file detection"); no new spec. Fixes an issue on the internal GitLab tracker.

## Why

`inat refresh` without `--force` printed `warning: <path> modified locally, skipping (use --force to override)` and `refreshed <kit> (1 file(s) skipped, locally modified)`, then overwrote that file with the kit content. The edit was lost and the run exited 0.

Cause: `refreshSingle` in `go/cli/refresh/refresh.go` builds the `skipped` list via `collectModified`, uses it only for the summary line, and then calls `install.DeliverToAgent`, which writes every file of the kit.

## Decisions

- **Skip the file, don't abort the kit.** `update` aborts (plan 0022) because skipping one file while advancing the recorded tree SHA would leave the kit half-updated. Refresh never moves the tree SHA, so that argument does not apply. Spec 0001 says the operation "warns and skips that file", the built-in rule refresh (spec 0011 §5) already skips per file, and refresh's main job is restoring deleted files: aborting the kit would refuse to restore a deleted skill because a sibling file was edited. The skipped file keeps its old recorded hash, so it keeps showing as modified until `--force`.
- **Filter at the write, not at the input.** Dropping the blob from `allFiles` works for skill files but breaks contextual rules (`findFileContent` errors) and would also drop the managed-section reference bullet that refresh must restore. Instead `DeliverToAgent` takes a `skip` set of project-relative delivered paths (the same strings recorded in `Delivered[].Path`): a skill file or contextual rule file in the set is not written and not returned; the contextual reference bullet and always-rule blocks are still re-delivered. Install, `agents add` and transaction apply pass `nil`.
- **Split `install.go` first.** It is at 372 lines (target 300, max 400). `DeliverToAgent` and its delivery helpers move to `go/cli/install/deliver.go` as a pure move in its own commit, before the behaviour change.
- **Exit code unchanged.** Refresh exits 0 with skips, as the built-in rule refresh does today. The summary line already reports the count.

## Changes

1. `go/cli/install/deliver.go` — new; `DeliverToAgent`, `deliverAlwaysRule`, `deliverContextualRule`, `extractSkillFiles`, `findFileContent` moved out of `install.go` unchanged.
2. `go/cli/refresh/refresh_drift_test.go` — new regression tests, written before the fix:
   - refresh without `--force` keeps an edited contextual rule (`.inat/rules/style.md`) and an edited skill file;
   - in the same run a deleted sibling file is still restored;
   - the recorded hash of the edited file is unchanged (it still counts as modified).
   `TestRefreshForceOverwrites` already covers `--force`.
3. `go/cli/install/deliver.go` — `DeliverToAgent(..., skip map[string]bool)`; skill files and contextual rule files whose delivered path is in `skip` are neither written nor returned. The per-entry work moves into `deliverSkillEntry` / `deliverRuleEntry` to keep `DeliverToAgent` under the function-size cap. Callers in `install.go`, `agents/agents.go`, `transaction/apply.go` pass `nil`.
4. `go/cli/refresh/refresh.go` — `collectModified` returns the set; `refreshSingle` passes it to `DeliverToAgent`.
5. `CHANGELOG.md` — `Fixed` entry under Unreleased.

## Verification

1. The new tests fail on `main` (`skill edit overwritten: "# Review\nDo code review."`, `rule edit overwritten: "Follow style guidelines."`) and pass with the fix.
2. `gofmt -l .`, `go vet ./...`, `staticcheck ./...`, `go test ./...` under `go/` — clean.
3. Manual repro from the issue with a built binary: install a kit, edit a delivered skill, delete another delivered file, `inat refresh`. Expect the warning, `refreshed <kit> (1 file(s) skipped, locally modified)`, the edit intact, the deleted file back. `inat refresh --force` then restores the kit content.
