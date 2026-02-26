package ecu

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// DemoProvider generates simulated ECU data for development and testing.
type DemoProvider struct {
	mu      sync.Mutex
	running bool
	t       float64 // virtual time accumulator
	stoich  float64
}

func NewDemoProvider() *DemoProvider {
	return &DemoProvider{stoich: 14.7}
}

func (d *DemoProvider) Name() string   { return "Demo (Simulated)" }
func (d *DemoProvider) Connect() error { d.running = true; return nil }
func (d *DemoProvider) Close() error   { d.running = false; return nil }

func (d *DemoProvider) RequestData() (*DataFrame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.t += 0.05 // ~20Hz tick

	// Simulate RPM cycling between idle and revving
	rpmBase := 850.0 + 4000.0*math.Sin(d.t*0.3)*math.Sin(d.t*0.3)
	rpm := uint16(rpmBase + rand.Float64()*50)

	mapVal := uint16(30 + (float64(rpm)-850)/(8000-850)*170) // 30-200 kPa
	tps := (float64(rpm) - 850) / (8000 - 850) * 100
	if tps < 0 {
		tps = 0
	}
	if tps > 100 {
		tps = 100
	}

	advance := int8(10 + (tps/100)*28)
	coolant := 85.0 + rand.Float64()*5
	iat := 30.0 + rand.Float64()*8

	afr := 14.7 - (tps/100)*1.5 + rand.Float64()*0.4
	if afr < 10 {
		afr = 10
	}
	if afr > 18 {
		afr = 18
	}
	battery := 13.8 + rand.Float64()*0.4

	pw1 := 2.0 + tps/100*10
	ve := uint8(40 + tps/100*55)

	dutyCycle := 0.0
	if rpm > 0 {
		cycleMs := 60000.0 / float64(rpm) * 2
		if cycleMs > 0 {
			dutyCycle = (pw1 / cycleMs) * 100
		}
	}
	if dutyCycle > 100 {
		dutyCycle = 100
	}

	speed := uint16(tps / 100 * 220)

	gear := uint8(0) // N
	switch {
	case speed > 180:
		gear = 6
	case speed > 140:
		gear = 5
	case speed > 100:
		gear = 4
	case speed > 60:
		gear = 3
	case speed > 30:
		gear = 2
	case speed > 5:
		gear = 1
	}

	// Generate realistic sensor values
	oilPressure := uint8(15 + tps/100*45) // 15-60 PSI
	if rpm < 500 {
		oilPressure = uint8(float64(rpm) / 500 * 15) // Drop at low RPM
	}

	f := &DataFrame{
		// Core engine
		Secl:     uint8(time.Now().Unix() % 256),
		RPM:      rpm,
		MAP:      mapVal,
		TPS:      tps,
		AFR:      afr,
		Lambda:   afr / d.stoich,
		Advance:  advance,
		Advance1: advance,
		Advance2: advance - 2,

		// Temperatures
		Coolant: coolant,
		IAT:     iat,

		// Fuel
		PulseWidth1: pw1,
		PulseWidth2: pw1,
		PulseWidth3: pw1 * 0.95,
		PulseWidth4: pw1 * 0.95,
		VE1:         ve,
		VE2:         ve - 5,
		VECurr:      ve,
		AFRTarget:   14.7,
		DutyCycle:   dutyCycle,

		// Corrections
		GammaEnrich:    uint16(95 + rand.Float64()*10),
		EGOCorrection:  uint8(95 + rand.Float64()*10),
		AirCorrection:  uint8(98 + rand.Float64()*4),
		WarmupEnrich:   100,
		BatCorrection:  uint8(100 + rand.Float64()*5),
		ASECurr:        0,
		BaroCorrection: 100,
		AccelEnrich:    uint8(rand.Float64() * 5),

		// Electrical
		BatteryVoltage: battery,
		Dwell:          3.5,
		DwellActual:    3.4,

		// Boost
		BoostTarget: uint8(mapVal / 2),
		BoostDuty:   uint8(tps / 100 * 80),

		// Speed / transmission
		VSS:  speed,
		Gear: gear,

		// Pressures
		FuelPressure: 43,
		OilPressure:  oilPressure,
		Baro:         101,

		// VVT
		VVT1Angle:  float64(int16(tps/100*40)) * 0.5,
		VVT1Target: tps / 100 * 20,
		VVT1Duty:   tps / 100 * 80,
		VVT2Angle:  float64(int16(tps/100*30)) * 0.5,
		VVT2Target: tps / 100 * 15,
		VVT2Duty:   tps / 100 * 60,

		// Flex fuel
		FlexPct:     uint8(0),
		FlexFuelCor: 100,
		FlexIgnCor:  0,

		// Exhaust
		AFR2: afr + 0.2,
		EMAP: uint16(100 + tps/100*50),

		// Idle
		IdleLoad:     uint8(25 + rand.Float64()*5),
		CLIdleTarget: 850,

		// Knock
		KnockCount: 0,
		KnockCor:   0,

		// Status
		Running:   true,
		Cranking:  false,
		ASE:       false,
		Warmup:    false,
		DFCOOn:    tps < 1 && rpm > 2000,
		Sync:      true,
		FanStatus: false,

		// Load
		FuelLoad: float64(mapVal),
		IgnLoad:  float64(mapVal),
		MAPdot:   int16(rand.Float64()*20 - 10),
		RPMdot:   int16((float64(rpm) - rpmBase) * 2),

		// Misc
		LoopsPerSecond: 5000 + uint16(rand.Float64()*200),
		FreeRAM:        4096 + uint16(rand.Float64()*512),
		FanDuty:        0,
		SDStatus:       0,
		Errors:         0,
		SyncLoss:       0,
	}

	// Fan control simulation
	if coolant > 90 {
		f.FanStatus = true
		f.FanDuty = float64(coolant-85) / 20 * 100
		if f.FanDuty > 100 {
			f.FanDuty = 100
		}
	}

	// WUE simulation when cold
	if coolant < 60 {
		f.Warmup = true
		f.WarmupEnrich = uint8(100 + (60-coolant)*1.5)
	}

	// ASE simulation after startup
	if d.t < 30 {
		f.ASE = true
		f.ASECurr = uint8(120 - d.t*2)
	}

	// Simulate occasional knock at high load/high RPM
	if tps > 85 && rpm > 5000 && rand.Float64() < 0.08 {
		f.KnockCount = uint8(1 + rand.Float64()*3)
		f.KnockCor = uint8(2 + rand.Float64()*4)
	}

	// Simulate occasional high IAT under boost
	if mapVal > 150 {
		f.IAT = 55 + rand.Float64()*15
	}

	return f, nil
}
