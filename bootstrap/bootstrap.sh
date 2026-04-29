#!/usr/bin/env bash
# bootstrap.sh — Idempotent VM environment setup.
# Runs INSIDE the VM (via colima ssh).
# Installs: Java 25 (Temurin/apt), Maven (latest 3.x), Node.js LTS, Claude Code.
# Writes ~/.aivm-bootstrap-version ONLY after all tools are verified.
set -euo pipefail

# ── Version tag — bump to force re-bootstrap ─────────────────────────────────
BOOTSTRAP_VERSION="2025-04-28-v6"

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

export DEBIAN_FRONTEND=noninteractive

# ── 1. System packages (apt) ──────────────────────────────────────────────────
step "Installing system packages"

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
  python3 \
  2>&1 | tee -a "$LOG_FILE"

success "System packages installed"

# ── 2. Docker validation ──────────────────────────────────────────────────────
step "Validating Docker"

# Docker is pre-installed by Colima/Lima in the VM.
if ! docker info >/dev/null 2>&1; then
  warn "Docker daemon not yet accessible — adding user to docker group"
  sudo usermod -aG docker "$USER" 2>/dev/null || true
  if ! sg docker -c "docker info" >/dev/null 2>&1; then
    fatal "Docker is not functional in this VM. Check Colima configuration."
  fi
fi

docker run --rm hello-world 2>&1 | tee -a "$LOG_FILE" \
  | grep -q "Hello from Docker!" \
  || fatal "docker run hello-world failed — Docker is not fully operational"

success "Docker is operational"

# ── 3. Java 25 (Temurin via Adoptium apt repo) ───────────────────────────────
step "Installing Java 25 (Temurin)"

if ! java -version 2>&1 | grep -qE 'version "2[5-9]|version "3[0-9]' 2>/dev/null; then
  # Add Adoptium GPG key and apt repository
  sudo mkdir -p /etc/apt/keyrings
  wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public \
    | sudo gpg --dearmor -o /etc/apt/keyrings/adoptium.gpg

  echo "deb [signed-by=/etc/apt/keyrings/adoptium.gpg] \
https://packages.adoptium.net/artifactory/deb \
$(lsb_release -sc) main" \
    | sudo tee /etc/apt/sources.list.d/adoptium.list > /dev/null

  sudo apt-get update -qq
  sudo apt-get install -y temurin-25-jdk 2>&1 | tee -a "$LOG_FILE"
else
  info "Java 25+ already installed"
fi

JAVA_ACTUAL=$(java -version 2>&1 | head -1)
info "Java: $JAVA_ACTUAL"
if ! java -version 2>&1 | grep -qE 'version "2[5-9]|version "3[0-9]'; then
  fatal "Java 25+ required but got: $JAVA_ACTUAL"
fi
success "Java installed: $JAVA_ACTUAL"

# ── 4. Maven (latest 3.x — direct download from Apache) ──────────────────────
step "Installing Maven"

MAVEN_INSTALL_DIR="/opt/maven"
if ! command -v mvn >/dev/null 2>&1; then
  # Resolve latest 3.x version from Apache dist index
  MAVEN_VERSION=$(curl -s "https://dlcdn.apache.org/maven/maven-3/" \
    | grep -oE '[0-9]+\.[0-9]+\.[0-9]+/' \
    | sort -V | tail -1 | tr -d '/')

  if [[ -z "$MAVEN_VERSION" ]]; then
    fatal "Could not resolve latest Maven 3.x version from Apache"
  fi

  info "Downloading Maven ${MAVEN_VERSION}"
  MAVEN_URL="https://dlcdn.apache.org/maven/maven-3/${MAVEN_VERSION}/binaries/apache-maven-${MAVEN_VERSION}-bin.tar.gz"
  MAVEN_TMP=$(mktemp -d)
  curl -fsSL "$MAVEN_URL" -o "$MAVEN_TMP/maven.tar.gz" 2>&1 | tee -a "$LOG_FILE"
  sudo mkdir -p "$MAVEN_INSTALL_DIR"
  sudo tar -xzf "$MAVEN_TMP/maven.tar.gz" -C "$MAVEN_INSTALL_DIR" --strip-components=1
  sudo ln -sf "$MAVEN_INSTALL_DIR/bin/mvn" /usr/local/bin/mvn
  rm -rf "$MAVEN_TMP"
else
  info "Maven already installed"
fi

MVN_ACTUAL=$(mvn --version 2>&1 | head -1)
info "Maven: $MVN_ACTUAL"
success "Maven installed: $MVN_ACTUAL"

# ── 5. Node.js LTS (via NodeSource apt repo) ─────────────────────────────────
step "Installing Node.js LTS"

if ! command -v node >/dev/null 2>&1; then
  curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - \
    2>&1 | tee -a "$LOG_FILE"
  sudo apt-get install -y nodejs 2>&1 | tee -a "$LOG_FILE"
else
  info "Node.js already installed"
fi

NODE_ACTUAL=$(node --version 2>/dev/null)
NPM_ACTUAL=$(npm --version 2>/dev/null)
info "Node: $NODE_ACTUAL, npm: $NPM_ACTUAL"
success "Node.js installed: $NODE_ACTUAL"

# ── 6. Claude Code ────────────────────────────────────────────────────────────
step "Installing Claude Code"

# Configure npm global prefix to a user-writable directory so sudo is not needed
NPM_GLOBAL_DIR="$HOME/.npm-global"
mkdir -p "$NPM_GLOBAL_DIR"
npm config set prefix "$NPM_GLOBAL_DIR"
export PATH="$NPM_GLOBAL_DIR/bin:$PATH"

if ! command -v claude >/dev/null 2>&1; then
  npm install -g @anthropic-ai/claude-code 2>&1 | tee -a "$LOG_FILE"
else
  info "Claude Code already installed — updating"
  npm update -g @anthropic-ai/claude-code 2>&1 | tee -a "$LOG_FILE" || true
fi

CLAUDE_ACTUAL=$(claude --version 2>/dev/null || echo "unknown")
info "Claude Code: $CLAUDE_ACTUAL"
success "Claude Code installed: $CLAUDE_ACTUAL"

# ── 7. Shell profile setup ────────────────────────────────────────────────────
step "Configuring shell profile"

PROFILE_BLOCK_START="# >>> aivm bootstrap >>>"
PROFILE_BLOCK_END="# <<< aivm bootstrap <<<"

write_profile_block() {
  local profile_file="$1"
  if grep -q "$PROFILE_BLOCK_START" "$profile_file" 2>/dev/null; then
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
export PATH="/opt/maven/bin:\$HOME/.npm-global/bin:\$PATH"
${PROFILE_BLOCK_END}
EOF
}

write_profile_block "$HOME/.bashrc"
[[ -f "$HOME/.bash_profile" ]] && write_profile_block "$HOME/.bash_profile" || true
[[ -f "$HOME/.profile" ]] && write_profile_block "$HOME/.profile" || true

success "Shell profile configured"

# ── 8. /home/<user>/dev symlink (convenience alias) ──────────────────────────
step "Setting up home dev symlink"

# Lima mounts the host at the exact same absolute path (e.g. /Users/simon/dev).
# Create /home/<user>/dev as a convenience symlink pointing to that mount.
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

success "Home dev symlink configured"

# ── 9. MCP client config ──────────────────────────────────────────────────────
step "Configuring MCP client for Claude Code"

MCPJUNGLE_PORT_ENV="${MCPJUNGLE_PORT:-8080}"

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

# ── 10. Verify all tools ──────────────────────────────────────────────────────
step "Verifying tool installations"

export PATH="/opt/maven/bin:$HOME/.npm-global/bin:$PATH"

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

check_tool "Maven"   mvn
check_tool "Git"     git
check_tool "Docker"  docker
check_tool "Node.js" node
check_tool "npm"     npm
check_tool "Claude"  claude

# Java uses -version (stderr)
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


