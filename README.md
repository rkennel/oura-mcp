# oura-mcp

A private MCP server that exposes Oura sleep data to Claude. Written in Go
and deployed as a container on Google Cloud Run.

## Layout

| Path | Purpose |
|---|---|
| `cmd/server` | The executable. Process wiring only. |
| `internal/app` | Configuration and server lifecycle. |
| `internal/httpapi` | HTTP routes and handlers. |
| `e2e/` | End-to-end tests and their Docker Compose environment. |
| `scripts/` | The CI stages, runnable locally. |
| `tools/gemini-review` | Fetches a pull request for the Gemini review, picks the model, and posts Gemini's reply. Its own Go module. |
| `Dockerfile` | The production image. |

## Prerequisites

- Go (the version in `go.mod`; newer `go` commands download it automatically)
- Docker with the Compose plugin (for end-to-end tests)

## Common commands

CI and local runs use the same scripts, so a local pass predicts a CI pass.
`golangci-lint` and `govulncheck` need no install: the script runs the
versions it pins with `go run`. The first run builds them, which takes a
minute or two. `govulncheck` needs to reach https://vuln.go.dev.

| Command | What it does |
|---|---|
| `scripts/static-analysis.sh` | `gofmt` check, `go vet`, a `go.mod` tidiness check, `golangci-lint` and `govulncheck` |
| `scripts/unit-test.sh` | Unit tests with the race detector, failing below 85% coverage |
| `scripts/e2e-test.sh` | Builds the production image, runs it, and runs the end-to-end tests against it |
| `scripts/local-ci.sh` | All three in order, stopping at the first failure |

Set `KEEP_E2E_UP=1` to leave the end-to-end container running after the
tests, for debugging.

To run the server by hand:

```sh
go run ./cmd/server                  # http://localhost:8080
curl http://localhost:8080/health
```

Set `PORT` to listen elsewhere.

## Testing approach

- **Unit tests** sit beside the code as `_test.go` files and test one unit in
  isolation. The 85% coverage gate applies to `internal/`.
- **End-to-end tests** live in `e2e/` behind the `e2e` build tag and drive the
  production container image over HTTP. `cmd/server` is a few lines of wiring
  and is covered here rather than by unit tests.

## Continuous integration

Every pull request runs three stages, each only if the previous one passes:
static analysis, unit tests with the coverage gate, then end-to-end tests.

Versions are pinned so a run today matches a run next month: the Go
toolchain in `go.mod`, the linters in `scripts/static-analysis.sh`, the base
images in `Dockerfile` by digest, and third-party actions by commit SHA. Lint
rules live in `.golangci.yml`.

## Code review by Gemini

`.github/workflows/gemini-code-review.yml` reviews every push to a pull
request with Gemini CLI and posts the result as a review by
`github-actions[bot]`, with line comments where it finds problems and a
"No issues found." review when it finds none. Gemini reads the pull request
and the repository with read-only tools and gets no GitHub token;
`tools/gemini-review` does the GitHub calls. A failed run means no review
was posted. Gemini's raw output is kept as the run's `gemini-output`
artifact. The review standards are in `GEMINI.md`.

### Which model reviews

`.github/gemini-review.json` picks the model for each pull request, so the
models, the size threshold and the critical paths change without a code
change:

- **High-risk model** (`gemini-4-argon`): the pull request has 400 or more
  changed lines, touches a critical path, or removes or rewrites an
  exported declaration in a shared package (or adds a method to an exported
  interface there).
- **Triage model** (`gemini-3.5-flash-lite`): reads any other pull request
  and escalates it to the high-risk model if it changes an interface other
  code or clients depend on.
- **Standard model** (`gemini-3.8-flash`): everything else.

Each critical path has a pattern and the reason it is critical. In a
pattern, `**` stands for any number of directories and `*` for part of one
name, so `**/*token*` matches any file with "token" in its name. The review
ends with which model reviewed and why, and the run's job summary shows the
evidence. If Gemini CLI answers with any other model than the one chosen,
the run fails rather than mislabel the review.

To review a pull request again by hand, run the workflow from the Actions
tab with its number.

## Status

Skeleton only: the server answers `GET /health`. Authentication, MCP tools
and Oura access arrive in later pull requests; see the repository's issues.
