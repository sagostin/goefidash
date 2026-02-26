// ================================================================
// SPEEDUINO DASH — Dashboard Display Logic
// Uses shared.js for WebSocket, state, and calculations
// ================================================================

(function () {
    'use strict';

    const D = window.SpeeduinoDash;
    const $ = (id) => document.getElementById(id);

    let smoothRPM = 0;
    let smoothSpeed = 0;
    let currentWarning = null;
    let warningTimer = null;

    // ---- Odometer (via frame callback) ----
    function updateOdometer(odo) {
        if (odo.total !== undefined) $('odoTotal').textContent = D.convertDistance(odo.total).toFixed(1);
        if (odo.trip !== undefined) $('odoTrip').textContent = D.convertDistance(odo.trip).toFixed(1);
    }

    // ---- Unit Labels ----
    function updateUnitLabels() {
        const u = D.units;
        $('speedUnit').textContent = u.speed === 'mph' ? 'MPH' : 'km/h';
        const pLabel = u.pressure === 'psi' ? 'PSI' : u.pressure === 'bar' ? 'BAR' : 'kPa';
        $('boostUnit').textContent = pLabel;
        $('oilUnit').textContent = pLabel;
        $('cltUnit').textContent = u.temperature === 'F' ? '°F' : '°C';
        $('iatUnit').textContent = u.temperature === 'F' ? '°F' : '°C';
        const distUnit = u.speed === 'mph' ? 'mi' : 'km';
        $('odoDistUnit').textContent = distUnit;
        $('odoTripUnit').textContent = distUnit;
    }

    // ---- Frame Handler ----
    D.onFrame = function (frame) {
        if (frame.odo) updateOdometer(frame.odo);
    };

    D.onConfig = function () {
        updateUnitLabels();
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
        $('speedValue').textContent = Math.round(D.convertSpeed(smoothSpeed));

        // HP estimation
        const stamp = frame.stamp || Date.now();
        const hp = D.calcEstimatedHP(rawSpeed, stamp);
        $('hpValue').textContent = Math.round(hp);
        $('hpPeak').textContent = Math.round(D.peakHP);
        $('hpBar').style.width = Math.min(hp / 500 * 100, 100) + '%';

        // GPS status
        if (gpsData) setStatus('statusGPS', gpsData.valid ? 'connected' : '');

        if (ecu) {
            // RPM
            smoothRPM += (ecu.rpm - smoothRPM) * 0.3;
            const rpmInt = Math.round(smoothRPM);
            $('rpmValue').textContent = rpmInt;
            const rpmEl = $('rpmValue');
            rpmEl.className = 'big-value' +
                (rpmInt >= t.rpmDanger ? ' danger' : rpmInt >= t.rpmWarn ? ' warn' : '');

            // Engine status
            const statusEl = $('engineStatus');
            if (ecu.coolant >= t.cltDanger || ecu.iat >= t.iatDanger) {
                statusEl.textContent = 'HOT'; statusEl.className = 'engine-status warning';
            } else if (ecu.running) {
                statusEl.textContent = 'RUN'; statusEl.className = 'engine-status running';
            } else {
                statusEl.textContent = 'OFF'; statusEl.className = 'engine-status';
            }

            // Gear
            const rawSpeedKph = speedData ? speedData.value : 0;
            const calculatedGear = D.detectGear(smoothRPM, rawSpeedKph);
            const gear = calculatedGear !== null ? calculatedGear : (ecu.gear || 0);
            const gearEl = $('gearDisplay');
            gearEl.textContent = gear === 0 ? 'N' : gear;
            gearEl.style.display = D.showGear ? '' : 'none';

            // AFR
            $('afrValue').textContent = ecu.afr.toFixed(1);
            $('afrMarker').style.left = (Math.max(0, Math.min(1, (ecu.afr - 10) / 8)) * 100) + '%';
            setCardState('afrCard', (ecu.afr > 16 || ecu.afr < 11) ? 'danger' : (ecu.afr > 15 || ecu.afr < 12) ? 'warn' : '');

            // Boost
            const boostKpa = ecu.map > 100 ? ecu.map - 100 : 0;
            $('boostValue').textContent = D.convertPressure(boostKpa).toFixed(D.units.pressure === 'psi' ? 1 : 0);
            $('boostBar').style.width = Math.min(boostKpa / 150 * 100, 100) + '%';

            // Oil
            const oilVal = D.units.pressure === 'psi' ? ecu.oilPressure : D.convertPressure(ecu.oilPressure * 6.895);
            $('oilValue').textContent = Math.round(oilVal);
            $('oilBar').style.width = Math.min(ecu.oilPressure / 80 * 100, 100) + '%';
            setCardState('oilCard', ecu.oilPressure < t.oilPWarn && ecu.running ? 'danger' : '');

            // Coolant
            const cltC = ecu.coolant;
            $('cltValue').textContent = Math.round(D.displayTemp(cltC));
            $('cltBar').style.width = Math.max(0, Math.min((cltC - 40) / 80 * 100, 100)) + '%';
            setCardState('cltCard', cltC >= t.cltDanger ? 'danger' : cltC >= t.cltWarn ? 'warn' : '');

            // IAT
            const iatC = ecu.iat;
            $('iatValue').textContent = Math.round(D.displayTemp(iatC));
            $('iatBar').style.width = Math.max(0, Math.min((iatC + 10) / 70 * 100, 100)) + '%';
            setCardState('iatCard', iatC >= t.iatDanger ? 'danger' : iatC >= t.iatWarn ? 'warn' : '');

            // Knock
            const knockCnt = ecu.knockCount || 0, knockRet = ecu.knockCor || 0;
            $('knockCount').textContent = knockCnt;
            $('knockRetard').textContent = knockRet + '°';
            const knockInd = $('knockIndicator');
            knockInd.classList.toggle('active', knockRet > 0 || knockCnt > 0);
            if (knockRet >= t.knockWarn) {
                $('knockCount').className = 'knock-value danger';
                $('knockRetard').className = 'knock-value danger';
                setCardState('knockCard', 'danger');
            } else if (knockRet > 0) {
                $('knockCount').className = 'knock-value warn';
                $('knockRetard').className = 'knock-value warn';
                setCardState('knockCard', 'warn');
            } else {
                $('knockCount').className = 'knock-value';
                $('knockRetard').className = 'knock-value';
                setCardState('knockCard', '');
            }
            $('knockBar').style.width = Math.min(knockRet / 10 * 100, 100) + '%';

            // ECU sync
            setStatus('statusSync', ecu.sync ? 'connected' : '');

            // ---- Warnings ----
            let wt = '', wp = '';
            if (cltC >= t.cltDanger) { wt = 'COOLANT ' + D.formatTemp(cltC); wp = 'critical'; }
            else if (iatC >= t.iatDanger) { wt = 'INTAKE HOT ' + D.formatTemp(iatC); wp = 'critical'; }
            else if (ecu.oilPressure < t.oilPWarn && ecu.running && ecu.rpm > 1000) {
                wt = 'LOW OIL ' + Math.round(oilVal) + ' ' + (D.units.pressure === 'psi' ? 'PSI' : D.units.pressure === 'bar' ? 'BAR' : 'kPa');
                wp = 'critical';
            }
            else if (knockRet >= t.knockWarn) { wt = 'KNOCK -' + knockRet + '°'; wp = 'critical'; }
            else if (cltC >= t.cltWarn) { wt = 'COOLANT ' + D.formatTemp(cltC); wp = 'warning'; }
            else if (iatC >= t.iatWarn) { wt = 'INTAKE ' + D.formatTemp(iatC); wp = 'warning'; }
            else if (ecu.afr > 16.5 || ecu.afr < 10.5) { wt = 'AFR ' + ecu.afr.toFixed(1); wp = 'danger'; }
            else if (ecu.batteryVoltage < t.battLow) { wt = 'LOW BATT ' + ecu.batteryVoltage.toFixed(1) + 'V'; wp = 'warning'; }
            else if (ecu.batteryVoltage > t.battHigh) { wt = 'HIGH BATT ' + ecu.batteryVoltage.toFixed(1) + 'V'; wp = 'warning'; }

            if (wt) showWarning(wt, wp); else clearWarning();
        } else {
            setStatus('statusSync', '');
            $('rpmValue').textContent = '0';
            $('engineStatus').textContent = 'OFF';
            $('engineStatus').className = 'engine-status';
            $('knockIndicator').classList.remove('active');
            clearWarning();
        }

        requestAnimationFrame(updateDisplay);
    }

    // ---- Trip Reset ----
    $('btnResetTrip').addEventListener('click', () => {
        fetch('/api/odo/reset-trip', { method: 'POST' })
            .then(() => { $('odoTrip').textContent = '0.0'; })
            .catch(() => { });
    });

    // ---- HP Peak Reset ----
    $('btnResetHP').addEventListener('click', () => {
        D.resetPeakHP();
        $('hpPeak').textContent = '0';
        $('hpValue').textContent = '0';
    });

    // ---- Init ----
    window.addEventListener('load', () => {
        D.onConnectionChange = (connected) => {
            $('connectionBanner').classList.toggle('active', !connected);
        };
        D.connect();
        requestAnimationFrame(updateDisplay);
    });
})();
