// Modified by the Dubash contributors: English text, speaker and states.
import { NAMES, el } from './langs.js';

function langItem(room, l) {
  return el('li', {},
    el('a', { className: 'lang-link', href: `/r/${room.id}?lang=${l}`, lang: l },
      el('span', {},
        el('strong', { textContent: NAMES[l] ?? l }),
        el('small', { lang: 'en', textContent: l === room.source ? 'Spoken language' : 'Automatic translation' }))));
}

function entry(room) {
  const label = room.ended ? 'Ended' : room.paused ? 'Paused' : room.live ? 'Live' : 'Not streaming';
  const status = el('span', { className: room.live && !room.paused ? 'status live' : 'status', textContent: label });
  const targets = room.targets.map((l) => NAMES[l] ?? l);
  const now = room.live && room.talkTitle ? `Now: ${room.talkTitle}. ` : '';
  const who = room.speaker ? `Speaker: ${room.speaker}. ` : '';
  const meta = `${now}${who}Spoken in ${NAMES[room.source]}` + (targets.length ? `. Translated into ${targets.join(', ')}.` : '.');
  return el('li', { className: 'room' },
    el('div', { className: 'room-head' }, el('h2', { textContent: room.title }), status),
    el('p', { className: 'room-meta', textContent: meta }),
    el('ul', { className: 'lang-list', ariaLabel: `Languages of ${room.title}` },
      ...[room.source, ...room.targets].map((l) => langItem(room, l))));
}

async function load() {
  try {
    const rooms = await (await fetch('/api/rooms')).json();
    document.getElementById('rooms').replaceChildren(...rooms.map(entry));
  } catch {
    // keep the last rendered list; the next poll retries
  }
}

load();
setInterval(load, 10000);
