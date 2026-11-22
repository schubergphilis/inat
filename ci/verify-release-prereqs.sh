#!/bin/sh
# Verifies release preconditions before any build job runs.
#
#   1. The tag ($1, or GITHUB_REF_NAME) matches the version in repo-root VERSION.
#      A pre-release tag matches on its base: v0.9.0-rc.1 for VERSION 0.9.0.
#   2. CHANGELOG.md has a `## [X.Y.Z]` section for the VERSION file's version.
#
# The Go-port cutover (spec 0009 M3b) replaced rust/Cargo.toml with the
# repo-root VERSION file as the single source of truth for the release
# version. The build/release pipeline reads VERSION via
# `go build -ldflags "-X main.version=$(cat VERSION)"`.
#
# Exits non-zero with a clear message on mismatch. Callable locally with
# a tag argument:
#
#     ci/verify-release-prereqs.sh v0.3.0
#
# Exit codes:
#   0 — OK
#   2 — no tag provided
#   3 — could not read VERSION file
#   4 — tag / VERSION mismatch
#   5 — CHANGELOG.md section missing

set -eu

TAG="${1:-${GITHUB_REF_NAME:-}}"
if [ -z "$TAG" ]; then
    echo "error: no tag provided (pass as arg or set GITHUB_REF_NAME)" >&2
    exit 2
fi

VERSION="${TAG#v}"

if [ ! -f VERSION ]; then
    echo "error: VERSION file not found at repo root" >&2
    exit 3
fi

FILE_VERSION=$(tr -d '[:space:]' < VERSION)
if [ -z "$FILE_VERSION" ]; then
    echo "error: VERSION file is empty" >&2
    exit 3
fi

# A pre-release tag (vX.Y.Z-rc.1) releases the X.Y.Z tree as-is.
if [ "$FILE_VERSION" != "${VERSION%%-*}" ]; then
    echo "error: tag version ($VERSION) != VERSION file ($FILE_VERSION)" >&2
    exit 4
fi

if ! grep -qE "^## \[${FILE_VERSION}\]" CHANGELOG.md; then
    echo "error: CHANGELOG.md has no '## [$FILE_VERSION]' section" >&2
    exit 5
fi

echo "release prereqs OK: tag $TAG matches VERSION ($FILE_VERSION) and CHANGELOG.md"
