# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.out
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race -count=1 -timeout=60s
DOCS_PORT     ?= :8080

# 3rd party tools
CMD_GOFUMPT     := go run mvdan.cc/gofumpt@v0.8.0
CMD_GORELEASER  := go run github.com/goreleaser/goreleaser/v2@v2.11.0
CMD_PKGSITE     := go run golang.org/x/pkgsite/cmd/pkgsite@latest
CMD_REVIVE      := go run github.com/mgechev/revive@v1.9.0
CMD_STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@2025.1.1

# Where built assets will be placed
OUT_DIR  ?= out
DIST_DIR ?= dist


# =============================================================================
# Build
# =============================================================================
build:
	mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(OUT_DIR)/ghavm .
.PHONY: build

clean:
	rm -rf $(OUT_DIR) $(DIST_DIR) $(COVERAGE_PATH)
.PHONY: clean


# =============================================================================
# Run (shortcut to quickly run against test data)
# =============================================================================
run: build
	$(OUT_DIR)/ghavm list ./testdata/workflows
.PHONY: run


# ===========================================================================
# Tests
# ===========================================================================
test:
	go test $(TEST_ARGS) ./...
.PHONY: test

# Test command to run for continuous integration, which includes code coverage
# based on codecov.io's documentation:
# https://github.com/codecov/example-go/blob/b85638743b972bd0bd2af63421fe513c6f968930/README.md
testci:
	go test $(TEST_ARGS) $(COVERAGE_ARGS) ./...
.PHONY: testci

testcover: testci
	go tool cover -html=$(COVERAGE_PATH)
.PHONY: testcover

test-reset-golden-fixtures: build
	PATH="$(shell readlink -f $(OUT_DIR)):$$PATH" ./testdata/bin/reset-golden-fixtures


# ===========================================================================
# Linting/formatting
# ===========================================================================
lint:
	test -z "$$($(CMD_GOFUMPT) -d -e .)" || (echo "Error: gofmt failed"; $(CMD_GOFUMPT) -d -e . ; exit 1)
	go vet ./...
	$(CMD_REVIVE) -set_exit_status ./...
	$(CMD_STATICCHECK) ./...
.PHONY: lint

fmt:
	$(CMD_GOFUMPT) -d -e -w .
.PHONY: fmt


docs:
	$(CMD_PKGSITE) -http $(DOCS_PORT)


# ===========================================================================
# Release
#
# Note: releases are built automatically via the release.yaml GitHub Actions
# workflow when a new release is create via the GitHub UI.
# ===========================================================================
release-dry-run: clean
	$(CMD_GORELEASER) release --snapshot --clean
.PHONY: release-dry-run

release: clean
	$(CMD_GORELEASER) release --clean
.PHONY: release
