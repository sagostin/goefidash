package ecu

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Speeduino implements the Provider interface for Speeduino ECUs
// using the TunerStudio r-command protocol with msEnvelope CRC32 framing.
type Speeduino struct {
	portPath string
	baudRate int
	canID    byte
	port     serial.Port
	mu       sync.Mutex
	stoich   float64 // Stoichiometric ratio for lambda calc
}

// SpeeduinoConfig holds connection configuration for the Speeduino provider.
type SpeeduinoConfig struct {
	PortPath string  `yaml:"port_path" json:"portPath"`
	BaudRate int     `yaml:"baud_rate" json:"baudRate"`
	CanID    byte    `yaml:"can_id" json:"canId"`
	Stoich   float64 `yaml:"stoich" json:"stoich"` // e.g. 14.7 for gasoline
}

const (
	speeduinoOCHBlockSize = 130
	rCommandType          = 0x30
)

// NewSpeeduino creates a new Speeduino ECU provider.
func NewSpeeduino(cfg SpeeduinoConfig) *Speeduino {
	if cfg.BaudRate == 0 {
		cfg.BaudRate = 115200
	}
	if cfg.Stoich == 0 {
		cfg.Stoich = 14.7
	}
	return &Speeduino{
		portPath: cfg.PortPath,
		baudRate: cfg.BaudRate,
		canID:    cfg.CanID,
		stoich:   cfg.Stoich,
	}
}

func (s *Speeduino) Name() string { return "Speeduino" }

func (s *Speeduino) Connect() error {
	mode := &serial.Mode{
		BaudRate: s.baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(s.portPath, mode)
	if err != nil {
		return fmt.Errorf("speeduino: failed to open %s: %w", s.portPath, err)
	}
	if err := port.SetReadTimeout(500 * time.Millisecond); err != nil {
		port.Close()
		return fmt.Errorf("speeduino: failed to set timeout: %w", err)
	}
	s.port = port
	log.Printf("[speeduino] connected to %s at %d baud", s.portPath, s.baudRate)
	return nil
}

func (s *Speeduino) Close() error {
	if s.port != nil {
		return s.port.Close()
	}
	return nil
}

// RequestData sends an 'r' command using the msEnvelope protocol and
// parses the 130-byte OutputChannels response into a DataFrame.
func (s *Speeduino) RequestData() (*DataFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.port == nil {
		return nil, fmt.Errorf("speeduino: not connected")
	}

	// Build the r-command payload:
	// 'r' <canId> <rType=0x30> <offset_lo> <offset_hi> <length_lo> <length_hi>
	offset := uint16(0)
	length := uint16(speeduinoOCHBlockSize)

	payload := []byte{
		'r',
		s.canID,
		rCommandType,
		byte(offset & 0xFF), byte(offset >> 8),
		byte(length & 0xFF), byte(length >> 8),
	}

	// msEnvelope: <size_hi> <size_lo> <payload...> <crc32 4 bytes big-endian>
	payloadLen := uint16(len(payload))
	envelope := make([]byte, 0, 2+len(payload)+4)
	envelope = append(envelope, byte(payloadLen>>8), byte(payloadLen&0xFF))
	envelope = append(envelope, payload...)

	crc := crc32.ChecksumIEEE(payload)
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)
	envelope = append(envelope, crcBytes...)

	// Flush any stale data
	s.port.ResetInputBuffer()

	// Send
	if _, err := s.port.Write(envelope); err != nil {
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	// Read response: the response is the raw data bytes (130 bytes)
	// In msEnvelope mode, response is: <data...> <crc32 4 bytes>
	respLen := speeduinoOCHBlockSize + 4 // data + CRC32
	resp := make([]byte, 0, respLen)
	deadline := time.Now().Add(1 * time.Second)

	for len(resp) < respLen && time.Now().Before(deadline) {
		buf := make([]byte, respLen-len(resp))
		n, err := s.port.Read(buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("speeduino: read failed: %w", err)
		}
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
	}

	if len(resp) < respLen {
		return nil, fmt.Errorf("speeduino: incomplete response: got %d bytes, want %d", len(resp), respLen)
	}

	// Validate CRC32
	data := resp[:speeduinoOCHBlockSize]
	respCRC := binary.BigEndian.Uint32(resp[speeduinoOCHBlockSize:])
	calcCRC := crc32.ChecksumIEEE(data)
	if respCRC != calcCRC {
		return nil, fmt.Errorf("speeduino: CRC mismatch: got 0x%08X, want 0x%08X", respCRC, calcCRC)
	}

	return s.parseOutputChannels(data), nil
}

// parseOutputChannels decodes the 130-byte OCH block into a DataFrame.
// Field offsets and types derived from speeduino.ini [OutputChannels].
func (s *Speeduino) parseOutputChannels(d []byte) *DataFrame {
	f := &DataFrame{}

	f.Secl = d[0]

	// Status1 bitfield (offset 1)
	f.DFCOOn = d[1]&(1<<4) != 0

	// Engine status (offset 2)
	f.Running = d[2]&(1<<0) != 0
	f.Cranking = d[2]&(1<<1) != 0
	f.ASE = d[2]&(1<<2) != 0
	f.Warmup = d[2]&(1<<3) != 0

	f.SyncLoss = d[3]

	// MAP (U16 LE, offset 4)
	f.MAP = binary.LittleEndian.Uint16(d[4:6])

	// Temperatures (raw + offset)
	f.IAT = float64(d[6]) - 40
	f.Coolant = float64(d[7]) - 40

	f.BatCorrection = d[8]
	f.BatteryVoltage = float64(d[9]) * 0.1

	f.AFR = float64(d[10]) * 0.1
	f.EGOCorrection = d[11]
	f.AirCorrection = d[12]
	f.WarmupEnrich = d[13]

	// RPM (U16 LE, offset 14)
	f.RPM = binary.LittleEndian.Uint16(d[14:16])

	f.AccelEnrich = d[16]

	// GammaE (U16 LE, offset 17) — note: ini says U16 at offset 17
	f.GammaEnrich = binary.LittleEndian.Uint16(d[17:19])

	f.VE1 = d[19]
	f.VE2 = d[20]
	f.AFRTarget = float64(d[21]) * 0.1

	// TPSdot (S16 LE, offset 22) — skipping for now, not in DataFrame

	// Advance (S08, offset 24)
	f.Advance = int8(d[24])

	// TPS (U08, offset 25, scale 0.5)
	f.TPS = float64(d[25]) * 0.5

	// Loops per second (U16 LE, offset 26)
	f.LoopsPerSecond = binary.LittleEndian.Uint16(d[26:28])

	// Free RAM (U16 LE, offset 28)
	f.FreeRAM = binary.LittleEndian.Uint16(d[28:30])

	// Boost (offset 30-31)
	f.BoostTarget = d[30] // ×2 kPa
	f.BoostDuty = d[31]

	// Status2 bitfield (offset 32)
	f.Sync = d[32]&(1<<7) != 0

	// rpmDOT (S16 LE, offset 33)
	f.RPMdot = int16(binary.LittleEndian.Uint16(d[33:35]))

	// Flex (offset 35-37)
	f.FlexPct = d[35]
	f.FlexFuelCor = d[36]
	f.FlexIgnCor = int8(d[37])

	f.IdleLoad = d[38]

	// AFR2 (offset 40)
	f.AFR2 = float64(d[40]) * 0.1
	f.Baro = d[41]

	// TPS ADC at 74, errors at 75 — skip to key fields

	f.Errors = d[75]

	// Pulse widths (U16 LE, offsets 76-83, scale 0.001 = ms)
	f.PulseWidth1 = float64(binary.LittleEndian.Uint16(d[76:78])) * 0.001
	f.PulseWidth2 = float64(binary.LittleEndian.Uint16(d[78:80])) * 0.001
	f.PulseWidth3 = float64(binary.LittleEndian.Uint16(d[80:82])) * 0.001
	f.PulseWidth4 = float64(binary.LittleEndian.Uint16(d[82:84])) * 0.001

	// Engine protect status (offset 85) — skip for now

	// Fuel/ign load (S16 LE, offsets 86, 88)
	f.FuelLoad = float64(int16(binary.LittleEndian.Uint16(d[86:88])))
	f.IgnLoad = float64(int16(binary.LittleEndian.Uint16(d[88:90])))

	// Dwell (U16 LE, offset 90, scale 0.001)
	f.Dwell = float64(binary.LittleEndian.Uint16(d[90:92])) * 0.001

	// CL Idle target (U08 offset 92, scale ×10)
	f.CLIdleTarget = uint16(d[92]) * 10

	// MAPdot (S16 LE, offset 93)
	f.MAPdot = int16(binary.LittleEndian.Uint16(d[93:95]))

	// VVT1 (offsets 95-98)
	f.VVT1Angle = float64(int16(binary.LittleEndian.Uint16(d[95:97]))) * 0.5
	f.VVT1Target = float64(d[97]) * 0.5
	f.VVT1Duty = float64(d[98]) * 0.5

	// Baro correction (offset 101)
	f.BaroCorrection = d[101]

	// VE current (offset 102)
	f.VECurr = d[102]
	f.ASECurr = d[103]

	// VSS (U16 LE, offset 104)
	f.VSS = binary.LittleEndian.Uint16(d[104:106])
	f.Gear = d[106]
	f.FuelPressure = d[107]
	f.OilPressure = d[108]

	// Status4 / fan (offset 110)
	f.FanStatus = d[110]&(1<<3) != 0

	// VVT2 (offsets 111-114)
	f.VVT2Angle = float64(int16(binary.LittleEndian.Uint16(d[111:113]))) * 0.5
	f.VVT2Target = float64(d[113]) * 0.5
	f.VVT2Duty = float64(d[114]) * 0.5

	// Advance 1 & 2 (offsets 118-119)
	f.Advance1 = int8(d[118])
	f.Advance2 = int8(d[119])

	f.SDStatus = d[120]

	// EMAP (U16 LE, offset 121)
	f.EMAP = binary.LittleEndian.Uint16(d[121:123])

	// Fan duty (offset 123, scale 0.5)
	f.FanDuty = float64(d[123]) * 0.5

	// Dwell actual (U16 LE, offset 125, scale 0.001)
	f.DwellActual = float64(binary.LittleEndian.Uint16(d[125:127])) * 0.001

	// Knock (offsets 128-129)
	f.KnockCount = d[128]
	f.KnockCor = d[129]

	// Calculated fields
	f.Lambda = f.AFR / s.stoich
	// Duty cycle: (PW1 / cycleTime) * 100
	if f.RPM > 0 {
		cycleTimeMs := 60000.0 / float64(f.RPM) * 2 // 4-stroke
		if cycleTimeMs > 0 {
			f.DutyCycle = (f.PulseWidth1 / cycleTimeMs) * 100
		}
	}

	return f
}
