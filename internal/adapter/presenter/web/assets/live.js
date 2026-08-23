// live.js — keeps an open page in sync with the review (server-sent events).
//
// The server nudges; the page re-fetches itself and swaps only the content that
// changed. Scroll position, open <details>, and anything being typed are
// preserved, because a review you are reading must not jump under you.

const stream = new EventSource('/events');

// Which parts of the page each event can affect.
const REGIONS = ['.cockpit__story', '.cockpit__changes', '.cockpit__stats',
  '.cockpit__status', '.brief', '.board', '.activity', '.queue'];

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
  } catch {
    // A failed refresh is not worth breaking the page over; the next event or a
    // manual reload will catch up.
  } finally {
    pending = false;
  }
}

for (const event of ['narrative', 'note', 'headline', 'answer']) {
  stream.addEventListener(event, refresh);
}

// Catch up on anything missed while the tab was in the background.
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) refresh();
});
