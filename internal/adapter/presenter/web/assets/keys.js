// keys.js — moving around a review, and between reviews, without the mouse
// (ADR 0022).
//
// The cockpit is a reading surface: a long column of files, a picker above it,
// and a workspace of other reviews behind that. Reading is the common case and
// it was the one thing that needed a mouse. These are single-key bindings
// because they are used constantly, and single keys are safe here for one
// reason — every one of them is ignored while something is being typed.

const isTyping = (el) =>
  !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable);

// ── The files in this review ────────────────────────────────────────────────

function posts() {
  return [...document.querySelectorAll('.post[data-file]')];
}

// The cursor is found from the DOM rather than remembered, so it survives the
// live refresh replacing the whole column underneath it.
function current() {
  return posts().findIndex((p) => p.dataset.cursor === 'true');
}

function focusPost(i) {
  const all = posts();
  if (!all.length) return;
  const next = Math.max(0, Math.min(i, all.length - 1));

  for (const p of all) delete p.dataset.cursor;
  const el = all[next];
  el.dataset.cursor = 'true';
  el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  // Focus the summary so Enter and screen readers agree with the highlight.
  el.querySelector('summary')?.focus({ preventScroll: true });
}

function stepPost(delta) {
  const at = current();
  focusPost(at < 0 ? (delta > 0 ? 0 : posts().length - 1) : at + delta);
}

function toggleCurrent() {
  const el = posts()[current()];
  if (!el) return;
  el.open = !el.open;
}

// ── Other reviews, and other repositories ───────────────────────────────────
//
// The picker's option list is already every review in the workspace, in order,
// carrying the repository each belongs to. Reading the navigation off it means
// there is one source of truth for what exists rather than two that can drift.

function refs() {
  return [...document.querySelectorAll('#refs option')];
}

function openRef(value) {
  if (value) window.location.href = `/?target=${encodeURIComponent(value)}`;
}

function here() {
  return new URL(window.location.href).searchParams.get('target');
}

function stepReview(delta) {
  const all = refs();
  if (!all.length) return;
  const at = all.findIndex((o) => o.value === here());
  // Not on any listed review (a compare, say): step from the top.
  const next = at < 0 ? 0 : at + delta;
  if (next < 0 || next >= all.length) return; // stop at the ends rather than wrap
  openRef(all[next].value);
}

function stepRepo(delta) {
  const all = refs();
  if (!all.length) return;

  const order = [];
  for (const o of all) {
    const repo = o.dataset.repo || '';
    if (!order.includes(repo)) order.push(repo);
  }
  if (order.length < 2) return; // one repository: nothing to switch to

  const at = all.findIndex((o) => o.value === here());
  const mine = at < 0 ? order[0] : all[at].dataset.repo || '';
  const next = order[(order.indexOf(mine) + delta + order.length) % order.length];

  // The first review of that repository, which is the newest: arriving at a
  // repository's oldest commit would be a strange place to be put.
  openRef(all.find((o) => (o.dataset.repo || '') === next)?.value);
}

// ── The cheatsheet ──────────────────────────────────────────────────────────

const KEYS = [
  ['j / k', 'next / previous file'],
  ['o  or  enter', 'open or close the file'],
  ['g / G', 'first / last file'],
  ['[  /  ]', 'previous / next review'],
  ['{  /  }', 'previous / next repository'],
  ['/', 'jump to the review picker'],
  ['a', 'ask about these changes'],
  ['r', 'mark this review done'],
  ['⌘K', 'go to anything'],
  ['⌘Z', 'zen mode'],
  ['⌘J', 'theme'],
  ['?', 'this list'],
];

let sheet = null;

function buildSheet() {
  sheet = document.createElement('div');
  sheet.className = 'palette keysheet';
  sheet.hidden = true;
  sheet.tabIndex = -1;
  sheet.innerHTML = `
    <div class="palette__sheet" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts">
      <p class="keysheet__title">Keyboard</p>
      <ul class="keysheet__list"></ul>
      <p class="keysheet__more"><a href="/tutorial">take the tour</a> — what this
        page is and how to work through it</p>
    </div>`;

  const list = sheet.querySelector('.keysheet__list');
  for (const [key, what] of KEYS) {
    const li = document.createElement('li');
    li.className = 'keysheet__row';
    li.innerHTML = `<kbd class="keysheet__key"></kbd><span class="keysheet__what"></span>`;
    li.querySelector('.keysheet__key').textContent = key;
    li.querySelector('.keysheet__what').textContent = what;
    list.appendChild(li);
  }

  sheet.addEventListener('click', (e) => {
    if (e.target === sheet) closeSheet();
  });
  // Handled on the dialog itself, so escape closes this without also reaching
  // the shell's escape and dropping the reader out of zen mode.
  sheet.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' || e.key === '?') {
      e.preventDefault();
      e.stopPropagation();
      closeSheet();
    }
  });
  document.body.appendChild(sheet);
}

function openSheet() {
  if (!sheet) buildSheet();
  sheet.hidden = false;
  sheet.focus();
}

function closeSheet() {
  if (sheet) sheet.hidden = true;
}

// The palette is the other way in, for anyone who has not learned `?` yet.
document.addEventListener('msr:shortcuts', openSheet);

// ── Bindings ────────────────────────────────────────────────────────────────

document.addEventListener('keydown', (e) => {
  // Never steal a key from a text field, and never from a shortcut the browser
  // or the shell owns.
  if (isTyping(e.target) || e.metaKey || e.ctrlKey || e.altKey) return;
  if (sheet && !sheet.hidden) return;

  switch (e.key) {
    case 'j': e.preventDefault(); stepPost(1); break;
    case 'k': e.preventDefault(); stepPost(-1); break;
    case 'o': e.preventDefault(); toggleCurrent(); break;
    case 'g': e.preventDefault(); focusPost(0); break;
    case 'G': e.preventDefault(); focusPost(posts().length - 1); break;
    // Newest first, so [ moves toward the newer end and ] toward the older,
    // matching the way [ and ] read everywhere else.
    case '[': e.preventDefault(); stepReview(-1); break;
    case ']': e.preventDefault(); stepReview(1); break;
    case '{': e.preventDefault(); stepRepo(-1); break;
    case '}': e.preventDefault(); stepRepo(1); break;
    case '?': e.preventDefault(); openSheet(); break;
    case '/':
      e.preventDefault();
      document.getElementById('target-input')?.focus();
      break;
    case 'a': {
      e.preventDefault();
      const ask = document.querySelector('.ask__input, #ask-input');
      ask?.focus();
      break;
    }
    case 'r': {
      e.preventDefault();
      // Opening the disclosure is as far as this goes: finishing a review is a
      // judgement, and a single keystroke must not be able to record one.
      const form = document.querySelector('.signoff__form');
      if (form) {
        form.open = true;
        form.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        form.querySelector('textarea')?.focus();
      }
      break;
    }
    default:
  }
});

// ── Annotating a line ───────────────────────────────────────────────────────
//
// Real review happens on lines (ADR 0028). Clicking one opens the file's own
// note form with the line already anchored, rather than inventing a second
// place to write notes: there is one form, and it learns what it is about.

function noteForm(post) {
  return post?.querySelector('form[action*="/notes"]');
}

document.addEventListener('click', (e) => {
  const line = e.target.closest('.diff__line[data-anchor]');
  if (!line) return;

  const post = line.closest('.post');
  const form = noteForm(post);
  if (!form) return;

  // Hidden fields rather than a separate endpoint: the same form, told which
  // line it is about. Submitting without clicking a line leaves them empty,
  // which is a note about the file — what every note was before this existed.
  for (const [name, value] of [['anchor', line.dataset.anchor], ['nth', line.dataset.nth]]) {
    let field = form.querySelector(`input[name="${name}"]`);
    if (!field) {
      field = document.createElement('input');
      field.type = 'hidden';
      field.name = name;
      form.appendChild(field);
    }
    field.value = value ?? '';
  }

  for (const marked of post.querySelectorAll('.diff__line--anchoring')) {
    marked.classList.remove('diff__line--anchoring');
  }
  line.classList.add('diff__line--anchoring');

  const box = form.querySelector('textarea');
  if (box) {
    box.placeholder = 'note on this line…';
    box.focus();
  }
});

// Enter on a focused line does the same, so this is reachable without a mouse.
document.addEventListener('keydown', (e) => {
  if (e.key !== 'Enter') return;
  const line = e.target.closest?.('.diff__line[data-anchor]');
  if (!line) return;
  e.preventDefault();
  line.click();
});
