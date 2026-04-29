#!/usr/bin/env bash
# Shared logging utilities for aivm — source this file, do not execute directly.

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

_aivm_ts() { date '+%H:%M:%S'; }
_aivm_prefix() { echo -e "${BOLD}${BLUE}[aivm]${NC}"; }

log_info()    { echo -e "$(_aivm_prefix) $(_aivm_ts) ${GREEN}INFO${NC}  $*"; }
log_warn()    { echo -e "$(_aivm_prefix) $(_aivm_ts) ${YELLOW}WARN${NC}  $*" >&2; }
log_error()   { echo -e "$(_aivm_prefix) $(_aivm_ts) ${RED}ERROR${NC} $*" >&2; }
log_success() { echo -e "$(_aivm_prefix) $(_aivm_ts) ${GREEN}✓${NC}     $*"; }
log_step()    { echo -e "\n$(_aivm_prefix) $(_aivm_ts) ${CYAN}────${NC} $* ${CYAN}────${NC}"; }
log_fatal()   { log_error "FATAL: $*"; exit 1; }
log_debug()   {
  [[ "${AIVM_DEBUG:-0}" == "1" ]] && echo -e "$(_aivm_prefix) $(_aivm_ts) ${CYAN}DEBUG${NC} $*" || true
}
