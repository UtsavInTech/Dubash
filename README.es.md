# Dubash

**Subtítulos y traducción en vivo, de código abierto, para conferencias.**
El audio de cada escenario se convierte en subtítulos en el idioma original y
traducidos al español, inglés, portugués, francés, alemán o italiano, para
muchas salas a la vez.
Hecho para la Vibeathon de [Nerdearla](https://nerdear.la) 2026.

> Este archivo es la guía del proyecto original,
> [Lenguaraz](https://github.com/RchrdHndrcks/lenguaraz), del que Dubash es
> un fork para la India. Describe las funciones de Lenguaraz.

*English: [README.md](README.md).*

![Una charla en inglés llega traducida al español a cada celular de la sala](docs/screenshots/hero.png)

## Qué resuelve

- **Una sala por escenario, todas en paralelo.** Cada sala es una sesión de
  Gemini Live (transcripción) más una llamada de texto barata por idioma
  (traducción). Muchas salas corren en una sola máquina chica.
- **Con Gemini o 100% local:** Whisper para transcribir y Gemma (vía
  Ollama) para traducir, en tu propio hardware y sin internet. Ver
  [Correrlo 100% local](#correrlo-100-local).
- **Tres formas de meter audio:** la consola web del operador (micrófono,
  línea del mixer o un archivo), o `dubash-ingest`, que toma el stream
  del escenario (SRT, RTMP, HLS, placa de captura) y se reconecta solo.
- **La audiencia elige sala e idioma** en el celular, o escanea el QR de la
  sala y le abre directamente en el idioma de su teléfono.
- **Pantalla del escenario** (letra enorme + QR) y **overlay transparente
  para OBS / vMix** para quemar los subtítulos en el stream.
- **Accesibilidad de verdad:** tipografía de alta legibilidad (Inclusive
  Sans), subtítulos con contraste de 7:1 o más, cuatro tamaños de letra, colores claro,
  oscuro y alto contraste, espaciado amplio para dislexia o baja visión, modo
  bilingüe (traducción + original) y la interfaz en el idioma que eligió cada
  persona.
- **Modo Escuchar:** con auriculares, el celular de cada persona lee en voz
  alta cada línea nueva en el idioma elegido, con las voces del propio
  dispositivo. Los subtítulos se vuelven interpretación simultánea para
  personas ciegas o con baja visión, o para quien prefiera escuchar. Sin
  costo de servidor, nada sale del teléfono, y no se atrasa: si la charla va
  más rápido que la voz, lee más rápido o saltea a lo último.
- **Glosario por sala:** nombres propios y términos técnicos se reconocen
  mejor y nunca se traducen.
- **Transcripción de cada charla** en VTT, SRT o TXT, en cualquier idioma,
  con los tiempos desde cero para subirla junto al video de la charla.
- **Sala de control** para producción: estado de cada sala, nivel del audio
  que llega, actividad del reconocedor, público, latencia de traducción,
  errores con su causa, botón de **Nueva charla** y métricas para Prometheus.
- **Carteles A4 con QR** listos para imprimir, uno por sala.
- Licencia **Apache-2.0**. La tipografía (SIL OFL) viene dentro del
  binario: ninguna página hace pedidos a terceros.

## Desplegarlo en tu conferencia

### 1. Lo que necesitás

- Una API key de Gemini ([Google AI Studio](https://aistudio.google.com/apikey)).
  Revisá las cuotas (sesiones Live simultáneas, pedidos por minuto) y pedí
  un aumento si vas a tener muchas salas.
- Una máquina con Docker (o Go 1.26+) accesible por https: un VPS, o tu
  notebook detrás de un túnel (Cloudflare Tunnel, ngrok, Tailscale Funnel).

### 2. Definí las salas

`rooms.yaml`:

```yaml
rooms:
  - id: sala-a
    title: "Sala A — Keynotes"
    source: en            # idioma que se habla normalmente: en | es | pt
    targets: [es, pt]     # los otros idiomas de la sala
    glossary: [Nerdearla, Kubernetes, eBPF]
  - id: sala-b
    title: "Sala B — Charlas"
    source: es
    targets: [en]
```

Si en la misma sala hay charlas en otro idioma, no hace falta tocar nada: al
transmitir, el operador elige el idioma que se habla (selector **Idioma que se
habla** en la consola, o `-lang es` en `dubash-ingest`) y los demás idiomas
de la sala pasan a ser las traducciones.

### 3. Levantalo

```bash
git clone https://github.com/UtsavInTech/dubash && cd dubash
export GEMINI_API_KEY=...  ADMIN_TOKEN=un-secreto  PUBLIC_URL=https://subs.tuevento.org
docker compose up -d --build
```

Idiomas disponibles: `en`, `es`, `pt`, `fr`, `de`, `it`.

Sin API key, para probar: `go run ./cmd/dubash -fake` (subtítulos guionados).

### 4. Conectá el audio de cada escenario

**Opción A — consola web** (`/operator/sala-a?token=…`, Chrome o Edge, por
https): elegí el micrófono o la entrada de línea y tocá **Transmitir**.

**Opción B — sin navegador**, desde la máquina de streaming:

```bash
go install ./cmd/dubash-ingest   # desde el repo; queda en $(go env GOPATH)/bin
export DUBASH_URL=https://subs.tuevento.org ADMIN_TOKEN=un-secreto
dubash-ingest -room sala-a -lang en -talk "Keynote de apertura" -i srt://mixer.local:9000
```

Un proceso por sala. Si se corta la red o el stream, reintenta solo.

### Charlas

Cada línea de subtítulo pertenece a una charla. Empieza una charla nueva
cuando el operador transmite con un título nuevo (campo **Charla** de la
consola, o `-talk` en `dubash-ingest`), cuando producción toca **Nueva
charla** en el panel (sin cortar el audio), o cuando la sala estuvo más de 5
minutos sin transmitir. Al terminar, el público descarga **Esta charla** o
**Toda la sala**, y producción la baja con
`/export/sala-a/srt?lang=es&talk=3`: los tiempos arrancan en cero, listos
para cargar sobre la grabación.

### 5. Mostralo

| Dónde | URL |
|---|---|
| Celulares del público | `/` o el QR de cada sala |
| Pantalla del escenario | `/r/sala-a?lang=es&mode=screen` |
| OBS / vMix (Browser Source, 1920×1080) | `/r/sala-a?lang=es&mode=overlay` — [guía paso a paso para OBS](docs/obs.md) |
| Carteles para imprimir | `/posters` |
| Producción | `/admin?token=…` |
| Transcripción de una charla | `/export/sala-a/srt?lang=es&talk=3` (también `vtt`, `txt`; sin `talk`, toda la sala) |
| Monitoreo (Prometheus) | `/metrics` con `Authorization: Bearer <ADMIN_TOKEN>` |

## Cuánto escala

Una instancia maneja muchas salas: el audio se factura una vez por sala y
cada idioma extra es solo una llamada de texto. Para eventos más grandes,
repartí salas entre instancias con distintos `rooms.yaml` y ruteá por path
(`/r/{sala}`, `/events/{sala}`, `/ingest/{sala}`) desde tu proxy. Detalles en
el [README en inglés](README.md#scaling-to-more-rooms).

## Correrlo 100% local

Para sedes sin buena conexión, o eventos que no pueden mandar audio a la
nube, las dos mitades corren en tu hardware. Dubash habla las APIs
compatibles con OpenAI que exponen los servidores de modelos locales:

```bash
# Transcripción: el servidor de whisper.cpp (o Speaches, LocalAI…)
whisper-server -m models/ggml-large-v3-turbo.bin --host 0.0.0.0 --port 8000 \
  --inference-path /v1/audio/transcriptions

# Traducción: Gemma con Ollama (o llama.cpp, vLLM…)
ollama pull gemma3

ASR_URL=http://localhost:8000/v1 TRANSLATE_URL=http://localhost:11434/v1 \
  ADMIN_TOKEN=un-secreto go run ./cmd/dubash
```

Con Docker Compose usá `http://host.docker.internal:…`. Sin `GEMINI_API_KEY`
alcanza; también se puede mezclar (Gemini para transcribir y Gemma para
traducir). Whisper recibe archivos, no streams: Dubash corta el audio en
las pausas de quien habla (máximo 10 s) y los subtítulos llegan de a una
frase. Se recomienda GPU o Apple Silicon para varias salas.

## Licencia

[Apache-2.0](LICENSE). Tipografía: Inclusive Sans (Olivia King), bajo SIL
Open Font License.
