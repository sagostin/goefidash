#!/bin/bash
# ================================================================
# Speeduino Dash — Kiosk Launcher
# ================================================================
# This script is the xinit entry point. It starts a bare X session
# with Chromium in full-screen kiosk mode — no desktop environment
# needed. Called by the speeduino-kiosk systemd service via xinit.
# ================================================================

# Wait for the dashboard server to be ready (up to 30s)
for i in $(seq 1 30); do
    if curl -s -o /dev/null http://localhost:8080; then
        break
    fi
    sleep 1
done

# Disable screen blanking / power management
xset s off 2>/dev/null || true
xset -dpms 2>/dev/null || true
xset s noblank 2>/dev/null || true

# Hide cursor after 3 seconds of inactivity
unclutter -idle 3 -root &

# Determine which chromium binary is available
CHROMIUM=""
for bin in chromium-browser chromium; do
    if command -v "$bin" &>/dev/null; then
        CHROMIUM="$bin"
        break
    fi
done

if [[ -z "$CHROMIUM" ]]; then
    echo "ERROR: No chromium binary found"
    sleep 30
    exit 1
fi

# Launch Chromium in kiosk mode
exec "$CHROMIUM" \
    --kiosk \
    --noerrdialogs \
    --disable-infobars \
    --disable-session-crashed-bubble \
    --disable-translate \
    --no-first-run \
    --start-fullscreen \
    --incognito \
    --disable-pinch \
    --overscroll-history-navigation=0 \
    --check-for-update-interval=31536000 \
    --disable-features=TranslateUI \
    http://localhost:8080
