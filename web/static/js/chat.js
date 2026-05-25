// chat.js — SSE chat handler, message rendering, session management
    // ── Chat ──────────────────────────────────────────────────────────────────────
    async function sendChat() {
        if (state.isStreaming) return;
        const input = document.getElementById('chat-input');
        const query = input.value.trim();
        if (!query) return;

        input.value = '';
        autoResize(input);
        state.isStreaming = true;
        document.getElementById('chat-send').disabled = true;

        const msgs = document.getElementById('messages');
        msgs.querySelector('.empty-state')?.remove();

        appendMessage('user', query);
        const assistantEl = appendMessage('assistant', '');
        const bubble = assistantEl.querySelector('.msg-bubble');
        bubble.classList.add('streaming');

        let accumulated = '';

        try {
            const resp = await fetch('/api/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ query }),
            });
            if (!resp.ok) throw new Error(`Server error ${resp.status}`);

            const reader  = resp.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;
                buffer += decoder.decode(value, { stream: true });
                const parts = buffer.split('\n\n');
                buffer = parts.pop() ?? '';

                for (const part of parts) {
                    if (!part.startsWith('data: ')) continue;
                    let data;
                    try { data = JSON.parse(part.slice(6)); } catch { continue; }

                    if (data.sources !== undefined) {
                        renderSources(assistantEl, data.sources);
                        continue;
                    }
                    if (data.error) {
                        accumulated += `\n\n⚠ ${data.error}`;
                    }
                    if (data.chunk !== undefined) {
                        accumulated += data.chunk;
                        // Re-render markdown on each token — marked is fast enough.
                        bubble.innerHTML = `<div class="md-preview">${renderMarkdown(accumulated)}</div>`;
                        msgs.scrollTop = msgs.scrollHeight;
                    }
                }
            }
        } catch(err) {
            bubble.innerHTML = `<div class="md-preview">⚠ ${esc(err.message)}</div>`;
        } finally {
            bubble.classList.remove('streaming');
            state.isStreaming = false;
            document.getElementById('chat-send').disabled = false;
            msgs.scrollTop = msgs.scrollHeight;
        }
    }

    function appendMessage(role, text) {
        const msgs = document.getElementById('messages');
        const el = document.createElement('div');
        el.className = 'message';
        el.innerHTML = `
    <div class="msg-role ${role}">${role === 'user' ? 'you' : 'assistant'}</div>
    <div class="msg-bubble ${role}">${text ? `<div class="md-preview">${renderMarkdown(text)}</div>` : ''}</div>`;
        msgs.appendChild(el);
        msgs.scrollTop = msgs.scrollHeight;
        return el;
    }

    function renderSources(msgEl, sources) {
        if (!sources?.length) return;
        const div = document.createElement('div');
        div.className = 'sources';
        div.innerHTML = `
    <div class="sources-label">Context from ${sources.length} note${sources.length !== 1 ? 's' : ''}</div>
    <div class="sources-list">${sources.map(s => {
            const tags = (s.tags||[]).slice(0,2).map(t=>`<span style="color:var(--accent-dim)"> #${esc(t)}</span>`).join('');
            return `<div class="source-chip" onclick="selectNote(${s.id});switchView('notes')" title="${esc(s.preview)}">
        <span class="source-id">#${s.id}</span>${esc(s.preview.slice(0,36))}…${tags}
      </div>`;
        }).join('')}</div>`;
        msgEl.insertBefore(div, msgEl.querySelector('.msg-bubble'));
    }

    // Auto-resize chat textarea
    function autoResize(el) {
        el.style.height = 'auto';
        el.style.height = Math.min(el.scrollHeight, 140) + 'px';
    }
    document.getElementById('chat-input').addEventListener('input', function() { autoResize(this); });
    document.getElementById('chat-input').addEventListener('keydown', e => {
        if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendChat(); }
    });
