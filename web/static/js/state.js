// state.js — shared application state and bootstrap

const state = {
    notes: [], tags: [],
    selectedId: null,
    activeTag: '',
    currentTags: [],
    isStreaming: false,
    editorMode: 'write',   // 'write' | 'preview' (mobile only)
    currentUser: null,     // { id, username } — set after auth
};

async function init() {
    setupKeyboard();

    // Auth must resolve before we touch any protected API.
    const authed = await bootstrapAuth();
    if (!authed) return; // login screen is now showing, stop here

    await Promise.all([loadNotes(), loadTags()]);
    checkHealth();
    setInterval(checkHealth, 30_000);
    const st = await loadAdminStatus();
    if (st && st.status === 'running') startAdminPolling();
    setInterval(loadAdminStatus, 10_000);
}