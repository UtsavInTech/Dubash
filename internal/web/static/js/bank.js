// Copyright 2026 the Dubash contributors. Apache-2.0.
// The Dictation Bank admin page.
import { NAMES, el } from './langs.js';

const $ = (id) => document.getElementById(id);
const params = new URLSearchParams(location.search);
// Languages offered for approved translations and "said in".
const LANGS = ['en', 'hi', 'te', 'kn', 'ta', 'mr', 'bn', 'gu', 'ml', 'or', 'pa'];

function storedToken() {
  try { return localStorage.getItem('dubash.token') ?? ''; } catch { return ''; }
}
$('token').value = params.get('token') ?? storedToken();
$('token').onchange = () => { try { localStorage.setItem('dubash.token', $('token').value); } catch { /* private mode */ } refresh(); };

async function api(method, path, body) {
  const opts = { method, headers: { Authorization: `Bearer ${$('token').value.trim()}` } };
  if (body instanceof URLSearchParams) opts.body = body;
  else if (body) { opts.body = JSON.stringify(body); opts.headers['Content-Type'] = 'application/json'; }
  const res = await fetch(path, opts);
  if (!res.ok) throw new Error(res.status === 401 ? 'Invalid token.' : (await res.text()).trim() || `${res.status}`);
  return res.json();
}

function fail(e) { $('error').textContent = e.message; }

const scopeLabel = (e) => ({ event: 'Event', speaker: 'Speaker', org: 'Organisation' }[e.scope] + (e.scope === 'org' ? '' : `: ${e.scopeId}`));
const button = (label, onclick, secondary = true) => {
  const b = el('button', { type: 'button', className: `button small${secondary ? ' secondary' : ''}`, textContent: label });
  b.onclick = () => onclick().catch(fail);
  return b;
};

let rooms = [];

async function events() {
  rooms = await (await fetch('/api/rooms')).json();
  $('event-ids').replaceChildren(...rooms.map((r) => el('option', { value: r.id, textContent: r.title })));
  const rows = await Promise.all(rooms.map(async (r) => {
    const v = await api('GET', `/api/admin/events/${r.id}/context`);
    const p = v.pending;
    const changes = [p.added.length && `add ${p.added.length}`, p.changed.length && `update ${p.changed.length}`, p.removed.length && `remove ${p.removed.length}`].filter(Boolean);
    const detail = [...p.added.map((t) => `+ ${t}`), ...p.changed.map((t) => `~ ${t}`), ...p.removed.map((t) => `− ${t}`)].join(', ');
    const publish = button('Publish a new version', async () => {
      const note = prompt(`Publish the bank for ${r.title}? It applies from the next line. Note (optional):`, '');
      if (note === null) return;
      await api('POST', `/api/admin/events/${r.id}/context`, new URLSearchParams({ note }));
      refresh();
    }, false);
    const older = v.versions.filter((x) => x.number !== v.inForce).slice(0, 3).map((x) =>
      button(`Use v${x.number}`, async () => {
        if (!confirm(`Put version ${x.number} of ${r.title} back in force?`)) return;
        await api('POST', `/api/admin/events/${r.id}/context/${x.id}/pin`);
        refresh();
      }));
    return el('tr', {},
      el('th', { scope: 'row' }, r.title, el('span', { className: 'sub', textContent: `${r.id}${r.speaker ? ` · ${r.speaker}` : ''}` })),
      el('td', {}, v.inForce ? `Version ${v.inForce}` : 'None', el('span', { className: 'sub', textContent: v.pinned ? `${v.pinned.entries.length} entries · ${new Date(v.pinned.createdAt).toLocaleString()}` : '' })),
      el('td', {}, changes.length ? changes.join(', ') : 'Nothing: up to date', el('span', { className: 'sub', textContent: detail })),
      el('td', { className: 'links' }, publish, ' ', ...older.flatMap((b) => [b, ' '])));
  }));
  $('events').replaceChildren(...rows);
}

function entryRow(e, actions) {
  const also = [...(e.aliases ?? []), ...(e.misheard ?? []).map((m) => `✗ ${m}`)].join(' · ');
  const tr = Object.entries(e.translations ?? {}).map(([l, t]) => el('span', { className: 'sub', lang: l, textContent: `${l}: ${t}` }));
  return el('tr', {},
    el('th', { scope: 'row' }, el('span', { lang: e.lang || undefined, textContent: e.term }),
      el('span', { className: 'sub', textContent: [e.keep && 'never translate', e.ambiguous && 'ambiguous', e.meaning].filter(Boolean).join(' · ') })),
    el('td', { textContent: e.lang ? (NAMES[e.lang] ?? e.lang) : 'Any' }),
    el('td', { textContent: also }),
    el('td', {}, ...tr),
    el('td', { textContent: scopeLabel(e) }),
    el('td', { textContent: `${e.status} · v${e.version}` }),
    el('td', { className: 'links' }, ...actions));
}

async function suggestions() {
  const list = await api('GET', '/api/admin/bank/entries?status=suggested');
  $('sug-count').textContent = list.length ? `(${list.length})` : '';
  $('suggestions').replaceChildren(...list.map((e) => el('tr', {},
    el('th', { scope: 'row', lang: e.lang || undefined, textContent: e.term }),
    el('td', { textContent: (e.misheard ?? []).join(' · ') }),
    el('td', { textContent: scopeLabel(e) }),
    el('td', { textContent: e.note || e.source }),
    el('td', { className: 'links' },
      button('Approve', async () => { await api('POST', `/api/admin/bank/entries/${e.id}/approve`); refresh(); }, false), ' ',
      button('Edit', async () => edit(e)), ' ',
      button('Reject', async () => { await api('POST', `/api/admin/bank/entries/${e.id}/reject`); refresh(); })))));
  if (!list.length) $('suggestions').replaceChildren(el('tr', {}, el('td', { colSpan: 5, className: 'hint', textContent: 'No suggestions.' })));
}

async function entries() {
  const q = new URLSearchParams();
  for (const [k, id] of [['scope', 'f-scope'], ['status', 'f-status'], ['q', 'f-q']]) if ($(id).value) q.set(k, $(id).value);
  let list = await api('GET', `/api/admin/bank/entries?${q}`);
  if (!$('f-status').value) list = list.filter((e) => e.status === 'approved' || e.status === 'suggested');
  $('entries').replaceChildren(...list.map((e) => entryRow(e, [
    button('Edit', async () => edit(e)), ' ',
    e.status !== 'retired' ? button('Retire', async () => {
      if (!confirm(`Retire "${e.term}"? It stops applying once the event's next version is published.`)) return;
      await api('POST', `/api/admin/bank/entries/${e.id}/retire`);
      refresh();
    }) : '',
  ])));
}

async function corrections() {
  const list = (await api('GET', '/api/admin/bank/corrections')).slice(-15).reverse();
  $('corrections').replaceChildren(...list.map((c) => el('li', {},
    el('span', { lang: c.lang, textContent: `${c.raw} → ${c.corrected}` }),
    el('small', { textContent: `${c.event}, line ${c.segment}, ${new Date(c.at).toLocaleTimeString()}${c.suggested?.length ? `, ${c.suggested.length} suggested` : ''}` }))));
}

// ---------- the entry form ----------
const form = $('entry-form');
form.elements.lang.replaceChildren(el('option', { value: '', textContent: 'Any language' }), ...LANGS.map((l) => el('option', { value: l, textContent: NAMES[l] ?? l })));
$('translations').replaceChildren(...LANGS.map((l) => el('label', { className: 'field' },
  el('span', { textContent: NAMES[l] ?? l }), el('input', { name: `tr-${l}`, lang: l, maxLength: 120 }))));
const lines = (s) => s.split(/\n|,/).map((x) => x.trim()).filter(Boolean);

function edit(e = { scope: 'event', scopeId: rooms[0]?.id ?? '', status: 'approved', lang: 'hi' }) {
  const f = form.elements;
  f.id.value = e.id ?? '';
  f.term.value = e.term ?? '';
  f.lang.value = e.lang ?? '';
  f.scope.value = e.scope;
  f.scopeId.value = e.scope === 'org' ? '' : e.scopeId ?? '';
  f.aliases.value = (e.aliases ?? []).join('\n');
  f.misheard.value = (e.misheard ?? []).join('\n');
  f.pronunciation.value = e.pronunciation ?? '';
  f.meaning.value = e.meaning ?? '';
  for (const l of LANGS) f[`tr-${l}`].value = e.translations?.[l] ?? '';
  f.keep.checked = !!e.keep;
  f.ambiguous.checked = !!e.ambiguous;
  f.status.value = e.status === 'suggested' ? 'approved' : e.status; // editing a suggestion approves it
  $('editor-title').textContent = e.id ? `Edit “${e.term}”` : 'Add an entry';
  $('form-error').textContent = '';
  $('editor').showModal();
}

form.onsubmit = async (ev) => {
  ev.preventDefault();
  const f = form.elements;
  const translations = {};
  for (const l of LANGS) if (f[`tr-${l}`].value.trim()) translations[l] = f[`tr-${l}`].value.trim();
  const entry = {
    term: f.term.value, lang: f.lang.value, scope: f.scope.value, scopeId: f.scopeId.value,
    aliases: lines(f.aliases.value), misheard: lines(f.misheard.value), pronunciation: f.pronunciation.value,
    meaning: f.meaning.value, translations, keep: f.keep.checked, ambiguous: f.ambiguous.checked, status: f.status.value,
  };
  try {
    await api(f.id.value ? 'PUT' : 'POST', `/api/admin/bank/entries${f.id.value ? `/${f.id.value}` : ''}`, entry);
    $('editor').close();
    refresh();
  } catch (e) {
    $('form-error').textContent = e.message;
  }
};
$('editor-close').onclick = () => $('editor').close();
$('add').onclick = () => edit();
for (const id of ['f-scope', 'f-status']) $(id).onchange = () => entries().catch(fail);
$('f-q').oninput = () => entries().catch(fail);

async function refresh() {
  $('error').textContent = '';
  try {
    await events();
    await Promise.all([suggestions(), entries(), corrections()]);
  } catch (e) {
    fail(e);
  }
}

refresh();
