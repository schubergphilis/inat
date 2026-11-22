#!/bin/sh
# Install rcodesign for ad-hoc signing of darwin Mach-O binaries.
#
# Used by the release workflow (.github/workflows/release.yml). The musl
# build of rcodesign is statically linked, so it runs on any Linux,
# glibc or musl.
#
# Pinned by version + sha256 on purpose — GitHub release assets are
# mutable in principle, and this script is the supply-chain boundary
# between an unrelated GitHub project and our signed Mach-O binaries.
# Bumping the pin requires updating both RCODESIGN_VERSION and
# RCODESIGN_SHA256 below; the SHA256 is published as a sibling .sha256
# asset on each rcodesign release.
#
# Usage:
#     sh ci/install-rcodesign.sh
#
# Behaviour:
#   - Idempotent: if /usr/local/bin/rcodesign already reports the pinned
#     version, exit 0 without re-downloading.
#   - Override install dir with INAT_RCODESIGN_DIR (default /usr/local/bin)
#     for local testing without root.
#
# Exit codes:
#   0  success
#   1  download or extraction failure
#   2  sha256 mismatch (treat as supply-chain failure; do not retry)
#   3  installed binary fails its own --version check

set -eu

RCODESIGN_VERSION="0.29.0"
RCODESIGN_SHA256="dbe85cedd8ee4217b64e9a0e4c2aef92ab8bcaaa41f20bde99781ff02e600002"
RCODESIGN_TRIPLE="x86_64-unknown-linux-musl"

INSTALL_DIR="${INAT_RCODESIGN_DIR:-/usr/local/bin}"
ARCHIVE="apple-codesign-${RCODESIGN_VERSION}-${RCODESIGN_TRIPLE}.tar.gz"
URL="https://github.com/indygreg/apple-platform-rs/releases/download/apple-codesign%2F${RCODESIGN_VERSION}/${ARCHIVE}"

# Idempotence: skip if already at pinned version.
if [ -x "${INSTALL_DIR}/rcodesign" ] \
    && "${INSTALL_DIR}/rcodesign" --version 2>/dev/null \
        | grep -q "rcodesign ${RCODESIGN_VERSION}"; then
    echo "rcodesign ${RCODESIGN_VERSION} already installed at ${INSTALL_DIR}"
    exit 0
fi

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

echo "==> downloading ${ARCHIVE}"
if ! curl --fail --silent --show-error --location \
        --output "${WORK}/${ARCHIVE}" "${URL}"; then
    echo "error: download failed: ${URL}" >&2
    exit 1
fi

echo "==> verifying sha256"
ACTUAL="$(sha256sum "${WORK}/${ARCHIVE}" | awk '{print $1}')"
if [ "${ACTUAL}" != "${RCODESIGN_SHA256}" ]; then
    echo "error: sha256 mismatch for ${ARCHIVE}" >&2
    echo "  expected: ${RCODESIGN_SHA256}" >&2
    echo "  actual:   ${ACTUAL}" >&2
    exit 2
fi

echo "==> extracting to ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}"
tar -xzf "${WORK}/${ARCHIVE}" -C "${WORK}"
mv "${WORK}/apple-codesign-${RCODESIGN_VERSION}-${RCODESIGN_TRIPLE}/rcodesign" \
    "${INSTALL_DIR}/rcodesign"
chmod +x "${INSTALL_DIR}/rcodesign"

if ! "${INSTALL_DIR}/rcodesign" --version >/dev/null 2>&1; then
    echo "error: installed rcodesign fails --version sanity check" >&2
    exit 3
fi

echo "==> rcodesign $("${INSTALL_DIR}/rcodesign" --version) ready"
