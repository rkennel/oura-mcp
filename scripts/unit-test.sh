#!/usr/bin/env bash
# Stage 2 of CI: unit tests for crates/core, failing below the coverage gate.
# Override the threshold with COVERAGE_MIN, e.g. COVERAGE_MIN=90.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

COVERAGE_MIN="${COVERAGE_MIN:-85}"

require_command cargo "https://rustup.rs"
require_cargo_subcommand llvm-cov cargo-llvm-cov

step "Unit tests (line coverage gate: ${COVERAGE_MIN}%)"
cargo llvm-cov -p oura-mcp-core --fail-under-lines "$COVERAGE_MIN"

step "Unit tests passed"
