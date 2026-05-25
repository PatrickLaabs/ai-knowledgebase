// state.js — shared application state and bootstrap

const state = {
    notes: [], tags: [],
    selectedId: null,
    activeTag: '',
    currentTags: [],
    isStreaming: false,
    editorMode: 'write', // 'write' | 'preview' (mobile only)
};

async function init() {
    setupKeyboard();
    await Promise.all([loadNotes(), loadTags()]);
    checkHealth();
    setInterval(checkHealth, 30_000);
    // Background poll: keeps topbar badge updated even when not on admin tab.
    const st = await loadAdminStatus();
    if (st && st.status === 'running') startAdminPolling();
    setInterval(loadAdminStatus, 10_000); // light background refresh
}
