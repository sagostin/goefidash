#!/bin/bash
# Speeduino Dash — Boot Appearance & Kiosk Setup
# Run with: sudo bash deploy/setup-kiosk.sh
#
# This script:
#   1. Installs the Plymouth boot splash theme
#   2. Configures clean boot (no rainbow, no text, no cursor)
#   3. Sets up auto-login on tty1
#   4. Installs a user-level systemd service for Chromium kiosk
#
set -e

DASH_USER="${SUDO_USER:-$(whoami)}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
THEME_DIR="/usr/share/plymouth/themes/speeduino-dash"

echo "=== Speeduino Dash — Kiosk Setup ==="
echo "    User: $DASH_USER"

# ----------------------------------------------------------------
# 1. Plymouth Boot Splash
# ----------------------------------------------------------------
echo "[1/4] Installing Plymouth boot splash..."
sudo apt-get install -y --no-install-recommends plymouth plymouth-themes

sudo mkdir -p "$THEME_DIR"
sudo cp "$SCRIPT_DIR/splash.png" "$THEME_DIR/"
sudo cp "$SCRIPT_DIR/plymouth/speeduino-dash.plymouth" "$THEME_DIR/"
sudo cp "$SCRIPT_DIR/plymouth/speeduino-dash.script" "$THEME_DIR/"

# Register and set as default theme
sudo plymouth-set-default-theme -R speeduino-dash 2>/dev/null || \
    sudo update-alternatives --install /usr/share/plymouth/themes/default.plymouth \
        default.plymouth "$THEME_DIR/speeduino-dash.plymouth" 100

sudo update-initramfs -u

echo "    Plymouth theme installed."

# ----------------------------------------------------------------
# 2. Clean Boot Configuration
# ----------------------------------------------------------------
echo "[2/4] Configuring clean boot..."

# /boot/cmdline.txt — quiet + splash + no logo + no cursor
CMDLINE="/boot/cmdline.txt"
if [ -f "/boot/firmware/cmdline.txt" ]; then
    CMDLINE="/boot/firmware/cmdline.txt"
fi

if [ -f "$CMDLINE" ]; then
    # Add splash flags if not already present
    for flag in "quiet" "splash" "plymouth.ignore-serial-consoles" "logo.nologo" "vt.global_cursor_default=0"; do
        if ! grep -q "$flag" "$CMDLINE"; then
            sudo sed -i "s/$/ $flag/" "$CMDLINE"
        fi
    done
    # Remove 'console=tty1' verbose output if present
    sudo sed -i 's/console=tty1//g' "$CMDLINE"
    echo "    Updated $CMDLINE"
fi

# /boot/config.txt — disable rainbow splash
CONFIG="/boot/config.txt"
if [ -f "/boot/firmware/config.txt" ]; then
    CONFIG="/boot/firmware/config.txt"
fi

if [ -f "$CONFIG" ]; then
    if ! grep -q "disable_splash=1" "$CONFIG"; then
        echo "" | sudo tee -a "$CONFIG" > /dev/null
        echo "# Speeduino Dash — clean boot" | sudo tee -a "$CONFIG" > /dev/null
        echo "disable_splash=1" | sudo tee -a "$CONFIG" > /dev/null
    fi
    echo "    Updated $CONFIG"
fi

# ----------------------------------------------------------------
# 3. Auto-Login on tty1
# ----------------------------------------------------------------
echo "[3/4] Setting up auto-login for $DASH_USER..."
AUTOLOGIN_DIR="/etc/systemd/system/getty@tty1.service.d"
sudo mkdir -p "$AUTOLOGIN_DIR"
cat <<EOF | sudo tee "$AUTOLOGIN_DIR/autologin.conf" > /dev/null
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin $DASH_USER --noclear %I \$TERM
EOF
sudo systemctl daemon-reload
echo "    Auto-login enabled for $DASH_USER on tty1."

# ----------------------------------------------------------------
# 4. Kiosk Chromium Service (user-level systemd)
# ----------------------------------------------------------------
echo "[4/4] Installing kiosk browser service..."
USER_SYSTEMD_DIR="/home/$DASH_USER/.config/systemd/user"
sudo -u "$DASH_USER" mkdir -p "$USER_SYSTEMD_DIR"

cat <<EOF | sudo -u "$DASH_USER" tee "$USER_SYSTEMD_DIR/speeduino-kiosk.service" > /dev/null
[Unit]
Description=Speeduino Dashboard Kiosk Browser
After=graphical-session.target
Wants=speeduino-dash.service

[Service]
Type=simple
Environment=DISPLAY=:0
ExecStartPre=/bin/bash -c 'for i in \$(seq 1 30); do curl -s -o /dev/null http://localhost:8080 && break; sleep 1; done'
ExecStart=/usr/bin/chromium-browser --kiosk --noerrdialogs --disable-infobars --disable-session-crashed-bubble --disable-translate --no-first-run --start-fullscreen --incognito --disable-pinch --overscroll-history-navigation=0 http://localhost:8080
Restart=on-failure
RestartSec=5

[Install]
WantedBy=graphical-session.target
EOF

# Enable lingering so user services start at boot
sudo loginctl enable-linger "$DASH_USER"
sudo -u "$DASH_USER" systemctl --user daemon-reload
sudo -u "$DASH_USER" systemctl --user enable speeduino-kiosk.service

echo ""
echo "=== Kiosk setup complete ==="
echo ""
echo "  Boot splash:  Plymouth 'speeduino-dash' theme"
echo "  Auto-login:   $DASH_USER on tty1"
echo "  Kiosk browser: speeduino-kiosk.service (user systemd)"
echo ""
echo "  To test Plymouth:   sudo plymouthd; sudo plymouth --show-splash"
echo "  To disable kiosk:   systemctl --user disable speeduino-kiosk"
echo ""
echo "  Reboot to see the full boot experience."
echo ""
