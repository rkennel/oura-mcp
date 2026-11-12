#!/usr/bin/env bash
# Stage 1 of CI: formatting, vet and module tidiness.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

require_command go "https://go.dev/doc/install"

step "Formatting (gofmt)"
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
    printf 'These files need gofmt:\n%s\n' "$unformatted" >&2
    exit 1
fi

# The e2e tag includes the end-to-end tests, which are otherwise not compiled.
step "Suspicious constructs (go vet)"
go vet -tags e2e ./...

step "go.mod and go.sum are tidy"
go mod tidy -diff

step "Static analysis passed"
