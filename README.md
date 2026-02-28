# Speeduino Dash

A realtime automotive dashboard for **Speeduino** ECUs, built in Go and served as a web app. Designed for **Raspberry Pi** with touchscreen in Chromium kiosk mode — but runs anywhere with a browser.

![License](https://img.shields.io/badge/license-MIT-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
![Platform](https://img.shields.io/badge/platform-Raspberry%20Pi-C51A4A?logo=raspberrypi)
![Release](https://img.shields.io/github/v/release/shaunagostinho/speeduino-dash?label=latest&color=green)

---

## Why This Exists

This project was born out of wanting a **system-agnostic, simple workaround** for an automotive dashboard that uses common, open software — no proprietary displays, no locked-down ecosystems. Just a Raspberry Pi, a browser, and a serial connection to your ECU.

I originally tried **TSDash by EFIAnalytics**, but found it wasn't reliable enough for my use case. V8 Creations also makes the [**SDC dash software**](https://sdc.v8creations.com/products/products.html), which I tried as well — it works, but has licensing and designing custom themes isn't exactly straightforward. Their support went above and beyond though, making multiple changes to try to accommodate the issues I was having — ever grateful for that. Ultimately I built this as a lightweight, fully open-source alternative.

The hardware side uses the excellent [**V8 Creations SDC I/O Hat**](https://www.v8creations.com/sdc/19-sdc-io-hat-serial-or-serial-can.html) for clean serial/CAN connectivity to the Speeduino. **Highly recommend V8 Creations** — great hardware and great people.

---

## Features

### ECU & Serial
- **Speeduino ECU support** — reads the full 130-byte OutputChannels via TunerStudio `r` command
- **Protocol auto-detection** — tries msEnvelope (CRC32 framing) first, falls back to secondary serial plain commands automatically
- **Exponential retry** — serial connections retry with backoff (1s → 60s cap)

### GPS & Speed
- **GPS integration** — standard NMEA 0183 (u-blox NEO-M8N recommended, ~$20, 10 Hz)
- **GPS-only mode** — speed and odometer work even without an ECU connected
- **Unified speed source** — prioritizes ECU VSS, falls back to GPS speed
- **Persistent odometer** — total + trip distance tracked via GPS haversine, saved to disk
- **Trip odometer reset** — reset trip distance from the dashboard UI

### Dashboard & Display
- **Multiple layouts** — Classic (cards + arc tach), Sweep (cinematic half-circle), Race (data-dense grid), Minimal (large RPM + speed only)
- **Canvas tachometer** — smooth animated RPM arc with configurable redline
- **Warning system** — fullscreen overlays for critical conditions (high CLT, low oil pressure, knock, lean AFR, etc.)
- **Dark automotive theme** — purpose-built for in-car readability

### Drivetrain & Calculations
- **Gear detection** — auto-detected from RPM/speed ratio, or manual gear ratio config
- **Estimated HP** — road-load physics model using mass, drag coefficient, frontal area, and rolling resistance
- **Peak HP tracking** — tracks and displays peak estimated horsepower with reset

### Configuration
- **Web settings page** — browser-based configuration for serial ports, units, thresholds, drivetrain, and vehicle physics
- **Layered config** — environment variables → `.env` file → `config.yaml` → built-in defaults
- **Unit conversions** — °C/°F, kPa/PSI/bar, km/h/MPH, AFR/Lambda configurable at runtime
- **Configurable warning thresholds** — RPM, CLT, IAT, AFR (lean/rich), oil pressure, battery voltage, knock retard

### Data Logging
- **CSV data logger** — configurable interval (default 10 Hz) with automatic file rotation

### Deployment
- **Kiosk mode** — auto-launch Chromium fullscreen on Raspberry Pi boot with branded splash screen
- **systemd service** — managed lifecycle with auto-restart
- **udev rules** — stable `/dev/ttySpeeduino` and `/dev/ttyGPS` symlinks
- **CI/CD** — GitHub Actions builds and publishes release archives on tag push

---

## Screenshots

> Screenshots coming soon — the dashboard supports four layouts: **Classic**, **Sweep**, **Race**, and **Minimal**. Run `make run` to see them live in demo mode.

---

## Quick Start

```bash
# Clone and build
git clone https://github.com/shaunagostinho/speeduino-dash.git
cd speeduino-dash
make              # or: go build -o speeduino-dash ./cmd/speeduino-dash/

# Run in demo mode (simulated ECU + GPS data)
make run          # or: ./speeduino-dash --demo --listen :8080

# Open http://localhost:8080 in your browser
# Click the ⚙ gear icon to access settings and switch layouts
```

### Command-Line Flags

| Flag | Description |
|------|-------------|
| `--demo` | Run with simulated ECU + GPS data (no hardware needed) |
| `--listen :8080` | Set the HTTP listen address |
| `--config /path/to/config.yaml` | Load config from a specific path |

### On Raspberry Pi

```bash
# Cross-compile for Pi 3B+ (32-bit, ARMv7)
make pi32

# Interactive setup — prompts for ECU serial, GPS, units, kiosk, etc.
make rpi-setup        # or: sudo bash deploy/rpi-setup.sh
```

The interactive setup walks you through:
- **ECU connection** — Pi UART (`/dev/ttyAMA0`), USB serial (auto-detect), or custom path
- **GPS module** — USB GPS with auto-generated udev rules, or disabled
- **Display units** — temperature, speed, pressure, layout
- **Kiosk mode** — optional Plymouth splash + auto-login + Chromium fullscreen

Alternatively, for a non-interactive install:

```bash
sudo bash deploy/install.sh          # Install binary, config, service
sudo bash deploy/setup-kiosk.sh      # Add kiosk mode
```

> 📖 See [**Raspberry Pi Setup Guide**](docs/RASPBERRY_PI_SETUP.md) for the complete walkthrough — from bare SD card to running dashboard.

---

## Configuration

Configuration loads in this priority order:

1. **Environment variables** (highest priority)
2. **`.env` file** (alongside config.yaml or in CWD)
3. **`config.yaml`** (YAML config file)
4. **Built-in defaults** (fallback)

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ECU_TYPE` | `demo` | `speeduino` or `demo` |
| `ECU_PORT` | `/dev/ttySpeeduino` | ECU serial port path |
| `ECU_BAUD` | `115200` | ECU baud rate |
| `ECU_STOICH` | `14.7` | Stoichiometric ratio (14.7 gas, 9.0 E85) |
| `GPS_TYPE` | `demo` | `nmea`, `demo`, or `disabled` |
| `GPS_PORT` | `/dev/ttyGPS` | GPS serial port path |
| `GPS_BAUD` | `9600` | GPS baud rate |
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `TEMP_UNIT` | `C` | `C` or `F` |
| `PRESSURE_UNIT` | `psi` | `kpa`, `psi`, or `bar` |
| `SPEED_UNIT` | `kph` | `kph` or `mph` |
| `LOG_ENABLED` | `false` | `true` to enable CSV data logging |
| `LOG_PATH` | `/var/log/speeduino-dash` | Directory for log CSV files |
| `LOG_INTERVAL_MS` | `100` | Min ms between log entries (100 = 10 Hz) |

Copy `.env.example` to `.env` and uncomment what you need. See [`config.yaml.example`](config.yaml.example) for the full YAML config with drivetrain, vehicle physics, and threshold settings.

### udev Rules

The install script creates `/etc/udev/rules.d/99-speeduino.rules`. Edit it to match your USB-serial adapters:

```bash
# Find your device IDs
udevadm info -a -n /dev/ttyUSB0 | grep -E "idVendor|idProduct"

# Example rule
ACTION=="add", SUBSYSTEM=="tty", ATTRS{idVendor}=="1a86", ATTRS{idProduct}=="7523", SYMLINK+="ttySpeeduino"
```

---

## Architecture

```
cmd/speeduino-dash/         Entry point, embed, CLI flags, retry logic
internal/
  ecu/
    provider.go             ECU Provider interface + DataFrame (70+ channels)
    speeduino.go            Speeduino serial driver (msEnvelope + secondary protocol auto-detect)
    demo.go                 Simulated ECU for testing
  gps/
    provider.go             GPS Provider interface + Data struct
    nmea.go                 NMEA 0183 parser + demo GPS
  logger/
    logger.go               CSV data logger with configurable interval + file rotation
  server/
    server.go               WebSocket hub, polling loops, odometer, speed source logic
    config.go               Layered config system (env → .env → YAML → defaults)
web/
    index.html              Dashboard — all four layouts (Classic, Sweep, Race, Minimal)
    style.css               Dark automotive theme + layout-specific styles
    dash.js                 Display logic, layout switching, warnings, tach rendering
    shared.js               Shared state, WebSocket client, unit conversions, gear detection, HP calc
    settings.html           Web-based configuration page
    settings.js             Settings logic, gear auto-learn, live preview
    settings.css            Settings page styles
deploy/
    rpi-setup.sh            Interactive Raspberry Pi setup (recommended)
    install.sh              Non-interactive Raspberry Pi installer
    setup-kiosk.sh          Boot splash + auto-login + Chromium service
    speeduino-dash.service  systemd unit file
    kiosk.sh                Chromium kiosk launcher (legacy)
    logrotate-speeduino-dash  Log rotation config
    splash.png              Boot splash image
    plymouth/               Plymouth theme files
docs/
    RASPBERRY_PI_SETUP.md               Complete Pi setup guide (SD card → running dash)
    TUNERSTUDIO_MSENVELOPE_PROTOCOL.md  msEnvelope CRC32 framing spec
    SPEEDUINO_SECONDARY_SERIAL_PROTOCOL.md  Secondary serial plain-byte protocol spec
    CONTRIBUTING.md                     Contributor guide
Makefile                    Build, cross-compile, install, test targets
ROADMAP.md                  Phased feature roadmap
CHANGELOG.md                Release history
```

### ECU Provider Interface

Adding a new ECU is as simple as implementing the `Provider` interface:

```go
type Provider interface {
    Name() string
    Connect() error
    Close() error
    RequestData() (*DataFrame, error)
}
```

The `DataFrame` struct exposes **70+ channels** including RPM, MAP, TPS, AFR, temperatures, pulse widths, VE, boost, VVT, flex fuel, knock, pressures, and status flags. See [`internal/ecu/provider.go`](internal/ecu/provider.go) for the full field list.

---

## Hardware

| Component | Recommendation | Cost |
|-----------|---------------|------|
| ECU | Speeduino UA4C | — |
| I/O Hat | [V8 Creations SDC I/O Hat](https://www.v8creations.com/sdc/19-sdc-io-hat-serial-or-serial-can.html) (Serial or Serial+CAN) | — |
| GPS | u-blox NEO-M8N (UART, 10 Hz) | ~$20 |
| SBC | Raspberry Pi 3B+/4/5 | ~$35-80 |
| Display | 7" or 10" touchscreen | ~$35-60 |
| USB-Serial | CH340 or FTDI adapter (if not using I/O Hat) | ~$5 |

> **💡 Recommended:** The [V8 Creations SDC I/O Hat](https://www.v8creations.com/sdc/19-sdc-io-hat-serial-or-serial-can.html) provides a clean, purpose-built serial/CAN connection from the Pi directly to the Speeduino — no USB adapters needed.

---

## Documentation

| Document | Description |
|----------|-------------|
| [Raspberry Pi Setup Guide](docs/RASPBERRY_PI_SETUP.md) | Complete guide from bare SD card to running dashboard |
| [TunerStudio msEnvelope Protocol](docs/TUNERSTUDIO_MSENVELOPE_PROTOCOL.md) | CRC32 framing specification for primary serial |
| [Secondary Serial Protocol](docs/SPEEDUINO_SECONDARY_SERIAL_PROTOCOL.md) | Plain-byte protocol for secondary serial port |
| [Contributing Guide](docs/CONTRIBUTING.md) | Dev setup, project structure, how to contribute |
| [Roadmap](ROADMAP.md) | Phased feature roadmap |
| [Changelog](CHANGELOG.md) | Release history |

---

## Contributing

Contributions are welcome! See the [**Contributing Guide**](docs/CONTRIBUTING.md) for dev setup instructions, project structure overview, and how to add new ECU providers or dashboard layouts.

---

## License

[MIT](LICENSE)
