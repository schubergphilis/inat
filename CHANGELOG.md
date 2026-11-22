# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.9.0] - 2026-11-22

inat moves to public GitHub as open source (spec 0027). Releases,
installers and `inat self update` now use GitHub Releases and need no
token. **Re-install once:** a v0.8.x or older binary looks for updates
on the old internal GitLab and cannot update itself to GitHub. Run the
one-liner from the README; `inat self update` works from then on.

### Changed

- Releases are published on
  [GitHub Releases](https://github.com/schubergphilis/inat/releases).
  `install.sh`, `install.ps1` and `inat self update` download
  anonymously; `INAT_TOKEN` and `INAT_TOKEN_TYPE` are no longer read.
  Releases flagged as pre-release on GitHub are skipped unless asked
  for, like `-` tags.
- Go module path is `github.com/schubergphilis/inat`.
- The built-in `inat-usage` and `inat-kits` rules link to
  <https://github.com/schubergphilis/inat>. Projects pick that up on
  their next `inat refresh`.
- CI is GitHub Actions: lint and tests on Linux, Windows and macOS,
  the installer test suites, and a tag-triggered release workflow.
  Dependabot watches Go modules and Actions.

### Removed

- The retired Rust implementation and the GitLab CI configuration are
  no longer part of the repository.

### Fixed

- `update` no longer aborts a kit as "locally-modified" when a skill
  moves to another installed kit in the same batch. The kit giving up a
  name now applies before the kit taking it, and kits are updated in
  sorted order, so `update --plan` lists them in the order they will
  run. Spec [`0026-update-cross-kit-order`](docs/specs/0026-update-cross-kit-order.md).

## [0.8.0] - 2026-11-16

The user scope. `--user` runs any command against your home directory
as if it were a project, so a kit installed there applies in every
session (spec 0025). Upgrade every machine that shares a home
directory: an older binary running `agents add/remove --global` drops
the user scope's sources and kits.

### Added

- `--user` selects the user scope: sources, kits and agents kept in
  `~/.inat/config.json` and delivered to the agent's user-level targets
  (`~/.claude/skills/`, `~/.claude/CLAUDE.md`, `~/.inat/rules/`), so a
  kit applies in every session. Accepted by every command except
  `init`, `validate` and `self`; no `init` is needed. `install` and
  `update` warn when a name they deliver is also installed in the other
  scope. Spec [`0025-user-scope`](docs/specs/0025-user-scope.md).

### Changed

- Running a project command with the home directory as the cwd is now
  refused (`use --user for the user scope`); it used to read the global
  agents file as a project config.
- The home directory is `%USERPROFILE%` on Windows for every scope, as
  Claude Code resolves it. Where `$HOME` differed, the source cache
  re-clones once and `agents add --global` entries need re-adding.
- `agents add/remove --global` now keeps the user scope's sources and
  installed kits when it rewrites `~/.inat/config.json`. **Binaries up
  to 0.7.2 do not**: running `--global` from an older binary drops
  them, leaving the delivered files untracked.

## [0.7.2] - 2026-10-28

Two small fixes. `inat refresh` no longer overwrites a delivered file
it reports as skipped: without `--force` the edit is kept and the rest
of the kit is still restored. The built-in `inat-usage` and
`inat-kits` rules now link to the project's GitLab home instead of
GitHub; projects pick that up on their next
`inat refresh`.

### Fixed

- `inat refresh` no longer overwrites locally-modified delivered files.
  It warned that a modified file was being skipped and then re-delivered
  the kit content over it anyway. Without `--force` the file is now
  kept, and the rest of the kit (deleted files, managed-section
  entries) is still restored; the file keeps reporting as modified
  until `inat refresh --force`. Plan
  [`0024-refresh-drift-skip`](docs/plan/0024-refresh-drift-skip.md).
- The built-in `inat-usage` and `inat-kits` rules linked
  a non-existent GitHub path instead of the project's home; they now
  link the project's GitLab home. An unmodified delivered copy is
  replaced on the next `inat refresh`.

## [0.7.1] - 2026-10-28

Two fixes to locally-modified-file detection. `inat update` no longer
overwrites a delivered file it reports as skipped: without `--force`
the kit aborts as `needs attention` and keeps the edit. Delivered
files whose only change is CRLF line endings, as a Windows checkout
under `core.autocrlf=true` produces, no longer count as modified, so
that fix does not make `update` abort in every Windows clone.
Hashes recorded by earlier versions stay valid; no migration needed.

### Fixed

- `inat update` no longer overwrites locally-modified delivered files.
  It warned that a modified file was being skipped and then delivered
  the new kit content over it anyway. Without `--force` the kit now
  aborts as `needs attention` (non-zero exit, `update aborted: …` on
  stderr) and the snapshot restores the kit's files, matching `inat
  replace`. `--force` still overwrites. Plan
  [`0022-update-drift-abort`](docs/plan/0022-update-drift-abort.md).
- Delivered files no longer read as locally modified when only their
  line endings changed. A Windows checkout under `core.autocrlf=true`
  rewrites them with CRLF; with the previous fix that would have made `inat
  update` abort for every kit. Hashes now normalise CRLF to LF, and
  hashes recorded by earlier versions are still accepted, so existing
  projects need no migration. Plan
  [`0023-eol-insensitive-hash`](docs/plan/0023-eol-insensitive-hash.md).

### Added

- Root `NOTICE` file declaring the project copyright (Schuberg Philis,
  2026), the direct third-party Go modules (`spf13/cobra`,
  `go-git/go-git/v5` — both Apache-2.0), and a note that the `rust/`
  subtree is an unmaintained pre-v0.3 archive covered by the root
  `LICENSE`. Apache-2.0 best practice ahead of an OSS release request;
  complements the per-file SPDX headers added in the previous entry.
- SPDX Apache-2.0 license headers on every `.go` source file under
  `go/**` (82 files). Closes the §4.1.1c "license terms and headers"
  gap from the SBP Open Source Policy ahead of an OSS release request.
  Form is the modern compact one-liner — `// SPDX-License-Identifier:
  Apache-2.0` plus `// Copyright 2026 Schuberg Philis` — above the
  `package` declaration. No behavior change; diff is purely additive.

## [0.7.0] - 2026-05-20

On-disk snapshot/restore for the apply phase of `inat update` and `inat
replace` (spec / plan 0021). Before each kit's apply, the current bytes
of its delivered files and the active agents' target files are copied
into `~/.inat/snapshots/<UTC-timestamp>-<sanitized-install-key>/`;
successful applies discard the snapshot, failed applies restore from it
and leave the directory for forensics. Closes the files-gone limitation
from spec 0016 §3 and spec 0017 §9: `inat replace`'s install-half
failure now restores the old kit's delivered files byte-identically to
pre-apply, and its drift-abort (no `--force`) restores any files
`RemoveDeliveredFiles` had already deleted before the drift check
fired. Two new typed errors surface restore outcomes:
`SnapshotCaptureFailed` (capture aborted before any mutation) and
`ApplyRestoreFailed` (apply failed AND restore failed; `errors.Is` /
`errors.As` walk both wrapped errors via `errors.Join`).

### Added

- On-disk snapshot/restore for the apply phase of `inat update` and
  `inat replace` (spec / plan 0021). Before each kit's apply, the
  current bytes of its delivered files and the active agents' target
  files (CLAUDE.md, the contextual-reference target) are copied into
  `~/.inat/snapshots/<UTC-timestamp>-<sanitized-install-key>/`.
  Successful applies discard the snapshot; failed applies restore from
  it and leave the directory for forensics. Two new typed errors:
  `SnapshotCaptureFailed` (capture aborted before any mutation) and
  `ApplyRestoreFailed` (apply failed AND restore failed; `errors.Is` /
  `errors.As` walk both wrapped errors via `errors.Join`).

### Changed

- `inat replace`'s install-half failure now restores the old kit's
  delivered files byte-identically to pre-apply, closing the
  files-gone limitation from spec 0016 §3 and spec 0017 §9.
  Previously the user was directed to `inat refresh` to recover; the
  `ReplaceInstallFailed` error message text is updated to drop that
  suggestion (typed-error class and constructor signature unchanged).
- `inat replace`'s drift-abort (no `--force`) now also restores any
  files `RemoveDeliveredFiles` had already deleted before the drift
  check fired. The drifted file itself stays in its user-modified
  state (the snapshot captured those bytes pre-apply).

## [0.6.0] - 2026-05-19

Kit reorganization workflows: `inat sync`, `inat rename`, and `inat
replace` (spec / plan 0016) cover the upstream-divergence cases that
previously required uninstall-then-reinstall — in-source `git mv`,
cross-source moves, and content-drift renames — with snapshot rollback
on the install-half of `replace`. `inat update` (and `inat replace`)
now run as explicit plan and apply phases (spec / plan 0017): the plan
phase fetches every relevant source and validates name uniqueness
across the batch, so a kit whose plan fails is reported
needs-attention without any of its delivered files being touched.
Multi-kit `update` skips kits that need attention and continues,
saving whatever updated cleanly and exiting non-zero with
`KitsNeedAttention(N)`. `update` also auto-detects pure-`git mv`
renames upstream and applies them in-place (spec / plan 0018) under
strict tree-SHA equality — the previous round-trip through `inat sync`
+ `inat rename` is gone for that case. `inat rename` now rewrites
stale always-rule managed-section headers across every active agent's
target file (spec / plan 0019) so `inat refresh` is no longer required
to clean them up. `inat update --plan` / `inat replace --plan` (spec
/ plan 0020) preview the planned actions to stdout without applying;
mutually exclusive with `--force`. Fix: dropped the post-sign
`rcodesign verify` call in `ci/build-release.sh` that broke under
rcodesign 0.29.0 (spec 0014); `rcodesign sign`'s exit code is the
gate.

### Added

- **`inat sync`, `inat rename`, `inat replace` for kit reorganization
  workflows.** Sync fetches every configured source and reports each
  installed kit that's been renamed (in-source `git mv`), moved
  between sources, or deleted upstream — read-only, exits non-zero on
  any divergence so CI can gate on drift. Rename rewrites the install
  key for the pure `git mv` case (tree-SHA equality enforced) without
  touching delivered files. Replace atomically removes the old install
  and installs the new kit (cross-source moves, content-drift cases),
  with snapshot rollback on install-half failure. Spec / plan
  [`0016-kit-reorg-baseline`](docs/specs/0016-kit-reorg-baseline.md).
- **`inat update --plan` and `inat replace --plan` preview planned
  actions without applying.** Both commands gain a `--plan` boolean
  flag that stops the pipeline after `transaction.Plan` and writes one
  detail line per planned action to stdout, returning success even when
  one or more actions would have been `needs-attention`. The
  per-action format is stable for scripted consumers: `<action> <key>`
  for `up-to-date` and `update`, `<action> <key> → <newKey>` for
  `rename` and `replace`, and `needs-attention <key>: <err>` for
  failures. The aggregate `planned: N update, N rename, ...` summary
  continues to go to stderr unchanged. `--plan` is mutually exclusive
  with `--force` (the latter is an apply-phase concern). No
  filesystem writes, no `.inat/config.json` mutation. JSON output is
  deliberately deferred to a future spec. Spec / plan
  [`0020-update-plan-dry-run`](docs/specs/0020-update-plan-dry-run.md).

### Changed

- **`inat update` now auto-detects and applies pure-`git mv` kit
  renames.** When `Plan` finds a target kit absent from its source's
  current registry but its recorded tree SHA is present at exactly one
  different name in the same source, the action plans as `Rename` and
  the apply phase rewrites the install key plus the always-rule
  managed-section headers — same semantics as the explicit `inat rename`
  verb (spec 0019). Strict tree-SHA equality only; cross-source moves
  and content-drift cases continue to route through `inat sync` and
  the explicit `inat rename` / `inat replace` verbs. The plan-summary
  line and final summary gain a `rename` term — field positions shift,
  so scripts that parse positionally should migrate to token-name
  matching (`grep -oE '[0-9]+ rename'`). **Exit-code change for
  scripted consumers:** `inat update` against a kit that was renamed
  in-source upstream now exits 0 with `renamed src/old → src/new` on
  stderr, where today it exits non-zero with `KitNotFound`. Scripts
  relying on the non-zero exit to alert on "kit moved" should grep
  stderr `^renamed ` lines instead. Genuine `KitNotFound` (upstream
  deletion), `RenameAmbiguous` (multi-candidate), and content-drift
  cases continue to exit non-zero. Spec / plan
  [`0018-kit-reorg-auto-rename`](docs/specs/0018-kit-reorg-auto-rename.md).
- **`inat update` and `inat replace` now run in two explicit phases —
  plan and apply.** The plan phase fetches every relevant source,
  validates name uniqueness across the post-update batch, and produces
  an in-memory plan; the apply phase performs disk writes against that
  plan. The user-visible CLI surface is unchanged, but failure
  semantics improve: a kit whose plan fails (source unreachable, kit
  deleted upstream, parse error, derived-name collision) is now
  reported as needs-attention without any of its delivered files being
  touched. Cross-kit name migration (skill `foo` moving from kit-a to
  kit-b in the same update batch) now succeeds where it previously
  failed under the per-kit uniqueness check. The disk-write residual
  risk during apply is documented in the spec's non-goals; recovery
  via `inat refresh` is unchanged. Spec / plan
  [`0017-transactional-update`](docs/specs/0017-transactional-update.md).
- **`inat update` (multi-kit form) now skips kits that need attention
  and continues.** Per-kit errors are captured into a needs-attention
  bucket, the loop runs to completion, the config is saved with
  whatever updated cleanly, a summary footer is printed to stderr, and
  the command exits non-zero with `KitsNeedAttention(N)` pointing at
  `inat sync` for guidance. The targeted form `inat update <src/kit>`
  keeps its fail-fast contract (single-kit error returned
  byte-identically). Spec
  [`0016-kit-reorg-baseline`](docs/specs/0016-kit-reorg-baseline.md).
- **`inat rename` now rewrites stale always-rule managed-section
  headers in place.** Always-priority rule blocks delivered into
  agent files (e.g. `CLAUDE.md`) embed `source/kit/filename` in their
  header. Pre-spec-0019, after `inat rename` those headers stayed
  pointed at the old key until the next `inat refresh`. Now rename
  itself rewrites the affected headers across every active agent's
  always-target before saving config — `inat refresh` is no longer
  required to clean them up. Spec / plan
  [`0019-rename-header-rewrite`](docs/specs/0019-rename-header-rewrite.md).

### Fixed

- **`release:build:darwin-*` no longer trips on rcodesign's broken
  `verify` subcommand.** The post-sign `rcodesign verify` call in
  `ci/build-release.sh` (added by spec 0014 §4.3 as cheap insurance)
  failed in v0.5.0's release pipeline with `CMS error: missing further
  values (at position 0)` against a successfully-signed Mach-O. This
  is a known upstream bug in rcodesign 0.29.0; the tool itself prints
  a self-deprecating warning telling callers to use Apple's tooling
  instead. The `verify` line is removed; `rcodesign sign`'s exit code
  is the gate. Spec 0014 §4.3 updated with a strikethrough rationale.

## [0.5.0] - 2026-04-30

macOS release binaries (spec / plan 0014) and the in-place `inat self
update` subcommand (spec / plan 0015). Tags now publish four archives —
Linux x86_64, Windows x86_64, macOS Intel, macOS Apple Silicon — with
the darwin pair ad-hoc-signed via `rcodesign` so they launch on Apple
Silicon. `inat self update` upgrades the running binary in place by
mirroring `install.sh`'s contract: GitLab releases API for tag
discovery, generic package registry for archive download, sibling
`SHA256SUMS` for verification, `INAT_TOKEN` env for auth. The swap
uses a uniform rename-self-aside pattern (Linux + macOS + Windows) so
there is no OS-tagged source. No new module dependencies.

### Added

- **`inat self update` upgrades the running binary in place.** Resolves
  the latest stable release from the package registry, downloads the
  archive matching the host triple, verifies its SHA-256 against the
  published `SHA256SUMS`, and swaps the binary atomically using a
  rename-self-aside pattern that works uniformly on Linux, macOS, and
  Windows. Same `INAT_TOKEN` env contract as `install.sh`; no new
  module dependencies. Subcommands: `inat self update [--check]
  [--version vX.Y.Z] [--force]`. macOS upgrades sidestep the
  Gatekeeper quarantine prompt because Go's `net/http` does not set
  `com.apple.quarantine` on downloads. Spec / plan
  [`0015-self-update`](docs/specs/0015-self-update.md).

- **macOS release binaries (Intel + Apple Silicon).** Tags now publish
  `inat-<v>-x86_64-apple-darwin.tar.gz` and
  `inat-<v>-aarch64-apple-darwin.tar.gz` alongside the existing Linux
  and Windows artifacts. Both are cross-compiled from the same Linux
  CI container as the other targets and ad-hoc-signed via `rcodesign`
  (pinned to `apple-codesign/0.29.0`) so they launch on Apple Silicon
  without manual `xattr` workarounds — closing the
  [`docs/specs/0009-go-port.md`](docs/specs/0009-go-port.md) §1.3
  signing requirement that had been written but never wired in. The
  signing timestamp is pinned via `SOURCE_DATE_EPOCH` so rebuilds of
  the same tag produce byte-identical tarballs. `install.sh` already
  detected darwin hosts and constructed the matching URL, so no
  installer-side change is needed; the registry path it had been
  requesting since v0.2.0 is now populated. Spec / plan
  [`0014-macos-release-binaries`](docs/specs/0014-macos-release-binaries.md).

  Notarization is out of scope (no Apple Developer account), so first
  launch on a downloaded binary still triggers Gatekeeper's *"inat"
  cannot be opened because Apple cannot check it for malicious
  software* dialog. Clear it once with
  `xattr -d com.apple.quarantine "$(command -v inat)"` (or right-click
  → Open in Finder); subsequent runs are silent. README §macOS walks
  through the install + first-launch flow.

## [0.4.1] - 2026-04-30

Azure DevOps HTTPS support (specs / plans 0012 + 0013) plus the refresh
contract for built-in rules (spec / plan 0011). Two distinct gates were
closed for Azure DevOps — the User-Agent gate at smart-HTTP discovery
and the `multi_ack` capability gate at the upload-pack POST. Manual
end-to-end smoke against an Azure DevOps HTTPS repository
verified the clone path; existing GitLab / GitHub / SSH / `file://`
sources are unaffected. `inat refresh` now also re-delivers the
built-in `inat-usage` (and `inat-kits` when applicable) rules with
hash-guarded modified-file detection identical to the installed-kit
path.

### Fixed

- **Azure DevOps HTTPS sources now work end-to-end.** `inat sources add`
  / `install` / `update` / `refresh` against `dev.azure.com` (and the
  legacy `*.visualstudio.com` form) previously failed with
  `git operation failed: unexpected client error: unexpected requesting
  ".../git-upload-pack" status code: 400`. Two distinct gates had to be
  closed:
  1. **User-Agent gate** — Azure DevOps's HTTP frontend rejects the
     smart-HTTP discovery (`GET /info/refs`) unless the `User-Agent`
     starts with `git/`. go-git's default UA does not match.
  2. **Multi-ack capability gate** — Azure DevOps rejects the
     upload-pack negotiation (`POST /git-upload-pack`) with
     `X-Tfs-Exception: GitProtocolException` /
     `TF401041: ... Clients must support multi-ack.` unless the
     client's first `want` pkt-line advertises the `multi_ack`
     capability. go-git advertises only `side-band-64k no-progress`.

  inat now installs a process-global HTTP transport at startup that
  (a) sets `User-Agent: git/2.40.0` on every outbound request and
  (b) for upload-pack POSTs, rewrites the first `want` pkt-line to
  add `multi_ack` and recomputes its 4-hex-char length prefix. Proxy /
  TLS / connection-pool defaults still flow through
  `http.DefaultTransport`. GitLab, GitHub, generic HTTPS, SSH, and
  `file://` sources are unaffected (the multi_ack rewrite is a no-op
  when it would change nothing).

  An `INAT_DEBUG_HTTP=1` env var dumps method, URL, headers, request
  body, response status, and 4xx response bodies to stderr — useful
  for diagnosing similar HTTP-layer issues against future hosts.
  Spec / plan 0012.

### Added

- **`inat refresh` now re-delivers built-in rules.** `inat-usage` (always)
  and `inat-kits` (when `cfg.KitsRepo`) are reconciled against the
  binary's embedded content on every refresh, with hash-guarded modified-
  file detection identical to the installed-kit path. Built-in deliveries
  are tracked under a new additive `builtinDelivered` field in
  `.inat/config.json` (sidecar map keyed by agent name; older configs
  round-trip without diff thanks to `omitempty`). Spec / plan 0011.
- **Bullet sync independent of file state.** Refresh always calls
  `delivery.AddContextualReference` for each built-in, so a wiped
  `CLAUDE.md` regains its `inat-usage` / `inat-kits` bullets even when
  the rule file body is unchanged or hand-edited. Closes a regression
  gap where `TestRefreshRestoresManagedSection` only restored kit
  bullets.

### Changed

- `inat refresh` no longer early-returns when `cfg.Installed` is empty.
  The new guard is `len(cfg.Installed) == 0 && len(activeAgents) == 0`,
  with message "nothing to refresh" — refresh now correctly handles
  projects that have agents but no installed kits.
- `agents add` / `init --agent` now record built-in deliveries to
  `cfg.BuiltinDelivered` so subsequent refreshes know what was delivered.
  `agents remove` clears the agent's entry from the map.

### Notes

- **One-time migration for v0.3.x upgraders.** The "Notes" section of
  the v0.4.0 entry below claimed the modified-file guard would not trip
  on first refresh; that turned out to be aspirational rather than
  implemented (no recorded hash existed against which to compare). On
  v0.3.x → next, on-disk content differs from the embedded content AND
  no record exists, so refresh emits a modified-file warning and makes
  no change. Run `inat refresh --force` once to re-deliver the built-in
  rule files; subsequent refreshes work without `--force`.
- **v0.4.0 → next is friction-free.** v0.4.0 already shipped the
  frontmattered embedded content, so on-disk content matches embedded
  content on first refresh. The migration path is "silent backfill" —
  refresh records the hash without writing anything, no warning.
- From this release onward, future binary upgrades that change embedded
  built-in content trigger spec 0011 §5 criterion 7's safe-upgrade path:
  on-disk == recorded → silent re-delivery, no `--force` needed.

## [0.4.0] - 2026-04-29

Per-rule "when to read" descriptions in the managed section
(spec / plan 0010). Additive feature — existing kits keep working;
authors are nudged with a stderr warning to add frontmatter.
End-to-end smoke verified all five plan §4 acceptance steps.

### Added

- **Per-rule "when to read" descriptions in the managed section**
  (spec / plan 0010). A rule `.md` may now begin with a YAML
  frontmatter block declaring `name` (must equal the filename-derived
  name) and `description` (a single-line "when to read" hint, ≤200
  chars). When present, the bullet in the inat-managed section
  renders as `- <path> — <description>`; when absent, the bullet
  renders bare (existing kits keep working) and `inat install` /
  `refresh` / `validate` emit a stderr warning naming the rule.
  `inat refresh` and `inat update` rewrite the managed section so
  authors can iterate on hints without reinstalling. Always-priority
  rules also support frontmatter; the renderer strips the YAML block
  before inlining the body. Built-in rules `inat-usage` and
  `inat-kits` ship with frontmatter from this release. Hand-rolled
  flat-key parser in new `go/core/frontmatter` — no new module
  dependencies.

### Changed

- `inat validate --json` output gains a top-level `warnings` array
  alongside `issues`. Exit code is 0 iff `issues` is empty,
  regardless of warning count. Existing consumers reading `issues`
  are unaffected.

### Notes

- **First-refresh diff after upgrade is intentional.** Existing
  projects see their managed section gain hints on every contextual
  rule whose source carries frontmatter, and a stderr warning on
  every rule that doesn't. The on-disk hashes of `inat-usage.md` and
  `inat-kits.md` change because they now ship frontmatter, but the
  prior on-disk content matches the prior recorded hash, so the
  modified-file guard does not trip; `inat refresh` overwrites
  cleanly without `--force`.
- Hard-error shape violations (missing `name`, name mismatch,
  oversize description, multi-line description, malformed YAML,
  duplicate `name`/`description` keys) block install / refresh /
  update / validate. Authors must fix them before consumers can
  install.

## [0.3.0] - 2026-04-28

Full Go port (spec 0009). The released artifact is now the Go binary
built from `go/`; the pre-cutover Rust crate at `rust/` is retained
as an unmaintained archive for historical reference.

### Changed

- **Implementation language: Rust → Go.** CLI library `spf13/cobra`,
  git operations via `go-git/go-git/v5`. CLI surface, config schema,
  managed-section format, and on-disk artefacts are byte-for-byte
  compatible with v0.2.0 — verified by the `parity:rust-vs-go` CI job
  comparing `agents list` / `sources list` / `status` output of both
  binaries against the same fixture.
- **Authentication on HTTPS URLs** now shells out to
  `git credential fill` and hands the result to `go-git` as
  BasicAuth. This mirrors `git2::Cred::credential_helper` semantics
  used by the Rust binary (verified end-to-end on Windows against
  Git for Windows 2.51's `credential.helper=manager`).
- **Release pipeline** cross-compiles all targets from the Linux
  runner via the Go toolchain. `ci/build-release.sh` accepts the same
  Rust-style target triples (`x86_64-unknown-linux-gnu`,
  `x86_64-pc-windows-msvc`) so v0.2 installer scripts keep working
  unchanged. The dependency on a self-hosted Windows runner for
  release builds is gone.
- **Pre-commit hook** swapped from `cargo fmt`/`clippy`/`test` to
  `gofmt -l`, `go vet`, `staticcheck`, `go test ./...` (against
  `go/`). `staticcheck` is the closest analogue to `clippy::pedantic`;
  `scripts/install-hooks.sh` installs it for you when Go is on PATH.
- **Spec-presence regex** in both the local hook and CI updated from
  `^rust/src/` to `^go/`.
- **`ci/verify-release-prereqs.sh`** now reads the repo-root `VERSION`
  file as the single source of truth instead of parsing
  `rust/Cargo.toml`. The Go binary picks up the version at link time
  via `-ldflags "-X main.version=$(cat ../VERSION)"`.
- **Coding standards** (`docs/guidelines/coding-standards.md`)
  rewritten in Go terms: typed errors via `errs.Error` +
  `errors.Is/As`, no goroutines for the CLI flow, `unsafe` package
  forbidden, dependency allowlist limited to `cobra` + `go-git` +
  stdlib.

### Notes

- `rust/` is intentionally **kept**, not deleted, so the historical
  implementation can still be inspected. It is no longer built,
  tested, or released; the pre-commit hook ignores it.
- A verbatim copy of the pre-cutover Rust release pipeline is
  retained for reference.

## [0.2.0] - 2026-04-23

First stable release of the v0.2 line. Promotes `v0.2.0-rc.1` after its
install-path smoke test passed end-to-end on a clean Windows and a
clean POSIX machine (README one-liner → working `inat --version` in
under 60 s, per spec 0002 §13 acceptance criteria).

### Added

- Post-RC CI hardening that landed between `v0.2.0-rc.1` and this tag:
  - README PowerShell install one-liner rewritten for Windows PowerShell
    5.1 (decode-then-`[scriptblock]::Create` pattern) after the generic
    package registry was observed serving `.ps1` as
    `application/octet-stream`, producing a `byte[]` body that `iex`
    stringified to ASCII codes and tripped the parser (plan 9c).
  - Scoped `retry` on `test:windows-x64` and `lint:windows` — retries
    only `runner_system_failure`, `stuck_or_timeout_failure`, and
    `script_failure` narrowed to `exit_codes: [128]` (git-fetch network
    blips). Real regressions still fail on attempt 1 (plan 9d).
  - Top-level `workflow.rules` that stops duplicate pipelines firing on
    branch push + MR open — only `merge_request_event`, default-branch
    commits, and version tags now start pipelines (plan 9e).

### Notes

- macOS binaries are still not produced — no macOS runner available
  (plan 0002 §8). The v0.2.0 line ships Linux x86_64 + Windows x86_64
  archives only; macOS users install from source via
  `cargo install --path .` until a runner lands.
- Installers require `INAT_TOKEN` (PAT or deploy token) while the repo
  remains private — see README "Provisioning an INAT_TOKEN".

## [0.2.0-rc.1] - 2026-04-23

Second release candidate — first RC to ship the full installer surface.
Closes the spec §11 auth-spike question and exercises the end-user
install path (POSIX + Windows) against a real release archive served
from the GitLab generic package registry.

### Added

- POSIX installer: `install.sh` + `uninstall.sh` at repo root, attached
  by the release pipeline to each tag and served from `/latest/install.sh`.
  Honours `INAT_TOKEN` for private-registry fetch, stages archive + sha256,
  extracts to a user-writable install dir, and manages the `PATH` entry in
  a shell-profile managed section.
- Windows installer: `install.ps1` + `uninstall.ps1` with the same
  contract as the POSIX pair. PSScriptAnalyzer runs in a new
  `lint:windows` job (`PSScriptAnalyzerSettings.psd1` suppresses
  `PSAvoidUsingWriteHost` + `PSUseShouldProcessForStateChangingFunctions`).
  Installer unit tests: `tests/installer_tests.sh` + `tests/installer_tests.ps1`
  wired into the lint stage.
- README install + uninstall one-liners for POSIX and PowerShell, a
  "Provisioning an INAT_TOKEN" section documenting the PAT vs
  deploy-token flow, and a troubleshooting pointer.

### Changed

- Plan 0002 §8 records the auth-spike finding: the project is private,
  package-registry assets return 401 anonymously, so installers require
  `INAT_TOKEN` (spec §11 option b).

### Notes

- macOS binaries still not produced (no macOS runner) — deferred per plan
  0002 §8. Linux + Windows only for the v0.2.0 line.
- This RC is the smoke-test for the README install path; `v0.2.0` will
  follow once the one-liner works end-to-end on a clean machine with a
  valid `INAT_TOKEN`.

## [0.2.0-rc.0] - 2026-04-22

Release-candidate smoke tag exercising the new release pipeline
end-to-end (verify → build:linux-x64 + build:windows-x64 → publish).
First opportunity to observe anonymous release-asset fetch behaviour
on the internal GitLab instance (plan 0002 §5 auth spike).

### Added

- GitLab CI release stage: tag-triggered `release:verify`,
  `release:build:linux-x64` (on glibc 2.31 floor via `rust:1.94-bullseye`),
  `release:build:windows-x64` (self-hosted runner), `release:publish`
  (uploads archives + `SHA256SUMS` to the generic package registry and
  creates a GitLab Release with notes extracted from this file).
- Release helper scripts in `ci/`: `build-release.sh`, `build-release.ps1`,
  `verify-release-prereqs.sh`, `extract-changelog-section.sh`.

### Notes

- macOS binaries are not produced — no macOS runner available on the
  internal GitLab instance (plan 0002 §8). Linux + Windows only for v0.2.0.
- Not for end-user consumption — tagged only to smoke-test the pipeline.

## [0.1.0] - 2026-04-21

Initial release. `inat` is a CLI kit manager for AI prompt and rule
kits, distributed via git and delivered per-project through built-in
agent profiles.

### Added

- CLI commands: `init`, `sources add/remove/list`, `agents add/remove/list`,
  `install`, `update`, `remove`, `refresh`, `status`. Kits are referenced
  as `<source>/<kit>`; bare names are accepted when unambiguous.
- Per-project configuration under `.inat/config.json` (sources, installed
  kits with tree SHAs and delivered-file hashes, project-level agents).
  Global configuration under `~/.inat/config.json` (agents). Global cache of
  cloned source repos under `~/.inat/cache/`.
- Built-in `claude-code` agent profile: skills delivered to
  `.claude/skills/<skill-name>/`, `always` rules inlined into `CLAUDE.md`
  inside a managed section, `contextual` rules copied to `.inat/rules/` and
  referenced from `CLAUDE.md`.
- Atomic `install`, `update`, `remove`, `refresh`, and `agents add/remove`
  operations — plan first, execute second, write config last, roll back on
  any failure.
- Modified-file detection via SHA-256 hashing at delivery time; `update`,
  `remove`, and `refresh` warn on user modifications and require `--force`
  to proceed.
- Tree SHA change tracking (no semver for kits in v1) — `status` reports
  whether each installed kit is up to date against its source.
- Name validation: `[a-z0-9]([a-z0-9-]*[a-z0-9])?`, no consecutive hyphens,
  64-character max. Names must be unique across all installed kits
  because delivered files use the skill/rule name only.
- Managed-section markers in edited rule files — the tool never mutates user
  content outside its own markers.

### Development

- 153 tests, `cargo clippy --all-targets --all-features -- -D warnings` clean
  with `clippy::pedantic` enabled at warn level.
- `#![forbid(unsafe_code)]` at crate root.
- Synchronous-only I/O (`std::fs`, `git2`) — no async runtime.
- Dependency allowlist: `anyhow`, `clap`, `git2`, `serde`, `serde_json`,
  `sha2`, `thiserror`, `tempfile` (dev). New crates require justification
  per `docs/guidelines/coding-standards.md`.
- Pre-commit hook runs `cargo fmt`, `cargo clippy -D warnings`, and
  `cargo test`.

[Unreleased]: https://github.com/schubergphilis/inat/compare/v0.9.0...HEAD
[0.9.0]: https://github.com/schubergphilis/inat/releases/tag/v0.9.0
