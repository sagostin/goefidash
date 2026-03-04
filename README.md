# goefidash

A GPS-based automotive dashboard built in Go, served as a web app. Designed for **Raspberry Pi** with touchscreen in Chromium kiosk mode — but runs anywhere with a browser.

Currently provides **GPS speed + odometer** with a clean, full-screen race display. ECU support (Speeduino, OBD-II, etc.) is planned for the future and can be optionally enabled.

![License](https://img.shields.io/badge/license-MIT-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
![Platform](https://img.shields.io/badge/platform-Raspberry%20Pi-C51A4A?logo=raspberrypi)
![Release](https://img.shields.io/github/v/release/shaunagostinho/speeduino-dash?label=latest&color=green)

---

## Why This Exists

This project was born out of wanting a **system-agnostic, simple workaround** for an automotive dashboard that uses common, open software — no proprietary displays, no locked-down ecosystems. Just a Raspberry Pi, a browser, and a GPS module.

The default display is a **big, clean speedometer** with odometer and trip distance — exactly what you need when driving. Trip resets on every boot so each session starts fresh.

---

## Features

### GPS & Speed
- **GPS integration** — standard NMEA 0183 (u-blox NEO-M8N recommended, ~$20, 10 Hz)
- **Giant speedometer display** — full-screen speed, designed for in-car readability
- **Persistent odometer** — total distance tracked via GPS haversine, saved to disk every 30s
- **Trip odometer** — resets on every boot, manual reset from dashboard UI
- **Async GPS polling** — GPS runs in its own goroutine; UI never blocks on serial I/O

### Dashboard & Display
- **Race layout (default)** — full-screen speed + bottom ODO/TRIP bar
- **Classic layout** — card-grid with RPM, speed, engine gauges (for ECU mode)
- **Sweep layout** — cinematic arc tachometer (for ECU mode)
- **Minimal layout** — ultra-clean speed + RPM (for ECU mode)
- **Dark automotive theme** — purpose-built for in-car readability

### Configuration
- **Web settings page** — browser-based configuration for serial ports, units, and display
- **Layered config** — environment variables → `.env` file → `config.yaml` → built-in defaults
- **Unit conversions** — km/h ↔ MPH, km ↔ miles configurable at runtime

### Data Logging
- **CSV data logger** — configurable interval (default 10 Hz) with automatic file rotation

### Deployment
- **Kiosk mode** — auto-launch Chromium fullscreen on Raspberry Pi boot with branded splash screen
- **systemd service** — managed lifecycle with auto-restart
- **udev rules** — stable `/dev/ttyGPS` symlink
- **CI/CD** — GitHub Actions builds and publishes release archives on tag push

### ECU Support (Optional, Disabled by Default)
- **Speeduino ECU** — reads realtime data via secondary serial protocol (set `ECU_TYPE=speeduino`)
- **Demo mode** — simulated ECU data for development (set `ECU_TYPE=demo`)
- Additional ECU providers (OBD-II, RuSEFI, Megasquirt) planned for the future

---

## Quick Start

```bash
# Clone and build
git clone https://github.com/shaunagostinho/speeduino-dash.git
cd goefidash
make              # or: go build -o goefidash ./cmd/goefidash/

# Run in demo mode (simulated GPS data)
make run          # or: ./goefidash --demo --listen :8080

# Open http://localhost:8080 in your browser
```

### Command-Line Flags

| Flag | Description |
|------|-------------|
| `--demo` | Run with simulated GPS data (no hardware needed) |
| `--listen :8080` | Set the HTTP listen address |
| `--config /path/to/config.yaml` | Load config from a specific path |

### Deploy to Raspberry Pi

```bash
# One command: builds locally, copies to Pi, runs interactive setup via SSH
./deploy-pi.sh pi@192.168.1.50
# or:
make deploy PI=pi@192.168.1.50
```

This builds the binary for ARMv7, SCPs it along with all deploy scripts to the Pi, then drops you into an interactive SSH session that walks you through:
- **GPS module** — USB GPS with auto-generated udev rules, or disabled
- **Display units** — speed, temperature, layout
- **Kiosk mode** — optional Plymouth splash + auto-login + Chromium fullscreen

> 📖 See [**Raspberry Pi Setup Guide**](docs/RASPBERRY_PI_SETUP.md) for the complete walkthrough.

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
| `ECU_TYPE` | `disabled` | `disabled`, `speeduino`, or `demo` |
| `GPS_TYPE` | `demo` | `nmea`, `demo`, or `disabled` |
| `GPS_PORT` | `/dev/ttyGPS` | GPS serial port path |
| `GPS_BAUD` | `9600` | GPS baud rate |
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `SPEED_UNIT` | `kph` | `kph` or `mph` |
| `LOG_ENABLED` | `false` | `true` to enable CSV data logging |

See [`config.yaml.example`](config.yaml.example) for the full YAML config.

---

## Architecture

```
cmd/goefidash/         Entry point, CLI flags, retry logic
internal/
  ecu/
    provider.go        ECU Provider interface + DataFrame
    speeduino.go       Speeduino serial driver (optional)
    demo.go            Simulated ECU for testing
  gps/
    provider.go        GPS Provider interface + Data struct
    nmea.go            NMEA 0183 parser + demo GPS
  logger/
    logger.go          CSV data logger with configurable interval
  server/
    server.go          WebSocket hub, async GPS polling, odometer, speed source
    config.go          Layered config system (env → .env → YAML → defaults)
web/
    index.html         Dashboard — Race (default), Classic, Sweep, Minimal layouts
    style.css          Dark automotive theme
    dash.js            Display logic, layout switching, speed rendering
    shared.js          WebSocket client, unit conversions
    settings.html      Web-based configuration page
deploy-pi.sh           Remote deploy (build → scp → SSH setup)
deploy/                Raspberry Pi setup scripts, systemd, kiosk
docs/                  Setup guides, protocol docs
```

### Data Flow

```
GPS serial → goroutine (10Hz) → lastGPS (mutex) → broadcast ticker → WebSocket → browser
                                                  ↗
                              odometer (haversine) → periodic save (30s)
```

GPS polling is fully async — runs in its own goroutine, never blocks the WebSocket broadcast or UI.

---

## Hardware

| Component | Recommendation | Cost |
|-----------|---------------|------|
| GPS | u-blox NEO-M8N (UART, 10 Hz) | ~$20 |
| SBC | Raspberry Pi 3B+/4/5 | ~$35-80 |
| Display | 7" or 10" touchscreen | ~$35-60 |

---

## Documentation

| Document | Description |
|----------|-------------|
| [Raspberry Pi Setup Guide](docs/RASPBERRY_PI_SETUP.md) | Complete guide from bare SD card to running dashboard |
| [Contributing Guide](docs/CONTRIBUTING.md) | Dev setup, project structure, how to contribute |
| [Roadmap](ROADMAP.md) | Phased feature roadmap |
| [Changelog](CHANGELOG.md) | Release history |

---

## Contributing

Contributions are welcome! See the [**Contributing Guide**](docs/CONTRIBUTING.md) for dev setup instructions and project structure overview.

---

## License

[MIT](LICENSE)
