// The in-page FFFI2 bridge of the keelson-wasm-frame-cost trial (ADR-0077
// SD1/SD2): the Go frame producer (public/thestack/imzero2/egui2/demo/wasmspike,
// built for GOOS=js or GOOS=wasip1) writes its command stream to fd 1 and
// reads fetcher replies from fd 0; both are served synchronously by this file
// from the fffi2stub wasm module (rust/fffi2stub). No threads, no
// SharedArrayBuffer: a write is a call into the stub, and the stub's reply is
// already queued when Go's blocking read runs — the "sandwich" order.
//
// Runs under Node and in a browser worker; the host passes the loaded module
// bytes and a `log` sink.

export async function loadStub(stubBytes, tableText, stageW, stageH) {
  const { instance } = await WebAssembly.instantiate(stubBytes, {});
  const ex = instance.exports;
  const stub = {
    ex,
    mem: () => new Uint8Array(ex.memory.buffer),
    push(bytes) {
      const p = ex.stub_alloc(bytes.length);
      this.mem().set(bytes, p);
      const n = ex.stub_consume(bytes.length);
      if (n === 0) return null;
      const r = this.mem().slice(ex.stub_reply_ptr(), ex.stub_reply_ptr() + n);
      ex.stub_reply_clear();
      return r;
    },
    stats() {
      const s = (i) => Number(ex.stub_stat(i));
      return { messages: s(0), bytes: s(1), frames: s(2), fetches: s(3), maxMessage: s(4) };
    },
  };
  const t = new TextEncoder().encode(tableText);
  const p = ex.stub_alloc(t.length);
  stub.mem().set(t, p);
  const k = ex.stub_load_table(t.length);
  if (k === 0xffffffff) throw new Error('fffi2stub: fetch table rejected');
  ex.stub_set_stage(stageW, stageH);
  return stub;
}

// The real Rust host (rust/imzero2 built with --features browser): the
// shared interpreter and egui tessellating into the ADR-0128 mesh wire. Its
// only genuine import is the clock; the JavaScript-binding imports that a few
// dependencies declare are stubbed to throw, since nothing in the measured
// scenes reaches them (graph layouts, the time-range picker's wall clock).
// Slots of the host's host_font export, in its numbering; the same four the
// native hosts take as paths.
export const FONT_SLOTS = ['main', 'mono', 'phosphor', 'fallback'];

// fonts: {main, mono, phosphor, fallback} → bytes (ArrayBuffer or Uint8Array),
// each optional; a tab has no font files, so they cross as bytes before init.
export async function loadHost(hostBytes, width, height, ppp, onMesh, paceMs, fonts) {
  const module = await WebAssembly.compile(hostBytes);
  const imports = { env: { now_ms: () => performance.now() } };
  for (const i of WebAssembly.Module.imports(module)) {
    if (i.module === 'env' && i.name === 'now_ms') continue;
    imports[i.module] = imports[i.module] || {};
    if (i.kind === 'function') imports[i.module][i.name] = () => { throw new Error(`unwired import ${i.module}.${i.name} (needs wasm-bindgen glue)`); };
  }
  const instance = await WebAssembly.instantiate(module, imports);
  const ex = instance.exports;
  const mem = () => new Uint8Array(ex.memory.buffer);
  for (const [i, slot] of FONT_SLOTS.entries()) {
    if (!fonts || !fonts[slot]) continue;
    const b = new Uint8Array(fonts[slot]);
    const p = ex.host_alloc(b.length);
    mem().set(b, p);
    ex.host_font(i, b.length);
  }
  ex.host_init(width, height, ppp);
  let stepNs = 0n, steps = 0, frames = 0, meshBytes = 0, lastFrameAt = 0;
  const now = () => (typeof process !== 'undefined' && process.hrtime) ? process.hrtime.bigint() : BigInt(Math.round(performance.now() * 1e6));
  return {
    ex,
    push(bytes) {
      const p = ex.host_alloc(bytes.length);
      mem().set(bytes, p);
      ex.host_write(bytes.length);
      return null; // replies come from step()
    },
    // Called when Go blocks on a read with nothing queued: interpret what
    // arrived (a frame, then its fetch, or a lone fetch) and hand back the
    // reply bytes. A rendered frame's mesh goes to onMesh.
    step() {
      const t0 = now();
      const rendered = ex.host_step();
      stepNs += now() - t0; steps++;
      if (rendered) {
        frames++;
        // one wire message at a time, as the carrier would send them
        const count = ex.host_mesh_count();
        for (let i = 0; i < count; i++) {
          const n = ex.host_mesh_len(i);
          meshBytes += n;
          const m = mem().slice(ex.host_mesh_ptr(i), ex.host_mesh_ptr(i) + n);
          if (onMesh) onMesh(m);
        }
        ex.host_mesh_clear();
        // Pacing for a demo: the Go loop runs as fast as it is answered, so
        // a worker that shows frames holds the reply until the frame's slot
        // is over (a spin, since a worker without SharedArrayBuffer cannot
        // sleep). Measurement runs leave paceMs unset.
        if (paceMs) {
          const t = performance.now();
          const wait = lastFrameAt + paceMs - t;
          if (wait > 0) { const until = t + wait; while (performance.now() < until) { /* spin */ } }
          lastFrameAt = performance.now();
        }
      }
      const rn = ex.host_reply_take();
      if (rn === 0) return null;
      return mem().slice(ex.host_reply_ptr(), ex.host_reply_ptr() + rn);
    },
    // One InputEvent as the viewer page encodes it, without the wire prefix.
    input(bytes) {
      const p = ex.host_alloc(bytes.length);
      mem().set(bytes, p);
      return ex.host_input(bytes.length) === 1;
    },
    // One SessionControl as the page encodes it, without the prefix: 1 when
    // the geometry changed (send a fresh hello), 2 when a paste was queued.
    session(bytes) {
      const p = ex.host_alloc(bytes.length);
      mem().set(bytes, p);
      return ex.host_session(bytes.length);
    },
    geometry() { return { width: ex.host_width(), height: ex.host_height(), ppp: ex.host_ppp() }; },
    // Milliseconds after the last pass at which egui asked to run again.
    repaintDelayMs() { return ex.host_repaint_delay_ms(); },
    lastError() { const n = ex.host_last_error(); return new TextDecoder().decode(mem().slice(ex.host_alloc(0), ex.host_alloc(0) + n)); },
    stats() {
      const s = (i) => Number(ex.host_stat(i));
      return { frames: s(0), bytesIn: s(1), lastInterpretUs: s(3), lastTessellateUs: s(4), lastSerializeUs: s(5), lastBodies: s(6), lastBodiesSent: s(7), errors: s(8), framesUnchanged: s(9), lastSentBbox: [10, 11, 12, 13].map((i) => { const b = new DataView(new ArrayBuffer(4)); b.setUint32(0, Number(ex.host_stat(i))); return Number(b.getFloat32(0).toFixed(1)); }), stepMs: Number(stepNs) / 1e6, steps, meshBytes, jsFrames: frames };
    },
  };
}

// A reply queue shared by both shims: fd 1 writes go to the stub, whatever it
// answers is queued for fd 0 reads.
export function makeQueue(stub, log) {
  const q = [];
  let chunks = 0, writeNs = 0n;
  const now = () => (typeof process !== 'undefined' && process.hrtime) ? process.hrtime.bigint() : BigInt(Math.round(performance.now() * 1e6));
  return {
    write(bytes) {
      const t0 = now();
      chunks++;
      const r = stub.push(bytes);
      if (r) q.push(r);
      writeNs += now() - t0;
    },
    read(into) {
      if (q.length === 0 && stub.step) { const r = stub.step(); if (r) q.push(r); }
      if (q.length === 0) return -1; // would block: the protocol never lets this happen
      const r = q[0];
      const n = Math.min(into.length, r.length);
      into.set(r.subarray(0, n));
      if (n < r.length) q[0] = r.subarray(n); else q.shift();
      return n;
    },
    stats() { return { chunks, bridgeMs: Number(writeNs) / 1e6 }; },
    log,
  };
}

// GOOS=js: the `globalThis.fs` object wasm_exec.js routes syscall/js file
// operations through. Only fds 0/1/2 exist.
export function makeJsFs(queue, stderr) {
  const enosys = () => { const e = new Error('not implemented'); e.code = 'ENOSYS'; return e; };
  return {
    constants: { O_WRONLY: -1, O_RDWR: -1, O_CREAT: -1, O_TRUNC: -1, O_APPEND: -1, O_EXCL: -1, O_DIRECTORY: -1 },
    writeSync(fd, buf) { if (fd === 2 || fd === 1 && false) stderr(buf); return buf.length; },
    write(fd, buf, offset, length, position, cb) {
      const b = buf.subarray(offset, offset + length);
      if (fd === 2) { stderr(b); cb(null, length); return; }
      if (fd === 1) { queue.write(b); cb(null, length); return; }
      cb(enosys());
    },
    read(fd, buf, offset, length, position, cb) {
      if (fd !== 0) { cb(enosys()); return; }
      const n = queue.read(buf.subarray(offset, offset + length));
      if (n < 0) { cb(new Error('fd 0 read with no reply queued (the bridge would block)')); return; }
      cb(null, n);
    },
    fstat(fd, cb) { cb(null, { isDirectory: () => false, isFile: () => false, mode: 0o20000 | 0o666, size: 0, dev: 0, ino: 0, nlink: 1, uid: 0, gid: 0, rdev: 0, blksize: 4096, blocks: 0, atimeMs: 0, mtimeMs: 0, ctimeMs: 0 }); },
    open(p, f, m, cb) { cb(enosys()); }, close(fd, cb) { cb(null); }, fsync(fd, cb) { cb(null); },
    readdir(p, cb) { cb(enosys()); }, mkdir(p, m, cb) { cb(enosys()); }, unlink(p, cb) { cb(enosys()); },
    stat(p, cb) { cb(enosys()); }, lstat(p, cb) { cb(enosys()); }, rename(a, b, cb) { cb(enosys()); },
  };
}

// GOOS=wasip1: the wasi_snapshot_preview1 imports Go's runtime needs, with
// fd 0/1 on the queue and fd 2 on the log. Everything else is absent.
// env: "NAME=value" strings the module sees as its environment (Go's
// os.Getenv; the ADR-0009 registry reads its variables from there).
export function makeWasi(queue, stderr, argv, env = []) {
  let mem;
  const u8 = () => new Uint8Array(mem.buffer);
  const dv = () => new DataView(mem.buffer);
  const ESUCCESS = 0, EAGAIN = 6, EBADF = 8, ENOSYS = 52;
  const enc = new TextEncoder();
  const args = argv.map((a) => enc.encode(a + '\0'));
  const envs = env.map((e) => enc.encode(e + '\0'));
  const nowNs = () => (typeof process !== 'undefined' && process.hrtime) ? process.hrtime.bigint() : BigInt(Math.round(performance.now() * 1e6));
  const imports = {
    args_sizes_get(pc, pb) { dv().setUint32(pc, args.length, true); dv().setUint32(pb, args.reduce((s, a) => s + a.length, 0), true); return ESUCCESS; },
    args_get(argvp, buf) { let off = buf; for (let i = 0; i < args.length; i++) { dv().setUint32(argvp + 4 * i, off, true); u8().set(args[i], off); off += args[i].length; } return ESUCCESS; },
    environ_sizes_get(pc, pb) { dv().setUint32(pc, envs.length, true); dv().setUint32(pb, envs.reduce((s, a) => s + a.length, 0), true); return ESUCCESS; },
    environ_get(envp, buf) { let off = buf; for (let i = 0; i < envs.length; i++) { dv().setUint32(envp + 4 * i, off, true); u8().set(envs[i], off); off += envs[i].length; } return ESUCCESS; },
    clock_time_get(id, prec, out) { const t = id === 0 ? BigInt(Date.now()) * 1000000n : nowNs(); dv().setBigUint64(out, t, true); return ESUCCESS; },
    random_get(p, n) { crypto.getRandomValues(u8().subarray(p, p + n)); return ESUCCESS; },
    proc_exit(code) { throw { wasiExit: code }; },
    sched_yield() { return ESUCCESS; },
    // Go's runtime sleeps and waits on timers through poll_oneoff. Only clock
    // subscriptions are honoured: the shortest one is slept for (Atomics.wait
    // where a SharedArrayBuffer exists, a spin otherwise) and reported as the
    // single event. An fd subscription cannot be served here (fd 0 is never
    // "readable" ahead of the protocol), so it is reported ready at once.
    poll_oneoff(inp, out, n, nev) {
      const d = dv();
      let best = -1, bestNs = 0n, events = 0;
      for (let i = 0; i < n; i++) {
        const sub = inp + i * 48;
        const tag = d.getUint8(sub + 8);
        if (tag === 0) {
          const timeout = d.getBigUint64(sub + 24, true);
          const flags = d.getUint16(sub + 40, true);
          const rel = (flags & 1) ? timeout - nowNs() : timeout;
          if (best < 0 || rel < bestNs) { best = i; bestNs = rel; }
        }
      }
      if (best >= 0) {
        const ms = Number(bestNs) / 1e6;
        if (ms > 0) {
          if (typeof SharedArrayBuffer !== 'undefined') { Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms); }
          else { const t = performance.now() + ms; while (performance.now() < t) { /* spin */ } }
        }
      }
      for (let i = 0; i < n; i++) {
        const sub = inp + i * 48, tag = d.getUint8(sub + 8);
        if (tag === 0 && i !== best) continue;
        const ev = out + events * 32;
        d.setBigUint64(ev, d.getBigUint64(sub, true), true);
        d.setUint16(ev + 8, 0, true);
        d.setUint8(ev + 10, tag);
        d.setBigUint64(ev + 16, 0n, true);
        d.setUint16(ev + 24, 0, true);
        events++;
      }
      d.setUint32(nev, events, true);
      return ESUCCESS;
    },
    fd_close() { return ESUCCESS; },
    fd_fdstat_get(fd, out) { if (fd > 2) return EBADF; const d = dv(); d.setUint8(out, 2); d.setUint16(out + 2, 0, true); d.setBigUint64(out + 8, 0n, true); d.setBigUint64(out + 16, 0n, true); return ESUCCESS; },
    fd_fdstat_set_flags() { return ESUCCESS; },
    fd_prestat_get() { return EBADF; },
    fd_prestat_dir_name() { return EBADF; },
    fd_write(fd, iovs, n, nw) {
      const d = dv(); let total = 0;
      for (let i = 0; i < n; i++) {
        const p = d.getUint32(iovs + i * 8, true), l = d.getUint32(iovs + i * 8 + 4, true);
        const b = u8().subarray(p, p + l);
        if (fd === 2) stderr(b);
        else if (fd === 1) queue.write(b);
        else return EBADF;
        total += l;
      }
      d.setUint32(nw, total, true); return ESUCCESS;
    },
    fd_read(fd, iovs, n, nr) {
      if (fd !== 0) return EBADF;
      const d = dv(); let total = 0;
      for (let i = 0; i < n; i++) {
        const p = d.getUint32(iovs + i * 8, true), l = d.getUint32(iovs + i * 8 + 4, true);
        const k = queue.read(u8().subarray(p, p + l));
        if (k < 0) { if (total === 0) { queue.log('fd_read with no reply queued'); return EAGAIN; } break; }
        total += k;
        if (k < l) break;
      }
      d.setUint32(nr, total, true); return ESUCCESS;
    },
  };
  // Anything else Go's runtime imports (file and path calls a program with
  // no filesystem never reaches) answers ENOSYS.
  const seen = new Set();
  const stubbed = new Proxy(imports, {
    get(t, name) {
      if (name in t) return t[name];
      return () => { if (!seen.has(name)) { seen.add(name); queue.log('wasi: ' + String(name) + ' called; answered ENOSYS'); } return ENOSYS; };
    },
  });
  // The module's own host imports (Go `//go:wasmimport env …`): HTTP, which
  // a wasip1 module cannot do itself. The Go side frames a request as
  // method, url, "Name: value" lines and body, each behind a u32 length;
  // the reply is status (0 when the host could not perform it, the message
  // in the header slot), headers and body the same way. Performed with a
  // synchronous XMLHttpRequest, which a worker may issue; a host without
  // one (Node) answers status 0.
  let httpReply = null;
  // The asynchronous path (ADR-0263 Update 2026-10-03): http_start begins a
  // fetch() and returns a handle at once; the reply waits in `pending` until
  // the module collects it, and onSettled (the worker's wake) earns the
  // module a frame to notice. Frames keep coming while a request is out.
  const pending = new Map();
  let nextHandle = 1;
  let onSettled = () => {};
  const u32 = (b, o) => new DataView(b.buffer, b.byteOffset).getUint32(o, true);
  const field = (b, o) => { const n = u32(b, o); return [b.subarray(o + 4, o + 4 + n), o + 4 + n]; };
  const frame = (status, headers, body) => {
    const h = enc.encode(headers);
    const out = new Uint8Array(12 + h.length + body.length);
    const d = new DataView(out.buffer);
    d.setUint32(0, status, true); d.setUint32(4, h.length, true); out.set(h, 8);
    d.setUint32(8 + h.length, body.length, true); out.set(body, 12 + h.length);
    return out;
  };
  const env_imports = {
    http_fetch(reqPtr, n) {
      const req = u8().slice(reqPtr, reqPtr + n);
      let o = 0, method, url, headers, body;
      [method, o] = field(req, o); [url, o] = field(req, o); [headers, o] = field(req, o); [body, o] = field(req, o);
      const dec = new TextDecoder();
      if (typeof XMLHttpRequest === 'undefined') { httpReply = frame(0, 'no synchronous HTTP in this host', new Uint8Array()); return httpReply.length; }
      try {
        const xhr = new XMLHttpRequest();
        xhr.open(dec.decode(method), dec.decode(url), false);
        xhr.responseType = 'arraybuffer';
        for (const line of dec.decode(headers).split('\n')) {
          const i = line.indexOf(':'); if (i < 0) continue;
          const k = line.slice(0, i).trim();
          if (/^(host|content-length|connection|accept-encoding|user-agent)$/i.test(k)) continue; // the browser owns these
          try { xhr.setRequestHeader(k, line.slice(i + 1).trim()); } catch (_) { /* forbidden header name */ }
        }
        xhr.send(body.length ? body : null);
        const respBody = xhr.response ? new Uint8Array(xhr.response) : new Uint8Array();
        httpReply = frame(xhr.status || 0, xhr.status ? xhr.getAllResponseHeaders().replace(/\r/g, '') : 'network error', respBody);
      } catch (e) {
        httpReply = frame(0, String(e && e.message || e), new Uint8Array());
      }
      return httpReply.length;
    },
    http_take(dst, cap) {
      if (!httpReply) return 0;
      const n = Math.min(cap, httpReply.length);
      u8().set(httpReply.subarray(0, n), dst);
      httpReply = null;
      return n;
    },
    http_start(reqPtr, n) {
      if (typeof fetch === 'undefined') return 0;
      const req = u8().slice(reqPtr, reqPtr + n);
      let o = 0, method, url, headers, body;
      [method, o] = field(req, o); [url, o] = field(req, o); [headers, o] = field(req, o); [body, o] = field(req, o);
      const dec = new TextDecoder();
      const h = nextHandle++;
      const ctl = new AbortController();
      const entry = { ctl, reply: null };
      pending.set(h, entry);
      const hdrs = new Headers();
      for (const line of dec.decode(headers).split('\n')) {
        const i = line.indexOf(':'); if (i < 0) continue;
        const k = line.slice(0, i).trim();
        if (/^(host|content-length|connection|accept-encoding|user-agent)$/i.test(k)) continue; // the browser owns these
        try { hdrs.append(k, line.slice(i + 1).trim()); } catch (_) { /* forbidden header name */ }
      }
      const m = dec.decode(method);
      fetch(dec.decode(url), { method: m, headers: hdrs, body: body.length && m !== 'GET' && m !== 'HEAD' ? body : undefined, signal: ctl.signal })
        .then(async (r) => {
          const b = new Uint8Array(await r.arrayBuffer());
          const hs = []; r.headers.forEach((v, k) => hs.push(k + ': ' + v));
          return frame(r.status, hs.join('\n'), b);
        })
        .catch((e) => frame(0, String(e && e.message || e), new Uint8Array()))
        .then((reply) => { if (pending.get(h) === entry) { entry.reply = reply; onSettled(); } });
      return h;
    },
    http_ready(h) {
      const e = pending.get(h);
      return e && e.reply ? e.reply.length : 0;
    },
    http_collect(h, dst, cap) {
      const e = pending.get(h);
      if (!e || !e.reply) return 0;
      const n = Math.min(cap, e.reply.length);
      u8().set(e.reply.subarray(0, n), dst);
      pending.delete(h);
      return n;
    },
    http_abort(h) {
      const e = pending.get(h);
      if (!e) return;
      pending.delete(h);
      e.ctl.abort();
    },
  };
  return { imports: stubbed, env: env_imports, setMemory(m) { mem = m; }, onHttpSettled(fn) { onSettled = fn; }, httpPending: () => pending.size, names: (mod) => WebAssembly.Module.imports(mod).map((i) => i.name) };
}

// Builds a complete import object for a module from the shim, one entry per
// import the module declares (a Proxy cannot back WebAssembly.instantiate).
export function wasiImportsFor(module, shim) {
  const o = {}, env = {};
  for (const i of WebAssembly.Module.imports(module)) {
    if (i.module === 'wasi_snapshot_preview1') o[i.name] = shim.imports[i.name];
    else if (i.module === 'env') env[i.name] = (shim.env && shim.env[i.name]) || (() => { throw new Error('unwired host import env.' + i.name); });
  }
  return { wasi_snapshot_preview1: o, env };
}

// Runs one arm. `goBytes` is the Go module, `target` "js" or "wasip1",
// `argv` the wasmspike flags. Resolves with {result, stub, bridge, wallMs}
// once Go exits; the RESULT line is parsed from what Go wrote to fd 2.
export async function runArm({ target, goBytes, stub, argv, log, GoCtor }) {
  const lines = []; let partial = '';
  const stderr = (b) => {
    partial += new TextDecoder().decode(b);
    let i;
    while ((i = partial.indexOf('\n')) >= 0) { const l = partial.slice(0, i); partial = partial.slice(i + 1); lines.push(l); if (log) log(l); }
  };
  const queue = makeQueue(stub, log || (() => {}));
  const t0 = performance.now();
  if (target === 'js') {
    globalThis.fs = makeJsFs(queue, stderr);
    const go = new GoCtor();
    go.argv = ['wasmspike', ...argv];
    go.env = {};
    const exited = new Promise((res) => { go.exit = (code) => res(code); });
    const { instance } = await WebAssembly.instantiate(goBytes, go.importObject);
    go.run(instance);
    await exited;
    // A Go timer still scheduled at exit would call _resume on an exited
    // program (wasm_exec.js throws); drop them.
    for (const id of go._scheduledTimeouts.values()) clearTimeout(id);
    go._scheduledTimeouts.clear();
  } else if (target === 'wasip1') {
    const wasi = makeWasi(queue, stderr, ['wasmspike', ...argv]);
    const module = await WebAssembly.compile(goBytes);
    const instance = await WebAssembly.instantiate(module, wasiImportsFor(module, wasi));
    wasi.setMemory(instance.exports.memory);
    try { instance.exports._start(); } catch (e) { if (!(e && e.wasiExit !== undefined)) throw e; }
  } else {
    throw new Error('unknown target ' + target);
  }
  const wallMs = performance.now() - t0;
  const r = lines.find((l) => l.startsWith('RESULT '));
  return { result: r ? JSON.parse(r.slice(7)) : null, stub: stub.stats(), bridge: queue.stats(), wallMs, lines };
}

// Starts a wasip1 reactor build of the Go module (-buildmode=c-shared, the
// spike's -reactor flag): `_initialize` runs main, which sets the loop up and
// returns; `frame()` then runs one frame per call, so the caller owns the
// cadence and yields between frames — the worker receives input then. The
// result's `frame` returns 0 while the loop runs and 1 once it stopped.
export async function startReactor({ goBytes, stub, argv, log, env }) {
  const lines = []; let partial = '';
  const stderr = (b) => {
    partial += new TextDecoder().decode(b);
    let i;
    while ((i = partial.indexOf('\n')) >= 0) { const l = partial.slice(0, i); partial = partial.slice(i + 1); lines.push(l); if (log) log(l); }
  };
  const queue = makeQueue(stub, log || (() => {}));
  // Go runs neither main nor reads argv in a c-shared module: the arguments
  // go NUL-separated into the buffer the module exports, and setup runs the
  // entry the module registered on them (browserhost.SetMain). A module
  // that needs a flag to know it is a reactor (the trial's spike) gets it
  // from the caller's argv.
  const wasi = makeWasi(queue, stderr, ['wasmspike'], env || []);
  const module = await WebAssembly.compile(goBytes);
  const instance = await WebAssembly.instantiate(module, wasiImportsFor(module, wasi));
  wasi.setMemory(instance.exports.memory);
  instance.exports._initialize();
  const args = new TextEncoder().encode(argv.join('\0'));
  if (args.length > instance.exports.argcap()) throw new Error('reactor: arguments exceed the module\'s buffer');
  new Uint8Array(instance.exports.memory.buffer).set(args, instance.exports.argbuf());
  const ready = instance.exports.setup(args.length);
  if (ready !== 0) throw new Error('reactor: setup did not leave a frame loop (' + ready + ')');
  return {
    frame() { try { return instance.exports.frame(); } catch (e) { if (e && e.wasiExit !== undefined) return 1; throw e; } },
    // A request the module started asynchronously settled: the worker
    // answers with a frame, in which the waiting goroutine collects it.
    onHttpSettled(fn) { wasi.onHttpSettled(fn); },
    lines,
    stats: () => ({ stub: stub.stats(), bridge: queue.stats() }),
  };
}
