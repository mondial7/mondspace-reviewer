// storyline.js — the cinematic layer (ADR 0012).
//
// It draws *behind* the review, reading everything it needs from the DOM. If
// WebGL is missing, Three.js fails to load, or the reviewer prefers focus mode
// or reduced motion, this module bows out and the plain review remains.

const FOCUS_KEY = 'msr:focus-mode';
const body = document.body;
const toggle = document.getElementById('focus-toggle');
const canvas = document.getElementById('starfield');

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
    /* storage is a convenience, never a requirement */
  }
}

// Reduced motion wins over a stored preference: never animate at someone.
let focusMode = prefersReducedMotion || stored(FOCUS_KEY) === 'on';

function applyFocus() {
  body.classList.toggle('page--focus', focusMode);
  if (toggle) toggle.setAttribute('aria-pressed', String(focusMode));
}

function setFocus(on) {
  focusMode = on;
  remember(FOCUS_KEY, on ? 'on' : 'off');
  applyFocus();
}

applyFocus();

if (toggle) toggle.addEventListener('click', () => setFocus(!focusMode));

document.addEventListener('keydown', (e) => {
  // Never steal the key while someone is typing an annotation.
  const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName ?? '');
  if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
  if (e.key === 'f') {
    e.preventDefault();
    setFocus(!focusMode);
  }
});

// Nothing below is required for the review to work.
if (!focusMode && canvas) {
  startScene().catch(() => {
    // A scene that cannot start must leave no trace.
    canvas.remove();
  });
}

async function startScene() {
  const THREE = await import('/assets/vendor/three.module.min.js');

  const renderer = new THREE.WebGLRenderer({ canvas, alpha: true, antialias: true });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(60, 1, 0.1, 400);
  camera.position.z = 60;

  // Three parallax starfield layers: far ones drift slowly, near ones fast.
  const layers = [
    starLayer(THREE, 1400, 260, 0.35, 0.10),
    starLayer(THREE, 700, 150, 0.65, 0.25),
    starLayer(THREE, 260, 80, 1.10, 0.55),
  ];
  layers.forEach((l) => scene.add(l.points));

  // One node per unit, positioned along the session's timeline. Size follows the
  // change, colour follows the flags — weight and risk, at a glance.
  const units = Array.from(document.querySelectorAll('.unit'));
  const nodes = units.map((el, i) => {
    const added = Number(el.dataset.added || 0);
    const removed = Number(el.dataset.removed || 0);
    const flags = Number(el.dataset.flags || 0);
    const churn = added + removed;
    const size = 0.8 + Math.min(Math.log10(churn + 1) * 1.5, 4.5);

    const colour = flags > 1 ? 0xf38ba8 : flags === 1 ? 0xf9e2af : 0x89dceb;
    const mesh = new THREE.Mesh(
      new THREE.IcosahedronGeometry(size, 1),
      new THREE.MeshBasicMaterial({ color: colour, wireframe: true, transparent: true, opacity: 0.75 }),
    );
    // A gentle helix so later units recede into depth.
    const t = i / Math.max(units.length - 1, 1);
    const angle = t * Math.PI * 3;
    mesh.position.set(Math.cos(angle) * 26, 24 - t * 48, Math.sin(angle) * 26 - t * 40);
    scene.add(mesh);
    return { el, mesh, spin: 0.002 + Math.random() * 0.004 };
  });

  let pointerX = 0;
  let pointerY = 0;
  window.addEventListener('pointermove', (e) => {
    pointerX = (e.clientX / window.innerWidth - 0.5) * 2;
    pointerY = (e.clientY / window.innerHeight - 0.5) * 2;
  });

  function resize() {
    const { clientWidth: w, clientHeight: h } = document.documentElement;
    renderer.setSize(w, h, false);
    camera.aspect = w / h;
    camera.updateProjectionMatrix();
  }
  resize();
  window.addEventListener('resize', resize);

  let raf = 0;
  function frame(now) {
    raf = requestAnimationFrame(frame);
    if (focusMode) return; // stop drawing the moment focus mode is on

    const t = now * 0.001;
    layers.forEach((l) => l.update(t, pointerX, pointerY));

    // The scroll position drives the camera: scrolling *is* travelling the story.
    const max = Math.max(document.body.scrollHeight - window.innerHeight, 1);
    const progress = Math.min(window.scrollY / max, 1);
    const targetY = 20 - progress * 46;
    camera.position.y += (targetY - camera.position.y) * 0.06;
    camera.position.x += (pointerX * 6 - camera.position.x) * 0.04;
    camera.lookAt(0, camera.position.y - 8, -20);

    nodes.forEach(({ mesh, spin, el }) => {
      mesh.rotation.x += spin;
      mesh.rotation.y += spin * 1.3;
      // The unit under the reader brightens — attention follows the cursor.
      const r = el.getBoundingClientRect();
      const near = r.top < window.innerHeight * 0.6 && r.bottom > 0;
      const target = near ? 0.95 : 0.35;
      mesh.material.opacity += (target - mesh.material.opacity) * 0.08;
    });

    renderer.render(scene, camera);
  }
  raf = requestAnimationFrame(frame);

  // Free the GPU when the tab is hidden.
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      cancelAnimationFrame(raf);
    } else {
      raf = requestAnimationFrame(frame);
    }
  });
}

// starLayer builds one parallax plane of stars.
function starLayer(THREE, count, spread, speed, drift) {
  const positions = new Float32Array(count * 3);
  for (let i = 0; i < count; i++) {
    positions[i * 3] = (Math.random() - 0.5) * spread * 2;
    positions[i * 3 + 1] = (Math.random() - 0.5) * spread * 2;
    positions[i * 3 + 2] = -Math.random() * spread;
  }
  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));

  const points = new THREE.Points(
    geometry,
    new THREE.PointsMaterial({ color: 0xcdd6f4, size: speed, transparent: true, opacity: 0.55 }),
  );

  return {
    points,
    update(t, px, py) {
      points.rotation.z = t * 0.01 * speed;
      points.position.x = -px * drift * 18;
      points.position.y = py * drift * 12;
    },
  };
}
