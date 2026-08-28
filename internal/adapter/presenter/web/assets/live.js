// live.js — keeps an open page in sync with the review (server-sent events).
//
// The server nudges; the page re-fetches itself and swaps only the content that
// changed. Scroll position, open <details>, and anything being typed are
// preserved, because a review you are reading must not jump under you.

const stream = new EventSource('/events');

// Which parts of the page each event can affect.
//
// #refs is the picker's option list. It is swapped on its own rather than with
// the panel around it: the panel holds the isometric canvas, and replacing that
// would restart the animation every time a commit landed.
const REGIONS = ['.cockpit__story', '.cockpit__changes', '.cockpit__stats',
  '.cockpit__status', '.reviewcard', '.brief', '.board', '.activity', '.queue',
  '.storynav', '#refs', '#pending', '#analyses'];

let pending = false;

async function refresh() {
  if (pending || document.hidden) return; // don't fight the network or work unseen
  pending = true;
  try {
    const res = await fetch(window.location.href, { headers: { 'X-Requested-With': 'live' } });
    if (!res.ok) return;
    const doc = new DOMParser().parseFromString(await res.text(), 'text/html');

    // Remember what the reader has open, then restore it after the swap.
    // The post *is* the disclosure now, so its own id identifies it.
    const open = new Set(
      Array.from(document.querySelectorAll('.post[open]')).map((d) => d.id).filter(Boolean),
    );
    const typing = document.activeElement;
    const typingID = /^(INPUT|TEXTAREA)$/.test(typing?.tagName ?? '') ? typing.id : null;
    const caret = typingID ? typing.selectionStart : null;

    for (const selector of REGIONS) {
      const next = doc.querySelector(selector);
      const current = document.querySelector(selector);
      if (next && current && next.innerHTML !== current.innerHTML) {
        current.innerHTML = next.innerHTML;
      }
    }

    for (const id of open) {
      document.getElementById(id)?.setAttribute('open', '');
    }
    if (typingID) {
      const restored = document.getElementById(typingID);
      if (restored) {
        restored.focus();
        if (caret != null) restored.setSelectionRange(caret, caret);
      }
    }

    // The pending region's innerHTML is replaced wholesale, so a dismissal has
    // to be re-applied to the new markup or the banner reappears every tick.
    applyDismissal();
  } catch {
    // A failed refresh is not worth breaking the page over; the next event or a
    // manual reload will catch up.
  } finally {
    pending = false;
  }
}

for (const event of ['narrative', 'note', 'headline', 'answer', 'targets', 'pending', 'analysis', 'work']) {
  stream.addEventListener(event, refresh);
}

// ── Pulses ──────────────────────────────────────────────────────────────────
//
// A pulse is the one event that carries content: what just moved in the
// repository, in the words it will be read in. It does two things — it updates
// the page like any other event, and it says so, because a reviewer deep in a
// diff will not notice a number changing three columns away.

const TOAST_LIFE = 9000; // long enough to read a sentence and decide
const MAX_TOASTS = 4; // beyond this it stops being information

let toasts = null;

function toastHost() {
  if (!toasts) {
    toasts = document.createElement('div');
    toasts.className = 'toasts';
    // polite, not assertive: this must never interrupt a screen reader
    // mid-sentence for something the reviewer did not ask for.
    toasts.setAttribute('role', 'status');
    toasts.setAttribute('aria-live', 'polite');
    document.body.appendChild(toasts);
  }
  return toasts;
}

function dismiss(el) {
  if (!el.isConnected) return;
  el.dataset.leaving = 'true';
  setTimeout(() => el.remove(), 200);
}

function toast(pulse) {
  const host = toastHost();
  while (host.children.length >= MAX_TOASTS) host.firstElementChild.remove();

  // Clicking a pulse opens what it is talking about — unless that is already
  // what is on screen, in which case there is nowhere to go and the words are
  // the whole message.
  const here = new URL(window.location.href).searchParams.get('target');
  const goes = pulse.ref && pulse.ref !== here;

  const el = document.createElement(goes ? 'a' : 'div');
  el.className = 'toast';
  el.dataset.kind = pulse.kind || 'files';
  if (goes) el.href = `/?target=${encodeURIComponent(pulse.ref)}`;

  const text = document.createElement('span');
  text.className = 'toast__text';
  text.textContent = pulse.text; // never innerHTML: this is git data
  el.appendChild(text);

  if (goes) {
    const go = document.createElement('span');
    go.className = 'toast__go';
    go.textContent = 'open';
    el.appendChild(go);
  }

  const close = document.createElement('button');
  close.className = 'toast__close';
  close.type = 'button';
  close.setAttribute('aria-label', 'dismiss');
  close.textContent = '×';
  close.addEventListener('click', (e) => {
    e.preventDefault();
    e.stopPropagation();
    dismiss(el);
  });
  el.appendChild(close);

  host.appendChild(el);
  setTimeout(() => dismiss(el), TOAST_LIFE);
}

stream.addEventListener('pulse', (e) => {
  let pulses = [];
  try {
    pulses = JSON.parse(e.data);
  } catch {
    return; // a malformed pulse is not worth breaking the page over
  }

  // The content is the substance; the toast only points at it. Refresh either
  // way, so a page left open is correct even if nobody reads the toast.
  refresh();

  // Only announce what someone is there to see. Anything missed while the tab
  // was hidden is already in the content by the time they come back, and a
  // wall of stale toasts on return is worse than none.
  if (document.hidden) return;
  for (const p of pulses) toast(p);
});

// ── Work that arrived mid-review ────────────────────────────────────────────
//
// "Keep reading" is a real answer and the most common one, so it has to stick:
// a banner that reappears on the next two-second refresh is not dismissable,
// it is just slower. It is remembered against the sentence it dismissed, so
// the next *different* piece of news comes back.

let dismissed = null;

function pendingHead() {
  return document.querySelector('.pending__head')?.textContent.trim() ?? null;
}

function applyDismissal() {
  const banner = document.querySelector('.pending__inner');
  if (!banner) return;
  banner.hidden = pendingHead() !== null && pendingHead() === dismissed;
}

document.addEventListener('click', (e) => {
  if (!e.target.closest('[data-pending-dismiss]')) return;
  dismissed = pendingHead();
  applyDismissal();
});

// Catch up on anything missed while the tab was in the background.
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) refresh();
});
