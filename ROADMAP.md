# Speeduino Dash — Roadmap

## Phase 1 — Core (✅ Complete)

- [x] Speeduino ECU driver (TunerStudio `r` command, msEnvelope CRC32)
- [x] NMEA GPS driver (10 Hz, u-blox compatible)
- [x] WebSocket real-time dashboard
- [x] Persistent odometer (haversine GPS distance)
- [x] Unified speed source (VSS → GPS fallback)
- [x] GPS-only mode (dashboard works without ECU)
- [x] Exponential backoff serial retry
- [x] `.env` + YAML + env var config system
- [x] CSV data logger with file rotation
- [x] Kiosk mode deployment (systemd + Chromium)

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

## Phase 3 — Gear Detection & Drivetrain Calculations

Automatic gear detection and TunerStudio-style drivetrain calculations based on RPM/speed relationships.

### Gear Detection
- [ ] **Auto-learn gear ratios** — record RPM-to-speed ratios at steady state, cluster into gears
- [ ] **Manual gear ratio config** — define ratios + final drive in config for known setups
- [ ] **Live gear display** — calculated from `gear_ratio = (RPM × tire_circ) / (speed × final_drive × 1000)`
- [ ] **Neutral / clutch detection** — RPM rising with no speed change = neutral/clutch in

### Drivetrain Config
```yaml
drivetrain:
  tire_diameter_mm: 635        # 245/40R18 = ~635mm
  final_drive: 4.10
  gear_ratios: [3.587, 2.022, 1.384, 1.000, 0.861, 0.717]  # 6-speed example
  transmission: manual         # manual | automatic | sequential
```

### Calculated Channels (TunerStudio-style)
- [ ] **Wheel speed** — `(RPM / gear_ratio / final_drive) × tire_circ × 60 / 1_000_000` → km/h
- [ ] **Engine load %** — `(MAP / baro) × 100` or VE-based
- [ ] **Injector duty cycle** — `(PW × RPM) / (60_000 / cylinders × 2)` for 4-stroke
- [ ] **Fuel consumption** — `(PW × RPM × injector_cc × num_inj) / (2 × 60 × 1000 × fuel_density)` → L/hr
- [ ] **Fuel economy** — `speed / fuel_consumption` → km/L or MPG
- [ ] **Estimated HP** — `(MAP × RPM × displacement) / (2 × 60 × baro × VE_correction)` (rough)
- [ ] **Estimated torque** — `HP × 5252 / RPM`
- [ ] **Boost (relative)** — `MAP - baro` → psi/kPa above atmospheric
- [ ] **Lambda from AFR** — `AFR / stoich` (already available, display option)
- [ ] **MAF estimate** — `(MAP × displacement × VE × RPM) / (2 × R × IAT_kelvin)` (ideal gas)
- [ ] **Knock retard tracking** — delta between target and actual advance

### Display
- [ ] **Calculated channels panel** — show derived values in a dedicated section
- [ ] **Fuel economy gauge** — instantaneous + average + trip economy
- [ ] **0-60 / 0-100 timer** — GPS-based acceleration timing

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
