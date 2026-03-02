#!/usr/bin/env bash
# ================================================================
# Speeduino Dash — Remote Raspberry Pi Deploy
# ================================================================
# Builds the binary locally and deploys to a Raspberry Pi.
#
# Usage:
#   ./deploy-pi.sh <pi-host>             # Quick deploy (build + install binary + restart)
#   ./deploy-pi.sh <pi-host> --quick     # Same as above (explicit)
#   ./deploy-pi.sh <pi-host> --full      # Full deploy (build + copy all + run setup wizard)
#
# Quick mode (default):
#   1. Cross-compiles the binary for ARMv7 (Pi 3B+)
#   2. SCPs the binary directly to /usr/local/bin/goefidash
#   3. Restarts the systemd service (if running)
#
# Full mode:
#   1. Cross-compiles the binary for ARMv7 (Pi 3B+)
#   2. SCPs the binary + entire deploy/ directory to the Pi
#   3. SSHs in and runs deploy/rpi-setup.sh interactively
# ================================================================
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'
YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'

if [[ -z "${1:-}" ]]; then
    echo -e "${RED}Usage:${NC} $0 <pi-host> [--quick|--full]"
    echo ""
    echo "  Quick deploy (default):"
    echo "    $0 pi@192.168.1.50"
    echo "    $0 root@goefidash --quick"
    echo ""
    echo "  Full deploy (interactive setup):"
    echo "    $0 pi@192.168.1.50 --full"
    echo ""
    exit 1
fi

PIHOST="$1"
MODE="${2:---quick}"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY="speeduino-dash"
REMOTE_DIR="~/speeduino-dash-deploy"

# ── Step 1: Build ───────────────────────────────────────────────
echo -e "\n${CYAN}${BOLD}[1/3] Building ${BINARY} ${VERSION} for linux/arm (GOARM=7)…${NC}"
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -ldflags "-s -w -X main.version=$VERSION" \
    -o "$BINARY" ./cmd/goefidash/

echo -e "  ${GREEN}✓${NC} Built ./${BINARY} ($(du -h "$BINARY" | cut -f1))"

if [[ "$MODE" == "--full" ]]; then
    # ── Full deploy: copy everything + run setup wizard ─────────
    echo -e "\n${CYAN}${BOLD}[2/3] Copying files to ${PIHOST}:${REMOTE_DIR}${NC}"

    if command -v rsync &>/dev/null; then
        echo -e "  Syncing all files via rsync..."
        rsync -az --mkpath \
            "$BINARY" \
            config.yaml.example \
            "${PIHOST}:${REMOTE_DIR}/"
        rsync -az --mkpath \
            deploy/ \
            "${PIHOST}:${REMOTE_DIR}/deploy/"
    else
        echo -e "  Copying files via scp..."
        ssh "$PIHOST" "mkdir -p ${REMOTE_DIR}/deploy/plymouth"
        scp -rq "$BINARY" config.yaml.example "${PIHOST}:${REMOTE_DIR}/"
        scp -rq deploy/ "${PIHOST}:${REMOTE_DIR}/deploy/"
    fi

    echo -e "  ${GREEN}✓${NC} All files copied"

    echo -e "\n${CYAN}${BOLD}[3/3] Running interactive setup on ${PIHOST}…${NC}"
    echo -e "  ${YELLOW}Handing off to the Pi — answer the prompts below.${NC}"
    echo ""

    ssh -t "$PIHOST" "cd ${REMOTE_DIR} && sudo bash deploy/rpi-setup.sh"
else
    # ── Quick deploy: just install binary + restart ─────────────
    echo -e "\n${CYAN}${BOLD}[2/3] Installing binary to ${PIHOST}:/usr/local/bin/${BINARY}${NC}"
    scp "$BINARY" "${PIHOST}:/tmp/${BINARY}"
    ssh "$PIHOST" "sudo mv /tmp/${BINARY} /usr/local/bin/${BINARY} && sudo chmod +x /usr/local/bin/${BINARY}"
    echo -e "  ${GREEN}✓${NC} Binary installed"

    echo -e "\n${CYAN}${BOLD}[3/3] Restarting service…${NC}"
    if ssh "$PIHOST" "sudo systemctl is-active --quiet speeduino-dash 2>/dev/null"; then
        ssh "$PIHOST" "sudo systemctl restart speeduino-dash"
        echo -e "  ${GREEN}✓${NC} Service restarted"
    else
        echo -e "  ${YELLOW}⚠${NC} No systemd service found — run manually:"
        echo -e "    ssh ${PIHOST}"
        echo -e "    /usr/local/bin/speeduino-dash --config /etc/speeduino-dash/config.yaml"
    fi
fi

# Clean up local binary
rm -f "$BINARY"

echo ""
echo -e "${GREEN}${BOLD}Deploy complete!${NC}"
echo -e "  Dashboard: ${BOLD}http://${PIHOST##*@}:8080${NC}"
echo ""
