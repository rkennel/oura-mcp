#!/usr/bin/env bash
# Tests scripts/start-server.sh and scripts/stop-server.sh against every
# acceptance criterion of KAN-18. They run in a throwaway copy of the
# repository, with their own .env, so your .env, bin/ and any server you are
# running are left alone. The server listens on TEST_PORT (default 18080).

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

TEST_PORT="${TEST_PORT:-18080}"
TEST_URL="http://localhost:${TEST_PORT}"

require_command go "https://go.dev/doc/install"
require_command curl "https://curl.se/download.html"

# The scripts read these from the shell before .env, so a developer's own
# settings would change what is being tested.
unset CONNECTOR_API_KEY PORT

WORK="$(mktemp -d)"
COPY="$WORK/repo"
OUTPUT="$WORK/output"
failures=0

cleanup() {
    local status=$?
    [ -x "$COPY/scripts/stop-server.sh" ] && "$COPY/scripts/stop-server.sh" >/dev/null 2>&1 || true
    rm -rf "$WORK"
    exit "$status"
}
trap cleanup EXIT

# copy_repository copies what the scripts need to build and run the server.
copy_repository() {
    mkdir -p "$COPY"
    cp -R scripts cmd internal go.mod .env.example "$COPY/"
    if [ -f go.sum ]; then cp go.sum "$COPY/"; fi
}

# run_script <name> runs scripts/<name> in the copy, keeping its combined
# output in $OUTPUT and its exit status in $status.
run_script() {
    status=0
    "$COPY/scripts/$1" >"$OUTPUT" 2>&1 || status=$?
}

fail() {
    printf '  FAIL: %s\n' "$*" >&2
    printf '  Output was:\n' >&2
    sed 's/^/    /' "$OUTPUT" >&2
    failures=$((failures + 1))
}

expect_status() {
    [ "$status" -eq "$1" ] || fail "exit status $status, want $1"
}

expect_output() {
    grep -qF -- "$1" "$OUTPUT" || fail "output does not mention: $1"
}

health_answers() {
    curl -fsS -o /dev/null --max-time 2 "$TEST_URL/health" 2>/dev/null
}

expect_health_answers() {
    health_answers || fail "GET /health does not answer on port $TEST_PORT"
}

expect_no_server() {
    if health_answers; then fail "a server answers on port $TEST_PORT"; fi
    if [ -e "$COPY/.run/server.pid" ]; then fail "the PID file was left behind"; fi
}

server_pid() {
    local pid=""
    if [ -f "$COPY/.run/server.pid" ]; then read -r pid _ <"$COPY/.run/server.pid" || true; fi
    printf '%s\n' "$pid"
}

write_env() {
    printf '%s\n' "$@" >"$COPY/.env"
}

scenario() {
    step "Scenario: $*"
}

copy_repository
if health_answers; then
    printf 'Port %s is already in use; set TEST_PORT to a free port.\n' "$TEST_PORT" >&2
    exit 1
fi

scenario "no .env and no key: start refuses and points to .env.example"
rm -f "$COPY/.env"
run_script start-server.sh
expect_status 1
expect_output ".env.example"
expect_no_server

scenario ".env without CONNECTOR_API_KEY: start refuses and points to .env.example"
write_env "PORT=$TEST_PORT" "CONNECTOR_API_KEY="
run_script start-server.sh
expect_status 1
expect_output ".env.example"
expect_no_server

scenario "nothing running: stop says so and succeeds"
run_script stop-server.sh
expect_status 0
expect_output "No server is running"

scenario "key set: start builds, starts in the background and reports URL and log"
write_env "PORT=$TEST_PORT" "CONNECTOR_API_KEY=server-scripts-test-key"
run_script start-server.sh
expect_status 0
expect_output "$TEST_URL"
expect_output "$COPY/.run/server.log"
expect_health_answers
[ -x "$COPY/bin/server" ] || fail "bin/server was not built"
first_pid="$(server_pid)"

scenario "already running: start says so and does not start a second copy"
run_script start-server.sh
expect_status 0
expect_output "already running"
expect_output "$TEST_URL"
[ "$(server_pid)" = "$first_pid" ] || fail "the PID changed, so a second server started"
expect_health_answers

scenario "port taken by another process: start refuses"
mv "$COPY/.run/server.pid" "$WORK/server.pid"
run_script start-server.sh
expect_status 1
expect_output "port $TEST_PORT"
[ ! -e "$COPY/.run/server.pid" ] || fail "a PID file was written for a server that did not start"
mv "$WORK/server.pid" "$COPY/.run/server.pid"

scenario "running: stop shuts the server down cleanly and confirms"
run_script stop-server.sh
expect_status 0
expect_output "Server stopped"
expect_no_server
grep -qF "server shutting down" "$COPY/.run/server.log" ||
    fail "the server log does not show a graceful shutdown"
if process_alive "$first_pid"; then fail "process $first_pid is still alive"; fi

scenario "stale PID file: stop treats it as nothing running and removes it"
printf '%s %s\n' "$first_pid" "$TEST_PORT" >"$COPY/.run/server.pid"
run_script stop-server.sh
expect_status 0
expect_output "No server is running"
expect_no_server

scenario "server exits during startup: start reports it and shows the log"
write_env "PORT=not-a-port" "CONNECTOR_API_KEY=server-scripts-test-key"
run_script start-server.sh
expect_status 1
expect_output "exited during startup"
expect_output "not a valid TCP port"
expect_no_server

if [ "$failures" -ne 0 ]; then
    printf '\n%d check(s) failed\n' "$failures" >&2
    exit 1
fi
step "Server script tests passed"
