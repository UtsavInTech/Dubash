// Reader preferences (colors, text size, spacing), applied before the
// page paints so there is no flash. Loaded as a classic script in <head>.
(function () {
  let p = {};
  try { p = JSON.parse(localStorage.getItem('dubash.prefs') || '{}'); } catch { /* private mode */ }
  const d = document.documentElement;
  if (['light', 'dark', 'contrast'].includes(p.theme)) d.dataset.theme = p.theme;
  if (p.size > 0) d.dataset.size = String(p.size);
  if (p.spacing) d.dataset.spacing = 'wide';
})();
