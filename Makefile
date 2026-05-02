BINARY := aivm
DEV_BINARY := aivm-dev
INSTALL_DIR := /usr/local/bin
BUILD_FLAGS := -ldflags="-s -w"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

DEV_STATE_DIR := ~/.aivm-dev
DEV_PROFILE   := aivm-dev
DEV_MCP_PORT  := 7594
DEV_BUILD_FLAGS := -ldflags="-s -w \
  -X main.defaultStateDir=$(DEV_STATE_DIR) \
  -X main.defaultProfile=$(DEV_PROFILE) \
  -X main.defaultMCPPort=$(DEV_MCP_PORT)"

.PHONY: build build-dev install install-dev uninstall uninstall-dev clean test test-integration fmt vet

build:
	go build $(BUILD_FLAGS) -o bin/$(BINARY) ./cmd/aivm

build-dev:
	go build $(DEV_BUILD_FLAGS) -o bin/$(DEV_BINARY) ./cmd/aivm

install: build
	@mkdir -p $(INSTALL_DIR)
	sudo cp bin/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	sudo codesign --force --sign - $(INSTALL_DIR)/$(BINARY)
	@mkdir -p ~/.aivm/logs ~/.aivm/sessions ~/.aivm/mcpjungle-data ~/.aivm/plugins
	@if [ ! -f aivm.yaml ]; then \
	  cp aivm.example.yaml aivm.yaml; \
	  echo "→ Edit aivm.yaml and set auth.claude_token before running aivm"; \
	fi
	@echo "✓  aivm installed to $(INSTALL_DIR)/$(BINARY)"

install-dev: build-dev
	@mkdir -p $(INSTALL_DIR)
	sudo cp bin/$(DEV_BINARY) $(INSTALL_DIR)/$(DEV_BINARY)
	sudo codesign --force --sign - $(INSTALL_DIR)/$(DEV_BINARY)
	@mkdir -p $(DEV_STATE_DIR)/logs $(DEV_STATE_DIR)/sessions $(DEV_STATE_DIR)/mcpjungle-data $(DEV_STATE_DIR)/plugins
	@if [ ! -f $(DEV_STATE_DIR)/aivm.yaml ]; then \
	  cp aivm.example.yaml $(DEV_STATE_DIR)/aivm.yaml; \
	  echo "→ Edit $(DEV_STATE_DIR)/aivm.yaml and set auth.claude_token before running $(DEV_BINARY)"; \
	fi
	@echo "✓  $(DEV_BINARY) installed to $(INSTALL_DIR)/$(DEV_BINARY)"
	@echo "   state dir : $(DEV_STATE_DIR)"
	@echo "   vm profile: $(DEV_PROFILE)"
	@echo "   mcp port  : $(DEV_MCP_PORT)"

uninstall:
	sudo rm -f $(INSTALL_DIR)/$(BINARY)
	rm -rf ~/.aivm/

uninstall-dev:
	sudo rm -f $(INSTALL_DIR)/$(DEV_BINARY)
	rm -rf $(DEV_STATE_DIR)/

clean:
	rm -rf bin/

test:
	go test ./...

test-integration:
	@go build -tags integration ./test/... 2>&1
	go test -tags integration -v -timeout 60m ./test/scenarios/

fmt:
	go fmt ./...

vet:
	go vet ./...
