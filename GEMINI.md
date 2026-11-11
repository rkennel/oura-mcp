# Review guidance for Gemini

This repository is an MCP server that exposes Oura sleep data, written in
Rust and deployed on Cloudflare Workers (WebAssembly). Review pull requests
against the standards below. Report only verifiable problems; do not restate
what the code does.

## Engineering standards

- **Test-driven.** New behaviour arrives with tests. Flag logic in
  `crates/core` that has no unit test.
- **Unit tests isolate one unit.** A unit test exercises a single function or
  method, with collaborators replaced by fakes injected through traits. Flag
  unit tests that reach the network, the clock or other modules' real logic.
- **Coverage.** `crates/core` must stay at or above 85% line coverage.
- **End-to-end tests are production-representative.** They live in
  `crates/e2e`, drive the real Worker build inside a container, and may use
  fakes only for external systems such as the Oura API.
- **Clean Code.** Small functions with one responsibility, intention-revealing
  names, no dead code, no duplicated logic, comments that explain why rather
  than what.
- **Idiomatic Rust.** Follow the Rust API Guidelines and clippy's advice:
  `Result` and `?` over panics, no `unwrap`/`expect` outside tests, borrow
  rather than clone where practical, no `unsafe`.

## Architecture rules

- `crates/core` is platform-independent: no Cloudflare or WebAssembly
  dependencies. Flag any that appear.
- `crates/worker` is a thin adapter. Flag business logic that belongs in
  `crates/core`.

## Security

- This service handles personal health data and OAuth tokens. Flag secrets or
  tokens that are logged, returned in responses, or committed.
- Flag any endpoint that returns Oura data without checking the caller's
  identity.
