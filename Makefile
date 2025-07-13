# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.out
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race -count=1 -timeout=60s
DOCS_PORT     ?= :8080

# 3rd party tools
CMD_GOFUMPT     := go run mvdan.cc/gofumpt@v0.8.0
CMD_GORELEASER  := go run github.com/goreleaser/goreleaser@latest
CMD_PKGSITE     := go run golang.org/x/pkgsite/cmd/pkgsite@latest
CMD_REVIVE      := go run github.com/mgechev/revive@v1.9.0
CMD_STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@2025.1.1

# Where built binaries will be placed
OUT_DIR         ?= out


# =============================================================================
# build
# =============================================================================
build:
	mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(OUT_DIR)/ghavm .
.PHONY: build

clean:
	rm -rf $(OUT_DIR) $(COVERAGE_PATH)
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
release-dry-run:
	$(CMD_GORELEASER) release --snapshot --clean
.PHONY: release-dry-run

release:
	$(CMD_GORELEASER) release --clean
.PHONY: release

# ===========================================================================
# Manual/break-glass release targets
# ===========================================================================

# Check if required environment variables are set for manual releases
check-manual-release-env:
	@echo "Checking environment variables for manual release..."
	@test -n "$(GITHUB_TOKEN)" || (echo "ERROR: GITHUB_TOKEN environment variable is required"; exit 1)
	@echo "✓ GITHUB_TOKEN is set"
	@docker info >/dev/null 2>&1 || (echo "ERROR: Docker is not running or accessible"; exit 1)
	@echo "✓ Docker is accessible"
.PHONY: check-manual-release-env

# Authenticate with Docker registries (run this first for manual releases)
docker-login:
	@echo "Logging in to Docker registries..."
	@echo "Please log in to Docker Hub when prompted:"
	@docker login
	@echo "Logging in to GitHub Container Registry..."
	@echo "$(GITHUB_TOKEN)" | docker login ghcr.io -u $(shell gh api user --jq .login 2>/dev/null || echo "$(USER)") --password-stdin
	@echo "✓ Successfully logged in to both registries"
.PHONY: docker-login

# Full manual release (use this if GitHub Actions is down)
# Usage: GITHUB_TOKEN=<token> make manual-release
manual-release: check-manual-release-env
	@echo "Starting manual release process..."
	@echo "⚠️  This will create and push a real release!"
	@echo "⚠️  Make sure you've already created the git tag and GitHub release!"
	@read -p "Are you sure you want to continue? [y/N] " confirm && [ "$$confirm" = "y" ]
	@echo "Running goreleaser..."
	$(CMD_GORELEASER) release --clean
	@echo "✓ Manual release completed successfully"
.PHONY: manual-release

# Snapshot release for testing (doesn't push to registries or create GitHub release)
manual-release-snapshot: check-manual-release-env
	@echo "Creating snapshot release for testing..."
	$(CMD_GORELEASER) release --snapshot --clean
	@echo "✓ Snapshot release completed - check the dist/ directory"
.PHONY: manual-release-snapshot

# Help target for manual release process
manual-release-help:
	@echo "Manual Release Process (Break-glass scenarios):"
	@echo ""
	@echo "1. Create git tag and GitHub release:"
	@echo "   git tag v1.2.3"
	@echo "   git push origin v1.2.3"
	@echo "   # Then create GitHub release via web UI or: gh release create v1.2.3"
	@echo ""
	@echo "2. Set environment variables:"
	@echo "   export GITHUB_TOKEN=<your-github-token>"
	@echo ""
	@echo "3. Authenticate with Docker registries:"
	@echo "   make docker-login"
	@echo ""
	@echo "4. Run the manual release:"
	@echo "   make manual-release"
	@echo ""
	@echo "Alternative: Test with snapshot (no push):"
	@echo "   make manual-release-snapshot"
.PHONY: manual-release-help
