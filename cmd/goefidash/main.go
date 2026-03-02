package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shaunagostinho/speeduino-dash/internal/ecu"
	"github.com/shaunagostinho/speeduino-dash/internal/gps"
	"github.com/shaunagostinho/speeduino-dash/internal/server"
	"github.com/shaunagostinho/speeduino-dash/web"
)

func main() {
	configPath := flag.String("config", "/etc/goefidash/config.yaml", "Path to config file")
	demo := flag.Bool("demo", false, "Run with simulated ECU and GPS data")
	listenAddr := flag.String("listen", "", "Override listen address (e.g. :8080)")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("[main] goefidash starting")

	// Load config
	cfg := server.LoadConfig(*configPath)

	if *demo {
		cfg.ECU.Type = "demo"
		cfg.GPS.Type = "demo"
	}
	if *listenAddr != "" {
		cfg.Server.ListenAddr = *listenAddr
	}

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[main] received %v, shutting down", sig)
		cancel()
	}()

	// Initialize ECU provider with exponential backoff retry
	var ecuProv ecu.Provider
	switch cfg.ECU.Type {
	case "speeduino":
		ecuProv = ecu.NewSpeeduino(ecu.SpeeduinoConfig{
			PortPath: cfg.ECU.PortPath,
			BaudRate: cfg.ECU.BaudRate,
			CanID:    byte(cfg.ECU.CanID),
			Stoich:   cfg.ECU.Stoich,
		})
	default:
		ecuProv = ecu.NewDemoProvider()
	}

	// Try connecting with exponential backoff (non-blocking — dashboard starts regardless)
	go connectWithRetry(ctx, "ECU", ecuProv, 10)

	// Initialize GPS provider
	var gpsProv gps.Provider
	switch cfg.GPS.Type {
	case "nmea":
		gpsProv = gps.NewNMEA(gps.NMEAConfig{
			PortPath: cfg.GPS.PortPath,
			BaudRate: cfg.GPS.BaudRate,
		})
	case "disabled":
		gpsProv = nil
	default:
		gpsProv = gps.NewDemoGPS()
	}

	if gpsProv != nil {
		go connectWithRetry(ctx, "GPS", gpsProv, 10)
	}

	// Start server — works immediately even if ECU/GPS are still connecting
	srv := server.New(cfg, ecuProv, gpsProv, web.FS)
	if err := srv.Run(ctx); err != nil {
		log.Printf("[main] server exited: %v", err)
	}
}

// connectable is satisfied by both ecu.Provider and gps.Provider.
type connectable interface {
	Connect() error
	Close() error
}

// connectWithRetry attempts to connect with exponential backoff.
// Starts at 1s, doubles each attempt up to 60s, retries up to maxAttempts
// then continues at max interval indefinitely.
func connectWithRetry(ctx context.Context, name string, c connectable, maxAttempts int) {
	delay := 1 * time.Second
	maxDelay := 60 * time.Second
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := c.Connect(); err != nil {
			attempt++
			if attempt <= maxAttempts {
				log.Printf("[%s] connect attempt %d/%d failed: %v (retry in %v)",
					name, attempt, maxAttempts, err, delay)
			} else {
				log.Printf("[%s] connect attempt %d failed: %v (retry in %v)",
					name, attempt, err, delay)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		} else {
			log.Printf("[%s] connected successfully (attempt %d)", name, attempt+1)
			return
		}
	}
}
