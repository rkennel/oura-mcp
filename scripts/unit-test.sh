#!/usr/bin/env bash
# Stage 2 of CI: unit tests with the race detector, failing below the
# coverage gate. Override the threshold with COVERAGE_MIN, e.g. COVERAGE_MIN=90.
#
# Coverage is measured over internal/. cmd/ holds only the few lines that
# wire the process together, which the end-to-end tests exercise.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

COVERAGE_MIN="${COVERAGE_MIN:-85}"
PROFILE="$(mktemp)"
trap 'rm -f "$PROFILE"' EXIT

require_command go "https://go.dev/doc/install"

step "Unit tests (line coverage gate: ${COVERAGE_MIN}%)"
go test -race -coverprofile="$PROFILE" ./internal/...

go tool cover -func="$PROFILE"
total="$(go tool cover -func="$PROFILE" | awk '/^total:/ { sub("%", "", $3); print $3 }')"
if ! awk -v total="$total" -v min="$COVERAGE_MIN" 'BEGIN { exit !(total >= min) }'; then
    printf 'Coverage %s%% is below the %s%% gate\n' "$total" "$COVERAGE_MIN" >&2
    exit 1
fi

step "Unit tests passed with ${total}% coverage"
