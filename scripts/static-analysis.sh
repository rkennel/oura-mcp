#!/usr/bin/env bash
# Stage 1 of CI: formatting, vet, module tidiness, lint and a vulnerability
# scan.
#
# The linters are pinned here and run with `go run`, so local runs and CI use
# the same versions with nothing to install but Go. Each is built from its
# own module, with the dependency versions its authors released it with.

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

GOLANGCI_LINT_VERSION="v2.15.0"
GOVULNCHECK_VERSION="v1.10.0"

require_command go "https://go.dev/doc/install"

# Build the tools with this repository's Go toolchain. `go run pkg@version`
# would otherwise use the older toolchain the tool's own go.mod names, and
# golangci-lint refuses to lint code for a newer Go than it was built with.
GOTOOLCHAIN="$(go env GOVERSION)"
export GOTOOLCHAIN

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

# Configuration, including the e2e build tag, is in .golangci.yml.
step "Lint (golangci-lint ${GOLANGCI_LINT_VERSION})"
go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}" run ./...

# Reports only vulnerabilities in code this module actually calls, including
# the Go standard library of the toolchain in go.mod.
step "Known vulnerabilities (govulncheck ${GOVULNCHECK_VERSION})"
go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" -tags e2e ./...

step "Static analysis passed"
