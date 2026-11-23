# Shared helpers for the scripts in this directory. Source, do not execute.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

step() {
    printf '\n==> %s\n' "$*"
}

# require_command <command> <how to install it>
require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        printf 'Missing required tool: %s\n  Install with: %s\n' "$1" "$2" >&2
        exit 1
    fi
}

# load_env_file exports the KEY=value settings in .env at the repository
# root, if it exists, so local runs get the variables production gets from
# its environment. A variable already set wins over .env. The file is
# parsed, not executed, and its values are never printed.
load_env_file() {
    local file="$REPO_ROOT/.env" line key value number=0
    [ -f "$file" ] || return 0

    while IFS= read -r line || [ -n "$line" ]; do
        number=$((number + 1))
        line="${line%$'\r'}"
        if [[ "$line" =~ ^[[:space:]]*(#|$) ]]; then
            continue
        fi
        if ! [[ "$line" =~ ^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
            printf 'Ignoring .env line %d: not KEY=value\n' "$number" >&2
            continue
        fi
        key="${BASH_REMATCH[2]}"
        value="${BASH_REMATCH[3]}"
        if [[ "$value" =~ ^\"(.*)\"$ || "$value" =~ ^\'(.*)\'$ ]]; then
            value="${BASH_REMATCH[1]}"
        fi
        if [ -z "${!key+set}" ]; then
            export "$key=$value"
        fi
    done <"$file"
}

# require_api_key stops the script when CONNECTOR_API_KEY is set neither in
# the shell nor in .env, before anything is built or started. Call it after
# load_env_file.
require_api_key() {
    [ -n "${CONNECTOR_API_KEY:-}" ] && return 0
    if [ -f "$REPO_ROOT/.env" ]; then
        printf 'CONNECTOR_API_KEY is not set in .env.\n  Set it as .env.example shows, then try again.\n' >&2
    else
        printf 'There is no .env file, and CONNECTOR_API_KEY is not set.\n  Run: cp .env.example .env   then edit .env\n' >&2
    fi
    exit 1
}

# Where scripts/start-server.sh keeps the background server: its binary, a
# PID file holding "PID PORT", and its log. bin/ and .run/ are git-ignored.
SERVER_BIN="$REPO_ROOT/bin/server"
RUN_DIR="$REPO_ROOT/.run"
SERVER_PID_FILE="$RUN_DIR/server.pid"
SERVER_LOG="$RUN_DIR/server.log"

# running_server prints "PID PORT" for the background server and succeeds
# if it is still running. Otherwise it removes any stale PID file and fails.
# A PID counts only while its process is still bin/server, so a PID the
# system has since reused never gets an unrelated process signalled.
running_server() {
    local pid="" port="" command=""
    [ -f "$SERVER_PID_FILE" ] || return 1
    read -r pid port <"$SERVER_PID_FILE" || true
    if [[ "$pid" =~ ^[0-9]+$ ]]; then
        command="$(ps -p "$pid" -o args= 2>/dev/null | sed 's/[[:space:]]*$//' || true)"
    fi
    if [ -n "$command" ] && [ "$command" = "$SERVER_BIN" ]; then
        printf '%s %s\n' "$pid" "$port"
        return 0
    fi
    rm -f "$SERVER_PID_FILE"
    return 1
}

# process_alive <pid> succeeds while the process exists and has not exited.
# An exited child that has not been reaped yet shows as a zombie (Z).
process_alive() {
    local state
    state="$(ps -p "$1" -o stat= 2>/dev/null || true)"
    [ -n "$state" ] && [[ "$state" != Z* ]]
}
