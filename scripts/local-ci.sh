#!/usr/bin/env bash
# Runs the three CI stages in order, stopping at the first failure, using the
# same scripts GitHub Actions runs. Tool versions on this machine may differ
# from the CI runner's.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

scripts/static-analysis.sh
scripts/unit-test.sh
scripts/e2e-test.sh

step "Local CI passed: static analysis, unit tests, end-to-end tests"
