# Speeduino Dash — Roadmap

## Phase 1 — Core (✅ Complete)

- [x] Speeduino ECU driver (secondary serial protocol, plain `A`/`n`/`r` commands)
- [x] Protocol auto-detection (secondary serial variant detection)
- [x] NMEA GPS driver (10 Hz, u-blox compatible)
- [x] WebSocket real-time dashboard
- [x] Persistent odometer (haversine GPS distance) with trip reset
- [x] Unified speed source (VSS → GPS fallback)
- [x] GPS-only mode (dashboard works without ECU)
- [x] Exponential backoff serial retry
- [x] `.env` + YAML + env var config system (layered priority)
- [x] CSV data logger with configurable interval + file rotation
- [x] Kiosk mode deployment (systemd + Chromium + Plymouth splash)
- [x] Multiple dashboard layouts (Classic, Sweep, Race, Minimal)
- [x] Gear detection (auto-learn from RPM/speed + manual ratio config)
- [x] Estimated HP (road-load physics: mass, drag, frontal area, rolling resistance)
- [x] Peak HP tracking with reset
- [x] Configurable warning thresholds (RPM, CLT, IAT, AFR, oil pressure, battery, knock)
- [x] Warning overlay system (fullscreen alerts for critical conditions)
- [x] Web-based settings page (units, thresholds, drivetrain, vehicle physics)
- [x] Drivetrain configuration (gear ratios, final drive, tire circumference, tolerance)
- [x] Vehicle physics configuration (mass, drag coefficient, frontal area, rolling resistance)
- [x] Unit conversions at runtime (°C/°F, kPa/PSI/bar, km/h/MPH, AFR/Lambda)
- [x] CI/CD release pipeline (GitHub Actions → tagged tar.gz archive)
- [x] Cross-compile targets (arm64 for Pi 4/5, armv7 for Pi 3B+)

---

## Phase 2 — Telemetry & Remote Upload

Upload ECU + GPS telemetry to a remote server when cellular (LTE dongle) or WiFi is available.

### Features
- [ ] **Telemetry uploader** — background goroutine batches frames and POSTs to a configurable endpoint
- [ ] **Offline buffer** — store frames to SQLite when no connectivity, flush when back online
- [ ] **Configurable endpoint** — `TELEMETRY_URL`, `TELEMETRY_KEY` env vars
- [ ] **Data selection** — choose which channels to upload (full frame vs. summary)
- [ ] **Upload interval** — configurable rate limiting (e.g. 1 Hz upload vs. 20 Hz local)
- [ ] **Connectivity detection** — ping check or HTTP probe before attempting upload
- [ ] **LTE dongle support** — document tested dongles (Huawei E3372, SIMCom 7600)
- [ ] **Remote dashboard** — simple web viewer for uploaded sessions (separate project)

### Config
```env
TELEMETRY_ENABLED=true
TELEMETRY_URL=https://api.example.com/v1/telemetry
TELEMETRY_KEY=your-api-key
TELEMETRY_INTERVAL_MS=1000
TELEMETRY_OFFLINE_BUFFER=true
```

---

## Phase 3 — Advanced Calculated Channels

TunerStudio-style derived metrics built from existing ECU + GPS data.

### Fuel & Efficiency
- [ ] **Fuel consumption** — `(PW × RPM × injector_cc × num_inj) / (2 × 60 × 1000 × fuel_density)` → L/hr
- [ ] **Fuel economy** — `speed / fuel_consumption` → km/L or MPG
- [ ] **Fuel economy gauge** — instantaneous + average + trip economy display

### Engine
- [ ] **Engine load %** — `(MAP / baro) × 100` or VE-based
- [ ] **Boost (relative)** — `MAP - baro` → psi/kPa above atmospheric
- [ ] **Lambda from AFR** — `AFR / stoich` display option (already calculated, needs UI)
- [ ] **MAF estimate** — `(MAP × displacement × VE × RPM) / (2 × R × IAT_kelvin)` (ideal gas)
- [ ] **Knock retard tracking** — delta between target and actual advance

### Performance
- [ ] **0-60 / 0-100 timer** — GPS-based acceleration timing
- [ ] **Calculated channels panel** — show derived values in a dedicated dashboard section

---

## Phase 4 — Additional ECU Support

- [ ] **RuSEFI provider** — TunerStudio protocol (same framing, different data map)
- [ ] **OBD-II provider** — ELM327 adapter for non-Speeduino vehicles
- [ ] **Megasquirt provider** — MS2/MS3 OutputChannels mapping
- [ ] **Generic CAN bus** — configurable DBC-based CAN frame parsing

---

## Phase 5 — Advanced Features

- [ ] **Data logging to SQLite** — structured storage with session management
- [ ] **Log replay** — play back recorded sessions in the dashboard
- [ ] **Lap timer** — GPS-based start/finish line detection, sector timing
- [ ] **Track map** — GPS trace overlay showing speed/throttle heatmap
- [ ] **Drag strip mode** — 60ft, 330ft, 1/8, 1/4 mile timing from GPS
- [ ] **Customizable layouts** — drag-and-drop gauge editor saved per profile
- [ ] **Multi-page dashboard** — swipe between race/street/diagnostics views
- [ ] **Alerts & notifications** — configurable audio/visual alerts per channel
- [ ] **OTA updates** — self-update mechanism for Raspberry Pi deployments

---

## Phase 6 — Utilities & Tooling

- [ ] **udev helper** — `speeduino-detect` CLI to list USB-serial devices and generate udev symlink rules
- [ ] **Config wizard** — guided first-run setup in the web UI
- [ ] **Firmware version check** — read Speeduino firmware version on connect
- [ ] **Diagnostic mode** — raw byte viewer for debugging serial communication
