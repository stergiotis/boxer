// Worker of the browser host: loads the stub and the Go module, runs one arm
// through bridge.js entirely off the main thread, and posts the report. The
// run blocks this worker for its whole duration; the page stays responsive.
// bridge.js is copied beside this file into the served directory by
// measure.sh from public/thestack/imzero2/browserhost/web/.
import { loadHost, loadStub, runArm } from './bridge.js';

// The report also goes to the serving host (serve.mjs), which is how a
// headless run collects it without reading the DOM.
const report = (line) => fetch('./report', { method: 'POST', body: line }).catch(() => {});

self.onmessage = async (e) => {
  const p = e.data;
  const log = (line) => self.postMessage({ kind: 'log', line });
  try {
    const target = p.target || 'wasip1';
    const stage = (p.stage || '1024x600').split('x').map(Number);
    const [table, stubBytes, goBytes] = await Promise.all([
      fetch('./fetchtable.txt').then((r) => r.text()),
      fetch('./fffi2stub.wasm').then((r) => r.arrayBuffer()),
      fetch(`./wasmspike_${target}.wasm`).then((r) => r.arrayBuffer()),
    ]);
    // host=1 drives the real Rust host (imzero2.wasm) instead of the stub;
    // each rendered frame's mesh goes to the page, which may paint it.
    let meshFrames = 0, meshBytes = 0;
    const stub = p.host === '1'
      ? await loadHost(await fetch('./imzero2.wasm').then((r) => r.arrayBuffer()), stage[0], stage[1], 1.0,
          (m) => { meshFrames++; meshBytes += m.length; self.postMessage({ kind: 'mesh', bytes: m.buffer }, [m.buffer]); })
      : await loadStub(stubBytes, table, stage[0], stage[1]);
    let GoCtor = null;
    if (target === 'js') {
      globalThis.fs = { constants: {}, writeSync() { return 0; } };
      // wasm_exec.js is a classic script; in a module worker it is loaded by
      // evaluating its text (importScripts is unavailable to module workers).
      const src = await fetch('./wasm_exec.js').then((r) => r.text());
      (0, eval)(src);
      GoCtor = globalThis.Go;
    }
    const argv = [
      '-consumer', p.consumer || 'pipe', '-scene', p.scene || 'gallery',
      '-frames', p.frames || '300', '-warmup', p.warmup || '30',
      '-rows', p.rows || '200', '-stage', `${stage[0]}x${stage[1]}`,
      '-target', target, '-arm', p.arm || `browser-${target}-${p.consumer || 'pipe'}`,
      '-fetchTable', table,
    ];
    if (p.demo) argv.push('-demo', p.demo);
    // deferred flushing is the channel default; `eager` asks for the old
    // per-message flush
    if (p.flush === 'eager') argv.push('-lazyFlush=false');
    const out = await runArm({ target, goBytes, stub, argv, GoCtor, log: (l) => { if (!l.startsWith('RESULT ') && !l.startsWith('{')) log(l); } });
    const arm = { result: out.result, stub: p.host === '1' ? undefined : out.stub, host: p.host === '1' ? { ...out.stub, meshFrames, meshBytes, lastError: stub.lastError() } : undefined, bridge: out.bridge, wallMs: out.wallMs, ua: navigator.userAgent };
    self.postMessage({ kind: 'arm', arm });
    await report('ARM ' + JSON.stringify(arm));
  } catch (err) {
    const message = String(err && err.stack || err);
    self.postMessage({ kind: 'error', message });
    await report('ERROR ' + message);
  }
};
