# oura-mcp

A private MCP server that exposes Oura sleep data to Claude, written in Go and
deployed as a container on Google Cloud Run. The README has the layout,
commands and configuration. Read it first.

## Engineering standards

These are the standards Gemini reviews every pull request against. Follow
them so the review finds nothing to flag:

@GEMINI.md

## How to work

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
  copying them.

## Workflow

- Work comes from Jira stories in the KAN project (epic KAN-1, whose
  description holds the engineering bar). Commit messages start with the
  story key, such as `KAN-18: Add scripts to start and stop the server`.
- Every change lands on `main` through a pull request with green CI and a
  Gemini review. Ryan decides when to merge; GitHub deletes the branch.
