#!/usr/bin/env bash
# Stage 1 of CI: formatting, lints (warnings are errors) and dependency audit.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command cargo "https://rustup.rs"
require_cargo_subcommand deny cargo-deny

step "Formatting (rustfmt)"
cargo fmt --all --check

step "Lints for native crates (clippy)"
cargo clippy -p oura-mcp-core -p oura-mcp-e2e --all-targets -- -D warnings

step "Lints for the Worker, built for WebAssembly (clippy)"
cargo clippy -p oura-mcp-worker --target wasm32-unknown-unknown -- -D warnings

step "Dependency advisories, licences and sources (cargo-deny)"
cargo deny check

step "Static analysis passed"
