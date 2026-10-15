// Language names in their own language, as a reader looks for them.
// Mirrors internal/lang (a Go test keeps them in step).
export const NAMES = { en: 'English', hi: 'हिन्दी', bn: 'বাংলা', mr: 'मराठी', te: 'తెలుగు', ta: 'தமிழ்', gu: 'ગુજરાતી', kn: 'ಕನ್ನಡ', ml: 'മലയാളം', or: 'ଓଡ଼ିଆ', pa: 'ਪੰਜਾਬੀ', es: 'Español', pt: 'Português', fr: 'Français', de: 'Deutsch', it: 'Italiano' };

export function el(tag, props = {}, ...children) {
  const node = Object.assign(document.createElement(tag), props);
  node.append(...children);
  return node;
}
