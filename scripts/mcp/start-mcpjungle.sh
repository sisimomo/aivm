#!/usr/bin/env bash
# start-mcpjungle.sh — Start MCPJungle on the HOST Docker runtime.
# Must NOT use the aivm Colima VM's Docker socket.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

source "$REPO_ROOT/scripts/utils/logging.sh"

# ── Load config ──────────────────────────────────────────────────────────────
[[ -f "$REPO_ROOT/.env" ]] && set -a && source "$REPO_ROOT/.env" && set +a

COLIMA_PROFILE="${AIVM_COLIMA_PROFILE:-aivm}"
MCPJUNGLE_PORT="${MCPJUNGLE_PORT:-8080}"
MCPJUNGLE_DATA_DIR="${MCPJUNGLE_DATA_DIR:-$HOME/.aivm/mcpjungle-data}"
DEV_ROOT="${AIVM_DEV_ROOT:-$HOME/dev}"
COMPOSE_FILE="$REPO_ROOT/docker-compose.mcpjungle.yml"

# ── Find a Docker socket that is NOT the aivm Colima VM ──────────────────────
find_host_docker_socket() {
  local aivm_sock="$HOME/.colima/${COLIMA_PROFILE}/docker.sock"

  # Check Docker context — if it's already pointing somewhere valid and NOT aivm, use it
  local current_ctx
  current_ctx=$(docker context show 2>/dev/null || echo "default")
  if [[ "$current_ctx" != "colima-${COLIMA_PROFILE}" ]]; then
    local ctx_sock
    ctx_sock=$(docker context inspect "$current_ctx" 2>/dev/null \
      | python3 -c "import sys,json; d=json.load(sys.stdin); print(d[0]['Endpoints']['docker']['Host'])" 2>/dev/null || true)
    if [[ -n "$ctx_sock" ]]; then
      local raw_path="${ctx_sock#unix://}"
      if [[ -S "$raw_path" ]] && [[ "$raw_path" != "${aivm_sock}" ]]; then
        log_debug "Using Docker context '$current_ctx': $ctx_sock"
        echo "$ctx_sock"
        return 0
      fi
    fi
  fi

  # Docker Desktop / standard Docker
  if [[ -S "/var/run/docker.sock" ]]; then
    local realpath_sock
    realpath_sock=$(readlink -f /var/run/docker.sock 2>/dev/null || echo "/var/run/docker.sock")
    if [[ "$realpath_sock" != "${aivm_sock}" ]]; then
      log_debug "Using /var/run/docker.sock"
      echo "unix:///var/run/docker.sock"
      return 0
    fi
  fi

  # OrbStack
  if [[ -S "$HOME/.orbstack/run/docker.sock" ]]; then
    log_debug "Using OrbStack socket"
    echo "unix://$HOME/.orbstack/run/docker.sock"
    return 0
  fi

  # Colima default profile
  if [[ -S "$HOME/.colima/default/docker.sock" ]]; then
    log_debug "Using Colima default profile socket"
    echo "unix://$HOME/.colima/default/docker.sock"
    return 0
  fi

  # Any Colima profile except aivm
  for sock_path in "$HOME"/.colima/*/docker.sock; do
    [[ -S "$sock_path" ]] || continue
    local profile_dir
    profile_dir="$(dirname "$sock_path")"
    local profile_name
    profile_name="$(basename "$profile_dir")"
    if [[ "$profile_name" != "$COLIMA_PROFILE" ]]; then
      log_debug "Using Colima '$profile_name' profile socket"
      echo "unix://$sock_path"
      return 0
    fi
  done

  return 1
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  log_step "Starting MCPJungle"

  # Already running?
  local docker_host
  docker_host=$(find_host_docker_socket) || {
    log_fatal "No suitable host Docker runtime found.
  MCPJungle requires a Docker runtime that is separate from the aivm Colima VM.
  Options:
    • Install Docker Desktop: https://www.docker.com/products/docker-desktop/
    • Install OrbStack:       https://orbstack.dev/
    • Start Colima default:   colima start
  Then re-run aivm."
  }

  export DOCKER_HOST="$docker_host"
  export AIVM_DEV_ROOT="$DEV_ROOT"
  export MCPJUNGLE_PORT MCPJUNGLE_DATA_DIR

  # Create data directory
  mkdir -p "$MCPJUNGLE_DATA_DIR"

  # Expand ~ in data dir
  MCPJUNGLE_DATA_DIR="${MCPJUNGLE_DATA_DIR/#\~/$HOME}"

  if DOCKER_HOST="$docker_host" docker compose -f "$COMPOSE_FILE" ps --services --filter status=running 2>/dev/null \
      | grep -q "mcpjungle"; then
    log_info "MCPJungle already running on port ${MCPJUNGLE_PORT}"
    return 0
  fi

  log_info "Pulling MCPJungle image (if needed)..."
  DOCKER_HOST="$docker_host" \
    MCPJUNGLE_DATA_DIR="$MCPJUNGLE_DATA_DIR" \
    AIVM_DEV_ROOT="$DEV_ROOT" \
    docker compose -f "$COMPOSE_FILE" pull --quiet 2>/dev/null || true

  log_info "Starting MCPJungle container..."
  DOCKER_HOST="$docker_host" \
    MCPJUNGLE_DATA_DIR="$MCPJUNGLE_DATA_DIR" \
    AIVM_DEV_ROOT="$DEV_ROOT" \
    docker compose -f "$COMPOSE_FILE" up -d

  # Wait for health
  log_info "Waiting for MCPJungle to become healthy..."
  local retries=20
  local i=0
  while (( i < retries )); do
    if curl -sf "http://127.0.0.1:${MCPJUNGLE_PORT}/health" >/dev/null 2>&1; then
      log_success "MCPJungle is healthy on port ${MCPJUNGLE_PORT}"
      return 0
    fi
    sleep 2
    (( i++ ))
  done

  log_fatal "MCPJungle failed to become healthy after $((retries * 2))s. Check logs: aivm logs"
}

main "$@"
