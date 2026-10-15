// Screenshots of the real UI with a realistic transcript, for the promo video.
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';

const BASE = 'http://127.0.0.1:8081';
const OUT = process.env.OUT ?? new URL('../public', import.meta.url).pathname;
const BIN = process.env.BIN ?? new URL('../bin', import.meta.url).pathname;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Keep both rooms "live" without feeding the fake engine enough audio to
// emit its scripted lines: one second of PCM, then an open, idle pipe.
const live = ['sala-a', 'sala-b'].map((room, i) => {
  const p = spawn(`${BIN}/dubash-ingest`, ['-server', BASE, '-room', room, ...(i ? ['-lang', 'es'] : [])], {
    env: { ...process.env, ADMIN_TOKEN: 'secreto' }, stdio: ['pipe', 'ignore', 'ignore'],
  });
  p.stdin.on('error', () => {});
  p.stdin.write(Buffer.alloc(16000 * 2 * 1));
  return p;
});
await sleep(1500);

const browser = await chromium.launch();
const wide = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
const s = await wide.newPage();
await s.goto(`${BASE}/r/sala-a?lang=es&mode=screen`);
await s.waitForSelector('#lines p');
await s.waitForSelector('#qr p:nth-of-type(2)');
await sleep(500);
await s.screenshot({ path: `${OUT}/screen.png` });

const o = await wide.newPage();
await o.goto(`${BASE}/r/sala-b?lang=en&mode=overlay&hold=0`);
await o.waitForSelector('#lines p');
await sleep(500);
await o.screenshot({ path: `${OUT}/overlay.png`, omitBackground: true });

const desk = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
// Real audio for the control room: the bundled sample through the operator
// page for sala-a, and a steady feed from dubash-ingest for sala-b.
for (const p of live) p.kill();
await sleep(1000);
const feed = spawn(`${BIN}/dubash-ingest`, ['-server', BASE, '-room', 'sala-b', '-lang', 'es', '-realtime'], {
  env: { ...process.env, ADMIN_TOKEN: 'secreto' }, stdio: ['pipe', 'ignore', 'ignore'],
});
const tone = Buffer.alloc(16000 * 2 * 30);
for (let i = 0; i < tone.length / 2; i++) tone.writeInt16LE(Math.round(3000 * Math.sin(i / 7) * (0.6 + 0.4 * Math.sin(i / 4000))), i * 2);
feed.stdin.on('error', () => {});
feed.stdin.end(tone);
const op = await desk.newPage();
await op.goto(`${BASE}/operator/sala-a?token=secreto`);
await op.waitForSelector('#links li');
await op.fill('#talk', 'Open source para todos');
await op.click('a[data-sample="en"]');
await op.click('#start');
await op.waitForFunction(() => document.getElementById('status').classList.contains('live'));
await sleep(9000);
await op.screenshot({ path: `${OUT}/operator.png` });
const a = await desk.newPage();
await a.goto(`${BASE}/admin?token=secreto`);
await a.waitForFunction(() => /^2\b/.test(document.querySelector('#sum-live b')?.textContent ?? ''));
await sleep(2500);
await a.screenshot({ path: `${OUT}/admin.png` });
feed.kill();

await browser.close();
for (const p of live) p.kill();
console.log('done');
