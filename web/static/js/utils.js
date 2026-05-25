// utils.js — view switching, sidebar, keyboard, clipboard, toast

// ── Utilities ─────────────────────────────────────────────────────────────────
function esc(s) {
    return String(s ?? '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
let toastTimer;
function toast(msg, type='') {
    const el = document.getElementById('toast');
    el.textContent = msg; el.className = `toast ${type} visible`;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.remove('visible'), 3000);
}

// ── Copy code button ──────────────────────────────────────────────────────────
function copyCode(btn) {
    const codeBlock = btn.nextElementSibling.querySelector('code');
    if (!codeBlock) return;
    navigator.clipboard.writeText(codeBlock.textContent).then(() => {
        const originalText = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = originalText; }, 2000);
    }).catch(() => toast('Failed to copy code.', 'error'));
}

// ── View switching ────────────────────────────────────────────────────────────
const VIEWS = ['notes', 'chat', 'admin'];
function switchView(name) {
    document.querySelectorAll('.view').forEach(v => v.classList.remove('active'));
    document.getElementById('view-' + name).classList.add('active');
    document.querySelectorAll('.tab').forEach((t, i) => {
        t.classList.toggle('active', VIEWS[i] === name);
    });
    VIEWS.forEach(v => {
        const el = document.getElementById('btab-' + v);
        if (el) el.classList.toggle('active', v === name);
    });
    if (name === 'admin') {
        loadAdminStatus();
        startAdminPolling();
    }
}

// ── Sidebar (mobile) ──────────────────────────────────────────────────────────
function toggleSidebar() {
    const open = document.getElementById('sidebar').classList.toggle('open');
    document.getElementById('sidebar-backdrop').classList.toggle('visible', open);
}
function closeSidebar() {
    document.getElementById('sidebar').classList.remove('open');
    document.getElementById('sidebar-backdrop').classList.remove('visible');
}

// ── Keyboard shortcuts ────────────────────────────────────────────────────────
function setupKeyboard() {
    document.addEventListener('keydown', e => {
        // Cmd/Ctrl+N — new note
        if ((e.metaKey || e.ctrlKey) && e.key === 'n') {
            e.preventDefault();
            newNote();
        }
        // Escape — close sidebar on mobile
        if (e.key === 'Escape') closeSidebar();
    });
}
