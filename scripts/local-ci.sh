#!/usr/bin/env bash
# Runs the three CI stages in order, stopping at the first failure, exactly
# as GitHub Actions does on a pull request.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

scripts/static-analysis.sh
scripts/unit-test.sh
scripts/e2e-test.sh

step "Local CI passed: static analysis, unit tests, end-to-end tests"
