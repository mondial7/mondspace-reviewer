// cockpit.js — the instrument panel (ADR 0012, ADR 0015).
//
// An isometric bar field where every block IS a changed file, not decoration:
//
//   height  = how many lines that file changed (log-scaled, so one generated
//             file cannot flatten everything else into the floor)
//   colour  = what kind of change it was — growth, deletion, or flagged
//   depth   = recency; the newest change stands at the front
//   motion  = only while the session is live, and strongest at the front
//
// The point is that the shape is readable. A row of short green blocks is steady
// additive work; one tall red block is a big deletion; an amber block is
// something the flags want you to look at. Watch it for a session and the
// patterns start to mean something before you have read a word.
//
// Like the rest of the chrome it reads the DOM and never feeds it. No WebGL, no Three.js,
// reduced motion or the panel hidden, and it bows out; the numbers and the feed
// are untouched.

const canvas = document.getElementById('cockpit-scene');
const body = document.body;

const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const MAX_BLOCKS = 24;   // a 400-file session must stay a readable shape
const SPACING = 1.45;    // gap between block centres, in block widths

function isLive() {
  return body.dataset.live === 'true';
}

// readChanges turns the feed into the data the field draws. Newest first, which
// is the order the feed is already in.
function readChanges() {
  const posts = [...document.querySelectorAll('.post')].slice(0, MAX_BLOCKS);
  return posts.map((el) => {
    const added = Number(el.dataset.added || 0);
    const removed = Number(el.dataset.removed || 0);
    const flags = Number(el.dataset.flags || 0);
    let kind = 'ctx';
    if (flags > 0) kind = 'flag';
    else if (removed > added) kind = 'del';
    else if (added > 0) kind = 'add';
    return { file: el.dataset.file || '', added, removed, flags, kind };
  });
}

async function start() {
  if (!canvas || prefersReducedMotion || body.classList.contains('page--focus')) return;

  let THREE;
  try {
    THREE = await import('/assets/vendor/three.module.min.js');
  } catch {
    return; // no Three.js, no panel — the page is already complete without it
  }

  let renderer;
  try {
    renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
  } catch {
    return; // no WebGL context (headless, blocked, software-render disabled)
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  const scene = new THREE.Scene();

  // A true isometric look needs an orthographic camera on the (1,1,1) diagonal;
  // a perspective camera would give a 3/4 view, not an isometric one.
  const camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0.1, 200);
  camera.position.set(14, 11, 14);

  // Lighting follows the ground the field sits on. A key light strong enough to
  // model blocks against near-black drags every face well below its own colour,
  // which on a pale theme is the difference between amber and mud. On a light
  // ground the ambient carries it and the key only shapes it.
  const ambient = new THREE.AmbientLight(0xffffff, 0.6);
  const key = new THREE.DirectionalLight(0xffffff, 0.85);
  key.position.set(8, 14, 5);
  scene.add(ambient, key);

  // pale reports whether the page is light, from the background it actually
  // has rather than from the name of the theme — a theme msr has never heard
  // of still gets lit correctly.
  function pale(css) {
    const bg = css.getPropertyValue('--bg').trim();
    const hex = bg.replace('#', '');
    if (hex.length !== 6) return false;
    const [r, g, b] = [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
    return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.5;
  }

  function relight(css) {
    const light = pale(css);
    ambient.intensity = light ? 2.2 : 0.6;
    key.intensity = light ? 0.9 : 0.85;
  }

  // Colours come from the stylesheet, so the field follows the theme and matches
  // the legend printed beneath it. Read on every repaint rather than once: a
  // canvas does not re-read a stylesheet when the theme changes, so reading at
  // start-up left the field in whichever palette the page happened to load in.
  const colour = (css, name, fallback) =>
    new THREE.Color(css.getPropertyValue(name).trim() || fallback);
  let palette;
  function readPalette() {
    const css = getComputedStyle(document.documentElement);
    relight(css);
    // The solid tokens, not the text ones: a colour dark enough to read as a
    // word is mud as a block this size, and on a light theme the two part
    // company entirely.
    palette = {
      add: colour(css, '--solid-add', '#a6e3a1'),
      del: colour(css, '--solid-del', '#f38ba8'),
      flag: colour(css, '--solid-flag', '#f9e2af'),
      ctx: colour(css, '--solid-ctx', '#cba6f7'),
    };
  }
  readPalette();

  const geometry = new THREE.BoxGeometry(1, 1, 1);
  let group = new THREE.Group();
  scene.add(group);
  let blocks = [];
  let side = 1;

  // build lays the field out from the feed. It runs again whenever the feed
  // changes, so a new file appearing in the session appears here too.
  function build() {
    scene.remove(group);
    for (const b of blocks) b.material.dispose();
    group = new THREE.Group();
    scene.add(group);
    blocks = [];

    const changes = readChanges();
    const n = Math.max(changes.length, 1);
    side = Math.ceil(Math.sqrt(n));

    changes.forEach((c, i) => {
      const cube = new THREE.Mesh(
        geometry,
        new THREE.MeshLambertMaterial({ color: palette[c.kind] }),
      );
      // Log scale: a 4,000-line generated file is taller than a 40-line one, but
      // not a hundred times taller, or every real change becomes invisible.
      const churn = c.added + c.removed;
      cube.userData.height = 0.25 + Math.log2(1 + churn) * 0.42;
      // Recency: index 0 is the newest change, drawn at the front of the field.
      cube.userData.recency = 1 - i / Math.max(n - 1, 1);
      cube.userData.phase = i * 0.35;

      const x = (i % side) - (side - 1) / 2;
      const z = Math.floor(i / side) - (side - 1) / 2;
      cube.position.set(x * SPACING, 0, z * SPACING);
      cube.scale.y = cube.userData.height;
      cube.position.y = cube.scale.y / 2;

      group.add(cube);
      blocks.push(cube);
    });
    resize();
  }

  function resize() {
    const { clientWidth: w, clientHeight: h } = canvas;
    if (!w || !h) return;
    renderer.setSize(w, h, false);

    // Fit the field to the pane whatever shape it is. An isometric grid is as
    // wide as its diagonal, hence the sqrt(2). The tallest block sets how much
    // headroom the top of the frustum needs.
    const tallest = blocks.reduce((m, b) => Math.max(m, b.userData.height), 1);
    const extent = (side - 1) * SPACING * Math.SQRT2 * 0.5 + tallest * 0.6 + 1.4;
    const aspect = w / h;
    const [halfW, halfH] = aspect >= 1
      ? [extent * aspect, extent]
      : [extent, extent / aspect];
    camera.left = -halfW;
    camera.right = halfW;
    camera.top = halfH;
    camera.bottom = -halfH;
    camera.lookAt(0, tallest * 0.35, 0); // aim at the mass, not the floor
    camera.updateProjectionMatrix();
  }

  build();
  window.addEventListener('resize', resize);

  // A new theme is a new palette, and the blocks hold their colour in a
  // material rather than in CSS — so they have to be rebuilt to change.
  document.addEventListener('msr:theme', () => { readPalette(); build(); });

  // Rebuild when live.js swaps the feed in: a change landing in the session is
  // exactly the moment the field should change shape.
  const feed = document.querySelector('.cockpit__changes');
  if (feed) new MutationObserver(build).observe(feed, { childList: true, subtree: false });

  // energy eases rather than switching, so a session going quiet reads as
  // settling rather than as a cut.
  let energy = isLive() ? 1 : 0;
  let t = 0;
  let raf = 0;

  function frame() {
    raf = requestAnimationFrame(frame);
    t += 0.016;
    energy += ((isLive() ? 1 : 0) - energy) * 0.02;

    // A slow drift always; the field turns a little faster while work is landing.
    group.rotation.y += 0.0009 + energy * 0.0022;

    for (const cube of blocks) {
      // Only the recent end of the field breathes, and only while the session is
      // live — so motion itself means "this is where work is happening now",
      // rather than being ambient decoration.
      const attention = energy * cube.userData.recency;
      const breath = Math.sin(t * 2 + cube.userData.phase) * 0.09 * attention;
      cube.scale.y = Math.max(0.1, cube.userData.height * (1 + breath));
      cube.position.y = cube.scale.y / 2;
      cube.material.emissive.setScalar(attention * 0.16);
    }
    renderer.render(scene, camera);
  }
  frame();

  // A backgrounded tab should not keep a GPU loop running.
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      cancelAnimationFrame(raf);
      raf = 0;
    } else if (!raf) {
      frame();
    }
  });
}

start();


// ── Linking the two columns ─────────────────────────────────────────────────
//
// The story and the changes are two views of the same session. An earlier
// version scrolled each one to follow the other; two smooth-scrolling panes
// driving each other fight the reader, and it was unusable.
//
// So the link is one-way and never moves anything the reader did not ask to
// move: scrolling the changes lights the chapter that covers what you are
// looking at, and clicking a chapter jumps the changes to it. Nothing scrolls
// on its own.
function linkColumns() {
  const story = document.getElementById('story-col');
  const changes = document.getElementById('changes-col');
  if (!story || !changes) return;

  const chapters = [...story.querySelectorAll('.chron__chapter[data-anchor]')];
  if (!chapters.length) return;

  function light(active) {
    for (const c of chapters) c.dataset.active = String(c === active);
  }

  // Click a chapter to bring its files up. This is the only thing that scrolls.
  for (const c of chapters) {
    c.addEventListener('click', () => {
      const target = document.getElementById('unit-' + c.dataset.anchor);
      if (!target) return;
      light(c);
      changes.scrollTo({ top: target.offsetTop - changes.offsetTop - 12, behavior: 'smooth' });
    });
  }

  // Scrolling the changes only *highlights* — it never moves the story column.
  const byAnchor = new Map(chapters.map((c) => ['unit-' + c.dataset.anchor, c]));
  let pending = 0;
  changes.addEventListener('scroll', () => {
    cancelAnimationFrame(pending);
    pending = requestAnimationFrame(() => {
      let active = chapters[0];
      for (const p of changes.querySelectorAll('.post')) {
        if (p.offsetTop - changes.offsetTop <= changes.scrollTop + 40 && byAnchor.has(p.id)) {
          active = byAnchor.get(p.id);
        }
      }
      light(active);
    });
  }, { passive: true });

  light(chapters[0]);
}

linkColumns();
// live.js swaps whole columns in; re-link whatever replaced them.
const storyCol = document.getElementById('story-col');
if (storyCol) new MutationObserver(linkColumns).observe(storyCol, { childList: true });

// ── The history overlay ─────────────────────────────────────────────────────
//
// A file's whole story, over the page rather than in the column: the diff, and
// the commits that touched it, steppable with the arrow keys. Fetched on open —
// this page already carries every file, and inlining every history would
// multiply it many times over.
(function historyOverlay() {
  const overlay = document.getElementById('overlay');
  if (!overlay) return;

  const body = document.getElementById('overlay-body');
  const label = document.getElementById('overlay-file');
  const pos = document.getElementById('overlay-pos');
  const prev = document.getElementById('overlay-prev');
  const next = document.getElementById('overlay-next');

  let base = '';          // the versions endpoint for the open file
  let hashes = [];        // every commit that touched it, newest first
  let index = 0;

  async function show(at) {
    const url = at ? `${base}?at=${encodeURIComponent(at)}` : base;
    body.innerHTML = '<p class="overlay__loading">loading…</p>';
    try {
      body.innerHTML = await (await fetch(url)).text();
    } catch {
      body.innerHTML = '<p class="overlay__loading">could not load this history</p>';
      return;
    }
    hashes = [...body.querySelectorAll('.versions__item[data-at]')].map((el) => el.dataset.at);
    if (at) index = Math.max(0, hashes.indexOf(at));
    pos.textContent = hashes.length ? `${index + 1} / ${hashes.length}` : '';
    prev.disabled = index >= hashes.length - 1;
    next.disabled = index <= 0;

    // Clicking a version in the list jumps straight to it.
    for (const [i, el] of [...body.querySelectorAll('.versions__item[data-at]')].entries()) {
      el.addEventListener('click', () => { index = i; show(el.dataset.at); });
    }
  }

  // index 0 is the newest, so "older" walks forward through the list.
  function step(by) {
    if (!hashes.length) return;
    const wanted = index + by;
    if (wanted < 0 || wanted >= hashes.length) return;
    index = wanted;
    show(hashes[index]);
  }

  function open(url, file) {
    base = url;
    index = 0;
    label.textContent = file;
    overlay.hidden = false;
    show('');
  }

  function close() {
    overlay.hidden = true;
    body.innerHTML = '';
  }

  // Delegated from the document, not bound to each button: live.js replaces the
  // whole changes column whenever the session moves, and listeners attached to
  // the elements it replaced are silently lost. On an active session that was
  // every fifteen seconds, so the button worked only until the first update.
  document.addEventListener('click', (e) => {
    const btn = e.target.closest?.('.post__open[data-versions]');
    if (btn) open(btn.dataset.versions, btn.dataset.file);
  });
  document.getElementById('overlay-close').addEventListener('click', close);
  prev.addEventListener('click', () => step(1));   // older
  next.addEventListener('click', () => step(-1));  // newer
  overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });

  document.addEventListener('keydown', (e) => {
    if (overlay.hidden) return;
    if (e.key === 'Escape') { e.preventDefault(); close(); }
    else if (e.key === 'ArrowLeft') { e.preventDefault(); step(1); }
    else if (e.key === 'ArrowRight') { e.preventDefault(); step(-1); }
  });
})();


// ── Detail / tree ───────────────────────────────────────────────────────────
//
// The same changes, two ways: grouped with their diffs, or the folder structure
// alone when you want the shape at a glance. The choice is remembered, because
// it is a working preference rather than a one-off.
(function viewSwitch() {
  const KEY = 'msr:changes-view';

  // Looked up on every call rather than captured: the changes column is
  // replaced wholesale when the session moves, and a cached element reference
  // would keep pointing at markup that is no longer on the page.
  function apply(view) {
    const detail = document.getElementById('detail-view');
    const tree = document.getElementById('tree-view');
    if (!detail || !tree) return;
    detail.hidden = view === 'tree';
    tree.hidden = view !== 'tree';
    for (const b of document.querySelectorAll('.viewswitch__btn')) {
      b.setAttribute('aria-pressed', String(b.dataset.view === view));
    }
    try { localStorage.setItem(KEY, view); } catch { /* preference will not persist */ }
  }

  function current() {
    try { return localStorage.getItem(KEY) || 'detail'; } catch { return 'detail'; }
  }
  apply(current());

  document.addEventListener('click', (e) => {
    const b = e.target.closest?.('.viewswitch__btn');
    if (b) apply(b.dataset.view);
  });

  // A swapped-in column renders with the detail view visible; re-apply the
  // remembered choice so a live update cannot silently flip it back.
  const changes = document.getElementById('changes-col');
  if (changes) {
    new MutationObserver(() => apply(current())).observe(changes, { childList: true });
  }

  // Delegated for the same reason the overlay is: this column gets replaced.
  document.addEventListener('click', (e) => {
    const link = e.target.closest?.('.tree__file');
    if (!link) return;
    apply('detail');
    const target = document.querySelector(link.getAttribute('href'));
    if (target) setTimeout(() => target.scrollIntoView({ block: 'start' }), 0);
  });
})();


// ── The reviewer assistant ──────────────────────────────────────────────────
//
// Asking a question is a mode, not a field at the bottom of a list. While one is
// being written the story gets out of the way, and while the model is thinking
// the page says so — a local reasoning model can take half a minute, and a form
// that looks idle for thirty seconds reads as broken.
(function ask() {
  const form = document.getElementById('ask-form');
  const input = document.getElementById('ask-input');
  if (!form || !input) return;

  const working = document.getElementById('ask-working');
  const workingText = document.getElementById('ask-working-text');
  const clear = document.getElementById('ask-clear');
  const story = document.getElementById('story-col');

  const send = document.getElementById('ask-send');
  const focus = (on) => story?.classList.toggle('cockpit__story--asking', on);

  // An empty box has nothing to ask and nothing to clear.
  const sync = () => {
    const typed = input.value.trim() !== '';
    if (send) send.disabled = !typed;
    if (clear) clear.disabled = !typed;
  };
  sync();

  input.addEventListener('focus', () => focus(true));
  input.addEventListener('input', () => { focus(input.value.trim() !== ''); sync(); });
  clear?.addEventListener('click', () => {
    input.value = '';
    focus(false);
    sync();
    input.focus();
  });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { input.value = ''; focus(false); sync(); input.blur(); }
    // A textarea takes Enter for a newline, so sending needs the modifier.
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) form.requestSubmit();
  });

  // The answer arrives with the next page render, so this is a submit-time
  // state: it has to survive until navigation, not until a callback.
  form.addEventListener('submit', () => {
    if (input.value.trim() === '') return;
    working.hidden = false;
    form.dataset.busy = 'yes';

    // Some honest texture about what is happening. A local model reasons before
    // it answers, and saying so is better than an unexplained wait.
    const stages = [
      'reading the session…',
      'reading the diffs and your notes…',
      'reasoning…',
      'still reasoning — a local model thinks before it answers…',
      'writing the answer…',
    ];
    let i = 0;
    setInterval(() => {
      i = Math.min(i + 1, stages.length - 1);
      workingText.textContent = stages[i];
    }, 4000);
  });
})();


// ── The picker ──────────────────────────────────────────────────────────────
//
// Choosing what to review is two questions — which project, then what in it —
// and one combined list made the second unreadable past a few repositories. The
// repository box filters the target box; it navigates nothing on its own.
(function picker() {
  const repo = document.getElementById('repo-filter');
  const targets = document.getElementById('target-select');
  if (!repo || !targets) return;

  // The options are removed from the DOM when filtered out rather than hidden:
  // Safari ignores `hidden` on <option>, and a filter that silently does
  // nothing on one browser is worse than no filter.
  const all = [...targets.options].map((o) => ({ option: o, repo: o.dataset.repo }));

  repo.addEventListener('change', () => {
    const want = repo.value;
    targets.textContent = '';
    for (const { option, repo: owner } of all) {
      if (!want || owner === want) targets.add(option);
    }
    targets.selectedIndex = 0;
  });
})();

// ── The rest of a diff ──────────────────────────────────────────────────────
//
// The feed shows a compacted diff, because 600 files with every line inlined is
// megabytes of HTML nobody reads. What was left out is offered as a link, and
// this turns that link into an expansion in place: the reviewer stays where they
// were, in the file they were reading, with the note box still under it.
//
// Without this the link still works — it opens the same diff as its own page.

async function expandDiff(link) {
  const box = link.closest('.post__diffbox');
  const pre = box?.querySelector('.post__diff');
  if (!box || !pre) return;

  link.textContent = 'loading…';
  link.setAttribute('aria-busy', 'true');

  let markup;
  try {
    const res = await fetch(link.href, { headers: { 'X-Msr-Fragment': '1' } });
    if (!res.ok) throw new Error(String(res.status));
    markup = await res.text();
  } catch {
    // Leave the link exactly as it was: it still goes somewhere that works.
    link.textContent = 'show the whole diff on its own page';
    link.removeAttribute('aria-busy');
    return;
  }

  const holder = document.createElement('div');
  holder.innerHTML = markup;
  const full = holder.querySelector('pre');
  if (!full) return;

  full.classList.add('post__diff', 'post__diff--full');
  pre.replaceWith(full);
  box.querySelector('.post__more')?.remove();

  // Said out loud, because the page just grew under the reader.
  const done = document.createElement('p');
  done.className = 'post__more post__more--done';
  done.textContent = 'the whole diff';
  box.append(done);
}

document.addEventListener('click', (e) => {
  const link = e.target.closest?.('[data-expand]');
  if (!link) return;
  // Cmd/ctrl-click still opens the page, which is what those mean everywhere.
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
  e.preventDefault();
  expandDiff(link);
});
