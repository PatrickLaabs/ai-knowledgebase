// ── Marked setup ──────────────────────────────────────────────────────────────
// Runs once on load. Safe against v4 and v5+ API differences.
(function initMarked() {
  if (typeof marked === "undefined") return;
  try {
    if (typeof marked.use === "function") {
      marked.use({ breaks: true, gfm: true });
    } else if (typeof marked.setOptions === "function") {
      marked.setOptions({ breaks: true, gfm: true });
    }
  } catch (_) {}
})();

// ── Markdown preview ──────────────────────────────────────────────────────────

function renderPreview(content) {
  var el = document.getElementById("md-preview");
  if (!el) return;
  if (!content || !content.trim()) {
    el.innerHTML =
      '<span class="text-gray-600 text-xs">Preview will appear here\u2026</span>';
    return;
  }
  try {
    el.innerHTML =
      typeof marked !== "undefined" ? marked.parse(content) : content;
  } catch (_) {
    el.innerHTML = '<span class="text-red-400 text-xs">Preview error</span>';
  }
}

// ── Tag helpers ───────────────────────────────────────────────────────────────

function getTags() {
  var h = document.getElementById("note-tags");
  if (!h || !h.value.trim()) return [];
  return h.value
    .split(",")
    .map(function (t) {
      return t.trim();
    })
    .filter(Boolean);
}

function syncHidden() {
  var h = document.getElementById("note-tags");
  var box = document.getElementById("tag-chip-box");
  if (!h || !box) return;
  var tags = [];
  box.querySelectorAll(".tag-chip[data-tag]").forEach(function (c) {
    tags.push(c.dataset.tag);
  });
  h.value = tags.join(",");
}

function removeChip(chip) {
  chip.remove();
  syncHidden();
  updateTagPlaceholder();
}

function updateTagPlaceholder() {
  var f = document.getElementById("tag-input-field");
  if (f) f.placeholder = getTags().length === 0 ? "type a tag\u2026" : "";
}

function addChip(raw) {
  var tag = raw.trim().replace(/,/g, "");
  if (!tag) return;
  if (getTags().indexOf(tag) !== -1) return;

  var box = document.getElementById("tag-chip-box");
  var field = document.getElementById("tag-input-field");
  if (!box || !field) return;

  var chip = document.createElement("span");
  chip.className =
    "tag-chip inline-flex items-center gap-1 text-xs px-2 py-0.5 " +
    "bg-indigo-950 text-indigo-300 border border-indigo-800/60 rounded";
  chip.dataset.tag = tag;

  var label = document.createTextNode(tag);
  var btn = document.createElement("button");
  btn.type = "button";
  btn.className =
    "tag-chip-remove text-indigo-500 hover:text-indigo-200 leading-none ml-1";
  btn.textContent = "\u00d7";
  btn.addEventListener("click", function () {
    removeChip(chip);
  });

  chip.appendChild(label);
  chip.appendChild(btn);
  box.insertBefore(chip, field);

  field.value = "";
  syncHidden();
  updateTagPlaceholder();
}

// Called by the "Add" button's onclick AND by the Enter keydown handler.
function addTagFromInput() {
  var field = document.getElementById("tag-input-field");
  if (!field || !field.value.trim()) return;
  addChip(field.value);
  field.focus();
}

// Expose for inline onclick on the Add button.
window.addTagFromInput = addTagFromInput;

// ── Tag event listeners (document-level delegation) ───────────────────────────

document.addEventListener("keydown", function (e) {
  if (!e.target || e.target.id !== "tag-input-field") return;
  if (e.key === "Enter" || e.key === ",") {
    e.preventDefault();
    e.stopPropagation();
    addTagFromInput();
    return;
  }
  if (e.key === "Backspace" && e.target.value === "") {
    var box = document.getElementById("tag-chip-box");
    var chips = box ? box.querySelectorAll(".tag-chip") : [];
    if (chips.length > 0) {
      removeChip(chips[chips.length - 1]);
    }
  }
});

document.addEventListener("focusout", function (e) {
  if (e.target && e.target.id === "tag-input-field" && e.target.value.trim()) {
    addTagFromInput();
  }
});

document.addEventListener("click", function (e) {
  if (!e.target) return;
  // Click anywhere in chip box → focus the input
  var box = document.getElementById("tag-chip-box");
  if (box && (box === e.target || box.contains(e.target))) {
    if (!e.target.classList.contains("tag-chip-remove")) {
      var f = document.getElementById("tag-input-field");
      if (f) f.focus();
    }
  }
});

// ── Mobile tab switcher ───────────────────────────────────────────────────────

var editorMQ = window.matchMedia("(min-width: 640px)");

function switchTab(tab) {
  var paneEdit = document.getElementById("pane-edit");
  var panePreview = document.getElementById("pane-preview");
  var tabEdit = document.getElementById("tab-edit");
  var tabPreview = document.getElementById("tab-preview");
  if (!paneEdit || !panePreview || !tabEdit || !tabPreview) return;

  if (tab === "preview") {
    var ta = document.getElementById("note-content");
    if (ta) renderPreview(ta.value);
    paneEdit.classList.add("hidden");
    paneEdit.classList.remove("flex");
    panePreview.classList.remove("hidden");
    panePreview.classList.add("flex");
    tabPreview.classList.add("text-indigo-400", "border-indigo-500");
    tabPreview.classList.remove("text-gray-500", "border-transparent");
    tabEdit.classList.remove("text-indigo-400", "border-indigo-500");
    tabEdit.classList.add("text-gray-500", "border-transparent");
  } else {
    panePreview.classList.add("hidden");
    panePreview.classList.remove("flex");
    paneEdit.classList.remove("hidden");
    paneEdit.classList.add("flex");
    tabEdit.classList.add("text-indigo-400", "border-indigo-500");
    tabEdit.classList.remove("text-gray-500", "border-transparent");
    tabPreview.classList.remove("text-indigo-400", "border-indigo-500");
    tabPreview.classList.add("text-gray-500", "border-transparent");
    var ta = document.getElementById("note-content");
    if (ta) ta.focus();
  }
}

document.addEventListener("click", function (e) {
  // Use closest() so tapping the text node inside the button still matches.
  if (!e.target) return;
  if (e.target.closest("#tab-edit")) switchTab("edit");
  if (e.target.closest("#tab-preview")) switchTab("preview");
});

editorMQ.addEventListener("change", function (e) {
  var paneEdit = document.getElementById("pane-edit");
  var panePreview = document.getElementById("pane-preview");
  if (!paneEdit || !panePreview) return;
  if (e.matches) {
    paneEdit.classList.remove("hidden");
    paneEdit.classList.add("flex");
    panePreview.classList.remove("hidden");
    panePreview.classList.add("flex");
  } else {
    panePreview.classList.add("hidden");
    panePreview.classList.remove("flex");
    paneEdit.classList.remove("hidden");
    paneEdit.classList.add("flex");
  }
});

// ── Live preview (document-level delegation) ──────────────────────────────────

document.addEventListener("input", function (e) {
  if (e.target && e.target.id === "note-content" && editorMQ.matches) {
    renderPreview(e.target.value);
  }
});

// ── Modal lifecycle ───────────────────────────────────────────────────────────

function onModalReady() {
  var textarea = document.getElementById("note-content");
  if (!textarea) return;
  renderPreview(textarea.value);
  updateTagPlaceholder();

  // Wire remove buttons on server-rendered chips (edit mode)
  var box = document.getElementById("tag-chip-box");
  if (box) {
    box.querySelectorAll(".tag-chip").forEach(function (chip) {
      var btn = chip.querySelector(".tag-chip-remove");
      if (btn)
        btn.addEventListener("click", function () {
          removeChip(chip);
        });
    });
  }

  // Wire tab buttons directly — belt-and-suspenders alongside the delegated
  // click listener, since mobile taps can miss ID checks on text nodes.
  var tabEdit = document.getElementById("tab-edit");
  var tabPreview = document.getElementById("tab-preview");
  if (tabEdit)
    tabEdit.addEventListener("click", function () {
      switchTab("edit");
    });
  if (tabPreview)
    tabPreview.addEventListener("click", function () {
      switchTab("preview");
    });

  textarea.focus();
  textarea.setSelectionRange(textarea.value.length, textarea.value.length);
}

document.addEventListener("htmx:afterSwap", function (e) {
  if (!e.detail || !e.detail.target) return;
  var modal = document.getElementById("note-modal");
  if (e.detail.target.id !== "note-modal") return;
  if (!modal || !modal.innerHTML.trim()) {
    if (modal) modal.style.display = "none";
    return;
  }
  modal.style.display = "";
  setTimeout(onModalReady, 10);
});

document.addEventListener("htmx:oobAfterSwap", function (e) {
  if (!e.detail || !e.detail.target) return;
  if (e.detail.target.id !== "note-modal") return;
  var modal = document.getElementById("note-modal");
  if (modal && !modal.innerHTML.trim()) modal.style.display = "none";
});
