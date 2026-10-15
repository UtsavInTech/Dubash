# Dubash promo video

The promo video, as code, with [Remotion](https://www.remotion.dev). Every
screen in it is Dubash's real interface: the audience phone is redrawn
from the viewer's own styles (so its lines can arrive live), and the stage
screen, OBS overlay, operator console and production panel are screenshots
of the running app. The music is synthesized by `make-music.py`, so the
video carries no third-party audio.

```bash
cd promo
npm install
npm run studio          # preview and edit in the browser
npm run render          # → out/dubash.mp4 (1920×1080, 30 fps, ~64 s)
```

`npm run render` downloads a headless Chromium on first use; to reuse one
you already have, add `-- --browser-executable=/path/to/chrome`.

To refresh the screenshots in `public/` after a UI change (needs Go, and
Playwright's Chromium: `npx playwright install chromium`):

```bash
npm run capture
```

It builds Dubash, seeds a realistic transcript (`capture/seed.mjs`),
runs the server with `-fake` and captures each page (`capture/shots.mjs`).

Remotion is free for individuals and small teams; check its
[license](https://www.remotion.dev/license) before using it in a company.
The typeface is Inclusive Sans under the SIL Open Font License
(`public/OFL-inclusivesans.txt`).
