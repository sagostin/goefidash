package ecu

// Provider is the interface that all ECU backends must implement.
// Speeduino is the first implementation; RuSEFI can be added later
// by implementing this same interface.
type Provider interface {
	// Name returns the human-readable name of this ECU provider.
	Name() string
	// Connect opens the serial port and verifies communication.
	Connect() error
	// Close cleanly shuts down the serial connection.
	Close() error
	// IsConnected returns whether the provider has an active connection.
	IsConnected() bool

	// RequestRawData performs serial I/O only: sends the poll command
	// and reads the raw response bytes. No parsing is done.
	// This should be called from the dedicated serial goroutine.
	RequestRawData() (*RawData, error)

	// ParseRawData parses raw bytes into a DataFrame.
	// This is CPU-only (no I/O) and safe to call from any goroutine.
	ParseRawData(raw *RawData) *DataFrame

	// RequestData is a convenience that calls RequestRawData + ParseRawData.
	// Prefer the split methods for async pipelines.
	RequestData() (*DataFrame, error)
}

// RawData carries the raw serial response for deferred async parsing.
type RawData struct {
	Tag  string // Protocol tag for the parser (e.g. "generic-n", "generic-a", "tunerstudio")
	Data []byte // Raw response bytes (after framing/header stripped)
}

// DataFrame holds all parsed realtime engine data channels.
// Field names match the Speeduino OutputChannels from speeduino.ini.
type DataFrame struct {
	// Core engine
	RPM      uint16  `json:"rpm"`
	MAP      uint16  `json:"map"`      // kPa
	TPS      float64 `json:"tps"`      // 0-100%
	AFR      float64 `json:"afr"`      // Air-fuel ratio
	Lambda   float64 `json:"lambda"`   // Calculated from AFR/stoich
	Advance  int8    `json:"advance"`  // Ignition advance (deg)
	Advance1 int8    `json:"advance1"` // Advance table 1
	Advance2 int8    `json:"advance2"` // Advance table 2

	// Temperatures (°C, raw - 40 offset applied)
	Coolant float64 `json:"coolant"` // CLT
	IAT     float64 `json:"iat"`     // Intake air temp

	// Fuel
	PulseWidth1 float64 `json:"pulseWidth1"` // ms
	PulseWidth2 float64 `json:"pulseWidth2"` // ms
	PulseWidth3 float64 `json:"pulseWidth3"` // ms
	PulseWidth4 float64 `json:"pulseWidth4"` // ms
	VE1         uint8   `json:"ve1"`         // Volumetric efficiency %
	VE2         uint8   `json:"ve2"`         // VE table 2 %
	VECurr      uint8   `json:"veCurr"`      // Current VE
	AFRTarget   float64 `json:"afrTarget"`   // Target AFR
	DutyCycle   float64 `json:"dutyCycle"`   // Calculated injector duty %

	// Corrections
	GammaEnrich    uint16 `json:"gammaEnrich"`    // Total gamma %
	EGOCorrection  uint8  `json:"egoCorrection"`  // O2 correction %
	AirCorrection  uint8  `json:"airCorrection"`  // IAT correction %
	WarmupEnrich   uint8  `json:"warmupEnrich"`   // WUE %
	BatCorrection  uint8  `json:"batCorrection"`  // Battery correction %
	ASECurr        uint8  `json:"aseCurr"`        // Afterstart enrichment %
	BaroCorrection uint8  `json:"baroCorrection"` // Baro correction %
	AccelEnrich    uint8  `json:"accelEnrich"`    // AE %

	// Electrical
	BatteryVoltage float64 `json:"batteryVoltage"` // Volts
	Dwell          float64 `json:"dwell"`          // ms
	DwellActual    float64 `json:"dwellActual"`    // ms

	// Boost
	BoostTarget uint8 `json:"boostTarget"` // kPa (×2)
	BoostDuty   uint8 `json:"boostDuty"`   // %

	// Speed / transmission
	VSS  uint16 `json:"vss"`  // km/h
	Gear uint8  `json:"gear"` // Current gear

	// Pressures
	FuelPressure uint8 `json:"fuelPressure"` // PSI
	OilPressure  uint8 `json:"oilPressure"`  // PSI

	// VVT
	VVT1Angle  float64 `json:"vvt1Angle"`
	VVT1Target float64 `json:"vvt1Target"`
	VVT1Duty   float64 `json:"vvt1Duty"`
	VVT2Angle  float64 `json:"vvt2Angle"`
	VVT2Target float64 `json:"vvt2Target"`
	VVT2Duty   float64 `json:"vvt2Duty"`

	// Flex fuel
	FlexPct     uint8 `json:"flexPct"`     // Ethanol %
	FlexFuelCor uint8 `json:"flexFuelCor"` // Flex fuel correction %
	FlexIgnCor  int8  `json:"flexIgnCor"`  // Flex ignition correction deg

	// Exhaust
	AFR2 float64 `json:"afr2"` // Second O2
	EMAP uint16  `json:"emap"` // Exhaust MAP kPa
	Baro uint8   `json:"baro"` // Barometric pressure kPa

	// Idle
	IdleLoad     uint8  `json:"idleLoad"`
	CLIdleTarget uint16 `json:"clIdleTarget"` // RPM (×10)

	// Knock
	KnockCount uint8 `json:"knockCount"` // Event count
	KnockCor   uint8 `json:"knockCor"`   // Correction deg

	// Status bits
	Running   bool `json:"running"`
	Cranking  bool `json:"cranking"`
	ASE       bool `json:"ase"` // Afterstart enrichment active
	Warmup    bool `json:"warmup"`
	DFCOOn    bool `json:"dfcoOn"` // Decel fuel cut
	Sync      bool `json:"sync"`   // Trigger sync
	FanStatus bool `json:"fanStatus"`

	// Load
	FuelLoad float64 `json:"fuelLoad"` // Current fuel load axis value
	IgnLoad  float64 `json:"ignLoad"`  // Current ign load axis value
	MAPdot   int16   `json:"mapDot"`   // kPa/s
	RPMdot   int16   `json:"rpmDot"`   // rpm/s

	// Errors
	Errors   uint8 `json:"errors"`
	SyncLoss uint8 `json:"syncLoss"` // Sync loss counter

	// Misc
	LoopsPerSecond uint16  `json:"loopsPerSecond"`
	FreeRAM        uint16  `json:"freeRAM"`
	FanDuty        float64 `json:"fanDuty"` // %
	SDStatus       uint8   `json:"sdStatus"`

	// Seconds counter
	Secl uint8 `json:"secl"`
}
