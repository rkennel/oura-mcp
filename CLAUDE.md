# oura-mcp

A private MCP server that exposes Oura sleep data to Claude, written in Go and
deployed as a container on Google Cloud Run. The README has the layout,
commands and configuration. Read it first.

## Rules

- **Test-driven.** New behaviour arrives with tests. `internal/` must stay at
  or above 85% statement coverage. Unit tests isolate one unit with fakes
  injected through interfaces; end-to-end tests in `e2e/` drive the production
  image.
- **Every API is explorable.** When you add or change an HTTP endpoint, add or
  update at least one `.http` file in `api-explore/` that shows how to call it,
  with a request for each success case and the main failure. Send the key as
  `x-api-key: {{apiKey}}`; never write a real key in the file.
- **Run `scripts/local-ci.sh` before opening a pull request.** It runs the
  same stages as CI.
- **`cmd/` is process wiring only.** Logic belongs in `internal/`, and
  configuration is read from the environment in one place (`internal/app`).
- **Secrets stay out of logs, responses and commits.** The service handles
  health data, an API key and OAuth tokens. Every endpoint except
  `GET /health` requires the API key, compared in constant time.
- **New settings go in `.env.example` and the README's configuration table.**

## Review standards

`GEMINI.md` holds the standards Gemini reviews pull requests against. Follow
them too, so the review finds nothing to flag.
