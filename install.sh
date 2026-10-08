#!/usr/bin/env bash
# Kumiho installer (PLAN.md §10).
#
#   sudo ./install.sh                    install or upgrade (idempotent)
#   sudo ./install.sh uninstall          remove; keep /var/lib/kumiho state
#   sudo ./install.sh uninstall --purge  also remove state, tokens and the
#                                        installer-created system user
#
# Options:
#   --binary PATH   binary to install (default: dist/kumiho-linux-<arch>)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
unit_src="$here/systemd/kumiho.service"
polkit_src="$here/systemd/50-kumiho-resolved.rules"
bin_dst=/usr/local/bin/kumiho
unit_dst=/etc/systemd/system/kumiho.service
polkit_dst=/etc/polkit-1/rules.d/50-kumiho-resolved.rules
config_dir=/etc/kumiho
config_file="$config_dir/config.toml"
state_dir=/var/lib/kumiho
service_user=kumiho

mode=install
purge=0
binary=""

die() { echo "install.sh: $*" >&2; exit 1; }

usage() {
    sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
    case "$1" in
        install) mode=install ;;
        uninstall) mode=uninstall ;;
        --purge) purge=1 ;;
        --binary)
            shift
            [ $# -gt 0 ] || die "--binary needs a path"
            binary="$1"
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *) die "unknown argument: $1 (try --help)" ;;
    esac
    shift
done

[ "$(id -u)" -eq 0 ] || die "must run as root (try: sudo $0 $*)"
command -v systemctl >/dev/null || die "systemd is required"

default_binary() {
    case "$(uname -m)" in
        x86_64) echo "$here/dist/kumiho-linux-amd64" ;;
        aarch64 | arm64) echo "$here/dist/kumiho-linux-arm64" ;;
        *) die "unsupported architecture: $(uname -m) (pass --binary PATH)" ;;
    esac
}

# The user is installer-created when it still looks like the system account
# this script creates; on developer machines a regular login user named
# kumiho is reused and must never be deleted by --purge.
user_is_installer_created() {
    local home shell
    home="$(getent passwd "$service_user" | cut -d: -f6)"
    shell="$(getent passwd "$service_user" | cut -d: -f7)"
    [ "$home" = "$state_dir" ] || return 1
    case "$shell" in /usr/sbin/nologin | /sbin/nologin | /usr/bin/false | /bin/false) return 0 ;; esac
    return 1
}

install_all() {
    [ -f "$unit_src" ] || die "missing $unit_src (run install.sh from the repository)"
    [ -f "$polkit_src" ] || die "missing $polkit_src"
    [ -n "$binary" ] || binary="$(default_binary)"
    [ -x "$binary" ] || die "missing or not executable: $binary (run: make build, or pass --binary PATH)"

    echo "==> user"
    if getent passwd "$service_user" >/dev/null; then
        echo "user $service_user exists; reusing it"
    else
        useradd --system --no-create-home --home-dir "$state_dir" \
            --shell /usr/sbin/nologin "$service_user"
        echo "created system user $service_user"
    fi

    echo "==> binary"
    install -m 0755 "$binary" "$bin_dst"

    echo "==> systemd unit"
    install -m 0644 "$unit_src" "$unit_dst"

    echo "==> polkit rule"
    if [ -d /etc/polkit-1/rules.d ]; then
        install -m 0644 "$polkit_src" "$polkit_dst"
    else
        echo "!! /etc/polkit-1/rules.d is missing: resolvectl may ask for"
        echo "   authorization the daemon cannot give (DNS will fall back)"
    fi

    echo "==> config"
    mkdir -p "$config_dir"
    if [ -f "$config_file" ]; then
        echo "$config_file exists; leaving it untouched"
    else
        cat >"$config_file" <<'EOF'
# Kumiho admin defaults (PLAN.md §8). Every key is optional; runtime
# settings changed from the TUI are persisted as overrides in
# /var/lib/kumiho/state.json and take precedence. Examples:
#
#   socks_port    = 1080
#   kill_switch   = "last"        # "on", "off" or "last" (remember)
#   autoconnect   = "last"
#   doh_provider  = "automatic"   # cloudflare | google | quad9 | off
#   dns_upstream  = ""            # resolver IP for DoH; "" = provider default
#   mtu           = 8500
#   allow_lan     = true
#   log_level     = "info"
EOF
    fi

    echo "==> systemd"
    systemctl daemon-reload
    systemctl enable kumiho.service >/dev/null
    if systemctl is-active --quiet kumiho.service; then
        echo "kumiho.service is running; restarting with the new binary"
        systemctl restart kumiho.service
    fi

    if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ]; then
        if id -nG "$SUDO_USER" 2>/dev/null | tr ' ' '\n' | grep -qx "$service_user"; then
            :
        else
            usermod -aG "$service_user" "$SUDO_USER"
            echo "added $SUDO_USER to group $service_user (log out and back in)"
        fi
    fi

    if systemctl is-active --quiet kumiho.service; then
        echo "installed; kumiho.service is enabled and running"
    else
        echo "installed; kumiho.service is enabled for boot"
        echo "next steps:"
        echo "  sudo systemctl start kumiho"
        echo "  kumiho login    # as ${SUDO_USER:-your user}"
        echo "  kumiho doctor"
    fi
}

uninstall_all() {
    echo "==> stopping service"
    systemctl disable --now kumiho.service >/dev/null 2>&1 || true
    rm -f "$unit_dst" "$polkit_dst" "$bin_dst"
    systemctl daemon-reload 2>/dev/null || true

    echo "==> config"
    rm -rf "$config_dir"

    if [ "$purge" = 1 ]; then
        echo "==> purge: state and user"
        rm -rf "$state_dir"
        if getent passwd "$service_user" >/dev/null; then
            if user_is_installer_created; then
                userdel "$service_user"
                echo "removed user $service_user"
            else
                echo "kept user $service_user (looks like a regular account,"
                echo "not one created by this installer)"
            fi
        fi
    else
        echo "kept $state_dir (tokens and state); use --purge to remove it"
    fi
    echo "uninstalled"
}

case "$mode" in
    install) install_all ;;
    uninstall) uninstall_all ;;
esac
