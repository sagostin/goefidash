package logger

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shaunagostinho/speeduino-dash/internal/ecu"
	"github.com/shaunagostinho/speeduino-dash/internal/gps"
)

// Logger records timestamped ECU + GPS data to CSV files with automatic rotation.
type Logger struct {
	mu       sync.Mutex
	dir      string
	interval time.Duration
	enabled  bool

	file   *os.File
	writer *csv.Writer
	lastTs time.Time
	rows   int
}

// Config holds logger configuration.
type Config struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Path       string `yaml:"path" json:"path"`
	IntervalMs int    `yaml:"interval_ms" json:"intervalMs"`
}

const (
	maxRowsPerFile = 100_000 // Rotate after 100k rows (~2.7 hrs at 10 Hz)
)

var csvHeader = []string{
	"timestamp", "rpm", "map_kpa", "tps_pct", "afr", "lambda",
	"coolant_c", "iat_c", "advance_deg", "battery_v",
	"pw1_ms", "pw2_ms", "duty_pct", "ve",
	"boost_target", "boost_duty", "vss_kph", "gear",
	"fuel_psi", "oil_psi", "dwell_ms",
	"ego_cor", "warmup_enrich", "gamma",
	"fan_on", "sync", "running",
	"gps_valid", "gps_lat", "gps_lon", "gps_speed_kph",
	"gps_heading", "gps_alt_m", "gps_sats",
}

// New creates a new Logger.
func New(cfg Config) *Logger {
	if cfg.Path == "" {
		cfg.Path = "/var/log/speeduino-dash"
	}
	interval := time.Duration(cfg.IntervalMs) * time.Millisecond
	if interval < 50*time.Millisecond {
		interval = 100 * time.Millisecond // Default 10 Hz
	}
	return &Logger{
		dir:      cfg.Path,
		interval: interval,
		enabled:  cfg.Enabled,
	}
}

// SetEnabled allows toggling logging at runtime.
func (l *Logger) SetEnabled(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = on
	if !on && l.file != nil {
		l.closeFile()
	}
}

// IsEnabled returns whether logging is active.
func (l *Logger) IsEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// Record writes an ECU + GPS snapshot if the minimum interval has elapsed.
func (l *Logger) Record(ecuData *ecu.DataFrame, gpsData *gps.Data) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.enabled {
		return
	}

	now := time.Now()
	if now.Sub(l.lastTs) < l.interval {
		return
	}
	l.lastTs = now

	// Open/rotate file if needed
	if l.writer == nil || l.rows >= maxRowsPerFile {
		if err := l.rotateFile(now); err != nil {
			log.Printf("[logger] rotate failed: %v", err)
			return
		}
	}

	row := l.buildRow(now, ecuData, gpsData)
	if err := l.writer.Write(row); err != nil {
		log.Printf("[logger] write failed: %v", err)
		return
	}
	l.writer.Flush()
	l.rows++
}

// Close flushes and closes the current log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeFile()
}

func (l *Logger) rotateFile(now time.Time) error {
	l.closeFile()

	if err := os.MkdirAll(l.dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", l.dir, err)
	}

	filename := fmt.Sprintf("speeduino_%s.csv", now.Format("2006-01-02_150405"))
	path := filepath.Join(l.dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	l.file = f
	l.writer = csv.NewWriter(f)
	l.rows = 0

	// Write header
	if err := l.writer.Write(csvHeader); err != nil {
		return err
	}
	l.writer.Flush()

	log.Printf("[logger] opened %s", path)
	return nil
}

func (l *Logger) closeFile() {
	if l.writer != nil {
		l.writer.Flush()
		l.writer = nil
	}
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

func (l *Logger) buildRow(ts time.Time, e *ecu.DataFrame, g *gps.Data) []string {
	row := make([]string, len(csvHeader))

	row[0] = ts.Format(time.RFC3339Nano)

	if e != nil {
		row[1] = fmt.Sprintf("%d", e.RPM)
		row[2] = fmt.Sprintf("%d", e.MAP)
		row[3] = fmt.Sprintf("%.1f", e.TPS)
		row[4] = fmt.Sprintf("%.1f", e.AFR)
		row[5] = fmt.Sprintf("%.3f", e.Lambda)
		row[6] = fmt.Sprintf("%.1f", e.Coolant)
		row[7] = fmt.Sprintf("%.1f", e.IAT)
		row[8] = fmt.Sprintf("%d", e.Advance)
		row[9] = fmt.Sprintf("%.1f", e.BatteryVoltage)
		row[10] = fmt.Sprintf("%.3f", e.PulseWidth1)
		row[11] = fmt.Sprintf("%.3f", e.PulseWidth2)
		row[12] = fmt.Sprintf("%.1f", e.DutyCycle)
		row[13] = fmt.Sprintf("%d", e.VECurr)
		row[14] = fmt.Sprintf("%d", e.BoostTarget)
		row[15] = fmt.Sprintf("%d", e.BoostDuty)
		row[16] = fmt.Sprintf("%d", e.VSS)
		row[17] = fmt.Sprintf("%d", e.Gear)
		row[18] = fmt.Sprintf("%d", e.FuelPressure)
		row[19] = fmt.Sprintf("%d", e.OilPressure)
		row[20] = fmt.Sprintf("%.1f", e.Dwell)
		row[21] = fmt.Sprintf("%d", e.EGOCorrection)
		row[22] = fmt.Sprintf("%d", e.WarmupEnrich)
		row[23] = fmt.Sprintf("%d", e.GammaEnrich)
		row[24] = boolStr(e.FanStatus)
		row[25] = boolStr(e.Sync)
		row[26] = boolStr(e.Running)
	}

	if g != nil {
		row[27] = boolStr(g.Valid)
		row[28] = fmt.Sprintf("%.6f", g.Latitude)
		row[29] = fmt.Sprintf("%.6f", g.Longitude)
		row[30] = fmt.Sprintf("%.1f", g.Speed)
		row[31] = fmt.Sprintf("%.1f", g.Heading)
		row[32] = fmt.Sprintf("%.1f", g.Altitude)
		row[33] = fmt.Sprintf("%d", g.Satellites)
	}

	return row
}

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
