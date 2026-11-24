#!/usr/bin/env bash
# Stage 2 of CI: unit tests with the race detector, failing below the
# coverage gate. Override the threshold with COVERAGE_MIN, e.g. COVERAGE_MIN=90.
#
# Coverage is measured over internal/, and separately over the fake Oura API
# the end-to-end tests run (e2e/fakeoura), which is held to the same gate.
# cmd/ directories hold only the few lines that wire a process together,
# which the end-to-end tests exercise.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

COVERAGE_MIN="${COVERAGE_MIN:-85}"
PROFILE="$(mktemp)"
trap 'rm -f "$PROFILE"' EXIT

require_command go "https://go.dev/doc/install"

# test_with_coverage <name> <packages...> runs the unit tests of packages
# and fails if their statement coverage is below COVERAGE_MIN.
test_with_coverage() {
    local name="$1" total
    shift
    step "Unit tests of ${name} (line coverage gate: ${COVERAGE_MIN}%)"
    go test -race -coverprofile="$PROFILE" "$@"

    go tool cover -func="$PROFILE"
    total="$(go tool cover -func="$PROFILE" | awk '/^total:/ { sub("%", "", $3); print $3 }')"
    if ! awk -v total="$total" -v min="$COVERAGE_MIN" 'BEGIN { exit !(total >= min) }'; then
        printf 'Coverage of %s, %s%%, is below the %s%% gate\n' "$name" "$total" "$COVERAGE_MIN" >&2
        exit 1
    fi
    step "Unit tests of ${name} passed with ${total}% coverage"
}

test_with_coverage "the server (internal/)" ./internal/...
test_with_coverage "the fake Oura API (e2e/fakeoura)" ./e2e/fakeoura
