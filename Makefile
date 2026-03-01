# Speeduino Dash — Makefile
# Usage:
#   make              # Build for current platform
#   make pi           # Cross-compile for Raspberry Pi (arm64)
#   make run          # Build and run in demo mode
#   make deploy PI=pi@192.168.1.50  # Remote deploy to Pi
#   make install      # Install on the Pi (requires sudo)
#   make rpi-setup    # Interactive RPi first-time setup (on-Pi)
#   make clean        # Remove built binary

BINARY  := speeduino-dash
CMD     := ./cmd/speeduino-dash/
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build pi run deploy install kiosk rpi-setup clean

# Default: build for current platform
all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

# Cross-compile for Raspberry Pi 4/5 (64-bit)
pi:
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

# Cross-compile for Raspberry Pi 3 / Zero 2 W (32-bit)
pi32:
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

# Build and run in demo mode
run: build
	./$(BINARY) --demo --listen :8080

# Install on the Pi (run from project root)
install:
	sudo bash deploy/install.sh

# Set up kiosk mode (Plymouth splash, auto-login, Chromium service)
kiosk:
	sudo bash deploy/setup-kiosk.sh

# Remote deploy: build + copy + run setup on Pi via SSH
# Usage: make deploy PI=pi@192.168.1.50
deploy:
	@test -n "$(PI)" || (echo "Usage: make deploy PI=user@host" && exit 1)
	bash deploy-pi.sh $(PI)

# Interactive Raspberry Pi setup (run directly on the Pi)
rpi-setup:
	sudo bash deploy/rpi-setup.sh

# Run tests
test:
	go test -v -race ./...

# Remove built binary
clean:
	rm -f $(BINARY)
