# Plan — delivered-file hashes ignore CRLF vs LF line endings

Lands inside spec 0001's existing contract (§ Rules, item 9 "Modified file detection"); no new spec. Fixes an issue on the internal GitLab tracker.

## Why

Delivered-file hashes are byte-exact SHA-256. On Windows with `core.autocrlf=true`, every git checkout of a consumer project (clone, branch switch, stash pop) rewrites delivered files with CRLF line endings. inat then reports each one as `modified locally`, although only the line endings changed.

Before plan 0022 this was noise: a warning, followed by the overwrite bug. Since plan 0022, `inat update` aborts a kit that has locally-modified files. Shipping 0022 without this fix would make `update` fail for every kit in a Windows clone, and the only way out would be `--force`, which also discards real edits.

## Decisions

- **Normalise CRLF to LF before hashing.** `hash.HashBytes` replaces every `\r\n` with `\n` before SHA-256, so the recorded hash and the on-disk check agree regardless of checkout line endings. A lone `\r` is left alone: git's autocrlf never produces one.
- **All files, not only text.** git does not apply autocrlf to files it detects as binary, so those keep their bytes and hash as before. The only side effect: a binary asset whose only change is `\r\n` ↔ `\n` would not count as modified. That is acceptable for a drift warning and avoids guessing whether a file is text.
- **Hashes already in `config.json` stay valid.** Existing configs record raw byte hashes. The modified check accepts a file if *either* its normalised or its raw hash matches the recorded value. Nobody sees a false drift after upgrading, and a config needs no migration. Hashes get the normalised form the next time a file is delivered.
- **One comparison helper.** A new `hash.IsMatch(content, expected)` holds the either-or rule. `IsModified` (refresh, remove, and the update/replace drift check) and the built-in rule refresh in `go/agents/builtin/refresh.go` both use it, so the rule can't drift between call sites.
- **Out of scope:** rewriting delivered files to a fixed line ending; the tree-SHA comparison in `inat status` (a git object hash, unaffected); line endings inside the `CLAUDE.md` managed section (not hash-checked).

## Acceptance criteria

1. A delivered file converted to CRLF, with no other change, is not reported as modified by `refresh`, `remove`, `update` or `replace`, and `update` does not abort.
2. A delivered file with a real content edit is still reported as modified, whether its line endings are LF or CRLF.
3. A project whose `config.json` holds hashes from v0.7.0 (raw bytes) shows no drift for unmodified files, LF or CRLF.
4. The built-in `inat-usage` / `inat-kits` rule refresh treats a CRLF copy of the embedded rule as unmodified.

## Changes

- `go/core/hash/hash.go` — normalisation in `HashBytes`; new `IsMatch`; `IsModified` uses it. Doc comment: no longer byte-for-byte identical to the Rust archive.
- `go/core/hash/eol_test.go` — CRLF/LF equality, lone `\r` untouched, legacy raw hash accepted, real edit still detected.
- `go/agents/builtin/refresh.go` — `decideAndApply` takes the on-disk bytes and compares through `IsMatch` instead of string-equal hashes.
- `go/agents/builtin/refresh_crlf_test.go`, `go/cli/update/update_crlf_test.go`, plus new cases in `go/cli/refresh/refresh_test.go` and `go/cli/remove/remove_test.go` — command-level regression tests for criteria 1, 2 and 4.
- `go/internal/testfixture/crlf.go` — `ConvertToCRLF` helper that simulates an autocrlf checkout.
- `CHANGELOG.md` — `Fixed` entry under Unreleased.

## Verification

1. TDD: the new tests fail on `main` first (CRLF file reported as modified), then pass. `gofmt -l .`, `go vet ./...`, `staticcheck ./...`, `go test ./...` under `go/` are clean (the pre-commit hook).
2. Manual run with the built binary, in a temp project with a local git source: install a kit; convert the delivered files to CRLF (`sed -i 's/$/\r/'`); run `inat refresh`, then `inat update` after an upstream change. Expect no `modified locally` warning, no abort, and new content delivered. Repeat with one real edit plus CRLF: expect `update aborted: … 1 locally-modified file(s)`.
3. Legacy config: in the same project, overwrite one recorded hash with the raw-byte hash v0.7.0 would have written, then `inat refresh`. Expect no drift.
