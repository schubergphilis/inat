# Plan — Move inat to GitHub as open source

Implements spec [0027](../specs/0027-github-migration.md), which amends specs 0002 and 0015.

## Why

The Schuberg Philis open source approvers approved publishing inat on 2026-11-18. The empty private repo `github.com/schubergphilis/inat` exists. Everything that ties inat to GitLab (self-update, installers, CI, module path) has to move before the first push, and the internal details have to leave the tree.

## Decisions

- **Work on a GitLab branch, then export.** All changes land on `feat/0027-github-migration` in the GitLab repo and go through review there. The GitHub repo is then seeded from that branch's tree as one fresh commit (`git archive`), so no GitLab history or excluded file reaches GitHub.
- **Delete the excluded paths on the branch too** (spec §4). GitLab is archived after the move, and its history keeps them.
- **Keep `ci/build-release.sh` / `.ps1`.** Only their tag source (`CI_COMMIT_TAG` → explicit argument, falling back to `GITHUB_REF_NAME`) and the Homepage URL change. The Actions workflow calls them as GitLab CI did.
- **Release with the `gh` CLI** preinstalled on GitHub runners (`gh release create`), not a third-party action. No new action dependency to pin and update.
- **Self-update keeps two endpoints.** An API base for the release list and a download base for assets. Tests point both at one `httptest` server.
- **Version 0.9.0.** The distribution change is user-visible: existing installs must re-install.

## Changes

Each step is its own commit on the branch and passes the pre-commit hook.

1. **Self-update → GitHub, no token** (TDD).
   - `go/cli/self/self_test.go` first: GitHub API path `/repos/schubergphilis/inat/releases`, `prerelease` handling, `/releases/download/v{ver}/…` asset paths, and no auth header sent.
   - Then `go/cli/self/release.go` (endpoints, `LatestVersion` skips `prerelease: true`, drop `Auth`/`LoadAuth`), `go/cli/self/self.go` (call sites), and `go/errs/errs.go`, `new.go` + `errs_test.go` (remove `NoToken` and `BadTokenType`, which only `LoadAuth` raised).
   - Proof: `go test ./cli/self/ ./errs/`; then `inat self update --version 0.8.0` against the rc release in step 7.
2. **Installers.**
   - `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1` (repo root): GitHub release URLs, drop the token code and the GitLab host and project ID. The installers skip releases flagged `prerelease` as well as `-` tags, like self-update.
   - Move the installer tests out of the archive, `rust/tests/installer_tests.{sh,ps1}` → `tests/` (where `ci/windows-lint.ps1` already looked), and change them first: anonymous requests, GitHub URLs, GitHub-shaped release JSON.
   - Proof: `tests/installer_tests.sh` under sh, dash and bash; `tests/installer_tests.ps1` under pwsh; PSScriptAnalyzer on both `.ps1` installers; a real install in step 7.
3. **Module path.**
   - `go mod edit -module github.com/schubergphilis/inat`, rewrite the imports, `gofmt -w`.
   - Proof: `go build ./... && go vet ./... && staticcheck ./... && go test ./...`.
4. **Built-in rules.**
   - Change the GitLab URL to `https://github.com/schubergphilis/inat` in `go/agents/builtin/inat_usage.md` and `inat_kits.md`. The tests hash the embedded content at run time, so no expected hash changes; add a test in `rules_test.go` that both rules carry the GitHub link and no GitLab one.
   - Proof: `go test ./agents/...`; `inat refresh` in a scratch project shows the new link.
5. **CI.**
   - `.github/workflows/ci.yml`: lint and test matrix on ubuntu, windows and macos, plus the cross-compile job. It also takes over from `ci/windows-lint.ps1`: `tests/installer_tests.sh` on ubuntu and macos, and PSScriptAnalyzer plus `tests/installer_tests.ps1` on windows.
   - `.github/workflows/release.yml`: on `v*` tags, `verify-release-prereqs.sh`, four `build-release.sh` builds, `SHA256SUMS`, `gh release create` (pre-release when the tag has `-`).
   - `.github/dependabot.yml` for gomod and github-actions.
   - Adapt `ci/*.sh`/`.ps1` as decided above; `verify-release-prereqs.sh` takes the same `GITHUB_REF_NAME` fallback. Remove the GitLab CI config and `ci/windows-lint.ps1` (spec §4).
   - Actions are pinned to commit SHAs with the version in a comment; Dependabot bumps both. The release is one ubuntu job (the four cross-builds take seconds), so no artifact hand-off actions. The PowerShell installer tests run under both Windows PowerShell 5.1 and pwsh, since the one-liner may run under either.
   - Proof: `actionlint`; the release job's steps run locally on a copy of the tree (verify, rcodesign, four archives, `sha256sum -c`, changelog notes); `tests/installer_tests.sh` under sh and bash. The real run comes in step 7.
6. **Public-tree cleanup.**
   - Delete the spec §4 paths and scrub the kept files.
   - Rewrite the README install, auth and build-from-source sections for GitHub. Add a "moved from GitLab, re-install once" note.
   - CHANGELOG v0.9.0 entry. `VERSION` → `0.9.0`.
   - CLAUDE.md: repo layout without `rust/`, GitHub CI, new module path.
   - CONTRIBUTING: PRs on GitHub.
   - Beyond §4: the stale delivered `.inat/rules/inat-usage.md` is refreshed to the embedded text; NOTICE drops its `rust/` section; Go comments that cited `rust/src/…` name "the Rust implementation" instead; GitLab issue numbers in CHANGELOG (`#1`–`#4`) are dropped, since GitHub would link them to unrelated issues; the copied third-party `.claude/skills/grill-me` is removed. Spec 0027 itself no longer spells out the internal strings; the exact grep pattern stays in the internal record.
   - Proof: the spec §6.1 grep returns only the allowed hits; tree review by the user before step 7.
7. **Seed GitHub privately and prove the release.**
   - A pre-release tag `vX.Y.Z-<pre>` is accepted when `VERSION` is `X.Y.Z`: `verify-release-prereqs.sh` and `build-release.{sh,ps1}` compare the tag's base to `VERSION`, the CHANGELOG check and release notes use `VERSION`, and the binary reports the full tag version. So `v0.9.0-rc.1` releases the exact tree that becomes `v0.9.0`, with no second commit before going public. A tag whose base differs from `VERSION` still fails.
   - `git archive feat/0027-github-migration`, unpacked into a new directory, `git init`, one commit, push to `github.com/schubergphilis/inat` `main`.
   - The user does the push from WSL; the box holds no GitHub credentials.
   - CI must go green on all three OSes. Tag `v0.9.0-rc.1` and check the pre-release assets, then `sha256sum -c`.
   - The repo is still private, so the anonymous checks wait for step 8. In step 7, the assets are checked by the user with `gh release download`.
   - Proof: spec §6.3 and §6.6.
8. **Go public.**
   - The user flips the repo to public. Delete the rc release and tag, then tag `v0.9.0`.
   - Run the Linux one-liner install in a fresh box and the Windows one on the host, with no token, followed by a `self update` round-trip.
   - Add inat to schubergphilis.github.io. Archive the GitLab project after a final README commit pointing at GitHub.
   - Proof: spec §6.4, §6.5, §6.7 and §6.8.
9. **Merge the GitLab MR** before archiving, so the internal history ends at the exported tree.

## Risks

- **Anonymous rate limit (60/hour per IP)** behind a shared NAT could fail `self update` for a busy office. If it bites, honour an optional `GITHUB_TOKEN`; that is a follow-up, not this plan.
- **Scrub misses a string.** The step 6 grep is the gate, and the single-commit export means a miss is fixed by amending before the repo goes public.
