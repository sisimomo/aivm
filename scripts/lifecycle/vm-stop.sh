#!/usr/bin/env bash
# vm-stop.sh — Stop and delete the aivm Colima VM (ephemeral: disk wiped on every stop).
# The next 'aivm start' creates a fresh VM and re-runs bootstrap from scratch.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

source "$REPO_ROOT/scripts/utils/logging.sh"

[[ -f "$REPO_ROOT/.env" ]] && set -a && source "$REPO_ROOT/.env" && set +a

COLIMA_PROFILE="${AIVM_COLIMA_PROFILE:-aivm}"
AIVM_STATE_DIR="$HOME/.aivm"
LIFECYCLE_LOCK_DIR="$AIVM_STATE_DIR/lifecycle.lock.d"

is_vm_running() {
  colima list 2>/dev/null | awk 'NR>1 {print $1, $2}' \
    | grep -q "^${COLIMA_PROFILE} Running"
}

is_vm_profile_exists() {
  colima list 2>/dev/null | awk 'NR>1 {print $1}' \
    | grep -q "^${COLIMA_PROFILE}$"
}

acquire_lifecycle_lock() {
  local deadline=$(( $(date +%s) + 30 ))
  while ! mkdir "$LIFECYCLE_LOCK_DIR" 2>/dev/null; do
    local lock_pid
    lock_pid=$(cat "$LIFECYCLE_LOCK_DIR/pid" 2>/dev/null || echo "")
    if [[ -n "$lock_pid" ]] && ! kill -0 "$lock_pid" 2>/dev/null; then
      rm -rf "$LIFECYCLE_LOCK_DIR"
      continue
    fi
    (( $(date +%s) >= deadline )) && log_fatal "Could not acquire lifecycle lock within 30s"
    sleep 1
  done
  echo $$ > "$LIFECYCLE_LOCK_DIR/pid"
}

release_lifecycle_lock() {
  rm -rf "$LIFECYCLE_LOCK_DIR" 2>/dev/null || true
}

main() {
  mkdir -p "$AIVM_STATE_DIR"

  acquire_lifecycle_lock
  trap 'release_lifecycle_lock' EXIT INT TERM

  # Gracefully stop Docker workloads and the VM only if it is currently running.
  if is_vm_running; then
    log_info "Stopping Docker containers inside VM..."
    colima ssh --profile "$COLIMA_PROFILE" -- \
      bash -lc "docker ps -q 2>/dev/null | xargs -r docker stop --time=10 2>/dev/null || true" \
      2>/dev/null || true

    log_step "Stopping Colima VM '${COLIMA_PROFILE}'"
    colima stop "$COLIMA_PROFILE" 2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log" || {
      log_warn "Graceful stop failed; forcing..."
      colima stop "$COLIMA_PROFILE" --force 2>/dev/null || true
    }
  fi

  # Always delete the profile so the next start gets a clean VM (ephemeral behaviour).
  # --data also wipes container runtime data; --force skips the confirmation prompt.
  if is_vm_profile_exists; then
    log_step "Deleting VM profile '${COLIMA_PROFILE}' (ephemeral — clean slate on next start)"
    colima delete "$COLIMA_PROFILE" --force --data \
      2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log" \
      || log_fatal "Failed to delete VM profile '${COLIMA_PROFILE}' — manual cleanup may be needed"
    log_success "VM '${COLIMA_PROFILE}' deleted"
  else
    log_info "VM profile '${COLIMA_PROFILE}' does not exist — nothing to delete"
  fi
}

main "$@"
