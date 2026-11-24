#!/usr/bin/env bash
# Tests scripts/deploy.sh without Google Cloud or Docker. It runs the script
# in a throwaway Git repository with stand-ins for gcloud, docker and curl
# first on PATH: each records how it was called and answers as a scenario
# sets it up. The checks cover what the deploy must do (the image, the push,
# the Cloud Run flags) and what it must refuse.

# The stand-ins' bodies are single-quoted on purpose: they expand when a
# stand-in runs, not when it is written.
# shellcheck disable=SC2016

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command git "https://git-scm.com/downloads"

# Overrides a developer may have set for real deploys would change what is
# being tested.
unset GCP_PROJECT GCP_REGION SERVICE_NAME ARTIFACT_REPOSITORY RUNTIME_SERVICE_ACCOUNT API_KEY_SECRET

WORK="$(mktemp -d)"
COPY="$WORK/repo"
STUBS="$WORK/stubs"
OUTPUT="$WORK/output"
export STUB_CALLS="$WORK/calls"
failures=0

cleanup() {
    local status=$?
    rm -rf "$WORK"
    exit "$status"
}
trap cleanup EXIT

SERVICE_URL="https://oura-mcp-test.a.run.app"
REGISTRY="us-central1-docker.pkg.dev/kennelfitness/oura-mcp/server"

# write_stub <name> <body> writes an executable stand-in that records its
# arguments in $STUB_CALLS, then runs <body>.
write_stub() {
    printf '#!/usr/bin/env bash\nprintf "%%s %%s\\n" %s "$*" >>"$STUB_CALLS"\n%s\n' "$1" "$2" >"$STUBS/$1"
    chmod +x "$STUBS/$1"
}

make_stubs() {
    mkdir -p "$STUBS"
    write_stub gcloud '
case "$*" in
"auth list"*) [ -z "${STUB_GCLOUD_ACCOUNT:-}" ] || printf "%s\n" "$STUB_GCLOUD_ACCOUNT" ;;
"run services describe"*) printf "%s\n" "$STUB_SERVICE_URL" ;;
esac'
    write_stub docker '[ "$1" = push ] && [ -n "${STUB_PUSH_FAILS:-}" ] && exit 1; exit 0'
    write_stub curl '
for url in "$@"; do :; done
case "$url" in
*/health) printf "%s" "$STUB_HEALTH_STATUS" ;;
*/mcp) printf "%s" "$STUB_MCP_STATUS" ;;
*) printf "000" ;;
esac'
}

# make_repository makes a committed Git repository holding the scripts, so
# the deploy sees a clean tree and a commit to tag the image with.
make_repository() {
    mkdir -p "$COPY"
    cp -R scripts "$COPY/"
    git -C "$COPY" init -q
    git -C "$COPY" add .
    git -C "$COPY" -c user.name=deploy-test -c user.email=deploy-test@example.invalid \
        commit -q -m "deploy test"
    COMMIT="$(git -C "$COPY" rev-parse --short=12 HEAD)"
}

# given_a_working_cloud resets the stand-ins to a logged-in gcloud and a
# deployed service that answers as it should.
given_a_working_cloud() {
    export STUB_GCLOUD_ACCOUNT="deployer@example.invalid"
    export STUB_SERVICE_URL="$SERVICE_URL"
    export STUB_HEALTH_STATUS=200
    export STUB_MCP_STATUS=401
    unset STUB_PUSH_FAILS
    : >"$STUB_CALLS"
}

# run_deploy [arguments...] runs the deploy in the copy, keeping its combined
# output in $OUTPUT and its exit status in $status.
run_deploy() {
    status=0
    PATH="$STUBS:$PATH" "$COPY/scripts/deploy.sh" "$@" >"$OUTPUT" 2>&1 || status=$?
}

fail() {
    printf '  FAIL: %s\n' "$*" >&2
    printf '  Output was:\n' >&2
    sed 's/^/    /' "$OUTPUT" >&2
    printf '  Calls were:\n' >&2
    sed 's/^/    /' "$STUB_CALLS" >&2
    failures=$((failures + 1))
}

expect_status() {
    [ "$status" -eq "$1" ] || fail "exit status $status, want $1"
}

expect_output() {
    grep -qF -- "$1" "$OUTPUT" || fail "output does not mention: $1"
}

expect_call() {
    grep -qF -- "$1" "$STUB_CALLS" || fail "no call included: $1"
}

expect_no_call() {
    if grep -qF -- "$1" "$STUB_CALLS"; then fail "unexpected call: $1"; fi
}

scenario() {
    step "Scenario: $*"
}

make_stubs
make_repository

scenario "clean tree and working cloud: builds, pushes and deploys the commit"
given_a_working_cloud
run_deploy
expect_status 0
expect_call "docker build --platform linux/amd64 --build-arg VERSION=$COMMIT --tag $REGISTRY:$COMMIT ."
expect_call "docker push $REGISTRY:$COMMIT"
expect_call "gcloud run deploy oura-mcp --project=kennelfitness --region=us-central1 --image=$REGISTRY:$COMMIT"
expect_call "--service-account=oura-mcp-runtime@kennelfitness.iam.gserviceaccount.com"
expect_call "--set-secrets=CONNECTOR_API_KEY=connector-api-key:latest"
expect_call "--max-instances=1 --ingress=all --allow-unauthenticated"
expect_output "Connector URL: $SERVICE_URL/mcp"

scenario "after deploying: checks /health and that /mcp refuses a request without the key"
expect_call "curl -sS -o /dev/null -w %{http_code} --max-time 30 $SERVICE_URL/health"
expect_call "$SERVICE_URL/mcp"
if grep -F -- "$SERVICE_URL/mcp" "$STUB_CALLS" | grep -qi "x-api-key"; then
    fail "the unauthenticated check sent an x-api-key header"
fi

scenario "settings overridden in the shell: the deploy uses them"
given_a_working_cloud
GCP_PROJECT=sandbox GCP_REGION=europe-west1 SERVICE_NAME=svc ARTIFACT_REPOSITORY=repo \
    RUNTIME_SERVICE_ACCOUNT=runner API_KEY_SECRET=key run_deploy
expect_status 0
expect_call "docker push europe-west1-docker.pkg.dev/sandbox/repo/server:$COMMIT"
expect_call "gcloud run deploy svc --project=sandbox --region=europe-west1"
expect_call "--service-account=runner@sandbox.iam.gserviceaccount.com"
expect_call "--set-secrets=CONNECTOR_API_KEY=key:latest"

scenario "--dry-run: prints the commands and runs none of them"
given_a_working_cloud
run_deploy --dry-run
expect_status 0
expect_output "+ docker build --platform linux/amd64"
expect_output "+ docker push $REGISTRY:$COMMIT"
expect_output "+ gcloud run deploy oura-mcp"
expect_output "Dry run"
[ ! -s "$STUB_CALLS" ] || fail "a dry run called gcloud, docker or curl"

scenario "unknown argument: prints usage and does nothing"
given_a_working_cloud
run_deploy --force
expect_status 2
expect_output "Usage: scripts/deploy.sh [--dry-run]"
[ ! -s "$STUB_CALLS" ] || fail "an unknown argument still called gcloud, docker or curl"

scenario "uncommitted changes: refuses before building"
given_a_working_cloud
printf 'change\n' >"$COPY/uncommitted.txt"
run_deploy
expect_status 1
expect_output "uncommitted changes"
expect_no_call "docker"
expect_no_call "gcloud run deploy"
rm "$COPY/uncommitted.txt"

scenario "gcloud not logged in: refuses before building and says how to log in"
given_a_working_cloud
export STUB_GCLOUD_ACCOUNT=""
run_deploy
expect_status 1
expect_output "gcloud auth login"
expect_no_call "docker"

scenario "push refused: stops before deploying and says how to let Docker push"
given_a_working_cloud
export STUB_PUSH_FAILS=1
run_deploy
expect_status 1
expect_output "gcloud auth configure-docker us-central1-docker.pkg.dev"
expect_no_call "gcloud run deploy"

scenario "deployed service unhealthy: fails and points to the logs"
given_a_working_cloud
export STUB_HEALTH_STATUS=503
run_deploy
expect_status 1
expect_output "GET /health answered 503, want 200"
expect_output "gcloud run services logs read oura-mcp"

scenario "deployed service answers without the key: fails"
given_a_working_cloud
export STUB_MCP_STATUS=200
run_deploy
expect_status 1
expect_output "POST /mcp without the API key answered 200, want 401"

scenario "Cloud Run reports no URL: fails"
given_a_working_cloud
export STUB_SERVICE_URL=""
run_deploy
expect_status 1
expect_output "did not report a URL"

if [ "$failures" -ne 0 ]; then
    printf '\n%d check(s) failed\n' "$failures" >&2
    exit 1
fi
step "Deploy script tests passed"
