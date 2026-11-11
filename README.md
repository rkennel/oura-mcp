# oura-mcp

A private MCP server that exposes Oura sleep data to Claude. Written in Rust,
compiled to WebAssembly, and deployed on Cloudflare Workers.

## Layout

| Path | Purpose |
|---|---|
| `crates/core` | Platform-independent logic. Plain Rust, unit tested natively. |
| `crates/worker` | Thin Cloudflare Workers adapter around `core`. |
| `crates/e2e` | End-to-end tests that call the running Worker over HTTP. |
| `e2e/` | Container setup for the end-to-end environment. |

## Prerequisites

- Rust (the pinned toolchain in `rust-toolchain.toml` installs itself via `rustup`)
- Node.js and npm (for `wrangler`)
- Docker (for end-to-end tests)
- `cargo install cargo-llvm-cov cargo-deny` (coverage and dependency audit)

## Common commands

CI and local runs use the same scripts, so a local pass predicts a CI pass.

| Command | What it does |
|---|---|
| `scripts/static-analysis.sh` | `rustfmt` check, `clippy` with warnings as errors, and `cargo-deny` |
| `scripts/unit-test.sh` | Unit tests for `crates/core`, failing below 85% line coverage |
| `scripts/e2e-test.sh` | Builds the Worker into a container, runs it in the Workers runtime, and runs `crates/e2e` against it |
| `scripts/local-ci.sh` | All three in order, stopping at the first failure |

Set `KEEP_E2E_UP=1` to leave the end-to-end container running after the
tests, for debugging.

To run the Worker by hand:

```sh
cd crates/worker
npx wrangler dev                     # http://localhost:8787
curl http://localhost:8787/health
```

## Testing approach

- **Unit tests** live beside the code in `crates/core` and test one unit in
  isolation. The 85% coverage gate applies to this crate.
- **End-to-end tests** live in `crates/e2e` and drive the real Worker build
  over HTTP inside a container. `crates/worker` is deliberately thin and is
  covered here rather than by unit tests.

## Continuous integration

Every pull request runs three stages, each only if the previous one passes:
static analysis, unit tests with the coverage gate, then end-to-end tests.

## Status

Scaffold only: the Worker answers `GET /health`. MCP tools, Oura access and
authentication arrive in later pull requests.
