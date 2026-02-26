package gps

// Provider is the interface for GPS data sources.
type Provider interface {
	Name() string
	Connect() error
	Close() error
	// Read returns the latest GPS fix. May block briefly.
	Read() (*Data, error)
}

// Data holds a single GPS fix.
type Data struct {
	Valid      bool    `json:"valid"`      // Fix is valid
	Latitude   float64 `json:"latitude"`   // Decimal degrees
	Longitude  float64 `json:"longitude"`  // Decimal degrees
	Speed      float64 `json:"speed"`      // km/h
	Heading    float64 `json:"heading"`    // Degrees true
	Altitude   float64 `json:"altitude"`   // Meters
	Satellites int     `json:"satellites"` // Sats in use
	FixQuality int     `json:"fixQuality"` // 0=none, 1=GPS, 2=DGPS
	HDOP       float64 `json:"hdop"`       // Horizontal dilution
	Timestamp  string  `json:"timestamp"`  // UTC time string
}
