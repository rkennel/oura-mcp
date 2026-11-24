#!/usr/bin/env bash
# Runs the CI stages the Gemini review takes facts from, and keeps how each
# ended in REVIEW_DIR for `gemini-review facts`. A failing stage does not
# stop the script: its failure is a fact for the review.
#
#   REVIEW_DIR=.gemini-review scripts/review-facts.sh
#
# REVIEW_DIR must already hold pr.diff, from `gemini-review prepare` or
# `gemini-review seed`. For each stage it writes stages/<stage>.exit and
# stages/<stage>.log, and it keeps the unit tests' coverage profile of
# internal/ in coverage/ and the standards check's result in standards.json.
# The end-to-end tests are left to CI: they need Docker and take minutes.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

: "${REVIEW_DIR:?Set REVIEW_DIR to the review directory}"
[ -f "$REVIEW_DIR/pr.diff" ] || {
    printf '%s/pr.diff is missing: run gemini-review prepare or seed first.\n' "$REVIEW_DIR" >&2
    exit 1
}
STAGES="$REVIEW_DIR/stages"
mkdir -p "$STAGES" "$REVIEW_DIR/coverage"

# run_stage <name> <command...> runs a stage, keeping its output and status.
run_stage() {
    local name="$1" status=0
    shift
    step "Review fact: ${name}"
    "$@" >"$STAGES/$name.log" 2>&1 || status=$?
    printf '%s\n' "$status" >"$STAGES/$name.exit"
    printf '%s exited with status %s\n' "$name" "$status"
}

run_stage static-analysis scripts/static-analysis.sh
run_stage unit-tests env COVERAGE_PROFILE_DIR="$REVIEW_DIR/coverage" scripts/unit-test.sh
run_stage standards env DIFF_FILE="$REVIEW_DIR/pr.diff" STANDARDS_RESULT="$REVIEW_DIR/standards.json" \
    scripts/check-standards.sh
