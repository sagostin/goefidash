package gps

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// NMEAProvider reads standard NMEA 0183 sentences from a UART GPS.
// Compatible with u-blox NEO-M8N and any standard NMEA GPS.
type NMEAProvider struct {
	portPath string
	baudRate int
	port     serial.Port
	scanner  *bufio.Scanner
	mu       sync.Mutex
	last     *Data
}

// NMEAConfig holds configuration for the NMEA GPS provider.
type NMEAConfig struct {
	PortPath string `yaml:"port_path" json:"portPath"`
	BaudRate int    `yaml:"baud_rate" json:"baudRate"`
}

// NewNMEA creates a new NMEA GPS provider.
func NewNMEA(cfg NMEAConfig) *NMEAProvider {
	if cfg.BaudRate == 0 {
		cfg.BaudRate = 9600 // Standard NMEA default
	}
	return &NMEAProvider{
		portPath: cfg.PortPath,
		baudRate: cfg.BaudRate,
		last:     &Data{},
	}
}

func (n *NMEAProvider) Name() string { return "NMEA GPS" }

func (n *NMEAProvider) Connect() error {
	mode := &serial.Mode{
		BaudRate: n.baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(n.portPath, mode)
	if err != nil {
		return fmt.Errorf("gps: failed to open %s: %w", n.portPath, err)
	}
	port.SetReadTimeout(200 * time.Millisecond)
	n.port = port
	n.scanner = bufio.NewScanner(port)
	log.Printf("[gps] connected to %s at %d baud", n.portPath, n.baudRate)
	return nil
}

func (n *NMEAProvider) Close() error {
	if n.port != nil {
		return n.port.Close()
	}
	return nil
}

// Read reads NMEA sentences until we have a complete fix update, or timeout.
func (n *NMEAProvider) Read() (*Data, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.scanner == nil {
		return n.last, fmt.Errorf("gps: not connected")
	}

	// Read up to 20 lines to find RMC + GGA
	gotRMC := false
	gotGGA := false
	for i := 0; i < 20 && !(gotRMC && gotGGA); i++ {
		if !n.scanner.Scan() {
			break
		}
		line := strings.TrimSpace(n.scanner.Text())
		if !strings.HasPrefix(line, "$") {
			continue
		}
		// Validate checksum
		if !validateNMEAChecksum(line) {
			continue
		}

		if strings.HasPrefix(line, "$GPRMC") || strings.HasPrefix(line, "$GNRMC") {
			n.parseRMC(line)
			gotRMC = true
		} else if strings.HasPrefix(line, "$GPGGA") || strings.HasPrefix(line, "$GNGGA") {
			n.parseGGA(line)
			gotGGA = true
		}
	}

	return n.last, nil
}

func (n *NMEAProvider) parseRMC(line string) {
	// $GPRMC,hhmmss.ss,A,llll.ll,a,yyyyy.yy,a,x.x,x.x,ddmmyy,x.x,a*hh
	parts := splitNMEA(line)
	if len(parts) < 10 {
		return
	}

	n.last.Timestamp = parts[1]
	n.last.Valid = parts[2] == "A"

	if n.last.Valid {
		n.last.Latitude = parseNMEACoord(parts[3], parts[4])
		n.last.Longitude = parseNMEACoord(parts[5], parts[6])

		if spd, err := strconv.ParseFloat(parts[7], 64); err == nil {
			n.last.Speed = spd * 1.852 // Knots to km/h
		}
		if hdg, err := strconv.ParseFloat(parts[8], 64); err == nil {
			n.last.Heading = hdg
		}
	}
}

func (n *NMEAProvider) parseGGA(line string) {
	// $GPGGA,hhmmss.ss,llll.ll,a,yyyyy.yy,a,x,xx,x.x,x.x,M,x.x,M,x.x,xxxx*hh
	parts := splitNMEA(line)
	if len(parts) < 11 {
		return
	}

	if fix, err := strconv.Atoi(parts[6]); err == nil {
		n.last.FixQuality = fix
	}
	if sats, err := strconv.Atoi(parts[7]); err == nil {
		n.last.Satellites = sats
	}
	if hdop, err := strconv.ParseFloat(parts[8], 64); err == nil {
		n.last.HDOP = hdop
	}
	if alt, err := strconv.ParseFloat(parts[9], 64); err == nil {
		n.last.Altitude = alt
	}
}

// splitNMEA splits a sentence and strips the checksum suffix.
func splitNMEA(line string) []string {
	// Strip checksum: everything after *
	if idx := strings.Index(line, "*"); idx >= 0 {
		line = line[:idx]
	}
	// Strip leading $
	if strings.HasPrefix(line, "$") {
		line = line[1:]
	}
	return strings.Split(line, ",")
}

// parseNMEACoord converts NMEA ddmm.mmmm format to decimal degrees.
func parseNMEACoord(raw, dir string) float64 {
	if raw == "" || dir == "" {
		return 0
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	deg := math.Floor(val / 100)
	min := val - deg*100
	result := deg + min/60

	if dir == "S" || dir == "W" {
		result = -result
	}
	return result
}

// validateNMEAChecksum checks the XOR checksum after *.
func validateNMEAChecksum(line string) bool {
	idx := strings.Index(line, "*")
	if idx < 0 || idx+3 > len(line) {
		return false
	}
	body := line[1:idx] // Between $ and *
	var calc byte
	for i := 0; i < len(body); i++ {
		calc ^= body[i]
	}
	expected, err := strconv.ParseUint(line[idx+1:idx+3], 16, 8)
	if err != nil {
		return false
	}
	return byte(expected) == calc
}

// DemoGPS generates simulated GPS data for testing.
type DemoGPS struct {
	mu sync.Mutex
	t  float64
}

func NewDemoGPS() *DemoGPS { return &DemoGPS{} }

func (d *DemoGPS) Name() string   { return "Demo GPS (Simulated)" }
func (d *DemoGPS) Connect() error { return nil }
func (d *DemoGPS) Close() error   { return nil }

func (d *DemoGPS) Read() (*Data, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.t += 0.1

	// Simulate driving in a circle around a point
	centerLat := 43.6532 // Toronto
	centerLon := -79.3832
	radius := 0.005 // ~500m

	return &Data{
		Valid:      true,
		Latitude:   centerLat + radius*math.Sin(d.t*0.1),
		Longitude:  centerLon + radius*math.Cos(d.t*0.1),
		Speed:      50 + 30*math.Sin(d.t*0.3) + rand.Float64()*5,
		Heading:    math.Mod(d.t*10, 360),
		Altitude:   76,
		Satellites: 12,
		FixQuality: 1,
		HDOP:       0.8,
		Timestamp:  time.Now().UTC().Format("150405.00"),
	}, nil
}
