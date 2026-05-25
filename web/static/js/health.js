// health.js — topbar connection status dot

async function checkHealth() {
    const dot = document.getElementById('status-dot');
    const lbl = document.getElementById('status-label');
    try {
        const r = await fetch('/api/health');
        dot.className = r.ok ? 'status-dot ok' : 'status-dot err';
        lbl.textContent = r.ok ? 'connected' : 'error';
    } catch {
        dot.className = 'status-dot err';
        lbl.textContent = 'offline';
    }
}
