#!/usr/bin/env bash
# Runs the CI stages in order, stopping at the first failure, using the same
# scripts GitHub Actions runs. Tool versions on this machine may differ
# from the CI runner's.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

scripts/static-analysis.sh
scripts/check-standards.sh
scripts/unit-test.sh
scripts/tools-test.sh
scripts/check-standards-test.sh
scripts/deploy-test.sh
scripts/server-scripts-test.sh
scripts/e2e-test.sh

step "Local CI passed: static analysis, standards check, unit tests, review tool, standards check script, deploy script, server scripts, end-to-end tests"
