// ================================================================
// SPEEDUINO DASH — Shared Module
// WebSocket, state, config, unit conversions, gear detection, HP
// Used by both dashboard and settings pages
// ================================================================

window.SpeeduinoDash = (function () {
    'use strict';

    // ---- State ----
    let ws = null;
    let reconnectTimer = null;
    let lastFrame = null;

    let thresholds = {
        rpmWarn: 6000, rpmDanger: 7000, rpmMax: 8000,
        oilMin: 15,
        cltWarn: 95, cltDanger: 105,
        iatWarn: 60, iatDanger: 75,
        knockWarn: 3,
        battLow: 12.0, battHigh: 15.5,
    };

    let units = { pressure: 'psi', speed: 'kph', temperature: 'C' };

    let drivetrain = {
        gearRatios: [], finalDrive: 3.73,
        tireCircumM: 1.95, gearTolerance: 0.15,
    };
    let showGear = true;

    let vehicle = {
        massKg: 1200, dragCoeff: 0.32,
        frontalAreaM2: 2.2, rollingResist: 0.012,
    };

    // HP estimation state
    let prevSpeedMs = 0, prevStamp = 0, smoothHP = 0, peakHP = 0;

    // Callbacks — pages register these
    let onFrame = null;
    let onConfig = null;

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
                if (frame.config) {
                    applyConfig(frame.config);
                    if (onConfig) onConfig(frame.config);
                }
                if (frame.ecu || frame.gps || frame.speed) lastFrame = frame;
                if (onFrame) onFrame(frame);
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

    // ---- Config ----
    function applyConfig(cfg) {
        if (cfg.units) units = { ...units, ...cfg.units };
        if (cfg.thresholds) thresholds = { ...thresholds, ...cfg.thresholds };
        if (cfg.drivetrain) {
            drivetrain = { ...drivetrain, ...cfg.drivetrain };
            if (cfg.drivetrain.showGear !== undefined) showGear = cfg.drivetrain.showGear;
        }
        if (cfg.vehicle) vehicle = { ...vehicle, ...cfg.vehicle };
    }

    // ---- Temperature Helpers ----
    function toFahrenheit(c) { return (c * 1.8) + 32; }
    function toCelsius(f) { return (f - 32) / 1.8; }
    function displayTemp(c) { return units.temperature === 'F' ? toFahrenheit(c) : c; }
    function formatTemp(c, decimals) {
        decimals = decimals || 0;
        return displayTemp(c).toFixed(decimals) + (units.temperature === 'F' ? '°F' : '°C');
    }

    // ---- Unit Conversions ----
    function convertPressure(kpa) {
        if (units.pressure === 'psi') return kpa * 0.14504;
        if (units.pressure === 'bar') return kpa * 0.01;
        return kpa;
    }
    function convertSpeed(kph) { return units.speed === 'mph' ? kph * 0.6214 : kph; }
    function convertDistance(km) { return units.speed === 'mph' ? km * 0.6214 : km; }

    // ---- Gear Detection ----
    function calcOverallRatio(rpm, speedKph) {
        if (speedKph < 5 || rpm < 500) return 0;
        const speedMs = speedKph / 3.6;
        const wheelRPM = (speedMs / drivetrain.tireCircumM) * 60;
        return rpm / wheelRPM;
    }

    function detectGear(rpm, speedKph) {
        if (!drivetrain.gearRatios || drivetrain.gearRatios.length === 0) return null;
        const actualRatio = calcOverallRatio(rpm, speedKph);
        if (actualRatio === 0) return 0;
        let bestGear = 0, bestError = Infinity;
        const tolerance = drivetrain.gearTolerance || 0.15;
        for (let i = 0; i < drivetrain.gearRatios.length; i++) {
            const expectedRatio = drivetrain.gearRatios[i] * drivetrain.finalDrive;
            const error = Math.abs(actualRatio - expectedRatio) / expectedRatio;
            if (error < bestError && error < tolerance) { bestError = error; bestGear = i + 1; }
        }
        return bestGear;
    }

    // ---- HP Estimation ----
    const AIR_DENSITY = 1.225;
    const GRAVITY = 9.81;

    function calcEstimatedHP(speedKph, stamp) {
        const speedMs = speedKph / 3.6;
        if (prevStamp === 0) { prevSpeedMs = speedMs; prevStamp = stamp; return 0; }
        const dt = (stamp - prevStamp) / 1000;
        if (dt <= 0 || dt > 2) { prevSpeedMs = speedMs; prevStamp = stamp; return smoothHP; }
        const accel = (speedMs - prevSpeedMs) / dt;
        prevSpeedMs = speedMs; prevStamp = stamp;
        const fAccel = vehicle.massKg * accel;
        const fDrag = 0.5 * AIR_DENSITY * vehicle.dragCoeff * vehicle.frontalAreaM2 * speedMs * speedMs;
        const fRolling = vehicle.rollingResist * vehicle.massKg * GRAVITY;
        const powerW = (fAccel + fDrag + fRolling) * speedMs;
        const rawHP = Math.max(0, powerW / 745.7);
        smoothHP += (rawHP - smoothHP) * 0.15;
        if (smoothHP > peakHP) peakHP = smoothHP;
        return smoothHP;
    }

    function resetPeakHP() { peakHP = 0; smoothHP = 0; }

    // ---- Public API ----
    return {
        connect,
        get lastFrame() { return lastFrame; },
        get thresholds() { return thresholds; },
        get units() { return units; },
        get drivetrain() { return drivetrain; },
        get showGear() { return showGear; },
        get vehicle() { return vehicle; },
        get peakHP() { return peakHP; },
        set onFrame(fn) { onFrame = fn; },
        set onConfig(fn) { onConfig = fn; },
        applyConfig,
        toFahrenheit, toCelsius, displayTemp, formatTemp,
        convertPressure, convertSpeed, convertDistance,
        calcOverallRatio, detectGear,
        calcEstimatedHP, resetPeakHP,
    };
})();
