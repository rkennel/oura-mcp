# Review guidance for Gemini

This repository is an MCP server that exposes Oura sleep data to Claude. It
is written in Go and deployed as a container on Google Cloud Run. Review
pull requests against the standards below. Report the problems you can
show. Raise one you find plausible but cannot prove as a `concern`, marked
as such, rather than dropping it. Do not praise, restate what the code does,
or ask the author to confirm anything.

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
  no panics for expected failures, accept interfaces and return concrete
  types, small interfaces defined by the consumer. Errors, contexts and
  goroutines have rules of their own below.
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

## Go rules

The places a Go service most often goes wrong where linters do not look.

- `context-propagation` **Contexts are passed down and honoured.**
  `context.Context` is the first parameter of anything that does I/O, and
  work done for a request uses that request's context: no
  `context.Background()` or `context.TODO()` inside request handling or
  anything it calls. I/O, waits and retries end when the context does.
- `outbound-http` **Outbound HTTP is bounded and cleaned up.** Every
  outbound HTTP call has a timeout set by the code that makes it, through an
  `http.Client` with `Timeout` or a context with a deadline;
  `http.DefaultClient` and `http.Get`, `http.Post` and the like have none.
  Every response body is closed on every path, error responses included.
- `goroutine-lifecycle` **Goroutines have an owner and a way to stop.**
  Whoever starts a goroutine waits for it or can tell it to stop, through a
  context or a closed channel, and no send or receive can block forever once
  the other side has gone. A goroutine that can outlive its caller is a
  leak.
- `error-handling` **Errors are wrapped and compared properly.** Errors are
  values: return them, don't log and continue. Add context when passing one
  up, with `fmt.Errorf` and `%w` so the cause stays inspectable. Compare
  sentinel errors with `errors.Is` and typed errors with `errors.As`, never
  with `==`, a type assertion on a wrapped error, or the error's text.

## Oura integration

- `oura-failures` **Oura failures map to MCP responses deliberately.** A 401
  from Oura triggers at most one token refresh and one retry. A 429 waits for
  the `Retry-After` Oura sends, within the request's context, or reports the
  limit, and never retries blindly. Any other Oura error reaches the MCP
  client as a message the server wrote, never Oura's response body or
  detail.

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

- `no-secret-leaks` **The server's own secrets stay secret.** The connector
  API key and any other secret the server is configured with, such as the
  Oura OAuth client secret, are never logged, returned in responses, or
  committed. Oura's tokens and data have a rule of their own below.
- `oura-data-private` **Oura tokens and health data stay private.** Oura
  access and refresh tokens, and the user's sleep and health data, are never
  logged, never placed in error messages, and never returned except as the
  tool results the MCP client asked for.
- `refresh-token-rotation` **Refresh tokens rotate safely.** Oura's refresh
  tokens are single-use: refreshing returns a new pair and invalidates the
  old refresh token. The new pair is saved durably before the old one is
  deleted or overwritten, and a failed save is an error, so that a crash or
  a failed write cannot leave the server with no valid refresh token.
- `api-key-required` **Only `/health` is open.** Flag any endpoint other
  than `/health` that is reachable without the API key.
- `constant-time-compare` **Secrets compare in constant time.** Flag secret
  comparisons that are not constant-time.
