#!/bin/bash
# Speeduino Dash — Raspberry Pi Installation Script
set -e

echo "=== Speeduino Dash Installer ==="

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/speeduino-dash"
LOG_DIR="/var/log/speeduino-dash"
SYSTEMD_DIR="/etc/systemd/system"
UDEV_DIR="/etc/udev/rules.d"

# Detect the real user (works when run via sudo)
DASH_USER="${SUDO_USER:-$(whoami)}"
DASH_GROUP="$(id -gn "$DASH_USER")"
echo "    Installing for user: $DASH_USER"

# Build if needed
if [ -f "go.mod" ]; then
    echo "[1/7] Building..."
    GOOS=linux GOARCH=arm64 go build -o speeduino-dash ./cmd/speeduino-dash/
fi

# Install binary
echo "[2/7] Installing binary..."
sudo cp speeduino-dash "$INSTALL_DIR/"
sudo chmod +x "$INSTALL_DIR/speeduino-dash"

# Install kiosk dependencies
echo "[3/7] Installing kiosk dependencies..."
sudo apt-get install -y --no-install-recommends unclutter chromium-browser || true

# Create config directory
echo "[4/7] Setting up config..."
sudo mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    cat <<'YAML' | sudo tee "$CONFIG_DIR/config.yaml" > /dev/null
ecu:
  type: speeduino
  port_path: /dev/ttySpeeduino
  baud_rate: 115200
  can_id: 0
  stoich: 14.7
  poll_hz: 20

gps:
  type: nmea
  port_path: /dev/ttyGPS
  baud_rate: 9600

display:
  units:
    temperature: C
    pressure: psi
    speed: kph
    afr: afr
  thresholds:
    rpm_warn: 6000
    rpm_danger: 7000
    clt_warn: 95
    clt_danger: 105
    afr_lean_warn: 15.5
    afr_rich_warn: 12.0
    oil_p_warn: 15
    batt_low: 12.0
    batt_high: 15.5
  layout: classic

logging:
  enabled: false
  path: /var/log/speeduino-dash
  interval_ms: 100

server:
  listen_addr: ":8080"
  kiosk: true
YAML
fi
sudo chown -R "$DASH_USER:$DASH_GROUP" "$CONFIG_DIR"

# Create log directory
echo "[5/7] Setting up logging..."
sudo mkdir -p "$LOG_DIR"
sudo chown "$DASH_USER:$DASH_GROUP" "$LOG_DIR"

# Install udev rules for serial port symlinks
echo "[6/7] Installing udev rules..."
cat <<'UDEV' | sudo tee "$UDEV_DIR/99-speeduino.rules" > /dev/null
# Speeduino ECU — adjust idVendor/idProduct for your USB-serial adapter
# Find with: udevadm info -a -n /dev/ttyUSB0 | grep -E "idVendor|idProduct|serial"
# Example for CH340 adapter:
# ACTION=="add", SUBSYSTEM=="tty", ATTRS{idVendor}=="1a86", ATTRS{idProduct}=="7523", SYMLINK+="ttySpeeduino"

# GPS — adjust for your USB-serial GPS adapter
# ACTION=="add", SUBSYSTEM=="tty", ATTRS{idVendor}=="1546", ATTRS{idProduct}=="01a7", SYMLINK+="ttyGPS"

# If using Pi UART GPIO directly:
# KERNEL=="ttyAMA0", SYMLINK+="ttyGPS"
# KERNEL=="ttyS0", SYMLINK+="ttyGPS"
UDEV
sudo udevadm control --reload-rules || true

# Install systemd service (patch user if not pi)
echo "[7/8] Installing systemd service..."
sed "s/User=pi/User=$DASH_USER/;s/Group=pi/Group=$DASH_GROUP/" \
    deploy/speeduino-dash.service | sudo tee "$SYSTEMD_DIR/speeduino-dash.service" > /dev/null
sudo systemctl daemon-reload
sudo systemctl enable speeduino-dash

# Install log rotation
echo "[8/8] Installing log rotation..."
if [ -f "deploy/logrotate-speeduino-dash" ]; then
    sudo cp deploy/logrotate-speeduino-dash /etc/logrotate.d/speeduino-dash
    echo "    Log rotation installed."
fi

echo ""
echo "=== Installation complete ==="
echo ""
echo "Next steps:"
echo "  1. Edit udev rules to match your USB adapters:"
echo "     sudo nano /etc/udev/rules.d/99-speeduino.rules"
echo ""
echo "  2. Find your device IDs:"
echo "     udevadm info -a -n /dev/ttyUSB0 | grep -E 'idVendor|idProduct'"
echo ""
echo "  3. Start the service:"
echo "     sudo systemctl start speeduino-dash"
echo ""
echo "  4. Open http://localhost:8080 in a browser"
echo ""
echo "  5. For kiosk mode with boot splash:"
echo "     sudo bash deploy/setup-kiosk.sh"
echo ""
echo "  Or use the Makefile:"
echo "     make install    # This script"
echo "     make kiosk      # Splash + auto-login + Chromium service"
echo ""
