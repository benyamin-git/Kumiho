#!/usr/bin/env bash
# Dev helper: live watchdog heal check against a real edge.
#
# Kills the edge TCP connection twice (ss -K, needs root) and verifies the
# daemon detects the loss, redials, and restores egress (PLAN.md §4.2).
# After each heal it samples the daemon log so write latency is visible.
# Run on the test server: scripts/watchdog-retest.sh
# Leaves the tunnel connected for inspection.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
# shellcheck source=/dev/null
. scripts/dev-env.sh >/dev/null

bin=./dist/kumiho-linux-amd64
log="$KUMIHO_RUNTIME_DIR/daemon.log"

if [[ ! -x "$bin" ]]; then
  echo "missing $bin; run: make build" >&2
  exit 1
fi

state() { "$bin" status | sed -n 's/^state: *//p'; }

wait_down() {
  for _ in $(seq 1 16); do
    [[ "$(state)" != CONNECTED ]] && return 0
    sleep 0.5
  done
  return 1
}

wait_connected() {
  for _ in $(seq 1 30); do
    [[ "$(state)" == CONNECTED ]] && return 0
    sleep 1
  done
  return 1
}

wait_socket() {
  for _ in $(seq 1 20); do
    ss -tnH state established | grep -q ':2499' && return 0
    sleep 0.5
  done
  return 1
}

check_egress() {
  for _ in 1 2 3; do
    if curl -4 -s --max-time 6 https://www.cloudflare.com/cdn-cgi/trace | grep -E '^(ip|loc)='; then
      return 0
    fi
    sleep 2
  done
  echo "egress check failed" >&2
  return 1
}

"$bin" disconnect >/dev/null 2>&1 || true

echo "== $(date +%T) connect"
if ! "$bin" connect; then
  echo "connect failed" >&2
  exit 1
fi
check_egress

for i in 1 2; do
  if ! wait_socket; then
    echo "no edge socket to kill" >&2
  fi
  ss -tn state established | grep 2499
  echo "== $(date +%T) kill #$i"
  sudo ss -K -t 'dport = 2499'
  if wait_down; then
    echo "loss detected at $(date +%T)"
  else
    echo "loss not observed via status" >&2
  fi
  if wait_connected; then
    echo "healed at $(date +%T)"
  else
    echo "NOT healed within 30 s" >&2
  fi
  echo "log visibility samples:"
  for _ in 1 2 3 4 5; do
    echo "  $(date +%T.%3N) watchdog=$(grep -c 'watchdog' "$log") pattern=$(grep -c -E '\[watchdog\]|\[edge\]' "$log") mtime=$(stat -c %y "$log" | cut -c12-23)"
    sleep 1
  done
  "$bin" status | sed -n '1,3p'
  check_egress
done

echo "== $(date +%T) watchdog log lines"
ls -l --time-style=full-iso "$log"
grep -E '\[watchdog\]|\[edge\]' "$log" | tail -n 8 || echo "(none)"
