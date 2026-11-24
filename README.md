# oura-mcp

A private MCP server that exposes Oura sleep data to Claude. Written in Go
and deployed as a container on Google Cloud Run.

## Layout

| Path | Purpose |
|---|---|
| `cmd/server` | The executable. Process wiring only. |
| `internal/app` | Configuration and server lifecycle. |
| `internal/httpapi` | HTTP routes and handlers. |
| `internal/mcpserver` | The MCP server: its name and version, its tools, and the HTTP transport. |
| `internal/oura` | The Oura API client and the rules that turn Oura's sleep data into a summary per wake-up date. |
| `api-explore/` | `.http` files that show how to call each API, for the REST Client extension. |
| `e2e/` | End-to-end tests and their Docker Compose environment. |
| `e2e/fakeoura` | A fake Oura API that the end-to-end environment runs as its own container. |
| `scripts/` | The CI stages, runnable locally, the scripts that run the server on your machine, and the deploy. |
| `docs/` | Guides, such as the Google Cloud setup and deploy. |
| `tools/gemini-review` | Fetches a pull request for the Gemini review, picks the model, works out the facts it reviews with, checks the mechanical rules of `GEMINI.md`, and posts Gemini's reply. Its own Go module. |
| `tools/gemini-review/seeded` | Seeded-defect cases: known changes that each break one rule of `GEMINI.md`, or none, for proving the review catches them. |
| `.github/actions/gemini-review` | The review steps (routing, triage, prompt, Gemini) shared by the pull request review and the seeded-defect run. |
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
| `scripts/check-standards.sh [BASE]` | Checks the mechanical rules of `GEMINI.md` against your change from `BASE` (default: where your branch left `origin/main`), untracked files included, and names each breach and its rule |
| `scripts/unit-test.sh` | Unit tests with the race detector, failing below 85% coverage (of `internal/`, and of `e2e/fakeoura` on its own) |
| `scripts/tools-test.sh` | Tests `tools/gemini-review`, including a check that every seeded case still fits `GEMINI.md` and its change |
| `scripts/check-standards-test.sh` | Tests `scripts/check-standards.sh` against throwaway copies with a change that breaks each rule |
| `scripts/deploy-test.sh` | Tests `scripts/deploy.sh` against stand-ins for `gcloud`, `docker` and `curl` |
| `scripts/e2e-test.sh` | Builds the production image, runs it beside the fake Oura API, and runs the end-to-end tests against them |
| `scripts/server-scripts-test.sh` | Tests the start and stop scripts below against a throwaway copy of the repository |
| `scripts/local-ci.sh` | All of the above in order, stopping at the first failure |
| `scripts/start-server.sh` | Builds the server and starts it in the background with the settings in `.env` |
| `scripts/stop-server.sh` | Stops the server that `start-server.sh` started |
| `scripts/run-server.sh` | Runs the server in the foreground with the settings in `.env` |
| `scripts/deploy.sh` | Builds the image from the current commit and deploys it to Cloud Run (see below); `--dry-run` only prints the commands |

Set `KEEP_E2E_UP=1` to leave the end-to-end container running after the
tests, for debugging.

## Running the server locally

Set up your local settings once (see [Configuration](#configuration)), then
start the server in the background and stop it when you are done:

```sh
scripts/start-server.sh                                # builds, starts, waits for /health
curl http://localhost:8080/health                      # open
curl -X POST http://localhost:8080/mcp \
  -H "x-api-key: <your CONNECTOR_API_KEY>" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}'
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
**Send Request** above a request. Requests that need the API key send it as
`{{apiKey}}`, which `.vscode/settings.json` sets to the placeholder key from
`.env.example`. If your `.env` uses another key, set
`rest-client.environmentVariables` in your own user settings instead.

**When you add an API endpoint, add at least one `.http` file to
`api-explore/` that shows how to call it.** Include a request for each
success case and for the main failure, such as a missing key. Take the base
URL from `@baseUrl`, and send the key as `x-api-key: {{apiKey}}` rather than
writing a real key in the file.

## The MCP endpoint

`POST /mcp` speaks the Model Context Protocol over the streamable HTTP
transport, using the official Go SDK. It is stateless: the server keeps no
session between requests, so each one stands alone and gets a plain JSON
answer. Like every route except `GET /health`, it needs the API key.

The server reports its name, `oura-mcp`, and its version in the MCP
handshake. The version is `dev` unless the build sets it: the `Dockerfile`
takes it as the `VERSION` build argument, and a plain `go build` takes
`-ldflags "-X main.version=..."`.

For now it offers one tool, `ping`, which takes no arguments and replies
`pong`, so you can check that a client reaches the server before any Oura
data is involved. `api-explore/mcp.http` has a request for each MCP call.

## Reading Oura

`internal/oura` reads two of Oura's collections for a range of wake-up
dates and summarizes each date. The MCP tool that exposes it comes in a
later story.

| Value | Where it comes from |
|---|---|
| Sleep score | The `score` of the date's `daily_sleep` document. |
| Lowest heart rate | The `lowest_heart_rate` of the date's main sleep in the `sleep` collection. |
| Average HRV | The `average_hrv` of the date's main sleep, rounded to a whole number. |

The main sleep is the date's `long_sleep`, or the longest of them when a
night is broken into several. Naps and short sleeps (`late_nap`, `sleep`,
`rest`) never count, even on a date with no long sleep. Anything Oura does
not have yet, such as a night the ring has not synced or a score Oura has
not worked out, is reported as `no data yet` and never estimated.

The client takes the access token from a `TokenSource`, so token storage
and refresh stay out of it. It follows `next_token` through every page and
gives up on a read after 15 seconds with an error that names the
collection. An error status from Oura comes back as a `StatusError` with the
status and Oura's explanation.

Oura's API reference does not say whether `end_date` is included, and users
report that the `sleep` collection leaves the `end_date` day out while
`daily_sleep` keeps it. The client therefore asks for a day either side of
the range and keeps only the documents whose `day` is in it, which gives the
same answer either way. This still needs checking against real Oura data in
the production rollout.

## Configuration

| Variable | Required | Purpose |
|---|---|---|
| `CONNECTOR_API_KEY` | Yes | The key every request except `GET /health` must send in its `x-api-key` header. In production it comes from Secret Manager. Never log it. |
| `PORT` | No | The port to listen on; 8080 by default. Cloud Run sets it. |
| `OURA_API_BASE_URL` | No | Where the server reaches the Oura API, for both its token endpoint and its data; `https://api.ouraring.com` by default. The end-to-end environment points it at the fake Oura API. The server logs it at startup and refuses to start if it is not an `http` or `https` URL with a host and no credentials, query or fragment. |

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

## Deploying to Cloud Run

The server runs on Cloud Run in the `kennelfitness` Google Cloud project, in
`us-central1`. [docs/gcp-setup.md](docs/gcp-setup.md) has the one-time
setup (APIs, image repository, service account, the key in Secret Manager,
the budget alert), the deploy, and how to add the connector in Claude.
After the setup, each deploy is:

```sh
scripts/deploy.sh
```

It deploys the committed code only, and finishes by checking that the
deployed `/health` answers and that `/mcp` refuses a request without the
key.

## Testing approach

- **Unit tests** sit beside the code as `_test.go` files and test one unit in
  isolation. The 85% coverage gate applies to `internal/`.
- **End-to-end tests** live in `e2e/` behind the `e2e` build tag and drive the
  production container image over HTTP. `cmd/server` is a few lines of wiring
  and is covered here rather than by unit tests.
- **External systems are faked**, never called, in the end-to-end
  environment. See [The fake Oura API](#the-fake-oura-api).

## The fake Oura API

`e2e/fakeoura` is a small Go service that stands in for Oura. The
end-to-end environment (`e2e/compose.yaml`) runs it as its own container at
`http://fakeoura:8081` inside the environment, and publishes it on
`http://127.0.0.1:8081` for the tests. It holds no real data and is never
deployed. It serves the parts of Oura the server uses, the way Oura does:

| Endpoint | What it does |
|---|---|
| `POST /oauth/token` | Exchanges a refresh token (`grant_type=refresh_token`) for a new pair. Like Oura, each refresh token works **once**: presenting it again gets `400 invalid_grant`. The client must authenticate as the `OURA_CLIENT_ID` and `OURA_CLIENT_SECRET` the fake runs with, in the form or with HTTP Basic. |
| `GET /v2/usercollection/daily_sleep` | The seeded `daily_sleep` documents whose `day` is from `start_date` to `end_date`, both included, as `{"data": [...], "next_token": null}`. Needs `Authorization: Bearer` with a current access token. Both dates are required (Oura would default them), so a client that relies on Oura's defaults fails loudly. |
| `GET /v2/usercollection/sleep` | The same for the `sleep` collection. |

Oura's API reference does not say whether `end_date` is included; the fake
includes it for both collections. The server's client does not depend on
either answer (see [Reading Oura](#reading-oura)).

Tests set the fake up through its control endpoints, all `POST`. They have
no authentication, which is why the fake is published on loopback only:

| Control | What it does |
|---|---|
| `/_control/reset` | Forgets every token, document and pending failure. Start each test with it. |
| `/_control/seed` | Replaces the documents of each collection in the body, such as `{"sleep": [{"day": "2026-11-01", "average_hrv": 42}]}`. Documents are served exactly as seeded; each needs a `day`. |
| `/_control/tokens` | Issues a token pair, as completing the Oura login would, and returns it in the token endpoint's form. |
| `/_control/expire-access-tokens` | Makes every access token issued so far invalid. Refresh tokens still work. |
| `/_control/failures` | Makes the next requests to an Oura endpoint fail, such as `{"endpoint": "/v2/usercollection/sleep", "status": 503, "count": 2}`. `count` is 1 when left out, and 0 clears it. |

`e2e/fakeoura_test.go` has helpers for these that new end-to-end tests can
reuse. `api-explore/fakeoura.http` shows each call; run
`KEEP_E2E_UP=1 scripts/e2e-test.sh` to keep the fake up to try them.

## Continuous integration

Every pull request runs three stages, each only if the previous one passes:
static analysis with the standards check, unit tests with the coverage gate,
then end-to-end tests. The unit test stage also tests the review tool, the
standards check and the deploy script, and the end-to-end stage the local
start and stop scripts.

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

### What the review is given, and what it must say

Gemini can read files but cannot run anything, so the review job works out
what it would otherwise guess. Before Gemini starts, `scripts/review-facts.sh`
runs the static analysis, the unit tests and `scripts/check-standards.sh` on
the pull request, and `gemini-review facts` writes how each ended, the
statement coverage of `internal/` by package, and the changed `internal/`
files whose package has no test change to `.gemini-review/facts.md`. The
prompt tells Gemini to cite those figures for the coverage and
test-driven rules rather than estimate them. A failing stage does not stop
the review: it is a fact for it. The end-to-end tests are left to CI.

Every rule in `GEMINI.md` has a stable ID, such as `constant-time-compare`.
Besides testing, structure and security basics, the rules cover where this
service is most likely to go wrong: contexts, outbound HTTP, goroutines,
error handling, how Oura's failures reach the MCP client, and keeping Oura's
tokens and health data private and its single-use refresh token safe. Rules
about code that does not exist yet, such as token storage, are judged not
applicable until a change touches it.

Gemini must give each rule a verdict (pass, fail, concern or not
applicable) with a one-line reason; a reply that leaves a rule out fails the
run and posts nothing. Rules marked "Checked by `scripts/check-standards.sh`" are
mechanical: the script decides them, the same way in CI and in the review,
and its findings are posted as line comments with the review. The review
ends with the checklist as a table, and says "No issues found." only when no
rule failed and nothing was commented on.

A problem Gemini finds plausible but cannot prove is raised as a
**concern** rather than dropped: a comment labelled `[CONCERN]` and
"Not confirmed", or a rule's verdict "concern, not confirmed". Confirmed
comments carry a severity of critical, high, medium or low, and a breach of
a security rule is critical or high. A review whose only findings are
concerns opens with "No confirmed issues." so a doubt is not read as a
defect.

### Proving the review catches defects

`.github/workflows/gemini-review-eval.yml` reviews each case in
`tools/gemini-review/seeded` with the same routing, models, CLI and prompt as
a pull request, posts nothing, and fails if the review missed what the case
expects: the broken rule failed, a confirmed comment on the lines that break
it (a concern does not count, and a case may ask for a least severity, such
as high for a leaked token), the computed coverage cited where the case is
about coverage, and no comment, concern or failed rule on the clean cases.
There is a case for each rule Gemini judges that the stories name, and two
clean cases: a documentation change, and an Oura client change that follows
every rule. Each job's summary shows the review it got.

It spends Gemini API calls, so it runs by hand. Run it **after changing the
review prompt (`tools/gemini-review/prompt.go`), the routing or model IDs in
`.github/gemini-review.json`, or the Gemini CLI pin in
`.github/actions/gemini-review`**: from the Actions tab, pick "Gemini review
seeded defects", choose the branch with the change, and run it, optionally
naming one case. GitHub offers the button only once the workflow is on
`main`.

A case is a folder with `case.json` (the title and description Gemini sees,
the rule it breaks, the file and lines a comment must land on, and
optionally `min_severity`, the least severity that comment may have) and
`change.diff`, a patch against `main`. `scripts/tools-test.sh` fails when a
case names a rule `GEMINI.md` no longer has, expects lines its change does
not show, or has a patch that no longer applies; regenerate the patch when
you change the code it touches.

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

The server answers `GET /health`, serves MCP at `/mcp` with a placeholder
`ping` tool, and requires the API key on everything except the health
check. `scripts/deploy.sh` deploys it to Cloud Run. The end-to-end
environment runs it beside a fake Oura API. `internal/oura` reads and
summarizes Oura's sleep data; the token storage and the tool that exposes the
summary arrive in later pull requests; see the KAN project in Jira.
