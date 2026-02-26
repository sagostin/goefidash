# Speeduino Dash

A realtime automotive dashboard for **Speeduino** ECUs, built in Go and served as a web app. Designed for **Raspberry Pi** with touchscreen in Chromium kiosk mode.

![License](https://img.shields.io/badge/license-MIT-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
![Platform](https://img.shields.io/badge/platform-Raspberry%20Pi-C51A4A?logo=raspberrypi)

## Features

- **Speeduino ECU** — reads the full 130-byte OutputChannels via TunerStudio `r` command with msEnvelope CRC32
- **GPS Integration** — standard NMEA 0183 (u-blox NEO-M8N recommended, ~$20, 10 Hz)
- **Web Dashboard** — Canvas tachometer, gauges, warning overlays, dark automotive theme
- **GPS-Only Mode** — speed and odometer work even without an ECU connected
- **Persistent Odometer** — total + trip distance tracked via GPS haversine, saved to disk
- **Config UI** — browser-based settings page for serial ports, units, thresholds
- **`.env` Support** — environment variable overrides for headless/automated deployments
- **Exponential Retry** — serial connections retry with backoff (1s → 60s cap)
- **Unit Conversions** — °C/°F, kPa/PSI/bar, km/h/MPH configurable at runtime
- **Kiosk Mode** — auto-launch Chromium fullscreen on Raspberry Pi boot

## Quick Start

```bash
# Clone and build
git clone https://github.com/shaunagostinho/speeduino-dash.git
cd speeduino-dash
make              # or: go build -o speeduino-dash ./cmd/speeduino-dash/

# Run in demo mode (simulated ECU + GPS data)
make run          # or: ./speeduino-dash --demo --listen :8080

# Open http://localhost:8080 in your browser
```

### On Raspberry Pi

```bash
# Cross-compile for Pi 4/5 (64-bit)
make pi

# Install (creates config, systemd service, udev rules, log rotation)
sudo bash deploy/install.sh

# Full kiosk mode (boot splash + auto-login + Chromium service)
sudo bash deploy/setup-kiosk.sh
```

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

Copy `.env.example` to `.env` and uncomment what you need.

### YAML Config

Default location: `/etc/speeduino-dash/config.yaml` (override with `--config` flag)

### udev Rules

The install script creates `/etc/udev/rules.d/99-speeduino.rules`. Edit it to match your USB-serial adapters:

```bash
# Find your device IDs
udevadm info -a -n /dev/ttyUSB0 | grep -E "idVendor|idProduct"

# Example rule
ACTION=="add", SUBSYSTEM=="tty", ATTRS{idVendor}=="1a86", ATTRS{idProduct}=="7523", SYMLINK+="ttySpeeduino"
```

## Architecture

```
cmd/speeduino-dash/     Entry point, embed, retry logic
internal/
  ecu/
    provider.go         ECU Provider interface + DataFrame
    speeduino.go        Speeduino serial driver (msEnvelope CRC32)
    demo.go             Simulated ECU for testing
  gps/
    provider.go         GPS Provider interface + Data struct
    nmea.go             NMEA 0183 parser + demo GPS
  logger/
    logger.go           CSV data logger with file rotation
  server/
    server.go           WebSocket hub, polling loops, odometer
    config.go           YAML + .env config system (deep-merge updates)
web/
    index.html          Dashboard layout
    style.css           Dark automotive theme
    dash.js             Gauges, warnings, WebSocket client
    shared.js           Shared state, unit conversions, gear detection, HP
    settings.html       Configuration page
    settings.js         Settings logic, gear auto-learn
    settings.css        Settings page styles
deploy/
    install.sh               One-shot Pi installer
    setup-kiosk.sh           Boot splash + auto-login + Chromium service
    speeduino-dash.service   systemd unit
    kiosk.sh                 Chromium kiosk launcher (legacy)
    logrotate-speeduino-dash Log rotation config
    splash.png               Boot splash image
    plymouth/                Plymouth theme files
Makefile                     Build, cross-compile, install, test
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

## Hardware

| Component | Recommendation | Cost |
|-----------|---------------|------|
| ECU | Speeduino UA4C | — |
| GPS | u-blox NEO-M8N (UART, 10 Hz) | ~$20 |
| SBC | Raspberry Pi 4/5 | ~$45-80 |
| Display | 7" or 10" touchscreen | ~$35-60 |
| USB-Serial | CH340 or FTDI adapter | ~$5 |

## Roadmap

- [ ] **udev helper utility** — detect connected USB-serial devices and generate udev symlink rules
- [ ] **RuSEFI support** — second ECU provider using TunerStudio protocol
- [ ] **Data logging** — CSV/SQLite recording with replay
- [ ] **LTE/Cellular telemetry** — upload GPS coords + ECU data to a remote server via cellular dongle for fleet tracking / analysis
- [ ] **Lap timer** — GPS-based lap detection and timing
- [ ] **Customizable gauge layouts** — drag-and-drop gauge editor
- [ ] **OBD-II provider** — ELM327 adapter support for non-Speeduino cars

## License

MIT
