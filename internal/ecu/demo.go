package ecu

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// DemoProvider generates simulated ECU data for development and testing.
type DemoProvider struct {
	mu        sync.Mutex
	connected bool
	startTime time.Time
	stoich    float64
}

// NewDemoProvider creates a demo ECU provider that generates realistic
// simulated engine data using sine-wave patterns.
func NewDemoProvider() *DemoProvider {
	return &DemoProvider{
		stoich: 14.7,
	}
}

func (d *DemoProvider) Connect() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected = true
	d.startTime = time.Now()
	return nil
}

func (d *DemoProvider) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connected = false
	return nil
}

func (d *DemoProvider) IsConnected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}

// RequestRawData returns a synthetic RawData with a pre-built 123-byte payload
// that will parse correctly through parseGenericFixed.
func (d *DemoProvider) RequestRawData() (*RawData, error) {
	d.mu.Lock()
	if !d.connected {
		d.mu.Unlock()
		return nil, nil
	}
	d.mu.Unlock()

	t := time.Since(d.startTime).Seconds()

	// Generate realistic oscillating values
	rpmBase := 2500.0 + 2000.0*math.Sin(t*0.3)
	rpm := uint16(math.Max(800, rpmBase+rand.Float64()*100))
	mapVal := uint16(60 + 30*math.Sin(t*0.5))
	tps := uint8(30 + 25*math.Sin(t*0.4))
	iat := uint8(65)   // 25°C after -40 offset
	clt := uint8(130)  // 90°C after -40 offset
	batt := uint8(140) // 14.0V
	afr := uint8(147)  // 14.7 AFR
	advance := uint8(28 + int8(10*math.Sin(t*0.2)))
	pw1 := uint16(3000 + 1500*math.Sin(t*0.3)) // µS

	// Build a 123-byte payload matching the fixed layout
	data := make([]byte, NewCANPacketSize)

	data[0] = uint8(int(t) % 256)         // secl
	data[1] = 0x01                        // status1: inj1 active
	data[2] = 0x01                        // engine: running
	data[3] = uint8(float64(pw1) / 100.0) // dwell approximation

	data[4] = byte(mapVal & 0xFF) // MAP low
	data[5] = byte(mapVal >> 8)   // MAP high
	data[6] = iat                 // IAT
	data[7] = clt                 // CLT
	data[8] = 100                 // batCorrection %
	data[9] = batt                // battery10
	data[10] = afr                // O2/AFR
	data[11] = 100                // egoCorrection
	data[12] = 100                // iatCorrection
	data[13] = 100                // wueCorrection

	data[14] = byte(rpm & 0xFF)               // RPM low
	data[15] = byte(rpm >> 8)                 // RPM high
	data[16] = 0                              // TAEamount
	data[17] = 100                            // corrections (GammaE)
	data[18] = uint8(70 + 15*math.Sin(t*0.2)) // VE
	data[19] = afr                            // afrTarget

	data[20] = byte(pw1 & 0xFF) // PW1 low
	data[21] = byte(pw1 >> 8)   // PW1 high
	data[22] = 0                // tpsDOT
	data[23] = advance          // advance
	data[24] = tps              // TPS

	data[25] = byte(5000 & 0xFF) // loopsPerSec low
	data[26] = byte(5000 >> 8)   // loopsPerSec high
	data[27] = byte(2048 & 0xFF) // freeRAM low
	data[28] = byte(2048 >> 8)   // freeRAM high

	data[29] = 50   // boostTarget
	data[30] = 0    // boostDuty
	data[31] = 0x04 // spark: synced
	data[40] = 101  // baro (kPa)

	// Enhanced bytes 75+
	data[75] = 0                // launchCorrection
	data[76] = byte(pw1 & 0xFF) // PW2 low
	data[77] = byte(pw1 >> 8)   // PW2 high
	data[82] = 0                // status3
	data[83] = 0                // engineProtect
	data[91] = 0                // CLIdleTarget
	data[98] = 100              // baroCorrection
	data[99] = 0                // ASEValue

	speed := uint16(60 + 30*math.Sin(t*0.15))
	data[100] = byte(speed & 0xFF) // VSS low
	data[101] = byte(speed >> 8)   // VSS high
	data[102] = 3                  // gear
	data[111] = 75                 // fuelTemp (35°C + 40)
	data[113] = data[18]           // VE1
	data[115] = advance            // advance1
	data[121] = 0                  // fanDuty

	return &RawData{
		Payload:  data,
		Command:  'n',
		Protocol: ProtoGenericFixed,
	}, nil
}

// ParseRawData parses the demo data through the real parser.
func (d *DemoProvider) ParseRawData(raw *RawData) *DataFrame {
	if raw == nil {
		return nil
	}
	return parseGenericFixed(raw.Payload, d.stoich)
}

// Compile-time interface check.
var _ Provider = (*DemoProvider)(nil)
