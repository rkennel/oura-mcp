#!/usr/bin/env bash
# Checks the mechanical rules of GEMINI.md, the ones marked "Checked by
# scripts/check-standards.sh", and fails naming each breach and its rule:
#
#   api-explore-file     a changed route with no change under api-explore/
#   http-no-secrets      an .http file holding what looks like a real key
#   scripts-tested       a start or stop script change with no test change
#   config-in-app        the environment read outside internal/app
#   settings-documented  a setting missing from .env.example or the README
#
# The first and third look at the change; the rest at the whole tree.
#
#   scripts/check-standards.sh [BASE]
#
# The change is the working tree against BASE, by default where this branch
# left origin/main, and includes files git does not track yet. CI passes the pull request's base. Set DIFF_FILE to check
# a diff you already have instead, as the Gemini review does, and
# STANDARDS_RESULT to keep the result as JSON for it.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command go "https://go.dev/doc/install"

# change_base prints the commit to compare the working tree with.
change_base() {
    if [ -n "${1:-}" ]; then
        git rev-parse --verify --quiet "$1^{commit}" || {
            printf 'Cannot compare with %s: no such commit.\n' "$1" >&2
            exit 1
        }
        return
    fi
    if git rev-parse --verify --quiet origin/main >/dev/null; then
        git merge-base origin/main HEAD
        return
    fi
    printf 'No origin/main to compare with; checking only uncommitted changes.\n' >&2
    git rev-parse HEAD
}

# write_change <base> prints the working tree's change from base, with each
# untracked file shown as added, since a new .http file counts as much as an
# edited one.
write_change() {
    local file
    git diff --no-color --no-ext-diff "$1" --
    while IFS= read -r -d '' file; do
        # Exit status 1 only means the file differs from /dev/null.
        git diff --no-color --no-ext-diff --no-index -- /dev/null "$file" || [ $? -eq 1 ]
    done < <(git ls-files --others --exclude-standard -z)
}

# absolute <path> prints path from the root, for the tool, which runs from
# its own directory.
absolute() {
    case "$1" in
        /*) printf '%s\n' "$1" ;;
        *) printf '%s/%s\n' "$REPO_ROOT" "$1" ;;
    esac
}

step "Mechanical rules of GEMINI.md"
if [ -n "${STANDARDS_RESULT:-}" ]; then
    STANDARDS_RESULT="$(absolute "$STANDARDS_RESULT")"
    export STANDARDS_RESULT
fi
if [ -n "${DIFF_FILE:-}" ]; then
    DIFF_FILE="$(absolute "$DIFF_FILE")"
else
    DIFF_FILE="$(mktemp)"
    trap 'rm -f "$DIFF_FILE"' EXIT
    base="$(change_base "${1:-}")"
    write_change "$base" >"$DIFF_FILE"
fi

# The tool is its own module, so it runs from its directory and is pointed
# back at the repository.
(cd tools/gemini-review && REPO_ROOT="$REPO_ROOT" DIFF_FILE="$DIFF_FILE" go run . standards)
