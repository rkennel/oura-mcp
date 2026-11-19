#!/usr/bin/env bash
# Stage 3 of CI: build the production image, run it, and run the end-to-end
# tests against it. The container is always stopped afterwards; set
# KEEP_E2E_UP=1 to leave it running for debugging.
# COMPOSE_FILE may add override files; CI uses it to enable layer caching.
# The server uses CONNECTOR_API_KEY from your shell or .env (see
# .env.example), or a throwaway key generated for this run when neither sets
# one. Choose it yourself to call a server you keep up with KEEP_E2E_UP.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

export COMPOSE_FILE="${COMPOSE_FILE:-e2e/compose.yaml}"
COMPOSE=(docker compose)

require_command go "https://go.dev/doc/install"
require_command docker "https://docs.docker.com/get-docker/"

load_env_file
if [ -z "${CONNECTOR_API_KEY:-}" ]; then
    CONNECTOR_API_KEY="e2e-$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
fi
export CONNECTOR_API_KEY

finish() {
    local status=$?
    if [ "$status" -ne 0 ]; then
        step "End-to-end stage failed; container logs follow"
        "${COMPOSE[@]}" logs || true
    fi
    if [ -z "${KEEP_E2E_UP:-}" ]; then
        step "Stopping the end-to-end environment"
        "${COMPOSE[@]}" down --volumes
    fi
    exit "$status"
}
trap finish EXIT

step "Building and starting the end-to-end environment"
"${COMPOSE[@]}" up --build --detach

# -count=1 disables the test cache: these tests depend on a running server,
# which Go cannot see has changed.
step "End-to-end tests"
go test -tags e2e -count=1 ./e2e/...

# The tests above sent the key, wrong keys and no key. The server must not
# have written the key to its logs. The logs are read in full before the
# search, because pipefail would turn grep -q's early exit into a failure.
step "The server never logs the API key"
server_logs="$("${COMPOSE[@]}" logs --no-log-prefix)"
if grep -qF -- "$CONNECTOR_API_KEY" <<<"$server_logs"; then
    printf 'The server logs contain CONNECTOR_API_KEY\n' >&2
    exit 1
fi

# refuses_to_start_without_key starts a second server container with an
# empty CONNECTOR_API_KEY and checks that it exits with an error naming the
# variable. If it keeps running instead, it is removed after a few seconds
# rather than left to hang the script.
refuses_to_start_without_key() {
    local container running exit_code output
    container="$("${COMPOSE[@]}" run --detach --no-deps --env CONNECTOR_API_KEY= server)"
    for _ in 1 2 3 4 5 6 7 8 9 10; do
        running="$(docker inspect --format '{{.State.Running}}' "$container")"
        [ "$running" = false ] && break
        sleep 1
    done
    exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container")"
    output="$(docker logs "$container" 2>&1)"
    docker rm --force "$container" >/dev/null

    if [ "$running" != false ] || [ "$exit_code" -eq 0 ]; then
        printf 'The server did not refuse to start without CONNECTOR_API_KEY:\n%s\n' "$output" >&2
        return 1
    fi
    if ! grep -qF "CONNECTOR_API_KEY is not set" <<<"$output"; then
        printf 'The server stopped without naming CONNECTOR_API_KEY:\n%s\n' "$output" >&2
        return 1
    fi
}

step "The server refuses to start without CONNECTOR_API_KEY"
refuses_to_start_without_key

step "End-to-end tests passed"
