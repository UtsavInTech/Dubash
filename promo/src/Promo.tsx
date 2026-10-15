import React from 'react';
import { AbsoluteFill, Audio, interpolate, staticFile, useCurrentFrame, useVideoConfig } from 'remotion';
import { linearTiming, TransitionSeries } from '@remotion/transitions';
import { fade } from '@remotion/transitions/fade';
import { C, FONT } from './theme';
import { Chip, FadeUp, Frame, Kicker, Line, Logo, Phone, Scene, Speaker, typed, useIn } from './ui';

const T = 15; // transition length (frames)
const SCENES = [150, 120, 390, 240, 270, 270, 240, 180, 180];
export const PROMO_FRAMES = SCENES.reduce((a, b) => a + b, 0) - T * (SCENES.length - 1);

// ---------- 1. hook ----------
const Hook: React.FC = () => (
  <Scene style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', padding: '0 200px', gap: 10 }}>
    <FadeUp delay={5}><div style={{ fontSize: 110, fontWeight: 700 }}>Una charla.</div></FadeUp>
    <FadeUp delay={28}><div style={{ fontSize: 110, fontWeight: 700 }}>Miles de personas.</div></FadeUp>
    <FadeUp delay={51}><div style={{ fontSize: 110, fontWeight: 700 }}>Seis idiomas.</div></FadeUp>
    <FadeUp delay={88}><div style={{ fontSize: 76, fontWeight: 700, color: C.yellow, marginTop: 40 }}>¿Cuántas personas la entienden?</div></FadeUp>
  </Scene>
);

// ---------- 2. title ----------
const Title: React.FC = () => {
  const p = useIn(0, 12);
  return (
    <Scene style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 44, transform: `scale(${0.85 + 0.15 * p})`, opacity: p }}>
        <Logo size={190} />
        <div style={{ fontSize: 190, fontWeight: 700, letterSpacing: '-.02em' }}>Dubash</div>
      </div>
      <FadeUp delay={22}><div style={{ fontSize: 50, marginTop: 30, textAlign: 'center' }}>Subtítulos y traducción <b style={{ color: C.yellow }}>en vivo</b> para conferencias.</div></FadeUp>
      <FadeUp delay={40}><div style={{ fontSize: 32, marginTop: 26, color: C.text2 }}>Open source · Apache-2.0</div></FadeUp>
    </Scene>
  );
};

// ---------- 3. live: stage → phone ----------
const TALK = [
  { en: 'Good morning, Nerdearla!', es: '¡Buen día, Nerdearla!', pt: 'Bom dia, Nerdearla!', start: 20 },
  { en: 'Today I want to talk about why open source needs accessibility.', es: 'Hoy quiero hablar de por qué el open source necesita accesibilidad.', pt: 'Hoje quero falar sobre por que o open source precisa de acessibilidade.', start: 70 },
  { en: 'Every talk here is streamed to thousands of people.', es: 'Cada charla de acá se transmite a miles de personas.', pt: 'Cada palestra aqui é transmitida para milhares de pessoas.', start: 150 },
  { en: 'But not everyone can hear it, and not everyone speaks English.', es: 'Pero no todos pueden escucharla, y no todos hablan inglés.', pt: 'Mas nem todos podem ouvi-la, e nem todos falam inglês.', start: 215 },
];
const CPS = 30;
const doneAt = (t: { en: string; start: number }) => t.start + Math.ceil((t.en.length / CPS) * 30);
const LAG = 45; // ~1.5 s from end of sentence to the audience

const Waveform: React.FC<{ active: boolean }> = ({ active }) => {
  const frame = useCurrentFrame();
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 7, height: 90 }}>
      {Array.from({ length: 28 }, (_, i) => {
        const h = active ? 18 + 70 * Math.abs(Math.sin(frame / 4 + i * 0.9) * Math.sin(frame / 11 + i * 0.37)) : 8;
        return <div key={i} style={{ width: 9, height: h, borderRadius: 5, background: C.yellow, opacity: 0.85 }} />;
      })}
    </div>
  );
};

const Live: React.FC = () => {
  const frame = useCurrentFrame();
  const lang = frame >= 345 ? 'pt' : 'es';
  const current = [...TALK].reverse().find((t) => frame >= t.start);
  const speaking = current ? frame < doneAt(current) + 8 : false;
  const lines: Line[] = TALK.map((t) => ({ text: t[lang as 'es' | 'pt'], at: doneAt(t) + LAG }));
  const switchP = useIn(345, 14);
  return (
    <Scene>
      <div style={{ position: 'absolute', left: 140, top: 150, width: 1000 }}>
        <Kicker>En el escenario · English</Kicker>
        <div style={{ marginTop: 40 }}><Waveform active={speaking} /></div>
        <div style={{ marginTop: 40, fontSize: 64, fontWeight: 700, lineHeight: 1.25, minHeight: 250 }}>
          {current ? typed(current.en, frame, current.start, CPS) : ''}
        </div>
        <FadeUp delay={110}>
          <div style={{ fontSize: 40, color: C.text2, lineHeight: 1.4, marginTop: 30 }}>
            Cada oración llega <b style={{ color: C.text }}>traducida</b> al celular de cada persona <b style={{ color: C.yellow }}>en ~2 segundos.</b>
          </div>
        </FadeUp>
        <div style={{ opacity: switchP, transform: `translateY(${(1 - switchP) * 20}px)`, fontSize: 40, marginTop: 26, color: C.live, fontWeight: 700 }}>
          Un toque y cambia de idioma →
        </div>
      </div>
      <div style={{ position: 'absolute', right: 170, top: 90 }}>
        <Phone lines={lines} lang={lang} scale={1} />
      </div>
    </Scene>
  );
};

// ---------- 4. listen ----------
const LISTEN: Line[] = [
  { text: 'Los subtítulos y la traducción tienen que ser parte del escenario, no un extra.', at: 0 },
  { text: 'Así que veamos cómo corremos Kubernetes y eBPF en producción.', at: 95 },
  { text: 'Empecemos por lo que ven los usuarios.', at: 175 },
];

const Rings: React.FC = () => {
  const frame = useCurrentFrame();
  return (
    <>
      {[0, 1, 2].map((i) => {
        const t = ((frame + i * 20) % 60) / 60;
        return <div key={i} style={{ position: 'absolute', left: -110 * t, top: -110 * t, width: 120 + 220 * t, height: 120 + 220 * t, borderRadius: '50%', border: `4px solid ${C.live}`, opacity: 1 - t }} />;
      })}
    </>
  );
};

const Headphones: React.FC<{ size: number }> = ({ size }) => (
  <svg viewBox="0 0 24 24" width={size} height={size} style={{ color: C.live }}>
    <path d="M4 14v-2a8 8 0 0 1 16 0v2" fill="none" stroke="currentColor" strokeWidth="2" />
    <rect x="3" y="13" width="5" height="8" rx="2" fill="currentColor" />
    <rect x="16" y="13" width="5" height="8" rx="2" fill="currentColor" />
  </svg>
);

const Listen: React.FC = () => {
  const frame = useCurrentFrame();
  const last = [...LISTEN].reverse().find((l) => frame >= l.at) ?? LISTEN[0];
  const readFor = 80;
  const highlight = Math.min(1, (frame - last.at) / readFor);
  return (
    <Scene>
      <div style={{ position: 'absolute', left: 180, top: 90 }}>
        <Phone lines={LISTEN} lang="es" listening highlight={highlight} />
        <div style={{ position: 'absolute', left: 470, top: 330 }}>
          <Rings />
          <div style={{ position: 'absolute', left: 0, top: 0, width: 120, height: 120, borderRadius: 60, background: C.surface, border: `3px solid ${C.live}`, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Headphones size={70} />
          </div>
        </div>
      </div>
      <div style={{ position: 'absolute', left: 920, top: 190, width: 860 }}>
        <FadeUp delay={5}><Kicker color={C.live}>Nuevo</Kicker></FadeUp>
        <FadeUp delay={12}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 26, marginTop: 14 }}>
            <Speaker size={96} color={C.text} />
            <div style={{ fontSize: 104, fontWeight: 700 }}>Modo Escuchar</div>
          </div>
        </FadeUp>
        <FadeUp delay={28}><div style={{ fontSize: 44, lineHeight: 1.35, marginTop: 24 }}>El celular lee la traducción en voz alta. Con auriculares, es <b style={{ color: C.yellow }}>interpretación simultánea.</b></div></FadeUp>
        <div style={{ marginTop: 40, display: 'flex', flexDirection: 'column', gap: 18, fontSize: 34, color: C.text2 }}>
          <FadeUp delay={60}>• Para personas ciegas o con baja visión</FadeUp>
          <FadeUp delay={75}>• Voces del propio teléfono: sin costo de servidor</FadeUp>
          <FadeUp delay={90}>• No se atrasa: si la charla corre, lee más rápido</FadeUp>
        </div>
      </div>
    </Scene>
  );
};

// ---------- 5. every screen ----------
const Stream: React.FC<{ delay: number }> = ({ delay }) => {
  const p = useIn(delay);
  const W = 720;
  const H = (W * 9) / 16;
  return (
    <div style={{ opacity: p, transform: `translateY(${(1 - p) * 40}px)` }}>
      <div style={{ position: 'relative', width: W, height: H, borderRadius: 18, overflow: 'hidden', boxShadow: '0 30px 90px rgba(0,0,0,.55), 0 0 0 2px #2c2e30', background: 'radial-gradient(circle at 50% 20%, #3b3f8f 0%, #1a1440 45%, #07060f 100%)' }}>
        <div style={{ position: 'absolute', left: W * 0.44, top: H * 0.2, width: 70, height: 70, borderRadius: 35, background: '#0a0a14' }} />
        <div style={{ position: 'absolute', left: W * 0.4, top: H * 0.2 + 76, width: 130, height: 170, borderRadius: '50px 50px 0 0', background: '#0a0a14' }} />
        <div style={{ position: 'absolute', left: W * 0.55, top: H * 0.52, width: 150, height: 140, background: '#15131f', borderTop: `6px solid ${C.yellow}` }} />
        <div style={{ position: 'absolute', left: 18, top: 16, padding: '4px 12px', background: '#c1301c', color: '#fff', fontWeight: 700, fontSize: 18, borderRadius: 4 }}>● LIVE</div>
        <img src={staticFile('overlay.png')} style={{ position: 'absolute', inset: 0, width: W, height: H }} />
      </div>
      <div style={{ marginTop: 22, fontSize: 32, fontWeight: 700 }}>Overlay transparente para OBS y vMix</div>
    </div>
  );
};

const Screens: React.FC = () => (
  <Scene>
    <div style={{ position: 'absolute', left: 110, top: 70 }}>
      <FadeUp><Kicker>En todas las pantallas</Kicker></FadeUp>
    </div>
    <Frame src={staticFile('screen.png')} width={960} label="Pantalla del escenario, con QR" delay={10} style={{ position: 'absolute', left: 110, top: 170 }} />
    <div style={{ position: 'absolute', left: 1110, top: 170 }}><Stream delay={40} /></div>
    <FadeUp delay={80} style={{ position: 'absolute', left: 1110, top: 690, width: 720, fontSize: 34, lineHeight: 1.4, color: C.text2 }}>
      El público escanea el QR y los subtítulos se abren <b style={{ color: C.text }}>en el idioma de su teléfono.</b>
    </FadeUp>
  </Scene>
);

// ---------- 6. operation ----------
const Ops: React.FC = () => (
  <Scene>
    <div style={{ position: 'absolute', left: 110, top: 70 }}>
      <FadeUp><Kicker>Para el equipo técnico</Kicker></FadeUp>
    </div>
    <Frame src={staticFile('operator.png')} width={820} label="Consola por sala: micrófono, mixer o archivo" delay={8} style={{ position: 'absolute', left: 110, top: 170 }} />
    <Frame src={staticFile('admin.png')} width={820} label="Panel de producción: todas las salas en vivo" delay={35} style={{ position: 'absolute', left: 990, top: 170 }} />
    <FadeUp delay={80} style={{ position: 'absolute', left: 110, top: 820, display: 'flex', alignItems: 'center', gap: 28 }}>
      <code style={{ fontFamily: 'ui-monospace, monospace', fontSize: 32, background: C.surface, border: `2px solid ${C.border}`, padding: '14px 22px', borderRadius: 8, color: C.yellow }}>dubash-ingest -room sala-a -i srt://mixer:9000</code>
      <span style={{ fontSize: 32, color: C.text2 }}>o sin navegador, desde el stream del escenario</span>
    </FadeUp>
    <FadeUp delay={100} style={{ position: 'absolute', left: 110, top: 930, fontSize: 32, color: C.text2 }}>
      Nivel de audio · latencia · errores · Nueva charla · métricas Prometheus
    </FadeUp>
  </Scene>
);

// ---------- 7. how it works ----------
const Box: React.FC<{ title: string; sub: string; delay: number; accent?: string }> = ({ title, sub, delay, accent = C.border }) => {
  const p = useIn(delay, 16);
  return (
    <div style={{ width: 360, padding: '30px 30px', border: `3px solid ${accent}`, borderRadius: 14, background: C.surface, opacity: p, transform: `scale(${0.9 + 0.1 * p})` }}>
      <div style={{ fontSize: 40, fontWeight: 700 }}>{title}</div>
      <div style={{ fontSize: 27, color: C.text2, marginTop: 10, lineHeight: 1.35 }}>{sub}</div>
    </div>
  );
};

const Arrow: React.FC<{ delay: number }> = ({ delay }) => {
  const frame = useCurrentFrame();
  const w = interpolate(frame, [delay, delay + 12], [0, 1], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' });
  return (
    <div style={{ width: 70, display: 'flex', alignItems: 'center' }}>
      <div style={{ height: 6, width: 56 * w, background: C.yellow }} />
      <div style={{ width: 0, height: 0, borderTop: '14px solid transparent', borderBottom: '14px solid transparent', borderLeft: `18px solid ${C.yellow}`, opacity: w }} />
    </div>
  );
};

const How: React.FC = () => (
  <Scene>
    <div style={{ position: 'absolute', left: 110, top: 180 }}>
      <FadeUp><Kicker>Cómo funciona</Kicker></FadeUp>
    </div>
    <div style={{ position: 'absolute', left: 110, top: 300, display: 'flex', alignItems: 'center', gap: 6 }}>
      <Box title="Audio" sub="micrófono, mixer, SRT, RTMP o HLS" delay={10} />
      <Arrow delay={30} />
      <Box title="Transcribe" sub="Gemini Live, con glosario de la sala" delay={40} accent={C.link} />
      <Arrow delay={60} />
      <Box title="Traduce" sub="cada oración, a 6 idiomas, en paralelo" delay={70} accent={C.link} />
      <Arrow delay={90} />
      <Box title="Muestra" sub="celulares, pantalla y OBS, por SSE" delay={100} accent={C.live} />
    </div>
    <FadeUp delay={130} style={{ position: 'absolute', left: 110, top: 640, fontSize: 50, fontWeight: 700 }}>
      O <span style={{ color: C.yellow }}>100% local, sin internet:</span> Whisper + Gemma (Ollama).
    </FadeUp>
    <FadeUp delay={155} style={{ position: 'absolute', left: 110, top: 750, fontSize: 38, color: C.text2, lineHeight: 1.5 }}>
      Un binario de Go · imagen Docker de 32 MB · muchas salas en paralelo<br />
      El audio se paga una vez por sala: cada idioma extra es solo texto.
    </FadeUp>
  </Scene>
);

// ---------- 8. accessibility ----------
const A11y: React.FC = () => {
  const chips = ['Contraste 7:1 (WCAG AAA)', 'Tipografía Inclusive Sans', '4 tamaños de letra', 'Alto contraste', 'Espaciado para dislexia', 'Lectores de pantalla', 'Original + traducción', 'Interfaz en 6 idiomas', 'Transcripción por charla: VTT · SRT · TXT'];
  return (
    <Scene>
      <div style={{ position: 'absolute', left: 110, top: 170 }}>
        <FadeUp><Kicker>Accesible de verdad</Kicker></FadeUp>
        <FadeUp delay={8}><div style={{ fontSize: 72, fontWeight: 700, marginTop: 16, maxWidth: 1500 }}>Los subtítulos existen por accesibilidad. La interfaz también.</div></FadeUp>
      </div>
      <div style={{ position: 'absolute', left: 110, top: 520, right: 110, display: 'flex', flexWrap: 'wrap', gap: 26 }}>
        {chips.map((c, i) => <Chip key={c} delay={25 + i * 9} color={i === chips.length - 1 ? C.yellow : C.text}>{c}</Chip>)}
      </div>
    </Scene>
  );
};

// ---------- 9. outro ----------
const Outro: React.FC = () => (
  <Scene style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center' }}>
    <FadeUp>
      <div style={{ display: 'flex', alignItems: 'center', gap: 34 }}>
        <Logo size={130} />
        <div style={{ fontSize: 130, fontWeight: 700 }}>Dubash</div>
      </div>
    </FadeUp>
    <FadeUp delay={15}><div style={{ fontSize: 46, marginTop: 30, textAlign: 'center' }}>Que cada charla llegue a todas las personas.</div></FadeUp>
    <FadeUp delay={30}><div style={{ fontSize: 50, marginTop: 50, fontWeight: 700, color: C.yellow }}>github.com/UtsavInTech/dubash</div></FadeUp>
    <FadeUp delay={45}><div style={{ fontSize: 30, marginTop: 40, color: C.text2 }}>Open source · Apache-2.0 · Hecho para la Vibeathon de Nerdearla 2026</div></FadeUp>
  </Scene>
);

export const Promo: React.FC = () => {
  const { durationInFrames } = useVideoConfig();
  const parts = [Hook, Title, Live, Listen, Screens, Ops, How, A11y, Outro];
  return (
    <AbsoluteFill style={{ background: C.bg, fontFamily: FONT }}>
      <TransitionSeries>
        {parts.map((Part, i) => (
          <React.Fragment key={i}>
            {i > 0 ? <TransitionSeries.Transition presentation={fade()} timing={linearTiming({ durationInFrames: T })} /> : null}
            <TransitionSeries.Sequence durationInFrames={SCENES[i]}><Part /></TransitionSeries.Sequence>
          </React.Fragment>
        ))}
      </TransitionSeries>
      <Audio src={staticFile('music.wav')} volume={(f) => interpolate(f, [0, 20, durationInFrames - 45, durationInFrames], [0, 0.9, 0.9, 0], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' })} />
    </AbsoluteFill>
  );
};
