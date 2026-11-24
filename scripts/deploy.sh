#!/usr/bin/env bash
# Builds the production image from the current commit, pushes it to Artifact
# Registry and deploys it to Cloud Run, then checks the deployed service:
# GET /health answers 200 and a request without the API key gets 401.
#
# Run the one-time setup in docs/gcp-setup.md first. The deploy uses your own
# gcloud login and Docker; the service reads CONNECTOR_API_KEY from Secret
# Manager, so the key never passes through this script.
#
# Usage: scripts/deploy.sh [--dry-run]
#   --dry-run  print the build, push and deploy commands without running them
#
# The settings below default to the kennelfitness project. Set any of them in
# your shell to override it for one run, such as
# GCP_PROJECT=my-sandbox scripts/deploy.sh.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

GCP_PROJECT="${GCP_PROJECT:-kennelfitness}"
GCP_REGION="${GCP_REGION:-us-central1}"
SERVICE_NAME="${SERVICE_NAME:-oura-mcp}"
ARTIFACT_REPOSITORY="${ARTIFACT_REPOSITORY:-oura-mcp}"
RUNTIME_SERVICE_ACCOUNT="${RUNTIME_SERVICE_ACCOUNT:-oura-mcp-runtime}"
API_KEY_SECRET="${API_KEY_SECRET:-connector-api-key}"

# Cloud Run runs x86-64 containers, so the image is built for that platform
# even on an Apple Silicon Mac.
IMAGE_PLATFORM="linux/amd64"

# A cold start can take a few seconds, so each check waits this long.
CHECK_TIMEOUT_SECONDS=30

usage() {
    printf 'Usage: scripts/deploy.sh [--dry-run]\n' >&2
}

dry_run=false
case "${1:-}" in
"") ;;
--dry-run) dry_run=true ;;
*)
    usage
    exit 2
    ;;
esac
if [ "$#" -gt 1 ]; then
    usage
    exit 2
fi

# run prints a command, then runs it unless this is a dry run.
run() {
    printf '+ %s\n' "$*"
    [ "$dry_run" = true ] || "$@"
}

# require_clean_tree stops the deploy when the working tree has changes, so
# the image tag always names the commit the image was built from.
require_clean_tree() {
    if [ -n "$(git status --porcelain)" ]; then
        printf 'The working tree has uncommitted changes.\n' >&2
        printf '  Commit or stash them, so the image matches its commit, then try again.\n' >&2
        exit 1
    fi
}

# require_gcloud_login stops the deploy when gcloud has no active account.
require_gcloud_login() {
    if [ -z "$(gcloud auth list --filter=status:ACTIVE --format='value(account)')" ]; then
        printf 'gcloud is not logged in.\n  Run: gcloud auth login\n' >&2
        exit 1
    fi
}

build_image() {
    step "Building ${image} for ${IMAGE_PLATFORM}"
    run docker build --platform "$IMAGE_PLATFORM" --build-arg "VERSION=${version}" --tag "$image" .
}

push_image() {
    step "Pushing ${image}"
    if ! run docker push "$image"; then
        printf 'The push failed. If Docker is not yet allowed to push to Artifact Registry, run:\n' >&2
        printf '  gcloud auth configure-docker %s-docker.pkg.dev\n' "$GCP_REGION" >&2
        exit 1
    fi
}

# deploy_service deploys with public ingress and no Cloud Run sign-in: Claude
# calls the service from the internet with only the x-api-key header, and
# the server itself rejects every request without the key except /health.
deploy_service() {
    step "Deploying ${SERVICE_NAME} to Cloud Run in ${GCP_REGION}"
    run gcloud run deploy "$SERVICE_NAME" \
        --project="$GCP_PROJECT" \
        --region="$GCP_REGION" \
        --image="$image" \
        --service-account="$service_account" \
        --set-secrets="CONNECTOR_API_KEY=${API_KEY_SECRET}:latest" \
        --max-instances=1 \
        --ingress=all \
        --allow-unauthenticated \
        --quiet
}

service_url() {
    gcloud run services describe "$SERVICE_NAME" \
        --project="$GCP_PROJECT" --region="$GCP_REGION" --format='value(status.url)'
}

# http_status <curl arguments...> prints the HTTP status of one request.
http_status() {
    curl -sS -o /dev/null -w '%{http_code}' --max-time "$CHECK_TIMEOUT_SECONDS" "$@" || true
}

# expect_status <want> <what> <curl arguments...> fails the deploy when the
# deployed service answers anything but <want>.
expect_status() {
    local want=$1 what=$2 got
    shift 2
    got="$(http_status "$@")"
    if [ "$got" != "$want" ]; then
        printf '%s answered %s, want %s.\n' "$what" "${got:-no response}" "$want" >&2
        printf '  See the logs: gcloud run services logs read %s --project=%s --region=%s\n' \
            "$SERVICE_NAME" "$GCP_PROJECT" "$GCP_REGION" >&2
        exit 1
    fi
    printf '%s answered %s\n' "$what" "$got"
}

check_service() {
    local url=$1
    step "Checking the deployed service"
    expect_status 200 "GET /health" "$url/health"
    expect_status 401 "POST /mcp without the API key" -X POST \
        -H 'Content-Type: application/json' \
        -H 'Accept: application/json, text/event-stream' \
        --data '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
        "$url/mcp"
}

require_command git "https://git-scm.com/downloads"
require_command docker "https://docs.docker.com/get-docker/"
require_command gcloud "https://cloud.google.com/sdk/docs/install"
require_command curl "https://curl.se/download.html"

require_clean_tree
version="$(git rev-parse --short=12 HEAD)"
image="${GCP_REGION}-docker.pkg.dev/${GCP_PROJECT}/${ARTIFACT_REPOSITORY}/server:${version}"
service_account="${RUNTIME_SERVICE_ACCOUNT}@${GCP_PROJECT}.iam.gserviceaccount.com"

if [ "$dry_run" = false ]; then
    require_gcloud_login
fi

build_image
push_image
deploy_service

if [ "$dry_run" = true ]; then
    step "Dry run: nothing was built, pushed or deployed"
    exit 0
fi

url="$(service_url)"
if [ -z "$url" ]; then
    printf 'Cloud Run did not report a URL for %s.\n' "$SERVICE_NAME" >&2
    exit 1
fi
check_service "$url"

step "Deployed ${version}"
printf 'Service URL:   %s\n' "$url"
printf 'Connector URL: %s/mcp\n' "$url"
