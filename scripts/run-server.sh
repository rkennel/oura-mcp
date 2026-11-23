#!/usr/bin/env bash
# Runs the server locally in the foreground with the settings in .env (see
# .env.example). Variables already set in your shell win over .env.
# To run it in the background instead, use scripts/start-server.sh.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command go "https://go.dev/doc/install"

load_env_file
require_api_key

step "Starting the server on port ${PORT:-8080} (Ctrl-C to stop)"
exec go run ./cmd/server
