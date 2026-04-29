#!/usr/bin/env bash
# vm-start.sh — Start the aivm Colima VM and run bootstrap if needed.
# Uses a lifecycle lock to prevent concurrent start attempts.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

source "$REPO_ROOT/scripts/utils/logging.sh"

[[ -f "$REPO_ROOT/.env" ]] && set -a && source "$REPO_ROOT/.env" && set +a

COLIMA_PROFILE="${AIVM_COLIMA_PROFILE:-aivm}"
VM_CPUS="${AIVM_VM_CPUS:-4}"
VM_MEMORY="${AIVM_VM_MEMORY:-8}"
VM_DISK="${AIVM_VM_DISK:-60}"
VM_TYPE="${AIVM_VM_TYPE:-vz}"
DEV_ROOT="${AIVM_DEV_ROOT:-$HOME/dev}"
DEV_ROOT="${DEV_ROOT/#\~/$HOME}"
AIVM_STATE_DIR="$HOME/.aivm"
LIFECYCLE_LOCK_DIR="$AIVM_STATE_DIR/lifecycle.lock.d"
BOOTSTRAP_SCRIPT="$REPO_ROOT/bootstrap/bootstrap.sh"

# ── Lifecycle lock (mkdir is atomic on APFS/HFS+) ────────────────────────────
acquire_lifecycle_lock() {
  local deadline=$(( $(date +%s) + 30 ))
  while ! mkdir "$LIFECYCLE_LOCK_DIR" 2>/dev/null; do
    local lock_pid
    lock_pid=$(cat "$LIFECYCLE_LOCK_DIR/pid" 2>/dev/null || echo "")
    if [[ -n "$lock_pid" ]] && ! kill -0 "$lock_pid" 2>/dev/null; then
      log_warn "Removing stale lifecycle lock (dead pid=$lock_pid)"
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

# ── Colima profile config dir ─────────────────────────────────────────────────
COLIMA_PROFILE_DIR="$HOME/.colima/${COLIMA_PROFILE}"

# ── Helpers ───────────────────────────────────────────────────────────────────
is_vm_running() {
  colima list 2>/dev/null | awk 'NR>1 {print $1, $2}' \
    | grep -q "^${COLIMA_PROFILE} Running" 2>/dev/null
}

vm_profile_exists() {
  colima list 2>/dev/null | awk 'NR>1 {print $1}' \
    | grep -q "^${COLIMA_PROFILE}$"
}

colima_vm_type_flag() {
  # vz requires macOS 13+ and Apple Silicon; fall back to qemu if vz not supported
  if [[ "$VM_TYPE" == "vz" ]]; then
    local ver
    ver=$(sw_vers -productVersion 2>/dev/null || echo "0")
    local major
    major=$(echo "$ver" | cut -d. -f1)
    if (( major >= 13 )); then
      echo "--vm-type vz --vz-rosetta"
      return
    fi
    log_warn "macOS ${ver} does not support vz VM type — falling back to qemu"
  fi
  echo "--vm-type qemu"
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  mkdir -p "$AIVM_STATE_DIR/logs"

  acquire_lifecycle_lock
  trap 'release_lifecycle_lock' EXIT INT TERM

  # Track whether the VM existed before this run; bootstrap only runs when we
  # just created it (since 'aivm stop' deletes the VM, the next start that
  # doesn't find the profile is, by definition, a fresh creation).
  local vm_was_created=0

  if is_vm_running; then
    log_info "VM '${COLIMA_PROFILE}' is already running"
  else
    if ! vm_profile_exists; then
      vm_was_created=1
    fi

    log_step "Starting Colima VM '${COLIMA_PROFILE}'"

    # Ensure dev root exists on host
    mkdir -p "$DEV_ROOT"

    # Determine VM type flags
    local vm_type_flags
    vm_type_flags=$(colima_vm_type_flag)

    log_info "CPU=${VM_CPUS} Memory=${VM_MEMORY}GiB Disk=${VM_DISK}GiB Type=${VM_TYPE}"

    colima start "$COLIMA_PROFILE" \
      --cpu "$VM_CPUS" \
      --memory "$VM_MEMORY" \
      --disk "$VM_DISK" \
      --mount "${DEV_ROOT}:w" \
      $vm_type_flags \
      --ssh-agent=false \
      2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log"

    # Wait for VM to be ready
    local retries=30
    local i=0
    while (( i < retries )); do
      if colima ssh --profile "$COLIMA_PROFILE" -- echo "VM ready" >/dev/null 2>&1; then
        break
      fi
      sleep 2
      (( ++i ))
    done
    if (( i >= retries )); then
      log_fatal "VM did not become reachable after $((retries * 2))s"
    fi

    log_success "VM '${COLIMA_PROFILE}' is running"
  fi

  # Run bootstrap only on a freshly created VM. A resumed (stopped→running)
  # VM keeps its disk and is already provisioned.
  if (( vm_was_created )); then
    log_step "Running VM bootstrap (one-time, on fresh VM)"
    local vm_bootstrap_path
    vm_bootstrap_path="${DEV_ROOT}/ai-vm/bootstrap/bootstrap.sh"
    # Inject only the specific vars bootstrap needs — never pass secrets or the full .env.
    colima ssh --profile "$COLIMA_PROFILE" -- \
      bash -lc "MCPJUNGLE_PORT='${MCPJUNGLE_PORT:-8080}' bash '${vm_bootstrap_path}'" \
      2>&1 | tee -a "$AIVM_STATE_DIR/logs/bootstrap.log"
    log_success "Bootstrap complete"
  else
    log_debug "VM was already provisioned — skipping bootstrap"
  fi
}

main "$@"
