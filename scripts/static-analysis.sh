#!/usr/bin/env bash
# Stage 1 of CI: formatting, lints (warnings are errors) and dependency audit.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command cargo "https://rustup.rs"
require_cargo_subcommand deny cargo-deny

step "Formatting (rustfmt)"
cargo fmt --all --check

# Every crate except the Worker builds natively, so new crates are linted
# without editing this script.
step "Lints for native crates (clippy)"
cargo clippy --workspace --exclude oura-mcp-worker --all-targets --locked -- -D warnings

step "Lints for the Worker, built for WebAssembly (clippy)"
cargo clippy -p oura-mcp-worker --target wasm32-unknown-unknown --locked -- -D warnings

step "Dependency advisories, licences and sources (cargo-deny)"
cargo deny check

step "Static analysis passed"
