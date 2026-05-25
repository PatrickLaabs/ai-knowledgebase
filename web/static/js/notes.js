// notes.js — note CRUD, rendering and tag list
    // ── Notes ─────────────────────────────────────────────────────────────────────
    async function loadNotes() {
        const params = new URLSearchParams();
        if (state.activeTag) params.set('tag', state.activeTag);
        const search = document.getElementById('search-input').value.trim();
        if (search) params.set('search', search);

        try {
            const r = await fetch('/api/notes?' + params);
            state.notes = await r.json();
        } catch { state.notes = []; }
        renderNoteList();
    }

    async function loadTags() {
        try {
            const r = await fetch('/api/tags');
            state.tags = await r.json();
        } catch { state.tags = []; }
        renderTagFilters();
    }

    function renderNoteList() {
        const list = document.getElementById('notes-list');
        document.getElementById('note-count').textContent = state.notes.length || '';

        if (!state.notes.length) {
            list.innerHTML = `<div class="empty-state">
      <div class="empty-icon">○</div>
      <div class="empty-title">No notes yet</div>
      <div class="empty-sub">Click "+ New note" to get started.</div>
    </div>`;
            return;
        }

        list.innerHTML = state.notes.map(n => {
            const active  = n.id === state.selectedId ? 'active' : '';
            const date    = new Date(n.updated_at).toLocaleDateString('en-GB', { day:'numeric', month:'short' });
            const tags    = (n.tags || []).slice(0,3).map(t => `<span class="note-tag">${esc(t)}</span>`).join('');
            const preview = stripMd(n.content).slice(0, 100);
            return `<div class="note-item ${active}" onclick="selectNote(${n.id})">
      <div class="note-preview">${esc(preview)}</div>
      <div class="note-meta"><span class="note-date">${date}</span>${tags}</div>
    </div>`;
        }).join('');
    }

    function renderTagFilters() {
        const container = document.getElementById('tag-filter-list');
        // Override the previous flex-wrap layout so the tree stacks vertically
        container.style.display = 'block';

        // 1. Build a nested tree object from the flat tags array
        const tree = {};
        state.tags.forEach(tagPath => {
            const parts = tagPath.split('/');
            let current = tree;
            let pathSoFar = '';

            parts.forEach((part) => {
                pathSoFar = pathSoFar ? pathSoFar + '/' + part : part;
                if (!current[part]) {
                    current[part] = { _full: pathSoFar, children: {} };
                }
                current = current[part].children;
            });
        });

        // 2. Recursive function to generate HTML for the tree
        function renderNode(key, node) {
            const hasChildren = Object.keys(node.children).length > 0;
            const isActive = state.activeTag === node._full;
            // Auto-expand if the active tag is this node, or a child of this node
            const isExpanded = state.activeTag.startsWith(node._full + '/') || isActive;

            let html = `<div class="tag-node">`;
            html += `<div class="tag-row ${isActive ? 'active' : ''}">`;

            if (hasChildren) {
                html += `<div class="tag-toggle ${isExpanded ? 'expanded' : ''}" onclick="toggleTagNode(event, this)">▶</div>`;
            } else {
                html += `<div style="width: 14px; flex-shrink: 0;"></div>`; // Spacer for alignment
            }

            html += `<div style="flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" onclick="filterByTag('${esc(node._full)}')">${esc(key)}</div>`;
            html += `</div>`;

            if (hasChildren) {
                html += `<div class="tag-children ${isExpanded ? 'expanded' : ''}">`;
                // Sort children alphabetically
                for (const childKey of Object.keys(node.children).sort()) {
                    html += renderNode(childKey, node.children[childKey]);
                }
                html += `</div>`;
            }

            html += `</div>`;
            return html;
        }

        // 3. Assemble the final HTML
        let html = `<div class="tag-tree">`;
        html += `<div class="tag-row ${state.activeTag === '' ? 'active' : ''}" onclick="filterByTag('')">
                    <div style="width:14px;"></div><div style="flex:1">all notes</div>
                 </div>`;

        for (const key of Object.keys(tree).sort()) {
            html += renderNode(key, tree[key]);
        }
        html += `</div>`;

        container.innerHTML = html;
    }

    // Helper to animate/toggle the collapse state
    window.toggleTagNode = function(e, el) {
        e.stopPropagation(); // Prevent clicking the chevron from triggering the filter
        el.classList.toggle('expanded');
        const children = el.closest('.tag-node').querySelector('.tag-children');
        if (children) children.classList.toggle('expanded');
    };

    function filterByTag(tag) {
        state.activeTag = tag;
        loadNotes();
        renderTagFilters();
    }

    function selectNote(id) {
        const n = state.notes.find(n => n.id === id);
        if (!n) return;
        state.selectedId = id;
        state.currentTags = [...(n.tags || [])];
        document.getElementById('note-content').value = n.content;
        document.getElementById('editor-state').textContent = `note #${id}`;
        document.getElementById('btn-delete').disabled = false;
        renderTagChips();
        updatePreview(n.content);
        renderNoteList();
        switchView('notes');
        closeSidebar();
    }

    function newNote() {
        state.selectedId = null;
        state.currentTags = [];
        document.getElementById('note-content').value = '';
        document.getElementById('editor-state').textContent = 'new note';
        document.getElementById('btn-delete').disabled = true;
        renderTagChips();
        updatePreview('');
        renderNoteList();
    }

    async function saveNote() {
        const content = document.getElementById('note-content').value.trim();
        if (!content) { toast('Content cannot be empty.', 'error'); return; }

        const btn = document.getElementById('btn-save');
        btn.disabled = true; btn.textContent = 'Saving…';

        const body = JSON.stringify({ content, tags: state.currentTags });
        try {
            let r;
            if (state.selectedId) {
                r = await fetch(`/api/notes/${state.selectedId}`, { method:'PUT', headers:{'Content-Type':'application/json'}, body });
                if (!r.ok) throw new Error('Update failed');
                toast('Note updated.', 'success');
            } else {
                r = await fetch('/api/notes', { method:'POST', headers:{'Content-Type':'application/json'}, body });
                if (!r.ok) throw new Error('Create failed');
                const d = await r.json();
                state.selectedId = d.id;
                document.getElementById('editor-state').textContent = `note #${d.id}`;
                document.getElementById('btn-delete').disabled = false;
                toast('Note saved.', 'success');
            }
            await Promise.all([loadNotes(), loadTags()]);
        } catch(e) {
            toast(e.message, 'error');
        } finally {
            btn.disabled = false; btn.textContent = 'Save';
        }
    }

    async function deleteNote() {
        if (!state.selectedId) return;
        if (!confirm(`Delete note #${state.selectedId}? This cannot be undone.`)) return;
        try {
            const r = await fetch(`/api/notes/${state.selectedId}`, { method:'DELETE' });
            if (!r.ok) throw new Error('Delete failed');
            toast('Note deleted.', 'success');
            newNote();
            await Promise.all([loadNotes(), loadTags()]);
        } catch(e) { toast(e.message, 'error'); }
    }

