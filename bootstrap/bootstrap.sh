#!/usr/bin/env bash
# bootstrap.sh — VM environment setup. Runs INSIDE the VM (via colima ssh).
# Installs: Java 25 (Temurin/apt), Maven (latest 3.x), Node.js LTS, Claude Code CLI.
#
# Designed to run EXACTLY ONCE, when the VM is first created. 'aivm stop'
# deletes the VM, so any next start either resumes an already-provisioned VM
# (no bootstrap) or creates a new VM (bootstrap runs again from scratch).
# Therefore this script is intentionally NOT idempotent — it assumes a clean,
# freshly-installed Ubuntu environment.
set -euo pipefail

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
# Bootstrap is invoked at VM creation time only. If the marker exists, the VM
# was already provisioned in a previous run of this script — bail out to avoid
# re-running expensive installers. Caller (vm-start.sh) shouldn't trigger us
# in that case, but we guard against accidental manual invocation.
if [[ -f "$MARKER_FILE" ]]; then
  echo "[bootstrap] Marker $MARKER_FILE exists — VM already bootstrapped. Aborting."
  echo "[bootstrap] To re-bootstrap, destroy the VM first: aivm stop && aivm start"
  exit 0
fi

mkdir -p "$HOME/.aivm"
echo "[$(ts)] Bootstrap started" > "$LOG_FILE"

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

# ── 2. Java 25 (Temurin via Adoptium apt repo) ───────────────────────────────
step "Installing Java 25 (Temurin)"

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

JAVA_ACTUAL=$(java -version 2>&1 | head -1)
info "Java: $JAVA_ACTUAL"
if ! java -version 2>&1 | grep -qE 'version "2[5-9]|version "3[0-9]'; then
  fatal "Java 25+ required but got: $JAVA_ACTUAL"
fi
success "Java installed: $JAVA_ACTUAL"

# ── 3. Maven (latest 3.x — direct download from Apache) ──────────────────────
step "Installing Maven"

MAVEN_INSTALL_DIR="/opt/maven"
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

MVN_ACTUAL=$(mvn --version 2>&1 | head -1)
info "Maven: $MVN_ACTUAL"
success "Maven installed: $MVN_ACTUAL"

# ── 4. Node.js LTS (via NodeSource apt repo) ─────────────────────────────────
step "Installing Node.js LTS"

curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - \
  2>&1 | tee -a "$LOG_FILE"
sudo apt-get install -y nodejs 2>&1 | tee -a "$LOG_FILE"

NODE_ACTUAL=$(node --version 2>/dev/null)
NPM_ACTUAL=$(npm --version 2>/dev/null)
info "Node: $NODE_ACTUAL, npm: $NPM_ACTUAL"
success "Node.js installed: $NODE_ACTUAL"

# ── 5. Python (via uv) ────────────────────────────────────────────────────────
step "Installing uv and Python"

# INSTALLER_NO_MODIFY_PATH=1 skips the interactive profile-edit prompt
INSTALLER_NO_MODIFY_PATH=1 \
  curl -LsSf https://astral.sh/uv/install.sh | sh \
  2>&1 | tee -a "$LOG_FILE"

export PATH="$HOME/.local/bin:$PATH"

# Install latest stable Python
uv python install 2>&1 | tee -a "$LOG_FILE"

UV_ACTUAL=$(uv --version 2>/dev/null)
PYTHON_ACTUAL=$(uv run python --version 2>/dev/null || python3 --version 2>/dev/null || echo "unknown")
info "uv: $UV_ACTUAL"
info "Python: $PYTHON_ACTUAL"
success "uv + Python installed: $UV_ACTUAL"

# ── 6. rtk (Rust Token Killer) ──────────────────────────────────────────────
step "Installing rtk"

curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh \
  2>&1 | tee -a "$LOG_FILE"

RTK_ACTUAL=$(rtk --version 2>/dev/null || echo "unknown")
info "rtk: $RTK_ACTUAL"
success "rtk installed: $RTK_ACTUAL"

# ── 7. Claude Code CLI ────────────────────────────────────────────────────────
step "Installing Claude Code CLI"

curl -fsSL https://claude.ai/install.sh | bash 2>&1 | tee -a "$LOG_FILE"

# Ensure the installer's bin dir is on PATH for the rest of this script
export PATH="$HOME/.claude/local/bin:$HOME/.local/bin:$PATH"

if ! command -v claude >/dev/null 2>&1; then
  fatal "Claude Code CLI not found after install"
fi

CLAUDE_ACTUAL=$(claude --version 2>/dev/null || echo "unknown")
info "Claude Code: $CLAUDE_ACTUAL"
success "Claude Code installed: $CLAUDE_ACTUAL"

step "Configuring Claude Code"
echo '{"hasCompletedOnboarding": true}' > "$HOME/.claude.json"
success "~/.claude.json written"

# ── 8. Shell profile setup ────────────────────────────────────────────────────
step "Configuring shell profile"

# Drop a single profile.d snippet — applies to all login shells.
# (Bootstrap runs on a fresh VM, so we don't need to scrub previous blocks.)
sudo tee /etc/profile.d/aivm.sh > /dev/null <<'EOF'
export PATH="/opt/maven/bin:$HOME/.local/bin:$HOME/.npm-global/bin:$PATH"
EOF
sudo chmod 0644 /etc/profile.d/aivm.sh

success "Shell profile configured (/etc/profile.d/aivm.sh)"

# ── 9. MCP client config for Claude Code ─────────────────────────────────────
step "Configuring MCP client for Claude Code"

MCPJUNGLE_PORT_ENV="${MCPJUNGLE_PORT:-8080}"
MCP_CONFIG="$HOME/.claude/mcp-config.json"
mkdir -p "$HOME/.claude"

cat > "$MCP_CONFIG" <<EOF
{
  "mcpServers": {
    "mcpjungle": {
      "type": "http",
      "url": "http://host.lima.internal:${MCPJUNGLE_PORT_ENV}/mcp"
    }
  }
}
EOF

info "MCP config written to $MCP_CONFIG"

success "MCP client configured → http://host.lima.internal:${MCPJUNGLE_PORT_ENV}/mcp"

# ── 10. Verify all tools ──────────────────────────────────────────────────────
step "Verifying tool installations"

export PATH="/opt/maven/bin:$HOME/.local/bin:$HOME/.npm-global/bin:$PATH"

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
check_tool "uv"      uv
check_tool "Claude Code" claude
check_tool "rtk"     rtk

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

# ── Write marker (LAST step) ─────────────────────────────────────────────────
# Records that bootstrap has completed in this VM. Presence of this file
# prevents accidental re-runs (see guard at top of this script).
date '+%Y-%m-%d %H:%M:%S' > "$MARKER_FILE"
success "Bootstrap complete!"
echo ""
echo "  Java:        $(java -version 2>&1 | head -1)"
echo "  Maven:       $(mvn --version 2>&1 | head -1)"
echo "  Node.js:     $(node --version)"
echo "  rtk:         $(rtk --version 2>/dev/null || echo 'installed')"
echo "  python:      $(python --version 2>/dev/null || echo 'installed')"
echo "  Claude Code: $(claude --version 2>/dev/null || echo 'installed')"
echo ""
