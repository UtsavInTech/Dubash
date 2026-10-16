const form = document.getElementById('signin-form');
const error = document.getElementById('signin-error');
const submit = document.getElementById('signin-submit');
const requested = new URLSearchParams(location.search).get('next');
const next = requested === '/admin' || requested === '/bank' ? requested : '/admin';
fetch('/api/session').then((r) => r.json()).then((s) => { if (s.organiser) location.replace(next); }).catch(() => {});
form.onsubmit = async (event) => {
  event.preventDefault(); error.textContent = ''; submit.disabled = true; submit.textContent = 'Signing in…';
  try {
    const response = await fetch('/api/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ key: document.getElementById('signin-key').value }) });
    if (!response.ok) throw new Error((await response.text()).trim());
    document.getElementById('signin-key').value = '';
    // Discard legacy copies: the new session is an HttpOnly cookie.
    try { localStorage.removeItem('dubash.token'); } catch { /* Storage unavailable. */ }
    location.replace(next);
  } catch (e) { error.textContent = e.message || 'Could not sign in. Check your connection.'; }
  finally { submit.disabled = false; submit.textContent = 'Sign in →'; }
};
