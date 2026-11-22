## inat — Move to GitHub as open source (Spec 0027)

---

### 1. Overview

inat moves from the company-private GitLab project to the public `github.com/schubergphilis/inat`, under the Apache License 2.0 already in the repo. The Schuberg Philis Open Source Approvers Group approved the release on 2026-11-18, on an internal peer review.

GitHub becomes the only home: source, CI, releases, installers and self-update. The GitLab project is archived with a pointer to GitHub and keeps the full pre-move history for internal reference.

This spec amends spec [0002](0002-ci-cd-and-install.md) (CI/CD and installers) and spec [0015](0015-self-update.md) (self-update): where they name GitLab endpoints, the package registry, or `INAT_TOKEN`, this spec replaces them. The CLI surface, `.inat/` schema and delivery behaviour are unchanged.

A companion implementation plan lives at [`docs/plan/0027-github-migration.md`](../plan/0027-github-migration.md).

---

### 2. Goals

1. **Fresh public history.** The GitHub repo starts with a single commit built from the cleaned tree (§4). The GitLab history is not pushed.
2. **Module path.** `go/go.mod` declares `module github.com/schubergphilis/inat`; every import follows.
3. **No token.** The repo is public, so self-update and the installers fetch anonymously. `INAT_TOKEN`, `INAT_TOKEN_TYPE`, the `PRIVATE-TOKEN` / `Deploy-Token` headers and the `NoToken` error are removed.
4. **Self-update from GitHub Releases.**
   - Latest version: `GET https://api.github.com/repos/schubergphilis/inat/releases?per_page=50`, first entry whose `prerelease` is false and whose `tag_name` contains no `-`, with the leading `v` stripped.
   - Archive: `https://github.com/schubergphilis/inat/releases/download/v{ver}/inat-{ver}-{triple}{.tar.gz|.zip}`.
   - Checksums: `https://github.com/schubergphilis/inat/releases/download/v{ver}/SHA256SUMS`, same coreutils format and verification as spec 0015.
   - Asset names, target triples, the swap procedure and `--version` are unchanged.
5. **Installers from release assets.** `install.sh`, `install.ps1`, `uninstall.sh`, `uninstall.ps1` are attached to every release. The README one-liners use `https://github.com/schubergphilis/inat/releases/latest/download/<script>`; the scripts resolve versions and download archives from the same URLs as goal 4.
6. **GitHub Actions CI** replaces the GitLab CI pipeline:
   - On every push and pull request: `gofmt -l`, `go vet`, `staticcheck`, `go test ./...` on Linux, Windows and macOS, plus cross-compiling all four release targets (linux x64, windows x64, darwin x64, darwin arm64).
   - On a `v*` tag: build the four archives with `ci/build-release.sh` (macOS archives ad-hoc signed with rcodesign as today), write `SHA256SUMS`, and publish a GitHub Release with the archives, `SHA256SUMS`, the four installer scripts, and notes from `ci/extract-changelog-section.sh`. Tags with `-` publish as pre-releases.
7. **Dependabot** for `gomod` (in `go/`) and `github-actions`, per the SBP "Open sourcing your project" checklist.
8. **No internal details in the public tree** (§4).
9. **First GitHub release is v0.9.0**, the first release whose binaries update from GitHub.

---

### 3. Non-goals

- **Bridge release.** No final GitLab release that updates from GitHub. Existing installs re-run the GitHub installer once; the README and the v0.9.0 CHANGELOG entry say so.
- **Release signing beyond SHA256SUMS.** Checksums and archives still share a trust root (GitHub). Signing (cosign, GitHub attestations) is a later spec.
- **GitHub Pages documentation site.** The docs stay in-repo Markdown for now.
- **Publishing the Rust archive.** `rust/` stays on GitLab only.
- **Rewriting the old history.** GitLab keeps it unchanged.
- **A GitHub API token for rate limits.** Anonymous calls (60/hour per IP) are enough for a CLI that checks once per `self update`.

---

### 4. Public tree

**Left out of the GitHub repo** (internal, or outside the peer review's scope):

| Path | Why |
|---|---|
| `rust/`, `ci/parity-rust-vs-go.sh` | Unmaintained archive, excluded from the peer review |
| The GitLab CI config and its Rust-era backup, `ci/windows-lint.ps1` | GitLab-only; the Windows script targets a retired self-hosted runner |
| `.inat/config.json` and the skills it installed under `.claude/skills/` | Point at and copy from a private internal kits source |
| The OSS peer-review report under `docs/oss/` | Internal approval record, names the reviewer; kept with the internal approval |
| `go/spike/RESULTS.md` | GitLab pipeline links from the go-git spike |

**Kept, with internal details removed:** README, CONTRIBUTING, CHANGELOG, CLAUDE.md, `docs/` specs, plans and guides, built-in rules, Go comments. Removed: internal hostnames and the GitLab project ID, the S3 host, runner names and IDs, internal Azure DevOps organisation and project names, merge-request, issue and pipeline numbers, internal ticket keys, and personal names and paths. Where removal leaves a sentence meaningless, it is rewritten to say the same thing generically. Historical specs and plans keep describing what was true at the time ("released on the internal GitLab"), without addresses.

The untracked policy PDF in `docs/` is never committed.

---

### 5. Built-in rule change

`go/agents/builtin/inat_usage.md` and `inat_kits.md` link to the GitLab project. They change to the GitHub URL. Existing projects see the new text on the next `inat refresh` like any other built-in rule change; no migration is needed.

---

### 6. Acceptance

1. A grep over the public tree for the internal hostnames, IDs and names listed in §4 finds nothing. The exact pattern is kept with the internal migration record, since publishing it would publish the strings. The company's public website and `security@` address in CONTRIBUTING and NOTICE are allowed.
2. `go build ./...`, `go vet ./...`, `staticcheck ./...`, `go test ./...` pass with the new module path.
3. A `v0.9.0-rc.1` tag on GitHub produces a pre-release with four archives, `SHA256SUMS` and four installers; `sha256sum -c SHA256SUMS` passes on the downloaded set.
4. On a clean Linux machine without `INAT_TOKEN`, the `install.sh` one-liner installs the latest stable release, and `inat self update --version <older>` then `inat self update` round-trips. Same on Windows with `install.ps1`.
5. `inat self update` from v0.9.0 finds a later GitHub release without any token set.
6. GitHub Actions runs green on Linux, Windows and macOS for the first commit.
7. The GitHub repo has exactly one commit before it is made public.
8. The GitLab project is archived and its README points at GitHub.
