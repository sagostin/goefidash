package ecu

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"log"
	"sync"
	"time"

	"go.bug.st/serial"
)

// protocolMode indicates which serial protocol variant is in use.
type protocolMode int

const (
	// protoGeneric is the plain Secondary Serial IO protocol.
	// Uses the 'n' command (119-byte enhanced data set) for polling,
	// with 'A' (75-byte legacy) as a connect-time fallback.
	// For secondarySerialProtocol = Generic (Fixed List) or Generic (ini File).
	protoGeneric protocolMode = iota
	// protoTunerStudio is the msEnvelope CRC32-framed protocol.
	// Uses the framed 'r' command to fetch the full 130-byte OCH block.
	// For secondarySerialProtocol = Tuner Studio, or the primary/USB port.
	protoTunerStudio
)

const (
	// TunerStudio / primary OCH block constants (from INI)
	ochBlockSize = 130
	rCommandType = 0x30

	// Generic / secondary data sizes
	genericNDataSize = 119 // Bytes returned by 'n' command (firmware 202409+)
	genericADataSize = 75  // Bytes returned by 'A' command (legacy)

	// Timing constants
	drainSilenceMs = 100                     // silence threshold for drain loop
	drainTimeout   = 1500 * time.Millisecond // max time to spend draining
	readTimeout    = 2 * time.Second         // per INI blockReadTimeout=2000
)

// Speeduino implements the Provider interface for Speeduino ECUs.
//
// Two explicit protocol modes, selected via config (no auto-detection):
//
//   - "generic"      — plain n/A commands on the secondary serial port
//   - "tunerstudio"  — msEnvelope CRC32-framed r command (primary/USB or
//     secondary port with secondarySerialProtocol="Tuner Studio")
//
// This driver is strictly read-only. It never sends write/burn/reset
// commands to the ECU, eliminating any risk of modifying ECU settings.
type Speeduino struct {
	portPath string
	baudRate int
	canID    byte
	port     serial.Port
	mu       sync.Mutex
	stoich   float64      // Stoichiometric ratio for lambda calc
	proto    protocolMode // Protocol mode
	useNCmd  bool         // true if generic mode uses 'n', false for 'A' fallback

	connected bool // True only after Connect() successfully handshakes
}

// SpeeduinoConfig holds connection configuration for the Speeduino provider.
type SpeeduinoConfig struct {
	PortPath string  `yaml:"port_path" json:"portPath"`
	BaudRate int     `yaml:"baud_rate" json:"baudRate"`
	CanID    byte    `yaml:"can_id" json:"canId"`
	Stoich   float64 `yaml:"stoich" json:"stoich"`     // e.g. 14.7 for gasoline
	Protocol string  `yaml:"protocol" json:"protocol"` // "tunerstudio" or "generic"
}

// NewSpeeduino creates a new Speeduino ECU provider.
func NewSpeeduino(cfg SpeeduinoConfig) *Speeduino {
	if cfg.BaudRate == 0 {
		cfg.BaudRate = 115200
	}
	if cfg.Stoich == 0 {
		cfg.Stoich = 14.7
	}

	proto := protoGeneric
	if cfg.Protocol == "tunerstudio" {
		proto = protoTunerStudio
	}

	return &Speeduino{
		portPath: cfg.PortPath,
		baudRate: cfg.BaudRate,
		canID:    cfg.CanID,
		stoich:   cfg.Stoich,
		proto:    proto,
		useNCmd:  true, // default to 'n' for generic, may fallback to 'A'
	}
}

func (s *Speeduino) Name() string { return "Speeduino" }

// IsConnected returns whether the ECU is currently connected and handshook.
func (s *Speeduino) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

// Connect opens the serial port and performs a protocol-specific handshake.
//
// For "generic" mode:
//  1. Open port, wait 1s (per INI delayAfterPortOpen=1000), drain boot garbage
//  2. Send 'n' command, look for 0x6E 0x32 header → success
//  3. If 'n' fails, try 'A' command, look for 0x41 echo → success with A fallback
//
// For "tunerstudio" mode:
//  1. Open port, wait 1s, drain boot garbage
//  2. Send msEnvelope-framed 'Q' command, validate CRC32 response → success
//
// On failure, the port is closed and an error is returned.
// The caller (main.go connectWithRetry) handles retry with backoff.
func (s *Speeduino) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close any existing connection
	if s.port != nil {
		s.port.Close()
		s.port = nil
		s.connected = false
	}

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
	if err := port.SetReadTimeout(readTimeout); err != nil {
		port.Close()
		return fmt.Errorf("speeduino: failed to set timeout: %w", err)
	}
	s.port = port

	protoName := "generic"
	if s.proto == protoTunerStudio {
		protoName = "tunerstudio"
	}
	log.Printf("[speeduino] opened %s at %d baud (protocol=%s)", s.portPath, s.baudRate, protoName)

	// Required post-open delay per Speeduino INI (delayAfterPortOpen=1000)
	time.Sleep(1 * time.Second)

	// Passively drain any boot garbage or unsolicited ECU output
	s.drainSerial("boot")

	switch s.proto {
	case protoGeneric:
		if err := s.connectGeneric(); err != nil {
			s.port.Close()
			s.port = nil
			return err
		}
	case protoTunerStudio:
		if err := s.connectTunerStudio(); err != nil {
			s.port.Close()
			s.port = nil
			return err
		}
	}

	s.connected = true
	log.Printf("[speeduino] connected to %s (protocol=%s)", s.portPath, protoName)
	return nil
}

// connectGeneric handshakes using the plain secondary serial protocol.
// Tries 'n' first (enhanced 119-byte data set), falls back to 'A' (legacy 75-byte).
func (s *Speeduino) connectGeneric() error {
	// --- Try 'n' command ---
	log.Printf("[speeduino] trying 'n' command on %s...", s.portPath)

	s.port.ResetInputBuffer()
	if _, err := s.port.Write([]byte{'n'}); err != nil {
		return fmt.Errorf("speeduino: n write failed: %w", err)
	}

	// Collect response: echo(0x6E) + type(0x32) + length + data
	maxResp := 3 + genericNDataSize
	resp, err := s.readResponse(maxResp, readTimeout)
	if err == nil {
		log.Printf("[speeduino] 'n' response (%d bytes): % X", len(resp), resp)

		// Scan for signature: 0x6E 0x32 <length>
		for i := 0; i+2 < len(resp); i++ {
			if resp[i] == 0x6E && resp[i+1] == 0x32 {
				dataLen := int(resp[i+2])
				log.Printf("[speeduino] 'n' echo at offset %d, data length=%d", i, dataLen)
				s.useNCmd = true
				return nil
			}
		}
		log.Printf("[speeduino] 'n' echo signature (6E 32) not found")
	} else {
		log.Printf("[speeduino] 'n' command failed: %v", err)
	}

	// Drain any leftover from 'n' attempt
	s.drainSerial("post-n")

	// --- Fallback: try 'A' command ---
	log.Printf("[speeduino] trying 'A' command on %s...", s.portPath)

	s.port.ResetInputBuffer()
	if _, err := s.port.Write([]byte{'A'}); err != nil {
		return fmt.Errorf("speeduino: A write failed: %w", err)
	}

	resp, err = s.readResponse(1+genericADataSize+32, readTimeout) // extra margin for garbage
	if err == nil {
		log.Printf("[speeduino] 'A' response (%d bytes): % X", len(resp), resp)

		// Scan for 0x41 echo with enough data following
		for j := 0; j < len(resp); j++ {
			if resp[j] == 0x41 && len(resp)-j >= 1+genericADataSize {
				log.Printf("[speeduino] 'A' echo at offset %d", j)
				s.useNCmd = false
				return nil
			}
		}
		// Accept echo even without full data (ECU may be slow)
		for j := 0; j < len(resp); j++ {
			if resp[j] == 0x41 {
				log.Printf("[speeduino] 'A' echo at offset %d (data may be incomplete)", j)
				s.useNCmd = false
				return nil
			}
		}
	}

	return fmt.Errorf("speeduino: generic handshake failed on %s — no valid 'n' or 'A' response (check secondarySerialProtocol is set to Generic in TunerStudio)", s.portPath)
}

// connectTunerStudio handshakes using the msEnvelope CRC32-framed protocol.
// Sends a framed 'Q' (version query) and validates the CRC32 response.
func (s *Speeduino) connectTunerStudio() error {
	log.Printf("[speeduino] trying msEnvelope 'Q' on %s...", s.portPath)

	s.port.ResetInputBuffer()

	envelope := s.wrapMsEnvelope([]byte{'Q'})
	log.Printf("[speeduino] sending msEnvelope Q (%d bytes): % X", len(envelope), envelope)

	if _, err := s.port.Write(envelope); err != nil {
		return fmt.Errorf("speeduino: Q write failed: %w", err)
	}

	// Read the msEnvelope response
	payload, err := s.readMsEnvelopeResponse()
	if err != nil {
		return fmt.Errorf("speeduino: msEnvelope Q handshake failed: %w", err)
	}

	// Check if payload looks like an ASCII version string
	isASCII := true
	for _, b := range payload {
		if b < 0x20 || b > 0x7E {
			isASCII = false
			break
		}
	}
	if isASCII {
		log.Printf("[speeduino] msEnvelope Q: version = %q", string(payload))
	} else {
		log.Printf("[speeduino] msEnvelope Q: payload (%d bytes) = % X", len(payload), payload)
	}

	return nil
}

// Close cleanly shuts down the serial connection.
func (s *Speeduino) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = false
	if s.port != nil {
		err := s.port.Close()
		s.port = nil
		return err
	}
	return nil
}

// RequestRawData performs serial I/O only: sends the poll command and reads
// the raw response bytes. No parsing is done. This keeps the serial goroutine
// as tight as possible — it's back ready for the next cycle immediately.
func (s *Speeduino) RequestRawData() (*RawData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.port == nil {
		return nil, fmt.Errorf("speeduino: not connected")
	}

	switch s.proto {
	case protoGeneric:
		if s.useNCmd {
			return s.rawGenericN()
		}
		return s.rawGenericA()
	case protoTunerStudio:
		return s.rawTunerStudio()
	default:
		return nil, fmt.Errorf("speeduino: unknown protocol mode")
	}
}

// ParseRawData parses raw bytes into a DataFrame.
// This is CPU-only (no I/O) and safe to call from any goroutine.
func (s *Speeduino) ParseRawData(raw *RawData) *DataFrame {
	switch raw.Tag {
	case "generic-n", "generic-a":
		return s.parseSecondaryData(raw.Data)
	case "tunerstudio":
		return s.parsePrimaryData(raw.Data)
	default:
		return &DataFrame{}
	}
}

// RequestData is a convenience that calls RequestRawData + ParseRawData.
func (s *Speeduino) RequestData() (*DataFrame, error) {
	raw, err := s.RequestRawData()
	if err != nil {
		return nil, err
	}
	return s.ParseRawData(raw), nil
}

// ============================================================================
// Generic Protocol — plain commands, no envelope
// ============================================================================

// rawGenericN sends the 'n' command and reads the raw enhanced data set.
// Serial I/O only — no parsing.
func (s *Speeduino) rawGenericN() (*RawData, error) {
	s.port.ResetInputBuffer()

	if _, err := s.port.Write([]byte{'n'}); err != nil {
		s.connected = false
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	// Response: echo(0x6E) + type(0x32) + length(1 byte) + data bytes
	header := make([]byte, 3)
	if err := s.readExact(header, readTimeout); err != nil {
		s.connected = false
		return nil, fmt.Errorf("speeduino: n-cmd header: %w", err)
	}

	if header[0] != 0x6E {
		return nil, fmt.Errorf("speeduino: n-cmd unexpected echo: got 0x%02X, want 0x6E", header[0])
	}
	if header[1] != 0x32 {
		return nil, fmt.Errorf("speeduino: n-cmd unexpected type: got 0x%02X, want 0x32", header[1])
	}

	dataLen := int(header[2])
	if dataLen == 0 || dataLen > 256 {
		return nil, fmt.Errorf("speeduino: n-cmd invalid length: %d", dataLen)
	}

	data := make([]byte, dataLen)
	if err := s.readExact(data, readTimeout); err != nil {
		s.connected = false
		return nil, fmt.Errorf("speeduino: n-cmd data: %w", err)
	}

	return &RawData{Tag: "generic-n", Data: data}, nil
}

// rawGenericA sends the legacy 'A' command and reads the raw simple data set.
// Serial I/O only — no parsing.
func (s *Speeduino) rawGenericA() (*RawData, error) {
	s.port.ResetInputBuffer()

	if _, err := s.port.Write([]byte{'A'}); err != nil {
		s.connected = false
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	// Response: echo(0x41) + 75 data bytes = 76 total
	respLen := 1 + genericADataSize
	resp := make([]byte, respLen)
	if err := s.readExact(resp, readTimeout); err != nil {
		s.connected = false
		return nil, fmt.Errorf("speeduino: A-cmd: %w", err)
	}

	if resp[0] != 0x41 {
		return nil, fmt.Errorf("speeduino: A-cmd unexpected echo: got 0x%02X, want 0x41", resp[0])
	}

	return &RawData{Tag: "generic-a", Data: resp[1:]}, nil
}

// ============================================================================
// TunerStudio Protocol — msEnvelope CRC32 framed
// ============================================================================

// rawTunerStudio sends an msEnvelope-framed 'r' command and reads the raw OCH data.
// Serial I/O only — no parsing.
func (s *Speeduino) rawTunerStudio() (*RawData, error) {
	s.port.ResetInputBuffer()

	envelope := s.buildMsEnvelopeR(0, ochBlockSize)

	if _, err := s.port.Write(envelope); err != nil {
		s.connected = false
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	// Response is msEnvelope-framed: <size_hi><size_lo><payload><crc32>
	payload, err := s.readMsEnvelopeResponse()
	if err != nil {
		s.connected = false
		return nil, fmt.Errorf("speeduino: %w", err)
	}

	// The payload may include a status byte prefix before the OCH data.
	var data []byte
	switch {
	case len(payload) == ochBlockSize:
		data = payload
	case len(payload) == ochBlockSize+1:
		// Skip the status byte (first byte)
		data = payload[1:]
	case len(payload) > ochBlockSize:
		// Take the last ochBlockSize bytes
		data = payload[len(payload)-ochBlockSize:]
	default:
		return nil, fmt.Errorf("speeduino: unexpected payload size: %d (want %d)", len(payload), ochBlockSize)
	}

	return &RawData{Tag: "tunerstudio", Data: data}, nil
}

// ============================================================================
// msEnvelope framing helpers
// ============================================================================

// buildMsEnvelopeR constructs an msEnvelope-framed 'r' command.
func (s *Speeduino) buildMsEnvelopeR(offset, length uint16) []byte {
	payload := []byte{
		'r',
		s.canID,
		rCommandType,
		byte(offset & 0xFF), byte(offset >> 8),
		byte(length & 0xFF), byte(length >> 8),
	}
	return s.wrapMsEnvelope(payload)
}

// wrapMsEnvelope wraps a payload in the msEnvelope format:
//
//	<size_hi> <size_lo> <payload...> <crc32_4bytes_BE>
func (s *Speeduino) wrapMsEnvelope(payload []byte) []byte {
	payloadLen := uint16(len(payload))
	envelope := make([]byte, 0, 2+len(payload)+4)
	envelope = append(envelope, byte(payloadLen>>8), byte(payloadLen&0xFF))
	envelope = append(envelope, payload...)
	crcVal := crc32.ChecksumIEEE(payload)
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crcVal)
	envelope = append(envelope, crcBytes...)
	return envelope
}

// readMsEnvelopeResponse reads an msEnvelope-framed response:
//
//	<size_hi> <size_lo> <payload...> <crc32_4bytes>
//
// Returns the payload bytes with CRC validated.
func (s *Speeduino) readMsEnvelopeResponse() ([]byte, error) {
	// Step 1: Read the 2-byte size header
	sizeHeader := make([]byte, 2)
	if err := s.readExact(sizeHeader, readTimeout); err != nil {
		return nil, fmt.Errorf("size header: %w", err)
	}
	respPayloadSize := int(binary.BigEndian.Uint16(sizeHeader))
	log.Printf("[speeduino] response envelope: payload size = %d", respPayloadSize)

	if respPayloadSize == 0 || respPayloadSize > 1024 {
		return nil, fmt.Errorf("invalid payload size: %d (raw header: %02X %02X)", respPayloadSize, sizeHeader[0], sizeHeader[1])
	}

	// Step 2: Read payload + 4-byte CRC32
	rest := make([]byte, respPayloadSize+4)
	if err := s.readExact(rest, readTimeout); err != nil {
		return nil, fmt.Errorf("payload+crc: %w", err)
	}

	payload := rest[:respPayloadSize]
	respCRC := binary.BigEndian.Uint32(rest[respPayloadSize:])
	calcCRC := crc32.ChecksumIEEE(payload)

	if respCRC != calcCRC {
		return nil, fmt.Errorf("CRC mismatch: got 0x%08X, want 0x%08X (payload %d bytes)", respCRC, calcCRC, respPayloadSize)
	}

	return payload, nil
}

// ============================================================================
// Serial I/O helpers
// ============================================================================

// drainSerial reads and discards all pending data from the serial port
// until there is silence for drainSilenceMs, or drainTimeout has elapsed.
// This is a passive operation — no bytes are written.
func (s *Speeduino) drainSerial(label string) {
	s.port.ResetInputBuffer()

	// Short timeout for drain reads
	s.port.SetReadTimeout(time.Duration(drainSilenceMs) * time.Millisecond)
	defer s.port.SetReadTimeout(readTimeout)

	totalDrained := 0
	deadline := time.Now().Add(drainTimeout)
	buf := make([]byte, 256)

	for time.Now().Before(deadline) {
		n, _ := s.port.Read(buf)
		if n == 0 {
			break // silence — buffer is clear
		}
		if totalDrained == 0 {
			log.Printf("[speeduino] drain(%s) first bytes: % X", label, buf[:n])
		}
		totalDrained += n
	}
	if totalDrained > 0 {
		log.Printf("[speeduino] drain(%s) cleared %d bytes total", label, totalDrained)
	}
}

// readExact reads exactly len(buf) bytes from the serial port within the deadline.
func (s *Speeduino) readExact(buf []byte, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	got := 0
	for got < len(buf) && time.Now().Before(deadline) {
		n, err := s.port.Read(buf[got:])
		if err != nil && n == 0 {
			return fmt.Errorf("read error after %d/%d bytes: %w", got, len(buf), err)
		}
		got += n
	}
	if got < len(buf) {
		return fmt.Errorf("incomplete: got %d bytes, want %d", got, len(buf))
	}
	return nil
}

// readResponse reads up to maxBytes within timeout, returning whatever was received.
func (s *Speeduino) readResponse(maxBytes int, timeout time.Duration) ([]byte, error) {
	resp := make([]byte, 0, maxBytes)
	deadline := time.Now().Add(timeout)

	for len(resp) < maxBytes && time.Now().Before(deadline) {
		buf := make([]byte, maxBytes-len(resp))
		n, err := s.port.Read(buf)
		if err != nil && n == 0 {
			if len(resp) > 0 {
				return resp, nil // return what we have
			}
			return nil, fmt.Errorf("read failed: %w", err)
		}
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
	}

	if len(resp) == 0 {
		return nil, fmt.Errorf("no response within %v", timeout)
	}
	return resp, nil
}

// ============================================================================
// Parsers
// ============================================================================

// parseSecondaryData decodes the secondary serial data layout into a DataFrame.
// Used by Generic mode ('n' and 'A' commands). Layout per docs/SPEEDUINO_SECONDARY_SERIAL_PROTOCOL.md
func (s *Speeduino) parseSecondaryData(d []byte) *DataFrame {
	f := &DataFrame{}
	n := len(d)

	u8 := func(off int) uint8 {
		if off < n {
			return d[off]
		}
		return 0
	}
	s8 := func(off int) int8 {
		if off < n {
			return int8(d[off])
		}
		return 0
	}
	u16le := func(off int) uint16 {
		if off+1 < n {
			return binary.LittleEndian.Uint16(d[off : off+2])
		}
		return 0
	}
	s16le := func(off int) int16 {
		return int16(u16le(off))
	}

	f.Secl = u8(0)
	f.DFCOOn = u8(1)&(1<<4) != 0

	f.Running = u8(2)&(1<<0) != 0
	f.Cranking = u8(2)&(1<<1) != 0
	f.ASE = u8(2)&(1<<2) != 0
	f.Warmup = u8(2)&(1<<3) != 0

	f.Dwell = float64(u8(3)) * 0.1

	f.MAP = u16le(4)
	f.IAT = float64(u8(6)) - 40
	f.Coolant = float64(u8(7)) - 40

	f.BatCorrection = u8(8)
	f.BatteryVoltage = float64(u8(9)) * 0.1
	f.AFR = float64(u8(10)) * 0.1
	f.EGOCorrection = u8(11)
	f.AirCorrection = u8(12)
	f.WarmupEnrich = u8(13)

	f.RPM = u16le(14)
	f.AccelEnrich = u8(16)
	f.GammaEnrich = uint16(u8(17))
	f.VECurr = u8(18)
	f.VE1 = u8(18)
	f.AFRTarget = float64(u8(19)) * 0.1
	f.PulseWidth1 = float64(u16le(20)) * 0.1

	f.Advance = s8(23)
	f.TPS = float64(u8(24))
	f.LoopsPerSecond = u16le(25)
	f.FreeRAM = u16le(27)
	f.BoostTarget = u8(29)
	f.BoostDuty = u8(30)
	f.Sync = u8(31)&(1<<7) != 0

	f.RPMdot = s16le(32)
	f.FlexPct = u8(34)
	f.FlexFuelCor = u8(35)
	f.FlexIgnCor = s8(36)
	f.IdleLoad = u8(37)
	f.AFR2 = float64(u8(39)) * 0.1
	f.Baro = u8(40)
	f.Errors = u8(74)

	// Enhanced data (bytes 75+, from 'n' command)
	if n > 75 {
		f.PulseWidth2 = float64(u16le(76)) * 0.1
		f.PulseWidth3 = float64(u16le(78)) * 0.1
		f.PulseWidth4 = float64(u16le(80)) * 0.1
		f.FuelLoad = float64(s16le(84))
		f.IgnLoad = float64(s16le(86))
		f.CLIdleTarget = uint16(u8(91)) * 10
		f.MAPdot = int16(s8(92))
		f.VVT1Angle = float64(s8(93))
		f.VVT1Target = float64(u8(94))
		f.VVT1Duty = float64(u8(95))
		f.BaroCorrection = u8(98)
		f.ASECurr = u8(99)
		f.VSS = u16le(100)
		f.Gear = u8(102)
		f.FuelPressure = u8(103)
		f.OilPressure = u8(104)
		f.FanStatus = u8(106)&(1<<3) != 0
		f.VVT2Angle = float64(s8(107))
		f.VVT2Target = float64(u8(108))
		f.VVT2Duty = float64(u8(109))
		f.VE1 = u8(113)
		f.VE2 = u8(114)
		f.Advance1 = s8(115)
		f.Advance2 = s8(116)
		f.SDStatus = u8(118)
	}

	s.computeDerived(f)
	return f
}

// parsePrimaryData decodes the TunerStudio/primary 130-byte OCH block into a DataFrame.
// Layout per speeduino.ini [OutputChannels] section.
func (s *Speeduino) parsePrimaryData(d []byte) *DataFrame {
	f := &DataFrame{}

	f.Secl = d[0]
	f.DFCOOn = d[1]&(1<<4) != 0

	f.Running = d[2]&(1<<0) != 0
	f.Cranking = d[2]&(1<<1) != 0
	f.ASE = d[2]&(1<<2) != 0
	f.Warmup = d[2]&(1<<3) != 0

	f.SyncLoss = d[3]
	f.MAP = binary.LittleEndian.Uint16(d[4:6])
	f.IAT = float64(d[6]) - 40
	f.Coolant = float64(d[7]) - 40

	f.BatCorrection = d[8]
	f.BatteryVoltage = float64(d[9]) * 0.1
	f.AFR = float64(d[10]) * 0.1
	f.EGOCorrection = d[11]
	f.AirCorrection = d[12]
	f.WarmupEnrich = d[13]

	f.RPM = binary.LittleEndian.Uint16(d[14:16])
	f.AccelEnrich = d[16]
	f.GammaEnrich = binary.LittleEndian.Uint16(d[17:19])
	f.VE1 = d[19]
	f.VE2 = d[20]
	f.AFRTarget = float64(d[21]) * 0.1

	f.Advance = int8(d[24])
	f.TPS = float64(d[25]) * 0.5

	f.LoopsPerSecond = binary.LittleEndian.Uint16(d[26:28])
	f.FreeRAM = binary.LittleEndian.Uint16(d[28:30])
	f.BoostTarget = d[30]
	f.BoostDuty = d[31]
	f.Sync = d[32]&(1<<7) != 0

	f.RPMdot = int16(binary.LittleEndian.Uint16(d[33:35]))
	f.FlexPct = d[35]
	f.FlexFuelCor = d[36]
	f.FlexIgnCor = int8(d[37])
	f.IdleLoad = d[38]
	f.AFR2 = float64(d[40]) * 0.1
	f.Baro = d[41]
	f.Errors = d[75]

	f.PulseWidth1 = float64(binary.LittleEndian.Uint16(d[76:78])) * 0.001
	f.PulseWidth2 = float64(binary.LittleEndian.Uint16(d[78:80])) * 0.001
	f.PulseWidth3 = float64(binary.LittleEndian.Uint16(d[80:82])) * 0.001
	f.PulseWidth4 = float64(binary.LittleEndian.Uint16(d[82:84])) * 0.001

	f.FuelLoad = float64(int16(binary.LittleEndian.Uint16(d[86:88])))
	f.IgnLoad = float64(int16(binary.LittleEndian.Uint16(d[88:90])))
	f.Dwell = float64(binary.LittleEndian.Uint16(d[90:92])) * 0.001
	f.CLIdleTarget = uint16(d[92]) * 10
	f.MAPdot = int16(binary.LittleEndian.Uint16(d[93:95]))

	f.VVT1Angle = float64(int16(binary.LittleEndian.Uint16(d[95:97]))) * 0.5
	f.VVT1Target = float64(d[97]) * 0.5
	f.VVT1Duty = float64(d[98]) * 0.5

	f.BaroCorrection = d[101]
	f.VECurr = d[102]
	f.ASECurr = d[103]

	f.VSS = binary.LittleEndian.Uint16(d[104:106])
	f.Gear = d[106]
	f.FuelPressure = d[107]
	f.OilPressure = d[108]
	f.FanStatus = d[110]&(1<<3) != 0

	f.VVT2Angle = float64(int16(binary.LittleEndian.Uint16(d[111:113]))) * 0.5
	f.VVT2Target = float64(d[113]) * 0.5
	f.VVT2Duty = float64(d[114]) * 0.5

	f.Advance1 = int8(d[118])
	f.Advance2 = int8(d[119])
	f.SDStatus = d[120]

	f.EMAP = binary.LittleEndian.Uint16(d[121:123])
	f.FanDuty = float64(d[123]) * 0.5
	f.DwellActual = float64(binary.LittleEndian.Uint16(d[125:127])) * 0.001
	f.KnockCount = d[128]
	f.KnockCor = d[129]

	s.computeDerived(f)
	return f
}

// computeDerived calculates lambda and duty cycle from raw data.
func (s *Speeduino) computeDerived(f *DataFrame) {
	if s.stoich > 0 {
		f.Lambda = f.AFR / s.stoich
	}
	if f.RPM > 0 {
		cycleTimeMs := 60000.0 / float64(f.RPM) * 2
		if cycleTimeMs > 0 {
			f.DutyCycle = (f.PulseWidth1 / cycleTimeMs) * 100
		}
	}
}
