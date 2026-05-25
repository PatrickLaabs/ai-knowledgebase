// admin.js — reindex panel, progress polling, topbar badge
    // ── Admin / Re-index ──────────────────────────────────────────────────────────
    let adminPollTimer = null;

    function startAdminPolling() {
        if (adminPollTimer) return; // already polling
        adminPollTimer = setInterval(async () => {
            const st = await loadAdminStatus();
            // Stop polling once the job is no longer running
            if (st && st.status !== 'running') {
                clearInterval(adminPollTimer);
                adminPollTimer = null;
            }
        }, 2000);
    }

    async function loadAdminStatus() {
        try {
            const r = await fetch('/api/admin/reindex/status');
            if (!r.ok) return null;
            const st = await r.json();
            renderAdminStatus(st);
            updateReindexBadge(st);
            return st;
        } catch { return null; }
    }

    function renderAdminStatus(st) {
        const wrap  = document.getElementById('reindex-progress-wrap');
        const fill  = document.getElementById('reindex-progress-fill');
        const label = document.getElementById('reindex-progress-label');
        const stats = document.getElementById('admin-stats');
        const errEl = document.getElementById('admin-error');
        const btn   = document.getElementById('btn-reindex');

        // Embed model label (best-effort from env — backend could expose this)
        // For now, reflect whatever the Go default/env is via a meta tag we inject
        const modelMeta = document.querySelector('meta[name="embed-model"]');
        if (modelMeta) document.getElementById('admin-embed-model').textContent = modelMeta.content;

        if (st.status === 'idle') {
            wrap.style.display = 'none';
            stats.style.display = 'none';
            errEl.style.display = 'none';
            btn.disabled = false;
            btn.textContent = 'Start';
            return;
        }

        // Show progress bar
        wrap.style.display = 'flex';
        stats.style.display = 'grid';
        errEl.style.display = 'none';

        const pct = st.total > 0
            ? Math.round((st.completed + st.failed) / st.total * 100)
            : 0;

        if (st.status === 'running') {
            btn.disabled = true;
            btn.textContent = 'Running…';
            if (st.total > 0) {
                fill.style.width = pct + '%';
                fill.classList.remove('indeterminate');
                label.textContent = `${st.completed + st.failed} / ${st.total} notes · ${pct}%` +
                    (st.failed > 0 ? ` · ${st.failed} failed` : '');
            } else {
                fill.style.width = '0%';
                fill.classList.add('indeterminate');
                label.textContent = 'Counting notes…';
            }
        } else if (st.status === 'done') {
            btn.disabled = false;
            btn.textContent = 'Re-run';
            fill.classList.remove('indeterminate');
            fill.style.width = '100%';
            label.textContent = `Done · ${st.completed} embedded` +
                (st.failed > 0 ? `, ${st.failed} failed` : '');
        } else if (st.status === 'error') {
            btn.disabled = false;
            btn.textContent = 'Retry';
            fill.classList.remove('indeterminate');
            fill.style.width = '0%';
            label.textContent = 'Job failed';
            errEl.textContent = '⚠ ' + (st.error || 'Unknown error');
            errEl.style.display = 'block';
        }

        // Stat grid
        const fmtTime = iso => iso ? new Date(iso).toLocaleTimeString() : '—';
        document.getElementById('stat-status').textContent    = st.status;
        document.getElementById('stat-completed').textContent = st.completed ?? '—';
        document.getElementById('stat-failed').textContent    = st.failed ?? '—';
        document.getElementById('stat-total').textContent     = st.total || '—';
        document.getElementById('stat-started').textContent   = fmtTime(st.started_at);
        document.getElementById('stat-finished').textContent  = fmtTime(st.finished_at);
    }

    // Shows/hides the amber "indexing…" badge in the topbar when running off-tab
    function updateReindexBadge(st) {
        document.getElementById('reindex-badge').style.display =
            st.status === 'running' ? 'flex' : 'none';
    }

    async function startReindex() {
        const btn = document.getElementById('btn-reindex');
        btn.disabled = true;
        btn.textContent = 'Starting…';
        try {
            const r = await fetch('/api/admin/reindex', { method: 'POST' });
            if (r.status === 409) {
                toast('Re-index already running', 'error');
                btn.disabled = false;
                btn.textContent = 'Running…';
                return;
            }
            if (!r.ok) throw new Error('Server error ' + r.status);
            toast('Re-index started');
            const st = await r.json();
            renderAdminStatus(st);
            updateReindexBadge(st);
            startAdminPolling();
        } catch (err) {
            toast('Failed to start: ' + err.message, 'error');
            btn.disabled = false;
            btn.textContent = 'Start';
        }
    }

