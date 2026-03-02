#!/bin/bash
# ================================================================
# Speeduino Dash — Kiosk Mode Setup
# ================================================================
# Sets up full kiosk mode on a headless Raspberry Pi (no desktop
# environment required). Installs minimal X11, Chromium, Plymouth
# boot splash, and auto-login with xinit.
#
# Run with: sudo bash deploy/setup-kiosk.sh
# ================================================================
set -euo pipefail

DASH_USER="${SUDO_USER:-$(whoami)}"
DASH_GROUP="$(id -gn "$DASH_USER")"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
THEME_DIR="/usr/share/plymouth/themes/speeduino-dash"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'
YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'

info()  { echo -e "  ${GREEN}✓${NC} $1"; }
warn()  { echo -e "  ${YELLOW}⚠${NC} $1"; }
err()   { echo -e "  ${RED}✗${NC} $1"; }

echo ""
echo -e "${CYAN}${BOLD}"
echo "  ╔═══════════════════════════════════════════╗"
echo "  ║    Speeduino Dash — Kiosk Mode Setup      ║"
echo "  ╚═══════════════════════════════════════════╝"
echo -e "${NC}"
echo -e "  User: ${BOLD}${DASH_USER}${NC}"
echo ""

# Detect boot config paths (Bookworm+ uses /boot/firmware/)
BOOT_CONFIG="/boot/config.txt"
BOOT_CMDLINE="/boot/cmdline.txt"
[[ -f "/boot/firmware/config.txt" ]]  && BOOT_CONFIG="/boot/firmware/config.txt"  || true
[[ -f "/boot/firmware/cmdline.txt" ]] && BOOT_CMDLINE="/boot/firmware/cmdline.txt" || true

# ── Step 1: Install minimal X11, Chromium, and kiosk deps ──────
echo -e "${CYAN}${BOLD}[1/5] Installing kiosk packages...${NC}"
echo "  This may take a few minutes on first run."
echo ""

apt-get update -qq

# Install minimal X server + xinit (no desktop environment)
apt-get install -y --no-install-recommends \
    xserver-xorg-core \
    xserver-xorg-input-libinput \
    xinit \
    x11-xserver-utils \
    unclutter \
    2>/dev/null || true

# Install Chromium (package name varies by distro)
if ! command -v chromium-browser &>/dev/null && ! command -v chromium &>/dev/null; then
    apt-get install -y --no-install-recommends chromium-browser 2>/dev/null || \
        apt-get install -y --no-install-recommends chromium 2>/dev/null || true
fi

# Verify we got a browser
CHROMIUM=""
for bin in chromium-browser chromium; do
    if command -v "$bin" &>/dev/null; then
        CHROMIUM="$bin"
        break
    fi
done

if [[ -z "$CHROMIUM" ]]; then
    err "Could not install Chromium. Please install manually:"
    err "  sudo apt-get install chromium-browser"
    exit 1
fi
info "Chromium installed: $CHROMIUM"
info "X11 minimal server installed"

# ── Step 2: Install kiosk launcher script ──────────────────────
echo ""
echo -e "${CYAN}${BOLD}[2/5] Installing kiosk launcher...${NC}"

KIOSK_SCRIPT="/usr/local/bin/speeduino-kiosk.sh"
cp "$SCRIPT_DIR/kiosk.sh" "$KIOSK_SCRIPT"
chmod +x "$KIOSK_SCRIPT"
info "Installed $KIOSK_SCRIPT"

# Configure auto-start via .bash_profile (only on tty1, only if no X)
BASH_PROFILE="/home/$DASH_USER/.bash_profile"
KIOSK_MARKER="# speeduino-kiosk-autostart"

if ! grep -q "$KIOSK_MARKER" "$BASH_PROFILE" 2>/dev/null; then
    cat >> "$BASH_PROFILE" <<EOF

$KIOSK_MARKER
if [[ -z "\$DISPLAY" && "\$(tty)" == "/dev/tty1" ]]; then
    exec xinit $KIOSK_SCRIPT -- :0 vt1 2>/dev/null
fi
EOF
    chown "$DASH_USER:$DASH_GROUP" "$BASH_PROFILE"
    info "Auto-start added to .bash_profile (tty1 only)"
else
    info "Auto-start already configured in .bash_profile"
fi

# Allow the user to start X without root
if [[ -f /etc/X11/Xwrapper.config ]]; then
    sed -i 's/allowed_users=console/allowed_users=anybody/' /etc/X11/Xwrapper.config 2>/dev/null || true
else
    mkdir -p /etc/X11
    echo "allowed_users=anybody" > /etc/X11/Xwrapper.config
fi
info "X11 permissions configured"

# ── Step 3: Plymouth boot splash ──────────────────────────────
echo ""
echo -e "${CYAN}${BOLD}[3/5] Setting up boot splash...${NC}"

if [[ -f "$SCRIPT_DIR/splash.png" && -f "$SCRIPT_DIR/plymouth/speeduino-dash.plymouth" ]]; then
    apt-get install -y --no-install-recommends plymouth plymouth-themes 2>/dev/null || true
    mkdir -p "$THEME_DIR"
    cp "$SCRIPT_DIR/splash.png" "$THEME_DIR/"
    cp "$SCRIPT_DIR/plymouth/speeduino-dash.plymouth" "$THEME_DIR/"
    [[ -f "$SCRIPT_DIR/plymouth/speeduino-dash.script" ]] && \
        cp "$SCRIPT_DIR/plymouth/speeduino-dash.script" "$THEME_DIR/" || true
    cp "$SCRIPT_DIR/plymouth/"*.png "$THEME_DIR/" 2>/dev/null || true

    plymouth-set-default-theme -R goefidash 2>/dev/null || \
        update-alternatives --install /usr/share/plymouth/themes/default.plymouth \
            default.plymouth "$THEME_DIR/speeduino-dash.plymouth" 100 2>/dev/null || true
    update-initramfs -u 2>/dev/null || true
    info "Plymouth boot splash installed"
else
    warn "Plymouth assets not found, skipping boot splash"
fi

# ── Step 4: Clean boot config ─────────────────────────────────
echo ""
echo -e "${CYAN}${BOLD}[4/5] Configuring clean boot...${NC}"

if [[ -f "$BOOT_CMDLINE" ]]; then
    for flag in "quiet" "splash" "plymouth.ignore-serial-consoles" "logo.nologo" "vt.global_cursor_default=0"; do
        if ! grep -q "$flag" "$BOOT_CMDLINE"; then
            sed -i "s/$/ $flag/" "$BOOT_CMDLINE"
        fi
    done
    sed -i 's/console=tty1//g' "$BOOT_CMDLINE"
    info "Updated $BOOT_CMDLINE"
fi

if [[ -f "$BOOT_CONFIG" ]]; then
    if ! grep -q "disable_splash=1" "$BOOT_CONFIG"; then
        echo "" >> "$BOOT_CONFIG"
        echo "# Speeduino Dash — clean boot" >> "$BOOT_CONFIG"
        echo "disable_splash=1" >> "$BOOT_CONFIG"
    fi
    info "Updated $BOOT_CONFIG"
fi

# ── Step 5: Auto-login on tty1 ────────────────────────────────
echo ""
echo -e "${CYAN}${BOLD}[5/5] Setting up auto-login...${NC}"

AUTOLOGIN_DIR="/etc/systemd/system/getty@tty1.service.d"
mkdir -p "$AUTOLOGIN_DIR"
cat > "$AUTOLOGIN_DIR/autologin.conf" <<EOF
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin $DASH_USER --noclear %I \$TERM
EOF
systemctl daemon-reload
info "Auto-login enabled for $DASH_USER on tty1"

# Remove old user-level kiosk service if it exists (superseded by xinit approach)
OLD_SERVICE="/home/$DASH_USER/.config/systemd/user/speeduino-kiosk.service"
if [[ -f "$OLD_SERVICE" ]]; then
    sudo -u "$DASH_USER" systemctl --user disable speeduino-kiosk.service 2>/dev/null || true
    rm -f "$OLD_SERVICE"
    info "Removed old user-level kiosk service (replaced by xinit)"
fi

# ── Summary ────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}${BOLD}=== Kiosk setup complete ===${NC}"
echo ""
echo -e "  ${BOLD}Boot splash:${NC}   Plymouth 'speeduino-dash' theme"
echo -e "  ${BOLD}Auto-login:${NC}    $DASH_USER on tty1"
echo -e "  ${BOLD}Kiosk browser:${NC} xinit → Chromium fullscreen"
echo -e "  ${BOLD}Launcher:${NC}      $KIOSK_SCRIPT"
echo ""
echo -e "  ${BOLD}How it works:${NC}"
echo -e "    1. Pi boots → auto-login on tty1"
echo -e "    2. .bash_profile detects tty1 → starts xinit"
echo -e "    3. xinit starts minimal X server → runs kiosk.sh"
echo -e "    4. Chromium opens fullscreen to http://localhost:8080"
echo ""
echo -e "  ${BOLD}Useful commands:${NC}"
echo -e "    sudo systemctl status speeduino-dash   # Dashboard service"
echo -e "    Ctrl+Alt+F2                             # Switch to tty2 for shell"
echo -e "    sudo systemctl restart getty@tty1       # Restart kiosk"
echo ""
echo -e "  ${YELLOW}Reboot to see the full boot → kiosk experience.${NC}"
echo ""
