#!/usr/bin/env bash
# Runs the server locally with the settings in .env (see .env.example).
# Variables already set in your shell win over .env.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command go "https://go.dev/doc/install"

load_env_file

if [ -z "${CONNECTOR_API_KEY:-}" ]; then
    printf 'CONNECTOR_API_KEY is not set.\n  Run: cp .env.example .env   then edit .env\n' >&2
    exit 1
fi

step "Starting the server on port ${PORT:-8080} (Ctrl-C to stop)"
exec go run ./cmd/server
