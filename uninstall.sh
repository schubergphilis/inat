#!/bin/sh
# inat uninstaller for POSIX (macOS, Linux).
#
# Canonical usage (pair with install.sh):
#     curl -fsSL https://github.com/schubergphilis/inat/releases/latest/download/uninstall.sh | sh
#
# Implements docs/specs/0002-ci-cd-and-install.md §9.5:
#   1. Remove the inat binary from INSTALL_DIR.
#   2. Remove the managed `>>> inat install >>>` / `<<< inat install <<<`
#      block from known shell profiles (bash, zsh, fish).
#   3. Leave user configuration and data alone (.inat/ in projects,
#      ~/.inat/ — not the uninstaller's job).
#
# Environment:
#   INAT_INSTALL_DIR  Directory the binary lives in. Default $HOME/.local/bin.
#
# Flags:
#   --install-dir <d>  Same as INAT_INSTALL_DIR (flag wins).
#
# Exit codes: 0 success, 1 unknown error, 2 unsupported flag.

set -eu

# Not readonly — install.sh declares the same constants and the
# test runner sources both scripts in the same shell.
MANAGED_START="# >>> inat install >>>"
MANAGED_END="# <<< inat install <<<"

INSTALL_DIR="${INAT_INSTALL_DIR:-$HOME/.local/bin}"
PROFILES_TOUCHED=""
BIN_ACTION=""

log()  { printf '%s\n' "$*" >&2; }
fail() { printf 'error: %s\n' "$*" >&2; exit "${2:-1}"; }

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --install-dir)      [ $# -ge 2 ] || fail "--install-dir needs a value" 2
                                INSTALL_DIR="$2"; shift 2 ;;
            --install-dir=*)    INSTALL_DIR="${1#--install-dir=}"; shift ;;
            -h|--help)          sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
            *)                  fail "unknown flag: $1" 2 ;;
        esac
    done
}

remove_binary() {
    bin="${INSTALL_DIR}/inat"
    if [ -f "${bin}" ]; then
        rm -f "${bin}"
        log "removed ${bin}"
        BIN_ACTION="removed"
    else
        log "no binary at ${bin} (already gone?)"
        BIN_ACTION="absent"
    fi
}

# Strip the managed block from a single profile file, atomically.
# Matches any content between MANAGED_START and MANAGED_END
# (inclusive). User content outside the markers is preserved.
strip_block() {
    profile="$1"
    [ -f "${profile}" ] || return 0
    grep -Fq -- "${MANAGED_START}" "${profile}" || return 0

    tmp="$(mktemp)"
    awk -v s="${MANAGED_START}" -v e="${MANAGED_END}" '
        index($0, s) { in_block = 1; next }
        in_block && index($0, e) { in_block = 0; next }
        !in_block { print }
    ' "${profile}" > "${tmp}"

    # Collapse trailing blank lines produced by the stripped block.
    # Use awk rather than sed -i for macOS compatibility.
    awk '
        { lines[NR] = $0 }
        END {
            last = NR
            while (last > 0 && lines[last] == "") last--
            for (i = 1; i <= last; i++) print lines[i]
        }
    ' "${tmp}" > "${tmp}.2"

    mv -f "${tmp}.2" "${profile}"
    rm -f "${tmp}"
    log "stripped managed block from ${profile}"
    PROFILES_TOUCHED="${PROFILES_TOUCHED} ${profile}"
}

strip_all_profiles() {
    PROFILES_TOUCHED=""
    for p in \
        "${HOME}/.bashrc" \
        "${HOME}/.zshrc" \
        "${HOME}/.config/fish/config.fish" \
        "${HOME}/.profile" \
        "${HOME}/.bash_profile"
    do
        strip_block "${p}"
    done
}

print_summary() {
    log ""
    log "inat uninstalled."
    log "  binary:     ${BIN_ACTION} (${INSTALL_DIR}/inat)"
    if [ -n "${PROFILES_TOUCHED}" ]; then
        log "  profiles:  ${PROFILES_TOUCHED# }"
        log "  → open a new shell (or source the profile) to drop the PATH entry."
    else
        log "  profiles:  none touched (no managed block found)"
    fi
    log ""
    log "Note: project-level .inat/ directories and ~/.inat/ are preserved"
    log "      — remove them manually if you want a fully clean slate."
}

main() {
    parse_args "$@"
    remove_binary
    strip_all_profiles
    print_summary
}

if [ "${INAT_UNINSTALL_TEST:-0}" != "1" ]; then
    main "$@"
fi
