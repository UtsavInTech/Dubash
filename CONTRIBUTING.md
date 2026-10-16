# Contributing to Dubash

Thanks for helping make conferences accessible. Issues, pull requests,
translations of the interface and reports from real events are all welcome,
in English or Spanish.

## Before you start

- For a bug, open an issue with what you did, what you expected and what
  happened; the server log and the production panel's error column help.
- For a feature, open an issue first so we can agree on the approach before
  you write code, especially for changes to the pipeline or the HTTP API.
- Be kind: this project follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

You need Go (the version in `go.mod`). No API key is required for most work:

```bash
go run ./cmd/dubash -fake     # scripted transcription and translation
go test -race ./...
```

The frontend lives in `internal/web/static`: plain HTML, CSS and JavaScript
modules, no build step, embedded in the binary. Reload the page after an
edit and restart the server.

For frontend changes, run the browser end-to-end suite too: it streams a
sample from the operator page and from `dubash-ingest` into two rooms
and checks the audience page, reading settings, listen mode, projector,
overlay, production panel, exports and metrics.

```bash
cd e2e && npm install && npx playwright install chromium && npm test
```

To try the real models, set `GEMINI_API_KEY`, or point `ASR_URL` and
`TRANSLATE_URL` at local servers (see the README).

## Pull requests

CI runs the same checks you can run locally:

```bash
gofmt -l .                       # must print nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go test -race ./...
for f in internal/web/static/js/*.js; do node --check "$f"; done
```

- Keep changes focused: one topic per pull request.
- Add or update tests for behavior you change, with the standard `testing`
  package: the fake engines and `httptest` servers stand in for the real
  models, so the suite runs offline.
- Follow the conventions of the Go standard library: doc comments on
  exported identifiers, errors that start in lowercase and wrap their cause
  with `%w`, and no new dependency where the standard library will do.
- User-facing text on the pages is in Spanish first; the audience page is
  translated into every supported language (`UI` in `viewer.js`).
- Write commit messages in the imperative ("Add Portuguese samples") and
  explain why in the body when it is not obvious.

## Adding a language

1. Add its code to `languages` in `internal/config/config.go`.
2. Add its locale to `languageCodes` in `internal/asr/gemini.go` and its
   English name to `names` in `internal/translate/translate.go`.
3. Add its name to `NAMES` in `internal/web/static/js/langs.js` and
   `operator.js`, and its interface strings to `UI` in `viewer.js`.

## Adding a model backend

Transcription engines implement `asr.Engine` and translators implement
`translate.Translator`. Both interfaces are small; `asr.Fake` and
`translate.Fake` are the simplest examples, `asr.Whisper` and
`translate.Chat` show how to talk to an HTTP model server.

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
