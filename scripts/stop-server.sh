#!/usr/bin/env bash
# Stops the server that scripts/start-server.sh started in the background.
# It is asked to shut down gracefully, which gives in-flight requests up to
# 8 seconds to finish. Running it when no server is running is not an error.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# The script waits for the process to exit this many times, 0.2 seconds
# apart: longer than the server's own 8-second shutdown deadline.
EXIT_TRIES=50

if ! server="$(running_server)"; then
    printf 'No server is running.\n'
    exit 0
fi
read -r pid _ <<<"$server"

step "Stopping the server (PID ${pid})"
kill -TERM "$pid" 2>/dev/null || true
for ((try = 0; try < EXIT_TRIES; try++)); do
    if ! process_alive "$pid"; then
        rm -f "$SERVER_PID_FILE"
        printf 'Server stopped.\n'
        exit 0
    fi
    sleep 0.2
done

kill -KILL "$pid" 2>/dev/null || true
rm -f "$SERVER_PID_FILE"
printf 'The server did not stop within 10 seconds, so it was killed.\n  Its log: %s\n' "$SERVER_LOG" >&2
exit 1
