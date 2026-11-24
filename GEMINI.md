# Review guidance for Gemini

This repository is an MCP server that exposes Oura sleep data to Claude. It
is written in Go and deployed as a container on Google Cloud Run. Review
pull requests against the standards below. Report only verifiable problems;
do not restate what the code does.

Each rule starts with its ID in backticks. The IDs are stable:
`tools/gemini-review` checks that the review gives a verdict on every one,
so rename an ID only together with the tool's seeded cases that name it.
Rules marked *Checked by `scripts/check-standards.sh`* are mechanical: that
script checks them in CI and in the review, so the review takes their
verdict from it.

## Engineering standards

- `test-driven` **Test-driven.** New behaviour arrives with tests. Flag
  logic under `internal/` that has no unit test.
- `unit-isolation` **Unit tests isolate one unit.** A unit test exercises a
  single function or method, with collaborators replaced by fakes injected
  through interfaces. Flag unit tests that reach the real network or other
  packages' real logic.
- `coverage` **Coverage.** `internal/` must stay at or above 85% statement
  coverage.
- `e2e-representative` **End-to-end tests are production-representative.**
  They live in `e2e/`, drive the production container image, and may use
  fakes only for external systems such as the Oura API.
- `clean-code` **Clean Code.** Small functions with one responsibility,
  intention-revealing names, no dead code, no duplicated logic, comments
  that explain why rather than what.
- `idiomatic-go` **Idiomatic Go.** Follow Effective Go and the Go Proverbs:
  errors are values and are wrapped with context, no panics for expected
  failures, accept interfaces and return concrete types, small interfaces
  defined by the consumer, `context.Context` as the first parameter of
  anything that does I/O.
- `api-explore-file` **Every API is explorable.** A pull request that adds
  or changes an HTTP route adds or updates at least one `.http` file in
  `api-explore/`. *Checked by `scripts/check-standards.sh`.*
- `api-explore-content` **`.http` files show every case.** An `.http` file
  for an endpoint shows how to call it, with a request for each success case
  and the main failure, sending the key as `x-api-key: {{apiKey}}`.
- `http-no-secrets` **No real keys in `.http` files.** An `.http` file never
  holds a real key or token. *Checked by `scripts/check-standards.sh`.*
- `scripts-tested` **Local scripts are tested.** A behaviour change to
  `scripts/start-server.sh` or `scripts/stop-server.sh` comes with a case in
  `scripts/server-scripts-test.sh`. *Checked by
  `scripts/check-standards.sh`.*

## Architecture rules

- `cmd-wiring-only` **`cmd/` is wiring.** `cmd/` contains only process
  wiring. Flag logic that belongs in `internal/`.
- `config-in-app` **Configuration in one place.** Configuration is read from
  the environment only in `internal/app`. *Checked by
  `scripts/check-standards.sh`.*
- `settings-documented` **Settings are documented.** A setting the server
  reads is in `.env.example` and in the README's configuration table.
  *Checked by `scripts/check-standards.sh`.*

## Security

- `no-secret-leaks` **Secrets stay secret.** This service handles personal
  health data, an API key and OAuth tokens. Flag secrets or tokens that are
  logged, returned in responses, or committed.
- `api-key-required` **Only `/health` is open.** Flag any endpoint other
  than `/health` that is reachable without the API key.
- `constant-time-compare` **Secrets compare in constant time.** Flag secret
  comparisons that are not constant-time.
