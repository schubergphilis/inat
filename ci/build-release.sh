#!/bin/sh
# Builds an inat release archive for a single Rust-style target triple.
#
# Cross-compiles via the Go toolchain (no host-platform toolchain needed):
# release archives can be produced on a Linux runner regardless of target
# OS/arch.
#
# Usage:
#     ci/build-release.sh <target-triple> [tag]
#
# If <tag> is omitted, falls back to $GITHUB_REF_NAME (the tag on a
# GitHub Actions tag push).
#
# Output:
#   linux/macOS targets — inat-<version>-<target-triple>.tar.gz
#   windows targets     — inat-<version>-<target-triple>.zip
#
# The archive contains a single directory `inat-<version>-<target-triple>/`
# holding `inat` (or `inat.exe`), `LICENSE`, and a short `README`. This
# layout matches what `install.sh` and `install.ps1` expect.
#
# Target triples are kept in the Rust nomenclature (`x86_64-unknown-linux-gnu`
# etc.) for backwards compatibility with v0.2.0 installer scripts.
#
# Exit codes:
#   0 — archive built
#   2 — bad arguments
#   3 — go build failed
#   4 — expected binary not found after build
#   5 — unsupported target triple
#   6 — rcodesign missing (only when target is *-apple-darwin)

set -eu

TARGET="${1:-}"
if [ -z "$TARGET" ]; then
    echo "error: target-triple required (e.g. x86_64-unknown-linux-gnu)" >&2
    exit 2
fi

TAG="${2:-${GITHUB_REF_NAME:-}}"
if [ -z "$TAG" ]; then
    echo "error: no tag (pass as arg 2 or set GITHUB_REF_NAME)" >&2
    exit 2
fi

VERSION="${TAG#v}"
STAGE="inat-${VERSION}-${TARGET}"

# Map Rust target triples to GOOS/GOARCH + archive shape.
case "$TARGET" in
    x86_64-unknown-linux-gnu)
        GOOS=linux;   GOARCH=amd64; BIN_NAME=inat;    ARCHIVE="${STAGE}.tar.gz" ;;
    aarch64-unknown-linux-gnu)
        GOOS=linux;   GOARCH=arm64; BIN_NAME=inat;    ARCHIVE="${STAGE}.tar.gz" ;;
    x86_64-pc-windows-msvc|x86_64-pc-windows-gnu)
        GOOS=windows; GOARCH=amd64; BIN_NAME=inat.exe; ARCHIVE="${STAGE}.zip" ;;
    x86_64-apple-darwin)
        GOOS=darwin;  GOARCH=amd64; BIN_NAME=inat;    ARCHIVE="${STAGE}.tar.gz" ;;
    aarch64-apple-darwin)
        GOOS=darwin;  GOARCH=arm64; BIN_NAME=inat;    ARCHIVE="${STAGE}.tar.gz" ;;
    *)
        echo "error: unsupported target triple '$TARGET'" >&2
        echo "supported: x86_64-unknown-linux-gnu, aarch64-unknown-linux-gnu, x86_64-pc-windows-msvc, x86_64-apple-darwin, aarch64-apple-darwin" >&2
        exit 5 ;;
esac

if [ ! -f VERSION ]; then
    echo "error: VERSION file not found at repo root" >&2
    exit 2
fi
# A pre-release tag (vX.Y.Z-rc.1) builds the X.Y.Z tree; the binary
# reports the full tag version.
if [ "$(tr -d '[:space:]' < VERSION)" != "${VERSION%%-*}" ]; then
    echo "error: tag version ($VERSION) != VERSION file ($(cat VERSION))" >&2
    exit 2
fi

echo "building inat ${VERSION} for ${TARGET} (GOOS=${GOOS} GOARCH=${GOARCH})"

# CGO disabled keeps the binary statically linked so it runs on any
# Linux distro without glibc-version concerns. Strip via -ldflags so
# we don't need a separate `strip` step (which may not be available
# for cross-compiled non-host binaries).
BIN_PATH="$(pwd)/build-tmp/${BIN_NAME}"
mkdir -p "$(dirname "$BIN_PATH")"

if ! (
    cd go && \
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
        go build \
            -trimpath \
            -ldflags "-s -w -X main.version=${VERSION}" \
            -o "$BIN_PATH" \
            .
); then
    echo "error: go build failed" >&2
    exit 3
fi

if [ ! -f "$BIN_PATH" ]; then
    echo "error: binary not found at $BIN_PATH" >&2
    exit 4
fi

# darwin Mach-O binaries need an ad-hoc signature to satisfy Apple
# Silicon's kernel signature check. Spec 0009 §1.3 mandates this for
# darwin/arm64; we sign darwin/amd64 too for parity (cheap, avoids
# divergent install instructions). The signing timestamp is pinned to
# the commit time via SOURCE_DATE_EPOCH so the resulting tarball
# hashes are reproducible across rebuilds of the same tag.
case "$TARGET" in
    *-apple-darwin)
        if ! command -v rcodesign >/dev/null 2>&1; then
            echo "error: rcodesign not on PATH; run ci/install-rcodesign.sh first" >&2
            exit 6
        fi
        if SDE="$(git log -1 --format=%ct 2>/dev/null)" && [ -n "$SDE" ]; then
            export SOURCE_DATE_EPOCH="$SDE"
        fi
        echo "==> ad-hoc signing $BIN_PATH"
        rcodesign sign "$BIN_PATH"
        # No `rcodesign verify` here: the verify subcommand has a known
        # CMS-parsing bug in rcodesign 0.29.0 and prints a self-deprecating
        # warning telling callers to use Apple's tooling instead. It fired
        # in CI during the v0.5.0 release with `CMS error: missing further
        # values (at position 0)` against a successfully-signed Mach-O.
        # The sign step's exit code is the real gate; spec 0014 §4.3.
        ;;
esac

rm -rf "${STAGE}"
mkdir -p "${STAGE}"
cp "${BIN_PATH}" "${STAGE}/${BIN_NAME}"
cp LICENSE "${STAGE}/LICENSE"

cat > "${STAGE}/README" <<EOF
inat ${VERSION}

A CLI kit manager for AI prompt and rule kits. Share skills and rules
across projects via git.

Homepage:      https://github.com/schubergphilis/inat
Documentation: see README.md in the repository
Licence:       Apache-2.0 (bundled as LICENSE alongside this file)
EOF

case "$ARCHIVE" in
    *.tar.gz)
        tar -czf "${ARCHIVE}" "${STAGE}" ;;
    *.zip)
        # zip is preinstalled on the GitHub ubuntu runner; apt-get install zip
        # elsewhere.
        zip -qr "${ARCHIVE}" "${STAGE}" ;;
esac

rm -rf "${STAGE}" build-tmp

echo "built ${ARCHIVE}"
ls -la "${ARCHIVE}"
