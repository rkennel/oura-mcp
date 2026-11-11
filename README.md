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

| Command | What it does |
|---|---|
| `make lint` | `rustfmt` check and `clippy` with warnings as errors |
| `make audit` | Dependency advisories, licences and sources (`cargo-deny`) |
| `make test` | Unit tests for `crates/core` |
| `make coverage` | Unit tests, failing below 85% line coverage |
| `make e2e` | Builds the Worker into a container, runs it in the Workers runtime, and runs `crates/e2e` against it |
| `make ci` | All of the above, in the order CI runs them |

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
