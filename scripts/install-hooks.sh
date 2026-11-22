#!/usr/bin/env bash
# One-time installer for the repo-local git hooks.
#
# Points git's core.hooksPath at the committed .githooks/ directory so
# .githooks/pre-commit (and any future hooks) fire on every commit.
#
# This is the single reliable cross-instance enforcement layer: unlike
# CLAUDE.md or .claude/settings.json hooks — which only fire when
# Claude Code discovers the project from its launch CWD — the native
# git hook runs regardless of who or what invoked `git commit`.
#
# Also installs `staticcheck` (the clippy-equivalent linter the hook
# requires), idempotently, when `go` is on PATH. If `go` is not
# available the script warns but does not fail; the hook itself blocks
# commits when staticcheck is absent.
#
# Idempotent: safe to re-run.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

git config core.hooksPath .githooks
chmod +x .githooks/pre-commit 2>/dev/null || true

if command -v staticcheck >/dev/null 2>&1; then
    echo "inat: staticcheck already installed ($(command -v staticcheck))"
elif command -v go >/dev/null 2>&1; then
    echo "inat: installing staticcheck via 'go install'…"
    go install honnef.co/go/tools/cmd/staticcheck@v0.8.2
    echo "inat: staticcheck installed (ensure \$(go env GOPATH)/bin is on your PATH)"
else
    echo "inat: warning — neither staticcheck nor go found on PATH;" >&2
    echo "      install Go (https://go.dev/dl/) and re-run this script," >&2
    echo "      or the pre-commit hook will block commits." >&2
fi

echo "inat: git hooks installed (core.hooksPath = .githooks)"
echo "inat: to bypass the spec-presence check on a single commit, use"
echo "      INAT_SKIP_SPEC_CHECK=1 git commit ..."
