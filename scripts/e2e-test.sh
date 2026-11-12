#!/usr/bin/env bash
# Stage 3 of CI: build the production image, run it, and run the end-to-end
# tests against it. The container is always stopped afterwards; set
# KEEP_E2E_UP=1 to leave it running for debugging.
# COMPOSE_FILE may add override files; CI uses it to enable layer caching.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

export COMPOSE_FILE="${COMPOSE_FILE:-e2e/compose.yaml}"
COMPOSE=(docker compose)

require_command go "https://go.dev/doc/install"
require_command docker "https://docs.docker.com/get-docker/"

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

step "End-to-end tests passed"
