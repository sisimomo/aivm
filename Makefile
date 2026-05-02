BINARY := aivm
INSTALL_DIR := /usr/local/bin
BUILD_FLAGS := -ldflags="-s -w"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build install uninstall clean test fmt vet

build:
	go build $(BUILD_FLAGS) -o bin/$(BINARY) ./cmd/aivm

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

uninstall:
	sudo rm -f $(INSTALL_DIR)/$(BINARY)
	rm -rf ~/.aivm/

clean:
	rm -rf bin/

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...
