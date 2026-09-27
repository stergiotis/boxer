// Counts the mesh wire a native headless mesh appliance sends to one viewer,
// per message kind, so it can be set beside what the browser host posts to
// the page for the same scene (the parity question of ADR-0263 §Consequences).
//
//   node mesh_count.mjs ws://127.0.0.1:PORT/ws <frames> [WxH] [ppp] [timeoutSeconds]
//
// Speaks the viewer page's side of the carrier handshake (ADR-0086): a
// client hello, then — once promoted to active by the roster — the viewport,
// as the page's measureAndSend would. Then it counts PREFIX_MESH messages by
// their first byte (1 frame, 2 texture update, 3 retirement) until <frames>
// frame messages have arrived, and prints one JSON line.
const [url, framesArg, sizeArg, pppArg, timeoutArg] = process.argv.slice(2);
const timeoutMs = Number(timeoutArg || 120) * 1000;
const wantFrames = Number(framesArg || 200);
const [w, h] = (sizeArg || '1000x660').split('x').map(Number);
const ppp = Number(pppArg || 1);

const PREFIX_SESSION = 0x03, PREFIX_MESH = 0x04;

// Minimal protobuf writer for the two messages this client sends.
class Writer {
  constructor() { this.parts = []; }
  varint(n) { const b = []; let v = BigInt(n); do { let x = Number(v & 0x7fn); v >>= 7n; if (v) x |= 0x80; b.push(x); } while (v); this.parts.push(Uint8Array.from(b)); return this; }
  tag(field, wire) { return this.varint((field << 3) | wire); }
  bool(field, v) { return this.tag(field, 0).varint(v ? 1 : 0); }
  str(field, s) { const b = new TextEncoder().encode(s); this.tag(field, 2).varint(b.length); this.parts.push(b); return this; }
  float(field, f) { this.tag(field, 5); const b = new Uint8Array(4); new DataView(b.buffer).setFloat32(0, f, true); this.parts.push(b); return this; }
  sub(field, wr) { const b = wr.bytes(); this.tag(field, 2).varint(b.length); this.parts.push(b); return this; }
  bytes() { const n = this.parts.reduce((s, p) => s + p.length, 0); const out = new Uint8Array(n); let o = 0; for (const p of this.parts) { out.set(p, o); o += p.length; } return out; }
}
const framed = (prefix, wr) => { const b = wr.bytes(); const out = new Uint8Array(1 + b.length); out[0] = prefix; out.set(b, 1); return out; };

// Minimal reader: enough to find the session control's oneof field.
function sessionKind(bytes) {
  let o = 0;
  const varint = () => { let r = 0n, s = 0n; for (;;) { const b = bytes[o++]; r |= BigInt(b & 0x7f) << s; if (!(b & 0x80)) return r; s += 7n; } };
  while (o < bytes.length) {
    const key = Number(varint()); const field = key >> 3, wire = key & 7;
    if (wire === 2) { const n = Number(varint()); const sub = bytes.subarray(o, o + n); o += n; return { field, sub }; }
    if (wire === 0) varint(); else if (wire === 5) o += 4; else if (wire === 1) o += 8; else break;
  }
  return null;
}
// Roster{ you_id=1, you_role=2, active_id=3, … }: active when you_id == active_id.
function rosterSaysActive(sub) {
  let o = 0, you = 0n, active = 0n;
  const varint = () => { let r = 0n, s = 0n; for (;;) { const b = sub[o++]; r |= BigInt(b & 0x7f) << s; if (!(b & 0x80)) return r; s += 7n; } };
  while (o < sub.length) {
    const key = Number(varint()); const field = key >> 3, wire = key & 7;
    if (wire === 0) { const v = varint(); if (field === 1) you = v; else if (field === 3) active = v; }
    else if (wire === 2) o += Number(varint()); else if (wire === 5) o += 4; else if (wire === 1) o += 8; else break;
  }
  return you !== 0n && you === active;
}

const counts = { frame: { n: 0, bytes: 0 }, texture: { n: 0, bytes: 0 }, retirement: { n: 0, bytes: 0 }, other: { n: 0, bytes: 0 } };
let firstFrameBytes = 0, firstFrameClosed = false;
let helloSeen = false, resizeSent = false, t0 = 0, tFirst = 0;
const ws = new WebSocket(url);
ws.binaryType = 'arraybuffer';
const done = (why) => {
  const dt = (performance.now() - tFirst) / 1000;
  const f = counts.frame.n || 1;
  process.stdout.write(JSON.stringify({ why, frames: counts.frame.n, firstFrameBytes, seconds: Number(dt.toFixed(2)), fps: Number((counts.frame.n / dt).toFixed(1)),
    bytesPerFrame: { frame: Math.round(counts.frame.bytes / f), texture: Math.round(counts.texture.bytes / f), retirement: Math.round(counts.retirement.bytes / f), total: Math.round((counts.frame.bytes + counts.texture.bytes + counts.retirement.bytes + counts.other.bytes) / f) },
    messagesPerFrame: { texture: Number((counts.texture.n / f).toFixed(2)), retirement: Number((counts.retirement.n / f).toFixed(2)) }, counts }) + '\n');
  ws.close(); setTimeout(() => process.exit(0), 50);
};
const timer = setTimeout(() => done('timeout'), timeoutMs);
ws.onopen = () => { if (process.env.MESH_COUNT_DEBUG) console.error('open'); ws.send(framed(PREFIX_SESSION, new Writer().sub(6, new Writer().bool(1, true).str(2, 'mesh_count')))); };
ws.onclose = (e) => { if (process.env.MESH_COUNT_DEBUG) console.error('close', e.code, e.reason); };
ws.onerror = (e) => { console.error('ws error', e.message || e); process.exit(1); };
ws.onmessage = (e) => {
  const data = new Uint8Array(e.data);
  if (data.length < 1) return;
  const payload = data.subarray(1);
  if (data[0] === PREFIX_SESSION) {
    const k = sessionKind(payload);
    if (process.env.MESH_COUNT_DEBUG) console.error('session field', k && k.field, 'len', payload.length);
    if (!k) return;
    if (k.field === 1) { helloSeen = true; }
    if (k.field === 7 && !resizeSent && rosterSaysActive(k.sub)) {
      resizeSent = true;
      ws.send(framed(PREFIX_SESSION, new Writer().sub(2, new Writer().float(1, w).float(2, h).float(3, ppp))));
    }
    return;
  }
  if (data[0] !== PREFIX_MESH) { if (process.env.MESH_COUNT_DEBUG) console.error('prefix', data[0], 'len', data.length); return; }
  if (!resizeSent) { if (process.env.MESH_COUNT_DEBUG) console.error('mesh before resize, kind', payload[0]); return; } // count only at the requested geometry
  const kind = payload[0] === 1 ? 'frame' : payload[0] === 2 ? 'texture' : payload[0] === 3 ? 'retirement' : 'other';
  if (kind === 'frame') {
    if (counts.frame.n === 0) tFirst = performance.now();
    counts.frame.n++;
  }
  counts[kind].bytes += payload.length; if (kind !== 'frame') counts[kind].n++;
  if (!firstFrameClosed) { if (kind === 'retirement') firstFrameClosed = true; else firstFrameBytes += payload.length; }
  if (counts.frame.n >= wantFrames) { clearTimeout(timer); done('frames'); }
};
