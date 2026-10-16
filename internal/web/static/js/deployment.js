fetch('/api/site').then((r) => r.json()).then((site) => {
  const messages = [];
  if (site.demo === 'true') messages.push('Demo mode: captions are scripted sample text. Speech providers are off, so no Sarvam credits are used.');
  if (site.temporaryData === 'true') messages.push('Temporary test deployment: created events and transcripts may reset on restart.');
  const notice = document.getElementById('deployment-mode');
  if (notice && messages.length) { notice.textContent = messages.join(' '); notice.hidden = false; }
}).catch(() => {});
