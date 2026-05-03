BINARY    ?= aivm
STATE_DIR ?= ~/.aivm

INSTALL_DIR := /usr/local/bin
BUILD_FLAGS := -ldflags="-s -w \
  -X main.defaultStateDir=$(STATE_DIR)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build install uninstall clean test test-integration build-test-image fmt vet

build:
	go build $(BUILD_FLAGS) -o bin/$(BINARY) ./cmd/aivm

install: build
	@mkdir -p $(INSTALL_DIR)
	sudo cp bin/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo codesign --force --sign - $(INSTALL_DIR)/$(BINARY)
	@mkdir -p $(STATE_DIR)/logs $(STATE_DIR)/sessions $(STATE_DIR)/mcpjungle-data $(STATE_DIR)/plugins
	@if [ ! -f $(STATE_DIR)/aivm.yaml ]; then \
	  cp aivm.example.yaml $(STATE_DIR)/aivm.yaml; \
	  echo "→ Edit $(STATE_DIR)/aivm.yaml and set auth.claude_token before running $(BINARY)"; \
	fi
	@echo "✓  $(BINARY) installed to $(INSTALL_DIR)/$(BINARY)"
	@echo "   state dir : $(STATE_DIR)"

uninstall:
	sudo rm -f $(INSTALL_DIR)/$(BINARY)
	rm -rf $(STATE_DIR)/

clean:
	rm -rf bin/

test:
	go test ./...

build-test-image:
	docker build -t aivm-test-base:latest ./test/docker/

test-integration: build-test-image
	@go build -tags integration ./test/... 2>&1
	go test -tags integration -v -timeout 60m ./test/scenarios/

fmt:
	go fmt ./...

vet:
	go vet ./...
