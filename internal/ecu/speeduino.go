package ecu

import (
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Speeduino implements the Provider interface for Speeduino ECUs
// using the generic secondary serial protocol ('n' and 'A' commands).
//
// Modeled after the ArduGauge reference implementation:
//   - Write command byte → read header → read data payload
//   - No handshake or negotiation required
//   - Auto-detects 'n' (enhanced 123-byte) vs 'A' (legacy 75-byte)
type Speeduino struct {
	cfg SpeeduinoConfig

	mu        sync.Mutex
	port      serial.Port
	connected bool

	// Protocol state
	useEnhanced  bool // true = 'n' (123 bytes), false = 'A' (75 bytes)
	modeDetected bool // true after first successful poll determines n-vs-A
}

// Serial timeout — matches ArduGauge's approach of a simple per-read deadline.
const serialTimeout = 2 * time.Second

// NewSpeeduino creates a new Speeduino ECU provider.
func NewSpeeduino(cfg SpeeduinoConfig) *Speeduino {
	if cfg.Stoich <= 0 {
		cfg.Stoich = 14.7
	}
	if cfg.BaudRate <= 0 {
		cfg.BaudRate = 115200
	}
	return &Speeduino{cfg: cfg}
}

// Connect opens the serial port. No handshake needed — the ECU responds
// to 'A' or 'n' commands immediately after the port is open.
func (s *Speeduino) Connect() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected {
		return nil
	}

	if s.port != nil {
		s.connected = true
		return nil
	}

	port, err := serial.Open(s.cfg.PortPath, &serial.Mode{
		BaudRate: s.cfg.BaudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return fmt.Errorf("serial open %s: %w", s.cfg.PortPath, err)
	}

	if err := port.SetReadTimeout(serialTimeout); err != nil {
		port.Close()
		return fmt.Errorf("set read timeout: %w", err)
	}

	s.port = port
	s.connected = true
	log.Printf("[ecu] connected to %s @ %d baud", s.cfg.PortPath, s.cfg.BaudRate)
	return nil
}

// Close shuts down the serial port.
func (s *Speeduino) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false
	s.modeDetected = false
	if s.port != nil {
		err := s.port.Close()
		s.port = nil
		return err
	}
	return nil
}

// IsConnected returns whether the ECU serial port is open.
func (s *Speeduino) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

// ═══════════════════════════════════════════════════════════════════════
// Serial I/O — synchronous, matching ArduGauge pattern
// ═══════════════════════════════════════════════════════════════════════

// readExact reads exactly n bytes from the serial port.
func (s *Speeduino) readExact(n int) ([]byte, error) {
	buf := make([]byte, n)
	total := 0
	for total < n {
		read, err := s.port.Read(buf[total:])
		if err != nil {
			return buf[:total], fmt.Errorf("serial read: %w", err)
		}
		if read == 0 {
			return buf[:total], fmt.Errorf("timeout: got %d/%d bytes", total, n)
		}
		total += read
	}

	if s.cfg.SerialDebug {
		log.Printf("[serial-rx] %d bytes:\n%s", n, hex.Dump(buf))
	}
	return buf, nil
}

// ═══════════════════════════════════════════════════════════════════════
// Generic Protocol (unframed secondary serial)
// ═══════════════════════════════════════════════════════════════════════

// RequestRawData sends the poll command and reads the raw response.
// On the first call, it auto-detects 'n' vs 'A' support.
func (s *Speeduino) RequestRawData() (*RawData, error) {
	s.mu.Lock()
	if !s.connected || s.port == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	s.mu.Unlock()

	if !s.modeDetected {
		return s.detectAndRequest()
	}
	if s.useEnhanced {
		return s.requestN()
	}
	return s.requestA()
}

// detectAndRequest tries 'n' first; if it fails, falls back to 'A'.
func (s *Speeduino) detectAndRequest() (*RawData, error) {
	raw, err := s.requestN()
	if err == nil {
		s.useEnhanced = true
		s.modeDetected = true
		log.Printf("[ecu] using enhanced mode ('n', %d bytes)", len(raw.Payload))
		return raw, nil
	}

	log.Printf("[ecu] 'n' failed (%v), trying 'A'", err)
	s.port.ResetInputBuffer() // flush stale response bytes before retry

	raw, err = s.requestA()
	if err != nil {
		return nil, fmt.Errorf("ECU not responding: %w", err)
	}

	s.useEnhanced = false
	s.modeDetected = true
	log.Printf("[ecu] using legacy mode ('A', %d bytes)", len(raw.Payload))
	return raw, nil
}

// requestN sends the 'n' command and reads the enhanced response.
//
// Protocol (from firmware comms_legacy.cpp sendValues):
//
//	TX: 'n'
//	RX: echo('n') + type(0x32) + length(1 byte) + data(length bytes)
func (s *Speeduino) requestN() (*RawData, error) {
	// Flush any stale bytes before sending command
	s.port.ResetInputBuffer()

	if _, err := s.port.Write([]byte{'n'}); err != nil {
		return nil, fmt.Errorf("write 'n': %w", err)
	}

	// Read header: echo + type + length
	header, err := s.readExact(3)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}

	if header[0] != 'n' {
		return nil, fmt.Errorf("bad echo: 0x%02X", header[0])
	}

	dataLen := int(header[2])
	if dataLen < 1 || dataLen > 256 {
		return nil, fmt.Errorf("bad length: %d", dataLen)
	}

	payload, err := s.readExact(dataLen)
	if err != nil {
		return nil, fmt.Errorf("payload: %w", err)
	}

	return &RawData{
		Payload:  payload,
		Command:  'n',
		Protocol: ProtoGenericFixed,
	}, nil
}

// requestA sends the 'A' command and reads the legacy response.
//
// Protocol (from firmware comms_legacy.cpp sendValues):
//
//	TX: 'A'
//	RX: echo('A') + data(75 bytes)
func (s *Speeduino) requestA() (*RawData, error) {
	// Flush any stale bytes before sending command
	s.port.ResetInputBuffer()

	if _, err := s.port.Write([]byte{'A'}); err != nil {
		return nil, fmt.Errorf("write 'A': %w", err)
	}

	// Read echo
	echo, err := s.readExact(1)
	if err != nil {
		return nil, fmt.Errorf("echo: %w", err)
	}

	if echo[0] != 'A' {
		return nil, fmt.Errorf("bad echo: 0x%02X", echo[0])
	}

	payload, err := s.readExact(CANPacketSize)
	if err != nil {
		return nil, fmt.Errorf("payload: %w", err)
	}

	return &RawData{
		Payload:  payload,
		Command:  'A',
		Protocol: ProtoGenericFixed,
	}, nil
}

// ParseRawData converts raw serial bytes into a structured DataFrame.
func (s *Speeduino) ParseRawData(raw *RawData) *DataFrame {
	if raw == nil || len(raw.Payload) == 0 {
		return nil
	}
	return parseGenericFixed(raw.Payload, s.cfg.Stoich)
}

// Ensure Speeduino implements Provider at compile time.
var _ Provider = (*Speeduino)(nil)

// Ensure io.Closer is covered.
var _ io.Closer = (*Speeduino)(nil)
