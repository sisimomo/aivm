#!/usr/bin/env bash
# install.sh — One-time setup for aivm.
# Installs the CLI symlink and creates required state directories.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
ok()   { echo -e "${GREEN}✓${NC} $*"; }
info() { echo -e "${BLUE}→${NC} $*"; }
warn() { echo -e "${YELLOW}⚠${NC} $*"; }
fail() { echo -e "${RED}✗${NC} $*" >&2; exit 1; }

echo ""
echo "  aivm installer"
echo "  ──────────────────────────────────────"

# ── Check dependencies ────────────────────────────────────────────────────────
info "Checking required tools..."

missing=()
command -v colima  >/dev/null 2>&1 || missing+=("colima  → brew install colima")
command -v incus   >/dev/null 2>&1 || missing+=("incus   → brew install incus")
command -v docker  >/dev/null 2>&1 || missing+=("docker  → https://www.docker.com/products/docker-desktop/")
command -v curl    >/dev/null 2>&1 || missing+=("curl    → brew install curl")
command -v python3 >/dev/null 2>&1 || missing+=("python3 → brew install python3")

if (( ${#missing[@]} > 0 )); then
  echo ""
  warn "Missing required tools:"
  for m in "${missing[@]}"; do
    echo "    • $m"
  done
  echo ""
  warn "Install the above, then re-run install.sh"
  echo ""
  exit 1
fi
ok "All required tools present"

# ── Make scripts executable ───────────────────────────────────────────────────
info "Setting executable permissions..."
chmod +x "$REPO_ROOT/bin/aivm"
chmod +x "$REPO_ROOT/bootstrap/bootstrap.sh"
find "$REPO_ROOT/scripts" -name "*.sh" -exec chmod +x {} \;
ok "Executable permissions set"

# ── Create state directories ──────────────────────────────────────────────────
info "Creating state directories..."
mkdir -p \
  "$HOME/.aivm/logs" \
  "$HOME/.aivm/sessions" \
  "$HOME/.aivm/mcpjungle-data"
ok "State directories: ~/.aivm/"

# ── Install CLI to PATH ───────────────────────────────────────────────────────
INSTALL_TARGET="/usr/local/bin/aivm"
info "Installing aivm to ${INSTALL_TARGET}..."

if [[ -w "/usr/local/bin" ]]; then
  ln -sf "$REPO_ROOT/bin/aivm" "$INSTALL_TARGET"
else
  sudo ln -sf "$REPO_ROOT/bin/aivm" "$INSTALL_TARGET"
fi
ok "Installed: $(which aivm)"

# ── Copy .env if not present ──────────────────────────────────────────────────
if [[ ! -f "$REPO_ROOT/.env" ]]; then
  info "Creating .env from .env.example..."
  cp "$REPO_ROOT/.env.example" "$REPO_ROOT/.env"
  warn "⚠️  Edit $REPO_ROOT/.env and set CLAUDE_CODE_OAUTH_TOKEN before running aivm"
else
  ok ".env already exists"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo "  ──────────────────────────────────────"
echo -e "  ${GREEN}Installation complete!${NC}"
echo ""
echo "  Next steps:"
echo "    1. Edit $REPO_ROOT/.env and set CLAUDE_CODE_OAUTH_TOKEN"
echo "    2. Run: aivm          (from any directory under ~/dev)"
echo ""
echo "  Other commands:"
echo "    aivm status           Show VM and service status"
echo "    aivm stop             Stop everything"
echo "    aivm help             Full help"
echo ""
