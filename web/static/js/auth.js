// auth.js — login/register UI, session bootstrap, logout

// ── Bootstrap ─────────────────────────────────────────────────────────────────
// Called once by state.js init(). Checks /api/auth/me and either shows the
// login screen or the main app, setting state.currentUser on success.
async function bootstrapAuth() {
    try {
        const r = await fetch('/api/auth/me');
        if (r.ok) {
            const user = await r.json();
            onAuthSuccess(user);
            return true;
        }
    } catch { /* network error — fall through to login */ }
    showLogin();
    return false;
}

function onAuthSuccess(user) {
    state.currentUser = user;
    document.getElementById('login-overlay').classList.add('hidden');
    document.getElementById('topbar-user').textContent = user.username;
    document.getElementById('topbar-user-wrap').style.display = 'flex';
    // Show/hide register tab based on server config
    const regTab = document.getElementById('login-tab-register');
    if (regTab) regTab.style.display = user.allow_registration ? '' : 'none';
}

function showLogin() {
    document.getElementById('login-overlay').classList.remove('hidden');
    document.getElementById('topbar-user-wrap').style.display = 'none';
}

// ── Tab switching (login / register) ──────────────────────────────────────────
function switchAuthTab(tab) {
    document.querySelectorAll('.login-tab').forEach(t =>
        t.classList.toggle('active', t.dataset.tab === tab)
    );
    document.getElementById('login-form').style.display  = tab === 'login'    ? '' : 'none';
    document.getElementById('register-form').style.display = tab === 'register' ? '' : 'none';
    clearAuthErrors();
}

function clearAuthErrors() {
    document.getElementById('login-error').textContent = '';
    document.getElementById('register-error').textContent = '';
}

// ── Login ─────────────────────────────────────────────────────────────────────
async function submitLogin(event) {
    // Stop the browser's native form submit — otherwise it fires a GET request
    // with username/password in the URL, which leaks them into server logs.
    if (event) event.preventDefault();

    const btn      = document.getElementById('login-btn');
    const errEl    = document.getElementById('login-error');
    const username = document.getElementById('login-username').value.trim();
    const password = document.getElementById('login-password').value;

    if (!username || !password) { errEl.textContent = 'Fill in both fields.'; return; }

    btn.disabled = true;
    btn.textContent = 'Signing in…';
    errEl.textContent = '';

    try {
        const r = await fetch('/api/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password }),
        });
        if (r.ok) {
            const user = await r.json();
            onAuthSuccess(user);
            // Kick off app data load now that we're authenticated.
            await Promise.all([loadNotes(), loadTags()]);
        } else {
            const msg = await r.text();
            errEl.textContent = msg || 'Invalid credentials.';
        }
    } catch {
        errEl.textContent = 'Network error — try again.';
    } finally {
        btn.disabled = false;
        btn.textContent = 'Sign in';
    }
}

// ── Register ──────────────────────────────────────────────────────────────────
async function submitRegister(event) {
    if (event) event.preventDefault();

    const btn      = document.getElementById('register-btn');
    const errEl    = document.getElementById('register-error');
    const username = document.getElementById('register-username').value.trim();
    const password = document.getElementById('register-password').value;
    const confirm  = document.getElementById('register-confirm').value;

    if (!username || !password) { errEl.textContent = 'Fill in all fields.'; return; }
    if (password !== confirm)   { errEl.textContent = 'Passwords do not match.'; return; }
    if (password.length < 8)    { errEl.textContent = 'Password must be at least 8 characters.'; return; }

    btn.disabled = true;
    btn.textContent = 'Creating account…';
    errEl.textContent = '';

    try {
        const r = await fetch('/api/auth/register', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password }),
        });
        if (r.ok) {
            const user = await r.json();
            onAuthSuccess(user);
            await Promise.all([loadNotes(), loadTags()]);
        } else {
            errEl.textContent = await r.text() || 'Registration failed.';
        }
    } catch {
        errEl.textContent = 'Network error — try again.';
    } finally {
        btn.disabled = false;
        btn.textContent = 'Create account';
    }
}

// ── Logout ────────────────────────────────────────────────────────────────────
async function logout() {
    await fetch('/api/auth/logout', { method: 'POST' });
    state.currentUser = null;
    state.notes = [];
    state.tags  = [];
    state.selectedId = null;
    // Clear notes list so the next user doesn't see stale data.
    document.getElementById('notes-list').innerHTML = '';
    document.getElementById('tag-filter-list').innerHTML = '';
    showLogin();
    switchAuthTab('login');
}

// Note: Enter-to-submit is handled natively by the <form onsubmit> elements,
// so no manual keydown listener is needed here.