#!/bin/bash
# ================================================================
# Speeduino Dash — Raspberry Pi Interactive Setup
# ================================================================
# One-stop interactive installer for the full dashboard stack:
#   • Binary installation
#   • ECU serial configuration (UART / USB / custom)
#   • GPS configuration (USB udev / disabled / custom)
#   • Display units & layout
#   • udev rules (auto-generated)
#   • systemd service
#   • Log rotation
#   • Optional kiosk mode (Plymouth + Chromium)
#
# Usage:  sudo bash deploy/rpi-setup.sh
# ================================================================
set -euo pipefail

# ── Colours & helpers ───────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'
YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'

banner()  { echo -e "\n${CYAN}${BOLD}$1${NC}"; }
info()    { echo -e "  ${GREEN}✓${NC} $1"; }
warn()    { echo -e "  ${YELLOW}⚠${NC} $1"; }
err()     { echo -e "  ${RED}✗${NC} $1"; }
ask()     { echo -en "  ${BOLD}$1${NC} "; }

prompt_default() {
    # prompt_default "Label" "default" → sets REPLY
    local label="$1" default="$2"
    ask "$label [${default}]:"
    read -r REPLY
    REPLY="${REPLY:-$default}"
}

confirm() {
    ask "$1 [Y/n]:"
    read -r REPLY
    [[ "${REPLY:-Y}" =~ ^[Yy]$ ]]
}

# ── Pre-flight checks ──────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    err "This script must be run as root (use sudo)."
    exit 1
fi

if [[ "$(uname -s)" != "Linux" ]]; then
    err "This script is intended for Linux (Raspberry Pi)."
    exit 1
fi

DASH_USER="${SUDO_USER:-$(whoami)}"
DASH_GROUP="$(id -gn "$DASH_USER")"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/speeduino-dash"
LOG_DIR="/var/log/speeduino-dash"
SYSTEMD_DIR="/etc/systemd/system"
UDEV_DIR="/etc/udev/rules.d"
UDEV_FILE="$UDEV_DIR/99-speeduino.rules"

# Detect boot config paths (Pi OS Bookworm moves them under /boot/firmware/)
BOOT_CONFIG="/boot/config.txt"
BOOT_CMDLINE="/boot/cmdline.txt"
[[ -f "/boot/firmware/config.txt" ]]  && BOOT_CONFIG="/boot/firmware/config.txt"
[[ -f "/boot/firmware/cmdline.txt" ]] && BOOT_CMDLINE="/boot/firmware/cmdline.txt"

# ── Banner ──────────────────────────────────────────────────────
clear 2>/dev/null || true
echo ""
echo -e "${CYAN}${BOLD}"
echo "  ╔═══════════════════════════════════════════╗"
echo "  ║       Speeduino Dash — RPi Setup          ║"
echo "  ║       Interactive Deployment Kit           ║"
echo "  ╚═══════════════════════════════════════════╝"
echo -e "${NC}"
echo -e "  Installing for user: ${BOLD}${DASH_USER}${NC}"
echo ""

# Track what we configure for the summary
ECU_PORT=""
ECU_BAUD="115200"
ECU_PROTOCOL="auto"
GPS_TYPE="disabled"
GPS_PORT=""
GPS_BAUD="9600"
TEMP_UNIT="C"
SPEED_UNIT="kph"
PRESSURE_UNIT="psi"
LAYOUT="classic"
SETUP_KIOSK=false
UDEV_RULES=""

# ================================================================
# STEP 1 — Binary
# ================================================================
banner "STEP 1 — Binary Installation"

if [[ -f "$INSTALL_DIR/speeduino-dash" ]]; then
    EXISTING_VER=$("$INSTALL_DIR/speeduino-dash" --version 2>/dev/null || echo "unknown")
    warn "Existing installation found ($EXISTING_VER)"
fi

if [[ -f "${SCRIPT_DIR}/../speeduino-dash" ]]; then
    BINARY="${SCRIPT_DIR}/../speeduino-dash"
    info "Found pre-built binary: $BINARY"
elif [[ -f "speeduino-dash" ]]; then
    BINARY="./speeduino-dash"
    info "Found pre-built binary in current directory"
elif [[ -f "go.mod" ]]; then
    warn "No pre-built binary found. Building from source..."
    GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
        go build -ldflags "-s -w" -o speeduino-dash ./cmd/speeduino-dash/
    BINARY="./speeduino-dash"
    info "Built successfully"
else
    err "No binary found and no Go source available."
    err "Please build first with: ./build-pi.sh"
    exit 1
fi

cp "$BINARY" "$INSTALL_DIR/speeduino-dash"
chmod +x "$INSTALL_DIR/speeduino-dash"
info "Installed to $INSTALL_DIR/speeduino-dash"

# ================================================================
# STEP 2 — ECU / Speeduino Serial Connection
# ================================================================
banner "STEP 2 — Speeduino ECU Connection"
echo ""
echo -e "  How is your Speeduino connected?"
echo -e "    ${BOLD}1${NC}) Pi UART (${CYAN}/dev/ttyAMA0${NC}) — secondary serial via GPIO"
echo -e "    ${BOLD}2${NC}) USB serial adapter — auto-detect vendor/product for udev"
echo -e "    ${BOLD}3${NC}) Custom device path"
echo ""
ask "Select [1/2/3]:"
read -r ECU_CHOICE

case "${ECU_CHOICE:-1}" in
    1)
        # ── Pi UART (ttyAMA0) ──────────────────────────────────
        ECU_PORT="/dev/ttyAMA0"
        info "Using Pi UART: $ECU_PORT"

        # Enable UART overlay in boot config
        if ! grep -q "^enable_uart=1" "$BOOT_CONFIG" 2>/dev/null; then
            echo "" >> "$BOOT_CONFIG"
            echo "# Speeduino Dash — enable UART for ECU" >> "$BOOT_CONFIG"
            echo "enable_uart=1" >> "$BOOT_CONFIG"
            info "Enabled UART in $BOOT_CONFIG"
        else
            info "UART already enabled in $BOOT_CONFIG"
        fi

        # Free ttyAMA0 from Bluetooth (Pi 3/4/5 uses it for BT by default)
        if ! grep -q "^dtoverlay=disable-bt" "$BOOT_CONFIG" 2>/dev/null; then
            echo "dtoverlay=disable-bt" >> "$BOOT_CONFIG"
            info "Disabled Bluetooth overlay to free ttyAMA0"
        else
            info "Bluetooth overlay already disabled"
        fi

        # Disable serial console in cmdline.txt
        if grep -q "console=serial0" "$BOOT_CMDLINE" 2>/dev/null; then
            sed -i 's/console=serial0,[0-9]* //g' "$BOOT_CMDLINE"
            info "Removed serial console from $BOOT_CMDLINE"
        elif grep -q "console=ttyAMA0" "$BOOT_CMDLINE" 2>/dev/null; then
            sed -i 's/console=ttyAMA0,[0-9]* //g' "$BOOT_CMDLINE"
            info "Removed serial console from $BOOT_CMDLINE"
        else
            info "Serial console not found in cmdline (OK)"
        fi

        # Disable serial getty service
        systemctl disable serial-getty@ttyAMA0.service 2>/dev/null || true
        systemctl stop serial-getty@ttyAMA0.service 2>/dev/null || true
        info "Disabled serial-getty on ttyAMA0"

        # Create symlink via udev
        UDEV_RULES+='# Speeduino ECU on Pi UART\n'
        UDEV_RULES+='KERNEL=="ttyAMA0", SYMLINK+="ttySpeeduino"\n\n'
        ;;

    2)
        # ── USB serial adapter ─────────────────────────────────
        echo ""
        info "Scanning for USB serial devices..."
        echo ""

        # List available ttyUSB devices
        USB_DEVICES=()
        if ls /dev/ttyUSB* 2>/dev/null 1>&2; then
            for dev in /dev/ttyUSB*; do
                VENDOR=$(udevadm info -a -n "$dev" 2>/dev/null | grep -m1 'ATTRS{idVendor}' | sed 's/.*=="\(.*\)"/\1/')
                PRODUCT=$(udevadm info -a -n "$dev" 2>/dev/null | grep -m1 'ATTRS{idProduct}' | sed 's/.*=="\(.*\)"/\1/')
                SERIAL_NUM=$(udevadm info -a -n "$dev" 2>/dev/null | grep -m1 'ATTRS{serial}' | sed 's/.*=="\(.*\)"/\1/')
                MFGR=$(udevadm info -a -n "$dev" 2>/dev/null | grep -m1 'ATTRS{manufacturer}' | sed 's/.*=="\(.*\)"/\1/')
                echo -e "    ${BOLD}${dev}${NC}  —  ${MFGR:-unknown} (${VENDOR:-????}:${PRODUCT:-????}) serial=${SERIAL_NUM:-n/a}"
                USB_DEVICES+=("$dev")
            done
        fi

        if [[ ${#USB_DEVICES[@]} -eq 0 ]]; then
            warn "No USB serial devices found. Plug in your adapter and re-run,"
            warn "or enter a custom path."
            prompt_default "ECU device path" "/dev/ttyUSB0"
            ECU_PORT="$REPLY"
        else
            echo ""
            prompt_default "Select ECU device" "${USB_DEVICES[0]}"
            ECU_PORT="$REPLY"
        fi

        # Get vendor/product IDs for the selected device
        VENDOR=$(udevadm info -a -n "$ECU_PORT" 2>/dev/null | grep -m1 'ATTRS{idVendor}' | sed 's/.*=="\(.*\)"/\1/')
        PRODUCT=$(udevadm info -a -n "$ECU_PORT" 2>/dev/null | grep -m1 'ATTRS{idProduct}' | sed 's/.*=="\(.*\)"/\1/')
        SERIAL_NUM=$(udevadm info -a -n "$ECU_PORT" 2>/dev/null | grep -m1 'ATTRS{serial}' | sed 's/.*=="\(.*\)"/\1/')

        if [[ -n "$VENDOR" && -n "$PRODUCT" ]]; then
            info "Detected: vendor=$VENDOR product=$PRODUCT serial=${SERIAL_NUM:-n/a}"
            UDEV_RULES+='# Speeduino ECU — USB serial adapter\n'
            if [[ -n "$SERIAL_NUM" ]]; then
                UDEV_RULES+="ACTION==\"add\", SUBSYSTEM==\"tty\", ATTRS{idVendor}==\"${VENDOR}\", ATTRS{idProduct}==\"${PRODUCT}\", ATTRS{serial}==\"${SERIAL_NUM}\", SYMLINK+=\"ttySpeeduino\"\n\n"
            else
                UDEV_RULES+="ACTION==\"add\", SUBSYSTEM==\"tty\", ATTRS{idVendor}==\"${VENDOR}\", ATTRS{idProduct}==\"${PRODUCT}\", SYMLINK+=\"ttySpeeduino\"\n\n"
            fi
            ECU_PORT="/dev/ttySpeeduino"
            info "Will create udev symlink: /dev/ttySpeeduino"
        else
            warn "Could not detect USB IDs. Using direct path: $ECU_PORT"
            warn "You may need to manually create a udev rule later."
        fi
        ;;

    3)
        # ── Custom path ────────────────────────────────────────
        prompt_default "ECU device path" "/dev/ttyUSB0"
        ECU_PORT="$REPLY"
        info "Using custom path: $ECU_PORT"
        ;;

    *)
        err "Invalid selection. Defaulting to /dev/ttyAMA0"
        ECU_PORT="/dev/ttyAMA0"
        ;;
esac

# ECU baud & protocol
echo ""
prompt_default "ECU baud rate" "115200"
ECU_BAUD="$REPLY"

echo ""
echo -e "  ECU protocol mode:"
echo -e "    ${BOLD}auto${NC}      — auto-detect (recommended)"
echo -e "    ${BOLD}secondary${NC} — force secondary serial (plain commands)"
echo -e "    ${BOLD}msenvelope${NC} — force TunerStudio protocol"
prompt_default "Protocol" "auto"
ECU_PROTOCOL="$REPLY"

info "ECU: $ECU_PORT @ ${ECU_BAUD} baud (protocol: $ECU_PROTOCOL)"

# ================================================================
# STEP 3 — GPS
# ================================================================
banner "STEP 3 — GPS Module"
echo ""
echo -e "  GPS module configuration:"
echo -e "    ${BOLD}1${NC}) USB GPS module — auto-detect for udev symlink"
echo -e "    ${BOLD}2${NC}) No GPS / disabled"
echo -e "    ${BOLD}3${NC}) Custom device path"
echo ""
ask "Select [1/2/3]:"
read -r GPS_CHOICE

case "${GPS_CHOICE:-2}" in
    1)
        # ── USB GPS ────────────────────────────────────────────
        GPS_TYPE="nmea"
        echo ""
        info "Scanning for USB serial devices..."
        echo ""

        USB_DEVICES=()
        GPS_SCAN_DEVS=()
        for d in /dev/ttyUSB* /dev/ttyACM*; do [[ -e "$d" ]] && GPS_SCAN_DEVS+=("$d"); done
        if [[ ${#GPS_SCAN_DEVS[@]} -gt 0 ]]; then
            for dev in "${GPS_SCAN_DEVS[@]}"; do
                VENDOR=$(udevadm info -a -n "$dev" 2>/dev/null | grep -m1 'ATTRS{idVendor}' | sed 's/.*=="\(.*\)"/\1/')
                PRODUCT=$(udevadm info -a -n "$dev" 2>/dev/null | grep -m1 'ATTRS{idProduct}' | sed 's/.*=="\(.*\)"/\1/')
                MFGR=$(udevadm info -a -n "$dev" 2>/dev/null | grep -m1 'ATTRS{manufacturer}' | sed 's/.*=="\(.*\)"/\1/')
                echo -e "    ${BOLD}${dev}${NC}  —  ${MFGR:-unknown} (${VENDOR:-????}:${PRODUCT:-????})"
                USB_DEVICES+=("$dev")
            done
        fi

        if [[ ${#USB_DEVICES[@]} -eq 0 ]]; then
            warn "No USB serial devices found for GPS."
            prompt_default "GPS device path" "/dev/ttyUSB1"
            GPS_PORT="$REPLY"
        else
            echo ""
            # Default to second device if available (first is likely ECU)
            DEFAULT_GPS="${USB_DEVICES[0]}"
            [[ ${#USB_DEVICES[@]} -gt 1 ]] && DEFAULT_GPS="${USB_DEVICES[1]}"
            prompt_default "Select GPS device" "$DEFAULT_GPS"
            GPS_PORT="$REPLY"
        fi

        # Auto-detect vendor/product for udev rule
        VENDOR=$(udevadm info -a -n "$GPS_PORT" 2>/dev/null | grep -m1 'ATTRS{idVendor}' | sed 's/.*=="\(.*\)"/\1/')
        PRODUCT=$(udevadm info -a -n "$GPS_PORT" 2>/dev/null | grep -m1 'ATTRS{idProduct}' | sed 's/.*=="\(.*\)"/\1/')
        SERIAL_NUM=$(udevadm info -a -n "$GPS_PORT" 2>/dev/null | grep -m1 'ATTRS{serial}' | sed 's/.*=="\(.*\)"/\1/')

        if [[ -n "$VENDOR" && -n "$PRODUCT" ]]; then
            info "Detected GPS: vendor=$VENDOR product=$PRODUCT"
            UDEV_RULES+='# GPS module — USB\n'
            if [[ -n "$SERIAL_NUM" ]]; then
                UDEV_RULES+="ACTION==\"add\", SUBSYSTEM==\"tty\", ATTRS{idVendor}==\"${VENDOR}\", ATTRS{idProduct}==\"${PRODUCT}\", ATTRS{serial}==\"${SERIAL_NUM}\", SYMLINK+=\"ttyGPS\"\n\n"
            else
                UDEV_RULES+="ACTION==\"add\", SUBSYSTEM==\"tty\", ATTRS{idVendor}==\"${VENDOR}\", ATTRS{idProduct}==\"${PRODUCT}\", SYMLINK+=\"ttyGPS\"\n\n"
            fi
            GPS_PORT="/dev/ttyGPS"
            info "Will create udev symlink: /dev/ttyGPS"
        else
            warn "Could not detect USB IDs for GPS. Using direct path: $GPS_PORT"
        fi

        prompt_default "GPS baud rate" "9600"
        GPS_BAUD="$REPLY"
        info "GPS: $GPS_PORT @ ${GPS_BAUD} baud"
        ;;

    2)
        GPS_TYPE="disabled"
        GPS_PORT=""
        info "GPS disabled"
        ;;

    3)
        GPS_TYPE="nmea"
        prompt_default "GPS device path" "/dev/ttyUSB1"
        GPS_PORT="$REPLY"
        prompt_default "GPS baud rate" "9600"
        GPS_BAUD="$REPLY"
        info "GPS: $GPS_PORT @ ${GPS_BAUD} baud"
        ;;

    *)
        GPS_TYPE="disabled"
        GPS_PORT=""
        warn "Invalid selection. GPS disabled."
        ;;
esac

# ================================================================
# STEP 4 — Display & Units
# ================================================================
banner "STEP 4 — Display Configuration"
echo ""

echo -e "  Temperature unit:"
echo -e "    ${BOLD}C${NC} — Celsius    ${BOLD}F${NC} — Fahrenheit"
prompt_default "Temperature" "C"
TEMP_UNIT="$REPLY"

echo -e "  Speed unit:"
echo -e "    ${BOLD}kph${NC} — km/h    ${BOLD}mph${NC} — miles/h"
prompt_default "Speed" "kph"
SPEED_UNIT="$REPLY"

echo -e "  Pressure unit:"
echo -e "    ${BOLD}psi${NC}    ${BOLD}kpa${NC}    ${BOLD}bar${NC}"
prompt_default "Pressure" "psi"
PRESSURE_UNIT="$REPLY"

echo -e "  Dashboard layout:"
echo -e "    ${BOLD}classic${NC}  ${BOLD}sweep${NC}  ${BOLD}race${NC}  ${BOLD}minimal${NC}"
prompt_default "Layout" "classic"
LAYOUT="$REPLY"

info "Display: ${TEMP_UNIT}° / ${SPEED_UNIT} / ${PRESSURE_UNIT} / ${LAYOUT}"

# ================================================================
# STEP 5 — Generate config.yaml
# ================================================================
banner "STEP 5 — Configuration File"

mkdir -p "$CONFIG_DIR"

CONFIG_FILE="$CONFIG_DIR/config.yaml"
WRITE_CONFIG=true

if [[ -f "$CONFIG_FILE" ]]; then
    warn "Existing config found at $CONFIG_FILE"
    if ! confirm "Overwrite with new settings?"; then
        WRITE_CONFIG=false
        info "Keeping existing config"
    fi
fi

if $WRITE_CONFIG; then
    # Build GPS section
    GPS_YAML=""
    if [[ "$GPS_TYPE" == "nmea" ]]; then
        GPS_YAML=$(cat <<EOF

gps:
  type: nmea
  port_path: ${GPS_PORT}
  baud_rate: ${GPS_BAUD}
EOF
)
    else
        GPS_YAML=$(cat <<EOF

gps:
  type: disabled
  port_path: ""
  baud_rate: 9600
EOF
)
    fi

    cat > "$CONFIG_FILE" <<EOF
# ================================================================
# Speeduino Dash — Configuration
# Generated by rpi-setup.sh on $(date '+%Y-%m-%d %H:%M')
# ================================================================

ecu:
  type: speeduino
  port_path: ${ECU_PORT}
  baud_rate: ${ECU_BAUD}
  can_id: 0
  stoich: 14.7
  poll_hz: 20
  protocol: ${ECU_PROTOCOL}
${GPS_YAML}

display:
  layout: ${LAYOUT}
  units:
    temperature: ${TEMP_UNIT}
    pressure: ${PRESSURE_UNIT}
    speed: ${SPEED_UNIT}
    afr: afr
  thresholds:
    rpm_warn: 6000
    rpm_danger: 7000
    rpm_max: 8000
    clt_warn: 95
    clt_danger: 105
    iat_warn: 60
    iat_danger: 75
    afr_lean_warn: 15.5
    afr_rich_warn: 12.0
    oil_p_warn: 15
    batt_low: 12.0
    batt_high: 15.5
    knock_warn: 3

logging:
  enabled: false
  path: /var/log/speeduino-dash
  interval_ms: 100

server:
  listen_addr: ":8080"
  kiosk: false
EOF

    chown -R "$DASH_USER:$DASH_GROUP" "$CONFIG_DIR"
    info "Config written to $CONFIG_FILE"
fi

# ================================================================
# STEP 6 — udev Rules
# ================================================================
banner "STEP 6 — udev Rules"

if [[ -n "$UDEV_RULES" ]]; then
    echo -e "$UDEV_RULES" > "$UDEV_FILE"
    udevadm control --reload-rules 2>/dev/null || true
    udevadm trigger 2>/dev/null || true
    info "udev rules written to $UDEV_FILE"
    echo ""
    echo -e "  ${CYAN}── Generated rules ──${NC}"
    cat "$UDEV_FILE" | sed 's/^/    /'
    echo ""
else
    info "No udev rules needed (using direct device paths)"
fi

# ================================================================
# STEP 7 — Log Directory & Rotation
# ================================================================
banner "STEP 7 — Logging"

mkdir -p "$LOG_DIR"
chown "$DASH_USER:$DASH_GROUP" "$LOG_DIR"
info "Log directory: $LOG_DIR"

if [[ -f "$SCRIPT_DIR/logrotate-speeduino-dash" ]]; then
    cp "$SCRIPT_DIR/logrotate-speeduino-dash" /etc/logrotate.d/speeduino-dash
    info "Log rotation installed"
fi

# ================================================================
# STEP 8 — Systemd Service
# ================================================================
banner "STEP 8 — Systemd Service"

sed "s/User=pi/User=$DASH_USER/;s/Group=pi/Group=$DASH_GROUP/" \
    "$SCRIPT_DIR/speeduino-dash.service" > "$SYSTEMD_DIR/speeduino-dash.service"

systemctl daemon-reload
systemctl enable speeduino-dash
info "Service installed and enabled"

# ================================================================
# STEP 9 — Kiosk Mode (Optional)
# ================================================================
banner "STEP 9 — Kiosk Mode (Optional)"
echo ""
echo -e "  Kiosk mode sets up:"
echo -e "    • Plymouth boot splash (custom Speeduino branding)"
echo -e "    • Auto-login on tty1"
echo -e "    • Full-screen Chromium browser on boot"
echo ""

if confirm "Enable kiosk mode?"; then
    SETUP_KIOSK=true

    # Install kiosk dependencies
    info "Installing kiosk dependencies..."
    apt-get install -y --no-install-recommends unclutter chromium-browser 2>/dev/null || \
        apt-get install -y --no-install-recommends unclutter chromium 2>/dev/null || true

    # Update config to enable kiosk
    if $WRITE_CONFIG; then
        sed -i 's/kiosk: false/kiosk: true/' "$CONFIG_FILE"
    fi

    # ── Plymouth boot splash ──
    THEME_DIR="/usr/share/plymouth/themes/speeduino-dash"
    if [[ -f "$SCRIPT_DIR/splash.png" && -f "$SCRIPT_DIR/plymouth/speeduino-dash.plymouth" ]]; then
        apt-get install -y --no-install-recommends plymouth plymouth-themes 2>/dev/null || true
        mkdir -p "$THEME_DIR"
        cp "$SCRIPT_DIR/splash.png" "$THEME_DIR/"
        cp "$SCRIPT_DIR/plymouth/speeduino-dash.plymouth" "$THEME_DIR/"
        cp "$SCRIPT_DIR/plymouth/speeduino-dash.script" "$THEME_DIR/"

        plymouth-set-default-theme -R speeduino-dash 2>/dev/null || \
            update-alternatives --install /usr/share/plymouth/themes/default.plymouth \
                default.plymouth "$THEME_DIR/speeduino-dash.plymouth" 100 2>/dev/null || true
        update-initramfs -u 2>/dev/null || true
        info "Plymouth boot splash installed"
    else
        warn "Plymouth assets not found, skipping boot splash"
    fi

    # ── Clean boot config ──
    if [[ -f "$BOOT_CMDLINE" ]]; then
        for flag in "quiet" "splash" "plymouth.ignore-serial-consoles" "logo.nologo" "vt.global_cursor_default=0"; do
            if ! grep -q "$flag" "$BOOT_CMDLINE"; then
                sed -i "s/$/ $flag/" "$BOOT_CMDLINE"
            fi
        done
        # Remove console=tty1 verbose output
        sed -i 's/console=tty1//g' "$BOOT_CMDLINE"
    fi

    if [[ -f "$BOOT_CONFIG" ]]; then
        if ! grep -q "disable_splash=1" "$BOOT_CONFIG"; then
            echo "" >> "$BOOT_CONFIG"
            echo "# Speeduino Dash — clean boot" >> "$BOOT_CONFIG"
            echo "disable_splash=1" >> "$BOOT_CONFIG"
        fi
    fi
    info "Clean boot configured"

    # ── Auto-login on tty1 ──
    AUTOLOGIN_DIR="/etc/systemd/system/getty@tty1.service.d"
    mkdir -p "$AUTOLOGIN_DIR"
    cat > "$AUTOLOGIN_DIR/autologin.conf" <<EOF
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin $DASH_USER --noclear %I \$TERM
EOF
    systemctl daemon-reload
    info "Auto-login enabled for $DASH_USER on tty1"

    # ── Kiosk Chromium service (user-level systemd) ──
    USER_SYSTEMD_DIR="/home/$DASH_USER/.config/systemd/user"
    sudo -u "$DASH_USER" mkdir -p "$USER_SYSTEMD_DIR"

    cat > "$USER_SYSTEMD_DIR/speeduino-kiosk.service" <<EOF
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

    chown -R "$DASH_USER:$DASH_GROUP" "/home/$DASH_USER/.config/systemd"
    loginctl enable-linger "$DASH_USER" 2>/dev/null || true
    sudo -u "$DASH_USER" systemctl --user daemon-reload 2>/dev/null || true
    sudo -u "$DASH_USER" systemctl --user enable speeduino-kiosk.service 2>/dev/null || true
    info "Kiosk browser service installed"
else
    info "Kiosk mode skipped"
fi

# ================================================================
# STEP 10 — Summary
# ================================================================
banner "SETUP COMPLETE"
echo ""
echo -e "  ${CYAN}── Configuration Summary ──${NC}"
echo ""
echo -e "  ${BOLD}Binary:${NC}      $INSTALL_DIR/speeduino-dash"
echo -e "  ${BOLD}Config:${NC}      $CONFIG_FILE"
echo -e "  ${BOLD}Log dir:${NC}     $LOG_DIR"
echo ""
echo -e "  ${BOLD}ECU:${NC}         $ECU_PORT @ ${ECU_BAUD} baud (${ECU_PROTOCOL})"
if [[ "$GPS_TYPE" == "nmea" ]]; then
echo -e "  ${BOLD}GPS:${NC}         $GPS_PORT @ ${GPS_BAUD} baud"
else
echo -e "  ${BOLD}GPS:${NC}         disabled"
fi
echo -e "  ${BOLD}Display:${NC}     ${LAYOUT} — ${TEMP_UNIT}° / ${SPEED_UNIT} / ${PRESSURE_UNIT}"
echo ""
if [[ -n "$UDEV_RULES" ]]; then
echo -e "  ${BOLD}udev rules:${NC}  $UDEV_FILE"
fi
echo -e "  ${BOLD}Service:${NC}     speeduino-dash.service (enabled)"
if $SETUP_KIOSK; then
echo -e "  ${BOLD}Kiosk:${NC}       enabled (Plymouth + auto-login + Chromium)"
fi
echo ""
echo -e "  ${CYAN}── Next Steps ──${NC}"
echo ""
if confirm "Start the service now?"; then
    systemctl start speeduino-dash
    info "Service started!"
    echo ""
    echo -e "  Dashboard available at: ${BOLD}http://localhost:8080${NC}"
else
    echo ""
    echo -e "  Start manually:  ${BOLD}sudo systemctl start speeduino-dash${NC}"
    echo -e "  Dashboard URL:   ${BOLD}http://localhost:8080${NC}"
fi
echo ""
echo -e "  ${BOLD}Useful commands:${NC}"
echo -e "    sudo systemctl status speeduino-dash   # Check service status"
echo -e "    sudo journalctl -u speeduino-dash -f   # Follow logs"
echo -e "    sudo nano $CONFIG_FILE         # Edit config"
if $SETUP_KIOSK; then
echo -e "    systemctl --user status speeduino-kiosk # Kiosk status"
echo ""
echo -e "  ${YELLOW}Reboot recommended to apply UART/boot changes.${NC}"
fi
echo ""
