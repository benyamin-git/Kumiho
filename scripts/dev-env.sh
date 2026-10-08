# Source this file to point kumiho at a local dev directory:
#   . scripts/dev-env.sh
# Then run (each in a shell with the env set):
#   ./dist/kumiho-linux-amd64 daemon    # terminal 1, leave running
#   ./dist/kumiho-linux-amd64 login     # terminal 2
#   ./dist/kumiho-linux-amd64 status
#   ./dist/kumiho-linux-amd64           # TUI
#
# This avoids the production paths (/etc/kumiho, /var/lib/kumiho, /run/kumiho)
# until install.sh + systemd units land in milestone M5.

KUMIHO_DEV_DIR="${KUMIHO_DEV_DIR:-$HOME/.local/state/kumiho-dev}"
mkdir -p "$KUMIHO_DEV_DIR"
export KUMIHO_CONFIG="$KUMIHO_DEV_DIR/config.toml"
export KUMIHO_STATE="$KUMIHO_DEV_DIR/state.json"
export KUMIHO_TOKENS="$KUMIHO_DEV_DIR/tokens.json"
export KUMIHO_RUNTIME_DIR="$KUMIHO_DEV_DIR"
echo "kumiho dev paths set under $KUMIHO_DEV_DIR"
