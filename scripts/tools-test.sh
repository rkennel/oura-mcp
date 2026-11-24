#!/usr/bin/env bash
# Tests tools/gemini-review, the review tool, which is its own Go module, and
# checks its seeded cases still apply. The tool also runs the mechanical
# standards check, so CI tests it beside the code.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command go "https://go.dev/doc/install"

step "Review tool tests (tools/gemini-review)"
(cd tools/gemini-review && go vet ./... && go test -race ./...)

# The seeded-defect run applies each case's patch to the branch it runs on,
# so a change to code a case touches must come with a regenerated patch.
step "Seeded cases still apply"
for change in tools/gemini-review/seeded/*/change.diff; do
    git apply --check "$change" || {
        printf '%s no longer applies: regenerate it against this branch.\n' "$change" >&2
        exit 1
    }
done
