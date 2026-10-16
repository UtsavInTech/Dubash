// Language names in their own language, as a reader looks for them.
// Mirrors internal/lang (a Go test keeps them in step).
export const NAMES = { en: 'English', hi: 'हिन्दी', bn: 'বাংলা', mr: 'मराठी', te: 'తెలుగు', ta: 'தமிழ்', gu: 'ગુજરાતી', kn: 'ಕನ್ನಡ', ml: 'മലയാളം', or: 'ଓଡ଼ିଆ', pa: 'ਪੰਜਾਬੀ', es: 'Español', pt: 'Português', fr: 'Français', de: 'Deutsch', it: 'Italiano' };

// English names, shown under each language's own name so anyone can find it.
export const ENGLISH = { en: 'English', hi: 'Hindi', bn: 'Bengali', mr: 'Marathi', te: 'Telugu', ta: 'Tamil', gu: 'Gujarati', kn: 'Kannada', ml: 'Malayalam', or: 'Odia', pa: 'Punjabi', es: 'Spanish', pt: 'Portuguese', fr: 'French', de: 'German', it: 'Italian' };

export function el(tag, props = {}, ...children) {
  const node = Object.assign(document.createElement(tag), props);
  node.append(...children);
  return node;
}
