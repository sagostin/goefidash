# Raspberry Pi Setup Guide

Complete guide to setting up Speeduino Dash on a Raspberry Pi — from bare SD card to running dashboard.

> 📖 See the main [README](../README.md) for features, configuration reference, and architecture overview.

---

## Recommended Hardware

| Component | Recommended | Notes |
|-----------|-------------|-------|
| **SBC** | Raspberry Pi 4B (2GB+) or Pi 5 | Pi 3B+ works but slower boot |
| **Display** | 7" official Pi touchscreen | 10" IPS recommended for readability |
| **GPS** | u-blox NEO-M8N (UART) | ~$15-20, 10 Hz capable |
| **USB-Serial** | CH340 or FTDI adapter | For ECU serial connection |
| **SD Card** | 16GB+ Class 10 / A1 | SanDisk Extreme recommended |
| **Power** | 5V 3A USB-C (Pi 4/5) | Use a good quality supply — voltage drops cause freezes |
| **Case** | SmartPi Touch 2 or custom | Needs touchscreen + Pi mounting |

### Optional
- **LTE dongle** — Huawei E3372 or SIMCom 7600 for future telemetry
- **USB fan/heatsink** — recommended if enclosed in a dash mount
- **Backup battery** — UPS HAT to handle ignition-off gracefully

---

## Step 1: Choose the Right OS Image

### ✅ Recommended: Raspberry Pi OS Lite (64-bit)

**Download**: [Raspberry Pi Imager](https://www.raspberrypi.com/software/) → Raspberry Pi OS (other) → **Raspberry Pi OS Lite (64-bit)**

- **Why Lite?** No desktop environment overhead. The dashboard is a fullscreen Chromium kiosk — you don't need a full desktop.
- **Why 64-bit?** The Go binary is compiled for `arm64`. Also better performance on Pi 4/5.

> [!IMPORTANT]
> Do **not** use the Desktop image unless you have a specific reason. Lite + kiosk gives faster boot and lower resource usage.

### Alternative Images

| Image | When to use |
|-------|-------------|
| Raspberry Pi OS Desktop (64-bit) | If you want a full desktop environment alongside the dashboard |
| Raspberry Pi OS Lite (32-bit) | Pi 3B or Zero 2 W — build with `make pi32` |
| DietPi | Ultra-minimal, ~200MB. Good for advanced users who want the fastest boot |

---

## Step 2: Flash the SD Card

1. Download and install [Raspberry Pi Imager](https://www.raspberrypi.com/software/)
2. Select your Pi model
3. Select **Raspberry Pi OS Lite (64-bit)**
4. Click the **⚙ gear icon** (or "Edit Settings") before writing:

| Setting | Value |
|---------|-------|
| Hostname | `speeduino-dash` |
| Username | Your choice (e.g. `pi` or `dash`) |
| Password | Set a password |
| SSH | **Enable** (password auth is fine for local) |
| WiFi | Configure if you need network (optional on the car) |
| Locale | Set your timezone |

5. Write to SD card

---

## Step 3: First Boot & SSH In

1. Insert SD card, connect ethernet (or WiFi was configured), power on
2. Find the Pi on your network: `ping speeduino-dash.local`
3. SSH in:
   ```bash
   ssh dash@goefidash.local
   ```

4. Update the system:
   ```bash
   sudo apt update && sudo apt upgrade -y
   ```

5. Install minimal X server (required for Chromium kiosk):
   ```bash
   sudo apt install -y --no-install-recommends \
       xserver-xorg x11-xserver-utils xinit openbox
   ```

---

## Step 4: Install Speeduino Dash

Transfer the pre-built binary from your dev machine:

```bash
# On your dev machine — cross-compile
cd goefidash
make pi

# Copy to the Pi
scp goefidash dash@goefidash.local:~/
scp -r deploy/ dash@goefidash.local:~/deploy/
scp config.yaml.example dash@goefidash.local:~/
```

Then on the Pi:

```bash
# Move files into place
cd ~
mkdir -p deploy
cp goefidash ./
cp config.yaml.example ./

# Run the installer
sudo bash deploy/install.sh

# (Optional) Set up full kiosk mode with boot splash
sudo bash deploy/setup-kiosk.sh
```

---

## Step 5: Wire the Hardware

### ECU (Speeduino) — USB Serial

```
Speeduino DB9        USB-Serial Adapter         Pi USB Port
  TX  ──────────────── RX
  RX  ──────────────── TX                      ──── USB-A
  GND ──────────────── GND
```

### GPS (u-blox NEO-M8N) — USB or UART

**USB (easiest)**:
Plug the GPS module's USB cable directly into the Pi.

**UART (GPIO)**:
```
GPS Module          Pi GPIO
  TX  ───────────── GPIO 15 (RXD)
  RX  ───────────── GPIO 14 (TXD)
  VCC ───────────── 3.3V (pin 1)
  GND ───────────── GND  (pin 6)
```

> [!TIP]
> If using UART GPIO, disable the Pi's serial console first:
> ```bash
> sudo raspi-config → Interface Options → Serial Port
>   → Login shell over serial: NO
>   → Hardware serial: YES
> ```

### Touchscreen

The official 7" touchscreen connects via the DSI ribbon cable. No driver install needed.

For HDMI screens, just plug in HDMI — configure resolution in `/boot/config.txt` if needed.

---

## Step 6: Configure udev Rules

Find your USB device IDs:

```bash
# Plug in the ECU USB adapter, then:
udevadm info -a -n /dev/ttyUSB0 | grep -E "idVendor|idProduct"

# Plug in the GPS adapter (if USB), then:
udevadm info -a -n /dev/ttyUSB1 | grep -E "idVendor|idProduct"
```

Edit the udev rules:

```bash
sudo nano /etc/udev/rules.d/99-speeduino.rules
```

Uncomment and update with your device IDs:

```
ACTION=="add", SUBSYSTEM=="tty", ATTRS{idVendor}=="1a86", ATTRS{idProduct}=="7523", SYMLINK+="ttySpeeduino"
ACTION=="add", SUBSYSTEM=="tty", ATTRS{idVendor}=="1546", ATTRS{idProduct}=="01a7", SYMLINK+="ttyGPS"
```

Reload rules:

```bash
sudo udevadm control --reload-rules
sudo udevadm trigger
ls -la /dev/ttySpeeduino /dev/ttyGPS   # Verify symlinks exist
```

---

## Step 7: Configure & Start

Edit config if needed:
```bash
sudo nano /etc/goefidash/config.yaml
```

Start the service:
```bash
sudo systemctl start goefidash
sudo systemctl status goefidash    # Verify it's running
```

Check the dashboard at `http://speeduino-dash.local:8080`

---

## Step 8: Enable Kiosk Mode (Optional)

If you haven't already run the kiosk setup:

```bash
sudo bash deploy/setup-kiosk.sh
sudo reboot
```

This gives you:
- ✅ Branded boot splash (no Linux boot text)
- ✅ Auto-login on tty1
- ✅ Chromium starts fullscreen automatically
- ✅ No cursor, no screen blanking

---

## Troubleshooting

### Dashboard doesn't start
```bash
sudo journalctl -u goefidash -f     # Watch live logs
```

### Serial port not found
```bash
ls /dev/ttyUSB*                           # Check if devices show up
dmesg | tail -20                          # Check kernel messages after plugging in
```

### GPS not getting a fix
- Make sure the antenna has **clear sky view** (won't work indoors/in a garage)
- First fix can take 30-60 seconds cold start
- Check: `cat /dev/ttyGPS` — you should see NMEA sentences starting with `$GP`

### Screen is blank / wrong resolution
Add to `/boot/config.txt` (or `/boot/firmware/config.txt`):
```
# Force HDMI output
hdmi_force_hotplug=1
hdmi_group=2
hdmi_mode=87
hdmi_cvt 1024 600 60 6 0 0 0    # For 1024x600 screens
```

### Chromium doesn't launch in kiosk mode
```bash
# Check the kiosk service status
systemctl --user status speeduino-kiosk

# Check if X is running
echo $DISPLAY    # Should be :0
```

---

## Performance Tips

- **Disable WiFi/Bluetooth** if not needed — saves power and reduces interference:
  ```
  # /boot/config.txt
  dtoverlay=disable-wifi
  dtoverlay=disable-bt
  ```
- **GPU memory split** — Chromium benefits from more GPU memory:
  ```
  # /boot/config.txt
  gpu_mem=128
  ```
- **Overclock** (Pi 4 only, with heatsink):
  ```
  # /boot/config.txt
  over_voltage=6
  arm_freq=2000
  ```
- **Read-only filesystem** — protects SD card from corruption on power loss. Use `overlayfs`:
  ```bash
  sudo raspi-config → Performance → Overlay File System → Enable
  ```

> [!WARNING]
> If you enable overlay filesystem, config changes and odometer data won't persist across reboots.
> Make sure `/etc/speeduino-dash/` is excluded from the overlay, or use a separate writable partition.
