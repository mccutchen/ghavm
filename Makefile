# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.out
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race -count=1 -timeout=60s
DOCS_PORT     ?= :8080

# 3rd party tools
CMD_GOFUMPT     := go run mvdan.cc/gofumpt@v0.8.0
CMD_GORELEASER  := go run github.com/goreleaser/goreleaser@v2.11.0
CMD_PKGSITE     := go run golang.org/x/pkgsite/cmd/pkgsite@latest
CMD_REVIVE      := go run github.com/mgechev/revive@v1.9.0
CMD_STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@2025.1.1

# Where built assets will be placed
OUT_DIR  ?= out  # make build
DIST_DIR ?= dist # make release


# =============================================================================
# build
# =============================================================================
build:
	mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(OUT_DIR)/ghavm .
.PHONY: build

clean:
	rm -rf $(OUT_DIR) $(DIST_DIR) $(COVERAGE_PATH)
.PHONY: clean


# =============================================================================
# run against test data
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
# Goreleaser
# ===========================================================================
release-dry-run: clean
	$(CMD_GORELEASER) release --snapshot --clean
.PHONY: release-dry-run

release: clean
	$(CMD_GORELEASER) release --clean
.PHONY: release

# ===========================================================================
# Manual/break-glass release targets
# ===========================================================================

# Full manual release (use this if GitHub Actions is down)
# Usage: GITHUB_TOKEN=<token> make manual-release
manual-release:
	./bin/manual-release
.PHONY: manual-release

# Snapshot release for testing (doesn't push to registries or create GitHub release)
manual-release-snapshot:
	./bin/manual-release --snapshot
.PHONY: manual-release-snapshot

# Help target for manual release process
manual-release-help:
	@echo "Manual Release Process (Break-glass scenarios):"
	@echo ""
	@echo "The manual release process has been moved to a shell script for better"
	@echo "maintainability. The script handles all checks, authentication, and"
	@echo "release steps automatically."
	@echo ""
	@echo "Usage:"
	@echo "  # For a real release (pushes to registries and GitHub):"
	@echo "  GITHUB_TOKEN=<token> make manual-release"
	@echo ""
	@echo "  # For testing (local build only, no push):"
	@echo "  GITHUB_TOKEN=<token> make manual-release-snapshot"
	@echo ""
	@echo "  # Or run the script directly:"
	@echo "  GITHUB_TOKEN=<token> ./bin/manual-release [--snapshot]"
	@echo ""
	@echo "The script will:"
	@echo "  • Check all prerequisites (git tag, environment, Docker auth)"
	@echo "  • Run goreleaser to build binaries and Docker images"
	@echo "  • Sign all Docker images and manifests with cosign"
	@echo "  • Provide clear status and error messages"
.PHONY: manual-release-help
