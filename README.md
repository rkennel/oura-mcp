# oura-mcp

A private MCP server that exposes Oura sleep data to Claude. Written in Go
and deployed as a container on Google Cloud Run.

## Layout

| Path | Purpose |
|---|---|
| `cmd/server` | The executable. Process wiring only. |
| `internal/app` | Configuration and server lifecycle. |
| `internal/httpapi` | HTTP routes and handlers. |
| `api-explore/` | `.http` files that show how to call each API, for the REST Client extension. |
| `e2e/` | End-to-end tests and their Docker Compose environment. |
| `scripts/` | The CI stages, runnable locally, and the scripts that run the server on your machine. |
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
| `scripts/server-scripts-test.sh` | Tests the start and stop scripts below against a throwaway copy of the repository |
| `scripts/local-ci.sh` | All of the above in order, stopping at the first failure |
| `scripts/start-server.sh` | Builds the server and starts it in the background with the settings in `.env` |
| `scripts/stop-server.sh` | Stops the server that `start-server.sh` started |
| `scripts/run-server.sh` | Runs the server in the foreground with the settings in `.env` |

Set `KEEP_E2E_UP=1` to leave the end-to-end container running after the
tests, for debugging.

## Running the server locally

Set up your local settings once (see [Configuration](#configuration)), then
start the server in the background and stop it when you are done:

```sh
scripts/start-server.sh                                # builds, starts, waits for /health
curl http://localhost:8080/health                      # open
curl -H "x-api-key: <your CONNECTOR_API_KEY>" http://localhost:8080/mcp
scripts/stop-server.sh                                 # graceful shutdown
```

`start-server.sh` builds the server into `bin/server`, starts it in the
background, waits until `GET /health` answers, then prints the server's URL
and where its log goes (`.run/server.log`, replaced on each start). It does
nothing if the server is already running, and refuses to start if
`CONNECTOR_API_KEY` is not set or another program already answers on the
port. `stop-server.sh` asks the server to shut down gracefully and waits for
it to exit; with no server running it just says so. Both keep the server's
PID in `.run/server.pid`, which Git ignores.

To watch requests as they arrive, run the server in the foreground instead
with `scripts/run-server.sh` and stop it with Ctrl-C.

## Exploring the API

`api-explore/` holds `.http` files you can run from VS Code with the
[REST Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client)
extension (`humao.rest-client`, recommended in `.vscode/extensions.json`).
Start the server, open a file such as `api-explore/health.http`, and click
**Send Request** above a request.

**When you add an API endpoint, add at least one `.http` file to
`api-explore/` that shows how to call it.** Include a request for each
success case and for the main failure, such as a missing key. Take the base
URL from `@baseUrl`, and send the key as `x-api-key: {{apiKey}}` rather than
writing a real key in the file.

## Configuration

| Variable | Required | Purpose |
|---|---|---|
| `CONNECTOR_API_KEY` | Yes | The key every request except `GET /health` must send in its `x-api-key` header. In production it comes from Secret Manager. Never log it. |
| `PORT` | No | The port to listen on; 8080 by default. Cloud Run sets it. |

### Setting them locally

The server reads only real environment variables. For local runs, keep them
in a `.env` file at the repository root, which the scripts load for you:

1. Copy the sample: `cp .env.example .env`
2. Edit `.env` and set each variable. Any value works for
   `CONNECTOR_API_KEY` locally; use the same value in the `x-api-key`
   header when you call the server.
3. Run `scripts/start-server.sh` (or `scripts/run-server.sh` for the
   foreground). `scripts/e2e-test.sh` loads `.env` too.

Git ignores `.env` (and `.env.*`, apart from `.env.example`), and Docker
leaves it out of the image, so a key in it stays on your machine. A variable
already set in your shell wins over `.env`, so
`PORT=9090 scripts/start-server.sh` overrides it for one run. The file is read
as plain `KEY=value` lines, not run as a script. When you add a setting the
server reads, add it to `.env.example` and the table above.

In production, Cloud Run sets these variables, with `CONNECTOR_API_KEY`
taken from Secret Manager.

A request with no key or the wrong key gets `401 unauthorized`, the same
answer either way. The key is compared as a SHA-256 digest in constant
time, so response timing reveals neither its content nor its length. The
server refuses to start if the key is missing or has leading or trailing
whitespace, such as a trailing newline saved with the secret.

The end-to-end script uses `CONNECTOR_API_KEY` from your shell or `.env`,
or generates a throwaway key for the run when neither sets one. Use your own
key to call a server you kept up with `KEEP_E2E_UP=1`.

## Testing approach

- **Unit tests** sit beside the code as `_test.go` files and test one unit in
  isolation. The 85% coverage gate applies to `internal/`.
- **End-to-end tests** live in `e2e/` behind the `e2e` build tag and drive the
  production container image over HTTP. `cmd/server` is a few lines of wiring
  and is covered here rather than by unit tests.

## Continuous integration

Every pull request runs three stages, each only if the previous one passes:
static analysis, unit tests with the coverage gate, then end-to-end tests.
The end-to-end stage also tests the local start and stop scripts.

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

### Pull requests from forks

The review runs only on pull requests from branches in this repository,
because it builds and runs the pull request's code with the Gemini key and a
token that can write. Before the review job starts, a job with no secrets and
a read-only token asks GitHub where the pull request's head branch lives. On
a fork's pull request, whether the pull request or a manual run started the
workflow, the review job is skipped and that job's summary says why.

## Status

The server answers `GET /health` and requires the API key on everything
else. MCP tools and Oura access arrive in later pull requests; see the KAN
project in Jira.
