#!/bin/sh
# Extracts a single `## [X.Y.Z]` section from CHANGELOG.md for use as
# release notes. Emits the section body (everything between the heading
# and the next `## [` or end of file), without the heading itself.
#
# Usage:
#
#     ci/extract-changelog-section.sh 0.2.0
#
# Exit codes:
#   0 — section found and printed
#   2 — no version provided
#   3 — section not present (or empty) in CHANGELOG.md

set -eu

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    echo "error: no version provided (usage: $0 X.Y.Z)" >&2
    exit 2
fi

# String-based match (not regex) because `[X.Y.Z]` would be a character
# class to awk's regex engine and `.` would match any char.
SECTION=$(
    awk -v version="$VERSION" '
        BEGIN { target = "## [" version "]" }
        substr($0, 1, length(target)) == target { found = 1; next }
        # End of section: either next `## [...]` heading or a
        # Keep-a-Changelog link reference line `[tag]: url`.
        found && substr($0, 1, 4) == "## [" { exit }
        found && substr($0, 1, 1) == "[" && index($0, "]: ") > 0 { exit }
        found { print }
    ' CHANGELOG.md
)

if [ -z "$SECTION" ]; then
    echo "error: no '## [$VERSION]' section in CHANGELOG.md (or section is empty)" >&2
    exit 3
fi

echo "$SECTION"
