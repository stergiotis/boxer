// Node host for the wasm arms of the keelson-wasm-frame-cost trial.
//
//   node run_node.mjs --target js|wasip1 --go <wasmspike.wasm> --stub <fffi2stub.wasm>
//        --table <fetchtable.txt> --wasm-exec <wasm_exec.js> [--stage WxH] -- <wasmspike flags>
//
// Prints one JSON line prefixed `ARM ` with Go's RESULT, the stub's counters,
// the bridge's own time and the wall time.
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { loadHost, loadStub, runArm, startReactor } from './bridge.js';

const args = process.argv.slice(2);
const opt = {};
const rest = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--') { rest.push(...args.slice(i + 1)); break; }
  if (args[i].startsWith('--')) { opt[args[i].slice(2)] = args[++i]; } else rest.push(args[i]);
}
const stage = (opt.stage || '1024x600').split('x').map(Number);
const table = readFileSync(opt.table, 'utf8');
// --host <imzero2.wasm> drives the real Rust host instead of the stub; the
// fetch table is then unused by the peer (the host answers from its state).
let meshFrames = 0, meshBytes = 0, meshLast = 0;
// per message kind, by the wire's first byte (1 frame, 2 texture, 3
// retirement, other = session control), for the parity check against the
// native appliance (mesh_count.mjs)
const meshKinds = { frame: { n: 0, bytes: 0 }, texture: { n: 0, bytes: 0 }, retirement: { n: 0, bytes: 0 }, other: { n: 0, bytes: 0 } };
let firstFrameBytes = 0, firstFrameClosed = false; // frame + textures up to the first retirement
const PREFIX_MESH = 0x04;
const stub = opt.host
  ? await loadHost(readFileSync(opt.host), stage[0], stage[1], 1.0, (m) => {
    meshFrames++; meshBytes += m.length; meshLast = m.length;
    const k = m[0] !== PREFIX_MESH ? 'other' : m[1] === 1 ? 'frame' : m[1] === 2 ? 'texture' : m[1] === 3 ? 'retirement' : 'other';
    meshKinds[k].n++; meshKinds[k].bytes += m.length - (k === 'other' ? 0 : 1);
    if (!firstFrameClosed && k !== 'other') { if (k === 'retirement') firstFrameClosed = true; else firstFrameBytes += m.length - 1; }
  })
  : await loadStub(readFileSync(opt.stub), table, stage[0], stage[1]);
const goBytes = readFileSync(opt.go);
let GoCtor = null;
if (opt.target === 'js') {
  // wasm_exec.js installs `Go` on globalThis; it needs no globalThis.fs of
  // its own since the bridge installs one before the first syscall.
  globalThis.fs = { constants: {}, writeSync() { return 0; } };
  createRequire(import.meta.url)(opt['wasm-exec']);
  GoCtor = globalThis.Go;
}
const argv = [...rest];
if (!argv.includes('-stage')) argv.push('-stage', `${stage[0]}x${stage[1]}`);
if (!argv.includes('-fetchTable')) argv.push('-fetchTable', table);
let out;
if (opt.reactor) {
  // --reactor 1 with a c-shared wasip1 module: frames are driven from here
  const t0 = performance.now();
  const r = await startReactor({ goBytes, stub, argv, log: (l) => { if (!l.startsWith('RESULT ')) process.stderr.write(l + '\n'); } });
  while (r.frame() === 0) { /* one frame per call */ }
  const line = r.lines.find((l) => l.startsWith('RESULT '));
  const st = r.stats();
  out = { result: line ? JSON.parse(line.slice(7)) : null, stub: st.stub, bridge: st.bridge, wallMs: performance.now() - t0 };
} else {
  out = await runArm({ target: opt.target, goBytes, stub, argv, GoCtor, log: (l) => { if (!l.startsWith('RESULT ')) process.stderr.write(l + '\n'); } });
}
const host = opt.host ? { ...out.stub, meshFrames, meshBytes, meshLast, meshKinds, firstFrameBytes, lastError: stub.lastError() } : undefined;
process.stdout.write('ARM ' + JSON.stringify({ result: out.result, stub: opt.host ? undefined : out.stub, host, bridge: out.bridge, wallMs: out.wallMs }) + '\n');
