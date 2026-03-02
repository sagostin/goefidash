# Speeduino Secondary Serial IO Interface

> Source: https://wiki.speeduino.com/en/Secondary_Serial_IO_interface

## Overview

The Arduino Mega2560 version of Speeduino supports the use of **Serial3** for supplementary IO.
The STM32F4XX and Teensy3.5/6 versions use **Serial2**.

On a Mega 2560, Serial3 can be found on pins 14 and 15 (not broken out to the IDC connector on 0.3/0.4 boards).

**Connection parameters:** 115200 baud, 8 data bits, no parity, 1 stop bit.

## Protocol Details

The secondary serial port uses a **plain-byte protocol**. Commands are sent as **plain bytes** with no CRC32 wrapping. Responses are also plain bytes without envelope framing.

**Our dashboard connects to the secondary serial port using this protocol.**

## Serial Port Functions

### Retrieve Realtime Data

Three commands are available:

#### Command `A` (Legacy — Simple Data Set)

Send a single byte `0x41` (`'A'`).

**Response:**
1. `0x41` — echo confirming received instruction
2. 75 bytes of realtime data (the "simple" data set)

> The `A` command data set will not be changed or expanded upon and is maintained for legacy devices.

#### Command `n` (Enhanced Data Set — Recommended)

Send a single byte `0x6E` (`'n'`).

**Response:**
1. `0x6E` — echo confirming received instruction  
2. `0x32` — command type byte
3. Length byte — number of data bytes to follow (currently `0x77` = 119 bytes as of 09/07/2021)
4. The realtime data bytes

#### Command `r` (Selective Data Read)

Send 7 bytes total:
1. `0x72` (`'r'`)
2. CAN ID byte (TS canID)
3. `0x30` (r-type command = 48 decimal)
4. Offset low byte (LSB first)
5. Offset high byte
6. Length low byte (LSB first)
7. Length high byte

**Response:**
1. `0x72` — echo confirming received instruction
2. Command type byte (typically `0x30`)
3. The requested data bytes starting at offset for the specified length

## Realtime Data List

The data bytes (returned by `A`, `n`, or `r`) are laid out as follows:

| Byte | Field | Description |
|------|-------|-------------|
| 0 | secl | Seconds counter (increments each second) |
| 1 | status1 | Bitfield: inj1Status(0), inj2Status(1), inj3Status(2), inj4Status(3), DFCOOn(4), boostCutFuel(5), toothLog1Ready(6), toothLog2Ready(7) |
| 2 | engine | Bitfield: running(0), crank(1), ase(2), warmup(3), tpsacden(5), mapaccden(7) |
| 3 | dwell | Dwell in ms × 10 |
| 4–5 | MAP | Manifold pressure (U16 LE, kPa) |
| 6 | IAT | Intake air temp (raw + CALIBRATION_TEMPERATURE_OFFSET) |
| 7 | coolant | Coolant temp (raw + CALIBRATION_TEMPERATURE_OFFSET) |
| 8 | batCorrection | Battery voltage correction (%) |
| 9 | battery10 | Battery voltage (÷ 10 = volts) |
| 10 | O2 | Primary O2 / AFR |
| 11 | egoCorrection | Exhaust gas correction (%) |
| 12 | iatCorrection | Air temperature correction (%) |
| 13 | wueCorrection | Warmup enrichment (%) |
| 14–15 | RPM | Engine RPM (U16 LE) |
| 16 | TAEamount | Acceleration enrichment (%) |
| 17 | corrections | Total GammaE (%) |
| 18 | VE | Current VE 1 (%) |
| 19 | afrTarget | Chosen AFR target |
| 20–21 | PW1 | Pulsewidth 1 (U16 LE, × 10 in ms, convert from µS to mS) |
| 22 | tpsDOT | TPS rate of change |
| 23 | advance | Current spark advance |
| 24 | TPS | TPS (0–100%) |
| 25–26 | loopsPerSecond | Loops per second (U16 LE) |
| 27–28 | freeRAM | Free RAM (U16 LE) |
| 29 | boostTarget | Target boost pressure |
| 30 | boostDuty | Current PWM boost duty cycle |
| 31 | spark | Bitfield: launchHard(0), launchSoft(1), hardLimitOn(2), softLimitOn(3), boostCutSpark(4), error(5), idleControlOn(6), sync(7) |
| 32–33 | rpmDOT | RPM rate of change (S16 LE, signed) |
| 34 | ethanolPct | Flex sensor value |
| 35 | flexCorrection | Flex fuel correction (%) |
| 36 | flexIgnCorrection | Flex ignition correction (deg) |
| 37 | idleLoad | Idle load |
| 38 | testOutputs | Bitfield: testEnabled(0), testActive(1) |
| 39 | O2_2 | Secondary O2 |
| 40 | baro | Barometer value |
| 41–72 | canin[0–15] | CAN input channels (U16 LE each, 16 channels × 2 bytes) |
| 73 | tpsADC | TPS raw (0–255) |
| 74 | errors | Error codes: errorNum(0:1), currentError(2:7) |

**Enhanced data (bytes 75–118, available via `n` and `r` commands):**

| Byte | Field | Description |
|------|-------|-------------|
| 75 | launchCorrection | Launch correction |
| 76–77 | PW2 | Pulsewidth 2 (U16 LE, × 10 ms) |
| 78–79 | PW3 | Pulsewidth 3 (U16 LE, × 10 ms) |
| 80–81 | PW4 | Pulsewidth 4 (U16 LE, × 10 ms) |
| 82 | status3 | Bitfield: resentLockOn(0), nitrousOn(1), fuel2Active(2), vssRefresh(3), halfSync(4), nSquirts(6:7) |
| 83 | engineProtectStatus | RPM(0), MAP(1), OIL(2), AFR(3) |
| 84–85 | fuelLoad | Fuel load (S16 LE) |
| 86–87 | ignLoad | Ignition load (S16 LE) |
| 88–89 | injAngle | Injection angle (U16 LE) |
| 90 | idleDuty | Idle duty |
| 91 | CLIdleTarget | Closed loop idle target |
| 92 | mapDOT | MAP rate of change |
| 93 | vvt1Angle | VVT1 angle (S8) |
| 94 | vvt1TargetAngle | VVT1 target |
| 95 | vvt1Duty | VVT1 duty |
| 96–97 | flexBoostCorrection | Flex boost correction (U16 LE) |
| 98 | baroCorrection | Baro correction |
| 99 | ASEValue | Current ASE (%) |
| 100–101 | vss | Vehicle speed (U16 LE) |
| 102 | gear | Current gear |
| 103 | fuelPressure | Fuel pressure |
| 104 | oilPressure | Oil pressure |
| 105 | wmiPW | WMI pulsewidth |
| 106 | status4 | Bitfield: wmiEmptyBit(0), vvt1Error(1), vvt2Error(2) |
| 107 | vvt2Angle | VVT2 angle (S8) |
| 108 | vvt2TargetAngle | VVT2 target |
| 109 | vvt2Duty | VVT2 duty |
| 110 | outputsStatus | Outputs status |
| 111 | fuelTemp | Fuel temperature (+ offset) |
| 112 | fuelTempCorrection | Fuel temp correction (%) |
| 113 | VE1 | VE 1 (%) |
| 114 | VE2 | VE 2 (%) |
| 115 | advance1 | Advance 1 |
| 116 | advance2 | Advance 2 |
| 117 | nitrous_status | Nitrous status |
| 118 | TS_SD_Status | SD card status |

## Reading External Analog Data from Remote Device

Speeduino can also poll the secondary port for analog sensor data from a remote device.

### Speeduino sends (request):
1. `'R'` (0x52)
2. CAN input channel number
3. CAN address (2 bytes, LSB first) — ignored for direct Serial3 connections

### Remote device responds:
1. `'G'` (0x47)
2. `0x01` — validity flag  
3. CAN input channel number (echo back)
4. 8 bytes of data (sensor value in bytes 0–1 as LSB/MSB)
