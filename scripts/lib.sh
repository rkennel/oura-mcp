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

# require_cargo_subcommand <subcommand> <crate that provides it>
require_cargo_subcommand() {
    if ! cargo "$1" --version >/dev/null 2>&1; then
        printf 'Missing required tool: cargo %s\n  Install with: cargo install %s --locked\n' "$1" "$2" >&2
        exit 1
    fi
}
