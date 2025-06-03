# Default flags used by the test, testci, testcover targets
COVERAGE_PATH ?= coverage.out
COVERAGE_ARGS ?= -covermode=atomic -coverprofile=$(COVERAGE_PATH)
TEST_ARGS     ?= -race -count=1 -timeout=60s
DOCS_PORT     ?= :8080

# 3rd party tools
CMD_GOFUMPT     := go run mvdan.cc/gofumpt@v0.8.0
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
	@echo "=================================================================="
	@echo "cleaning old data, recreating directory structure"
	@echo "=================================================================="
	rm -rf testdata/golden
	mkdir -p testdata/golden testdata/golden/cmd-pin.outdir
	mkdir -p testdata/golden testdata/golden/cmd-upgrade-{default,compat,latest}.outdir
	for d in testdata/golden/*.outdir; do cp -r testdata/workflows/*.y*ml $$d; done
	@echo ""
	@echo "=================================================================="
	@echo 'regenerating golden files for `ghavm list`'
	@echo "=================================================================="
	$(OUT_DIR)/ghavm list --workers=1 --color=never  testdata/workflows/ >testdata/golden/cmd-list-plain.stdout 2>testdata/golden/cmd-list-plain.stderr 
	$(OUT_DIR)/ghavm list --workers=1 --color=always testdata/workflows/ >testdata/golden/cmd-list-color.stdout 2>testdata/golden/cmd-list-color.stderr
	@echo ""
	@echo "=================================================================="
	@echo 'regenerating golden file for `ghavm pin`'
	@echo "=================================================================="
	$(OUT_DIR)/ghavm pin     testdata/golden/cmd-pin.outdir/
	@echo ""
	@echo "=================================================================="
	@echo 'regenerating golden file for `ghavm upgrade`'
	@echo "=================================================================="
	$(OUT_DIR)/ghavm upgrade testdata/golden/cmd-upgrade-default.outdir/
	@echo ""
	@echo "=================================================================="
	@echo 'regenerating golden file for `ghavm upgrade --mode=compat`'
	@echo "=================================================================="
	$(OUT_DIR)/ghavm upgrade testdata/golden/cmd-upgrade-compat.outdir/ --mode=compat
	@echo ""
	@echo "=================================================================="
	@echo 'regenerating golden file for `ghavm upgrade --mode=latest`'
	@echo "=================================================================="
	$(OUT_DIR)/ghavm upgrade testdata/golden/cmd-upgrade-latest.outdir/ --mode=latest


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
