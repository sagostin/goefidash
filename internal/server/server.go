package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shaunagostinho/speeduino-dash/internal/ecu"
	"github.com/shaunagostinho/speeduino-dash/internal/gps"
	"github.com/shaunagostinho/speeduino-dash/internal/logger"
)

// Server coordinates ECU/GPS polling and broadcasts data to WebSocket clients.
type Server struct {
	cfg     *Config
	ecuProv ecu.Provider
	gpsProv gps.Provider
	webFS   fs.FS
	logger  *logger.Logger

	clients   map[*wsClient]struct{}
	clientsMu sync.RWMutex

	upgrader websocket.Upgrader

	// Odometer — persistent distance tracking
	odoMu        sync.Mutex
	odoTotal     float64 // Total km
	odoTrip      float64 // Trip km (resettable)
	lastGPSLat   float64
	lastGPSLon   float64
	lastGPSValid bool
	odoPath      string // File path for persistence
	odoTicker    *time.Ticker
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

// Frame is the JSON structure sent to all WebSocket clients.
type Frame struct {
	ECU        *ecu.DataFrame    `json:"ecu,omitempty"`
	GPS        *gps.Data         `json:"gps,omitempty"`
	Config     *DisplayConfig    `json:"config,omitempty"`
	Drivetrain *DrivetrainConfig `json:"drivetrain,omitempty"`
	Vehicle    *VehicleConfig    `json:"vehicle,omitempty"`
	Odo        *OdoData          `json:"odo,omitempty"`
	Speed      *SpeedData        `json:"speed,omitempty"` // Calculated best-available speed
	Stamp      int64             `json:"stamp"`           // Unix ms
}

// OdoData is the odometer info sent to clients.
type OdoData struct {
	Total float64 `json:"total"` // km
	Trip  float64 `json:"trip"`  // km
}

// SpeedData provides a unified speed value from the best available source.
type SpeedData struct {
	Value  float64 `json:"value"`  // km/h
	Source string  `json:"source"` // "gps", "vss", or "none"
}

// New creates a new Server.
func New(cfg *Config, ecuProv ecu.Provider, gpsProv gps.Provider, webFS fs.FS) *Server {
	odoPath := filepath.Join(filepath.Dir(cfg.path), "odometer.dat")
	if cfg.path == "" {
		odoPath = "/etc/speeduino-dash/odometer.dat"
	}

	s := &Server{
		cfg:     cfg,
		ecuProv: ecuProv,
		gpsProv: gpsProv,
		webFS:   webFS,
		logger: logger.New(logger.Config{
			Enabled:    cfg.Logging.Enabled,
			Path:       cfg.Logging.Path,
			IntervalMs: cfg.Logging.Interval,
		}),
		clients: make(map[*wsClient]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		odoPath: odoPath,
	}
	s.loadOdometer()
	return s
}

// Run starts the HTTP server and data polling loops.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// Serve embedded web files
	mux.Handle("/", http.FileServer(http.FS(s.webFS)))

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWS)

	// Config API
	mux.HandleFunc("/api/config", s.handleConfig)

	// Odometer API
	mux.HandleFunc("/api/odo/reset-trip", s.handleResetTrip)

	// Start data polling — ECU and GPS are independent
	go s.pollLoop(ctx)

	// Persist odometer every 30 seconds
	s.odoTicker = time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				s.saveOdometer()
				return
			case <-s.odoTicker.C:
				s.saveOdometer()
			}
		}
	}()

	srv := &http.Server{
		Addr:    s.cfg.Server.ListenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		s.saveOdometer()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()

	log.Printf("[server] listening on %s", s.cfg.Server.ListenAddr)
	return srv.ListenAndServe()
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 64),
	}

	s.clientsMu.Lock()
	s.clients[client] = struct{}{}
	s.clientsMu.Unlock()

	log.Printf("[ws] client connected (%d total)", len(s.clients))

	// Send initial config + odometer
	s.odoMu.Lock()
	odo := &OdoData{Total: s.odoTotal, Trip: s.odoTrip}
	s.odoMu.Unlock()

	cfgFrame := Frame{
		Config:     &s.cfg.Display,
		Drivetrain: &s.cfg.Drivetrain,
		Vehicle:    &s.cfg.Vehicle,
		Odo:        odo,
		Stamp:      time.Now().UnixMilli(),
	}
	if data, err := json.Marshal(cfgFrame); err == nil {
		client.send <- data
	}

	// Writer goroutine
	go func() {
		defer conn.Close()
		for msg := range client.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				break
			}
		}
	}()

	// Reader goroutine (handle incoming messages / keep-alive)
	go func() {
		defer func() {
			s.clientsMu.Lock()
			delete(s.clients, client)
			s.clientsMu.Unlock()
			close(client.send)
			log.Printf("[ws] client disconnected (%d total)", len(s.clients))
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := s.cfg.ToJSON()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if err := s.cfg.UpdateFromJSON(body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := s.cfg.Save(); err != nil {
			log.Printf("[config] save failed: %v", err)
		}
		// Broadcast updated config
		cfgFrame := Frame{Config: &s.cfg.Display, Stamp: time.Now().UnixMilli()}
		s.broadcast(cfgFrame)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleResetTrip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	s.odoMu.Lock()
	s.odoTrip = 0
	s.odoMu.Unlock()
	s.saveOdometer()
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// pollLoop continuously requests data from ECU and GPS independently,
// then broadcasts combined frames. GPS continues even if ECU is unavailable.
func (s *Server) pollLoop(ctx context.Context) {
	ecuHz := s.cfg.ECU.PollHz
	if ecuHz <= 0 {
		ecuHz = 20
	}
	ecuTicker := time.NewTicker(time.Second / time.Duration(ecuHz))
	gpsTicker := time.NewTicker(100 * time.Millisecond)                   // 10 Hz
	broadcastTicker := time.NewTicker(time.Second / time.Duration(ecuHz)) // Match ECU rate
	defer ecuTicker.Stop()
	defer gpsTicker.Stop()
	defer broadcastTicker.Stop()

	var (
		lastECU *ecu.DataFrame
		lastGPS *gps.Data
		ecuMu   sync.Mutex
		gpsMu   sync.Mutex
	)

	// GPS polling goroutine — runs independently
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-gpsTicker.C:
				if s.gpsProv != nil {
					if data, err := s.gpsProv.Read(); err == nil {
						gpsMu.Lock()
						lastGPS = data
						gpsMu.Unlock()
						// Update odometer with GPS distance
						if data.Valid && data.Speed > 1 { // Only accumulate if moving
							s.updateOdometer(data)
						}
					}
				}
			}
		}
	}()

	// ECU polling goroutine — runs independently
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ecuTicker.C:
				if s.ecuProv != nil {
					if data, err := s.ecuProv.RequestData(); err == nil {
						ecuMu.Lock()
						lastECU = data
						ecuMu.Unlock()
					}
				}
			}
		}
	}()

	// Broadcast loop — combines latest ECU + GPS and sends to clients
	for {
		select {
		case <-ctx.Done():
			s.logger.Close()
			return
		case <-broadcastTicker.C:
			ecuMu.Lock()
			ecuSnap := lastECU
			ecuMu.Unlock()

			gpsMu.Lock()
			gpsSnap := lastGPS
			gpsMu.Unlock()

			// Calculate best-available speed
			speed := s.calcSpeed(ecuSnap, gpsSnap)

			// Get odometer
			s.odoMu.Lock()
			odo := &OdoData{Total: math.Round(s.odoTotal*10) / 10, Trip: math.Round(s.odoTrip*10) / 10}
			s.odoMu.Unlock()

			// Only broadcast if we have at least something
			if ecuSnap != nil || gpsSnap != nil {
				frame := Frame{
					ECU:   ecuSnap,
					GPS:   gpsSnap,
					Odo:   odo,
					Speed: speed,
					Stamp: time.Now().UnixMilli(),
				}
				s.broadcast(frame)

				// Record to CSV log
				s.logger.Record(ecuSnap, gpsSnap)
			}
		}
	}
}

// calcSpeed returns the best available speed from ECU VSS or GPS.
func (s *Server) calcSpeed(ecuData *ecu.DataFrame, gpsData *gps.Data) *SpeedData {
	// Prefer ECU VSS if available and > 0
	if ecuData != nil && ecuData.VSS > 0 {
		return &SpeedData{Value: float64(ecuData.VSS), Source: "vss"}
	}
	// Fall back to GPS speed
	if gpsData != nil && gpsData.Valid {
		return &SpeedData{Value: gpsData.Speed, Source: "gps"}
	}
	return &SpeedData{Value: 0, Source: "none"}
}

// updateOdometer accumulates distance from GPS position changes.
func (s *Server) updateOdometer(data *gps.Data) {
	s.odoMu.Lock()
	defer s.odoMu.Unlock()

	if !s.lastGPSValid {
		// First valid fix — seed position, don't accumulate
		s.lastGPSLat = data.Latitude
		s.lastGPSLon = data.Longitude
		s.lastGPSValid = true
		return
	}

	// Haversine distance
	dist := haversineKm(s.lastGPSLat, s.lastGPSLon, data.Latitude, data.Longitude)

	// Sanity check: ignore jumps > 500m per tick (GPS glitch)
	if dist > 0.5 {
		s.lastGPSLat = data.Latitude
		s.lastGPSLon = data.Longitude
		return
	}

	// Minimum movement threshold: ~2 meters
	if dist > 0.002 {
		s.odoTotal += dist
		s.odoTrip += dist
		s.lastGPSLat = data.Latitude
		s.lastGPSLon = data.Longitude
	}
}

// haversineKm calculates the great-circle distance between two lat/lon points.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Earth radius km
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// loadOdometer reads persisted odometer values from disk.
func (s *Server) loadOdometer() {
	data, err := os.ReadFile(s.odoPath)
	if err != nil {
		log.Printf("[odo] no saved data at %s (starting at 0)", s.odoPath)
		return
	}
	parts := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(parts) >= 1 {
		if v, err := strconv.ParseFloat(parts[0], 64); err == nil {
			s.odoTotal = v
		}
	}
	if len(parts) >= 2 {
		if v, err := strconv.ParseFloat(parts[1], 64); err == nil {
			s.odoTrip = v
		}
	}
	log.Printf("[odo] loaded: total=%.1f km, trip=%.1f km", s.odoTotal, s.odoTrip)
}

// saveOdometer persists odometer values to disk.
func (s *Server) saveOdometer() {
	s.odoMu.Lock()
	total := s.odoTotal
	trip := s.odoTrip
	s.odoMu.Unlock()

	// Ensure directory exists
	os.MkdirAll(filepath.Dir(s.odoPath), 0755)

	data := fmt.Sprintf("%.6f\n%.6f\n", total, trip)
	if err := os.WriteFile(s.odoPath, []byte(data), 0644); err != nil {
		log.Printf("[odo] save failed: %v", err)
	}
}

func (s *Server) broadcast(frame Frame) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for client := range s.clients {
		select {
		case client.send <- data:
		default:
			// Client too slow, skip
		}
	}
}
