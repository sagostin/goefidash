// ================================================================
// SPEEDUINO DASH — Dashboard Display Logic
// Uses shared.js for WebSocket, state, and calculations
// Supports multiple layouts: classic, sweep, race, minimal
// ================================================================

(function () {
    'use strict';

    const D = window.SpeeduinoDash;
    const $ = (id) => document.getElementById(id);

    let smoothRPM = 0;
    let smoothSpeed = 0;
    let currentWarning = null;
    let warningTimer = null;
    let activeLayout = 'classic';

    // ---- Layout Switching ----
    function activateLayout(name) {
        const layouts = document.querySelectorAll('.layout');
        layouts.forEach(l => l.classList.remove('active'));
        const target = $('layout-' + name);
        if (target) {
            target.classList.add('active');
            activeLayout = name;
        } else {
            // Fallback to classic
            $('layout-classic').classList.add('active');
            activeLayout = 'classic';
        }
    }

    // ---- Odometer (via frame callback) ----
    function updateOdometer(odo) {
        // Classic
        if (odo.total !== undefined && $('odoTotal')) $('odoTotal').textContent = D.convertDistance(odo.total).toFixed(1);
        if (odo.trip !== undefined && $('odoTrip')) $('odoTrip').textContent = D.convertDistance(odo.trip).toFixed(1);
        // Race
        if (odo.total !== undefined && $('raceOdoTotal')) $('raceOdoTotal').textContent = D.convertDistance(odo.total).toFixed(1);
        if (odo.trip !== undefined && $('raceOdoTrip')) $('raceOdoTrip').textContent = D.convertDistance(odo.trip).toFixed(1);
    }

    // ---- Unit Labels ----
    function updateUnitLabels() {
        const u = D.units;
        const speedLabel = u.speed === 'mph' ? 'MPH' : 'km/h';
        const pLabel = u.pressure === 'psi' ? 'PSI' : u.pressure === 'bar' ? 'BAR' : 'kPa';
        const tLabel = u.temperature === 'F' ? '°F' : '°C';
        const distUnit = u.speed === 'mph' ? 'mi' : 'km';

        // Classic
        if ($('speedUnit')) $('speedUnit').textContent = speedLabel;
        if ($('boostUnit')) $('boostUnit').textContent = pLabel;
        if ($('oilUnit')) $('oilUnit').textContent = pLabel;
        if ($('cltUnit')) $('cltUnit').textContent = tLabel;
        if ($('iatUnit')) $('iatUnit').textContent = tLabel;
        if ($('odoDistUnit')) $('odoDistUnit').textContent = distUnit;
        if ($('odoTripUnit')) $('odoTripUnit').textContent = distUnit;

        // Sweep
        if ($('sweepSpeedUnit')) $('sweepSpeedUnit').textContent = speedLabel;

        // Race
        if ($('raceSpeedUnit')) $('raceSpeedUnit').textContent = speedLabel;
        if ($('raceBoostUnit')) $('raceBoostUnit').textContent = pLabel;
        if ($('raceOilUnit')) $('raceOilUnit').textContent = pLabel;
        if ($('raceCltUnit')) $('raceCltUnit').textContent = tLabel;
        if ($('raceIatUnit')) $('raceIatUnit').textContent = tLabel;
        if ($('raceOdoDistUnit')) $('raceOdoDistUnit').textContent = distUnit;

        // Minimal
        if ($('minSpeedUnit')) $('minSpeedUnit').textContent = speedLabel;
    }

    // ---- Frame Handler ----
    D.onFrame = function (frame) {
        if (frame.odo) updateOdometer(frame.odo);
    };

    D.onConfig = function (cfg) {
        updateUnitLabels();
        if (cfg && cfg.layout) {
            activateLayout(cfg.layout);
        }
    };

    // ---- Warning System ----
    function showWarning(text, priority) {
        const priorities = { 'critical': 3, 'danger': 2, 'warning': 1 };
        const currentPriority = currentWarning ? priorities[currentWarning.priority] || 0 : 0;
        if ((priorities[priority] || 0) >= currentPriority) {
            currentWarning = { text, priority };
            $('warningText').textContent = text;
            $('warningOverlay').classList.add('active');
            if (warningTimer) clearTimeout(warningTimer);
            if (priority !== 'critical') {
                warningTimer = setTimeout(() => {
                    $('warningOverlay').classList.remove('active');
                    currentWarning = null;
                }, 3000);
            }
        }
    }

    function clearWarning() {
        if (currentWarning && currentWarning.priority !== 'critical') {
            $('warningOverlay').classList.remove('active');
            currentWarning = null;
        }
    }

    function setCardState(id, state) {
        const el = $(id);
        if (!el) return;
        el.classList.remove('warn', 'danger');
        if (state) el.classList.add(state);
    }

    function setStatus(id, cls) {
        const el = $(id);
        if (!el) return;
        el.classList.remove('connected');
        if (cls) el.classList.add(cls);
    }

    // ---- Sweep arc calculation ----
    // The arc path length is ~502 (semicircle r=160). dash-offset controls fill.
    const SWEEP_ARC_LENGTH = 502;

    function updateSweepArc(rpm) {
        const arc = $('sweepArc');
        if (!arc) return;
        const t = D.thresholds;
        const max = t.rpmMax || 8000;
        const pct = Math.min(rpm / max, 1);
        arc.style.strokeDashoffset = SWEEP_ARC_LENGTH * (1 - pct);
    }

    // ---- Main Update Loop ----
    function updateDisplay() {
        const frame = D.lastFrame;
        if (!frame) { requestAnimationFrame(updateDisplay); return; }

        const ecu = frame.ecu;
        const gpsData = frame.gps;
        const speedData = frame.speed;
        const t = D.thresholds;

        // Speed
        const rawSpeed = speedData ? speedData.value : 0;
        smoothSpeed += (rawSpeed - smoothSpeed) * 0.3;
        const displaySpeed = Math.round(D.convertSpeed(smoothSpeed));

        // HP estimation
        const stamp = frame.stamp || Date.now();
        const hp = D.calcEstimatedHP(rawSpeed, stamp);
        const hpRound = Math.round(hp);
        const peakRound = Math.round(D.peakHP);

        // GPS status — all layouts
        if (gpsData) {
            const gpsConnected = gpsData.valid ? 'connected' : '';
            setStatus('statusGPS', gpsConnected);
            setStatus('sweepStatusGPS', gpsConnected);
            setStatus('raceStatusGPS', gpsConnected);
            setStatus('minStatusGPS', gpsConnected);
        }

        // Gear detection
        let gear = 0;
        let gearText = 'N';

        if (ecu) {
            // RPM
            smoothRPM += (ecu.rpm - smoothRPM) * 0.3;
            const rpmInt = Math.round(smoothRPM);
            const engineRunning = smoothRPM > 500;

            // Gear
            const rawSpeedKph = speedData ? speedData.value : 0;
            const calculatedGear = D.detectGear(smoothRPM, rawSpeedKph);
            gear = calculatedGear !== null ? calculatedGear : (ecu.gear || 0);
            gearText = gear === 0 ? 'N' : String(gear);

            // RPM class
            const rpmWarn = engineRunning && rpmInt >= t.rpmDanger ? 'danger' : engineRunning && rpmInt >= t.rpmWarn ? 'warn' : '';

            // Computed values
            const boostKpa = ecu.map > 100 ? ecu.map - 100 : 0;
            const boostDisp = D.convertPressure(boostKpa).toFixed(D.units.pressure === 'psi' ? 1 : 0);
            const oilVal = D.units.pressure === 'psi' ? ecu.oilPressure : D.convertPressure(ecu.oilPressure * 6.895);
            const cltC = ecu.coolant;
            const iatC = ecu.iat;
            const knockRet = ecu.knockCor || 0;
            const knockCnt = ecu.knockCount || 0;

            // Engine status determination
            let engineStatusText = 'OFF';
            let engineStatusClass = '';
            if (engineRunning && (cltC >= t.cltDanger || iatC >= t.iatDanger)) {
                engineStatusText = 'HOT'; engineStatusClass = 'warning';
            } else if (ecu.running) {
                engineStatusText = 'RUN'; engineStatusClass = 'running';
            }

            // Card states
            const afrState = engineRunning ? ((ecu.afr > 16 || ecu.afr < 11) ? 'danger' : (ecu.afr > 15 || ecu.afr < 12) ? 'warn' : '') : '';
            const oilState = engineRunning && ecu.oilPressure < t.oilPWarn ? 'danger' : '';
            const cltState = engineRunning ? (cltC >= t.cltDanger ? 'danger' : cltC >= t.cltWarn ? 'warn' : '') : '';
            const iatState = engineRunning ? (iatC >= t.iatDanger ? 'danger' : iatC >= t.iatWarn ? 'warn' : '') : '';
            const knockState = engineRunning ? (knockRet >= t.knockWarn ? 'danger' : knockRet > 0 ? 'warn' : '') : '';

            // ECU sync — all layouts
            const syncCls = ecu.sync ? 'connected' : '';
            setStatus('statusSync', syncCls);
            setStatus('sweepStatusSync', syncCls);
            setStatus('raceStatusSync', syncCls);
            setStatus('minStatusSync', syncCls);

            // ================================================================
            // UPDATE CLASSIC LAYOUT
            // ================================================================
            if (activeLayout === 'classic') {
                $('rpmValue').textContent = rpmInt;
                $('rpmValue').className = 'big-value' + (rpmWarn ? ' ' + rpmWarn : '');
                $('speedValue').textContent = displaySpeed;
                $('hpValue').textContent = hpRound;
                $('hpPeak').textContent = peakRound;
                $('hpBar').style.width = Math.min(hp / 500 * 100, 100) + '%';

                const statusEl = $('engineStatus');
                statusEl.textContent = engineStatusText;
                statusEl.className = 'engine-status' + (engineStatusClass ? ' ' + engineStatusClass : '');

                const gearEl = $('gearDisplay');
                gearEl.textContent = gearText;
                gearEl.style.display = D.showGear ? '' : 'none';

                // AFR
                $('afrValue').textContent = ecu.afr.toFixed(1);
                $('afrMarker').style.left = (Math.max(0, Math.min(1, (ecu.afr - 10) / 8)) * 100) + '%';
                setCardState('afrCard', afrState);

                // Boost
                $('boostValue').textContent = boostDisp;
                $('boostBar').style.width = Math.min(boostKpa / 150 * 100, 100) + '%';

                // Oil
                $('oilValue').textContent = Math.round(oilVal);
                $('oilBar').style.width = Math.min(ecu.oilPressure / 80 * 100, 100) + '%';
                setCardState('oilCard', oilState);

                // Coolant
                $('cltValue').textContent = Math.round(D.displayTemp(cltC));
                $('cltBar').style.width = Math.max(0, Math.min((cltC - 40) / 80 * 100, 100)) + '%';
                setCardState('cltCard', cltState);

                // IAT
                $('iatValue').textContent = Math.round(D.displayTemp(iatC));
                $('iatBar').style.width = Math.max(0, Math.min((iatC + 10) / 70 * 100, 100)) + '%';
                setCardState('iatCard', iatState);

                // Knock
                $('knockCount').textContent = knockCnt;
                $('knockRetard').textContent = knockRet + '°';
                $('knockIndicator').classList.toggle('active', engineRunning && (knockRet > 0 || knockCnt > 0));
                $('knockCount').className = 'knock-value' + (knockState ? ' ' + knockState : '');
                $('knockRetard').className = 'knock-value' + (knockState ? ' ' + knockState : '');
                setCardState('knockCard', knockState);
                $('knockBar').style.width = Math.min(knockRet / 10 * 100, 100) + '%';
            }

            // ================================================================
            // UPDATE SWEEP LAYOUT
            // ================================================================
            else if (activeLayout === 'sweep') {
                $('sweepRpmValue').textContent = rpmInt;
                $('sweepRpmValue').className = 'sweep-rpm' + (rpmWarn ? ' ' + rpmWarn : '');
                updateSweepArc(smoothRPM);

                const sweepStatus = $('sweepEngineStatus');
                sweepStatus.textContent = engineStatusText;
                sweepStatus.className = 'sweep-engine-status' + (engineStatusClass ? ' ' + engineStatusClass : '');

                $('sweepSpeedValue').textContent = displaySpeed;
                const sweepGear = $('sweepGearDisplay');
                sweepGear.textContent = gearText;
                sweepGear.style.display = D.showGear ? '' : 'none';

                $('sweepHpValue').textContent = hpRound;
                $('sweepHpPeak').textContent = peakRound;

                // Gauges
                $('sweepAfrValue').textContent = ecu.afr.toFixed(1);
                $('sweepAfrBar').style.width = (Math.max(0, Math.min(1, (ecu.afr - 10) / 8)) * 100) + '%';
                setCardState('sweepAfrCard', afrState);

                $('sweepBoostValue').textContent = boostDisp;
                $('sweepBoostBar').style.width = Math.min(boostKpa / 150 * 100, 100) + '%';
                setCardState('sweepBoostCard', '');

                $('sweepOilValue').textContent = Math.round(oilVal);
                $('sweepOilBar').style.width = Math.min(ecu.oilPressure / 80 * 100, 100) + '%';
                setCardState('sweepOilCard', oilState);

                $('sweepCltValue').textContent = Math.round(D.displayTemp(cltC));
                $('sweepCltBar').style.width = Math.max(0, Math.min((cltC - 40) / 80 * 100, 100)) + '%';
                setCardState('sweepCltCard', cltState);

                $('sweepIatValue').textContent = Math.round(D.displayTemp(iatC));
                $('sweepIatBar').style.width = Math.max(0, Math.min((iatC + 10) / 70 * 100, 100)) + '%';
                setCardState('sweepIatCard', iatState);

                $('sweepKnockValue').textContent = knockRet + '°';
                $('sweepKnockBar').style.width = Math.min(knockRet / 10 * 100, 100) + '%';
                setCardState('sweepKnockCard', knockState);
                $('sweepKnockIndicator').classList.toggle('active', engineRunning && (knockRet > 0 || knockCnt > 0));
            }

            // ================================================================
            // UPDATE RACE LAYOUT
            // ================================================================
            else if (activeLayout === 'race') {
                $('raceRpmValue').textContent = rpmInt;
                const rpmMax = t.rpmMax || 8000;
                $('raceRpmBar').style.width = Math.min(smoothRPM / rpmMax * 100, 100) + '%';

                $('raceSpeedValue').textContent = displaySpeed;
                $('raceGearDisplay').textContent = gearText;
                $('raceHpValue').textContent = hpRound;

                // Engine status label
                const raceStatusEl = $('raceEngineStatus');
                raceStatusEl.textContent = engineStatusText;
                raceStatusEl.className = 'race-info-label' + (engineStatusClass === 'running' ? ' running' : engineStatusClass === 'warning' ? ' hot' : '');

                // Values
                $('raceAfrValue').textContent = ecu.afr.toFixed(1);
                setCardState('raceAfrCard', afrState);

                $('raceBoostValue').textContent = boostDisp;
                setCardState('raceBoostCard', '');

                $('raceOilValue').textContent = Math.round(oilVal);
                setCardState('raceOilCard', oilState);

                $('raceCltValue').textContent = Math.round(D.displayTemp(cltC));
                setCardState('raceCltCard', cltState);

                $('raceIatValue').textContent = Math.round(D.displayTemp(iatC));
                setCardState('raceIatCard', iatState);

                $('raceKnockValue').textContent = knockRet + '°';
                setCardState('raceKnockCard', knockState);
                $('raceKnockIndicator').classList.toggle('active', engineRunning && (knockRet > 0 || knockCnt > 0));
            }

            // ================================================================
            // UPDATE MINIMAL LAYOUT
            // ================================================================
            else if (activeLayout === 'minimal') {
                $('minSpeedValue').textContent = displaySpeed;
                $('minRpmValue').textContent = rpmInt;
                $('minRpmValue').className = 'minimal-rpm' + (rpmWarn ? ' ' + rpmWarn : '');
                $('minGearDisplay').textContent = gearText;
                $('minGearDisplay').style.display = D.showGear ? '' : 'none';

                const minStatus = $('minEngineStatus');
                minStatus.textContent = engineStatusText;
                minStatus.className = 'minimal-status-item' + (engineStatusClass === 'running' ? ' running' : engineStatusClass === 'warning' ? ' hot' : '');
            }

            // ---- Warnings (shared across all layouts, only when engine running) ----
            let wt = '', wp = '';
            if (engineRunning) {
                if (cltC >= t.cltDanger) { wt = 'COOLANT ' + D.formatTemp(cltC); wp = 'critical'; }
                else if (iatC >= t.iatDanger) { wt = 'INTAKE HOT ' + D.formatTemp(iatC); wp = 'critical'; }
                else if (ecu.oilPressure < t.oilPWarn && ecu.rpm > 1000) {
                    wt = 'LOW OIL ' + Math.round(oilVal) + ' ' + (D.units.pressure === 'psi' ? 'PSI' : D.units.pressure === 'bar' ? 'BAR' : 'kPa');
                    wp = 'critical';
                }
                else if (knockRet >= t.knockWarn) { wt = 'KNOCK -' + knockRet + '°'; wp = 'critical'; }
                else if (cltC >= t.cltWarn) { wt = 'COOLANT ' + D.formatTemp(cltC); wp = 'warning'; }
                else if (iatC >= t.iatWarn) { wt = 'INTAKE ' + D.formatTemp(iatC); wp = 'warning'; }
                else if (ecu.afr > 16.5 || ecu.afr < 10.5) { wt = 'AFR ' + ecu.afr.toFixed(1); wp = 'danger'; }
                else if (ecu.batteryVoltage < t.battLow) { wt = 'LOW BATT ' + ecu.batteryVoltage.toFixed(1) + 'V'; wp = 'warning'; }
                else if (ecu.batteryVoltage > t.battHigh) { wt = 'HIGH BATT ' + ecu.batteryVoltage.toFixed(1) + 'V'; wp = 'warning'; }
            }

            if (wt) showWarning(wt, wp); else clearWarning();
        } else {
            // No ECU data — all layouts
            setStatus('statusSync', '');
            setStatus('sweepStatusSync', '');
            setStatus('raceStatusSync', '');
            setStatus('minStatusSync', '');

            if (activeLayout === 'classic') {
                $('rpmValue').textContent = '0';
                $('engineStatus').textContent = 'OFF';
                $('engineStatus').className = 'engine-status';
                $('knockIndicator').classList.remove('active');
            } else if (activeLayout === 'sweep') {
                $('sweepRpmValue').textContent = '0';
                updateSweepArc(0);
                $('sweepEngineStatus').textContent = 'OFF';
                $('sweepEngineStatus').className = 'sweep-engine-status';
                $('sweepKnockIndicator').classList.remove('active');
            } else if (activeLayout === 'race') {
                $('raceRpmValue').textContent = '0';
                $('raceRpmBar').style.width = '0%';
                $('raceEngineStatus').textContent = 'OFF';
                $('raceEngineStatus').className = 'race-info-label';
                $('raceKnockIndicator').classList.remove('active');
            } else if (activeLayout === 'minimal') {
                $('minRpmValue').textContent = '0';
                $('minEngineStatus').textContent = 'OFF';
                $('minEngineStatus').className = 'minimal-status-item';
            }
            clearWarning();
        }

        // Always update speed display even without ECU
        if (activeLayout === 'classic') $('speedValue').textContent = displaySpeed;
        else if (activeLayout === 'sweep') $('sweepSpeedValue').textContent = displaySpeed;
        else if (activeLayout === 'race') $('raceSpeedValue').textContent = displaySpeed;
        else if (activeLayout === 'minimal') $('minSpeedValue').textContent = displaySpeed;

        requestAnimationFrame(updateDisplay);
    }

    // ---- Trip Reset ----
    if ($('btnResetTrip')) {
        $('btnResetTrip').addEventListener('click', () => {
            fetch('/api/odo/reset-trip', { method: 'POST' })
                .then(() => {
                    if ($('odoTrip')) $('odoTrip').textContent = '0.0';
                    if ($('raceOdoTrip')) $('raceOdoTrip').textContent = '0.0';
                })
                .catch(() => { });
        });
    }

    // ---- HP Peak Reset ----
    if ($('btnResetHP')) {
        $('btnResetHP').addEventListener('click', () => {
            D.resetPeakHP();
            if ($('hpPeak')) $('hpPeak').textContent = '0';
            if ($('hpValue')) $('hpValue').textContent = '0';
            if ($('sweepHpPeak')) $('sweepHpPeak').textContent = '0';
            if ($('sweepHpValue')) $('sweepHpValue').textContent = '0';
        });
    }

    // ---- Init ----
    window.addEventListener('load', () => {
        D.onConnectionChange = (connected) => {
            $('connectionBanner').classList.toggle('active', !connected);
        };

        // Load initial layout from config
        fetch('/api/config')
            .then(r => r.json())
            .then(cfg => {
                const layout = cfg.display?.layout || 'classic';
                activateLayout(layout);
                updateUnitLabels();
            })
            .catch(() => { /* use default classic */ });

        D.connect();
        requestAnimationFrame(updateDisplay);
    });
})();
