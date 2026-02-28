# TunerStudio msEnvelope Serial Protocol

> Source: [EFI Analytics ECU Definition Files](EFI_Analytics_ECU_Definition_files.pdf) + Speeduino `speeduino.ini`

## Overview

The **msEnvelope** protocol is the standard serial framing used by TunerStudio (EFI Analytics) to communicate with ECU firmware. It wraps every command and response in a size + CRC32 frame to ensure data integrity.

Speeduino uses `messageEnvelopeFormat = msEnvelope_1.0` on its **primary serial port** (USB/Serial0) and optionally on the **secondary serial port** when `secondarySerialProtocol` is set to "Tuner Studio" (mode 5).

## Frame Format

Every command and response is wrapped in the same envelope:

```
┌──────────┬──────────────────┬──────────────┐
│ Size (2B)│   Payload (NB)   │  CRC32 (4B)  │
└──────────┴──────────────────┴──────────────┘
```

| Field | Size | Byte Order | Description |
|-------|------|------------|-------------|
| **Size** | 2 bytes | Big Endian | Number of bytes in the Payload only |
| **Payload** | N bytes | — | The command or response data |
| **CRC32** | 4 bytes | Big Endian | CRC-32 IEEE 802.3 of the Payload bytes only |

**Total frame size = 2 + N + 4 = N + 6 bytes**

### CRC-32 Specification

- **Polynomial**: IEEE 802.3 (same as Ethernet, PKZIP)
- **Go**: `crc32.ChecksumIEEE(payload)`
- **Polynomial value**: `0xEDB88320` (reversed) / `0x04C11DB7` (normal)
- CRC is computed over the **payload bytes only** (not the size header)

## Commands (Host → ECU)

All commands are sent as msEnvelope-wrapped payloads. The payload format depends on the command type.

### `Q` — Query/Identify (Handshake)

Returns the ECU's signature string to confirm identity.

**Payload**: `Q` (1 byte = `0x51`)

**Response payload**: ASCII signature string, e.g. `speeduino 202501`

```
# Request frame:
00 01  51  <crc32 of [0x51]>
size=1  Q   CRC32

# Response frame:
00 10  73 70 65 65 64 75 69 6E 6F 20 32 30 32 35 30 31  <crc32>
size=16  "speeduino 202501"                                CRC32
```

> From speeduino.ini: `queryCommand = "Q"`, `signature = "speeduino 202501"`

### `S` — Version Info

Returns a human-readable version string for display.

**Payload**: `S` (1 byte = `0x53`)

**Response payload**: ASCII version/title string

> From speeduino.ini: `versionInfo = "S"`

### `r` — Read Data (Output Channels / Config Pages)

Reads data from the ECU. This is the primary command for retrieving realtime data.

**Payload** (7 bytes):

| Offset | Size | Field | Description |
|--------|------|-------|-------------|
| 0 | 1 | Command | `0x72` (`'r'`) |
| 1 | 1 | CAN ID | TunerStudio CAN target ID (`$tsCanId`, usually `0x00`) |
| 2 | 1 | Type | Page/table type (`0x30` = 48 for output channels) |
| 3–4 | 2 | Offset | Start position in data block (U16 **Little Endian**) |
| 5–6 | 2 | Length | Number of bytes to read (U16 **Little Endian**) |

**Response payload**: `<data bytes>` — the requested data starting at offset for length bytes

```
# Request: read 130 bytes of output channels starting at offset 0
00 07  72 00 30 00 00 82 00  <crc32>
size=7  r  id type off_lo off_hi len_lo len_hi  CRC32
              0x30  0      0      130    0
```

> From speeduino.ini: `ochGetCommand = "r\$tsCanId\x30%2o%2c"` where `%2o` = 2-byte offset (LE), `%2c` = 2-byte count (LE)
>
> `ochBlockSize = 130` — the full output channel data block is 130 bytes

### `w` — Write Data (Config Pages)

Writes configuration data to the ECU (not used by our dashboard — read-only).

**Payload**: `w` + canID + page + offset(2B LE) + length(2B LE) + data

### `B` — Burn to EEPROM

Saves current configuration page to permanent storage.

**Payload**: `B` + page_number(2B)

> From speeduino.ini: `burnCommand = "B%2i"` where `%2i` = 2-byte page index

## Response Format

ECU responses use the same msEnvelope framing:

```
<size_hi> <size_lo> <payload...> <crc32_4bytes_BE>
```

### Response Status Bytes

Some commands (notably `r`) may prefix the response payload with a status byte:

| Status | Meaning |
|--------|---------|
| `0x00` | Success (some implementations) |
| `0x80` | Command acknowledged / busy |
| Other | Error or unsupported command |

When the response payload size is `ochBlockSize + 1`, the first byte is a status byte and the remaining bytes are the actual data.

## INI Configuration Variables

The INI file uses escape sequences and variables in command strings:

| Token | Meaning |
|-------|---------|
| `\$tsCanId` | Substituted with the configured CAN target ID byte |
| `\x30` | Literal hex byte `0x30` (48 decimal) |
| `%2o` | 2-byte offset, Little Endian |
| `%2c` | 2-byte count/length, Little Endian |
| `%2i` | 2-byte page index |

## Key INI Sections

### `[MegaTune]`

```ini
queryCommand   = "Q"           ; Handshake command
signature      = "speeduino 202501"  ; Expected response to Q
versionInfo    = "S"           ; Human-readable version command
```

### `[TunerStudio]`

```ini
messageEnvelopeFormat = msEnvelope_1.0  ; Enables CRC32 framing
delayAfterPortOpen = 1000              ; Wait 1s after opening port
blockReadTimeout   = 2000              ; 2s timeout for reads
blockingFactor     = 251               ; Max payload per transfer (Mega)
```

### `[OutputChannels]`

```ini
ochGetCommand  = "r\$tsCanId\x30%2o%2c"  ; Read output channels
ochBlockSize   = 130                     ; Full realtime data = 130 bytes
```

## Timing Requirements

| Parameter | Value | Source |
|-----------|-------|--------|
| Post-open delay | 1000 ms | `delayAfterPortOpen` |
| Read timeout | 2000 ms | `blockReadTimeout` |
| Max payload (Mega) | 251 bytes | `blockingFactor` |
| Max payload (STM32) | 121 bytes | `blockingFactor` (STM32 override) |

## Implementation Notes

### Our Dashboard (`speeduino.go`)

1. **Handshake**: Send msEnvelope `Q`, expect back an msEnvelope response containing `"speeduino 202501"`
2. **Data request**: Send msEnvelope `r` with canID=0, type=0x30, offset=0, length=130
3. **Response handling**: Parse the 130-byte OCH block (or 131 if status byte is prepended)
4. **CRC validation**: Verify CRC-32 IEEE 802.3 on every response

### Protocol Detection

The dashboard auto-detects the protocol by trying msEnvelope first (to avoid corrupting the TS parser with plain bytes), then falling back to secondary serial plain commands.

### Debugging Tips

- If the ECU returns `0x80` as a 1-byte payload, it likely received a valid frame but couldn't process the command (wrong page type, unsupported command, etc.)
- A repeating byte pattern regardless of input suggests the ECU is in a streaming mode (msDroid or RealDash) — change `secondarySerialProtocol` in TunerStudio
- CRC mismatches usually indicate framing alignment issues — ensure the serial buffer is fully drained before sending commands
