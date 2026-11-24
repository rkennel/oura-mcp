#!/usr/bin/env bash
# Tests scripts/check-standards.sh: each mechanical rule of GEMINI.md it
# checks fails a change that breaks it, naming the rule, and a change that
# keeps them all passes. Each case runs in a throwaway git copy of the
# repository, so your working tree is left alone.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command go "https://go.dev/doc/install"
require_command git "https://git-scm.com/downloads"

WORK="$(mktemp -d)"
OUTPUT="$WORK/output"
failures=0
trap 'rm -rf "$WORK"' EXIT

# fresh_copy makes $COPY a git repository holding the working tree, tracked
# and untracked files alike, committed once so a case's change is what the
# script sees.
fresh_copy() {
    COPY="$WORK/repo"
    rm -rf "$COPY"
    mkdir -p "$COPY"
    git ls-files --cached --others --exclude-standard -z |
        while IFS= read -r -d '' file; do
            [ -e "$file" ] && printf '%s\0' "$file"
        done |
        tar --null -T - -cf - | tar -xf - -C "$COPY"
    git -C "$COPY" init -q
    git -C "$COPY" add -A
    git -C "$COPY" -c user.name=test -c user.email=test@example.com commit -q -m base
}

# run_check runs the script in the copy against its one commit, keeping its
# combined output in $OUTPUT and its exit status in $status.
run_check() {
    status=0
    "$COPY/scripts/check-standards.sh" HEAD >"$OUTPUT" 2>&1 || status=$?
}

fail() {
    printf '  FAIL: %s\n' "$*" >&2
    sed 's/^/    /' "$OUTPUT" >&2
    failures=$((failures + 1))
}

expect_pass() {
    [ "$status" -eq 0 ] || fail "exit status $status, want 0"
}

# expect_breach <rule> <path> fails the case unless the script failed naming
# that rule at that file.
expect_breach() {
    [ "$status" -ne 0 ] || fail "the script passed, want a breach of $1"
    grep -qF -- "FAIL [$1] $2:" "$OUTPUT" || fail "output does not name [$1] at $2"
}

add_route() {
    local handler="$COPY/internal/httpapi/handler.go"
    awk '/mux.HandleFunc\("GET \/health"/ { print "\tmux.HandleFunc(\"GET /version\", nil)" } { print }' \
        "$handler" >"$handler.new"
    mv "$handler.new" "$handler"
    grep -q 'GET /version' "$COPY/internal/httpapi/handler.go" || {
        printf 'Could not add a route to internal/httpapi/handler.go; update this test.\n' >&2
        exit 1
    }
}

step "A clean copy passes"
fresh_copy
run_check
expect_pass

step "api-explore-file: a new route with no .http change fails"
fresh_copy
add_route
run_check
expect_breach api-explore-file internal/httpapi/handler.go

step "api-explore-file: a new route with a new, untracked .http file passes"
fresh_copy
add_route
printf 'GET {{baseUrl}}/version\nx-api-key: {{apiKey}}\n' >"$COPY/api-explore/version.http"
run_check
expect_pass

step "http-no-secrets: an .http file holding a literal key fails"
fresh_copy
printf 'GET {{baseUrl}}/mcp\nx-api-key: a-real-looking-key\n' >"$COPY/api-explore/leak.http"
run_check
expect_breach http-no-secrets api-explore/leak.http

step "config-in-app: reading the environment outside internal/app fails"
fresh_copy
printf 'package oura\n\nimport "os"\n\nvar token = os.Getenv("OURA_TOKEN")\n' >"$COPY/internal/oura/token.go"
run_check
expect_breach config-in-app internal/oura/token.go

step "settings-documented: a setting missing from .env.example fails"
fresh_copy
printf 'package app\n\nfunc logLevel(getenv func(string) string) string {\n\treturn getenv("LOG_LEVEL")\n}\n' >"$COPY/internal/app/loglevel.go"
run_check
expect_breach settings-documented internal/app/loglevel.go

step "scripts-tested: changing stop-server.sh with no test change fails"
fresh_copy
printf 'echo "stopped for good"\n' >>"$COPY/scripts/stop-server.sh"
run_check
expect_breach scripts-tested scripts/stop-server.sh

step "scripts-tested: the same change with a test change passes"
printf '# covers the new message\n' >>"$COPY/scripts/server-scripts-test.sh"
run_check
expect_pass

step "An unknown base fails"
fresh_copy
status=0
"$COPY/scripts/check-standards.sh" no-such-commit >"$OUTPUT" 2>&1 || status=$?
[ "$status" -ne 0 ] && grep -qF 'no such commit' "$OUTPUT" || fail "an unknown base did not fail clearly"

if [ "$failures" -gt 0 ]; then
    printf '\n%d check-standards case(s) failed.\n' "$failures" >&2
    exit 1
fi
printf '\nEvery check-standards case passed.\n'
