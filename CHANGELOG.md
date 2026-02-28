# Changelog

All notable changes to Speeduino Dash are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

---

## [Unreleased]

### Added
- **Speeduino ECU driver** — full 130-byte OutputChannels via TunerStudio `r` command with msEnvelope CRC32 framing
- **Protocol auto-detection** — automatically detects msEnvelope (primary serial) vs. secondary serial plain commands
- **NMEA GPS driver** — 10 Hz NMEA 0183 parser, tested with u-blox NEO-M8N
- **GPS-only mode** — dashboard displays speed and odometer even without an ECU connected
- **Unified speed source** — prioritizes ECU VSS, falls back to GPS speed
- **Real-time WebSocket dashboard** — server polls ECU + GPS at ~20 Hz, pushes JSON frames to browser clients
- **Multiple dashboard layouts** — Classic (cards + arc tach), Sweep (cinematic half-circle), Race (data-dense grid), Minimal (large RPM + speed)
- **Gear detection** — auto-detect from RPM/speed ratio, or configure manual gear ratios + final drive + tire circumference
- **Estimated HP** — road-load physics model using mass, drag coefficient, frontal area, and rolling resistance
- **Peak HP tracking** — tracks and displays peak estimated horsepower with reset button
- **Persistent odometer** — total + trip distance via GPS haversine, saved to disk, trip reset from UI
- **Warning overlay system** — fullscreen alerts for high CLT, low oil pressure, knock, lean/rich AFR, low battery
- **Configurable warning thresholds** — RPM (warn/danger/max), CLT, IAT, AFR (lean/rich), oil pressure, battery voltage, knock retard
- **Web-based settings page** — configure serial ports, units, thresholds, drivetrain, and vehicle physics from the browser
- **Layered config system** — environment variables → `.env` file → `config.yaml` → built-in defaults
- **Unit conversions** — °C/°F, kPa/PSI/bar, km/h/MPH, AFR/Lambda switchable at runtime
- **CSV data logger** — configurable interval (default 10 Hz) with automatic file rotation
- **Exponential backoff retry** — serial connections retry with backoff (1s → 60s cap)
- **Dark automotive theme** — purpose-built for in-car readability
- **Kiosk mode deployment** — auto-launch Chromium fullscreen on Raspberry Pi boot with Plymouth splash screen
- **systemd service** — managed lifecycle with auto-restart on failure
- **udev rules** — stable `/dev/ttySpeeduino` and `/dev/ttyGPS` symlinks for USB-serial adapters
- **CI/CD release pipeline** — GitHub Actions builds ARMv7 binary and publishes `.tar.gz` archive on tag push
- **Cross-compile targets** — `make pi` (arm64), `make pi32` (armv7)
- **Demo mode** — simulated ECU + GPS data for development without hardware

### Platforms
- Raspberry Pi 3B+ (32-bit ARMv7)
- Raspberry Pi 4/5 (64-bit ARM64)
- Any platform with Go 1.21+ (macOS, Linux, Windows for development)
