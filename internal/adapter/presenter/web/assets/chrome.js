// chrome.js — the shell every page shares: the spatial backdrop, zen mode, and
// the command palette (ADR 0012, ADR 0016).
//
// It owns nothing on the page except decoration and navigation, so a browser
// with no WebGL, no JavaScript module support, or reduced-motion preferences
// still gets the whole application — just without the starfield.

const ZEN_KEY = 'msr:zen';
const body = document.body;

const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

function stored(key) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null; // private windows and blocked storage must not break the page
  }
}

function remember(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* nothing to do: the preference simply will not persist */
  }
}

// ── Zen mode ────────────────────────────────────────────────────────────────
//
// Zen strips the shell — backdrop and nav rail — leaving only the work. It is
// one state across every page, remembered between them, so moving around does
// not flip the reader in and out of it.

let zen = prefersReducedMotion || stored(ZEN_KEY) === 'on';

function applyZen() {
  body.classList.toggle('page--zen', zen);
  for (const b of document.querySelectorAll('[data-zen-toggle]')) {
    b.setAttribute('aria-pressed', String(zen));
    b.textContent = zen ? 'exit zen' : 'zen';
  }
}

function setZen(on) {
  zen = on;
  remember(ZEN_KEY, on ? 'on' : 'off');
  applyZen();
}

applyZen();
for (const b of document.querySelectorAll('[data-zen-toggle]')) {
  b.addEventListener('click', () => setZen(!zen));
}

// ── Command palette ─────────────────────────────────────────────────────────
//
// Zen hides the nav rail, so navigation cannot depend on it. The palette is the
// way around the application from anywhere, and it is built from the same list
// of destinations the rail renders.

const DESTINATIONS = [
  { label: 'Cockpit', hint: 'story, changes and the live panel', href: '/' },
  { label: 'Activity', hint: 'every model call, with what it cost', href: '/activity' },
  { label: 'Status', hint: 'is the reviewer model online', href: '/status' },
  { label: 'Sessions', hint: 'every review, across repositories', href: '/sessions' },
];

let palette = null;
let input = null;
let list = null;
let matches = [];
let cursor = 0;

function buildPalette() {
  palette = document.createElement('div');
  palette.className = 'palette';
  palette.hidden = true;
  palette.innerHTML = `
    <div class="palette__sheet" role="dialog" aria-modal="true" aria-label="Go to">
      <input class="palette__input" type="text" autocomplete="off" spellcheck="false"
             placeholder="go to… (esc to close)">
      <ul class="palette__list"></ul>
    </div>`;
  body.appendChild(palette);

  input = palette.querySelector('.palette__input');
  list = palette.querySelector('.palette__list');

  palette.addEventListener('click', (e) => {
    if (e.target === palette) closePalette(); // clicking the backdrop dismisses
  });
  input.addEventListener('input', () => render(input.value));
  input.addEventListener('keydown', onPaletteKey);
}

// targets are the fixed destinations plus every changed file on this page, so
// the palette answers "where is auth/token.go" as well as "where is status".
function targets() {
  const files = [...document.querySelectorAll('.post[data-file]')].map((el) => ({
    label: el.dataset.file,
    hint: `+${el.dataset.added || 0} −${el.dataset.removed || 0}`,
    anchor: el.id,
  }));
  return [...DESTINATIONS, ...files];
}

function render(query) {
  const q = query.trim().toLowerCase();
  matches = targets()
    .filter((t) => !q || t.label.toLowerCase().includes(q))
    .slice(0, 12);
  cursor = 0;
  list.innerHTML = '';
  for (const [i, t] of matches.entries()) {
    const li = document.createElement('li');
    li.className = 'palette__item';
    li.dataset.selected = String(i === cursor);
    li.innerHTML = `<span class="palette__label"></span><span class="palette__hint"></span>`;
    li.querySelector('.palette__label').textContent = t.label;
    li.querySelector('.palette__hint').textContent = t.hint || '';
    li.addEventListener('click', () => go(t));
    list.appendChild(li);
  }
}

function moveCursor(delta) {
  if (!matches.length) return;
  cursor = (cursor + delta + matches.length) % matches.length;
  for (const [i, li] of [...list.children].entries()) {
    li.dataset.selected = String(i === cursor);
  }
  list.children[cursor]?.scrollIntoView({ block: 'nearest' });
}

function go(target) {
  closePalette();
  if (target.href) {
    window.location.href = target.href;
    return;
  }
  const el = document.getElementById(target.anchor);
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    el.dataset.active = 'true';
  }
}

function onPaletteKey(e) {
  if (e.key === 'ArrowDown') { e.preventDefault(); moveCursor(1); }
  else if (e.key === 'ArrowUp') { e.preventDefault(); moveCursor(-1); }
  else if (e.key === 'Enter') { e.preventDefault(); if (matches[cursor]) go(matches[cursor]); }
  else if (e.key === 'Escape') { e.preventDefault(); closePalette(); }
}

function openPalette() {
  if (!palette) buildPalette();
  palette.hidden = false;
  input.value = '';
  render('');
  input.focus();
}

function closePalette() {
  if (palette) palette.hidden = true;
}

function paletteOpen() {
  return palette && !palette.hidden;
}

// ── Key bindings ────────────────────────────────────────────────────────────

document.addEventListener('keydown', (e) => {
  const mod = e.metaKey || e.ctrlKey;

  if (mod && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    paletteOpen() ? closePalette() : openPalette();
    return;
  }
  if (mod && e.key.toLowerCase() === 'z') {
    // Only when nothing is being typed: inside a field this is undo, and
    // stealing it would be hostile.
    if (isTyping(e.target)) return;
    e.preventDefault();
    setZen(!zen);
    return;
  }
  if (e.key === 'Escape') {
    if (paletteOpen()) { closePalette(); return; }
    if (zen) { setZen(false); return; }
  }
});

function isTyping(el) {
  if (!el) return false;
  const tag = el.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || el.isContentEditable;
}

// ── Backdrop ────────────────────────────────────────────────────────────────
//
// A slow starfield behind every page, so the whole application sits in the same
// space rather than the cockpit alone. Drawn on a 2D canvas: it must never
// compete with the isometric field for a WebGL context.

async function backdrop() {
  const canvas = document.getElementById('backdrop');
  if (!canvas || prefersReducedMotion) return;
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  let stars = [];
  let raf = 0;

  function seed() {
    const { clientWidth: w, clientHeight: h } = canvas;
    canvas.width = w * Math.min(devicePixelRatio, 2);
    canvas.height = h * Math.min(devicePixelRatio, 2);
    const count = Math.round((w * h) / 9000);
    stars = Array.from({ length: count }, (_, i) => ({
      x: Math.random() * canvas.width,
      y: Math.random() * canvas.height,
      // Three depths, drifting at different speeds: parallax is what stops it
      // reading as a flat pattern.
      z: 0.35 + (i % 3) * 0.32,
    }));
  }
  seed();
  window.addEventListener('resize', seed);

  const ink = getComputedStyle(document.documentElement)
    .getPropertyValue('--fg-dim').trim() || '#8b90a8';

  function frame() {
    raf = requestAnimationFrame(frame);
    if (body.classList.contains('page--zen')) { ctx.clearRect(0, 0, canvas.width, canvas.height); return; }
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    for (const s of stars) {
      s.x -= s.z * 0.12;
      if (s.x < 0) { s.x = canvas.width; s.y = Math.random() * canvas.height; }
      ctx.globalAlpha = 0.10 + s.z * 0.18;
      ctx.fillStyle = ink;
      ctx.fillRect(s.x, s.y, s.z * 1.6, s.z * 1.6);
    }
    ctx.globalAlpha = 1;
  }
  frame();

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) { cancelAnimationFrame(raf); raf = 0; }
    else if (!raf) frame();
  });
}

backdrop();
