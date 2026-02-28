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

// protocolMode indicates which serial protocol variant is in use.
type protocolMode int

const (
	// protoSecondaryN is the plain Secondary Serial IO protocol using the 'n' command.
	protoSecondaryN protocolMode = iota
	// protoSecondaryA is the plain Secondary Serial IO protocol using the legacy 'A' command.
	protoSecondaryA
	// protoSecondaryR is the plain Secondary Serial IO protocol using the 'r' command.
	protoSecondaryR
	// protoPrimary is the msEnvelope CRC32-framed protocol (primary/USB port
	// or secondary port with secondarySerialProtocol="Tuner Studio").
	protoPrimary
)

// Speeduino implements the Provider interface for Speeduino ECUs.
//
// It auto-detects whether the connected port uses the Secondary Serial IO
// protocol (plain A/r commands) or the msEnvelope protocol (CRC32 framed),
// and adapts accordingly.
//
// The Speeduino's secondary serial port can be configured to different
// protocol modes via secondarySerialProtocol in TunerStudio:
//
//	0: Generic (Fixed List) — uses plain A/n/r commands
//	1: Generic (ini File)   — uses plain A/n/r commands
//	2: CAN
//	3: msDroid
//	4: Real Dash
//	5: Tuner Studio          — uses msEnvelope (same as primary port)
//
// Protocol reference: docs/SPEEDUINO_SECONDARY_SERIAL_PROTOCOL.md
type Speeduino struct {
	portPath  string
	baudRate  int
	canID     byte
	port      serial.Port
	mu        sync.Mutex
	stoich    float64      // Stoichiometric ratio for lambda calc
	proto     protocolMode // Detected protocol
	protocol  string       // Configured protocol preference (auto/secondary/msenvelope)
	connected bool         // True only after Connect() successfully handshakes
}

// SpeeduinoConfig holds connection configuration for the Speeduino provider.
type SpeeduinoConfig struct {
	PortPath string  `yaml:"port_path" json:"portPath"`
	BaudRate int     `yaml:"baud_rate" json:"baudRate"`
	CanID    byte    `yaml:"can_id" json:"canId"`
	Stoich   float64 `yaml:"stoich" json:"stoich"`     // e.g. 14.7 for gasoline
	Protocol string  `yaml:"protocol" json:"protocol"` // "auto", "secondary", "msenvelope"
}

const (
	// Primary port (msEnvelope) constants
	primaryOCHBlockSize = 130
	rCommandType        = 0x30

	// Secondary port constants
	secondaryADataSize = 75  // Bytes returned by 'A' command
	secondaryNDataSize = 119 // Bytes returned by 'n' command (current as of firmware 202409)
	secondaryRDataSize = 119 // Request 119 bytes via 'r' for full data set

	// Drain / timing constants
	drainSilenceMs = 100                     // silence threshold for drain loop
	drainTimeout   = 1500 * time.Millisecond // max time to spend draining
	postWriteDelay = 80 * time.Millisecond   // delay after write before read
)

// NewSpeeduino creates a new Speeduino ECU provider.
func NewSpeeduino(cfg SpeeduinoConfig) *Speeduino {
	if cfg.BaudRate == 0 {
		cfg.BaudRate = 115200
	}
	if cfg.Stoich == 0 {
		cfg.Stoich = 14.7
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "auto"
	}
	return &Speeduino{
		portPath: cfg.PortPath,
		baudRate: cfg.BaudRate,
		canID:    cfg.CanID,
		stoich:   cfg.Stoich,
		protocol: cfg.Protocol,
	}
}

func (s *Speeduino) Name() string { return "Speeduino" }

// Connect opens the serial port and auto-detects the protocol.
//
// Detection order (msEnvelope FIRST to avoid corrupting the msEnvelope parser
// with plain command bytes — once plain bytes enter the msEnvelope parser,
// it tries to interpret them as size headers, causing persistent framing errors):
//
//  1. Try msEnvelope 'Q' command (safe — on secondary Generic mode, the 'Q' byte
//     is still handled and the envelope overhead is harmlessly consumed)
//  2. Try msEnvelope 'r' command (full OCH block request)
//  3. Wait for msEnvelope parser timeout (blockReadTimeout=2000ms per INI)
//  4. Try plain 'n' command (secondary port in Generic mode)
//  5. Try plain 'A' command (legacy fallback)
//
// The protocol config option can force a specific mode:
//   - "auto"       — try all protocols in order (default)
//   - "msenvelope" — only try msEnvelope
//   - "secondary"  — only try secondary serial (plain commands)
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
	if err := port.SetReadTimeout(2 * time.Second); err != nil {
		port.Close()
		return fmt.Errorf("speeduino: failed to set timeout: %w", err)
	}
	s.mu.Lock()
	s.port = port
	s.mu.Unlock()

	log.Printf("[speeduino] opened %s at %d baud (protocol=%s)", s.portPath, s.baudRate, s.protocol)

	// Required post-open delay per Speeduino INI (delayAfterPortOpen=1000)
	time.Sleep(1 * time.Second)

	// Aggressively drain any boot garbage or unsolicited ECU output
	s.drainSerial("boot")

	tryMsEnvelope := s.protocol == "auto" || s.protocol == "msenvelope"
	trySecondary := s.protocol == "auto" || s.protocol == "secondary"

	// =========================================================================
	// Phase 1: msEnvelope — try FIRST to avoid corrupting the TS parser
	// =========================================================================
	if tryMsEnvelope {
		// DO NOT send null-byte flushes here — they corrupt the msEnvelope
		// parser. Just drain, then send a clean msEnvelope Q command.

		// --- Try 1a: msEnvelope 'Q' command (version query) ---
		// This is the safest msEnvelope probe: if the ECU is in Generic mode,
		// the secondary parser handles 'Q' (version response) and the envelope
		// overhead bytes (size header, CRC) are consumed as unknown commands.
		// If the ECU is in TS mode, it processes the full msEnvelope Q.
		log.Printf("[speeduino] trying msEnvelope 'Q' command on %s...", s.portPath)
		if err := s.tryMsEnvelopeQ(); err == nil {
			s.proto = protoPrimary
			s.connected = true
			log.Printf("[speeduino] connected to %s at %d baud (msEnvelope protocol via Q)", s.portPath, s.baudRate)
			return nil
		} else {
			log.Printf("[speeduino] msEnvelope 'Q' attempt failed: %v", err)
		}

		// Drain Q response data
		s.drainSerial("post-Q-envelope")

		// --- Try 1b: msEnvelope 'r' command (data request) ---
		log.Printf("[speeduino] trying msEnvelope 'r' command on %s...", s.portPath)
		if err := s.tryPrimaryHandshake(); err == nil {
			s.proto = protoPrimary
			s.connected = true
			log.Printf("[speeduino] connected to %s at %d baud (msEnvelope protocol)", s.portPath, s.baudRate)
			return nil
		} else {
			log.Printf("[speeduino] msEnvelope 'r' attempt failed: %v", err)
		}

		// Drain and wait for the msEnvelope parser to time out and reset.
		// Per INI: blockReadTimeout=2000ms. We wait a bit longer to be safe.
		s.drainSerial("post-r-envelope")
		if trySecondary {
			log.Printf("[speeduino] waiting for msEnvelope parser timeout before trying secondary...")
			time.Sleep(2500 * time.Millisecond)
			s.drainSerial("post-timeout")
		}
	}

	// =========================================================================
	// Phase 2: Secondary serial (plain commands)
	// =========================================================================
	if trySecondary {
		// Flush the ECU command buffer with null bytes — this is safe for
		// the secondary parser (each 0x00 is handled as unknown: break).
		s.flushECUCommandBuffer()

		// --- Diagnostic: send plain 'Q' (version query) ---
		s.probeVersion()

		// --- Try 2a: secondary serial 'n' command ---
		s.flushECUCommandBuffer()
		log.Printf("[speeduino] trying secondary serial 'n' command on %s...", s.portPath)
		if err := s.trySecondaryHandshakeN(); err == nil {
			s.proto = protoSecondaryN
			s.connected = true
			log.Printf("[speeduino] connected to %s at %d baud (secondary serial 'n' protocol)", s.portPath, s.baudRate)
			return nil
		} else {
			log.Printf("[speeduino] secondary serial 'n' attempt failed: %v", err)
		}

		s.drainSerial("post-n-cmd")

		// --- Try 2b: secondary serial plain 'A' command (legacy fallback) ---
		s.flushECUCommandBuffer()
		log.Printf("[speeduino] trying secondary serial 'A' command on %s...", s.portPath)
		if err := s.trySecondaryHandshakeA(); err == nil {
			s.proto = protoSecondaryA
			s.connected = true
			log.Printf("[speeduino] connected to %s at %d baud (secondary serial 'A' protocol)", s.portPath, s.baudRate)
			return nil
		} else {
			log.Printf("[speeduino] secondary serial 'A' attempt failed: %v", err)
		}
	}

	s.mu.Lock()
	s.port.Close()
	s.port = nil
	s.mu.Unlock()
	return fmt.Errorf("speeduino: no valid protocol detected on %s (tried: protocol=%s) — check secondarySerialProtocol setting in TunerStudio and wiring", s.portPath, s.protocol)
}

// drainSerial reads and discards all pending data from the serial port
// until there is silence (no data) for drainSilenceMs, or drainTimeout
// has elapsed. This handles unsolicited ECU output, stale buffers, and
// streaming protocol modes (RealDash, msDroid).
func (s *Speeduino) drainSerial(label string) {
	s.port.ResetInputBuffer()

	// Short timeout for drain reads
	s.port.SetReadTimeout(time.Duration(drainSilenceMs) * time.Millisecond)
	defer s.port.SetReadTimeout(2 * time.Second)

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

// flushECUCommandBuffer sends null bytes (0x00) to the ECU to push through
// any stale bytes stuck in the secondary serial command parser. The firmware's
// secondserial_Command() treats 0x00 as an unknown command (default: break),
// so these are harmlessly consumed one per main loop iteration.
// After sending, we drain any resulting output.
func (s *Speeduino) flushECUCommandBuffer() {
	// Send 20 null bytes — enough to flush through any partial command state
	flush := make([]byte, 20)
	s.port.Write(flush)

	// Wait for ECU to process all null bytes (each consumed once per main loop, ~1ms each)
	time.Sleep(200 * time.Millisecond)

	// Drain any output generated while processing
	s.drainSerial("flush")
}

// probeVersion sends 'Q' and 'S' commands and logs whatever comes back.
// This is diagnostic-only — it helps determine whether the ECU is reachable
// at all before trying protocol-specific handshakes.
//
// 'Q' returns a 20-byte ASCII version string on the primary port (e.g.
// "speeduino 202409-dev") and is also handled on the secondary port.
// 'S' is handled similarly. If we get ASCII back, we know the ECU is alive.
func (s *Speeduino) probeVersion() {
	for _, cmd := range []struct {
		name string
		b    byte
	}{
		{"Q", 'Q'},
		{"S", 'S'},
	} {
		s.port.ResetInputBuffer()
		if _, err := s.port.Write([]byte{cmd.b}); err != nil {
			log.Printf("[speeduino] probe '%s' write failed: %v", cmd.name, err)
			continue
		}

		time.Sleep(postWriteDelay)

		buf := make([]byte, 128)
		resp := make([]byte, 0, 128)
		deadline := time.Now().Add(1 * time.Second)
		for len(resp) < 128 && time.Now().Before(deadline) {
			n, _ := s.port.Read(buf)
			if n == 0 {
				break
			}
			resp = append(resp, buf[:n]...)
		}

		if len(resp) > 0 {
			// Check if response looks like ASCII text
			isASCII := true
			for _, b := range resp {
				if b < 0x20 || b > 0x7E {
					isASCII = false
					break
				}
			}
			if isASCII {
				log.Printf("[speeduino] probe '%s' response (%d bytes, ASCII): %s", cmd.name, len(resp), string(resp))
			} else {
				log.Printf("[speeduino] probe '%s' response (%d bytes, binary): % X", cmd.name, len(resp), resp)
			}
		} else {
			log.Printf("[speeduino] probe '%s' response: no data (ECU may not be reachable)", cmd.name)
		}
	}

	// Drain any remaining probe data
	s.drainSerial("post-probe")
}

// trySecondaryHandshakeN sends the 'n' command and looks for the distinctive
// 3-byte header: echo(0x6E) + type(0x32) + length byte.
// This is the most robust detection method since the header signature is
// very unlikely to appear in random data.
func (s *Speeduino) trySecondaryHandshakeN() error {
	if _, err := s.port.Write([]byte{'n'}); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	time.Sleep(postWriteDelay)

	// Collect response bytes (echo + type + length + up to 119 data bytes)
	maxResp := 3 + secondaryNDataSize
	resp := make([]byte, 0, maxResp)
	deadline := time.Now().Add(2 * time.Second)

	for len(resp) < maxResp && time.Now().Before(deadline) {
		buf := make([]byte, maxResp-len(resp))
		n, err := s.port.Read(buf)
		if err != nil && n == 0 {
			break
		}
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
	}

	log.Printf("[speeduino] 'n' command raw response (%d bytes): % X", len(resp), resp)

	// Scan for the signature: 0x6E 0x32 <length>
	for i := 0; i+2 < len(resp); i++ {
		if resp[i] == 0x6E && resp[i+1] == 0x32 {
			dataLen := int(resp[i+2])
			log.Printf("[speeduino] 'n' echo found at offset %d, data length=%d", i, dataLen)

			// Verify we got enough data bytes after the header
			headerEnd := i + 3
			available := len(resp) - headerEnd
			if available < dataLen {
				log.Printf("[speeduino] 'n' incomplete data: have %d, want %d (may still work)", available, dataLen)
			}
			return nil
		}
	}

	return fmt.Errorf("'n' echo signature (6E 32) not found in %d bytes", len(resp))
}

// trySecondaryHandshakeA sends a plain 'A' command and scans the response
// for the 0x41 echo byte, tolerating any leading garbage from unsolicited
// ECU output or previous protocol probes.
func (s *Speeduino) trySecondaryHandshakeA() error {
	if _, err := s.port.Write([]byte{'A'}); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	time.Sleep(postWriteDelay)

	// Read a generous buffer — 'A' returns 1 (echo) + 75 data = 76 bytes,
	// but there may be leading garbage.
	maxResp := 256
	resp := make([]byte, 0, maxResp)
	deadline := time.Now().Add(2 * time.Second)

	for len(resp) < maxResp && time.Now().Before(deadline) {
		buf := make([]byte, maxResp-len(resp))
		n, err := s.port.Read(buf)
		if err != nil && n == 0 {
			break
		}
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
		// Once we have at least 76 bytes after an 'A', we can stop
		for j := 0; j < len(resp); j++ {
			if resp[j] == 0x41 && len(resp)-j >= 1+secondaryADataSize {
				log.Printf("[speeduino] 'A' echo found at offset %d (total %d bytes)", j, len(resp))
				return nil
			}
		}
	}

	log.Printf("[speeduino] 'A' command raw response (%d bytes): % X", len(resp), resp)

	// Final scan
	for j := 0; j < len(resp); j++ {
		if resp[j] == 0x41 {
			log.Printf("[speeduino] 'A' echo found at offset %d (data may be incomplete)", j)
			return nil
		}
	}

	return fmt.Errorf("'A' echo (0x41) not found in %d bytes of response", len(resp))
}

// tryPrimaryHandshake sends an msEnvelope-framed 'r' command and validates the response.
func (s *Speeduino) tryPrimaryHandshake() error {
	envelope := s.buildMsEnvelope(0, 4)
	log.Printf("[speeduino] sending msEnvelope (%d bytes): % X", len(envelope), envelope)

	if _, err := s.port.Write(envelope); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	time.Sleep(postWriteDelay)

	payload, err := s.readMsEnvelopeResponse()
	if err != nil {
		return err
	}

	log.Printf("[speeduino] msEnvelope handshake OK (payload %d bytes): % X", len(payload), payload)
	return nil
}

// tryMsEnvelopeQ sends an msEnvelope-framed 'Q' command and checks for a valid response.
// This is the safest msEnvelope probe because:
//   - If the ECU is in TS mode: it processes the full msEnvelope Q and responds in kind
//   - If the ECU is in Generic mode: the 'Q' byte inside the envelope triggers a plain
//     version response; the envelope overhead is consumed as unknown commands (harmless)
//
// We check for both an msEnvelope-framed response (TS mode) and fall back to checking
// for a plain ASCII version string (Generic mode responding to the unwrapped Q).
func (s *Speeduino) tryMsEnvelopeQ() error {
	s.port.ResetInputBuffer()

	envelope := s.buildMsEnvelopeCmd('Q')
	log.Printf("[speeduino] sending msEnvelope Q (%d bytes): % X", len(envelope), envelope)

	if _, err := s.port.Write(envelope); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	time.Sleep(postWriteDelay)

	// Read whatever comes back — could be msEnvelope-framed or plain ASCII
	maxResp := 256
	resp := make([]byte, 0, maxResp)
	deadline := time.Now().Add(2 * time.Second)

	for len(resp) < maxResp && time.Now().Before(deadline) {
		buf := make([]byte, maxResp-len(resp))
		n, err := s.port.Read(buf)
		if err != nil && n == 0 {
			break
		}
		if n > 0 {
			resp = append(resp, buf[:n]...)
		}
		// If we have a reasonable amount of data, break early
		if len(resp) > 20 {
			// Short sleep to catch any trailing bytes
			time.Sleep(50 * time.Millisecond)
			buf2 := make([]byte, 128)
			n2, _ := s.port.Read(buf2)
			if n2 > 0 {
				resp = append(resp, buf2[:n2]...)
			}
			break
		}
	}

	log.Printf("[speeduino] msEnvelope Q response (%d bytes): % X", len(resp), resp)

	if len(resp) < 2 {
		return fmt.Errorf("no response to msEnvelope Q (%d bytes)", len(resp))
	}

	// Check 1: Valid msEnvelope response? (size + payload + CRC32)
	respPayloadSize := int(binary.BigEndian.Uint16(resp[:2]))
	if respPayloadSize > 0 && respPayloadSize < 128 && len(resp) >= 2+respPayloadSize+4 {
		payload := resp[2 : 2+respPayloadSize]
		respCRC := binary.BigEndian.Uint32(resp[2+respPayloadSize : 2+respPayloadSize+4])
		calcCRC := crc32.ChecksumIEEE(payload)

		if respCRC == calcCRC {
			// Check if payload looks like a version string (ASCII)
			isASCII := true
			for _, b := range payload {
				if b < 0x20 || b > 0x7E {
					isASCII = false
					break
				}
			}
			if isASCII {
				log.Printf("[speeduino] msEnvelope Q: valid CRC, version = %q", string(payload))
			} else {
				log.Printf("[speeduino] msEnvelope Q: valid CRC, payload = % X", payload)
			}
			return nil
		}
		log.Printf("[speeduino] msEnvelope Q: CRC mismatch (got=0x%08X calc=0x%08X)", respCRC, calcCRC)
	}

	// Check 2: Plain ASCII version string? (ECU in Generic mode responded to bare Q)
	// The envelope bytes before Q are consumed as unknown commands, so the response
	// is a plain version string like "speeduino 202409-dev"
	for i := 0; i < len(resp); i++ {
		if resp[i] >= 0x20 && resp[i] <= 0x7E {
			// Found printable ASCII — check if there's a contiguous run
			end := i
			for end < len(resp) && resp[end] >= 0x20 && resp[end] <= 0x7E {
				end++
			}
			asciiStr := string(resp[i:end])
			if len(asciiStr) >= 5 {
				log.Printf("[speeduino] msEnvelope Q: got plain ASCII response %q — ECU appears to be in Generic (secondary) mode, not msEnvelope", asciiStr)
				// This means the ECU is NOT in TS mode — it's in Generic mode.
				// Return error so Connect() falls through to secondary probes.
				return fmt.Errorf("ECU responded with plain ASCII (%q) — not in msEnvelope mode", asciiStr)
			}
		}
	}

	return fmt.Errorf("msEnvelope Q: unrecognized response (%d bytes)", len(resp))
}

// buildMsEnvelope constructs an msEnvelope-framed 'r' command.
func (s *Speeduino) buildMsEnvelope(offset, length uint16) []byte {
	payload := []byte{
		'r',
		s.canID,
		rCommandType,
		byte(offset & 0xFF), byte(offset >> 8),
		byte(length & 0xFF), byte(length >> 8),
	}
	return s.wrapMsEnvelope(payload)
}

// buildMsEnvelopeCmd constructs an msEnvelope-framed single-byte command.
// Used for commands like 'Q' (version query) that don't need extra parameters.
func (s *Speeduino) buildMsEnvelopeCmd(cmd byte) []byte {
	return s.wrapMsEnvelope([]byte{cmd})
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
	if err := s.readExact(sizeHeader, 2*time.Second); err != nil {
		return nil, fmt.Errorf("size header: %w", err)
	}
	respPayloadSize := int(binary.BigEndian.Uint16(sizeHeader))
	log.Printf("[speeduino] response envelope: payload size = %d", respPayloadSize)

	if respPayloadSize == 0 || respPayloadSize > 1024 {
		return nil, fmt.Errorf("invalid payload size: %d", respPayloadSize)
	}

	// Step 2: Read payload + 4-byte CRC32
	rest := make([]byte, respPayloadSize+4)
	if err := s.readExact(rest, 2*time.Second); err != nil {
		return nil, fmt.Errorf("payload+crc: %w", err)
	}

	payload := rest[:respPayloadSize]
	respCRC := binary.BigEndian.Uint32(rest[respPayloadSize:])
	calcCRC := crc32.ChecksumIEEE(payload)

	log.Printf("[speeduino] response CRC: got=0x%08X calc=0x%08X", respCRC, calcCRC)

	if respCRC != calcCRC {
		return nil, fmt.Errorf("CRC mismatch: got 0x%08X, want 0x%08X", respCRC, calcCRC)
	}

	return payload, nil
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
		log.Printf("[speeduino] readExact: got %d/%d bytes: % X", got, len(buf), buf[:got])
		return fmt.Errorf("incomplete: got %d bytes, want %d", got, len(buf))
	}
	return nil
}

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

// RequestData sends a data request and parses the response.
func (s *Speeduino) RequestData() (*DataFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected || s.port == nil {
		return nil, fmt.Errorf("speeduino: not connected")
	}

	switch s.proto {
	case protoSecondaryN:
		return s.requestSecondaryN()
	case protoSecondaryA:
		return s.requestSecondaryA()
	case protoSecondaryR:
		return s.requestSecondary()
	case protoPrimary:
		return s.requestPrimary()
	default:
		return nil, fmt.Errorf("speeduino: unknown protocol mode")
	}
}

// ============================================================================
// Secondary Serial Protocol — plain commands, no envelope
// ============================================================================

// requestSecondaryN sends the 'n' command and parses the enhanced data set.
func (s *Speeduino) requestSecondaryN() (*DataFrame, error) {
	if s.port == nil {
		return nil, fmt.Errorf("speeduino: not connected")
	}
	s.port.ResetInputBuffer()

	if _, err := s.port.Write([]byte{'n'}); err != nil {
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	// Response: echo(0x6E) + type(0x32) + length(1 byte) + data bytes
	// Read header first: 3 bytes
	header := make([]byte, 3)
	if err := s.readExact(header, 2*time.Second); err != nil {
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
	if err := s.readExact(data, 2*time.Second); err != nil {
		return nil, fmt.Errorf("speeduino: n-cmd data: %w", err)
	}

	return s.parseSecondaryData(data), nil
}

// requestSecondaryA sends the legacy 'A' command and parses the simple data set.
func (s *Speeduino) requestSecondaryA() (*DataFrame, error) {
	s.port.ResetInputBuffer()

	if _, err := s.port.Write([]byte{'A'}); err != nil {
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	// Response: echo(0x41) + 75 data bytes = 76 total
	respLen := 1 + secondaryADataSize
	resp := make([]byte, respLen)
	if err := s.readExact(resp, 2*time.Second); err != nil {
		return nil, fmt.Errorf("speeduino: A-cmd: %w", err)
	}

	if resp[0] != 0x41 {
		return nil, fmt.Errorf("speeduino: A-cmd unexpected echo: got 0x%02X, want 0x41", resp[0])
	}

	data := resp[1:]
	return s.parseSecondaryData(data), nil
}

// requestSecondary sends a plain 'r' command on the secondary serial port.
func (s *Speeduino) requestSecondary() (*DataFrame, error) {
	s.port.ResetInputBuffer()

	offset := uint16(0)
	length := uint16(secondaryRDataSize)

	cmd := []byte{
		'r',
		s.canID,
		rCommandType,
		byte(offset & 0xFF), byte(offset >> 8),
		byte(length & 0xFF), byte(length >> 8),
	}

	if _, err := s.port.Write(cmd); err != nil {
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	// Response: echo('r') + type(0x30) + data bytes
	respLen := 2 + int(length)
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

	if resp[0] != 'r' {
		return nil, fmt.Errorf("speeduino: unexpected echo: got 0x%02X, want 0x72", resp[0])
	}
	if resp[1] != rCommandType {
		return nil, fmt.Errorf("speeduino: unexpected r-type: got 0x%02X, want 0x%02X", resp[1], rCommandType)
	}

	data := resp[2:]
	return s.parseSecondaryData(data), nil
}

// ============================================================================
// Primary msEnvelope Protocol — CRC32 framed
// ============================================================================

// requestPrimary sends an msEnvelope-framed 'r' command and reads the enveloped response.
func (s *Speeduino) requestPrimary() (*DataFrame, error) {
	s.port.ResetInputBuffer()

	envelope := s.buildMsEnvelope(0, primaryOCHBlockSize)

	if _, err := s.port.Write(envelope); err != nil {
		return nil, fmt.Errorf("speeduino: write failed: %w", err)
	}

	time.Sleep(postWriteDelay)

	// Response is msEnvelope-framed: <size_hi><size_lo><payload><crc32>
	payload, err := s.readMsEnvelopeResponse()
	if err != nil {
		return nil, fmt.Errorf("speeduino: %w", err)
	}

	// The payload may include a status byte prefix before the OCH data.
	// If payload is exactly primaryOCHBlockSize, it's pure data.
	// If payload is primaryOCHBlockSize+1, first byte is status.
	var data []byte
	switch {
	case len(payload) == primaryOCHBlockSize:
		data = payload
	case len(payload) == primaryOCHBlockSize+1:
		// Skip the status byte (first byte)
		data = payload[1:]
	case len(payload) > primaryOCHBlockSize:
		// Take the last primaryOCHBlockSize bytes
		data = payload[len(payload)-primaryOCHBlockSize:]
	default:
		return nil, fmt.Errorf("speeduino: unexpected payload size: %d (want %d)", len(payload), primaryOCHBlockSize)
	}

	return s.parsePrimaryData(data), nil
}

// ============================================================================
// Parsers
// ============================================================================

// parseSecondaryData decodes the secondary serial data layout into a DataFrame.
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

// parsePrimaryData decodes the primary msEnvelope 130-byte OCH block into a DataFrame.
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
