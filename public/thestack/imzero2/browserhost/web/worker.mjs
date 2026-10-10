// The worker behind the viewer page's ?worker= mode (ADR-0263): the Go tab
// host (imzero2tab, GOOS=wasip1 as a reactor) and the Rust browser host as
// two wasm modules on this one thread, joined by the fd 0/1 shim in
// bridge.mjs. The worker owns the cadence: it calls the Go module's frame
// export per tick and yields in between, which is when the page's input
// arrives; each rendered frame's wire messages are posted to the page, which
// paints them with the painter it already has.
//
// Query parameters of this module's URL:
//   app=<id>            the registered app to mount (the module's default otherwise)
//   module=<file>       the Go module (default imzero2tab.wasm); host=<file> the
//                       Rust host (default imzero2_browser.wasm)
//   arg=<flag>          extra module arguments, repeatable
//   env=NAME=value      the module's environment, repeatable; CLICKHOUSE_URL
//                       defaults to <origin>/ch/, which `imzero2tab serve` proxies;
//                       BOXER_TAB_BASE defaults to the URL of this worker's
//                       directory, which is the page's;
//                       IMZERO2_THEME also reaches the Rust host
//   stage=WxH           the initial viewport in points (the page's resize
//                       takes over)
//   fps=, idle=, cadence=continuous   the cadence, see below
//   <slot>Font=<url>    a font slot (main, mono, phosphor, fallback); default
//                       ./fonts/<slot>.ttf, a failed fetch leaves egui's face
//   log=1               forward the module's stderr to the server (POST ./log)
//
// The cadence is reactive (ADR-0077 SD6): after each frame the worker asks
// the host how soon egui wants to run again — now for an animation or a
// repaint request, which is also how the Go side asks for one — and sleeps
// until then, at most `idle` ms (default 1000, the heartbeat) and at least
// 1000/`fps` (default 60, the ceiling); input and session messages wake it
// at once. `cadence=continuous` ticks at `fps` regardless.
import { FONT_SLOTS, loadHost, startReactor } from './bridge.mjs';

const q = new URL(self.location.href).searchParams;
const stage = (q.get('stage') || '1024x600').split('x').map(Number);
const fps = Number(q.get('fps') || '60');
const idleMs = Number(q.get('idle') || '1000');
const continuous = q.get('cadence') === 'continuous';
const log = (line) => self.postMessage({ kind: 'log', line });
// `log=1` also posts every line the module writes to fd 2 — the app's
// structured log included — to the page's server (`imzero2tab serve` logs them),
// since a worker's console is out of reach for a headless capture.
const tee = q.get('log') ? (line) => { fetch('./log', { method: 'POST', body: line }).catch(() => {}); } : () => {};
const PREFIX_INPUT = 0x02, PREFIX_SESSION = 0x03;

try {
  const fontBytes = (slot) => fetch(q.get(`${slot}Font`) || `./fonts/${slot}.ttf`)
    .then((r) => (r.ok ? r.arrayBuffer() : null)).catch(() => null);
  const [hostBytes, goBytes, ...fontList] = await Promise.all([
    fetch(q.get('host') || './imzero2_browser.wasm').then((r) => r.arrayBuffer()),
    fetch(q.get('module') || './imzero2tab.wasm').then((r) => r.arrayBuffer()),
    ...FONT_SLOTS.map(fontBytes),
  ]);
  const fonts = Object.fromEntries(FONT_SLOTS.map((slot, i) => [slot, fontList[i]]));
  log('worker — fonts: ' + (FONT_SLOTS.filter((s) => fonts[s]).join(', ') || 'egui defaults'));
  // The colour theme is the module's IMZERO2_THEME; the host is told the same.
  const theme = q.getAll('env').filter((e) => e.startsWith('IMZERO2_THEME=')).map((e) => e.slice('IMZERO2_THEME='.length)).pop();
  const stub = await loadHost(hostBytes, stage[0], stage[1], 1.0,
    (m) => self.postMessage({ kind: 'mesh', bytes: m.buffer }, [m.buffer]), undefined, fonts, theme);
  // The hello the carrier would send: the canvas backing size in pixels and
  // the scale. Sent at start and again whenever the page's resize changed
  // the host's geometry.
  const sendHello = () => {
    const g = stub.geometry();
    self.postMessage({ kind: 'hello', hello: { width: Math.round(g.width * g.ppp), height: Math.round(g.height * g.ppp), ppp: g.ppp, codec: 'mesh', cadence: continuous ? 0 : 1 } });
  };
  // wake is set once the reactor runs: anything from the page earns a frame
  let wake = () => {};
  self.onmessage = (e) => {
    const m = e.data;
    if (m.kind !== 'input') return;
    const b = new Uint8Array(m.bytes);
    if (b.length < 2) return;
    if (b[0] === PREFIX_INPUT) stub.input(b.subarray(1));
    else if (b[0] === PREFIX_SESSION && stub.session(b.subarray(1)) === 1) sendHello();
    wake();
  };
  sendHello();
  const argv = [...q.getAll('arg')];
  if (q.get('app')) argv.push('-app', q.get('app'));
  // The module's environment: every `env=NAME=value` parameter, plus the
  // page's origin as the ClickHouse endpoint unless one is given — the
  // server behind the page proxies /ch/ to ClickHouse (`imzero2tab serve`), which is
  // how the data plane stays same-origin (ADR-0077 SD9).
  const env = q.getAll('env');
  if (!env.some((e) => e.startsWith('CLICKHOUSE_URL='))) env.push(`CLICKHOUSE_URL=${self.location.origin}/ch/`);
  // The directory the page is served from, which the module resolves paths
  // against (ADR-0299): an applet document named by
  // BOXER_SQLAPPLET_TAB_DOC. The worker sits beside the page in a bundle.
  if (!env.some((e) => e.startsWith('BOXER_TAB_BASE='))) env.push(`BOXER_TAB_BASE=${new URL('./', self.location.href).href}`);
  const r = await startReactor({ goBytes, stub, argv, env, log: (l) => { tee(l); if (!l.startsWith('{')) log(l); } });
  log('worker — running the application');
  let frames = 0, timer = null, lastTick = -Infinity, exited = false;
  const minMs = 1000 / fps;
  const schedule = (ms) => { clearTimeout(timer); timer = setTimeout(tick, Math.max(0, ms)); };
  const tick = () => {
    timer = null;
    const early = lastTick + minMs - performance.now();
    if (early > 0) { schedule(early); return; }
    lastTick = performance.now();
    const rc = r.frame();
    frames++;
    if (rc !== 0) { exited = true; log(`worker — the application exited after ${frames} frames`); return; }
    schedule(continuous ? minMs : Math.min(Math.max(stub.repaintDelayMs(), minMs), idleMs));
  };
  wake = () => { if (!exited) schedule(0); };
  r.onHttpSettled(() => wake());
  tick();
} catch (err) {
  log('worker error: ' + (err && err.stack || err));
}
