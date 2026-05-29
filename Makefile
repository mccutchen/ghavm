# Built binaries will be placed here
DIST_PATH  	  ?= dist

# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.txt
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race

# 3rd party tools
GOFUMPT     := go run mvdan.cc/gofumpt@v0.9.2
GORELEASER  := go run github.com/goreleaser/goreleaser/v2@v2.15.2
REFLEX      := go run github.com/cespare/reflex@v0.3.2
REVIVE      := go run github.com/mgechev/revive@v1.15.0
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@2026.1


# =============================================================================
# build
# =============================================================================
build:
	mkdir -p $(DIST_PATH)
	GIT_COMMIT=$$(git describe --always --dirty 2>/dev/null || echo "unknown"); \
	BUILD_DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
	CGO_ENABLED=0; \
	go build -ldflags="-s -w -X main.commit=$$GIT_COMMIT -X main.buildDate=$$BUILD_DATE" -o $(DIST_PATH)/ghavm .
.PHONY: build

clean:
	rm -rf $(DIST_PATH) $(COVERAGE_PATH)
.PHONY: clean


# =============================================================================
# run locally (shortcut to quickly run against test data)
# =============================================================================
run: build
	$(DIST_PATH)/ghavm list ./testdata/workflows
.PHONY: run


# =============================================================================
# test
# =============================================================================
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
.PHONY: test-reset-golden-fixtures


# ===========================================================================
# linting/formatting
# ===========================================================================
lint:
	$(GOFUMPT) -d .
	go vet ./...
	$(REVIVE) -set_exit_status ./...
	$(STATICCHECK) ./...
.PHONY: lint

fmt:
	$(GOFUMPT) -w .
.PHONY: fmt


# ===========================================================================
# Release
# ===========================================================================
#
# Note: Releases are built automatically via the release.yaml GitHub Actions
# workflow when a new release is create via the GitHub UI.
#
# The release target requires valid values for these env vars:
#
#   QUILL_SIGN_P12
#   QUILL_SIGN_PASSWORD
#   QUILL_NOTARY_ISSUER
#   QUILL_NOTARY_KEY_ID
#   QUILL_NOTARY_KEY
#
# See quill's usage docs[1] and goreleaser's macOS notarization docs[2] for
# more info about these values and how to generate them.
#
# [1]: https://github.com/anchore/quill/blob/main/README.md#usage
# [2]: https://goreleaser.com/customization/notarize/
# ===========================================================================
release: clean
	$(GORELEASER) release --clean --verbose
.PHONY: release

release-dry-run: clean
	$(GORELEASER) release --clean --verbose --snapshot
.PHONY: release-dry-run
