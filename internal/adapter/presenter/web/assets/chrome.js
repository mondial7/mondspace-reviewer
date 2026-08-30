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

// ── Folding the shell ───────────────────────────────────────────────────────
//
// Three states, and the third is the other two at once. The sidebar folds to a
// strip of two-letter stops; the cockpit's instrument panel folds to a sliver;
// and zen (⌘Z) hides both, because "get out of my way" is one thought rather
// than two clicks. Each is remembered, and one state across every page, so
// moving around does not keep re-opening what you closed.

const FOLD_KEY = 'msr:sidenav';
const PANEL_KEY = 'msr:panel';

let folded = stored(FOLD_KEY) === 'on';
let panelFolded = stored(PANEL_KEY) === 'on';
let zen = prefersReducedMotion || stored(ZEN_KEY) === 'on';

function applyShell() {
  body.classList.toggle('page--folded', folded && !zen);
  body.classList.toggle('page--panelfolded', panelFolded && !zen);
  body.classList.toggle('page--zen', zen);

  for (const b of document.querySelectorAll('[data-sidenav-toggle]')) {
    b.setAttribute('aria-expanded', String(!folded));
    b.textContent = folded ? '\u203A' : '\u2039';
    b.title = folded ? 'Expand the sidebar' : 'Collapse the sidebar';
  }
  for (const b of document.querySelectorAll('[data-panel-toggle]')) {
    b.setAttribute('aria-expanded', String(!panelFolded));
    b.textContent = panelFolded ? '\u2039' : '\u203A';
    b.title = panelFolded ? 'Show the panel' : 'Collapse the panel';
  }
}

function setZen(on) {
  zen = on;
  remember(ZEN_KEY, on ? 'on' : 'off');
  applyShell();
}

function setFolded(on) {
  folded = on;
  remember(FOLD_KEY, on ? 'on' : 'off');
  applyShell();
}

function setPanelFolded(on) {
  panelFolded = on;
  remember(PANEL_KEY, on ? 'on' : 'off');
  applyShell();
}

applyShell();
for (const b of document.querySelectorAll('[data-sidenav-toggle]')) {
  b.addEventListener('click', () => setFolded(!folded));
}
for (const b of document.querySelectorAll('[data-panel-toggle]')) {
  b.addEventListener('click', () => setPanelFolded(!panelFolded));
}

// ── Theme ───────────────────────────────────────────────────────────────────
//
// Following the operating system is a state of its own, not the absence of a
// choice: a two-state toggle silently overrides the OS forever after one click,
// which is the wrong default for someone who switches at sunset.
//
// With five of them, cycling is no longer a reasonable way to arrive anywhere —
// four clicks to get back to where you were is not a control. The button opens
// a menu instead, built here rather than in the six templates that carry the
// nav, so adding a theme is one list and not six edits.

const THEME_KEY = 'msr:theme';
const THEMES = [
  { id: 'system', label: 'auto', hint: 'follow the operating system' },
  { id: 'light', label: 'light', hint: 'the daylight palette' },
  { id: 'dark', label: 'dark', hint: 'the deep-universe palette' },
  { id: 'solarized-light', label: 'solar light', hint: 'Solarized, on paper' },
  { id: 'solarized-dark', label: 'solar dark', hint: 'Solarized, at night' },
];

const themeIds = THEMES.map((t) => t.id);
let theme = themeIds.includes(stored(THEME_KEY)) ? stored(THEME_KEY) : 'system';

function themeLabel(id) {
  return (THEMES.find((t) => t.id === id) || THEMES[0]).label;
}

function applyTheme() {
  const root = document.documentElement;
  if (theme === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', theme);
  for (const b of document.querySelectorAll('[data-theme-toggle]')) {
    b.textContent = themeLabel(theme);
    b.title = `Theme: ${themeLabel(theme)} — click to change (\u2318J cycles)`;
    b.setAttribute('aria-expanded', 'false');
  }
  for (const item of document.querySelectorAll('[data-theme-id]')) {
    item.setAttribute('aria-checked', String(item.dataset.themeId === theme));
  }
  // The starfield and the isometric field are painted on canvases from these
  // same custom properties, and a canvas does not re-read a stylesheet on its
  // own. Without this the page changed theme and the two drawings did not.
  document.dispatchEvent(new CustomEvent('msr:theme', { detail: { theme } }));
}

function setTheme(id) {
  theme = themeIds.includes(id) ? id : 'system';
  remember(THEME_KEY, theme);
  applyTheme();
}

function cycleTheme() {
  setTheme(themeIds[(themeIds.indexOf(theme) + 1) % themeIds.length]);
}

// themeMenu builds the popover next to one toggle button.
function themeMenu(button) {
  const menu = document.createElement('div');
  menu.className = 'thememenu';
  menu.setAttribute('role', 'menu');
  menu.hidden = true;

  for (const t of THEMES) {
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'thememenu__item';
    item.dataset.themeId = t.id;
    item.setAttribute('role', 'menuitemradio');
    item.setAttribute('aria-checked', String(t.id === theme));
    const name = document.createElement('span');
    name.className = 'thememenu__name';
    name.textContent = t.label;
    const hint = document.createElement('span');
    hint.className = 'thememenu__hint';
    hint.textContent = t.hint;
    item.append(name, hint);
    item.addEventListener('click', () => {
      setTheme(t.id);
      close();
    });
    menu.append(item);
  }

  function open() {
    menu.hidden = false;
    button.setAttribute('aria-expanded', 'true');
    menu.querySelector('[aria-checked="true"]')?.focus();
  }
  function close() {
    menu.hidden = true;
    button.setAttribute('aria-expanded', 'false');
  }

  button.setAttribute('aria-haspopup', 'menu');
  button.addEventListener('click', (e) => {
    e.stopPropagation();
    menu.hidden ? open() : close();
  });
  menu.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { close(); button.focus(); }
  });
  // Anywhere else dismisses it. A menu that only closes by choosing something
  // makes changing your mind cost a choice.
  document.addEventListener('click', () => { if (!menu.hidden) close(); });

  const shell = document.createElement('span');
  shell.className = 'thememenu__anchor';
  button.replaceWith(shell);
  shell.append(button, menu);
}

for (const b of document.querySelectorAll('[data-theme-toggle]')) {
  themeMenu(b);
}
applyTheme();

// ── Command palette ─────────────────────────────────────────────────────────
//
// Zen hides the nav rail, so navigation cannot depend on it. The palette is the
// way around the application from anywhere, and it is built from the same list
// of destinations the rail renders.

const DESTINATIONS = [
  { label: 'Cockpit', hint: 'story, changes and the live panel', href: '/' },
  { label: 'Activity', hint: 'every model call, with what it cost', href: '/activity' },
  { label: 'Settings', hint: 'model, remote, repositories, usage', href: '/settings' },
  { label: 'How to use this', hint: 'the four things, in order', href: '/tutorial' },
  { label: 'Search the review log', hint: 'every note, question and finding', href: '/search' },
  { label: 'Reviews', hint: 'every review, across repositories', href: '/settings?s=reviews' },
  { label: 'Next theme', hint: 'auto · light · dark · solarized', action: cycleTheme },
  { label: 'Toggle zen', hint: 'hide the sidebar and the panel — ⌘Z', action: () => setZen(!zen) },
  { label: 'Fold the sidebar', hint: 'two-letter stops instead of words', action: () => setFolded(!folded) },
  { label: 'Fold the panel', hint: 'give the diff the whole width', action: () => setPanelFolded(!panelFolded) },
  { label: 'Keyboard shortcuts', hint: 'moving around without the mouse — or press ?',
    action: () => document.dispatchEvent(new CustomEvent('msr:shortcuts')) },
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
  if (target.action) {
    target.action();
    return;
  }
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
  if (mod && e.key.toLowerCase() === 'j') {
    if (isTyping(e.target)) return;
    e.preventDefault();
    cycleTheme();
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

  const readInk = () => getComputedStyle(document.documentElement)
    .getPropertyValue('--fg-dim').trim() || '#8b90a8';
  let ink = readInk();
  document.addEventListener('msr:theme', () => { ink = readInk(); });

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

// ── Controls with nothing to do ─────────────────────────────────────────────
//
// A button that is lit and does nothing teaches you to distrust the lit ones.
// The review picker submits the review you are already reading until you change
// it, so it is disabled until you do — from here rather than from the server,
// because with no JavaScript it must stay pressable.

for (const button of document.querySelectorAll('[data-unless-changed]')) {
  const input = document.getElementById(button.dataset.unlessChanged);
  if (!input) continue;

  const initial = input.value;
  const sync = () => {
    const same = input.value.trim() === initial.trim();
    button.disabled = same;
    button.title = same ? 'this is the review you are reading' : 'open ' + input.value;
  };
  sync();
  input.addEventListener('input', sync);
  input.addEventListener('change', sync);
}
