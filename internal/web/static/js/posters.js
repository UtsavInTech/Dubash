import { NAMES, el } from './langs.js';

const only = new URLSearchParams(location.search).get('room');

function poster(room, base) {
  const langs = [room.source, ...room.targets];
  const url = `${base.replace(/^https?:\/\//, '')}/r/${room.id}`;
  return el('article', { className: 'poster' },
    el('div', { className: 'poster-top' },
      el('p', { className: 'poster-kicker', textContent: 'Subtítulos en vivo' }),
      el('h2', { textContent: room.title }),
      el('p', { className: 'poster-langs', textContent: langs.map((l) => NAMES[l] ?? l).join(' · ') })),
    el('img', { src: `/qr/${room.id}`, alt: `Código QR que abre ${url}`, width: 400, height: 400 }),
    el('p', { className: 'poster-cta', textContent: 'Escaneá para leer la charla en tu idioma' }),
    el('p', { className: 'poster-alt', textContent: 'Live captions in your language · Legendas ao vivo no seu idioma' }),
    el('p', { className: 'poster-url' }, el('span', { textContent: url }), el('strong', { textContent: 'Dubash' })));
}

async function main() {
  const [rooms, site] = await Promise.all([
    fetch('/api/rooms').then((r) => r.json()),
    fetch('/api/site').then((r) => r.json()),
  ]);
  document.getElementById('sheets').replaceChildren(
    ...rooms.filter((r) => !only || r.id === only).map((r) => poster(r, site.url)));
}

main();
