// ================================================================
// SPEEDUINO DASH — 6" Display with Temperature Warnings
// Critical values: RPM, Speed, AFR, Boost, Oil, Coolant, IAT, Knock
// ================================================================

(function () {
    'use strict';

    // ---- State ----
    let ws = null;
    let reconnectTimer = null;
    let smoothRPM = 0;
    let smoothSpeed = 0;
    let lastFrame = null;
    let currentWarning = null;
    let warningTimer = null;

    // Thresholds (always stored in Celsius internally, converted for display if needed)
    let thresholds = {
        rpmWarn: 6000,
        rpmDanger: 7000,
        rpmMax: 8000,
        oilMin: 15,
        cltWarn: 95,
        cltDanger: 105,
        iatWarn: 60,
        iatDanger: 75,
        knockWarn: 3,
        battLow: 12.0,
        battHigh: 15.5,
    };

    let units = {
        pressure: 'psi',
        speed: 'kph',
        temperature: 'C'
    };

    const $ = (id) => document.getElementById(id);

    // Temperature conversion helpers
    function toFahrenheit(c) { return (c * 1.8) + 32; }
    function toCelsius(f) { return (f - 32) / 1.8; }
    
    // Convert for display
    function displayTemp(c) {
        return units.temperature === 'F' ? toFahrenheit(c) : c;
    }
    
    // Convert threshold from display unit to Celsius for comparison
    function thresholdToCelsius(val) {
        return units.temperature === 'F' ? toCelsius(val) : val;
    }

    // Format temperature with unit
    function formatTemp(c, decimals = 0) {
        const val = displayTemp(c);
        return val.toFixed(decimals) + (units.temperature === 'F' ? '°F' : '°C');
    }

    // ---- WebSocket ----
    function connect() {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(`${proto}//${location.host}/ws`);

        ws.onopen = () => {
            if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
        };

        ws.onmessage = (evt) => {
            try {
                const frame = JSON.parse(evt.data);
                if (frame.config) applyConfig(frame.config);
                if (frame.ecu || frame.gps || frame.speed) lastFrame = frame;
            } catch (e) {
                console.error('[ws] parse error', e);
            }
        };

        ws.onclose = () => scheduleReconnect();
        ws.onerror = () => ws.close();
    }

    function scheduleReconnect() {
        if (!reconnectTimer) {
            reconnectTimer = setTimeout(() => { reconnectTimer = null; connect(); }, 1000);
        }
    }

    function applyConfig(cfg) {
        if (cfg.units) {
            units = { ...units, ...cfg.units };
            updateUnitLabels();
        }
        if (cfg.thresholds) {
            thresholds = { ...thresholds, ...cfg.thresholds };
            // Re-convert threshold inputs to match current unit
            updateThresholdInputs();
        }
    }

    function updateUnitLabels() {
        $('speedUnit').textContent = units.speed === 'mph' ? 'MPH' : 'km/h';
        $('boostUnit').textContent = units.pressure === 'psi' ? 'PSI' : units.pressure === 'bar' ? 'BAR' : 'kPa';
        $('oilUnit').textContent = units.pressure === 'psi' ? 'PSI' : units.pressure === 'bar' ? 'BAR' : 'kPa';
        $('cltUnit').textContent = units.temperature === 'F' ? '°F' : '°C';
        $('iatUnit').textContent = units.temperature === 'F' ? '°F' : '°C';
    }

    // Update threshold input values when unit changes
    function updateThresholdInputs() {
        const cltWarnInput = $('cfgCltWarn');
        const cltDangerInput = $('cfgCltDanger');
        const iatWarnInput = $('cfgIatWarn');
        const iatDangerInput = $('cfgIatDanger');
        
        if (cltWarnInput) cltWarnInput.value = Math.round(displayTemp(thresholds.cltWarn));
        if (cltDangerInput) cltDangerInput.value = Math.round(displayTemp(thresholds.cltDanger));
        if (iatWarnInput) iatWarnInput.value = Math.round(displayTemp(thresholds.iatWarn));
        if (iatDangerInput) iatDangerInput.value = Math.round(displayTemp(thresholds.iatDanger));
    }

    // Unit conversions for display
    function convertPressure(kpa) {
        if (units.pressure === 'psi') return kpa * 0.14504;
        if (units.pressure === 'bar') return kpa * 0.01;
        return kpa;
    }

    function convertSpeed(kph) {
        return units.speed === 'mph' ? kph * 0.6214 : kph;
    }

    // ---- Canvas Setup ----
    const tachoCanvas = $('tachoCanvas');
    const tachoCtx = tachoCanvas.getContext('2d');
    const speedoCanvas = $('speedoCanvas');
    const speedoCtx = speedoCanvas.getContext('2d');

    function setupCanvases() {
        const dpr = window.devicePixelRatio || 1;
        
        const tachoRect = tachoCanvas.getBoundingClientRect();
        tachoCanvas.width = (tachoRect.width || 500) * dpr;
        tachoCanvas.height = (tachoRect.height || 260) * dpr;
        tachoCtx.scale(dpr, dpr);
        
        const speedoRect = speedoCanvas.getBoundingClientRect();
        speedoCanvas.width = (speedoRect.width || 340) * dpr;
        speedoCanvas.height = (speedoRect.height || 200) * dpr;
        speedoCtx.scale(dpr, dpr);
    }

    // ---- Draw Tachometer ----
    function drawTacho(rpm) {
        const w = tachoCanvas.width / (window.devicePixelRatio || 1);
        const h = tachoCanvas.height / (window.devicePixelRatio || 1);
        const cx = w / 2;
        const cy = h - 18;
        const radius = Math.min(w, h * 1.4) * 0.76;

        tachoCtx.clearRect(0, 0, w, h);

        const startAngle = Math.PI;
        const endAngle = 2 * Math.PI;
        const rpmMax = thresholds.rpmMax;

        // Background track
        tachoCtx.beginPath();
        tachoCtx.arc(cx, cy, radius, startAngle, endAngle);
        tachoCtx.strokeStyle = 'rgba(168, 85, 247, 0.1)';
        tachoCtx.lineWidth = 26;
        tachoCtx.lineCap = 'round';
        tachoCtx.stroke();

        // Warning zones
        const warnStart = startAngle + (thresholds.rpmWarn / rpmMax) * Math.PI;
        const dangerStart = startAngle + (thresholds.rpmDanger / rpmMax) * Math.PI;

        tachoCtx.beginPath();
        tachoCtx.arc(cx, cy, radius, warnStart, dangerStart);
        tachoCtx.strokeStyle = 'rgba(251, 191, 36, 0.35)';
        tachoCtx.lineWidth = 26;
        tachoCtx.stroke();

        tachoCtx.beginPath();
        tachoCtx.arc(cx, cy, radius, dangerStart, endAngle);
        tachoCtx.strokeStyle = 'rgba(248, 113, 113, 0.5)';
        tachoCtx.lineWidth = 26;
        tachoCtx.stroke();

        // Fill arc
        const rpmFrac = Math.min(rpm / rpmMax, 1);
        const fillEnd = startAngle + rpmFrac * Math.PI;

        const gradient = tachoCtx.createLinearGradient(0, cy, w, cy);
        gradient.addColorStop(0, '#7e22ce');
        gradient.addColorStop(0.5, '#a855f7');
        gradient.addColorStop(0.75, '#c084fc');
        if (rpmFrac > thresholds.rpmWarn / rpmMax) gradient.addColorStop(0.85, '#fbbf24');
        if (rpmFrac > thresholds.rpmDanger / rpmMax) gradient.addColorStop(0.95, '#f87171');

        tachoCtx.beginPath();
        tachoCtx.arc(cx, cy, radius, startAngle, fillEnd);
        tachoCtx.strokeStyle = gradient;
        tachoCtx.lineWidth = 26;
        tachoCtx.lineCap = 'round';
        tachoCtx.stroke();

        // Glow at needle
        const glowColor = rpm > thresholds.rpmDanger ? '#f87171' :
                         rpm > thresholds.rpmWarn ? '#fbbf24' : '#a855f7';
        
        tachoCtx.save();
        tachoCtx.shadowColor = glowColor;
        tachoCtx.shadowBlur = 25;
        tachoCtx.beginPath();
        tachoCtx.arc(cx, cy, radius, Math.max(startAngle, fillEnd - 0.1), fillEnd);
        tachoCtx.strokeStyle = glowColor;
        tachoCtx.lineWidth = 8;
        tachoCtx.stroke();
        tachoCtx.restore();

        // Ticks
        for (let i = 0; i <= rpmMax; i += 500) {
            const angle = startAngle + (i / rpmMax) * Math.PI;
            const isMajor = i % 1000 === 0;
            const innerR = radius - (isMajor ? 38 : 30);
            const outerR = radius - 12;

            tachoCtx.beginPath();
            tachoCtx.moveTo(cx + innerR * Math.cos(angle), cy + innerR * Math.sin(angle));
            tachoCtx.lineTo(cx + outerR * Math.cos(angle), cy + outerR * Math.sin(angle));
            
            if (i >= thresholds.rpmDanger) tachoCtx.strokeStyle = 'rgba(248, 113, 113, 0.9)';
            else if (i >= thresholds.rpmWarn) tachoCtx.strokeStyle = 'rgba(251, 191, 36, 0.8)';
            else tachoCtx.strokeStyle = 'rgba(196, 181, 253, 0.5)';
            
            tachoCtx.lineWidth = isMajor ? 3 : 1.5;
            tachoCtx.stroke();

            if (isMajor) {
                const labelR = radius - 52;
                tachoCtx.font = 'bold 15px Arial, sans-serif';
                tachoCtx.fillStyle = i >= thresholds.rpmWarn ? 'rgba(248, 113, 113, 0.9)' : 'rgba(196, 181, 253, 0.9)';
                tachoCtx.textAlign = 'center';
                tachoCtx.textBaseline = 'middle';
                tachoCtx.fillText((i / 1000).toString(), cx + labelR * Math.cos(angle), cy + labelR * Math.sin(angle));
            }
        }
    }

    // ---- Draw Speedometer ----
    function drawSpeedo(speed) {
        const w = speedoCanvas.width / (window.devicePixelRatio || 1);
        const h = speedoCanvas.height / (window.devicePixelRatio || 1);
        const cx = w / 2;
        const cy = h - 14;
        const radius = Math.min(w, h * 1.5) * 0.73;

        speedoCtx.clearRect(0, 0, w, h);

        const startAngle = Math.PI * 0.75;
        const endAngle = Math.PI * 2.25;
        const maxSpeed = units.speed === 'mph' ? 160 : 260;

        // Background
        speedoCtx.beginPath();
        speedoCtx.arc(cx, cy, radius, startAngle, endAngle);
        speedoCtx.strokeStyle = 'rgba(34, 211, 238, 0.1)';
        speedoCtx.lineWidth = 20;
        speedoCtx.lineCap = 'round';
        speedoCtx.stroke();

        // Fill
        const speedFrac = Math.min(speed / maxSpeed, 1);
        const fillEnd = startAngle + speedFrac * (endAngle - startAngle);

        const gradient = speedoCtx.createLinearGradient(0, cy, w, cy);
        gradient.addColorStop(0, '#0891b2');
        gradient.addColorStop(0.5, '#22d3ee');
        gradient.addColorStop(1, '#67e8f9');

        speedoCtx.beginPath();
        speedoCtx.arc(cx, cy, radius, startAngle, fillEnd);
        speedoCtx.strokeStyle = gradient;
        speedoCtx.lineWidth = 20;
        speedoCtx.lineCap = 'round';
        speedoCtx.stroke();

        // Glow
        speedoCtx.save();
        speedoCtx.shadowColor = '#22d3ee';
        speedoCtx.shadowBlur = 20;
        speedoCtx.beginPath();
        speedoCtx.arc(cx, cy, radius, Math.max(startAngle, fillEnd - 0.08), fillEnd);
        speedoCtx.strokeStyle = '#22d3ee';
        speedoCtx.lineWidth = 6;
        speedoCtx.stroke();
        speedoCtx.restore();

        // Ticks
        const step = units.speed === 'mph' ? 20 : 20;
        for (let i = 0; i <= maxSpeed; i += step) {
            const angle = startAngle + (i / maxSpeed) * (endAngle - startAngle);
            const isMajor = i % (step * 2) === 0;
            const innerR = radius - (isMajor ? 28 : 22);
            const outerR = radius - 8;

            speedoCtx.beginPath();
            speedoCtx.moveTo(cx + innerR * Math.cos(angle), cy + innerR * Math.sin(angle));
            speedoCtx.lineTo(cx + outerR * Math.cos(angle), cy + outerR * Math.sin(angle));
            speedoCtx.strokeStyle = 'rgba(34, 211, 238, 0.6)';
            speedoCtx.lineWidth = isMajor ? 3 : 1.5;
            speedoCtx.stroke();

            if (isMajor) {
                const labelR = radius - 42;
                speedoCtx.font = 'bold 13px Arial, sans-serif';
                speedoCtx.fillStyle = 'rgba(34, 211, 238, 0.9)';
                speedoCtx.textAlign = 'center';
                speedoCtx.textBaseline = 'middle';
                speedoCtx.fillText(i.toString(), cx + labelR * Math.cos(angle), cy + labelR * Math.sin(angle));
            }
        }
    }

    // ---- Warning System ----
    function showWarning(text, priority) {
        // Only show if higher priority or no current warning
        const priorities = { 'critical': 3, 'danger': 2, 'warning': 1 };
        const currentPriority = currentWarning ? priorities[currentWarning.priority] || 0 : 0;
        const newPriority = priorities[priority] || 0;
        
        if (newPriority >= currentPriority) {
            currentWarning = { text, priority };
            const overlay = $('warningOverlay');
            const warningText = $('warningText');
            warningText.textContent = text;
            overlay.classList.add('active');
            
            // Auto-clear warning after 3 seconds unless it's critical
            if (warningTimer) clearTimeout(warningTimer);
            if (priority !== 'critical') {
                warningTimer = setTimeout(() => {
                    overlay.classList.remove('active');
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

    // ---- Main Update Loop ----
    function updateDisplay() {
        if (!lastFrame) {
            requestAnimationFrame(updateDisplay);
            return;
        }

        const ecu = lastFrame.ecu;
        const gpsData = lastFrame.gps;
        const speedData = lastFrame.speed;

        // Speed
        const rawSpeed = speedData ? speedData.value : 0;
        smoothSpeed += (rawSpeed - smoothSpeed) * 0.3;
        $('speedValue').textContent = Math.round(convertSpeed(smoothSpeed));
        drawSpeedo(smoothSpeed);

        // GPS status
        if (gpsData) {
            setStatus('statusGPS', gpsData.valid ? 'connected' : '');
        }

        if (ecu) {
            // RPM
            smoothRPM += (ecu.rpm - smoothRPM) * 0.3;
            const rpmInt = Math.round(smoothRPM);
            $('rpmValue').textContent = rpmInt;
            
            const rpmEl = $('rpmValue');
            rpmEl.className = 'rpm-value' + 
                (rpmInt >= thresholds.rpmDanger ? ' danger' : 
                 rpmInt >= thresholds.rpmWarn ? ' warn' : '');
            drawTacho(smoothRPM);

            // Engine status
            const statusEl = $('engineStatus');
            if (ecu.coolant >= thresholds.cltDanger || ecu.iat >= thresholds.iatDanger) {
                statusEl.textContent = 'HOT';
                statusEl.className = 'engine-status warning';
            } else if (ecu.running) {
                statusEl.textContent = 'RUN';
                statusEl.className = 'engine-status running';
            } else {
                statusEl.textContent = 'OFF';
                statusEl.className = 'engine-status';
            }

            // Gear
            $('gearDisplay').textContent = ecu.gear === 0 ? 'N' : ecu.gear;

            // AFR
            $('afrValue').textContent = ecu.afr.toFixed(1);
            const afrPos = Math.max(0, Math.min(1, (ecu.afr - 10) / 8));
            $('afrMarker').style.left = (afrPos * 100) + '%';
            setCardState('afrCard', (ecu.afr > 16 || ecu.afr < 11) ? 'danger' : 
                                    (ecu.afr > 15 || ecu.afr < 12) ? 'warn' : '');

            // Boost
            const boostKpa = ecu.map > 100 ? ecu.map - 100 : 0;
            const boostVal = convertPressure(boostKpa);
            $('boostValue').textContent = boostVal.toFixed(units.pressure === 'psi' ? 1 : 0);
            const boostPct = Math.min(boostKpa / 150 * 100, 100);
            $('boostBar').style.width = boostPct + '%';

            // Oil Pressure
            const oilVal = units.pressure === 'psi' ? ecu.oilPressure : convertPressure(ecu.oilPressure * 6.895);
            $('oilValue').textContent = Math.round(oilVal);
            const oilPct = Math.min(ecu.oilPressure / 80 * 100, 100);
            $('oilBar').style.width = oilPct + '%';
            setCardState('oilCard', ecu.oilPressure < thresholds.oilMin && ecu.running ? 'danger' : '');

            // Coolant - ALWAYS use Celsius for threshold comparison
            const cltC = ecu.coolant;
            const cltDisplay = displayTemp(cltC);
            $('cltValue').textContent = Math.round(cltDisplay);
            const cltPct = Math.max(0, Math.min((cltC - 40) / 80 * 100, 100));
            $('cltBar').style.width = cltPct + '%';
            
            const cltDanger = cltC >= thresholds.cltDanger;
            const cltWarn = cltC >= thresholds.cltWarn;
            setCardState('cltCard', cltDanger ? 'danger' : cltWarn ? 'warn' : '');

            // Intake Air Temp - ALWAYS use Celsius for threshold comparison
            const iatC = ecu.iat;
            const iatDisplay = displayTemp(iatC);
            $('iatValue').textContent = Math.round(iatDisplay);
            const iatPct = Math.max(0, Math.min((iatC + 10) / 70 * 100, 100));
            $('iatBar').style.width = iatPct + '%';
            
            const iatDanger = iatC >= thresholds.iatDanger;
            const iatWarn = iatC >= thresholds.iatWarn;
            setCardState('iatCard', iatDanger ? 'danger' : iatWarn ? 'warn' : '');

            // Knock
            const knockCnt = ecu.knockCount || 0;
            const knockRet = ecu.knockCor || 0;
            
            $('knockCount').textContent = knockCnt;
            $('knockRetard').textContent = knockRet + '°';
            
            const knockInd = $('knockIndicator');
            if (knockRet > 0 || knockCnt > 0) {
                knockInd.classList.add('active');
            } else {
                knockInd.classList.remove('active');
            }
            
            const knockCntEl = $('knockCount');
            const knockRetEl = $('knockRetard');
            
            if (knockRet >= thresholds.knockWarn) {
                knockCntEl.className = 'knock-value danger';
                knockRetEl.className = 'knock-value danger';
                setCardState('knockCard', 'danger');
            } else if (knockRet > 0) {
                knockCntEl.className = 'knock-value warn';
                knockRetEl.className = 'knock-value warn';
                setCardState('knockCard', 'warn');
            } else {
                knockCntEl.className = 'knock-value';
                knockRetEl.className = 'knock-value';
                setCardState('knockCard', '');
            }
            
            const knockPct = Math.min(knockRet / 10 * 100, 100);
            $('knockBar').style.width = knockPct + '%';

            // ECU status
            setStatus('statusSync', ecu.sync ? 'connected' : '');

            // ---- WARNING SYSTEM ----
            // Check all warning conditions and show most critical
            let warningText = '';
            let warningPriority = '';
            
            // Critical: Coolant over temp
            if (cltC >= thresholds.cltDanger) {
                warningText = 'COOLANT ' + formatTemp(cltC);
                warningPriority = 'critical';
            }
            // Critical: Intake air over temp
            else if (iatC >= thresholds.iatDanger) {
                warningText = 'INTAKE HOT ' + formatTemp(iatC);
                warningPriority = 'critical';
            }
            // Danger: Low oil pressure
            else if (ecu.oilPressure < thresholds.oilMin && ecu.running && ecu.rpm > 1000) {
                warningText = 'LOW OIL ' + Math.round(units.pressure === 'psi' ? ecu.oilPressure : convertPressure(ecu.oilPressure * 6.895)) + (units.pressure === 'psi' ? ' PSI' : units.pressure === 'bar' ? ' BAR' : ' KPA');
                warningPriority = 'critical';
            }
            // Danger: Knock
            else if (knockRet >= thresholds.knockWarn) {
                warningText = 'KNOCK -' + knockRet + '°';
                warningPriority = 'critical';
            }
            // Warning: Coolant warm
            else if (cltC >= thresholds.cltWarn) {
                warningText = 'COOLANT ' + formatTemp(cltC);
                warningPriority = 'warning';
            }
            // Warning: Intake air warm
            else if (iatC >= thresholds.iatWarn) {
                warningText = 'INTAKE ' + formatTemp(iatC);
                warningPriority = 'warning';
            }
            // Warning: AFR
            else if (ecu.afr > 16.5 || ecu.afr < 10.5) {
                warningText = 'AFR ' + ecu.afr.toFixed(1);
                warningPriority = 'danger';
            }
            // Warning: Battery
            else if (ecu.batteryVoltage < thresholds.battLow) {
                warningText = 'LOW BATT ' + ecu.batteryVoltage.toFixed(1) + 'V';
                warningPriority = 'warning';
            }
            else if (ecu.batteryVoltage > thresholds.battHigh) {
                warningText = 'HIGH BATT ' + ecu.batteryVoltage.toFixed(1) + 'V';
                warningPriority = 'warning';
            }

            if (warningText) {
                showWarning(warningText, warningPriority);
            } else {
                clearWarning();
            }
        } else {
            setStatus('statusSync', '');
            drawTacho(0);
            $('rpmValue').textContent = '0';
            $('engineStatus').textContent = 'OFF';
            $('engineStatus').className = 'engine-status';
            $('knockIndicator').classList.remove('active');
            clearWarning();
        }

        requestAnimationFrame(updateDisplay);
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

    // ---- Config Modal ----
    $('btnConfig').addEventListener('click', () => {
        $('configModal').classList.add('active');
        loadConfigUI();
    });

    $('btnConfigClose').addEventListener('click', () => {
        $('configModal').classList.remove('active');
    });

    // Unit change handlers - convert threshold values
    $('cfgTempUnit').addEventListener('change', function() {
        units.temperature = this.value;
        updateThresholdInputs();
    });

    $('btnConfigSave').addEventListener('click', saveConfig);

    function loadConfigUI() {
        fetch('/api/config')
            .then(r => r.json())
            .then(cfg => {
                // Units
                const tempUnit = cfg.display?.units?.temperature || 'C';
                $('cfgTempUnit').value = tempUnit;
                units.temperature = tempUnit;
                
                $('cfgPressureUnit').value = cfg.display?.units?.pressure || 'psi';
                $('cfgSpeedUnit').value = cfg.display?.units?.speed || 'kph';
                
                // Thresholds - store raw Celsius values
                thresholds = { ...thresholds, ...cfg.display?.thresholds };
                
                // Update inputs with converted values
                updateThresholdInputs();
                
                $('cfgRpmWarn').value = thresholds.rpmWarn;
                $('cfgRpmDanger').value = thresholds.rpmDanger;
                $('cfgOilWarn').value = thresholds.oilMin;
                $('cfgKnockWarn').value = thresholds.knockWarn;
                $('cfgBattLow').value = thresholds.battLow;
                $('cfgBattHigh').value = thresholds.battHigh;
                
                $('cfgEcuType').value = cfg.ecu?.type || 'demo';
                $('cfgGpsType').value = cfg.gps?.type || 'demo';
                
                updateUnitLabels();
            })
            .catch(() => {});
    }

    function saveConfig() {
        // Convert temperature thresholds back to Celsius for storage
        const tempUnit = $('cfgTempUnit').value;
        const cltWarnVal = parseFloat($('cfgCltWarn').value);
        const cltDangerVal = parseFloat($('cfgCltDanger').value);
        const iatWarnVal = parseFloat($('cfgIatWarn').value);
        const iatDangerVal = parseFloat($('cfgIatDanger').value);
        
        const cfg = {
            ecu: { type: $('cfgEcuType').value },
            gps: { type: $('cfgGpsType').value },
            display: {
                units: {
                    pressure: $('cfgPressureUnit').value,
                    speed: $('cfgSpeedUnit').value,
                    temperature: tempUnit,
                },
                thresholds: {
                    rpmWarn: parseInt($('cfgRpmWarn').value),
                    rpmDanger: parseInt($('cfgRpmDanger').value),
                    cltWarn: tempUnit === 'F' ? toCelsius(cltWarnVal) : cltWarnVal,
                    cltDanger: tempUnit === 'F' ? toCelsius(cltDangerVal) : cltDangerVal,
                    iatWarn: tempUnit === 'F' ? toCelsius(iatWarnVal) : iatWarnVal,
                    iatDanger: tempUnit === 'F' ? toCelsius(iatDangerVal) : iatDangerVal,
                    oilPWarn: parseInt($('cfgOilWarn').value),
                    knockWarn: parseInt($('cfgKnockWarn').value),
                    battLow: parseFloat($('cfgBattLow').value),
                    battHigh: parseFloat($('cfgBattHigh').value),
                },
            },
        };

        fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(cfg),
        })
        .then(() => { $('configModal').classList.remove('active'); })
        .catch(() => {});
    }

    // ---- Init ----
    window.addEventListener('load', () => {
        setupCanvases();
        window.addEventListener('resize', setupCanvases);
        connect();
        requestAnimationFrame(updateDisplay);
    });
})();
