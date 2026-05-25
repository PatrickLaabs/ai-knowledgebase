// markdown.js — marked + highlight.js configuration
marked.setOptions({ breaks: true, gfm: true });

// Wire highlight.js into marked for fenced code blocks.
const renderer = new marked.Renderer();
renderer.code = function(code, lang) {
    const language = (lang && hljs.getLanguage(lang)) ? lang : 'plaintext';
    let highlighted;
    try {
        highlighted = hljs.highlight(String(code), { language, ignoreIllegals: true }).value;
    } catch {
        highlighted = esc(String(code));
    }

    return `<div class="code-wrapper">
            <button class="copy-btn" onclick="copyCode(this)" title="Copy code">Copy</button>
            <pre><code class="hljs language-${language}">${highlighted}</code></pre>
        </div>`;
};
marked.use({ renderer });

function renderMarkdown(md) {
    return marked.parse(md || '');
}

// Strip markdown to plain text for sidebar previews.
function stripMd(md) {
    return md
        .replace(/```[\s\S]*?```/g, '[code]')
        .replace(/`[^`]+`/g, '$&')
        .replace(/#{1,6}\s+/g, '')
        .replace(/\*\*(.+?)\*\*/g, '$1')
        .replace(/\*(.+?)\*/g, '$1')
        .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
        .replace(/>\s+/g, '')
        .replace(/[-*+]\s+/g, '')
        .replace(/\n+/g, ' ')
        .trim();
}