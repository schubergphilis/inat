# Plan — User scope (`inat --user`)

Implements spec [`0025-user-scope.md`](../specs/0025-user-scope.md).

## Decisions

- **The user scope is a project rooted at home.** `ConfigPath(home)` already is `~/.inat/config.json`, and the spec 0006 `GlobalConfig` (`agents` only) is a subset of `Config`. So the user scope reuses every project code path with `dir = home`. Only three things differ: the profile mapping, the reference text, and loading a missing config as empty.
- **Scope rides on the config, not on new parameters.** `Config` gains `Scope scope.Scope` (`json:"-"`). `config.LoadConfig(dir)` sets `ScopeUser` when `dir` is the home directory (`os.SameFile`) and routes to `LoadUserConfig`. Profile lookups become `agents.ProfileFor(name, cfg.Scope)`. This avoids threading a new argument through roughly 15 public functions and their tests. It is sound because spec §6.4 makes "root == home" equivalent to "user scope": the CLI never hands a project command `dir == home`.
- **`Scope` lives in a leaf package**, `go/core/scope`, so `agents` does not import `config`.
- **File path vs reference text.** `RuleContextualMapping` gains `RefPrefix` (`""` for project, `"~/"` for user) and two helpers: `RuleFile(name)` (scope-relative path, recorded in config, used as the `skip` key) and `RefText(name)` (`RefPrefix + RuleFile(name)`, written into and removed from the managed section). Every site that builds `Contextual.Path + name + ".md"` switches to the helpers.
- **`--global` writes go through the full `Config`.** `AgentsAddGlobal` and `AgentsRemoveGlobal` load and save via `LoadUserConfig` and `SaveConfig`. Otherwise the new binary would itself drop user-scope `sources` and `installed` on every `--global` write. `GlobalConfig` stays read-only, for `CollectActiveAgents`.
- **Collision warnings run in the CLI layer.** `RunE` loads this scope's config before and after the command, diffs the names, and checks the new names against the other scope's config. A small helper keeps `install` and `update` untouched underneath.
- **Split before growing.** `errs/errs.go` (396 lines) and `core/config/config.go` (301 lines) are split as pure moves before anything is added to them.

## Changes, in order

Each step is one commit. Every step runs the pre-commit hook (`gofmt`, `vet`, `staticcheck`, `go test ./...`). New tests are written before the code (TDD).

1. **Spec + plan.** Commit `docs/specs/0025-user-scope.md` and this file.
   *Proof:* the hook's spec-presence check passes.
2. **Pure moves.** `errs/errs.go` → constructors move to `errs/new.go`. `core/config/config.go` → `GlobalConfig`, `Load/SaveGlobalConfig*`, `CollectActiveAgents`, `HomeDir` and `globalConfigPath` move to `core/config/user.go`.
   *Proof:* `go test ./...` is green with no test changes, and `git diff --stat` shows only moves.
3. **`HomeDir()` → `os.UserHomeDir()`** (spec §6.5). Update the doc comments in `config` and `git/cache/cache.go`.
   *Proof:* new `TestHomeDirFollowsHOME` (non-Windows: `t.Setenv("HOME", tmp)` → `tmp`) passes, and the existing suite is green.
4. **Scope + profile user variant.** New `core/scope` (`Project` = zero value, `User`). In `agents`: `UserMapping` on `AgentProfile`, `RefPrefix` plus the `RuleFile`/`RefText` helpers, and `ProfileFor(name, scope)` returning the profile with the scope's mapping. `GetProfile` stays as the project-scope alias. Switch the reference-building sites to the helpers: `cli/install/deliver.go`, `agents/builtin/rules.go`, `agents/builtin/refresh.go`, `cli/remove/remove.go`.
   *Proof:* a table test pins the claude-code project and user mappings against spec §5. A test pins project `RefText == RuleFile`. Existing delivery tests are byte-identical (project output unchanged).
5. **User config load/save.** New `errs` kinds `HomeAsProject` and `ConfigInvalid`. Add `config.LoadUserConfig(home)` (missing → empty with `ScopeUser`; `kits_repo: true` → `ConfigInvalid`), `config.IsUserRoot(dir)`, and `LoadConfig` routing. `cli/agents` `--global` add/remove move to the full config.
   *Proof:* tests for a missing file, a spec-0006 `agents`-only file, a `kits_repo` rejection, and a `--global` add that preserves `sources` and `installed` (acceptance 1, second half).
6. **Thread `cfg.Scope` into profile lookups.** All `agents.GetProfile` callers that hold a `cfg` switch to `ProfileFor(name, cfg.Scope)`: install, remove, refresh, rename, `transaction/apply*.go`, and `cli/agents`. `builtin.DeliverInatUsage/RemoveInatUsage` take a `scope.Scope`. `AgentsList` prints a single `user agents:` block when `cfg.Scope == User`.
   *Proof:* new function-level tests in `cli/install`, `cli/remove`, `cli/agents`, `cli/refresh` and `cli/update`, with `HOME = dir = tmp`, cover acceptance 2–6: file locations, the `~/` reference, nothing written outside `$HOME`, user text around the markers byte-identical, `remove` exact, and drift skip plus `--force`.
7. **CLI surface.** New `cli/scope` package: `ResolveDir(cmd)` (`--user` → `HomeDir()`; otherwise `Getwd()`, refusing `HomeAsProject` when cwd is home) and `RejectUser(cmd)`. Add the persistent `--user` flag on root in `main.go`. Replace every `os.Getwd()` in the `RunE`s with `ResolveDir`. `init`, `validate` and `self` call `RejectUser`.
   *Proof:* unit tests for `ResolveDir` and `RejectUser` (acceptance 8, 9). `main.go` stays under 300 lines.
8. **Collision warnings** (spec §6.3). `cli/scope/collision.go`: `WarnNewCollisions(before, after *config.Config, otherDir string)`. It is best effort: an unreadable other config means silence and never an error. Wire it into the `install` and `update` `RunE`s (skipped under `--plan`).
   *Proof:* tests for a warning line per colliding name in both directions, no output when the other config is missing or corrupt, and exit status unchanged (acceptance 7).
9. **Docs.** Spec 0001 §10 rule 11 and the §14 bullet (spec §9). README usage section. Project `CLAUDE.md` Key Concepts ("Source … project-only") and the CLI list. A one-line `--user` mention in the built-in `inat-usage` rule. A `CHANGELOG.md` Unreleased entry, including the older-binary `--global` caveat from spec §6.5.
   *Proof:* reading the docs back. `go test` covers the built-in rule content.

## Verification (end to end)

1. **Scripted run with a real binary.** `go build` produces `inat`. A scratch script makes `HOME` a temporary directory, builds a local fixture source repo (one kit with one skill, one always rule and one contextual rule), and runs, from an unrelated cwd: `--user sources add`, `--user agents add claude-code`, `--user install`, `--user status`, edit plus `--user refresh` (skip) and `--force`, `--user update`, `--user remove`. After each command it asserts the files and config from acceptance 1–6. Then: a project install of the same kit (collision warning, acceptance 7), `init` and `status` in `$HOME` (acceptance 8), and `--user init` (acceptance 9). The script lives in the session scratchpad and is not committed.
2. **Hook checks** clean on every commit (acceptance 10, 11).
3. **Acceptance 12, Windows host, manual.** This needs you, so I'll give you the steps then: a released or cross-compiled `inat.exe`, `inat --user` install of a test kit, then a fresh Claude Code session in an unrelated directory showing the inlined rule and the `~/.inat/rules/…` reference.

## Out of this plan

- **Release (version bump and tag).** That is a separate `chore(release)` step, as for v0.7.2.
- **claude-docker mounts for `~/.inat/rules` and user skills** (spec §8). Once this plan is approved, I'll ask the host about them via agent-bus. That item is tracked in `.claude/OPEN.md`.
