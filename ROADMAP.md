# goefidash — Roadmap

## Phase 1 — GPS Dashboard (✅ Complete)

- [x] NMEA GPS driver (10 Hz, u-blox compatible)
- [x] GPS speed display — full-screen simplified race layout
- [x] Persistent odometer (haversine GPS distance, saved every 30s)
- [x] Trip odometer — resets on every boot, manual reset from UI
- [x] Async GPS polling — goroutine-based, never blocks UI
- [x] WebSocket real-time dashboard
- [x] Multiple dashboard layouts (Race, Classic, Sweep, Minimal)
- [x] `.env` + YAML + env var config system (layered priority)
- [x] CSV data logger with configurable interval + file rotation
- [x] Kiosk mode deployment (systemd + Chromium + Plymouth splash)
- [x] Web-based settings page (units, display config)
- [x] Unit conversions at runtime (km/h ↔ MPH, km ↔ miles)
- [x] CI/CD release pipeline (GitHub Actions → tagged tar.gz archive)
- [x] Cross-compile targets (arm64 for Pi 4/5, armv7 for Pi 3B+)
- [x] Dark automotive theme — purpose-built for in-car readability

---

## Phase 2 — ECU Support (Future)

Optional ECU integration — disabled by default, enable with `ECU_TYPE=speeduino`.

### Speeduino (Implemented, Disabled)
- [x] Speeduino ECU driver (secondary serial protocol, `A`/`n`/`r` commands)
- [x] Protocol auto-detection (secondary serial variant detection)
- [x] Exponential backoff serial retry
- [x] Demo ECU provider for development/testing
- [x] Unified speed source (VSS → GPS fallback)
- [x] Gear detection (auto-learn from RPM/speed + manual ratio config)
- [x] Estimated HP (road-load physics model)
- [x] Warning overlay system (coolant, oil, AFR, knock, battery)
- [x] Configurable warning thresholds

### Additional ECUs (Planned)
- [ ] **OBD-II provider** — ELM327 adapter for non-Speeduino vehicles
- [ ] **RuSEFI provider** — TunerStudio protocol
- [ ] **Megasquirt provider** — MS2/MS3 OutputChannels mapping
- [ ] **Generic CAN bus** — configurable DBC-based CAN frame parsing

---

## Phase 3 — Telemetry & Remote Upload

Upload GPS telemetry to a remote server when cellular (LTE dongle) or WiFi is available.

- [ ] **Telemetry uploader** — background goroutine batches frames and POSTs to endpoint
- [ ] **Offline buffer** — store frames to SQLite when no connectivity, flush when back online
- [ ] **Configurable endpoint** — `TELEMETRY_URL`, `TELEMETRY_KEY` env vars
- [ ] **LTE dongle support** — documented tested dongles
- [ ] **Remote dashboard** — simple web viewer for uploaded sessions

---

## Phase 4 — Advanced Features

- [ ] **Lap timer** — GPS-based start/finish line detection, sector timing
- [ ] **Track map** — GPS trace overlay showing speed heatmap
- [ ] **Drag strip mode** — 60ft, 330ft, 1/8, 1/4 mile timing from GPS
- [ ] **0-60 / 0-100 timer** — GPS-based acceleration timing
- [ ] **Data logging to SQLite** — structured storage with session management
- [ ] **Log replay** — play back recorded sessions in the dashboard
- [ ] **Customizable layouts** — drag-and-drop gauge editor
- [ ] **OTA updates** — self-update mechanism for Raspberry Pi deployments
