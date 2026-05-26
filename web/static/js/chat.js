/**
 * chat.js — minimal SSE streaming client for /api/chat
 *
 * This is the only JavaScript in the new htmx-based frontend.
 * It handles the one thing htmx can't do cleanly: streaming JSON chunks
 * from an SSE response and appending them to the DOM as they arrive.
 */
(function () {
  'use strict';

  const messagesDiv = document.getElementById('chat-messages');
  const sourcesDiv  = document.getElementById('chat-sources');
  const chatInput   = document.getElementById('chat-input');
  const sendBtn     = document.getElementById('chat-send');

  if (!messagesDiv || !chatInput || !sendBtn) return; // not on app page

  let isStreaming = false;

  // ── Helpers ─────────────────────────────────────────────────────────────

  function escapeHtml(str) {
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function scrollToBottom() {
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
  }

  function setStreaming(active) {
    isStreaming        = active;
    sendBtn.disabled   = active;
    chatInput.disabled = active;
    sendBtn.textContent = active ? '…' : 'Send';
  }

  /** Appends a chat bubble and returns the inner text node element. */
  function appendBubble(role) {
    const wrapper = document.createElement('div');
    wrapper.className = role === 'user'
      ? 'flex justify-end'
      : 'flex justify-start';

    const bubble = document.createElement('div');
    bubble.className = role === 'user'
      ? 'bg-indigo-600 text-white rounded-2xl rounded-tr-sm px-4 py-2.5 max-w-[75%] text-sm'
      : 'bg-gray-800 text-gray-100 rounded-2xl rounded-tl-sm px-4 py-2.5 max-w-[75%] text-sm whitespace-pre-wrap';

    wrapper.appendChild(bubble);
    messagesDiv.appendChild(wrapper);
    scrollToBottom();
    return bubble;
  }

  // ── Source citations ─────────────────────────────────────────────────────

  function renderSources(sources) {
    if (!sources || !sources.length) {
      sourcesDiv.classList.add('hidden');
      return;
    }
    sourcesDiv.classList.remove('hidden');
    sourcesDiv.innerHTML =
      '<span class="font-medium text-gray-500 mr-2">Sources:</span>' +
      sources.map(function (s) {
        const preview = escapeHtml((s.content || '').slice(0, 50)) + '…';
        return '<span class="inline-block bg-gray-800 text-indigo-400 border border-gray-700 ' +
               'rounded px-2 py-0.5 mr-1 mb-1">' + preview + '</span>';
      }).join('');
  }

  // ── Main send ────────────────────────────────────────────────────────────

  function sendMessage() {
    const query = chatInput.value.trim();
    if (!query || isStreaming) return;

    // Clear welcome message on first send
    const welcome = messagesDiv.querySelector('p');
    if (welcome) welcome.remove();

    chatInput.value = '';

    // Auto-grow textarea back to 1 row
    chatInput.rows = 1;

    appendBubble('user').textContent = query;

    const assistantBubble = appendBubble('assistant');
    assistantBubble.textContent = '';
    let assistantText = '';

    setStreaming(true);
    sourcesDiv.classList.add('hidden');
    sourcesDiv.innerHTML = '';

    fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: query, session_id: 'default' }),
    })
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const reader  = res.body.getReader();
        const decoder = new TextDecoder();
        let   buffer  = '';

        function pump() {
          return reader.read().then(function (chunk) {
            if (chunk.done) {
              setStreaming(false);
              return;
            }
            buffer += decoder.decode(chunk.value, { stream: true });

            // SSE lines end with \n\n; split on newlines and process complete ones
            var lines = buffer.split('\n');
            buffer = lines.pop(); // last element may be an incomplete line

            lines.forEach(function (line) {
              if (!line.startsWith('data: ')) return;
              try {
                var msg = JSON.parse(line.slice(6));

                if (msg.sources !== undefined) {
                  renderSources(msg.sources);
                }
                if (msg.chunk) {
                  assistantText += msg.chunk;
                  assistantBubble.textContent = assistantText;
                  scrollToBottom();
                }
                if (msg.error) {
                  assistantBubble.textContent = '⚠ ' + msg.error;
                  setStreaming(false);
                }
                if (msg.done) {
                  setStreaming(false);
                }
              } catch (_) {
                // ignore malformed JSON lines
              }
            });

            return pump();
          });
        }

        return pump();
      })
      .catch(function (err) {
        assistantBubble.textContent =
          '⚠ Could not reach the server. Is Ollama running? (' + err.message + ')';
        setStreaming(false);
      });
  }

  // ── Event listeners ──────────────────────────────────────────────────────

  sendBtn.addEventListener('click', sendMessage);

  chatInput.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });

  // Auto-grow textarea as the user types
  chatInput.addEventListener('input', function () {
    this.rows = 1;
    const lineHeight = parseInt(getComputedStyle(this).lineHeight, 10) || 20;
    const rows = Math.min(6, Math.ceil(this.scrollHeight / lineHeight));
    this.rows = rows;
  });
})();
