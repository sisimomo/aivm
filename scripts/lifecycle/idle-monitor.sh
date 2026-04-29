#!/usr/bin/env bash
# idle-monitor.sh — Host-side daemon that shuts down aivm when idle.
#
# Lifecycle:
#   • Started by 'aivm' when the VM starts; one instance at a time.
#   • Monitors session lock files in ~/.aivm/sessions/
#   • If no active sessions for AIVM_IDLE_TIMEOUT seconds → graceful shutdown.
#   • Writes PID to ~/.aivm/idle-monitor.pid
#
# Session lock format: ~/.aivm/sessions/<pid>.lock
# Content: "<pid> <epoch_start_time>"
# A session is active iff the PID is alive AND the lock file exists.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

source "$REPO_ROOT/scripts/utils/logging.sh"

[[ -f "$REPO_ROOT/.env" ]] && set -a && source "$REPO_ROOT/.env" && set +a

AIVM_STATE_DIR="$HOME/.aivm"
SESSION_DIR="$AIVM_STATE_DIR/sessions"
PID_FILE="$AIVM_STATE_DIR/idle-monitor.pid"
LAST_ACTIVE_FILE="$AIVM_STATE_DIR/last-session-end"
COLIMA_PROFILE="${AIVM_COLIMA_PROFILE:-aivm}"
IDLE_TIMEOUT="${AIVM_IDLE_TIMEOUT:-300}"   # seconds (default: 5 minutes)
POLL_INTERVAL=30                           # check every 30 seconds

# ── Guard: only one monitor instance ─────────────────────────────────────────
ensure_single_instance() {
  if [[ -f "$PID_FILE" ]]; then
    local existing_pid
    existing_pid=$(cat "$PID_FILE" 2>/dev/null || echo "")
    if [[ -n "$existing_pid" ]] && kill -0 "$existing_pid" 2>/dev/null; then
      log_debug "Idle monitor already running (pid=$existing_pid)"
      exit 0
    fi
    log_debug "Stale PID file found — removing"
    rm -f "$PID_FILE"
  fi
  echo $$ > "$PID_FILE"
}

cleanup() {
  log_debug "Idle monitor exiting (pid=$$)"
  rm -f "$PID_FILE"
}

# ── Session counting ──────────────────────────────────────────────────────────

# Returns the count of genuinely active sessions.
# Removes stale lock files (PID dead or lock data invalid).
count_active_sessions() {
  mkdir -p "$SESSION_DIR"
  local active=0

  shopt -s nullglob
  for lock_file in "$SESSION_DIR"/*.lock; do
    local content
    content=$(cat "$lock_file" 2>/dev/null || echo "")
    local pid start_epoch
    pid=$(echo "$content" | awk '{print $1}')
    start_epoch=$(echo "$content" | awk '{print $2}')

    # Validate lock data
    if [[ -z "$pid" ]] || ! [[ "$pid" =~ ^[0-9]+$ ]]; then
      log_warn "Invalid lock file $lock_file — removing"
      rm -f "$lock_file"
      continue
    fi

    # Check PID is alive
    if ! kill -0 "$pid" 2>/dev/null; then
      log_debug "Session PID $pid is dead — removing stale lock"
      rm -f "$lock_file"
      echo "$(date +%s)" > "$LAST_ACTIVE_FILE"
      continue
    fi

    # Optional: cross-check start time to guard against PID reuse
    if [[ -n "$start_epoch" ]] && command -v ps >/dev/null 2>&1; then
      local proc_start
      proc_start=$(ps -p "$pid" -o lstart= 2>/dev/null | tr -s ' ' | xargs || echo "")
      if [[ -n "$proc_start" ]]; then
        # Convert proc start to epoch for comparison
        local proc_epoch
        proc_epoch=$(date -j -f "%a %b %d %T %Y" "$proc_start" +%s 2>/dev/null \
          || date --date="$proc_start" +%s 2>/dev/null || echo "0")
        # Allow 2s tolerance for PID start time comparison
        if [[ -n "$proc_epoch" ]] && (( proc_epoch > 0 )); then
          local diff=$(( proc_epoch - start_epoch ))
          diff=${diff#-}  # abs
          if (( diff > 10 )); then
            log_warn "PID $pid start time mismatch (potential PID reuse) — removing lock"
            rm -f "$lock_file"
            continue
          fi
        fi
      fi
    fi

    (( ++active ))
  done
  shopt -u nullglob

  echo "$active"
}

is_vm_running() {
  colima list 2>/dev/null | awk 'NR>1 {print $1, $2}' \
    | grep -q "^${COLIMA_PROFILE} Running" 2>/dev/null
}

# ── Shutdown sequence ─────────────────────────────────────────────────────────
shutdown_all() {
  log_step "Idle timeout reached — initiating shutdown"

  # 1. Stop VM
  log_info "Stopping VM..."
  "$REPO_ROOT/scripts/lifecycle/vm-stop.sh" 2>&1 \
    | tee -a "$AIVM_STATE_DIR/logs/idle-monitor.log" || {
    log_warn "VM stop had errors (continuing cleanup)"
  }

  # 2. Stop MCPJungle
  log_info "Stopping MCPJungle..."
  "$REPO_ROOT/scripts/mcp/stop-mcpjungle.sh" 2>&1 \
    | tee -a "$AIVM_STATE_DIR/logs/idle-monitor.log" || {
    log_warn "MCPJungle stop had errors"
  }

  log_success "aivm shutdown complete"
}

# ── Main loop ─────────────────────────────────────────────────────────────────
main() {
  mkdir -p "$AIVM_STATE_DIR/logs" "$SESSION_DIR"

  # Redirect output to log
  exec >> "$AIVM_STATE_DIR/logs/idle-monitor.log" 2>&1

  ensure_single_instance
  trap cleanup EXIT

  log_info "Idle monitor started (pid=$$, timeout=${IDLE_TIMEOUT}s, poll=${POLL_INTERVAL}s)"

  # Initialise last-active timestamp
  if [[ ! -f "$LAST_ACTIVE_FILE" ]]; then
    date +%s > "$LAST_ACTIVE_FILE"
  fi

  while true; do
    sleep "$POLL_INTERVAL"

    # If VM stopped externally, exit monitor
    if ! is_vm_running; then
      log_info "VM is no longer running — idle monitor exiting"
      # Still attempt MCPJungle cleanup
      "$REPO_ROOT/scripts/mcp/stop-mcpjungle.sh" >/dev/null 2>&1 || true
      exit 0
    fi

    local active
    active=$(count_active_sessions)

    if (( active > 0 )); then
      log_debug "Active sessions: ${active}"
      date +%s > "$LAST_ACTIVE_FILE"
      continue
    fi

    # No active sessions — check idle duration
    local last_active_epoch
    last_active_epoch=$(cat "$LAST_ACTIVE_FILE" 2>/dev/null || date +%s)
    local now
    now=$(date +%s)
    local idle_seconds=$(( now - last_active_epoch ))

    log_debug "No active sessions. Idle for ${idle_seconds}s / ${IDLE_TIMEOUT}s"

    if (( idle_seconds >= IDLE_TIMEOUT )); then
      shutdown_all
      exit 0
    fi
  done
}

main "$@"
