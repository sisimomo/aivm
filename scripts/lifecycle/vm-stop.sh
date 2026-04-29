#!/usr/bin/env bash
# vm-stop.sh — Stop the aivm Colima VM gracefully.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

source "$REPO_ROOT/scripts/utils/logging.sh"

[[ -f "$REPO_ROOT/.env" ]] && set -a && source "$REPO_ROOT/.env" && set +a

COLIMA_PROFILE="${AIVM_COLIMA_PROFILE:-aivm}"
AIVM_STATE_DIR="$HOME/.aivm"
LIFECYCLE_LOCK="$AIVM_STATE_DIR/lifecycle.lock"

is_vm_running() {
  colima list 2>/dev/null | awk 'NR>1 {print $1, $2}' \
    | grep -q "^${COLIMA_PROFILE} Running"
}

main() {
  mkdir -p "$AIVM_STATE_DIR"

  exec 200>"$LIFECYCLE_LOCK"
  flock -w 30 200 || log_fatal "Could not acquire lifecycle lock within 30s"

  if ! is_vm_running; then
    log_info "VM '${COLIMA_PROFILE}' is not running"
    flock -u 200
    return 0
  fi

  log_step "Stopping Colima VM '${COLIMA_PROFILE}'"

  # Stop Docker workloads inside VM
  log_info "Stopping Docker containers inside VM..."
  colima ssh --profile "$COLIMA_PROFILE" -- \
    bash -lc "docker ps -q 2>/dev/null | xargs -r docker stop --time=10 2>/dev/null || true" \
    2>/dev/null || true

  # Stop Colima VM
  colima stop "$COLIMA_PROFILE" 2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log" || {
    log_warn "Graceful stop failed; forcing..."
    colima stop "$COLIMA_PROFILE" --force 2>/dev/null || true
  }

  log_success "VM '${COLIMA_PROFILE}' stopped"

  flock -u 200
}

main "$@"
