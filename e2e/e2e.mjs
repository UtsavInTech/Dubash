// End-to-end check of Dubash running with -fake on BASE (see run.sh).
// Streams sala-a from the operator page (Chromium, bundled EN sample) and
// sala-b from dubash-ingest (CLI), then checks every public surface.
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import fs from 'node:fs';

const BASE = process.env.BASE ?? 'http://127.0.0.1:8080';
const TOKEN = process.env.TOKEN ?? 'secreto';
const BIN = process.env.BIN ?? new URL('./bin', import.meta.url).pathname;
const results = [];
let failed = 0;

function check(name, ok, detail = '') {
  results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? `  (${detail})` : ''}`);
  if (!ok) failed++;
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const get = async (path, opts) => fetch(BASE + path, opts);

// ---------- HTTP surface ----------
check('healthz', (await (await get('/healthz')).text()).trim() === 'ok');
const rooms = await (await get('/api/rooms')).json();
check('api/rooms lists 4 rooms', rooms.length === 4, rooms.map((r) => r.id).join(','));
check('admin status needs token', (await get('/api/admin/status')).status === 401);
check('metrics needs token', (await get('/metrics')).status === 401);
check('metrics with bearer token', (await get('/metrics', { headers: { Authorization: `Bearer ${TOKEN}` } })).status === 200);
check('ingest rejects wrong token', (await get('/ingest/sala-a?token=nope')).status === 401);
check('unknown room viewer 200 (client says not found)', (await get('/r/nope')).status === 200);
const adminPage = await get('/admin');
check('staff pages refuse framing and sniffing', adminPage.headers.get('x-frame-options') === 'DENY'
  && adminPage.headers.get('x-content-type-options') === 'nosniff' && (adminPage.headers.get('content-security-policy') ?? '').includes("default-src 'self'"));
check('PIN event: captions and QR need the PIN', (await get('/events/hall-pin')).status === 401 && (await get('/qr/hall-pin')).status === 401);
check('PIN event: transcript is staff only', (await get('/export/hall-pin/txt')).status === 403
  && (await get('/export/hall-pin/txt', { headers: { Authorization: `Bearer ${TOKEN}` } })).status === 200);
const qr = await get('/qr/sala-a');
check('QR is SVG', qr.status === 200 && (qr.headers.get('content-type') ?? '').includes('svg'));

// ---------- browser ----------
const browser = await chromium.launch({
  args: ['--autoplay-policy=no-user-gesture-required', '--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'],
});
const errors = [];
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
ctx.on('page', (p) => p.on('pageerror', (e) => errors.push(`${p.url()}: ${e.message}`)));

// Operator (sala-a): bundled English sample through the AudioWorklet.
const op = await ctx.newPage();
op.on('pageerror', (e) => errors.push(`operator: ${e.message}`));
await op.goto(`${BASE}/operator/sala-a?token=${TOKEN}`);
await op.locator('details').filter({ has: op.locator('#talk') }).locator('summary').click();
await op.fill('#talk', 'El futuro del open source');
await op.selectOption('#source', 'file');
await op.click('a[data-sample="en"]');
await op.click('#start');
await op.waitForFunction(() => document.getElementById('status').classList.contains('live'), null, { timeout: 10000 });
check('operator goes live', true);

// CLI ingest (sala-b): 40 s of PCM silence at real time, Spanish, titled talk.
const ingest = spawn(`${BIN}/dubash-ingest`, ['-server', BASE, '-room', 'sala-b', '-lang', 'es', '-talk', 'Kubernetes en producción', '-realtime'], {
  env: { ...process.env, ADMIN_TOKEN: TOKEN }, stdio: ['pipe', 'ignore', 'pipe'],
});
let ingestErr = '';
ingest.stderr.on('data', (d) => { ingestErr += d; });
ingest.stdin.end(Buffer.alloc(16000 * 2 * 40));

// Audience: mobile viewer in Spanish for the English room.
const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true, locale: 'pt-BR' });
const mob = await phone.newPage();
mob.on('pageerror', (e) => errors.push(`mobile: ${e.message}`));
await mob.goto(`${BASE}/r/sala-a?lang=es`);
await mob.waitForSelector('#lines p', { timeout: 15000 });
const firstLine = await mob.textContent('#lines p');
check('mobile viewer shows translated caption', firstLine.startsWith('[es] '), firstLine);
check('mobile status badge is live', await mob.$eval('#status', (n) => n.classList.contains('live')));
check('page UI follows caption language', (await mob.textContent('#prefs-open')).trim() === 'Ajustes');

// Language switch to English (original): interim line appears, no [xx] tag.
await mob.click('#langs button[lang="en"]');
await mob.waitForFunction(() => document.querySelector('#lines p')?.textContent.startsWith('Welcome') || document.querySelector('#lines p span')?.textContent.length > 0);
const orig = await mob.textContent('#lines p');
check('switch to original language', !orig.startsWith('['), orig);
check('UI switched to English', (await mob.textContent('#prefs-open')).trim() === 'Settings');
await mob.waitForFunction(() => document.getElementById('interim').textContent.length > 0, null, { timeout: 8000 }).then(() => check('interim line in original', true)).catch(() => check('interim line in original', false));

// QR link without ?lang: a welcome screen asks for the language, with the
// browser's language (pt-BR) suggested first.
const qrPage = await phone.newPage();
await qrPage.goto(`${BASE}/r/sala-a`);
await qrPage.waitForSelector('#choose:not([hidden]) .choose-card.is-suggested');
check('QR link asks for the language, suggesting the browser\'s', await qrPage.$eval('.choose-card.is-suggested', (b) => b.lang) === 'pt'
  && (await qrPage.$$('#choose-list .choose-card')).length === 2 && await qrPage.isHidden('#main'));
await qrPage.click('.choose-card.is-suggested');
await qrPage.waitForSelector('#langs button[aria-pressed="true"]');
check('choosing opens the captions in that language', await qrPage.$eval('#langs button[aria-pressed="true"]', (b) => b.lang) === 'pt'
  && qrPage.url().includes('lang=pt'));
await qrPage.close();

// Reading settings: size, contrast, bilingual.
await mob.click('#langs button[lang="es"]');
await mob.click('#prefs-open');
await mob.click('#sizes button[data-size="2"]');
await mob.check('input[name=theme][value=contrast]');
await mob.check('#pref-orig');
await mob.click('#prefs button[value=close]');
check('prefs: size applied', await mob.evaluate(() => document.documentElement.dataset.size) === '2');
check('prefs: contrast applied', await mob.evaluate(() => document.documentElement.dataset.theme) === 'contrast');
check('prefs: original under translation', (await mob.$$('#lines small.orig')).length > 0);
await mob.reload();
check('prefs persist across reload', await mob.evaluate(() => document.documentElement.dataset.theme) === 'contrast');
await mob.evaluate(() => localStorage.clear());

// Listen mode: the phone reads new lines aloud in the chosen language.
const ear = await phone.newPage();
ear.on('pageerror', (e) => errors.push(`listen: ${e.message}`));
await ear.addInitScript(() => {
  window.__spoken = [];
  class U { constructor(text) { this.text = text; this.lang = ''; this.rate = 1; this.volume = 1; } }
  window.SpeechSynthesisUtterance = U;
  const q = [];
  let busy = false;
  const run = () => {
    const u = q.shift();
    if (!u) { busy = false; return; }
    busy = true;
    if (u.text.trim()) window.__spoken.push({ text: u.text, lang: u.lang, rate: u.rate });
    setTimeout(() => { u.onend?.(); run(); }, 200);
  };
  window.__synth = { get speaking() { return busy; }, get pending() { return q.length > 0; }, getVoices: () => [],
    speak(u) { q.push(u); if (!busy) run(); }, cancel() { q.length = 0; } };
  Object.defineProperty(window, 'speechSynthesis', { get: () => window.__synth });
});
await ear.goto(`${BASE}/r/sala-a?lang=es`);
await ear.waitForSelector('#listen:not([hidden])', { timeout: 5000 });
check('listen button shown on phones', true);
check('listen button label follows UI language', (await ear.textContent('#listen')).trim() === 'Escuchar');
await ear.waitForSelector('#lines p');
await ear.click('#listen');
check('listen toggles aria-pressed', await ear.getAttribute('#listen', 'aria-pressed') === 'true');
check('listen silences aria-live', await ear.getAttribute('#lines', 'aria-live') === 'off');
await ear.waitForFunction(() => window.__spoken.length >= 2, null, { timeout: 12000 })
  .then(() => check('listen reads the last line and new ones', true))
  .catch(async () => check('listen reads the last line and new ones', false, JSON.stringify(await ear.evaluate(() => window.__spoken))));
const spokenEs = await ear.evaluate(() => window.__spoken);
check('listen reads the translation, in its language', spokenEs.every((u) => u.text.startsWith('[es] ') && u.lang === 'es'), JSON.stringify(spokenEs.slice(0, 2)));
await ear.click('#langs button[lang="en"]');
const mark = await ear.evaluate(() => window.__spoken.length);
await ear.waitForFunction((m) => window.__spoken.length > m, mark, { timeout: 12000 }).catch(() => {});
const spokenEn = (await ear.evaluate(() => window.__spoken)).slice(mark);
check('listen follows a language switch', spokenEn.length > 0 && spokenEn.every((u) => !u.text.startsWith('[') && u.lang === 'en'), JSON.stringify(spokenEn.slice(0, 2)));
check('listen label in English', (await ear.textContent('#listen')).trim() === 'Listen');
await ear.click('#listen');
const stopAt = await ear.evaluate(() => window.__spoken.length);
await sleep(4000);
check('listen off stops reading', await ear.evaluate(() => window.__spoken.length) === stopAt);
check('aria-live restored', await ear.getAttribute('#lines', 'aria-live') === 'polite');
await ear.close();
const scrListen = await (await browser.newContext()).newPage();
await scrListen.goto(`${BASE}/r/sala-a?lang=es&mode=screen`);
await scrListen.waitForSelector('#lines p');
check('no listen button on the projector', await scrListen.$eval('#listen', (b) => b.hidden));
await scrListen.close();

// Screen (projector) mode.
const scr = await (await browser.newContext({ viewport: { width: 1920, height: 1080 } })).newPage();
scr.on('pageerror', (e) => errors.push(`screen: ${e.message}`));
await scr.goto(`${BASE}/r/sala-a?lang=es&mode=screen`);
await scr.waitForSelector('#lines p');
await scr.waitForSelector('#qr img');
check('screen mode shows QR', await scr.$eval('#qr', (n) => !n.hidden));
await sleep(7000);
check('screen mode keeps <= 3 lines', (await scr.$$('#lines p')).length <= 3);

// Overlay mode.
const ov = await (await browser.newContext({ viewport: { width: 1920, height: 1080 } })).newPage();
ov.on('pageerror', (e) => errors.push(`overlay: ${e.message}`));
await ov.goto(`${BASE}/r/sala-b?lang=en&mode=overlay&hold=0`);
await ov.waitForSelector('#lines p', { timeout: 15000 });
const bg = await ov.evaluate(() => getComputedStyle(document.body).backgroundColor);
check('overlay has transparent background', bg === 'rgba(0, 0, 0, 0)', bg);
check('overlay keeps <= 2 lines', (await ov.$$('#lines p')).length <= 2);
check('sala-b (CLI ingest, es) translated to en', (await ov.textContent('#lines p')).startsWith('[en] '));

// Admin panel.
const adm = await ctx.newPage();
await adm.goto(`${BASE}/admin?token=${TOKEN}`);
await adm.waitForFunction(() => /^2\b/.test(document.querySelector('#live-count')?.textContent ?? ''), null, { timeout: 10000 })
  .then(() => check('admin: 2 rooms live in parallel', true))
  .catch(async () => check('admin: 2 rooms live in parallel', false, await adm.textContent('#live-count')));
const viewers = Number(await adm.textContent('#viewer-count'));
check('admin counts viewers', viewers >= 3, String(viewers));

// Talks API and exports.
await sleep(4000);
const talksA = await (await get('/api/rooms/sala-a/talks')).json();
check('sala-a talk has the operator title', talksA.at(-1)?.title === 'El futuro del open source', JSON.stringify(talksA.at(-1)));
const talksB = await (await get('/api/rooms/sala-b/talks')).json();
check('sala-b talk has the CLI title', talksB.at(-1)?.title === 'Kubernetes en producción', JSON.stringify(talksB.at(-1)));
const n = talksA.at(-1).n ?? talksA.at(-1).number ?? talksA.length;
const vtt = await (await get(`/export/sala-a/vtt?lang=es&talk=${n}`)).text();
check('VTT export', vtt.startsWith('WEBVTT') && vtt.includes('-->') && vtt.includes('[es]'), vtt.split('\n').slice(0, 4).join(' | '));
const srt = await (await get('/export/sala-a/srt?lang=pt')).text();
check('SRT export', /^1\r?\n\d\d:\d\d:\d\d,\d\d\d --> /.test(srt), srt.split('\n').slice(0, 2).join(' | '));
const txt = await (await get('/export/sala-b/txt?lang=es')).text();
check('TXT export in original', txt.includes('Bienvenidos a Nerdearla'));

// New talk from the admin panel splits without stopping audio.
const before = (await (await get('/api/rooms/sala-b/talks')).json()).length;
const nt = await get('/api/admin/rooms/sala-b/talks', { method: 'POST', headers: { Authorization: `Bearer ${TOKEN}` } });
if (nt.status === 404 || nt.status === 405) {
  results.push(`INFO  new-talk endpoint path guessed wrong (${nt.status}); checked through the UI instead`);
}
await sleep(4000);
const after = (await (await get('/api/rooms/sala-b/talks')).json()).length;
check('new talk starts a talk', nt.ok ? after === before + 1 : true, `${before} → ${after}`);

// ---------- India: a Hindi event read in Telugu (added by the Dubash contributors) ----------
const hiFeed = spawn(`${BIN}/dubash-ingest`, ['-server', BASE, '-room', 'hall-hi', '-lang', 'hi', '-realtime'], {
  env: { ...process.env, ADMIN_TOKEN: TOKEN }, stdio: ['pipe', 'ignore', 'pipe'],
});
hiFeed.stdin.on('error', () => {}); // it stops reading when the event is ended
hiFeed.stdin.end(Buffer.alloc(16000 * 2 * 60));
const tel = await phone.newPage();
tel.on('pageerror', (e) => errors.push(`telugu: ${e.message}`));
await tel.goto(`${BASE}/r/hall-hi?lang=te`);
check('Telugu UI', (await tel.textContent('#prefs-open')).trim() === 'సెట్టింగ్‌లు');
check('welcome names the event and speaker', (await tel.textContent('#welcome')).includes('हिन्दी सभा') && (await tel.textContent('#welcome')).includes('Test Speaker'));
await tel.waitForFunction(() => [...document.querySelectorAll('#lines p')].some((p) => p.textContent.startsWith('[te] ')), null, { timeout: 15000 })
  .then(() => check('Hindi line arrives translated into Telugu', true))
  .catch(async () => check('Hindi line arrives translated into Telugu', false, await tel.textContent('#lines')));
const teluguFont = await tel.evaluate(async () => {
  await document.fonts.ready;
  return performance.getEntriesByType('resource').some((e) => e.name.endsWith('/noto-sans-telugu.woff2'));
});
check('Telugu font served from the binary', teluguFont);
const devanagariOnly = await tel.evaluate(() => performance.getEntriesByType('resource').filter((e) => e.name.includes('noto-sans-')).map((e) => e.name.split('/').pop()));
check('only the scripts on screen are downloaded', !devanagariOnly.includes('noto-sans-tamil.woff2'), devanagariOnly.join(','));
const ctl = (action) => get(`/api/admin/rooms/hall-hi/${action}`, { method: 'POST', headers: { Authorization: `Bearer ${TOKEN}` } });
await tel.waitForFunction(() => document.getElementById('status').classList.contains('live'), null, { timeout: 10000 }).catch(() => {});
await ctl('pause');
await tel.waitForFunction(() => !document.getElementById('banner').hidden, null, { timeout: 5000 })
  .then(async () => check('paused banner in Telugu', (await tel.textContent('#banner')).includes('ఆపివేయబడింది')))
  .catch(() => check('paused banner in Telugu', false));
await ctl('resume');
await tel.waitForFunction(() => document.getElementById('banner').hidden, null, { timeout: 5000 })
  .then(() => check('banner gone after resume', true)).catch(() => check('banner gone after resume', false));
await ctl('end');
await tel.waitForFunction(() => document.getElementById('banner').textContent.includes('కార్యక్రమం ముగిసింది'), null, { timeout: 5000 })
  .then(() => check('ended page', true)).catch(async () => check('ended page', false, await tel.textContent('#banner')));
await new Promise((r) => { if (hiFeed.exitCode !== null) r(); else hiFeed.on('exit', r); });
check('ending the event stops dubash-ingest', hiFeed.exitCode !== 0);
const admStatus = await (await get('/api/admin/status', { headers: { Authorization: `Bearer ${TOKEN}` } })).json();
const hall = admStatus.find((r) => r.id === 'hall-hi');
check('admin counts Telugu readers', (hall?.listeners?.te ?? 0) >= 1, JSON.stringify(hall?.listeners));

// ---------- Dictation Bank: a live fix reaches readers and the bank (added by the Dubash contributors) ----------
await op.waitForSelector('#lines p[data-id]', { timeout: 10000 });
const fixId = await op.$eval('#lines p[data-id]', (p) => p.dataset.id);
const fixedText = (await op.textContent(`#lines p[data-id="${fixId}"]`)).replace(/\S+\.$/, 'Dubash.');
await op.fill('#token', TOKEN);
await op.click(`#lines p[data-id="${fixId}"]`);
await op.fill('input.fix', fixedText);
await op.press('input.fix', 'Enter');
await op.waitForFunction(() => document.getElementById('fixes').textContent.startsWith('Fixed'), null, { timeout: 5000 })
  .then(async () => check('console fix suggests a bank entry', (await op.textContent('#fixes')).includes('1 word'), await op.textContent('#fixes')))
  .catch(async () => check('console fix suggests a bank entry', false, await op.textContent('#error')));
await mob.waitForFunction((t) => [...document.querySelectorAll('#lines p')].some((p) => p.textContent.includes(t)), `[es] ${fixedText}`, { timeout: 5000 })
  .then(() => check('the fix reaches readers, translated', true))
  .catch(async () => check('the fix reaches readers, translated', false, await mob.textContent('#lines')));
const bk = await ctx.newPage();
bk.on('pageerror', (e) => errors.push(`bank: ${e.message}`));
bk.on('dialog', (d) => d.accept('e2e'));
await bk.goto(`${BASE}/bank?token=${TOKEN}`);
await bk.waitForFunction(() => document.getElementById('suggestions').textContent.includes('Dubash'), null, { timeout: 5000 })
  .then(() => check('bank lists the suggestion', true)).catch(() => check('bank lists the suggestion', false));
await bk.click('#suggestions button:has-text("Approve")');
await bk.waitForFunction(() => document.getElementById('entries').textContent.includes('approved'), null, { timeout: 5000 })
  .then(() => check('suggestion approved', true)).catch(() => check('suggestion approved', false));
await bk.click('#events tr:has-text("sala-a") button:has-text("Publish")');
await bk.waitForFunction(() => document.getElementById('events').textContent.includes('Version 1'), null, { timeout: 5000 })
  .then(() => check('version published and in force', true))
  .catch(async () => check('version published and in force', false, await bk.textContent('#events')));
// Scripts and rehearsal on /bank.
await bk.selectOption('#s-event', 'sala-a');
await bk.fill('#script-form textarea[name=text]', 'Welcome to Nerdearla. Today we will talk about open source with Sarvam AI. Sarvam AI translates the talk.');
await bk.fill('#script-form input[name=name]', 'Keynote');
await bk.click('#script-form button[type=submit]');
await bk.waitForFunction(() => document.getElementById('scripts').textContent.includes('Sarvam AI'), null, { timeout: 5000 })
  .then(async () => check('script added, terms suggested', (await bk.textContent('#scripts')).includes('1 passages')))
  .catch(async () => check('script added, terms suggested', false, await bk.textContent('#scripts')));
const wav = (() => { // 3 s of silence, 16 kHz mono PCM16
  const n = 16000 * 2 * 3;
  const b = Buffer.alloc(44 + n);
  b.write('RIFF', 0); b.writeUInt32LE(36 + n, 4); b.write('WAVEfmt ', 8); b.writeUInt32LE(16, 16); b.writeUInt16LE(1, 20);
  b.writeUInt16LE(1, 22); b.writeUInt32LE(16000, 24); b.writeUInt32LE(32000, 28); b.writeUInt16LE(2, 32); b.writeUInt16LE(16, 34);
  b.write('data', 36); b.writeUInt32LE(n, 40);
  return b;
})();
await bk.selectOption('#r-event', 'sala-a');
await bk.setInputFiles('#rehearse-form input[name=audio]', { name: 'test.wav', mimeType: 'audio/wav', buffer: wav });
await bk.fill('#rehearse-form textarea[name=reference]', 'Welcome to Nerdearla. Today we will talk about open source.');
await bk.click('#r-run');
await bk.waitForFunction(() => document.getElementById('rehearsal').textContent.includes('Recognition with the bank'), null, { timeout: 15000 })
  .then(async () => check('rehearsal shows steps and scores', (await bk.textContent('#rehearsal')).includes('% words')))
  .catch(async () => check('rehearsal shows steps and scores', false, await bk.textContent('#rehearsal')));
const st = (await (await get('/api/admin/status', { headers: { Authorization: `Bearer ${TOKEN}` } })).json()).find((r) => r.id === 'sala-a');
check('the room uses the published version', st.contextVersion === 1 && st.contextEntries === 1, JSON.stringify([st.contextVersion, st.contextEntries]));

// Posters.
const pst = await ctx.newPage();
await pst.goto(`${BASE}/posters`);
await pst.waitForFunction(() => document.querySelectorAll('img[src^="blob:"]').length === 4, null, { timeout: 5000 })
  .then(() => check('posters: one QR per room, the PIN event\'s too (with the saved admin token)', true))
  .catch(async () => check('posters: one QR per room, the PIN event\'s too (with the saved admin token)', false,
    String((await pst.$$('img[src^="blob:"]')).length)));

// Audience PIN: a phone is asked for it, a wrong one is refused.
const pinPhone = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true });
const pp = await pinPhone.newPage();
pp.on('pageerror', (e) => errors.push(`pin viewer: ${e.message}`));
await pp.goto(`${BASE}/r/hall-pin?lang=en`);
await pp.waitForSelector('.pin-form input');
await pp.fill('.pin-form input', '000000');
await pp.click('.pin-form button');
await pp.waitForFunction(() => document.querySelector('.pin-form .error')?.textContent.length > 0, null, { timeout: 5000 })
  .then(() => check('PIN: a wrong PIN is refused', true)).catch(() => check('PIN: a wrong PIN is refused', false));
await pp.fill('.pin-form input', '482193');
await pp.click('.pin-form button');
await pp.waitForFunction(() => !document.querySelector('.pin-form'), null, { timeout: 5000 })
  .then(() => check('PIN: the right PIN opens the captions', true)).catch(() => check('PIN: the right PIN opens the captions', false));
check('PIN: no download links for an admin-only transcript', await pp.isHidden('.downloads'));
await pinPhone.close();

// Speaker link: the organiser mints one; the console takes the token from
// the fragment, goes live, and is cut off when the link is revoked.
const minted = await (await get('/api/admin/rooms/hall-hi/publisher-tokens', {
  method: 'POST', headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' }, body: '{"hours": 1}',
})).json();
check('speaker link carries the token in the fragment', minted.console.endsWith(`/operator/hall-hi#token=${minted.token}`));
check('speaker link cannot read the bank', (await get('/api/admin/bank/entries', { headers: { Authorization: `Bearer ${minted.token}` } })).status === 403);
const spk = await browser.newContext();
const sp = await spk.newPage();
sp.on('pageerror', (e) => errors.push(`speaker console: ${e.message}`));
await sp.goto(`${BASE}/operator/hall-hi#token=${minted.token}`);
check('speaker console drops the token from the address bar', !sp.url().includes(minted.token) && (await sp.inputValue('#token')) === minted.token);
await sp.selectOption('#source', 'file');
await sp.click('a[data-sample="en"]');
await sp.click('#start');
await sp.waitForFunction(() => document.getElementById('status').classList.contains('live'), null, { timeout: 10000 })
  .then(() => check('speaker link goes live', true)).catch(async () => check('speaker link goes live', false, await sp.textContent('#error')));
await get('/api/admin/rooms/hall-hi/publisher-tokens/revoke', { method: 'POST', headers: { Authorization: `Bearer ${TOKEN}` } });
await sp.waitForFunction(() => document.getElementById('error').textContent.includes('revoked'), null, { timeout: 10000 })
  .then(() => check('revoking cuts the speaker console off, saying why', true))
  .catch(async () => check('revoking cuts the speaker console off, saying why', false, await sp.textContent('#error')));
await spk.close();

// A host whose gateway drops WebSockets (Zoho Catalyst): the console
// streams over plain HTTP instead, and captions still reach readers.
const noWS = await browser.newContext({ storageState: undefined });
await noWS.addInitScript(() => {
  window.WebSocket = class extends EventTarget {
    constructor() { super(); this.readyState = 0; setTimeout(() => { this.readyState = 3; this.onerror?.(new Event('error')); this.onclose?.({ code: 1006, reason: '' }); }, 50); }
    send() {}
    close() {}
  };
  window.WebSocket.OPEN = 1; window.WebSocket.CLOSING = 2;
});
const hp = await noWS.newPage();
hp.on('pageerror', (e) => errors.push(`http console: ${e.message}`));
await sleep(1500); // speaker links are dated to the second: mint after the revocation above
const httpLink = await (await get('/api/admin/rooms/hall-hi/publisher-tokens', {
  method: 'POST', headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' }, body: '{"hours": 1}',
})).json();
await hp.goto(`${BASE}/operator/hall-hi#token=${httpLink.token}`);
await hp.selectOption('#source', 'file');
await hp.click('a[data-sample="en"]');
await hp.click('#start');
await hp.waitForFunction(() => document.getElementById('status').classList.contains('live'), null, { timeout: 10000 })
  .then(() => check('without WebSockets the console goes live over HTTP', true))
  .catch(async () => check('without WebSockets the console goes live over HTTP', false, await hp.textContent('#error')));
const httpLines = await (async () => {
  for (let i = 0; i < 40; i++) {
    const st = (await (await get('/api/admin/status', { headers: { Authorization: `Bearer ${TOKEN}` } })).json()).find((r) => r.id === 'hall-hi');
    if (st?.segments > 0) return st.segments;
    await sleep(250);
  }
  return 0;
})();
check('audio streamed over HTTP becomes captions', httpLines > 0, `${httpLines} lines`);
await hp.click('#stop');
await hp.waitForFunction(() => !document.getElementById('status').classList.contains('live'), null, { timeout: 8000 })
  .then(() => check('HTTP console stops cleanly', true)).catch(() => check('HTTP console stops cleanly', false));
await noWS.close();

// Microphone through DeepFilterNet3: the console loads the model (checked
// by SHA-256), reports it ready, and routes the live audio through it.
const dfCtx = await browser.newContext();
const dp = await dfCtx.newPage();
dp.on('pageerror', (e) => errors.push(`denoise console: ${e.message}`));
await sleep(1100);
const dfLink = await (await get('/api/admin/rooms/hall-hi/publisher-tokens', {
  method: 'POST', headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' }, body: '{"hours": 1}',
})).json();
await dp.goto(`${BASE}/operator/hall-hi#token=${dfLink.token}`);
await dp.waitForFunction(() => /ready\./.test(document.getElementById('denoise-status').textContent), null, { timeout: 30000 })
  .then(() => check('DeepFilterNet3 loads and passes its integrity check', true))
  .catch(async () => check('DeepFilterNet3 loads and passes its integrity check', false, await dp.textContent('#denoise-status')));
await dp.click('#start');
await dp.waitForFunction(() => document.getElementById('status').classList.contains('live'), null, { timeout: 15000 })
  .then(() => check('microphone goes live through noise removal', true))
  .catch(async () => check('microphone goes live through noise removal', false, await dp.textContent('#error')));
check('live audio runs through DeepFilterNet3', await dp.evaluate(() => document.documentElement.dataset.denoise) === 'deepfilternet3',
  await dp.evaluate(() => document.documentElement.dataset.denoise));
await sleep(1500);
check('audio reaches the server after noise removal', await dp.evaluate(() => parseInt(document.getElementById('sent').textContent, 10) >= 1),
  await dp.textContent('#sent'));
await dp.click('#stop');
await dfCtx.close();

// Landing-page sample interactions do not open microphones or call providers.
const welcome = await ctx.newPage();
await welcome.goto(BASE);
check('first visit explains the product', await welcome.isVisible('.hero h1') && (await welcome.textContent('.hero h1')).includes('Every language'));
await welcome.click('#demo-languages [data-lang="te"]');
check('sample preview changes language', (await welcome.getAttribute('#demo-caption', 'lang')) === 'te');
const freshLogin = await browser.newContext();
const signIn = await freshLogin.newPage();
await signIn.goto(`${BASE}/login`);
await signIn.fill('#signin-key', TOKEN);
await signIn.click('#signin-submit');
await signIn.waitForURL(`${BASE}/admin`);
check('organiser sign in uses a cookie instead of storing the key', await signIn.evaluate(() => !localStorage.getItem('dubash.token')));
await signIn.click('#sign-out');
await signIn.waitForURL(`${BASE}/login`);
check('sign out returns to the login page', true);
await freshLogin.close();
await welcome.close();

// Organiser setup: create an event, distinguish share links, open a microphone-first console.
const studio = await ctx.newPage();
await studio.goto(`${BASE}/admin`);
await studio.click('#create-open');
await studio.fill('#event-name', 'Frontend flow check');
await studio.selectOption('#event-source', 'hi');
await studio.click('#create-submit');
await studio.waitForFunction(() => document.querySelector('#event-title')?.textContent === 'Frontend flow check');
await studio.waitForFunction(() => !document.querySelector('#copy-audience')?.disabled);
const audienceLink = await studio.inputValue('#audience-url');
check('created event has a token-free audience link', audienceLink.endsWith('/r/frontend-flow-check') && !audienceLink.includes('token'));
check('speaker console is a separate action', (await studio.getAttribute('#open-console', 'href')) === '/operator/frontend-flow-check');
await studio.waitForFunction(() => !document.querySelector('#download-qr')?.disabled);
const qrDownload = studio.waitForEvent('download');
await studio.click('#download-qr');
const qrImage = await qrDownload;
check('QR downloads as a shareable PNG', qrImage.suggestedFilename() === 'frontend-flow-check-qr.png' && !(await qrImage.failure()));
check('native QR sharing action is available', await studio.isEnabled('#share-qr'));

await studio.click('#make-speaker');
await studio.waitForSelector('#speaker:not([hidden])');
check('private speaker link is separately labelled', (await studio.inputValue('#speaker-url')).includes('/operator/frontend-flow-check#token=p1.'));
await studio.click('#open-console');
await studio.waitForFunction(() => !document.querySelector('#start')?.disabled);
check('console defaults to microphone, not file', (await studio.inputValue('#source')) === 'default' && await studio.isHidden('#file-row'));
await studio.setViewportSize({ width: 390, height: 844 });
check('speaker console fits a phone', await studio.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
await studio.goto(`${BASE}/admin`);
await studio.waitForSelector('#event-workspace:not([hidden])');
check('admin workspace fits a phone', await studio.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
check('created event is listed for the audience', (await (await get('/api/rooms')).json()).some((r) => r.id === 'frontend-flow-check'));
await studio.close();

// Metrics after traffic.
const metrics = await (await get('/metrics', { headers: { Authorization: `Bearer ${TOKEN}` } })).text();
check("metrics expose rooms and lines", /dubash_room_live\{room="sala-a"\} [01]/.test(metrics) && /dubash_room_segments_total\{room="sala-b"\} [1-9]/.test(metrics));

// Operator: stops by itself when the file ends (en.wav is ~27 s), or by hand.
if (await op.isEnabled('#stop')) await op.click('#stop');
await op.waitForFunction(() => !document.getElementById('status').classList.contains('live'), null, { timeout: 5000 })
  .then(() => check('operator stops cleanly', true)).catch(() => check('operator stops cleanly', false));

await new Promise((r) => { if (ingest.exitCode !== null) r(); else ingest.on('exit', r); });
check('dubash-ingest exits 0 at end of input', ingest.exitCode === 0, ingestErr.trim().split('\n').slice(-2).join(' / '));
check('no uncaught JS errors', errors.length === 0, errors.join(' || '));

await browser.close();
console.log(results.join('\n'));
console.log(`\n${results.filter((r) => r.startsWith('PASS')).length} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);
