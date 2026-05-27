BINARY    ?= aivm
STATE_DIR ?= ~/.aivm

INSTALL_DIR := $(or $(AIVM_INSTALL_DIR),$(HOME)/.local/bin)
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

# demo-edit time parameters (seconds). Override on the command line:
#   make demo-edit SPEED_START=5 SPEED_END=90 CUT_START=91 CUT_END=98
SPEED_START ?= 2   # end of intro → start of 15x speed-up segment
SPEED_END   ?= 85  # end of 15x speed-up segment
CUT_START   ?= 88  # end of post-speed normal segment → start of cut
CUT_END     ?= 96  # end of cut → resume normal playback

.PHONY: build install install-test uninstall clean test test-unit test-e2e test-bootstrap fmt vet release-snapshot release-dry-run demo demo-edit

build:
	go build $(BUILD_FLAGS) -o bin/$(BINARY) ./cmd/aivm

install: build
	@mkdir -p "$(INSTALL_DIR)"
	install -m 755 bin/$(BINARY) "$(INSTALL_DIR)/$(BINARY)"
	xattr -dr com.apple.quarantine "$(INSTALL_DIR)/$(BINARY)" 2>/dev/null || true
	@mkdir -p $(STATE_DIR)/logs $(STATE_DIR)/sessions $(STATE_DIR)/plugins
	@if [ ! -f $(STATE_DIR)/aivm.yaml ]; then \
	  cp aivm.example.yaml $(STATE_DIR)/aivm.yaml; \
	  echo "→ Edit $(STATE_DIR)/aivm.yaml and set auth.claude_token before running $(BINARY)"; \
	fi
	@echo "✓  $(BINARY) installed to $(INSTALL_DIR)/$(BINARY)"
	@echo "   state dir : $(STATE_DIR)"

install-test:
	go build $(BUILD_FLAGS) -o bin/aivm-test ./cmd/aivm
	@mkdir -p "$(INSTALL_DIR)"
	install -m 755 bin/aivm-test "$(INSTALL_DIR)/aivm-test"
	xattr -dr com.apple.quarantine "$(INSTALL_DIR)/aivm-test" 2>/dev/null || true
	@echo "✓  aivm-test installed to $(INSTALL_DIR)/aivm-test"

uninstall:
	rm -f "$(INSTALL_DIR)/$(BINARY)"

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

demo: ## Record the aivm demo video (requires vhs: brew install vhs)
	@command -v vhs >/dev/null 2>&1 || { echo "vhs not found — install with: brew install vhs"; exit 1; }
	@mkdir -p demo
	@echo "→ Swapping out ~/.aivm..."
	@if [ -e $(HOME)/.aivm.bak ]; then \
	  echo "$(HOME)/.aivm.bak already exists — remove or rename it before running make demo"; \
	  exit 1; \
	fi
	@if [ -e $(HOME)/.aivm ]; then mv $(HOME)/.aivm $(HOME)/.aivm.bak; fi
	@mkdir -p $(HOME)/.aivm
	@cp demo/configs/aivm.yaml $(HOME)/.aivm/aivm.yaml
	@echo "→ Destroying VM (if exists)..."
	@$(BINARY) destroy 2>/dev/null || true
	@vhs demo/aivm.tape; STATUS=$$?; \
	  rm -rf $(HOME)/.aivm; \
	  if [ -e $(HOME)/.aivm.bak ]; then \
	    mv $(HOME)/.aivm.bak $(HOME)/.aivm; \
	    echo "✓  Restored $(HOME)/.aivm"; \
	  fi; \
	  exit $$STATUS
	@echo ""
	@echo "→ Review demo/aivm.mp4, then edit it with:"
	@echo "    make demo-edit SPEED_START=<t> SPEED_END=<t> CUT_START=<t> CUT_END=<t>"
	@echo "  Segments (all in seconds):"
	@echo "    0 → SPEED_START  : normal playback (intro)"
	@echo "    SPEED_START → SPEED_END : 15x speed-up with overlay"
	@echo "    SPEED_END → CUT_START   : normal playback"
	@echo "    CUT_START → CUT_END     : cut (removed)"
	@echo "    CUT_END → end           : normal playback"

demo-edit: ## Edit demo/aivm.mp4: speed up bootstrap phase and cut dead time (see SPEED_START/SPEED_END/CUT_START/CUT_END)
	@command -v ffmpeg >/dev/null 2>&1 || { echo "ffmpeg not found — install with: brew install ffmpeg"; exit 1; }
	ffmpeg -i demo/aivm.mp4 -filter_complex \
		"[0:v]trim=0:$(SPEED_START),setpts=PTS-STARTPTS[v1]; \
		 [0:v]trim=$(SPEED_START):$(SPEED_END),setpts=(1/15)*(PTS-STARTPTS),drawtext=text='15x Speed':fontcolor=white:fontsize=48:box=1:boxcolor=black@0.5:boxborderw=15:x=w-tw-40:y=40:fontfile=/System/Library/Fonts/Supplemental/Arial.ttf[v2]; \
		 [0:v]trim=$(SPEED_END):$(CUT_START),setpts=PTS-STARTPTS[v3]; \
		 [0:v]trim=start=$(CUT_END),setpts=PTS-STARTPTS[v4]; \
		 [v1][v2][v3][v4]concat=n=4:v=1:a=0[outv]" \
		-map "[outv]" demo/output.mp4
	rm demo/aivm.mp4
	mv demo/output.mp4 demo/aivm.mp4
