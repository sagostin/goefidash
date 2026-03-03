# Speeduino Secondary Serial Port Protocol

Technical reference for the Speeduino ECU's secondary serial port, derived from firmware source (v2025.04-dev).

> 📖 Firmware source files used: [`comms_secondary.h`](../tmp-reference/comms_secondary.h), [`comms_secondary.cpp`](../tmp-reference/comms_secondary.cpp), [`comms.cpp`](../tmp-reference/comms.cpp)

---

## Serial Configuration

| Parameter | Value |
|-----------|-------|
| **Baud Rate** | 115200 |
| **Data Bits** | 8 |
| **Parity** | None |
| **Stop Bits** | 1 |
| **Hardware Port** | `Serial3` (Mega2560 pins 14/15) or `Serial2` (STM32/Teensy) |

---

## Protocol Modes

The firmware supports six protocol modes for the secondary serial port, configured via `configPage9.secondarySerialProtocol`:

| Value | Macro | Description |
|-------|-------|-------------|
| 0 | `SECONDARY_SERIAL_PROTO_GENERIC_FIXED` | Generic with legacy fixed byte order |
| 1 | `SECONDARY_SERIAL_PROTO_GENERIC_INI` | Generic using INI-defined field order |
| 2 | `SECONDARY_SERIAL_PROTO_CAN` | CAN bus interface mode |
| 3 | `SECONDARY_SERIAL_PROTO_MSDROID` | MSDroid compatibility (remaps `S` → `Q`) |
| 4 | `SECONDARY_SERIAL_PROTO_REALDASH` | RealDash compatibility |
| 5 | `SECONDARY_SERIAL_PROTO_TUNERSTUDIO` | Full TunerStudio — diverts primary serial to secondary port |

> [!IMPORTANT]
> This dashboard uses **Generic INI** mode (value `1`). The secondary port processes commands natively as plain single-byte commands — no `msEnvelope` framing (size header + CRC32) is required. The `msEnvelope` protocol is **only** used when mode is set to `TUNERSTUDIO` (5), which redirects primary serial handling to the secondary port.

---

## TunerStudio Mode (Protocol 5)

When `secondarySerialProtocol` is set to `TUNERSTUDIO` (5), the secondary serial port behaves **identically** to the primary USB port. The firmware achieves this by swapping the serial output pointer before calling the primary serial handler.

### How It Works (Pointer Swap)

From `comms_secondary.cpp`:

```cpp
void secondserial_Command(void)
{
  if(configPage9.secondarySerialProtocol == SECONDARY_SERIAL_PROTO_TUNERSTUDIO)
  {
    pPrimarySerial = pSecondarySerial;  // Redirect output to secondary port
    serialReceive();                     // Run normal primary serial handler
    if(serialStatusFlag == SERIAL_INACTIVE) { pPrimarySerial = &Serial; }  // Reset
    return;
  }
  // ... generic command handling for modes 0-4
}
```

The `pPrimarySerial` pointer is temporarily swapped to point at the secondary serial interface. This means `serialReceive()` reads from and writes to the secondary port, using the exact same msEnvelope-framed protocol that TunerStudio uses on the primary USB port. After the command completes, the pointer is reset to `&Serial` so the primary port continues working.

### msEnvelope Framing

In TunerStudio mode, **all commands** must be wrapped in an msEnvelope frame:

```
┌──────────────┬──────────────────┬──────────────┐
│ Length (2B)   │ Payload (N bytes)│ CRC32 (4B)   │
│ big-endian   │ command + args   │ over payload │
└──────────────┴──────────────────┴──────────────┘
```

- **Length**: 2 bytes, big-endian — the byte count of the payload only
- **Payload**: The command byte + any arguments
- **CRC32**: 4 bytes — CRC32 checksum of the payload bytes

Responses also use framing:
```
┌──────────────┬──────────────────┬──────────────┐
│ Length (2B)   │ Response (N B)   │ CRC32 (4B)   │
│ big-endian   │ RC_OK + data     │ over response│
└──────────────┴──────────────────┴──────────────┘
```

### Handshake Sequence

TunerStudio performs the following handshake to establish a connection:

#### Step 1: `F` — Protocol Version (Legacy, No Framing)

The `F` command is **always allowed** without msEnvelope framing. It's the initial probe that determines which protocol the ECU speaks.

```
Client → ECU:  0x46                 ('F', single byte, no envelope)
ECU → Client:  [length 2B] [0x00 '0' '0' '2'] [CRC32 4B]
```

Response payload = `{SERIAL_RC_OK, '0', '0', '2'}` → Serial protocol version **002**.

> [!NOTE]
> The `F` command is special — it bypasses the msEnvelope requirement. The firmware detects it by peeking at the first byte. If the first byte is `'F'`, it routes directly to the legacy command handler. This allows TunerStudio to discover the ECU before knowing the protocol version.

#### Step 2: `Q` — Firmware Signature (msEnvelope Required)

Once the client knows it's protocol 002, all subsequent commands use msEnvelope framing.

```
Client → ECU:  [0x00 0x01] [0x51] [CRC32]     (length=1, payload='Q')
ECU → Client:  [length 2B] [0x00 'speeduino 202504-dev'] [CRC32 4B]
```

Response: `SERIAL_RC_OK` + firmware signature string (21 bytes total payload).

The signature is used by TunerStudio to match the ECU to the correct INI file.

#### Step 3: `C` — Communications Test

Verifies the ECU is alive and responsive.

```
Client → ECU:  [0x00 0x01] [0x43] [CRC32]     (length=1, payload='C')
ECU → Client:  [length 2B] [0x00 0xFF] [CRC32 4B]
```

Response: `{SERIAL_RC_OK, 0xFF}` (2 bytes payload).

#### Step 4: `f` — Serial Capabilities

Queries the ECU's serial capabilities and blocking factors.

```
Client → ECU:  [0x00 0x01] [0x66] [CRC32]     (length=1, payload='f')
ECU → Client:  [length 2B] [0x00 0x02 BF_H BF_L TBF_H TBF_L] [CRC32 4B]
```

Response (6 bytes payload):
| Byte | Value | Description |
|------|-------|-------------|
| 0 | `0x00` | SERIAL_RC_OK |
| 1 | `0x02` | Serial protocol version (2) |
| 2–3 | big-endian | BLOCKING_FACTOR (max payload per write) |
| 4–5 | big-endian | TABLE_BLOCKING_FACTOR (max table payload) |

#### Step 5: `A` or `r` — Realtime Data Polling

Once connected, TunerStudio polls realtime data using either:

- **`A`** — Returns `LOG_ENTRY_SIZE` (138) bytes of INI-ordered output channels
- **`r`** with sub-command `0x30` — Returns a specific offset/length from the output channels

For the `A` command in TunerStudio mode:
```
Client → ECU:  [0x00 0x01] [0x41] [CRC32]
ECU → Client:  [length 2B] [0x00 + 138 data bytes] [CRC32 4B]
```

> [!IMPORTANT]
> In TunerStudio mode, the `A` command returns **`LOG_ENTRY_SIZE` (138 bytes)** using the INI output channel order — not the 75-byte legacy set used by the generic secondary commands. The data is prefixed with `SERIAL_RC_OK` (`0x00`).

For the `r` command (selective read):
```
Client → ECU:  [0x00 0x07] [0x72 0x00 0x30 offset_L offset_H len_L len_H] [CRC32]
ECU → Client:  [length 2B] [0x00 + N data bytes] [CRC32 4B]
```

### Additional TunerStudio Commands

| Command | Description |
|---------|-------------|
| `I` | Returns CAN ID: `{SERIAL_RC_OK, 0x00}` |
| `E` | Command button handler — executes TS-defined actions |
| `H` / `h` | Start / stop tooth logger |
| `J` / `j` | Start / stop composite logger |
| `O` / `o` | Start / stop tertiary composite logger |
| `X` / `x` | Start / stop cam composite logger |
| `k` | Returns CRC32 of a calibration page |

### Return Codes

All TunerStudio responses begin with a return code byte:

| Code | Constant | Meaning |
|------|----------|---------|
| `0x00` | `SERIAL_RC_OK` | Success |
| `0x01` | `SERIAL_RC_REALTIME` | Unused |
| `0x02` | `SERIAL_RC_PAGE` | Unused |
| `0x04` | `SERIAL_RC_BURN_OK` | EEPROM write succeeded |
| `0x80` | `SERIAL_RC_TIMEOUT` | Receive timeout (400ms) |
| `0x82` | `SERIAL_RC_CRC_ERR` | CRC32 mismatch — ECU will flush RX buffer |
| `0x83` | `SERIAL_RC_UKWN_ERR` | Unknown command |
| `0x84` | `SERIAL_RC_RANGE_ERR` | Out of range — TS will **not** retry |
| `0x85` | `SERIAL_RC_BUSY_ERR` | ECU busy — TS will wait and retry |

### Legacy Command Lockout

Once the ECU successfully processes a **CRC-validated msEnvelope command**, it sets `currentStatus.allowLegacyComms = false`. This locks out all unframed legacy commands (except `F`) until the next power cycle. This prevents accidental corruption from mixed protocols.

### DTR Reset Byte

Windows sends a `0xF0` byte on initial serial connection (DTR toggle). The firmware explicitly ignores this byte to prevent it from being interpreted as a command. See [Speeduino issue #1112](https://github.com/speeduino/speeduino/issues/1112).

### TunerStudio Mode Handshake Flow

```
Client                                              ECU (Secondary Port, Mode 5)
  │                                                    │
  │──── 'F' (no framing) ─────────────────────────────>│
  │<──── [len][RC_OK '002'][CRC] ──────────────────────│  Protocol version = 002
  │                                                    │
  │──── [len]['Q'][CRC] ──────────────────────────────>│
  │<──── [len][RC_OK 'speeduino 202504-dev'][CRC] ────│  Firmware signature
  │                                                    │
  │──── [len]['C'][CRC] ──────────────────────────────>│
  │<──── [len][RC_OK 0xFF][CRC] ──────────────────────│  Comms test OK
  │                                                    │
  │──── [len]['f'][CRC] ──────────────────────────────>│
  │<──── [len][RC_OK 0x02 BF TBF][CRC] ──────────────│  Capabilities
  │                                                    │
  │  ┌─── Polling Loop ───────────────────────────┐    │
  │  │ [len]['A'][CRC]  or  [len]['r'...][CRC]    │───>│
  │  │<── [len][RC_OK + data][CRC] ───────────────│<───│
  │  └────────────────────────────────────────────┘    │
```

---

## Command Set (Generic Modes 0–4)

All commands on the secondary port (modes 0–4) are **single-byte ASCII** commands sent without framing. The ECU responds immediately.

### Data Commands

#### `A` — Legacy Realtime Data (75 bytes)

Retrieves the first 75 bytes of realtime engine data.

- **Request**: Send `0x41` (`'A'`)
- **Response**:
  1. Echo byte `0x41`
  2. 75 bytes of realtime data

The data layout depends on the protocol mode:
- **GENERIC_FIXED (0)**: Uses the legacy fixed byte order via `getLegacySecondarySerialLogEntry()`
- **All others**: Uses the INI-defined output channel order

---

#### `n` — Enhanced Realtime Data (123 bytes)

Retrieves the full 123-byte enhanced realtime data set. **This is the recommended command.**

- **Request**: Send `0x6E` (`'n'`)
- **Response**:
  1. Echo byte `0x6E`
  2. Type byte `0x32`
  3. Length byte `0x7B` (123)
  4. 123 bytes of realtime data

> [!CAUTION]
> The Speeduino wiki documents this as 119 bytes. The actual firmware defines `NEW_CAN_PACKET_SIZE = 123` in `comms_secondary.h`. Always read the length byte from the response header to stay compatible.

---

#### `r` — Selective Data Read (Variable)

Fetches specific data fields by offset and length.

- **Request**: Send `0x72` (`'r'`) + 6 bytes:
  - TS CAN ID (1 byte)
  - Command type `0x30` (1 byte)
  - Offset (2 bytes, LSB first)
  - Length (2 bytes, LSB first)
- **Response**:
  1. Echo byte `0x72`
  2. Type echo `0x30`
  3. Requested data bytes

> [!NOTE]
> On the **secondary port** in generic mode, the `r` command is plain serial (7 bytes total). In TunerStudio mode (5), the same command must be wrapped in msEnvelope framing.

---

### Management Commands

| Command | Description |
|---------|-------------|
| `Q` | Returns firmware version string (e.g. `speeduino 202504-dev`) |
| `S` | Returns firmware version. In MSDroid mode, remapped to `Q` for compatibility |
| `s` | Returns the "a" stream code version (`Speeduino csx02019.8`) |
| `b` | Burns a single EEPROM page |
| `B` | Same as `b` but forces compatibility mode (slower burn rate) |
| `d` | Returns CRC32 hash of a given page |
| `M` | Writes data to a page (page ID + offset + length + data) |
| `p` | Reads page values (page ID + offset + length) |
| `k` | Placeholder for new CAN interface commands |
| `Z` | Reserved for development use |

---

### CAN Bus Commands

#### `G` — CAN Input Response

Handles incoming sensor data from a remote CAN device. The ECU reads this when a remote device responds to a previous request.

- **Payload**: 9 bytes
  - Success flag (1 byte): `0` = fail, `1` = success
  - Destination CAN input channel (1 byte)
  - 8 bytes of CAN data (if success)
- **Result**: Stores the value in `currentStatus.canin[channel]` as a 16-bit value (high byte << 8 | low byte)

#### `R` — Request Remote Sensor Data

Sent **by the ECU** to request analog data from a remote device over Serial3.

- **Payload**: `'R'` + CAN input channel (1 byte) + source CAN address (2 bytes, LSB first)

#### `L` — Listen for CAN Message

Sent **by the ECU** to instruct the CAN interface to listen for a specific CAN address.

- **Payload**: `'L'` + 11-bit CAN address (2 bytes)

---

## Realtime Data Layout (123 bytes, `n` command)

The 123-byte enhanced data set when using GENERIC_FIXED mode. Multi-byte values are little-endian (low byte first).

| Offset | Size | Field | Description |
|--------|------|-------|-------------|
| 0 | 1 | `secl` | Seconds counter |
| 1 | 1 | `status1` | Injector status + DFCOOn + boostCutFuel |
| 2 | 1 | `engine` | Engine status bits (running, crank, ase, warmup) |
| 3 | 1 | `dwell` | Dwell in ms × 100 (divide by 100 for ms) |
| 4–5 | 2 | `MAP` | Manifold Absolute Pressure (kPa) |
| 6 | 1 | `IAT` | Intake Air Temperature (°C + offset) |
| 7 | 1 | `CLT` | Coolant Temperature (°C + offset) |
| 8 | 1 | `batCorrection` | Battery voltage correction (%) |
| 9 | 1 | `battery10` | Battery voltage × 10 |
| 10 | 1 | `O2` | Primary O2/AFR × 10 |
| 11 | 1 | `egoCorrection` | EGO correction (%) |
| 12 | 1 | `iatCorrection` | IAT correction (%) |
| 13 | 1 | `wueCorrection` | Warm-up enrichment correction (%) |
| 14–15 | 2 | `RPM` | Engine RPM |
| 16 | 1 | `TAEamount` | Transient accel enrichment (%) |
| 17 | 1 | `corrections` | Total GammaE (%) |
| 18 | 1 | `VE` | Current VE 1 (%) |
| 19 | 1 | `afrTarget` | Current AFR target |
| 20–21 | 2 | `PW1` | Pulse width 1 (raw µS) |
| 22 | 1 | `tpsDOT` | TPS rate-of-change (÷ 10) |
| 23 | 1 | `advance` | Current spark advance (°) |
| 24 | 1 | `TPS` | Throttle position (0–100%) |
| 25–26 | 2 | `loopsPerSecond` | Main loop frequency |
| 27–28 | 2 | `freeRAM` | Free RAM (bytes) |
| 29 | 1 | `boostTarget` | Boost target (÷ 2) |
| 30 | 1 | `boostDuty` | Boost duty (÷ 100) |
| 31 | 1 | `spark` | Status2 bitfield (launch, sync, etc.) |
| 32–33 | 2 | `rpmDOT` | RPM rate-of-change (signed) |
| 34 | 1 | `ethanolPct` | Flex fuel ethanol % |
| 35 | 1 | `flexCorrection` | Flex fuel correction (%) |
| 36 | 1 | `flexIgnCorrection` | Flex ignition correction (°) |
| 37 | 1 | `idleLoad` | Current idle load |
| 38 | 1 | `testOutputs` | Test mode flags |
| 39 | 1 | `O2_2` | Secondary O2 sensor |
| 40 | 1 | `baro` | Barometric pressure (kPa) |
| 41–72 | 32 | `canin[0–15]` | 16 CAN input channels (2 bytes each) |
| 73 | 1 | `tpsADC` | Raw TPS ADC value (0–255) |
| 74 | 1 | `error` | Error codes: errorNum (bits 0–1), currentError (bits 2–7) |
| 75 | 1 | `launchCorrection` | Launch correction |
| 76–77 | 2 | `PW2` | Pulse width 2 (raw µS) |
| 78–79 | 2 | `PW3` | Pulse width 3 (raw µS) |
| 80–81 | 2 | `PW4` | Pulse width 4 (raw µS) |
| 82 | 1 | `status3` | Nitrous, fuel2Active, vssRefresh, etc. |
| 83 | 1 | `engineProtectStatus` | Protection flags: RPM, MAP, OIL, AFR |
| 84–85 | 2 | `fuelLoad` | Fuel load (kPa) |
| 86–87 | 2 | `ignLoad` | Ignition load (kPa) |
| 88–89 | 2 | `injAngle` | Injection angle (°) |
| 90 | 1 | `idleLoad` | Current idle load (duplicate) |
| 91 | 1 | `CLIdleTarget` | Closed-loop idle target RPM |
| 92 | 1 | `mapDOT` | MAP rate-of-change (÷ 10) |
| 93 | 1 | `vvt1Angle` | VVT 1 current angle (signed) |
| 94 | 1 | `vvt1TargetAngle` | VVT 1 target angle |
| 95 | 1 | `vvt1Duty` | VVT 1 PWM duty |
| 96–97 | 2 | `flexBoostCorrection` | Flex boost correction |
| 98 | 1 | `baroCorrection` | Barometric correction (%) |
| 99 | 1 | `ASEValue` | Afterstart enrichment (%) |
| 100–101 | 2 | `vss` | Vehicle speed |
| 102 | 1 | `gear` | Current gear |
| 103 | 1 | `fuelPressure` | Fuel pressure |
| 104 | 1 | `oilPressure` | Oil pressure |
| 105 | 1 | `wmiPW` | Water-methanol injection pulse width |
| 106 | 1 | `status4` | WMI empty, VVT errors, fan, burn pending, staging |
| 107 | 1 | `vvt2Angle` | VVT 2 current angle (signed) |
| 108 | 1 | `vvt2TargetAngle` | VVT 2 target angle |
| 109 | 1 | `vvt2Duty` | VVT 2 PWM duty |
| 110 | 1 | `outputsStatus` | Output pin status |
| 111 | 1 | `fuelTemp` | Fuel temperature (+40° offset) |
| 112 | 1 | `fuelTempCorrection` | Fuel temp correction (%) |
| 113 | 1 | `VE1` | VE table 1 (%) |
| 114 | 1 | `VE2` | VE table 2 (%) |
| 115 | 1 | `advance1` | Advance table 1 (°) |
| 116 | 1 | `advance2` | Advance table 2 (°) |
| 117 | 1 | `nitrous_status` | Nitrous system status |
| 118 | 1 | `TS_SD_Status` | SD card log status |
| 119–120 | 2 | `EMAP` | Exhaust manifold pressure |
| 121 | 1 | `fanDuty` | Radiator fan PWM duty |
| 122 | 1 | `airConStatus` | A/C system status bitfield |

---

## Scaling Reference

Key fields require scaling to convert raw bytes to usable values:

| Field | Raw Unit | Conversion |
|-------|----------|------------|
| `dwell` (offset 3) | div100(µS) | `raw × 0.01` → ms |
| `battery10` (offset 9) | V × 10 | `raw × 0.1` → volts |
| `O2` / `O2_2` (10, 39) | AFR × 10 | `raw × 0.1` → AFR |
| `PW1–PW4` (20, 76, 78, 80) | µS | `raw × 0.001` → ms |
| `tpsDOT` (offset 22) | val / 10 | Already pre-divided by firmware |
| `MAP` (offset 4–5) | kPa | Direct (little-endian uint16) |
| `RPM` (offset 14–15) | RPM | Direct (little-endian uint16) |
| `boostTarget` (offset 29) | kPa / 2 | `raw × 2` → kPa |
| `boostDuty` (offset 30) | % / 100 | `raw × 100` → % |
| `IAT` / `CLT` (6, 7) | °C + offset | Subtract CALIBRATION_TEMPERATURE_OFFSET (40) |
| `fuelTemp` (offset 111) | °C + 40 | `raw - 40` → °C |

---

## Protocol Flow Examples

### Generic Mode: Polling Realtime Data (`n` command)

```
Dashboard → ECU:  0x6E              (1 byte: 'n')
ECU → Dashboard:  0x6E              (echo)
                  0x32              (type = secondary data)
                  0x7B              (length = 123)
                  [123 data bytes]  (realtime payload)
```

Total response: **126 bytes** (3 header + 123 data)

### Generic Mode: Polling Legacy Data (`A` command)

```
Dashboard → ECU:  0x41              (1 byte: 'A')
ECU → Dashboard:  0x41              (echo)
                  [75 data bytes]   (legacy payload)
```

Total response: **76 bytes** (1 echo + 75 data)

### Generic Mode: Selective Read (`r` command)

```
Dashboard → ECU:  0x72              ('r')
                  0x00              (CAN ID)
                  0x30              (output channels command)
                  0x0E 0x00         (offset 14 = RPM)
                  0x02 0x00         (length 2)
ECU → Dashboard:  0x72              (echo)
                  0x30              (type echo)
                  [2 data bytes]    (RPM low/high)
```

### TunerStudio Mode: `Q` Handshake

```
Dashboard → ECU:  0x00 0x01         (length = 1)
                  0x51              (payload = 'Q')
                  [CRC32 4B]        (CRC of payload)
ECU → Dashboard:  0x00 0x15         (length = 21)
                  0x00              (SERIAL_RC_OK)
                  'speeduino 202504-dev'  (20 chars)
                  [CRC32 4B]        (CRC of response)
```

### TunerStudio Mode: Realtime Polling (`r` command)

```
Dashboard → ECU:  0x00 0x07         (length = 7)
                  0x72 0x00 0x30    ('r', CAN ID, cmd 0x30)
                  0x00 0x00         (offset 0)
                  0x8A 0x00         (length 138)
                  [CRC32 4B]
ECU → Dashboard:  0x00 0x8B         (length = 139)
                  0x00              (SERIAL_RC_OK)
                  [138 data bytes]  (full output channels)
                  [CRC32 4B]
```

---

## Key Implementation Notes

1. **No framing on secondary port (generic)**: In modes 0–4, commands are bare single-byte ASCII. No size header, no CRC32 footer. This is fundamentally different from TunerStudio mode (5).

2. **TunerStudio mode is a full redirect**: Mode 5 doesn't add new commands — it swaps the serial pointer and calls `serialReceive()` directly. The secondary port becomes a second TunerStudio port with identical capabilities, framing, and command set.

3. **Mode affects byte order**: When protocol is GENERIC_FIXED (0), data uses the legacy fixed byte order via `getLegacySecondarySerialLogEntry()`. When GENERIC_INI (1), data follows the INI file's output channel definition order.

4. **Dynamic length detection**: Always read the length byte from the `n` command response header rather than hardcoding 123. The firmware could change this value in future versions.

5. **Non-blocking I/O**: The firmware uses non-blocking serial writes with a per-byte status flag system (`serialSecondaryStatusFlag`). Expect data to arrive incrementally, not as a single burst.

6. **Legacy lockout in TS mode**: After the first successful CRC-validated command, the ECU disables legacy (unframed) commands until the next power cycle. The `F` command is exempt from this lockout.

7. **Timeout**: The ECU enforces a 400ms receive timeout. If a command isn't fully received within this window, the ECU sends `SERIAL_RC_TIMEOUT` and flushes the RX buffer.

8. **DTR byte**: Windows sends `0xF0` on connection — the firmware silently discards it.
