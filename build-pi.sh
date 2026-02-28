#!/usr/bin/env bash
# Build speeduino-dash for Raspberry Pi 3B+ (linux/arm, GOARM=7)
# Usage: ./build-pi.sh [IP]
#   If IP is provided, the binary is scp'd to the Pi after building.
set -euo pipefail

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY="speeduino-dash"

echo "▸ Building $BINARY $VERSION for linux/arm (GOARM=7)…"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
  go build -ldflags "-s -w -X main.version=$VERSION" \
  -o "$BINARY" ./cmd/speeduino-dash/

echo "Built ./$BINARY ($(du -h "$BINARY" | cut -f1))"

# Optional: scp to Pi
if [ -n "${1:-}" ]; then
  PIHOST="$1"
  echo "Copying to ${PIHOST}..."
  scp "$BINARY" "${PIHOST}:~/${BINARY}"
  echo "Deployed to ${PIHOST}:~/${BINARY}"
fi
