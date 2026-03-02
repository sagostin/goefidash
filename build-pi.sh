#!/usr/bin/env bash
# Build goefidash for Raspberry Pi 3B+ (linux/arm, GOARM=7)
# Usage: ./build-pi.sh [IP]
#   If IP is provided, the binary is scp'd to the Pi after building.
set -euo pipefail

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY="speeduino-dash"

echo "▸ Building $BINARY $VERSION for linux/arm (GOARM=7)…"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
  go build -ldflags "-s -w -X main.version=$VERSION" \
  -o "$BINARY" ./cmd/goefidash/

echo "Built ./$BINARY ($(du -h "$BINARY" | cut -f1))"

# Optional: scp to Pi and install to /usr/local/bin/
if [ -n "${1:-}" ]; then
  PIHOST="$1"
  echo "Deploying to ${PIHOST}:/usr/local/bin/${BINARY}..."
  scp "$BINARY" "${PIHOST}:/tmp/${BINARY}"
  ssh "$PIHOST" "sudo mv /tmp/${BINARY} /usr/local/bin/${BINARY} && sudo chmod +x /usr/local/bin/${BINARY}"
  echo "Installed to ${PIHOST}:/usr/local/bin/${BINARY}"
fi
