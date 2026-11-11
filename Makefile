# One entry point for the checks CI runs, so local and CI behave the same.

COVERAGE_MIN := 85
COMPOSE := docker compose -f e2e/compose.yaml

.PHONY: fmt lint audit test coverage e2e-up e2e-test e2e-down e2e ci

fmt: ## Format all code
	cargo fmt --all

lint: ## Static analysis: formatting and clippy, warnings are errors
	cargo fmt --all --check
	cargo clippy -p oura-mcp-core -p oura-mcp-e2e --all-targets -- -D warnings
	cargo clippy -p oura-mcp-worker --target wasm32-unknown-unknown -- -D warnings

audit: ## Dependency advisories, licences and sources (needs cargo-deny)
	cargo deny check

test: ## Unit tests
	cargo test -p oura-mcp-core

coverage: ## Unit tests with the coverage gate (needs cargo-llvm-cov)
	cargo llvm-cov -p oura-mcp-core --fail-under-lines $(COVERAGE_MIN)

e2e-up: ## Build and start the Worker container
	$(COMPOSE) up --build --detach --wait

e2e-test: ## Run end-to-end tests against the running container
	cargo test -p oura-mcp-e2e

e2e-down: ## Stop the Worker container
	$(COMPOSE) down --volumes

e2e: e2e-up ## Full end-to-end cycle
	$(MAKE) e2e-test; status=$$?; $(MAKE) e2e-down; exit $$status

ci: lint audit coverage e2e ## Everything CI runs, in order
