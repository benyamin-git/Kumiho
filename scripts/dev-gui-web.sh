#!/usr/bin/env bash
# Dev helper: run the GUI web harness (real UI assets + mock daemon) on
# 0.0.0.0:7407, with a pidfile and a file log. 7407 is this project's
# registered port in the home dashboard; KUMIHO_GUI_WEB_PORT overrides it.
#
# The harness binds the port strictly: if something else already holds it,
# startup fails instead of falling back to another port.
#
# Unlike dev-daemon.sh this needs no root: the harness binds no privileged
# port and touches no system state. Preferences are pointed at the scratch
# directory too (KUMIHO_GUI_PREFS), so theme experiments never touch the
# user's real gui.toml.
#
# Usage:
#   scripts/dev-gui-web.sh [start]   # (re)start; prints the URL and log tail
#   scripts/dev-gui-web.sh stop      # graceful stop (SIGTERM, then SIGKILL)
#   scripts/dev-gui-web.sh log       # tail -f the harness log
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(dirname "$here")"

bin="$repo/dist/kumiho-gui-web-linux-amd64"
port="${KUMIHO_GUI_WEB_PORT:-7407}"

# shellcheck source=/dev/null
source "$here/dev-env.sh" >/dev/null
run_dir="$KUMIHO_RUNTIME_DIR"
pidfile="$run_dir/gui-web.pid"
log="$run_dir/gui-web.log"
prefs="$run_dir/gui.toml"

# running_pid prints the live harness pid from the pidfile; it fails unless
# the pid exists and still belongs to our binary, so a recycled pid can never
# be killed by accident.
running_pid() {
  local pid exe want
  [[ -f "$pidfile" ]] || return 1
  pid="$(cat "$pidfile")"
  [[ -n "$pid" && -d "/proc/$pid" ]] || return 1
  exe="$(readlink -f "/proc/$pid/exe" 2>/dev/null || true)"
  want="$(readlink -f "$bin" 2>/dev/null || printf '%s' "$bin")"
  [[ -n "$exe" && "$exe" == "$want" ]] || return 1
  printf '%s' "$pid"
}

stop_harness() {
  local pid
  if ! pid="$(running_pid)"; then
    rm -f "$pidfile"
    echo "harness not running"
    return 0
  fi
  kill -TERM "$pid" 2>/dev/null || true
  for _ in $(seq 1 50); do
    [[ -d "/proc/$pid" ]] || break
    sleep 0.1
  done
  if [[ -d "/proc/$pid" ]]; then
    echo "harness did not stop within 5s; sending SIGKILL" >&2
    kill -KILL "$pid" 2>/dev/null || true
  fi
  rm -f "$pidfile"
}

port_busy() {
  ss -tlnH "sport = :$port" 2>/dev/null | grep -q .
}

start_harness() {
  if [[ ! -x "$bin" ]]; then
    echo "missing $bin; run: make build-web" >&2
    exit 1
  fi
  # A live harness from a previous start stops first; anything else on the
  # port is refused, never killed.
  stop_harness >/dev/null || true
  if port_busy; then
    echo "port $port is already in use by another process; refusing to start" >&2
    ss -tlnp "sport = :$port" >&2 || true
    exit 1
  fi
  mkdir -p "$run_dir"
  nohup env KUMIHO_GUI_WEB_PORT="$port" KUMIHO_GUI_PREFS="$prefs" \
    "$bin" >>"$log" 2>&1 &
  echo "$!" >"$pidfile"
  sleep 0.5
  if ! running_pid >/dev/null; then
    echo "harness failed to start; last log lines:" >&2
    tail -n 20 "$log" >&2 || true
    rm -f "$pidfile"
    exit 1
  fi
  echo "kumiho web harness: http://0.0.0.0:$port/ (dev panel at /dev)"
  echo "log: $log"
  tail -n 5 "$log" || true
}

case "${1:-start}" in
  start)
    start_harness
    ;;
  stop)
    stop_harness
    ;;
  log)
    tail -n 50 -f "$log"
    ;;
  *)
    echo "usage: $0 [start|stop|log]" >&2
    exit 2
    ;;
esac
