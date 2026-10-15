import { continueRender, delayRender, staticFile } from 'remotion';

// Colors and type from Dubash's own stylesheet (internal/web/static/css/app.css).
export const C = {
  bg: '#0d0e0e',
  surface: '#1b1c1d',
  text: '#f2f2f2',
  text2: '#b9bcbe',
  border: '#505457',
  link: '#8cb8ff',
  blue: '#1a5fb4',
  live: '#4fcf7c',
  yellow: '#ffd400',
  black: '#0b0c0c',
};
export const FONT = '"Inclusive Sans", system-ui, sans-serif';

const handle = delayRender('Inclusive Sans');
const face = new FontFace('Inclusive Sans', `url(${staticFile('inclusive-sans.woff2')}) format("woff2")`, { weight: '300 700' });
face.load().then(() => { document.fonts.add(face); continueRender(handle); }).catch(() => continueRender(handle));
