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

  scene.add(new THREE.AmbientLight(0xffffff, 0.6));
  const key = new THREE.DirectionalLight(0xffffff, 0.85);
  key.position.set(8, 14, 5);
  scene.add(key);

  // Colours come from the stylesheet, so the field follows the light/dark theme
  // and matches the legend printed beneath it.
  const css = getComputedStyle(document.documentElement);
  const colour = (name, fallback) =>
    new THREE.Color(css.getPropertyValue(name).trim() || fallback);
  const palette = {
    add: colour('--add', '#a6e3a1'),
    del: colour('--del', '#f38ba8'),
    flag: colour('--inferred', '#f9e2af'),
    ctx: colour('--accent', '#cba6f7'),
  };

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


// ── Magnet scrolling ────────────────────────────────────────────────────────
//
// The story and the changes are two views of the same session, so they are kept
// in register: scrolling either one brings the other alongside. Each chapter
// knows the first unit it covers, which is what makes the correspondence real
// rather than a proportional guess — a three-line chapter and a 400-line diff
// are the same chapter, and proportional scrolling would drift immediately.
function linkColumns() {
  const story = document.getElementById('story-col');
  const changes = document.getElementById('changes-col');
  if (!story || !changes) return;

  const chapters = [...story.querySelectorAll('.chron__chapter[data-anchor]')];
  if (!chapters.length) return;

  // A scroll we caused must not bounce back and drive the other column again.
  let settling = 0;
  const nudge = (container, target) => {
    if (!target) return;
    settling = Date.now() + 500;
    container.scrollTo({
      top: target.offsetTop - container.offsetTop - 12,
      behavior: 'smooth',
    });
  };
  const quiet = () => Date.now() < settling;

  function markActive(active) {
    for (const c of chapters) {
      c.dataset.active = String(c === active);
    }
    const anchor = active && active.dataset.anchor;
    for (const p of changes.querySelectorAll('.post')) {
      p.dataset.active = String(!!anchor && p.id === 'unit-' + anchor);
    }
  }

  // Whichever chapter sits nearest the top of the story column is the one whose
  // files should be showing.
  function nearestChapter() {
    let best = chapters[0];
    let bestGap = Infinity;
    for (const c of chapters) {
      const gap = Math.abs(c.offsetTop - story.offsetTop - story.scrollTop);
      if (gap < bestGap) { bestGap = gap; best = c; }
    }
    return best;
  }

  let pending = 0;
  story.addEventListener('scroll', () => {
    if (quiet()) return;
    cancelAnimationFrame(pending);
    pending = requestAnimationFrame(() => {
      const active = nearestChapter();
      markActive(active);
      nudge(changes, document.getElementById('unit-' + active.dataset.anchor));
    });
  });

  // The reverse direction: scrolling the changes lights the chapter that covers
  // whatever file you have reached.
  const byAnchor = new Map(chapters.map((c) => ['unit-' + c.dataset.anchor, c]));
  changes.addEventListener('scroll', () => {
    if (quiet()) return;
    cancelAnimationFrame(pending);
    pending = requestAnimationFrame(() => {
      let active = null;
      for (const p of changes.querySelectorAll('.post')) {
        if (p.offsetTop - changes.offsetTop <= changes.scrollTop + 24 && byAnchor.has(p.id)) {
          active = byAnchor.get(p.id);
        }
      }
      if (active) {
        markActive(active);
        nudge(story, active);
      }
    });
  });

  markActive(chapters[0]);
}

linkColumns();
// live.js swaps whole columns in; re-link whatever replaced them.
const storyCol = document.getElementById('story-col');
if (storyCol) new MutationObserver(linkColumns).observe(storyCol, { childList: true });


// ── Full diffs, on demand ───────────────────────────────────────────────────
//
// The cockpit shows a compacted diff for every file. The whole diff is fetched
// only when asked: inlining all of them would multiply a large session's page
// several times over for content almost nobody opens.
for (const box of document.querySelectorAll('details[data-diff]')) {
  box.addEventListener('toggle', async () => {
    if (!box.open || box.dataset.loaded) return;
    box.dataset.loaded = 'yes';
    const slot = box.querySelector('.post__fulldiff');
    try {
      const res = await fetch(box.dataset.diff);
      slot.innerHTML = res.ok ? await res.text() : 'could not load this diff';
    } catch {
      slot.textContent = 'could not load this diff';
      box.dataset.loaded = '';   // let a retry happen on the next open
    }
  });
}
