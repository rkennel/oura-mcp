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
