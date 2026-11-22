# Contributing to inat

Thanks for your interest in inat. This guide covers how to set up, what the
project expects of a change, and how contributions are licensed.

inat is published by [Schuberg Philis](https://www.schubergphilis.com/) under
the Apache License 2.0. Contributions are welcome from anyone.

## Scope

- **`go/` is the product.** It is the binary that ships. All code changes go here.
- **Installers** (`install.sh`, `install.ps1`, `uninstall.*`) live at the repo root,
  with their tests in `tests/`.

## Getting started

1. Install [Go 1.22+](https://go.dev/dl/).
2. Clone the repo and install the git hooks:

   ```bash
   bash scripts/install-hooks.sh
   ```

   This points `core.hooksPath` at the committed `.githooks/` directory, so every
   `git commit` runs the same gates CI does. It also installs `staticcheck` for you
   (via `go install`) when Go is on `PATH`. The hook is a native git hook — it fires
   regardless of how the commit was invoked.

3. Build and test:

   ```bash
   cd go
   go build ./...
   go test ./...
   ```

## Quality gates

Every change must pass all four gates locally and in CI — the pre-commit hook and
the pipeline both enforce them:

- `gofmt -l .` returns empty (code is formatted)
- `go vet ./...`
- `staticcheck ./...`
- `go test ./...`

inat follows TDD: write tests before implementation, and don't submit untested code.
See [`docs/guidelines/coding-standards.md`](docs/guidelines/coding-standards.md) for
the full rules (file/function size, the typed-error model, the dependency allowlist,
and the no-`unsafe` / no-goroutines constraints).

## Spec-first / plan-first workflow

Before changing code under `go/`, add or update a design doc:

- A **spec** in [`docs/specs/`](docs/specs/) — the contract (goals, CLI surface,
  semantics, error model, non-goals, acceptance criteria).
- For non-trivial features, a companion **plan** in [`docs/plan/`](docs/plan/) —
  the rollout, paired by the same `NNNN` number.

The pre-commit hook blocks any commit that touches `go/` without also touching
`docs/specs/` or `docs/plan/`. For purely mechanical commits (formatting, `go.sum`
bumps, renames with no behaviour change) override it with
`INAT_SKIP_SPEC_CHECK=1 git commit ...`.

Pick the next `NNNN` from the highest existing number across **both** `docs/specs/`
and `docs/plan/`, and reserve it for both files.

## Submitting a change

1. Fork the repo and create a branch from `main`.
2. Make the change with its spec/plan and tests; keep commits focused.
3. Ensure all four gates pass. CI (GitHub Actions) runs them on Linux, Windows and
   macOS, plus the installer tests.
4. Open a pull request on [GitHub](https://github.com/schubergphilis/inat) describing the change and linking the spec/plan and any
   related issue.

Keep pull requests small and single-purpose — they review and land faster.

## Licensing and provenance

- Contributions are accepted under the **Apache License 2.0** (see [`LICENSE`](LICENSE)).
- Every new `.go` file under `go/` must carry the SPDX header, above the `package`
  declaration, separated by a blank line:

  ```go
  // SPDX-License-Identifier: Apache-2.0
  // Copyright 2026 Schuberg Philis
  ```

- New third-party dependencies require justification (the allowlist is `spf13/cobra`,
  `go-git/go-git/v5`, and the standard library) and must carry a permissive license
  (Apache-2.0, BSD, MIT, ISC). No GPL/LGPL/AGPL or other copyleft dependencies.
- Sign off your commits to certify the [Developer Certificate of Origin](https://developercertificate.org/):

  ```bash
  git commit -s
  ```

  By signing off you confirm the contribution is your own work (or you have the right
  to submit it) and may be distributed under the project's license.

## Reporting bugs and requesting features

Open an issue with enough detail to reproduce: what you ran, what you expected, what
happened, and your OS and inat version (`inat --version`). For feature ideas, describe
the problem first, not just the proposed solution.

## Security

Please do **not** open public issues for security vulnerabilities. Report them
privately to the maintainers at [security@schubergphilis.com](mailto:security@schubergphilis.com).

## Code of conduct

Be respectful and constructive in issues, reviews, and discussion. Engage with
feedback in a respectful manner. Contributions and conduct should reflect the quality
and values people expect of a Schuberg Philis project.
