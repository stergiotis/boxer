// The worker behind the viewer page's ?worker= mode: the Go application
// (wasmspike, GOOS=wasip1 as a reactor) and the Rust browser host as two wasm
// modules on this one thread, joined by the fd 0/1 shim in bridge.js. The
// worker owns the cadence: it calls the Go module's frame export per tick
// and yields in between, which is when the page's input arrives; each
// rendered frame's mesh messages are posted to the page, which paints them
// with the painter it already has. Query parameters of this module's URL
// select the scene (`scene`, `rows`, `demo`), the stage (`stage`) and the
// tick rate (`fps`).
import { loadHost, startReactor } from './bridge.js';

const q = new URL(self.location.href).searchParams;
const stage = (q.get('stage') || '1024x600').split('x').map(Number);
const fps = Number(q.get('fps') || '30');
const log = (line) => self.postMessage({ kind: 'log', line });
const PREFIX_INPUT = 0x02;

try {
  const [hostBytes, goBytes] = await Promise.all([
    fetch('./imzero2.wasm').then((r) => r.arrayBuffer()),
    fetch('./wasmspike_wasip1_reactor.wasm').then((r) => r.arrayBuffer()),
  ]);
  const stub = await loadHost(hostBytes, stage[0], stage[1], 1.0,
    (m) => self.postMessage({ kind: 'mesh', bytes: m.buffer }, [m.buffer]));
  self.onmessage = (e) => {
    const m = e.data;
    if (m.kind !== 'input') return;
    const b = new Uint8Array(m.bytes);
    if (b.length > 1 && b[0] === PREFIX_INPUT) stub.input(b.subarray(1));
  };
  self.postMessage({ kind: 'hello', hello: { width: stage[0], height: stage[1], ppp: 1, codec: 'mesh', cadence: 0 } });
  const argv = ['-consumer', 'pipe', '-scene', q.get('scene') || 'gallery', '-rows', q.get('rows') || '200',
    '-frames', q.get('frames') || '1000000', '-warmup', '0', '-stage', `${stage[0]}x${stage[1]}`, '-target', 'wasip1', '-arm', 'viewer-worker'];
  if (q.get('demo')) argv.push('-demo', q.get('demo'));
  const r = await startReactor({ goBytes, stub, argv, log: (l) => { if (!l.startsWith('{')) log(l); } });
  log('worker — running the application');
  let frames = 0;
  const tick = () => {
    const done = r.frame();
    frames++;
    if (done !== 0) { log(`worker — the application exited after ${frames} frames`); return; }
    setTimeout(tick, 1000 / fps);
  };
  tick();
} catch (err) {
  log('worker error: ' + (err && err.stack || err));
}
