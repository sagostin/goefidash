package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds all dashboard configuration.
type Config struct {
	mu sync.RWMutex

	// Serial ports
	ECU ECUConfig `yaml:"ecu" json:"ecu"`
	GPS GPSConfig `yaml:"gps" json:"gps"`

	// Display preferences
	Display DisplayConfig `yaml:"display" json:"display"`

	// Drivetrain (gear detection)
	Drivetrain DrivetrainConfig `yaml:"drivetrain" json:"drivetrain"`

	// Vehicle physics (HP estimation)
	Vehicle VehicleConfig `yaml:"vehicle" json:"vehicle"`

	// Logging
	Logging LoggingConfig `yaml:"logging" json:"logging"`

	// Server
	Server ServerConfig `yaml:"server" json:"server"`

	path string // file path for save/load
}

type ECUConfig struct {
	Type     string  `yaml:"type" json:"type"`          // "speeduino" or "demo"
	PortPath string  `yaml:"port_path" json:"portPath"` // e.g. /dev/ttySpeeduino
	BaudRate int     `yaml:"baud_rate" json:"baudRate"`
	CanID    int     `yaml:"can_id" json:"canId"`
	Stoich   float64 `yaml:"stoich" json:"stoich"`
	PollHz   int     `yaml:"poll_hz" json:"pollHz"`    // ECU polling rate
	Protocol string  `yaml:"protocol" json:"protocol"` // "generic" or "tunerstudio"
}

type GPSConfig struct {
	Type     string `yaml:"type" json:"type"`          // "nmea" or "demo" or "disabled"
	PortPath string `yaml:"port_path" json:"portPath"` // e.g. /dev/ttyGPS
	BaudRate int    `yaml:"baud_rate" json:"baudRate"`
}

type DisplayConfig struct {
	Units      UnitsConfig     `yaml:"units" json:"units"`
	Thresholds ThresholdConfig `yaml:"thresholds" json:"thresholds"`
	Layout     string          `yaml:"layout" json:"layout"` // "race", "street", "minimal"
}

type UnitsConfig struct {
	Temperature string `yaml:"temperature" json:"temperature"` // "C" or "F"
	Pressure    string `yaml:"pressure" json:"pressure"`       // "kpa", "psi", "bar"
	Speed       string `yaml:"speed" json:"speed"`             // "kph" or "mph"
	AFR         string `yaml:"afr" json:"afr"`                 // "afr" or "lambda"
}

type ThresholdConfig struct {
	RPMWarn     uint16  `yaml:"rpm_warn" json:"rpmWarn"`
	RPMDanger   uint16  `yaml:"rpm_danger" json:"rpmDanger"`
	RPMMax      uint16  `yaml:"rpm_max" json:"rpmMax"`
	CLTWarn     float64 `yaml:"clt_warn" json:"cltWarn"`     // °C
	CLTDanger   float64 `yaml:"clt_danger" json:"cltDanger"` // °C
	IATWarn     float64 `yaml:"iat_warn" json:"iatWarn"`     // °C
	IATDanger   float64 `yaml:"iat_danger" json:"iatDanger"` // °C
	AFRLeanWarn float64 `yaml:"afr_lean_warn" json:"afrLeanWarn"`
	AFRRichWarn float64 `yaml:"afr_rich_warn" json:"afrRichWarn"`
	OilPWarn    uint8   `yaml:"oil_p_warn" json:"oilPWarn"`
	BattLow     float64 `yaml:"batt_low" json:"battLow"`
	BattHigh    float64 `yaml:"batt_high" json:"battHigh"`
	KnockWarn   uint8   `yaml:"knock_warn" json:"knockWarn"` // degrees retard
}

// DrivetrainConfig holds gear ratios for RPM-based gear detection.
// If GearRatios is non-empty the dashboard will calculate the current gear
// from RPM and vehicle speed instead of using the ECU's reported value.
type DrivetrainConfig struct {
	ShowGear      bool      `yaml:"show_gear" json:"showGear"`
	GearRatios    []float64 `yaml:"gear_ratios" json:"gearRatios"`       // [1st, 2nd, 3rd, ...]
	FinalDrive    float64   `yaml:"final_drive" json:"finalDrive"`       // Diff ratio
	TireCircumM   float64   `yaml:"tire_circum_m" json:"tireCircumM"`    // Tire circumference in meters
	GearTolerance float64   `yaml:"gear_tolerance" json:"gearTolerance"` // Match tolerance (0.0-1.0), default 0.15
}

// VehicleConfig holds physical parameters for HP estimation.
type VehicleConfig struct {
	MassKg        float64 `yaml:"mass_kg" json:"massKg"`                // Vehicle mass in kg
	DragCoeff     float64 `yaml:"drag_coeff" json:"dragCoeff"`          // Aerodynamic drag coefficient
	FrontalAreaM2 float64 `yaml:"frontal_area_m2" json:"frontalAreaM2"` // Frontal area in m²
	RollingResist float64 `yaml:"rolling_resist" json:"rollingResist"`  // Rolling resistance coefficient
}

type LoggingConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Path     string `yaml:"path" json:"path"`
	Interval int    `yaml:"interval_ms" json:"intervalMs"` // ms between log entries
}

type ServerConfig struct {
	ListenAddr string `yaml:"listen_addr" json:"listenAddr"`
	Kiosk      bool   `yaml:"kiosk" json:"kiosk"` // Auto-launch Chromium
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ECU: ECUConfig{
			Type:     "demo",
			PortPath: "/dev/ttySpeeduino",
			BaudRate: 115200,
			CanID:    0,
			Stoich:   14.7,
			PollHz:   20,
			Protocol: "generic",
		},
		GPS: GPSConfig{
			Type:     "demo",
			PortPath: "/dev/ttyGPS",
			BaudRate: 9600,
		},
		Display: DisplayConfig{
			Units: UnitsConfig{
				Temperature: "C",
				Pressure:    "psi",
				Speed:       "kph",
				AFR:         "afr",
			},
			Thresholds: ThresholdConfig{
				RPMWarn:     6000,
				RPMDanger:   7000,
				RPMMax:      8000,
				CLTWarn:     95,
				CLTDanger:   105,
				IATWarn:     60,
				IATDanger:   75,
				AFRLeanWarn: 15.5,
				AFRRichWarn: 12.0,
				OilPWarn:    15,
				BattLow:     12.0,
				BattHigh:    15.5,
				KnockWarn:   3,
			},
			Layout: "classic",
		},
		Drivetrain: DrivetrainConfig{
			ShowGear:      true,
			GearRatios:    nil,
			FinalDrive:    3.73,
			TireCircumM:   1.95,
			GearTolerance: 0.15,
		},
		Vehicle: VehicleConfig{
			MassKg:        1200,
			DragCoeff:     0.32,
			FrontalAreaM2: 2.2,
			RollingResist: 0.012,
		},
		Logging: LoggingConfig{
			Enabled:  false,
			Path:     "/var/log/speeduino-dash",
			Interval: 100,
		},
		Server: ServerConfig{
			ListenAddr: ":8080",
			Kiosk:      false,
		},
	}
}

// LoadConfig reads config from a YAML file, then applies .env and environment
// variable overrides. Falls back to defaults if YAML not found.
func LoadConfig(path string) *Config {
	cfg := DefaultConfig()
	cfg.path = path

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[config] no config at %s, using defaults", path)
	} else if err := yaml.Unmarshal(data, cfg); err != nil {
		log.Printf("[config] error parsing %s: %v, using defaults", path, err)
		cfg = DefaultConfig()
		cfg.path = path
	} else {
		log.Printf("[config] loaded from %s", path)
	}

	// Load .env file from the same directory as the config, or from CWD
	envPaths := []string{
		filepath.Join(filepath.Dir(path), ".env"),
		".env",
	}
	for _, ep := range envPaths {
		loadEnvFile(ep)
	}

	// Apply environment variable overrides
	cfg.applyEnvOverrides()
	return cfg
}

// loadEnvFile reads a simple KEY=VALUE .env file and sets os env vars.
func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	log.Printf("[config] loading .env from %s", path)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding quotes
		val = strings.Trim(val, `"'`)
		// Only set if not already set in real env (real env takes precedence)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

// applyEnvOverrides reads environment variables and overrides config values.
// Supported: ECU_TYPE, ECU_PORT, ECU_BAUD, ECU_STOICH, GPS_TYPE, GPS_PORT,
// GPS_BAUD, LISTEN_ADDR, TEMP_UNIT, PRESSURE_UNIT, SPEED_UNIT
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("ECU_TYPE"); v != "" {
		c.ECU.Type = v
	}
	if v := os.Getenv("ECU_PORT"); v != "" {
		c.ECU.PortPath = v
	}
	if v := os.Getenv("ECU_BAUD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.ECU.BaudRate = n
		}
	}
	if v := os.Getenv("ECU_STOICH"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			c.ECU.Stoich = n
		}
	}
	if v := os.Getenv("GPS_TYPE"); v != "" {
		c.GPS.Type = v
	}
	if v := os.Getenv("GPS_PORT"); v != "" {
		c.GPS.PortPath = v
	}
	if v := os.Getenv("GPS_BAUD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.GPS.BaudRate = n
		}
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		c.Server.ListenAddr = v
	}
	if v := os.Getenv("TEMP_UNIT"); v != "" {
		c.Display.Units.Temperature = v
	}
	if v := os.Getenv("PRESSURE_UNIT"); v != "" {
		c.Display.Units.Pressure = v
	}
	if v := os.Getenv("SPEED_UNIT"); v != "" {
		c.Display.Units.Speed = v
	}
	if v := os.Getenv("ECU_PROTOCOL"); v != "" {
		c.ECU.Protocol = v
	}
	// Logging
	if v := os.Getenv("LOG_ENABLED"); v != "" {
		c.Logging.Enabled = v == "1" || v == "true" || v == "yes"
	}
	if v := os.Getenv("LOG_PATH"); v != "" {
		c.Logging.Path = v
	}
	if v := os.Getenv("LOG_INTERVAL_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Logging.Interval = n
		}
	}
}

// Save writes the config to its YAML file.
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.path == "" {
		c.path = "/etc/speeduino-dash/config.yaml"
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0644)
}

// ToJSON serializes config for the API.
func (c *Config) ToJSON() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return json.Marshal(c)
}

// UpdateFromJSON applies a partial JSON config update by deep-merging
// incoming fields into the existing config. Fields not present in the
// incoming JSON are preserved (e.g. port paths, baud rates, logging).
func (c *Config) UpdateFromJSON(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Marshal current config to a generic map
	currentBytes, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal current config: %w", err)
	}
	var base map[string]interface{}
	if err := json.Unmarshal(currentBytes, &base); err != nil {
		return fmt.Errorf("unmarshal current config: %w", err)
	}

	// Unmarshal incoming partial update to a map
	var patch map[string]interface{}
	if err := json.Unmarshal(data, &patch); err != nil {
		return fmt.Errorf("unmarshal patch: %w", err)
	}

	// Deep merge patch into base
	deepMerge(base, patch)

	// Marshal merged result and unmarshal back into the config struct
	merged, err := json.Marshal(base)
	if err != nil {
		return fmt.Errorf("marshal merged config: %w", err)
	}
	return json.Unmarshal(merged, c)
}

// deepMerge recursively merges src into dst. For nested maps, values are
// merged rather than replaced. For all other types, src overwrites dst.
func deepMerge(dst, src map[string]interface{}) {
	for key, srcVal := range src {
		if srcMap, ok := srcVal.(map[string]interface{}); ok {
			if dstMap, ok := dst[key].(map[string]interface{}); ok {
				deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[key] = srcVal
	}
}
