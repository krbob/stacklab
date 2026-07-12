SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := check
.NOTPARALLEL:

GO_PACKAGES := ./cmd/... ./internal/...
GO_FORMAT_PATHS := cmd internal

.PHONY: check check-toolchain check-toolchain-go check-toolchain-node
.PHONY: check-backend check-backend-test check-backend-coverage check-backend-hygiene
.PHONY: backend-test backend-coverage backend-hygiene frontend-dependencies frontend-api-contract frontend-checks docs-checks release-script-tests hygiene-checks
.PHONY: check-frontend check-docs check-hygiene

check: check-toolchain backend-test backend-hygiene frontend-dependencies frontend-checks hygiene-checks

check-toolchain:
	@echo "==> Toolchain"
	@./scripts/quality/check-toolchain.sh

check-toolchain-go:
	@echo "==> Go toolchain"
	@./scripts/quality/check-toolchain.sh go

check-toolchain-node:
	@echo "==> Node toolchain"
	@./scripts/quality/check-toolchain.sh node

check-backend: check-toolchain-go backend-test backend-hygiene

check-backend-test: check-toolchain-go backend-test

backend-test:
	@echo "==> Backend tests"
	@go test $(GO_PACKAGES)

COVERAGE_DIR ?= coverage

check-backend-coverage: check-toolchain-go backend-coverage

backend-coverage:
	@echo "==> Backend tests and package coverage"
	@./scripts/quality/check-go-coverage.sh "$(COVERAGE_DIR)"

check-backend-hygiene: check-toolchain-go backend-hygiene

backend-hygiene:
	@echo "==> Backend formatting and vet"
	@unformatted="$$(gofmt -l $(GO_FORMAT_PATHS))"; \
	if [[ -n "$${unformatted}" ]]; then \
	  echo "Unformatted Go files:" >&2; \
	  echo "$${unformatted}" >&2; \
	  exit 1; \
	fi
	@go vet $(GO_PACKAGES)

frontend-dependencies:
	@echo "==> Frontend dependencies"
	@npm --prefix frontend ci

check-frontend: check-toolchain-node frontend-dependencies frontend-checks

frontend-api-contract:
	@echo "==> Frontend API contract"
	@npm --prefix frontend run generate
	@git diff --exit-code -- frontend/src/lib/api-contract.generated.ts

frontend-checks: frontend-api-contract
	@echo "==> Frontend tests"
	@npm --prefix frontend test
	@echo "==> Frontend typecheck"
	@npm --prefix frontend run typecheck
	@echo "==> Frontend build"
	@npm --prefix frontend run build

check-docs: check-toolchain-node docs-checks

docs-checks:
	@echo "==> Documentation"
	@node --test scripts/quality/check-docs.test.mjs
	@node scripts/quality/check-docs.mjs

release-script-tests:
	@echo "==> Release scripts"
	@scripts/release/generate-release-notes.test.sh

check-hygiene: check-toolchain frontend-dependencies hygiene-checks

hygiene-checks: docs-checks release-script-tests
	@./scripts/quality/check-repository-hygiene.sh
