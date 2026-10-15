# Subtítulos en OBS

Esta guía arma el circuito completo en una sola máquina: el audio que sale de
OBS entra a Dubash y los subtítulos vuelven a OBS como una capa sobre el
video, así quedan "quemados" en la transmisión o la grabación.

```
OBS (micrófono, video de la charla…) ──audio──▶ dubash-ingest ──▶ Dubash
   ▲                                                                    │
   └────────────── fuente de navegador (overlay transparente) ◀─────────┘
```

Escrita para OBS 28 o posterior; los nombres de menú van en español y, entre
paréntesis, en inglés.

## 0. Levantar Dubash

```bash
git clone https://github.com/UtsavInTech/dubash && cd dubash
export GEMINI_API_KEY=...   ADMIN_TOKEN=prueba
go run ./cmd/dubash            # o: docker compose up --build
```

Sin API key, `go run ./cmd/dubash -fake` genera subtítulos de mentira: sirve
para practicar el armado en OBS, no para medir calidad.

## 1. El overlay: los subtítulos sobre el video

1. En la escena, **Fuentes → + → Navegador** (*Browser*).
2. URL: `http://localhost:8080/r/sala-a?lang=es&mode=overlay`
3. **Ancho 1920, Alto 1080**: el mismo tamaño que el lienzo de OBS
   (Ajustes → Video). El texto se escala con el ancho.
4. Dejá el CSS personalizado como viene: el fondo ya es transparente.
5. Desmarcá **Apagar la fuente cuando no esté visible** (*Shutdown source when
   not visible*) para que no se desconecte al cambiar de escena.
6. Poné la fuente **arriba de todo** en la lista, para que quede sobre el video.

Se puede ajustar desde la URL:

| Parámetro | Valores | Para qué |
|---|---|---|
| `lang` | `es`, `en`, `pt`, `fr`, `de`, `it` | idioma de los subtítulos (una fuente por idioma si hace falta) |
| `lines` | `1` a `5` (por defecto `2`) | cuántas líneas se muestran |
| `size` | `0` a `3` (por defecto `0`) | tamaño del texto |
| `hold` | segundos (por defecto `8`; `0` = nunca) | cuánto queda el texto en pantalla después de que la persona deja de hablar |

El overlay no muestra frases viejas al cargar, se reconecta solo si Dubash
se reinicia, y se borra en las pausas: el stream nunca queda con texto viejo.

La primera vez que cargue bien, el overlay se guarda en OBS. Desde ahí, si
abrís OBS antes que Dubash, espera al servidor y se conecta solo. La
primera vez, en cambio, Dubash tiene que estar corriendo: si no lo estaba,
clic derecho en la fuente → **Actualizar** (*Refresh*). Esto funciona con
`localhost` o `https`; con otra dirección (`http://192.168…`), actualizá la
fuente cada vez que el servidor arranque después que OBS.

**Extra:** para ver los subtítulos y el estado sin salir de OBS, agregá
**Paneles → Paneles de navegador personalizados** (*Docks → Custom Browser
Docks*) con `http://localhost:8080/admin?token=prueba` o
`http://localhost:8080/r/sala-a?lang=es`.

## 2. Mandarle el audio a Dubash

### Opción A: el audio que sale de OBS (recomendada)

Así Dubash escucha exactamente lo mismo que se transmite, sin cables extra, y
la salida de **transmisión** queda libre para YouTube o Twitch: se usa la salida
de **grabación**.

1. Instalá ffmpeg (`brew install ffmpeg`, `sudo apt install ffmpeg` o
   `winget install ffmpeg`) y el cliente de ingesta:
   ```bash
   go install ./cmd/dubash-ingest   # desde la carpeta del repo
   ```
   Si después tu terminal dice `command not found: dubash-ingest`, agregá
   la carpeta de Go al PATH (`export PATH="$PATH:$(go env GOPATH)/bin"`) o
   corrélo sin instalar: `go run ./cmd/dubash-ingest …`.
2. En OBS: **Ajustes → Salida** (*Settings → Output*), **Modo de salida:
   Avanzado** (*Output Mode: Advanced*), pestaña **Grabación** (*Recording*):
   - **Tipo:** Salida personalizada (FFmpeg) (*Custom Output (FFmpeg)*)
   - **Tipo de salida FFmpeg:** Salida a URL (*Output to URL*)
   - **URL:** `udp://127.0.0.1:5000?pkt_size=1316`
   - **Formato de contenedor:** `mpegts`
   - **Codificador de video:** el que viene (`libx264`); **de audio:** `aac`
3. En una terminal, dejá escuchando la sala (con `-lang` el idioma que se
   habla en el escenario):
   ```bash
   ADMIN_TOKEN=prueba dubash-ingest -room sala-a -lang es \
     -i "udp://127.0.0.1:5000?overrun_nonfatal=1&fifo_size=1000000"
   ```
4. En OBS: **Iniciar grabación**. En unos segundos aparecen los subtítulos.
   Al detener la grabación, `dubash-ingest` queda esperando la próxima.

> Si necesitás grabar en disco al mismo tiempo, usá la opción B o el plugin
> *Source Record*.

**Variante SRT** (usa la salida de *transmisión*, así que sirve para probar,
no para transmitir a la vez): Ajustes → Emisión → Servicio **Personalizado**,
Servidor `srt://127.0.0.1:9000?mode=caller`; y
`dubash-ingest -room sala-a -lang es -i "srt://127.0.0.1:9000?mode=listener"`.

### Opción B: la consola del operador (sin instalar nada)

Abrí `http://localhost:8080/operator/sala-a?token=prueba` en Chrome o Edge,
tocá **Detectar micrófonos**, elegí el mismo micrófono que usa OBS, elegí el
idioma que se habla y **Transmitir**.

## 3. Probar que es efectivo

1. **Usá una charla real**, no las muestras sintéticas. Por ejemplo, un video
   de una charla anterior:
   ```bash
   yt-dlp -f "bv*[height<=720]+ba/b" -o charla.mp4 "<url de la charla>"
   ```
   Agregalo como **Fuentes → + → Multimedia** (*Media Source*). Su audio va a la
   mezcla de OBS y de ahí a Dubash (opción A).
2. **Cargá el glosario** de la charla en `rooms.yaml` (nombres de personas,
   productos, siglas) y reiniciá Dubash: mejora mucho los nombres propios.
3. **Mirá la latencia** en `http://localhost:8080/admin?token=prueba`. Como
   referencia, el texto en el idioma original aparece mientras la persona
   habla, y la traducción llega uno a tres segundos después de terminada cada
   frase (la columna *Latencia* mide esa traducción).
4. **Probá los dos sentidos**: una charla en inglés con `-lang en` y el
   overlay en `lang=es`, y una en español con `-lang es` y el overlay en
   `lang=en`.
5. **Revisá la calidad al final**: descargá la transcripción completa y leela
   de corrido, o cargala en un reproductor sobre el video:
   `http://localhost:8080/export/sala-a/srt?lang=es` (también `vtt` y `txt`).
6. **Hacé una transmisión de prueba** privada o no listada en YouTube para ver
   el resultado como lo vería el público.

## Problemas frecuentes

| Síntoma | Causa y solución |
|---|---|
| El overlay no muestra nada | ¿Abriste OBS antes que Dubash la primera vez? Actualizá la fuente (clic derecho → Actualizar). ¿Está corriendo la ingesta y dice `ingest connected`? ¿La fuente está arriba de todo? Probá la URL sin `&mode=overlay` en el navegador para ver si llegan subtítulos. |
| Los subtítulos están en otro idioma | El `-lang` de `dubash-ingest` (o el selector de la consola) tiene que ser el idioma que **se habla**; el `lang` del overlay es el idioma en que se **lee**. |
| `room busy` en la ingesta | Ya hay otra fuente transmitiendo en esa sala (por ejemplo la consola del operador abierta). Cerrala; la ingesta reintenta sola. |
| `token rejected` | El `ADMIN_TOKEN` de la ingesta no coincide con el del servidor. |
| El texto queda cortado o es muy grande | Ajustá `size` y `lines` en la URL, o el ancho de la fuente para que coincida con el lienzo. |
| Se ve el texto de la charla anterior | Recargá la fuente (clic derecho → Actualizar). No debería pasar: el overlay oculta lo viejo al conectarse. |
