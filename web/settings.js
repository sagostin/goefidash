// ================================================================
// SPEEDUINO DASH — Settings Page Logic
// ================================================================

(function () {
    'use strict';

    const D = window.SpeeduinoDash;
    const $ = (id) => document.getElementById(id);

    // ---- Live Preview ----
    D.onFrame = function (frame) {
        if (frame.ecu) {
            $('previewRPM').textContent = frame.ecu.rpm;
            const speedKph = frame.speed ? frame.speed.value : 0;
            $('previewSpeed').textContent = Math.round(D.convertSpeed(speedKph));
            const gear = D.detectGear(frame.ecu.rpm, speedKph);
            $('previewGear').textContent = gear === null ? (frame.ecu.gear || 'N') : (gear === 0 ? 'N' : gear);
            const hp = D.calcEstimatedHP(speedKph, frame.stamp || Date.now());
            $('previewHP').textContent = Math.round(hp);
        }
    };

    // ---- Load Config ----
    function loadConfig() {
        fetch('/api/config')
            .then(r => r.json())
            .then(cfg => {
                // Layout
                $('cfgLayout').value = cfg.display?.layout || 'classic';

                // Units
                $('cfgTempUnit').value = cfg.display?.units?.temperature || 'C';
                $('cfgPressureUnit').value = cfg.display?.units?.pressure || 'psi';
                $('cfgSpeedUnit').value = cfg.display?.units?.speed || 'kph';

                // Thresholds
                const t = { ...D.thresholds, ...cfg.display?.thresholds };
                const tempF = $('cfgTempUnit').value === 'F';
                $('cfgRpmWarn').value = t.rpmWarn;
                $('cfgRpmDanger').value = t.rpmDanger;
                $('cfgOilWarn').value = t.oilPWarn || 15;
                $('cfgKnockWarn').value = t.knockWarn;
                $('cfgCltWarn').value = Math.round(tempF ? D.toFahrenheit(t.cltWarn) : t.cltWarn);
                $('cfgCltDanger').value = Math.round(tempF ? D.toFahrenheit(t.cltDanger) : t.cltDanger);
                $('cfgIatWarn').value = Math.round(tempF ? D.toFahrenheit(t.iatWarn) : t.iatWarn);
                $('cfgIatDanger').value = Math.round(tempF ? D.toFahrenheit(t.iatDanger) : t.iatDanger);
                $('cfgBattLow').value = t.battLow;
                $('cfgBattHigh').value = t.battHigh;

                $('cfgEcuType').value = cfg.ecu?.type || 'demo';
                $('cfgGpsType').value = cfg.gps?.type || 'demo';

                // Drivetrain
                const dt = { ...D.drivetrain, ...cfg.drivetrain };
                $('cfgFinalDrive').value = dt.finalDrive;
                $('cfgTireCircum').value = dt.tireCircumM;
                $('cfgGearTolerance').value = Math.round(dt.gearTolerance * 100);
                $('cfgShowGear').checked = dt.showGear !== false;
                buildGearRatioList(dt.gearRatios || []);

                // Vehicle
                const v = { ...D.vehicle, ...cfg.vehicle };
                $('cfgVehicleMass').value = v.massKg;
                $('cfgDragCoeff').value = v.dragCoeff;
                $('cfgFrontalArea').value = v.frontalAreaM2;
                $('cfgRollingResist').value = v.rollingResist;

                // Apply config to shared module for live preview
                D.applyConfig(cfg.display || {});
            })
            .catch(err => console.error('[settings] load failed', err));
    }

    // ---- Gear Ratio List ----
    function buildGearRatioList(ratios) {
        const container = $('gearRatioList');
        if (!container) return;
        container.innerHTML = '';
        const maxGears = Math.max(ratios.length, 6);

        for (let i = 0; i < maxGears; i++) {
            const row = document.createElement('div');
            row.className = 'gear-ratio-row';
            row.innerHTML = `
                <span class="gear-num">${i + 1}</span>
                <input type="number" step="0.001" class="gear-ratio-input"
                       id="gearRatio${i}" value="${ratios[i]?.toFixed(3) || ''}"
                       placeholder="—">
                <button class="gear-learn-btn" data-gear="${i}"
                        title="Drive in gear ${i + 1} at steady speed, then click">Learn</button>
            `;
            container.appendChild(row);
        }

        container.querySelectorAll('.gear-learn-btn').forEach(btn => {
            btn.addEventListener('click', function () {
                learnGear(parseInt(this.dataset.gear), this);
            });
        });
    }

    // ---- Multi-sample Gear Learning ----
    const SAMPLE_COUNT = 10;
    const SAMPLE_INTERVAL = 200; // ms between samples (10 samples × 200ms = 2s)

    function learnGear(idx, btnEl) {
        const frame = D.lastFrame;
        if (!frame || !frame.ecu) {
            btnEl.textContent = 'No data';
            setTimeout(() => { btnEl.textContent = 'Learn'; }, 1500);
            return;
        }
        const speedKph = frame.speed ? frame.speed.value : 0;
        if (speedKph < 10 || frame.ecu.rpm < 800) {
            btnEl.textContent = 'Drive faster';
            setTimeout(() => { btnEl.textContent = 'Learn'; }, 1500);
            return;
        }

        // Start multi-sample collection
        const samples = [];
        btnEl.classList.add('sampling');
        btnEl.disabled = true;
        btnEl.textContent = '0/' + SAMPLE_COUNT;

        const sampleTimer = setInterval(() => {
            const f = D.lastFrame;
            if (!f || !f.ecu) return; // skip bad frame
            const spd = f.speed ? f.speed.value : 0;
            const ratio = D.calcOverallRatio(f.ecu.rpm, spd);
            if (ratio > 0) {
                samples.push(ratio);
                btnEl.textContent = samples.length + '/' + SAMPLE_COUNT;
            }

            if (samples.length >= SAMPLE_COUNT) {
                clearInterval(sampleTimer);
                finishLearn(idx, btnEl, samples);
            }
        }, SAMPLE_INTERVAL);

        // Timeout safety — if we can't get 10 good samples in 5s, use what we have
        setTimeout(() => {
            clearInterval(sampleTimer);
            if (samples.length >= 3) {
                finishLearn(idx, btnEl, samples);
            } else {
                btnEl.classList.remove('sampling');
                btnEl.disabled = false;
                btnEl.textContent = 'Hold steady';
                setTimeout(() => { btnEl.textContent = 'Learn'; }, 1500);
            }
        }, 5000);
    }

    function finishLearn(idx, btnEl, samples) {
        // Average the samples
        const avgRatio = samples.reduce((s, v) => s + v, 0) / samples.length;
        const finalDrive = parseFloat($('cfgFinalDrive').value) || 3.73;
        const gearRatio = avgRatio / finalDrive;

        const input = $('gearRatio' + idx);
        input.value = gearRatio.toFixed(3);
        input.classList.remove('estimated');
        btnEl.classList.remove('sampling');
        btnEl.disabled = false;
        btnEl.textContent = '✓ ' + gearRatio.toFixed(3);
        btnEl.style.color = '#4ade80';
        setTimeout(() => { btnEl.textContent = 'Learn'; btnEl.style.color = ''; }, 2500);
    }

    // ---- Auto-Fill Extrapolation ----
    // Uses geometric progression: each higher gear ≈ previous × step_ratio
    // Typical step_ratio for manual transmissions is ~0.70–0.78 between gears
    function autoFillGears() {
        const maxGears = 6;
        // Find any learned (non-empty) gear
        let knownIdx = -1, knownRatio = 0;
        for (let i = 0; i < maxGears; i++) {
            const el = $('gearRatio' + i);
            if (!el) break;
            const v = parseFloat(el.value);
            if (v > 0 && !el.classList.contains('estimated')) {
                knownIdx = i;
                knownRatio = v;
                break;
            }
        }

        // Fall back to any value including estimates
        if (knownIdx < 0) {
            for (let i = 0; i < maxGears; i++) {
                const el = $('gearRatio' + i);
                if (!el) break;
                const v = parseFloat(el.value);
                if (v > 0) { knownIdx = i; knownRatio = v; break; }
            }
        }

        if (knownIdx < 0) {
            const btn = $('btnAutoFill');
            btn.textContent = 'Learn at least one gear first';
            setTimeout(() => { btn.textContent = '⚡ Auto-Fill Remaining Gears'; }, 2000);
            return;
        }

        // Typical geometric step ratio (each gear up ≈ 0.74× the previous)
        const stepRatio = 0.74;

        for (let i = 0; i < maxGears; i++) {
            const el = $('gearRatio' + i);
            if (!el) break;
            const existing = parseFloat(el.value);
            if (existing > 0) continue; // don't overwrite learned gears

            // Steps from known gear: negative = lower gear (multiply), positive = higher gear (divide)
            const stepsFromKnown = i - knownIdx;
            const estimated = knownRatio * Math.pow(stepRatio, stepsFromKnown);
            el.value = '~' + estimated.toFixed(3);
            el.classList.add('estimated');
        }

        const btn = $('btnAutoFill');
        btn.textContent = '✓ Filled — verify and re-learn as needed';
        setTimeout(() => { btn.textContent = '⚡ Auto-Fill Remaining Gears'; }, 3000);
    }

    // ---- Collect Gear Ratios ----
    function collectGearRatios() {
        const ratios = [];
        for (let i = 0; i < 8; i++) {
            const el = $('gearRatio' + i);
            if (!el) break;
            // Strip leading ~ from estimated values
            const raw = el.value.toString().replace(/^~/, '');
            const v = parseFloat(raw);
            if (v > 0) ratios.push(v);
            else break;
        }
        return ratios.length > 0 ? ratios : [];
    }

    // ---- Save Config ----
    function saveConfig() {
        const tempUnit = $('cfgTempUnit').value;
        const cltWarnVal = parseFloat($('cfgCltWarn').value);
        const cltDangerVal = parseFloat($('cfgCltDanger').value);
        const iatWarnVal = parseFloat($('cfgIatWarn').value);
        const iatDangerVal = parseFloat($('cfgIatDanger').value);
        const toC = (v) => tempUnit === 'F' ? D.toCelsius(v) : v;

        const cfg = {
            ecu: { type: $('cfgEcuType').value },
            gps: { type: $('cfgGpsType').value },
            display: {
                layout: $('cfgLayout').value,
                units: {
                    pressure: $('cfgPressureUnit').value,
                    speed: $('cfgSpeedUnit').value,
                    temperature: tempUnit,
                },
                thresholds: {
                    rpmWarn: parseInt($('cfgRpmWarn').value),
                    rpmDanger: parseInt($('cfgRpmDanger').value),
                    cltWarn: toC(cltWarnVal),
                    cltDanger: toC(cltDangerVal),
                    iatWarn: toC(iatWarnVal),
                    iatDanger: toC(iatDangerVal),
                    oilPWarn: parseInt($('cfgOilWarn').value),
                    knockWarn: parseInt($('cfgKnockWarn').value),
                    battLow: parseFloat($('cfgBattLow').value),
                    battHigh: parseFloat($('cfgBattHigh').value),
                },
            },
            drivetrain: {
                showGear: $('cfgShowGear').checked,
                finalDrive: parseFloat($('cfgFinalDrive').value) || 3.73,
                tireCircumM: parseFloat($('cfgTireCircum').value) || 1.95,
                gearTolerance: (parseInt($('cfgGearTolerance').value) || 15) / 100,
                gearRatios: collectGearRatios(),
            },
            vehicle: {
                massKg: parseFloat($('cfgVehicleMass').value) || 1200,
                dragCoeff: parseFloat($('cfgDragCoeff').value) || 0.32,
                frontalAreaM2: parseFloat($('cfgFrontalArea').value) || 2.2,
                rollingResist: parseFloat($('cfgRollingResist').value) || 0.012,
            },
        };

        fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(cfg),
        })
            .then(() => { window.location.href = '/'; })
            .catch(err => { console.error('[settings] save failed', err); });
    }

    // ---- Temperature unit change ----
    $('cfgTempUnit').addEventListener('change', function () {
        const newUnit = this.value;
        const fields = ['cfgCltWarn', 'cfgCltDanger', 'cfgIatWarn', 'cfgIatDanger'];
        fields.forEach(id => {
            const el = $(id);
            if (!el) return;
            const val = parseFloat(el.value);
            // If switching to F, current value is C → convert to F
            // If switching to C, current value is F → convert to C
            if (newUnit === 'F') el.value = Math.round(D.toFahrenheit(val));
            else el.value = Math.round(D.toCelsius(val));
        });
    });

    // ---- Event Listeners ----
    $('btnSave').addEventListener('click', saveConfig);
    $('btnSaveBottom').addEventListener('click', saveConfig);
    $('btnAutoFill').addEventListener('click', autoFillGears);

    // Instructions toggle
    $('gearInstructionsToggle').addEventListener('click', function () {
        this.classList.toggle('open');
        $('gearInstructionsBody').classList.toggle('open');
    });

    // ---- Init ----
    window.addEventListener('load', () => {
        D.connect();
        loadConfig();
    });
})();
