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
  same stages as CI. The end-to-end stage needs Docker and `govulncheck`
  needs https://vuln.go.dev; where either is unavailable, run the other
  scripts on their own and say in the pull request which stage you could not
  run. `tools/gemini-review` is its own Go module: test it with
  `go test -race ./...` from that directory.
- **Try changes against a running server.** `scripts/start-server.sh` builds
  and starts it in the background with `.env` (log in `.run/server.log`), and
  `scripts/stop-server.sh` stops it. Stop it when you are done.
- **Scripts in `scripts/` source `lib.sh`** for strict mode, the repository
  root as working directory and the shared helpers; reuse those rather than
  copying them. A change to the start or stop scripts comes with a case in
  `scripts/server-scripts-test.sh`.
- **`cmd/` is process wiring only.** Logic belongs in `internal/`, and
  configuration is read from the environment in one place (`internal/app`).
- **Secrets stay out of logs, responses and commits.** The service handles
  health data, an API key and OAuth tokens. Every endpoint except
  `GET /health` requires the API key, compared in constant time.
- **New settings go in `.env.example` and the README's configuration table.**

## Workflow

- Work comes from Jira stories in the KAN project (epic KAN-1, whose
  description holds the engineering bar). Commit messages start with the
  story key, such as `KAN-18: Add scripts to start and stop the server`.
- Every change lands on `main` through a pull request with green CI and a
  Gemini review. Ryan decides when to merge; GitHub deletes the branch.

## Review standards

`GEMINI.md` holds the standards Gemini reviews pull requests against. Follow
them too, so the review finds nothing to flag.
