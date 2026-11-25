#!/bin/sh
# Unit tests for install.sh and uninstall.sh (plan 0002 §4.4).
#
# Covers parse_args, fetch (anonymous), list_releases, resolve_version,
# asset_url, strip_block — anything purely local. End-to-end install
# against a real release is out of scope here (see
# docs/specs/0002-ci-cd-and-install.md §13 and spec 0027 §6).
#
# Run: sh tests/installer_tests.sh

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0
FAIL=0

# shellcheck disable=SC2317   # helpers called indirectly via the test list
green() { printf '\033[0;32m%s\033[0m' "$1"; }
red()   { printf '\033[0;31m%s\033[0m' "$1"; }

ok()     { PASS=$((PASS + 1)); printf '  [%s] %s\n' "$(green OK)"   "$1"; }
notok()  { FAIL=$((FAIL + 1)); printf '  [%s] %s\n  %s\n' "$(red FAIL)" "$1" "$2"; }

assert_eq() {
    # $1=label  $2=actual  $3=expected
    if [ "$2" = "$3" ]; then ok "$1"
    else notok "$1" "expected '$3', got '$2'"
    fi
}

# Run a subshell expected to exit non-zero; pass if it does, fail if not.
assert_exits() {
    label="$1"; expected_code="$2"; shift 2
    set +e
    ( "$@" ) >/dev/null 2>&1
    code=$?
    set -e
    if [ "${code}" = "${expected_code}" ]; then ok "${label}"
    else notok "${label}" "expected exit ${expected_code}, got ${code}"
    fi
}

# --- Source install.sh in test mode (suppresses main execution). ---
# shellcheck disable=SC1091
INAT_INSTALL_TEST=1 . "${SCRIPT_DIR}/install.sh"

# --- parse_args ---

run_parse() {
    VERSION=""; PRERELEASE="no"; MODIFY_PATH="yes"; INSTALL_DIR="/default"
    parse_args "$@"
}

run_parse --version v0.2.0
assert_eq "parse_args --version <v>"   "${VERSION}"    "v0.2.0"
run_parse --version=v0.2.0
assert_eq "parse_args --version=<v>"   "${VERSION}"    "v0.2.0"
run_parse --pre-release
assert_eq "parse_args --pre-release"   "${PRERELEASE}" "yes"
run_parse --no-modify-path
assert_eq "parse_args --no-modify-path" "${MODIFY_PATH}" "no"
run_parse --install-dir /tmp/inat-test
assert_eq "parse_args --install-dir <d>" "${INSTALL_DIR}" "/tmp/inat-test"
run_parse --install-dir=/tmp/inat-test
assert_eq "parse_args --install-dir=<d>" "${INSTALL_DIR}" "/tmp/inat-test"

assert_exits "parse_args rejects --unknown"     2  parse_args --unknown
assert_exits "parse_args rejects bare --version" 2 parse_args --version

# --- fail ---

set +e
fail_msg="$( (fail "boom" 4) 2>&1 )"
fail_code=$?
set -e
assert_eq "fail prints only the message" "${fail_msg}" "error: boom"
assert_eq "fail exits with the given code" "${fail_code}" "4"

# --- fetch is anonymous ---
# Stub curl to record its arguments; a token left in the environment
# must never reach the request.

curl_args="$(mktemp)"
curl() { printf '%s\n' "$@" > "${curl_args}"; }
INAT_TOKEN="should-not-be-sent"; export INAT_TOKEN
fetch "$(mktemp)" "https://example.invalid/asset"
assert_eq "fetch sends no auth header" \
    "$(grep -c -e '--header' -e 'should-not-be-sent' "${curl_args}")" "0"
unset -f curl
unset INAT_TOKEN
rm -f "${curl_args}"

# --- list_releases ---
# GitHub's /releases JSON is pretty-printed, with nested objects that
# have their own keys; prerelease is a flag independent of the tag.

tmp_releases="$(mktemp)"
cat > "${tmp_releases}" <<'EOF'
[
  {
    "url": "https://api.github.com/repos/schubergphilis/inat/releases/5",
    "author": {
      "login": "github-actions[bot]",
      "type": "Bot"
    },
    "tag_name": "v0.4.0",
    "name": "v0.4.0",
    "draft": false,
    "prerelease": true,
    "assets": [
      {
        "name": "SHA256SUMS",
        "browser_download_url": "https://github.com/schubergphilis/inat/releases/download/v0.4.0/SHA256SUMS"
      }
    ]
  },
  {
    "tag_name": "v0.3.0-rc.1",
    "prerelease": true
  },
  {
    "tag_name": "v0.2.0",
    "prerelease": false
  },
  {
    "tag_name": "v0.2.0-rc.0",
    "prerelease": true
  },
  {
    "tag_name": "v0.1.0",
    "prerelease": false
  }
]
EOF
releases="$(list_releases "${tmp_releases}" | tr '\n' ',' | sed 's/,$//')"
assert_eq "list_releases pairs tag and flag, in order" "${releases}" \
    "v0.4.0 true,v0.3.0-rc.1 true,v0.2.0 false,v0.2.0-rc.0 true,v0.1.0 false"

tmp_compact="$(mktemp)"
printf '%s' '[{"tag_name":"v0.2.0","prerelease":false},{"tag_name":"v0.1.0","prerelease":false}]' \
    > "${tmp_compact}"
releases="$(list_releases "${tmp_compact}" | tr '\n' ',' | sed 's/,$//')"
assert_eq "list_releases handles compact JSON" "${releases}" "v0.2.0 false,v0.1.0 false"
rm -f "${tmp_compact}"

# --- resolve_version (stable vs pre-release filter) ---
# VERSION unset + PRERELEASE=no should pick v0.2.0: v0.4.0 is flagged
# prerelease and v0.3.0-rc.1 has a '-' tag.
# VERSION unset + PRERELEASE=yes should pick v0.4.0 (first any).
# We override fetch() to copy the fixture rather than hit the network.

fetch() { cp "${tmp_releases}" "$1"; }

VERSION=""; PRERELEASE="no"
resolve_version
assert_eq "resolve_version stable skips flagged pre-release" "${VERSION}" "v0.2.0"

VERSION=""; PRERELEASE="yes"
resolve_version
assert_eq "resolve_version pre-release" "${VERSION}" "v0.4.0"

# --version takes precedence — even when set, resolve_version must not
# overwrite it (and must not call fetch).
fetch() { echo "fetch should not be called" >&2; return 1; }
VERSION="v0.99.0"; PRERELEASE="no"
resolve_version
assert_eq "resolve_version honours --version" "${VERSION}" "v0.99.0"

rm -f "${tmp_releases}"

# --- GitHub URLs ---

assert_eq "RELEASES_URL is the GitHub API" "${RELEASES_URL}" \
    "https://api.github.com/repos/schubergphilis/inat/releases?per_page=50"

VERSION="0.9.0"; normalise_version
assert_eq "asset_url is a GitHub release download" \
    "$(asset_url inat-0.9.0-x86_64-unknown-linux-gnu.tar.gz)" \
    "https://github.com/schubergphilis/inat/releases/download/v0.9.0/inat-0.9.0-x86_64-unknown-linux-gnu.tar.gz"

# --- normalise_version ---

VERSION="0.2.0"; normalise_version
assert_eq "normalise bare version → VTAG" "${VTAG}" "v0.2.0"
assert_eq "normalise bare version → VNUM" "${VNUM}" "0.2.0"

VERSION="v0.2.0-rc.0"; normalise_version
assert_eq "normalise tag form → VTAG"  "${VTAG}" "v0.2.0-rc.0"
assert_eq "normalise tag form → VNUM"  "${VNUM}" "0.2.0-rc.0"

# --- uninstall.sh strip_block ---
# Run in a subshell so uninstall.sh's `readonly` declarations don't
# clash with install.sh's (same constant names, already set above).
uninstall_tests_out="$(mktemp)"
(
    # shellcheck disable=SC1091
    INAT_UNINSTALL_TEST=1 . "${SCRIPT_DIR}/uninstall.sh"

    fail_msg="$( (fail "boom" 2) 2>&1 )" || true

    workdir="$(mktemp -d)"
    profile="${workdir}/.bashrc"
    cat > "${profile}" <<EOF
# user content before
export PS1='>'

# >>> inat install >>>
# Added by inat installer; do not edit directly.
case ":\$PATH:" in *":/foo/bin:"*) ;; *) export PATH="/foo/bin:\$PATH" ;; esac
# <<< inat install <<<

# user content after
alias ll='ls -la'
EOF

    strip_block "${profile}"
    cleaned="$(cat "${profile}")"
    has_markers=0
    case "${cleaned}" in
        *">>> inat install >>>"*) has_markers=1 ;;
        *"<<< inat install <<<"*) has_markers=1 ;;
    esac
    has_user=0
    case "${cleaned}" in
        *"user content before"*"user content after"*) has_user=1 ;;
    esac

    # Idempotency: second strip on markerless file is a no-op.
    strip_block "${profile}"
    cleaned2="$(cat "${profile}")"
    idem=0
    [ "${cleaned}" = "${cleaned2}" ] && idem=1

    printf '%s\n%s\n%s\n%s\n' "${has_markers}" "${has_user}" "${idem}" "${fail_msg}"
    rm -rf "${workdir}"
) > "${uninstall_tests_out}"

has_markers="$(sed -n '1p' "${uninstall_tests_out}")"
has_user="$(sed -n '2p' "${uninstall_tests_out}")"
idem="$(sed -n '3p' "${uninstall_tests_out}")"
uninstall_fail_msg="$(sed -n '4p' "${uninstall_tests_out}")"
rm -f "${uninstall_tests_out}"

assert_eq "strip_block removes markers"       "${has_markers}" "0"
assert_eq "strip_block preserves user content" "${has_user}"    "1"
assert_eq "strip_block idempotent"             "${idem}"        "1"
assert_eq "uninstall fail prints only the message" "${uninstall_fail_msg}" "error: boom"

# --- Summary ---
printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[ "${FAIL}" -eq 0 ]
