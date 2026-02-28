# Contributing to Speeduino Dash

Thanks for your interest in contributing! This guide covers everything you need to get started.

---

## Prerequisites

- **Go 1.21+** — [download](https://go.dev/dl/)
- **Git**
- A browser (Chrome/Chromium recommended for testing)
- *(Optional)* A Speeduino ECU + USB-serial adapter — **not required**, demo mode simulates everything

---

## Dev Setup

```bash
# Clone the repo
git clone https://github.com/shaunagostinho/speeduino-dash.git
cd speeduino-dash

# Build and run in demo mode
make run

# Open http://localhost:8080
```

Demo mode simulates realistic ECU and GPS data — you can develop the full dashboard without any hardware.

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make` / `make build` | Build for current platform |
| `make pi` | Cross-compile for Raspberry Pi 4/5 (linux/arm64) |
| `make pi32` | Cross-compile for Raspberry Pi 3B+ (linux/arm, ARMv7) |
| `make run` | Build and run in demo mode on `:8080` |
| `make test` | Run tests with race detector |
| `make clean` | Remove built binary |

---

## Project Structure

```
cmd/speeduino-dash/         Entry point, CLI flags, static file embedding
internal/
  ecu/                      ECU abstraction layer
    provider.go             Provider interface + DataFrame struct
    speeduino.go            Speeduino implementation (msEnvelope + secondary serial)
    demo.go                 Simulated ECU for development
  gps/                      GPS abstraction layer
    provider.go             Provider interface + Data struct
    nmea.go                 NMEA 0183 parser + demo GPS
  logger/
    logger.go               CSV data logger
  server/
    server.go               HTTP server, WebSocket hub, polling, odometer
    config.go               Layered config system
web/                        Frontend (embedded into the Go binary)
    index.html              Dashboard HTML (all layouts)
    style.css               Styles (dark theme + layout variants)
    dash.js                 Display logic, layout switching, warning system
    shared.js               Shared module (WebSocket, state, conversions, gear/HP calcs)
    settings.html/js/css    Configuration page
deploy/                     Raspberry Pi deployment scripts
docs/                       Protocol specs and guides
```

### Key Concepts

- **Everything is embedded** — the `web/` directory is compiled into the Go binary via `go:embed`. No external files needed at runtime.
- **Provider pattern** — ECU and GPS are abstracted behind interfaces. Adding a new ECU type means implementing 4 methods.
- **WebSocket push** — the server polls ECU + GPS at ~20 Hz and pushes JSON frames to all connected browser clients.
- **Layered config** — env vars override `.env`, which overrides `config.yaml`, which overrides defaults. The config system supports live updates from the settings page.

---

## Adding a New ECU Provider

1. Create a new file in `internal/ecu/` (e.g. `rusefi.go`)

2. Implement the `Provider` interface:

```go
type Provider interface {
    Name() string              // Human-readable name (e.g. "RuSEFI")
    Connect() error            // Open serial port, perform handshake
    Close() error              // Clean shutdown
    RequestData() (*DataFrame, error)  // Poll and parse realtime data
}
```

3. Populate the `DataFrame` struct with as many fields as your ECU supports — unused fields default to zero values and the frontend handles missing data gracefully.

4. Wire it up in `cmd/speeduino-dash/main.go` by adding a case to the ECU type switch.

---

## Adding a New Dashboard Layout

1. **HTML** — add a new `<div class="layout layout-yourname">` section in `web/index.html`, following the pattern of existing layouts (Classic, Sweep, Race, Minimal).

2. **CSS** — add layout-specific styles in `web/style.css` under a `.layout-yourname.active` selector.

3. **JavaScript** — update `web/dash.js` to populate your layout's elements in the `updateDisplay()` function. The shared module (`window.SpeeduinoDash`) provides all data and conversion utilities.

4. **Config** — add your layout name to the layout dropdown in `web/settings.html`.

---

## Code Style

- **Go** — standard `gofmt` formatting, no external linters required
- **JavaScript** — vanilla JS (no frameworks), IIFE module pattern, `'use strict'`
- **CSS** — BEM-ish class naming, CSS custom properties for theming
- **Commits** — descriptive messages, present tense ("Add sweep layout" not "Added sweep layout")

---

## Releases

Releases are automated via GitHub Actions. To create a release:

1. Tag a commit: `git tag v0.2.0`
2. Push the tag: `git push origin v0.2.0`
3. GitHub Actions builds a `linux/arm` (ARMv7) binary and publishes a `.tar.gz` archive containing the binary, `config.yaml.example`, and the `deploy/` directory.

---

## Questions?

Open an issue on GitHub — happy to help!
