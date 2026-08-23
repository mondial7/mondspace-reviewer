// cockpit.js — the isometric pulse (ADR 0012, ADR 0015).
//
// An isometric grid of blocks, one per changed file, that breathes while the
// agent is working and settles when it stops. Like storyline.js it is decoration
// only: it reads the DOM and never feeds it. If WebGL is missing, Three.js fails
// to load, or the reviewer prefers reduced motion or focus mode, the module bows
// out and the cockpit's numbers and feed remain exactly as they were.

const canvas = document.getElementById('cockpit-scene');
const body = document.body;

const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// isLive is read fresh each frame: live.js swaps regions of the page as the
// session moves, so this must not be captured once at start-up.
function isLive() {
  return body.dataset.live === 'true';
}

// blockCount is one block per changed file, bounded so a 400-file session does
// not turn the pulse into a wall of geometry.
function blockCount() {
  const files = document.querySelectorAll('.post').length;
  return Math.max(1, Math.min(files || 1, 36));
}

async function start() {
  if (!canvas || prefersReducedMotion || body.classList.contains('page--focus')) return;

  let THREE;
  try {
    THREE = await import('/assets/vendor/three.module.min.js');
  } catch {
    return; // no Three.js, no animation — the page is already complete without it
  }

  let renderer;
  try {
    renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true });
  } catch {
    return; // no WebGL context (headless, blocked, or software-render disabled)
  }
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

  const scene = new THREE.Scene();

  // A true isometric look needs an orthographic camera on the (1,1,1) diagonal;
  // a perspective camera would give a 3/4 view, not an isometric one.
  const camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0.1, 100);
  camera.position.set(12, 12, 12);
  camera.lookAt(0, 0, 0);

  scene.add(new THREE.AmbientLight(0xffffff, 0.55));
  const key = new THREE.DirectionalLight(0xffffff, 0.9);
  key.position.set(6, 12, 4);
  scene.add(key);

  // Colours come from the stylesheet, so the scene follows the light/dark theme
  // instead of hard-coding a palette that only works in one of them.
  const css = getComputedStyle(document.documentElement);
  const accent = new THREE.Color(css.getPropertyValue('--accent').trim() || '#cba6f7');
  const added = new THREE.Color(css.getPropertyValue('--add').trim() || '#a6e3a1');

  const group = new THREE.Group();
  scene.add(group);

  const geometry = new THREE.BoxGeometry(1, 1, 1);
  const blocks = [];
  const n = blockCount();
  const side = Math.ceil(Math.sqrt(n));

  for (let i = 0; i < n; i++) {
    const material = new THREE.MeshLambertMaterial({
      color: i % 3 === 0 ? added : accent,
    });
    const cube = new THREE.Mesh(geometry, material);
    const x = (i % side) - (side - 1) / 2;
    const z = Math.floor(i / side) - (side - 1) / 2;
    cube.position.set(x * 1.35, 0, z * 1.35);
    // A staggered phase makes the grid ripple rather than pulse as one slab.
    cube.userData.phase = (x + z) * 0.6 + i * 0.15;
    group.add(cube);
    blocks.push(cube);
  }

  function resize() {
    const { clientWidth: w, clientHeight: h } = canvas;
    if (!w || !h) return;
    renderer.setSize(w, h, false);
    // Keep the frustum square-ish so blocks never shear when the pane resizes.
    const span = 0.55 * side * 1.35 + 2;
    const aspect = w / h;
    camera.left = -span * aspect;
    camera.right = span * aspect;
    camera.top = span;
    camera.bottom = -span;
    camera.updateProjectionMatrix();
  }
  resize();
  window.addEventListener('resize', resize);

  // Height and spin both ease toward a target rather than switching, so the
  // moment a session goes quiet reads as settling, not as a cut.
  let energy = isLive() ? 1 : 0;
  let t = 0;
  let raf = 0;

  function frame() {
    raf = requestAnimationFrame(frame);
    t += 0.016;
    energy += ((isLive() ? 1 : 0) - energy) * 0.02;

    group.rotation.y += 0.0015 + energy * 0.004;
    for (const cube of blocks) {
      const wave = Math.sin(t * 1.6 + cube.userData.phase);
      const height = 0.4 + energy * (0.6 + wave * 0.55);
      cube.scale.y = Math.max(0.12, height);
      cube.position.y = cube.scale.y / 2;
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
