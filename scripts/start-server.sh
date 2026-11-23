#!/usr/bin/env bash
# Builds the server and starts it in the background with the settings in
# .env (see .env.example), then waits until GET /health answers. Variables
# already set in your shell win over .env. Stop it with
# scripts/stop-server.sh. To run it in the foreground, use
# scripts/run-server.sh.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# The health check is tried this many times, 0.2 seconds apart.
HEALTH_TRIES=50

require_command go "https://go.dev/doc/install"
require_command curl "https://curl.se/download.html"

# port_answers <url> succeeds if anything answers HTTP there, whatever the
# status, so a port another program holds is caught before the build.
port_answers() {
    curl -s -o /dev/null --max-time 2 "$1" 2>/dev/null
}

healthy() {
    curl -fsS -o /dev/null --max-time 1 "$1/health" 2>/dev/null
}

# give_up <message> stops a server that did not come up, shows the end of
# its log and exits with an error.
give_up() {
    local pid=$1
    shift
    kill -TERM "$pid" 2>/dev/null || true
    rm -f "$SERVER_PID_FILE"
    printf '%s Last lines of %s:\n' "$*" "$SERVER_LOG" >&2
    tail -n 20 "$SERVER_LOG" >&2
    exit 1
}

# wait_until_healthy <pid> <url> returns once GET /health answers, while
# the process it started is still alive.
wait_until_healthy() {
    local pid=$1 url=$2 try
    for ((try = 0; try < HEALTH_TRIES; try++)); do
        process_alive "$pid" || give_up "$pid" "The server exited during startup."
        healthy "$url" && return 0
        sleep 0.2
    done
    give_up "$pid" "The server did not answer GET /health in time, so it was stopped."
}

if server="$(running_server)"; then
    read -r pid port <<<"$server"
    printf 'The server is already running (PID %s) at http://localhost:%s\n' "$pid" "$port"
    printf '  Stop it with scripts/stop-server.sh\n'
    exit 0
fi

load_env_file
require_api_key
PORT="${PORT:-8080}"
url="http://localhost:${PORT}"

if port_answers "$url"; then
    printf 'Something else is already answering on port %s, so the server cannot start there.\n' "$PORT" >&2
    printf '  Stop that program, or set PORT in .env to a free port.\n' >&2
    exit 1
fi

step "Building the server"
go build -o "$SERVER_BIN" ./cmd/server

step "Starting the server in the background on port ${PORT}"
mkdir -p "$RUN_DIR"
nohup "$SERVER_BIN" </dev/null >"$SERVER_LOG" 2>&1 &
pid=$!
printf '%s %s\n' "$pid" "$PORT" >"$SERVER_PID_FILE"
wait_until_healthy "$pid" "$url"

printf '\nThe server is running (PID %s) at %s\n' "$pid" "$url"
printf '  Log: %s\n' "$SERVER_LOG"
printf '  Stop it with scripts/stop-server.sh\n'
