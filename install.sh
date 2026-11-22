#!/bin/sh
# inat installer for POSIX (macOS, Linux).
#
# Canonical usage:
#     curl -fsSL https://github.com/schubergphilis/inat/releases/latest/download/install.sh | sh
#
# Flags go through `sh -s --`, e.g. to pin a version:
#     curl -fsSL https://github.com/schubergphilis/inat/releases/latest/download/install.sh \
#       | sh -s -- --version v0.9.0
#
# Implements the POSIX half of docs/specs/0002-ci-cd-and-install.md §9,
# as amended by spec 0027 (GitHub Releases, anonymous downloads).
# Shell: POSIX sh (dash-compatible). No bashisms.
#
# Environment:
#   INAT_INSTALL_DIR  Override install directory. Default
#                     "$HOME/.local/bin".
#
# Flags:
#   --version <v>       Install a specific release (e.g. v0.2.0).
#   --pre-release       Include pre-releases when resolving "latest".
#   --no-modify-path    Do not touch any shell profile; print a snippet.
#   --install-dir <d>   Same as INAT_INSTALL_DIR (flag wins).
#
# Exit codes (mirrors plan 0002 §4.3):
#   0 success
#   1 unknown error
#   2 unsupported OS/arch or unsupported flag
#   3 checksum mismatch
#   4 network failure after one retry
#   5 requested version not found
#   6 archive layout unexpected after extraction

set -eu

readonly REPO="schubergphilis/inat"
readonly RELEASES_URL="https://api.github.com/repos/${REPO}/releases?per_page=50"
readonly DOWNLOAD_BASE="https://github.com/${REPO}/releases/download"

# Not readonly — uninstall.sh declares the same constants and the
# test runner sources both scripts in the same shell.
MANAGED_START="# >>> inat install >>>"
MANAGED_END="# <<< inat install <<<"

readonly EXIT_UNSUPPORTED=2
readonly EXIT_CHECKSUM=3
readonly EXIT_NETWORK=4
readonly EXIT_NOT_FOUND=5
readonly EXIT_ARCHIVE=6

VERSION=""
PRERELEASE="no"
MODIFY_PATH="yes"
INSTALL_DIR="${INAT_INSTALL_DIR:-$HOME/.local/bin}"

log()  { printf '%s\n' "$*" >&2; }
fail() { printf 'error: %s\n' "$*" >&2; exit "${2:-1}"; }

usage() {
    sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --version)          [ $# -ge 2 ] || fail "--version needs a value" $EXIT_UNSUPPORTED
                                VERSION="$2"; shift 2 ;;
            --version=*)        VERSION="${1#--version=}"; shift ;;
            --pre-release)      PRERELEASE="yes"; shift ;;
            --no-modify-path)   MODIFY_PATH="no"; shift ;;
            --install-dir)      [ $# -ge 2 ] || fail "--install-dir needs a value" $EXIT_UNSUPPORTED
                                INSTALL_DIR="$2"; shift 2 ;;
            --install-dir=*)    INSTALL_DIR="${1#--install-dir=}"; shift ;;
            -h|--help)          usage; exit 0 ;;
            *)                  fail "unknown flag: $1" $EXIT_UNSUPPORTED ;;
        esac
    done
}

detect_target() {
    os="$(uname -s)"
    arch="$(uname -m)"
    case "${os}-${arch}" in
        Linux-x86_64|Linux-amd64)   TARGET="x86_64-unknown-linux-gnu" ;;
        Linux-aarch64|Linux-arm64)  TARGET="aarch64-unknown-linux-gnu" ;;
        Darwin-x86_64)              TARGET="x86_64-apple-darwin" ;;
        Darwin-arm64|Darwin-aarch64) TARGET="aarch64-apple-darwin" ;;
        *) fail "unsupported host: ${os} ${arch}. Supported: Linux x86_64/aarch64, macOS x86_64/arm64." $EXIT_UNSUPPORTED ;;
    esac
}

# Single anonymous curl wrapper with one automatic retry on transient
# failure. Args: <out-path> <url>. Prints the URL to stderr on failure.
fetch() {
    out="$1"; url="$2"
    if ! curl --fail --silent --show-error --location \
             --output "${out}" "${url}" \
    && ! (sleep 2 && curl --fail --silent --show-error --location \
             --output "${out}" "${url}"); then
        fail "download failed after retry: ${url}" $EXIT_NETWORK
    fi
}

# Print "<tag> <prerelease>" per release from GitHub's /releases JSON,
# newest first. No jq dependency: grep -o pulls out just the two keys,
# whatever the whitespace, and awk pairs them up per release.
list_releases() {
    grep -oE '"(tag_name|prerelease)": *("[^"]*"|true|false)' "$1" | awk '
        /^"tag_name"/   { t = $0; sub(/^"tag_name": *"/, "", t); sub(/"$/, "", t) }
        /^"prerelease"/ { p = ($0 ~ /true$/) ? "true" : "false" }
        t != "" && p != "" { print t, p; t = ""; p = "" }'
}

# Stable means not flagged prerelease and no '-' in the tag, matching
# `inat self update` (spec 0027 §4).
resolve_version() {
    if [ -n "${VERSION}" ]; then
        return 0
    fi
    tmp="$(mktemp)"
    fetch "${tmp}" "${RELEASES_URL}"
    if [ "${PRERELEASE}" = "yes" ]; then
        VERSION="$(list_releases "${tmp}" | awk '{ print $1; exit }')"
    else
        VERSION="$(list_releases "${tmp}" | awk '$2 == "false" && $1 !~ /-/ { print $1; exit }')"
    fi
    rm -f "${tmp}"
    if [ -z "${VERSION}" ]; then
        fail "no release found (pre-release=${PRERELEASE}). Try --pre-release or --version vX.Y.Z." $EXIT_NOT_FOUND
    fi
}

# Expects VERSION set (with or without leading v). Normalises to the
# bare form (no leading v) for URL and archive construction — matches
# what ci/build-release.sh packages.
normalise_version() {
    VTAG="${VERSION#v}"
    VTAG="v${VTAG}"       # canonical tag form
    VNUM="${VTAG#v}"      # bare version
}

# Release-asset URL for file $1 in the release tagged VTAG.
asset_url() {
    printf '%s/%s/%s' "${DOWNLOAD_BASE}" "${VTAG}" "$1"
}

download_and_verify() {
    STAGE="$(mktemp -d)"
    trap 'rm -rf "${STAGE}"' EXIT

    archive="inat-${VNUM}-${TARGET}.tar.gz"
    sums="SHA256SUMS"
    archive_url="$(asset_url "${archive}")"
    sums_url="$(asset_url "${sums}")"

    log "==> downloading ${archive}"
    fetch "${STAGE}/${archive}" "${archive_url}"
    log "==> downloading ${sums}"
    fetch "${STAGE}/${sums}" "${sums_url}"

    expected="$(awk -v f="${archive}" '$2 == f || $2 == "*" f { print $1 }' "${STAGE}/${sums}")"
    if [ -z "${expected}" ]; then
        fail "no SHA256 for ${archive} in SHA256SUMS (release version ${VNUM} not published for ${TARGET}?)" $EXIT_NOT_FOUND
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "${STAGE}/${archive}" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "${STAGE}/${archive}" | awk '{print $1}')"
    else
        fail "neither sha256sum nor shasum found on PATH"
    fi

    if [ "${expected}" != "${actual}" ]; then
        fail "checksum mismatch for ${archive}: expected ${expected}, got ${actual}" $EXIT_CHECKSUM
    fi
    log "==> checksum OK"
}

extract_binary() {
    ( cd "${STAGE}" && tar -xzf "inat-${VNUM}-${TARGET}.tar.gz" )
    src="${STAGE}/inat-${VNUM}-${TARGET}/inat"
    [ -f "${src}" ] || fail "archive missing expected file: inat-${VNUM}-${TARGET}/inat" $EXIT_ARCHIVE

    mkdir -p "${INSTALL_DIR}"
    mv -f "${src}" "${INSTALL_DIR}/inat"
    chmod +x "${INSTALL_DIR}/inat"
}

# PATH already has INSTALL_DIR → no-op. Otherwise append a managed
# block to the right profile file (never touching user content).
profile_file() {
    shell_name="$(basename "${SHELL:-/bin/sh}")"
    case "${shell_name}" in
        bash) printf '%s' "${HOME}/.bashrc" ;;
        zsh)  printf '%s' "${HOME}/.zshrc" ;;
        fish) printf '%s' "${HOME}/.config/fish/config.fish" ;;
        *)    printf '' ;;
    esac
}

render_block() {
    if [ "$1" = "fish" ]; then
        cat <<EOF
${MANAGED_START}
# Added by inat installer; do not edit directly.
if not contains ${INSTALL_DIR} \$PATH
    set -x PATH ${INSTALL_DIR} \$PATH
end
${MANAGED_END}
EOF
    else
        cat <<EOF
${MANAGED_START}
# Added by inat installer; do not edit directly.
case ":\$PATH:" in *":${INSTALL_DIR}:"*) ;; *) export PATH="${INSTALL_DIR}:\$PATH" ;; esac
${MANAGED_END}
EOF
    fi
}

ensure_path() {
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*) PATH_ACTION="already-present"; return 0 ;;
    esac

    if [ "${MODIFY_PATH}" = "no" ]; then
        PATH_ACTION="skipped"
        log ""
        log "PATH mutation skipped (--no-modify-path). Add this to your shell profile:"
        log ""
        render_block "$(basename "${SHELL:-sh}")" >&2
        return 0
    fi

    profile="$(profile_file)"
    shell_name="$(basename "${SHELL:-sh}")"
    if [ -z "${profile}" ]; then
        PATH_ACTION="unknown-shell"
        log ""
        log "unknown shell (\$SHELL=${SHELL:-unset}); add this to your profile manually:"
        log ""
        render_block "${shell_name}" >&2
        return 0
    fi

    mkdir -p "$(dirname "${profile}")"
    touch "${profile}"
    if grep -Fq -- "${MANAGED_START}" "${profile}"; then
        PATH_ACTION="already-marked"
        return 0
    fi
    {
        printf '\n'
        render_block "${shell_name}"
    } >> "${profile}"
    PATH_ACTION="appended:${profile}"
}

print_summary() {
    log ""
    log "inat ${VNUM} installed."
    log "  binary:       ${INSTALL_DIR}/inat"
    log "  PATH action:  ${PATH_ACTION}"
    case "${PATH_ACTION}" in
        appended:*) log "  → open a new shell (or source the profile) to pick up the PATH change." ;;
    esac
}

main() {
    parse_args "$@"
    detect_target
    resolve_version
    normalise_version
    download_and_verify
    extract_binary
    ensure_path
    print_summary
}

# Allow this file to be sourced for unit tests without running main.
if [ "${INAT_INSTALL_TEST:-0}" != "1" ]; then
    main "$@"
fi
