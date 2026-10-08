#!/usr/bin/env bash
# Dev helper: run the daemon as root, with logs written to a file.
#
# Why root: netcfg shells out to nft/ip, and file capabilities (setcap) do NOT
# propagate to child processes, so a setcap'd daemon still gets EPERM from its
# nft/ip children. The daemon hands the control socket to $SUDO_UID, so CLI
# commands stay unprivileged. Production (M5) will use the systemd unit with
# AmbientCapabilities=, which children do inherit.
#
# Usage:
#   scripts/dev-daemon.sh [start]   # (re)start; prints the startup log
#   scripts/dev-daemon.sh stop      # graceful stop (SIGTERM, full teardown)
#   scripts/dev-daemon.sh log       # tail -f the daemon log
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(dirname "$here")"

# shellcheck source=/dev/null
source "$here/dev-env.sh" >/dev/null

bin="$repo/dist/kumiho-linux-amd64"
log="$KUMIHO_RUNTIME_DIR/daemon.log"
# The [m] keeps pkill/pgrep from matching this pattern's own command line.
pattern='dist/kumiho-linux-a[m]d64 daemon'

stop_daemon() {
  sudo -v # prompt in the foreground (creds are then cached for sudo -b below)
  if ! sudo pkill -TERM -f "$pattern"; then
    return 0
  fi
  # A process stopped by job control (a previous `sudo ... &`) only acts on
  # the pending SIGTERM once it is continued.
  sudo pkill -CONT -f "$pattern" || true
  for _ in $(seq 1 50); do
    pgrep -f "$pattern" >/dev/null || return 0
    sleep 0.1
  done
  echo "daemon still running 5s after SIGTERM; sending SIGKILL" >&2
  sudo pkill -KILL -f "$pattern" || true
  sleep 0.5
}

case "${1:-start}" in
  start)
    if [[ ! -x "$bin" ]]; then
      echo "missing $bin; run: make build" >&2
      exit 1
    fi
    stop_daemon
    # The prompt goes to stderr, which is redirected into the log, so
    # authenticate first; sudo -b then only forks the daemon in the background.
    sudo -v
    sudo -b env KUMIHO_CONFIG="$KUMIHO_CONFIG" KUMIHO_STATE="$KUMIHO_STATE" \
      KUMIHO_TOKENS="$KUMIHO_TOKENS" KUMIHO_RUNTIME_DIR="$KUMIHO_RUNTIME_DIR" \
      "$bin" daemon >>"$log" 2>&1
    sleep 1
    echo "daemon log: $log"
    tail -n 15 "$log"
    ;;
  stop)
    stop_daemon
    ;;
  log)
    tail -n 50 -f "$log"
    ;;
  *)
    echo "usage: $0 [start|stop|log]" >&2
    exit 2
    ;;
esac
