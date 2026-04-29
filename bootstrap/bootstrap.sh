#!/usr/bin/env bash
# bootstrap.sh — Idempotent VM environment setup.
# Runs INSIDE the VM (via colima ssh).
# Installs: Java 25, Maven (latest 3.x), Node.js, Claude Code, Docker validation.
# Writes ~/.aivm-bootstrap-version ONLY after all tools are verified.
set -euo pipefail

# ── Version tag — bump to force re-bootstrap ─────────────────────────────────
BOOTSTRAP_VERSION="2025-04-28-v1"

MARKER_FILE="$HOME/.aivm-bootstrap-version"
LOG_FILE="$HOME/.aivm-bootstrap.log"

# ── Logging ───────────────────────────────────────────────────────────────────
ts()      { date '+%Y-%m-%d %H:%M:%S'; }
info()    { echo "[$(ts)] INFO  $*" | tee -a "$LOG_FILE"; }
success() { echo "[$(ts)] ✓     $*" | tee -a "$LOG_FILE"; }
warn()    { echo "[$(ts)] WARN  $*" | tee -a "$LOG_FILE" >&2; }
fatal()   { echo "[$(ts)] FATAL $*" | tee -a "$LOG_FILE" >&2; exit 1; }
step()    { echo "" && echo "[$(ts)] ──── $* ────" | tee -a "$LOG_FILE"; }

# ── Idempotency check ─────────────────────────────────────────────────────────
if [[ -f "$MARKER_FILE" ]]; then
  installed=$(cat "$MARKER_FILE")
  if [[ "$installed" == "$BOOTSTRAP_VERSION" ]]; then
    echo "[bootstrap] Already at version ${BOOTSTRAP_VERSION} — nothing to do."
    exit 0
  fi
  info "Upgrading bootstrap from ${installed} to ${BOOTSTRAP_VERSION}"
fi

mkdir -p "$HOME/.aivm"
echo "[$(ts)] Bootstrap started (version=${BOOTSTRAP_VERSION})" > "$LOG_FILE"

# ── 1. System packages ────────────────────────────────────────────────────────
step "Installing system packages"

export DEBIAN_FRONTEND=noninteractive
sudo apt-get update -qq
sudo apt-get install -y --no-install-recommends \
  git \
  curl \
  wget \
  unzip \
  zip \
  ca-certificates \
  gnupg \
  lsb-release \
  bash-completion \
  jq \
  htop \
  2>&1 | tee -a "$LOG_FILE"

success "System packages installed"

# ── 2. Docker validation ──────────────────────────────────────────────────────
step "Validating Docker"

# Docker is pre-installed by Colima/Lima in the VM.
# Ensure daemon is running and the current user can use it.
if ! docker info >/dev/null 2>&1; then
  warn "Docker daemon not yet accessible — adding user to docker group"
  sudo usermod -aG docker "$USER" 2>/dev/null || true
  # Try again via newgrp context
  if ! sg docker -c "docker info" >/dev/null 2>&1; then
    fatal "Docker is not functional in this VM. Check Colima configuration."
  fi
fi

docker run --rm hello-world 2>&1 | tee -a "$LOG_FILE" \
  | grep -q "Hello from Docker!" \
  || fatal "docker run hello-world failed — Docker is not fully operational"

success "Docker is operational"

# ── 3. SDKMAN ─────────────────────────────────────────────────────────────────
step "Installing SDKMAN"

export SDKMAN_DIR="$HOME/.sdkman"
if [[ ! -d "$SDKMAN_DIR" ]]; then
  curl -s "https://get.sdkman.io" | bash 2>&1 | tee -a "$LOG_FILE"
else
  info "SDKMAN already installed"
fi

# Source SDKMAN
set +u
# shellcheck source=/dev/null
source "$SDKMAN_DIR/bin/sdkman-init.sh"
set -u

success "SDKMAN $(sdk version 2>/dev/null | head -1)"

# ── 4. Java 25 ────────────────────────────────────────────────────────────────
step "Installing Java 25"

# Find the latest available Java 25 build from OpenJDK (open/tem vendor)
JAVA_VERSION=$(sdk list java 2>/dev/null \
  | grep -E '^\s*(25\.[0-9]|25-)[^|]+\|\s*(open|tem)' \
  | awk '{print $NF}' \
  | head -1)

if [[ -z "$JAVA_VERSION" ]]; then
  # Fallback: try 25.0.1-open or similar pattern
  JAVA_VERSION=$(sdk list java 2>/dev/null \
    | grep -oP '25\.[0-9]+\.[0-9]+-open' \
    | head -1)
fi

if [[ -z "$JAVA_VERSION" ]]; then
  # Last resort: any 25.x
  JAVA_VERSION=$(sdk list java 2>/dev/null \
    | grep -oP '25[^ ]+' \
    | grep -v 'ea\|rc\|beta' \
    | head -1)
fi

if [[ -z "$JAVA_VERSION" ]]; then
  fatal "Could not find Java 25 in SDKMAN. Try: sdk list java | grep 25"
fi

info "Installing Java ${JAVA_VERSION}"
sdk install java "$JAVA_VERSION" < /dev/null 2>&1 | tee -a "$LOG_FILE" || true
sdk use java "$JAVA_VERSION" 2>&1 | tee -a "$LOG_FILE" || true
sdk default java "$JAVA_VERSION" 2>&1 | tee -a "$LOG_FILE" || true

JAVA_ACTUAL=$(java -version 2>&1 | head -1)
info "Java: $JAVA_ACTUAL"
if ! java -version 2>&1 | grep -qE 'version "2[5-9]|version "3[0-9]'; then
  fatal "Java 25+ required but got: $JAVA_ACTUAL"
fi
success "Java installed: $JAVA_ACTUAL"

# ── 5. Maven (latest 3.x) ─────────────────────────────────────────────────────
step "Installing Maven"

MAVEN_VERSION=$(sdk list maven 2>/dev/null \
  | grep -oP '3\.[0-9]+\.[0-9]+' \
  | sort -V \
  | tail -1)

if [[ -z "$MAVEN_VERSION" ]]; then
  fatal "Could not find Maven 3.x in SDKMAN"
fi

info "Installing Maven ${MAVEN_VERSION}"
sdk install maven "$MAVEN_VERSION" < /dev/null 2>&1 | tee -a "$LOG_FILE" || true
sdk default maven "$MAVEN_VERSION" 2>&1 | tee -a "$LOG_FILE" || true

MVN_ACTUAL=$(mvn --version 2>&1 | head -1)
info "Maven: $MVN_ACTUAL"
success "Maven installed: $MVN_ACTUAL"

# ── 6. Node.js (via fnm) ──────────────────────────────────────────────────────
step "Installing Node.js"

export FNM_DIR="$HOME/.local/share/fnm"
export PATH="$HOME/.local/share/fnm:$PATH"

if ! command -v fnm >/dev/null 2>&1; then
  curl -fsSL https://fnm.vercel.app/install | bash 2>&1 | tee -a "$LOG_FILE"
  # Add to PATH for rest of script
  export PATH="$HOME/.local/share/fnm:$PATH"
  eval "$(fnm env --use-on-cd 2>/dev/null)" 2>/dev/null || true
else
  info "fnm already installed"
  eval "$(fnm env --use-on-cd 2>/dev/null)" 2>/dev/null || true
fi

if ! command -v node >/dev/null 2>&1; then
  fnm install --lts 2>&1 | tee -a "$LOG_FILE"
  fnm use lts-latest 2>&1 | tee -a "$LOG_FILE" || fnm use "$(fnm list | tail -1 | awk '{print $2}')" 2>/dev/null || true
fi

NODE_ACTUAL=$(node --version 2>/dev/null)
NPM_ACTUAL=$(npm --version 2>/dev/null)
info "Node: $NODE_ACTUAL, npm: $NPM_ACTUAL"
success "Node.js installed: $NODE_ACTUAL"

# ── 7. Claude Code ────────────────────────────────────────────────────────────
step "Installing Claude Code"

if ! command -v claude >/dev/null 2>&1; then
  npm install -g @anthropic-ai/claude-code 2>&1 | tee -a "$LOG_FILE"
else
  info "Claude Code already installed — updating"
  npm update -g @anthropic-ai/claude-code 2>&1 | tee -a "$LOG_FILE" || true
fi

CLAUDE_ACTUAL=$(claude --version 2>/dev/null || echo "unknown")
info "Claude Code: $CLAUDE_ACTUAL"
success "Claude Code installed: $CLAUDE_ACTUAL"

# ── 8. Shell profile setup ────────────────────────────────────────────────────
step "Configuring shell profile"

PROFILE_BLOCK_START="# >>> aivm bootstrap >>>"
PROFILE_BLOCK_END="# <<< aivm bootstrap <<<"

write_profile_block() {
  local profile_file="$1"
  # Remove existing block
  if grep -q "$PROFILE_BLOCK_START" "$profile_file" 2>/dev/null; then
    # Use python3 for multi-line deletion (sed -i is not portable across macOS/Linux)
    python3 - <<PYEOF
import re, pathlib
p = pathlib.Path("$profile_file")
content = p.read_text()
content = re.sub(
  r'# >>> aivm bootstrap >>>.*?# <<< aivm bootstrap <<<\n?',
  '',
  content,
  flags=re.DOTALL
)
p.write_text(content)
PYEOF
  fi

  cat >> "$profile_file" <<EOF

${PROFILE_BLOCK_START}
# SDKMAN
export SDKMAN_DIR="\$HOME/.sdkman"
[[ -s "\$SDKMAN_DIR/bin/sdkman-init.sh" ]] && source "\$SDKMAN_DIR/bin/sdkman-init.sh"
# fnm (Node.js)
export PATH="\$HOME/.local/share/fnm:\$PATH"
eval "\$(fnm env --use-on-cd 2>/dev/null)" 2>/dev/null || true
${PROFILE_BLOCK_END}
EOF
}

write_profile_block "$HOME/.bashrc"
[[ -f "$HOME/.bash_profile" ]] && write_profile_block "$HOME/.bash_profile" || true
[[ -f "$HOME/.profile" ]] && write_profile_block "$HOME/.profile" || true

success "Shell profile configured"

# ── 9. /home/<user>/dev symlink ───────────────────────────────────────────────
step "Setting up path symlink"

# Lima mounts the host ~/dev at /Users/<user>/dev (same macOS path).
# Create /home/<user>/dev → /Users/<user>/dev so that the canonical VM path works.
HOST_DEV_LIMA="/Users/$(whoami)/dev"
VM_DEV_HOME="$HOME/dev"

if [[ -d "$HOST_DEV_LIMA" ]]; then
  if [[ ! -e "$VM_DEV_HOME" ]]; then
    ln -s "$HOST_DEV_LIMA" "$VM_DEV_HOME"
    info "Created symlink: $VM_DEV_HOME → $HOST_DEV_LIMA"
  elif [[ -L "$VM_DEV_HOME" ]]; then
    info "Symlink already exists: $VM_DEV_HOME → $(readlink "$VM_DEV_HOME")"
  else
    warn "$VM_DEV_HOME exists and is not a symlink — skipping"
  fi
else
  warn "Host dev dir not mounted at $HOST_DEV_LIMA — check Colima mount config"
fi

success "Path symlink configured"

# ── 10. MCP client config ─────────────────────────────────────────────────────
step "Configuring MCP client for Claude Code"

MCPJUNGLE_PORT_ENV="${MCPJUNGLE_PORT:-8080}"
CLAUDE_CONFIG="$HOME/.claude.json"

# Build MCP config
python3 - <<PYEOF
import json, os, pathlib

config_path = pathlib.Path(os.path.expanduser("~/.claude.json"))
config = {}
if config_path.exists():
    try:
        config = json.loads(config_path.read_text())
    except Exception:
        config = {}

port = os.environ.get("MCPJUNGLE_PORT", "${MCPJUNGLE_PORT_ENV}")
config.setdefault("mcpServers", {})["mcpjungle"] = {
    "url": f"http://host.lima.internal:{port}/mcp",
    "transport": "http"
}

config_path.write_text(json.dumps(config, indent=2))
print(f"MCP config written to {config_path}")
PYEOF

success "MCP client configured → http://host.lima.internal:${MCPJUNGLE_PORT_ENV}/mcp"

# ── 11. Verify all tools ──────────────────────────────────────────────────────
step "Verifying tool installations"

set +u
source "$SDKMAN_DIR/bin/sdkman-init.sh" 2>/dev/null || true
export PATH="$HOME/.local/share/fnm:$PATH"
eval "$(fnm env 2>/dev/null)" 2>/dev/null || true
set -u

FAILED=0

check_tool() {
  local name="$1" cmd="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    local ver
    ver=$("$cmd" --version 2>&1 | head -1)
    info "  ✓ $name: $ver"
  else
    warn "  ✗ $name ($cmd) not found in PATH"
    FAILED=1
  fi
}

check_tool "Maven"      mvn
check_tool "Git"        git
check_tool "Docker"     docker
check_tool "Node.js"    node
check_tool "npm"        npm
check_tool "Claude"     claude

# Java uses -version
if command -v java >/dev/null 2>&1; then
  ver=$(java -version 2>&1 | head -1)
  info "  ✓ Java: $ver"
else
  warn "  ✗ Java not found in PATH"
  FAILED=1
fi

if (( FAILED )); then
  fatal "One or more tools failed verification — bootstrap incomplete. Check $LOG_FILE"
fi

success "All tools verified"

# ── Write version marker (LAST step) ─────────────────────────────────────────
echo "$BOOTSTRAP_VERSION" > "$MARKER_FILE"
success "Bootstrap complete! Version: ${BOOTSTRAP_VERSION}"
echo ""
echo "  Java:        $(java -version 2>&1 | head -1)"
echo "  Maven:       $(mvn --version 2>&1 | head -1)"
echo "  Node.js:     $(node --version)"
echo "  Claude Code: $(claude --version 2>/dev/null || echo 'installed')"
echo ""
