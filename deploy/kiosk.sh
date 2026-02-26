#!/bin/bash
# Kiosk mode launcher for Chromium on Raspberry Pi
# Called from a user-level systemd unit or .bashrc on auto-login

set -e

# Wait for the dashboard server to be ready
for i in $(seq 1 30); do
    if curl -s -o /dev/null http://localhost:8080; then
        break
    fi
    sleep 1
done

# Disable screen saver / blanking
xset s off
xset -dpms
xset s noblank

# Hide cursor after 3 seconds of inactivity
unclutter -idle 3 -root &

# Launch Chromium in kiosk mode
exec chromium-browser \
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
    http://localhost:8080
