# oura-mcp

A private MCP server that exposes Oura sleep data to Claude. Written in Rust,
compiled to WebAssembly, and deployed on Cloudflare Workers.

## Layout

| Path | Purpose |
|---|---|
| `crates/core` | Platform-independent logic. Plain Rust, unit tested natively. |
| `crates/worker` | Thin Cloudflare Workers adapter around `core`. |

## Prerequisites

- Rust (the pinned toolchain in `rust-toolchain.toml` installs itself via `rustup`)
- Node.js and npm (for `wrangler`)

## Common commands

```sh
cargo test -p oura-mcp-core          # unit tests
cargo fmt --all --check              # formatting
cargo clippy --workspace --all-targets --target wasm32-unknown-unknown -- -D warnings

cd crates/worker
npx wrangler dev                     # run locally at http://localhost:8787
curl http://localhost:8787/health
```

## Status

Scaffold only: the Worker answers `GET /health`. MCP tools, Oura access and
authentication arrive in later pull requests.
