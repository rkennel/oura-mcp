# Review guidance for Gemini

This repository is an MCP server that exposes Oura sleep data to Claude. It
is written in Go and deployed as a container on Google Cloud Run. Review
pull requests against the standards below. Report only verifiable problems;
do not restate what the code does.

## Engineering standards

- **Test-driven.** New behaviour arrives with tests. Flag logic under
  `internal/` that has no unit test.
- **Unit tests isolate one unit.** A unit test exercises a single function or
  method, with collaborators replaced by fakes injected through interfaces.
  Flag unit tests that reach the real network or other packages' real logic.
- **Coverage.** `internal/` must stay at or above 85% statement coverage.
- **End-to-end tests are production-representative.** They live in `e2e/`,
  drive the production container image, and may use fakes only for external
  systems such as the Oura API.
- **Clean Code.** Small functions with one responsibility, intention-revealing
  names, no dead code, no duplicated logic, comments that explain why rather
  than what.
- **Idiomatic Go.** Follow Effective Go and the Go Proverbs: errors are
  values and are wrapped with context, no panics for expected failures,
  accept interfaces and return concrete types, small interfaces defined by
  the consumer, `context.Context` as the first parameter of anything that
  does I/O.

- **Every API is explorable.** A pull request that adds or changes an HTTP
  endpoint adds or updates at least one `.http` file in `api-explore/` that
  shows how to call it. Flag endpoints without one, and `.http` files that
  contain a real key or token.

## Architecture rules

- `cmd/` contains only process wiring. Flag logic that belongs in `internal/`.
- Configuration is read from the environment in one place (`internal/app`).

## Security

- This service handles personal health data, an API key and OAuth tokens.
  Flag secrets or tokens that are logged, returned in responses, or committed.
- Flag any endpoint other than `/health` that is reachable without the API key.
- Flag secret comparisons that are not constant-time.
