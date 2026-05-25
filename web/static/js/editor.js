// editor.js — live preview, mobile tabs, markdown toolbar, tag chips, semantic search
    // ── Live preview ──────────────────────────────────────────────────────────────
    let previewTimer;
    function schedulePreview() {
        clearTimeout(previewTimer);
        previewTimer = setTimeout(() => updatePreview(document.getElementById('note-content').value), 120);
    }

    function updatePreview(md) {
        const empty   = document.getElementById('preview-empty');
        const content = document.getElementById('preview-content');
        if (!md || !md.trim()) {
            empty.style.display = '';
            content.style.display = 'none';
            return;
        }
        empty.style.display = 'none';
        content.style.display = '';
        content.innerHTML = renderMarkdown(md);
    }

    document.getElementById('note-content').addEventListener('input', function() {
        schedulePreview();
    });

    // ── Mobile editor mode tabs ───────────────────────────────────────────────────
    function setEditorMode(mode) {
        state.editorMode = mode;
        document.getElementById('mode-write').classList.toggle('active', mode === 'write');
        document.getElementById('mode-preview').classList.toggle('active', mode === 'preview');
        document.getElementById('write-pane').classList.toggle('hidden', mode !== 'write');
        document.getElementById('preview-pane').classList.toggle('hidden', mode !== 'preview');
        if (mode === 'preview') {
            updatePreview(document.getElementById('note-content').value);
        }
    }

    // ── Markdown toolbar helpers ──────────────────────────────────────────────────
    // Wrap selection (or placeholder) with before/after tokens.
    function md(before, after, placeholder) {
        const ta = document.getElementById('note-content');
        const s = ta.selectionStart, e = ta.selectionEnd;
        const sel = ta.value.slice(s, e) || placeholder;
        ta.setRangeText(before + sel + after, s, e, 'end');
        ta.focus();
        schedulePreview();
    }

    // Prepend before to the current line.
    function mdLine(before) {
        const ta = document.getElementById('note-content');
        const s = ta.selectionStart;
        const lineStart = ta.value.lastIndexOf('\n', s - 1) + 1;
        ta.setRangeText(before, lineStart, lineStart, 'end');
        ta.focus();
        schedulePreview();
    }

    // Insert a fenced block.
    function mdBlock(open, close) {
        const ta = document.getElementById('note-content');
        const s = ta.selectionStart, e = ta.selectionEnd;
        const sel = ta.value.slice(s, e) || 'code here';
        ta.setRangeText(open + sel + close, s, e, 'end');
        ta.focus();
        schedulePreview();
    }

    function mdHr() {
        const ta = document.getElementById('note-content');
        const s = ta.selectionStart;
        ta.setRangeText('\n---\n', s, s, 'end');
        ta.focus();
        schedulePreview();
    }

    function mdLink() {
        const ta = document.getElementById('note-content');
        const s = ta.selectionStart, e = ta.selectionEnd;
        const sel = ta.value.slice(s, e) || 'link text';
        ta.setRangeText(`[${sel}](url)`, s, e, 'end');
        ta.focus();
        schedulePreview();
    }

    // Keyboard shortcuts
    function setupKeyboard() {
        document.getElementById('note-content').addEventListener('keydown', e => {
            if ((e.ctrlKey || e.metaKey) && !e.shiftKey) {
                if (e.key === 'b') { e.preventDefault(); md('**','**','bold'); }
                if (e.key === 'i') { e.preventDefault(); md('*','*','italic'); }
                if (e.key === 'k') { e.preventDefault(); mdLink(); }
                if (e.key === 'e') { e.preventDefault(); md('`','`','code'); }
            }
            // Tab key → insert 2 spaces (useful for code blocks)
            if (e.key === 'Tab') {
                e.preventDefault();
                const ta = e.target, s = ta.selectionStart;
                ta.setRangeText('  ', s, s, 'end');
                schedulePreview();
            }
        });
    }

    // ── Tag chips ─────────────────────────────────────────────────────────────────
    function renderTagChips() {
        const row = document.getElementById('tag-input-row');
        row.querySelectorAll('.tag-chip').forEach(c => c.remove());
        const input = document.getElementById('tag-text-input');
        state.currentTags.forEach(tag => {
            const chip = document.createElement('span');
            chip.className = 'tag-chip';
            chip.innerHTML = `${esc(tag)}<button onclick="removeTag('${esc(tag)}')" title="Remove">×</button>`;
            row.insertBefore(chip, input);
        });
    }
    function removeTag(tag) {
        state.currentTags = state.currentTags.filter(t => t !== tag);
        renderTagChips();
    }
    document.getElementById('tag-text-input').addEventListener('keydown', e => {
        if (e.key === 'Enter' || e.key === ',') {
            e.preventDefault();
            const val = e.target.value.trim().replace(/,/g,'').toLowerCase();
            if (val && !state.currentTags.includes(val)) { state.currentTags.push(val); renderTagChips(); }
            e.target.value = '';
        }
        if (e.key === 'Backspace' && !e.target.value && state.currentTags.length) {
            state.currentTags.pop(); renderTagChips();
        }
    });

    // ── Semantic search (debounced) ───────────────────────────────────────────────
    let searchTimer;
    document.getElementById('search-input').addEventListener('input', () => {
        clearTimeout(searchTimer);
        searchTimer = setTimeout(loadNotes, 350);
    });

