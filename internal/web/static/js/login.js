const form = document.getElementById('signin-form');
const error = document.getElementById('signin-error');
const submit = document.getElementById('signin-submit');
const params = new URLSearchParams(location.search);
const requested = params.get('next');
const next = requested === '/admin' || requested === '/bank' ? requested : '/admin';

if (params.get('error') === 'expired') {
  const p = document.getElementById('signin-problem');
  p.textContent = 'That sign-in link expired. Please try again.';
  p.hidden = false;
}

// Google first when the deployment has it; otherwise the access key form
// opens by itself.
fetch('/api/session').then((r) => r.json()).then((s) => {
  if (s.organiser) return location.replace(next);
  document.getElementById('google-signin').hidden = !s.google;
  if (!s.google) document.getElementById('key-option').open = true;
}).catch(() => { document.getElementById('key-option').open = true; });

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
