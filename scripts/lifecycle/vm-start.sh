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
VM_MAX_AGE_DAYS="${AIVM_VM_MAX_AGE_DAYS:-7}"
DEV_ROOT="${AIVM_DEV_ROOT:-$HOME/dev}"
DEV_ROOT="${DEV_ROOT/#\~/$HOME}"
AIVM_STATE_DIR="$HOME/.aivm"
LIFECYCLE_LOCK_DIR="$AIVM_STATE_DIR/lifecycle.lock.d"
BOOTSTRAP_SCRIPT="$REPO_ROOT/bootstrap/bootstrap.sh"
VM_CREATED_AT_FILE="$AIVM_STATE_DIR/vm-created-at"

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

# Returns 0 (true) if the existing stopped VM is too old and the user wants to recreate it.
should_recreate_vm() {
  if [[ ! -f "$VM_CREATED_AT_FILE" ]]; then
    return 1
  fi
  local created_at
  created_at=$(cat "$VM_CREATED_AT_FILE" 2>/dev/null || echo "0")
  if ! [[ "$created_at" =~ ^[0-9]+$ ]] || (( created_at == 0 )); then
    return 1
  fi
  local now age_days
  now=$(date +%s)
  age_days=$(( (now - created_at) / 86400 ))
  if (( age_days < VM_MAX_AGE_DAYS )); then
    return 1
  fi
  log_warn "VM '${COLIMA_PROFILE}' is ${age_days} day(s) old (threshold: ${VM_MAX_AGE_DAYS} days)"
  if [[ -t 0 ]]; then
    local answer
    read -r -p "  → Delete and recreate for a clean slate? [y/N] " answer < /dev/tty
    [[ "${answer,,}" == "y" ]]
  else
    log_info "Non-interactive mode — keeping existing VM despite age"
    return 1
  fi
}

BSP_REPO_DIR="$AIVM_STATE_DIR/backend-skills-plugin"
BSP_REPO_URL="git@github.com:TouchTunes/backend-skills-plugin.git"

# Clone or update backend-skills-plugin on the HOST using the host's own git/SSH.
# The clone lives in ~/.aivm/ — no SSH key or agent ever enters the VM.
BSP_REPO_BRANCH="feat/oracle-instant-client-linux-arm64"

ensure_bsp_repo() {
  if [[ ! -d "$BSP_REPO_DIR/.git" ]]; then
    log_info "Cloning backend-skills-plugin (host-side, branch: ${BSP_REPO_BRANCH})..."
    mkdir -p "$BSP_REPO_DIR"
    git clone --depth 1 --branch "$BSP_REPO_BRANCH" "$BSP_REPO_URL" "$BSP_REPO_DIR" \
      2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log" \
      || log_fatal "Failed to clone backend-skills-plugin"
  else
    log_info "Updating backend-skills-plugin (host-side)..."
    git -C "$BSP_REPO_DIR" pull --ff-only \
      2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log" || true
  fi
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  mkdir -p "$AIVM_STATE_DIR/logs"

  acquire_lifecycle_lock
  trap 'release_lifecycle_lock' EXIT INT TERM

  # vm_was_created=1 means we created a fresh VM this run → bootstrap + record timestamp.
  local vm_was_created=0

  if is_vm_running; then
    log_info "VM '${COLIMA_PROFILE}' is already running"
  else
    # If the profile exists but is stopped, check its age and optionally delete it.
    if vm_profile_exists && should_recreate_vm; then
      log_step "Deleting aged VM profile '${COLIMA_PROFILE}'"
      colima delete "$COLIMA_PROFILE" --force --data \
        2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log" \
        || log_fatal "Failed to delete VM profile '${COLIMA_PROFILE}'"
      rm -f "$VM_CREATED_AT_FILE"
      log_success "Old VM deleted"
    fi

    if ! vm_profile_exists; then
      # ── Fresh VM creation ────────────────────────────────────────────────────
      vm_was_created=1

      # Clone the plugin on the host before starting the VM so it can be mounted.
      ensure_bsp_repo

      log_step "Creating Colima VM '${COLIMA_PROFILE}'"
      mkdir -p "$DEV_ROOT"
      mkdir -p "$AIVM_STATE_DIR/.claude/projects"

      local vm_type_flags
      vm_type_flags=$(colima_vm_type_flag)
      log_info "CPU=${VM_CPUS} Memory=${VM_MEMORY}GiB Disk=${VM_DISK}GiB Type=${VM_TYPE}"

      local extra_mounts=()
      if [[ "$REPO_ROOT" != "$DEV_ROOT"* ]]; then
        extra_mounts=(--mount "${REPO_ROOT}/bootstrap:r")
      fi

      colima start "$COLIMA_PROFILE" \
        --cpu "$VM_CPUS" \
        --memory "$VM_MEMORY" \
        --disk "$VM_DISK" \
        --mount "${DEV_ROOT}:w" \
        --mount "$AIVM_STATE_DIR/.claude/projects:w" \
        --mount "$BSP_REPO_DIR:r" \
        "${extra_mounts[@]}" \
        $vm_type_flags \
        --ssh-agent=false \
        2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log"
    else
      # ── Resume stopped VM ────────────────────────────────────────────────────
      log_step "Resuming stopped VM '${COLIMA_PROFILE}'"
      colima start "$COLIMA_PROFILE" \
        2>&1 | tee -a "$AIVM_STATE_DIR/logs/colima.log"
    fi

    # Wait for VM to be SSH-reachable
    local retries=30 i=0
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

    # Record creation timestamp so age checks work on future starts.
    if (( vm_was_created )); then
      date +%s > "$VM_CREATED_AT_FILE"
    fi
  fi

  # Run bootstrap only on a freshly created VM. A resumed (stopped→running)
  # VM keeps its disk and is already provisioned.
  if (( vm_was_created )); then
    log_step "Running VM bootstrap (one-time, on fresh VM)"
    local vm_bootstrap_path
    vm_bootstrap_path="$BOOTSTRAP_SCRIPT"
    local vm_bootstrap_path_q
    vm_bootstrap_path_q=$(printf '%q' "$vm_bootstrap_path")
    colima ssh --profile "$COLIMA_PROFILE" -- \
      bash -lc "MCPJUNGLE_PORT='${MCPJUNGLE_PORT:-8080}' \
        CLAUDE_CODE_OAUTH_TOKEN='${CLAUDE_CODE_OAUTH_TOKEN:-}' \
        AIVM_HOST_HOME='${HOME}' \
        BSP_AWS_ACCESS_KEY_ID='${BSP_AWS_ACCESS_KEY_ID:-}' \
        BSP_AWS_SECRET_ACCESS_KEY='${BSP_AWS_SECRET_ACCESS_KEY:-}' \
        BSP_AWS_REGION='${BSP_AWS_REGION:-us-east-1}' \
        bash ${vm_bootstrap_path_q}" \
      2>&1 | tee -a "$AIVM_STATE_DIR/logs/bootstrap.log"
    log_success "Bootstrap complete"
  else
    log_debug "VM was already provisioned — skipping bootstrap"
  fi
}

main "$@"
