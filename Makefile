BINARY    ?= aivm
STATE_DIR ?= ~/.aivm

INSTALL_DIR := /usr/local/bin
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_FLAGS := -ldflags="-s -w -X main.defaultStateDir=$(STATE_DIR) -X main.version=$(VERSION)"

# Test parallelism — number of concurrent test goroutines.
# Capped at 4 to avoid saturating the Colima VM during concurrent claude installs
# (curl | bash from claude.ai) and to keep Docker daemon pressure manageable.
# Override with: make test-e2e PARALLEL=8
PARALLEL ?= 4

# Test filter — run only tests matching this regex (default: all).
# Override with: make test-e2e RUN=TestIdle
RUN ?= .

.PHONY: build install install-test uninstall clean test test-unit test-e2e test-bootstrap fmt vet release-snapshot release-dry-run

build:
	go build $(BUILD_FLAGS) -o bin/$(BINARY) ./cmd/aivm

install: build
	@mkdir -p $(INSTALL_DIR)
	sudo cp bin/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo codesign --force --sign - $(INSTALL_DIR)/$(BINARY)
	@mkdir -p $(STATE_DIR)/logs $(STATE_DIR)/sessions $(STATE_DIR)/plugins
	@if [ ! -f $(STATE_DIR)/aivm.yaml ]; then \
	  cp aivm.example.yaml $(STATE_DIR)/aivm.yaml; \
	  echo "→ Edit $(STATE_DIR)/aivm.yaml and set auth.claude_token before running $(BINARY)"; \
	fi
	@echo "✓  $(BINARY) installed to $(INSTALL_DIR)/$(BINARY)"
	@echo "   state dir : $(STATE_DIR)"

install-test:
	go build $(BUILD_FLAGS) -o bin/aivm-test ./cmd/aivm
	@mkdir -p $(INSTALL_DIR)
	sudo cp bin/aivm-test $(INSTALL_DIR)/aivm-test
	@echo "✓  aivm-test installed to $(INSTALL_DIR)/aivm-test"

uninstall:
	sudo rm -f $(INSTALL_DIR)/$(BINARY)
	rm -rf $(STATE_DIR)/

clean:
	rm -rf bin/

test-unit:
	go test ./test/unit/...

test-e2e:
	@go build $(BUILD_FLAGS) -o bin/aivm-test ./cmd/aivm
	@PATH="$(CURDIR)/bin:$$PATH" go test -tags integration -v -timeout 60m -parallel $(PARALLEL) -run "$(RUN)" ./test/e2e/

test-bootstrap:
	@go build -tags bootstrap ./test/... 2>&1
	go test -tags bootstrap -v -timeout 120m -parallel $(PARALLEL) -run "$(RUN)" ./test/bootstrap/

fmt:
	go fmt ./...

vet:
	go vet ./...

release-snapshot:
	goreleaser release --snapshot --clean

release-dry-run:
	goreleaser release --skip=publish --clean
