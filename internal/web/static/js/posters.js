// Modified by the Dubash contributors: English and Indian languages.
import { NAMES, el } from './langs.js';

const only = new URLSearchParams(location.search).get('room');

// "Scan to read in your language", in the event's languages.
const SCAN = [
  ['hi', 'अपनी भाषा में पढ़ने के लिए स्कैन करें'], ['te', 'మీ భాషలో చదవడానికి స్కాన్ చేయండి'],
  ['kn', 'ನಿಮ್ಮ ಭಾಷೆಯಲ್ಲಿ ಓದಲು ಸ್ಕ್ಯಾನ್ ಮಾಡಿ'], ['ta', 'உங்கள் மொழியில் படிக்க ஸ்கேன் செய்யுங்கள்'],
];

function poster(room, base) {
  const langs = [room.source, ...room.targets];
  const url = `${base.replace(/^https?:\/\//, '')}/r/${room.id}`;
  return el('article', { className: 'poster' },
    el('div', { className: 'poster-top' },
      el('p', { className: 'poster-kicker', textContent: 'Live captions · लाइव कैप्शन' }),
      el('h2', { textContent: room.title }),
      el('p', { className: 'poster-langs', textContent: langs.map((l) => NAMES[l] ?? l).join(' · ') })),
    el('img', { src: `/qr/${room.id}`, alt: `QR code that opens ${url}`, width: 400, height: 400 }),
    el('p', { className: 'poster-cta', textContent: 'Scan to read the speaker in your language' }),
    el('p', { className: 'poster-alt', textContent: SCAN.filter(([l]) => langs.includes(l)).map(([, s]) => s).join(' · ') }),
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
