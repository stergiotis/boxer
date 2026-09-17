// Test runtime for the opt-in viewer browser regression harness (ADR-0242).
// run.sh injects this file AFTER the viewer page's own inline script, so the
// page is fully initialised (listeners installed, socket constructed) when the
// suites below it register their cases.
//
// Everything here is test-owned: the protobuf writer/reader used to build
// server frames and to decode what the page sent is written independently of
// the page's own codec, so a case asserts on wire bytes rather than on the
// page's opinion of them. Nothing in a suite matches page source text.
"use strict";

(function () {
  const H = window.__imzero2Harness;
  if (!H) throw new Error("harness stub did not run before the viewer script");

  const PREFIX = { VIDEO: 0x01, INPUT: 0x02, SESSION: 0x03, MESH: 0x04 };
  H.PREFIX = PREFIX;

  // ---- protobuf writer (test side) ---------------------------------------
  // Fields are always emitted, including zero values: these are server frames
  // fed to the page's reader, and being explicit keeps a test's intent visible.
  class Out {
    constructor() { this.b = []; }
    varint(v) {
      let n = typeof v === "bigint" ? v : BigInt(Math.trunc(Number(v)));
      if (n < 0n) throw new Error("negative varint");
      for (;;) {
        const byte = Number(n & 0x7fn);
        n >>= 7n;
        if (n === 0n) { this.b.push(byte); return this; }
        this.b.push(byte | 0x80);
      }
    }
    tag(f, w) { return this.varint((f << 3) | w); }
    uint(f, v) { return this.tag(f, 0).varint(v); }
    bool(f, v) { return this.tag(f, 0).varint(v ? 1 : 0); }
    f32(f, v) {
      this.tag(f, 5);
      const t = new Uint8Array(4);
      new DataView(t.buffer).setFloat32(0, v, true);
      for (const x of t) this.b.push(x);
      return this;
    }
    str(f, s) {
      const e = new TextEncoder().encode(s === undefined || s === null ? "" : String(s));
      this.tag(f, 2).varint(e.length);
      for (const x of e) this.b.push(x);
      return this;
    }
    raw(f, u8) {
      this.tag(f, 2).varint(u8.length);
      for (const x of u8) this.b.push(x);
      return this;
    }
    sub(f, inner) { return this.raw(f, inner.bytes()); }
    bytes() { return new Uint8Array(this.b); }
  }
  H.Out = Out;

  // ---- protobuf reader (test side) ---------------------------------------
  function walk(b) {
    const out = [];
    let i = 0;
    const varint = () => {
      let r = 0n, s = 0n;
      for (;;) {
        const byte = b[i++];
        if (byte === undefined) throw new Error("truncated varint");
        r |= BigInt(byte & 0x7f) << s;
        if ((byte & 0x80) === 0) return r;
        s += 7n;
      }
    };
    while (i < b.length) {
      const t = Number(varint());
      const f = t >>> 3, w = t & 7;
      if (w === 0) out.push({ f: f, w: w, u: varint() });
      else if (w === 5) {
        const dv = new DataView(b.buffer, b.byteOffset + i, 4);
        out.push({ f: f, w: w, f32: dv.getFloat32(0, true) });
        i += 4;
      } else if (w === 1) {
        const dv = new DataView(b.buffer, b.byteOffset + i, 8);
        out.push({ f: f, w: w, f64: dv.getFloat64(0, true), u64: dv.getBigUint64(0, true) });
        i += 8;
      } else if (w === 2) {
        const n = Number(varint());
        out.push({ f: f, w: w, bytes: b.subarray(i, i + n) });
        i += n;
      } else {
        throw new Error("unsupported wire type " + w);
      }
    }
    return out;
  }
  const pick = (fs, n) => fs.find((x) => x.f === n);
  const gU = (fs, n) => { const x = pick(fs, n); return x ? Number(x.u) : 0; };
  const gB = (fs, n) => { const x = pick(fs, n); return x ? x.u !== 0n : false; };
  const gF = (fs, n) => { const x = pick(fs, n); return x ? x.f32 : 0; };
  const gS = (fs, n) => { const x = pick(fs, n); return x ? new TextDecoder().decode(x.bytes) : ""; };

  // InputEvent oneof (proto/boxer/imzero2/v1/input.proto); 9 = focus is the
  // ADR-0242 addition.
  const INPUT_KINDS = {
    1: "mouse_move", 2: "mouse_button", 3: "mouse_wheel", 4: "key",
    5: "text", 6: "pointer_gone", 7: "pinch_zoom", 8: "accesskit_action",
    9: "focus",
  };
  // SessionControl oneof; the client sends 2,3,4,5,6,8,9.
  const SESSION_KINDS = {
    1: "hello", 2: "viewport_resize", 3: "ping", 4: "set_cadence",
    5: "decode_capabilities", 6: "client_hello", 7: "roster",
    8: "take_session", 9: "clipboard", 10: "cursor_shape",
  };

  function decodeOneof(frame, prefix, names) {
    if (frame[0] !== prefix) throw new Error("frame prefix " + frame[0] + ", expected " + prefix);
    const top = walk(frame.subarray(1));
    const m = { prefix: prefix, oneofCount: top.length, kind: "unknown", field: 0, frameBytes: frame.length };
    if (top.length !== 1) { m.kind = "malformed"; return m; }
    const only = top[0];
    m.field = only.f;
    m.kind = names[only.f] || ("field" + only.f);
    m.wire = only.w;
    m.body = only.w === 2 ? only.bytes : new Uint8Array(0);
    m.bodyLength = m.body.length;
    m.fields = only.w === 2 ? walk(m.body) : [];
    return m;
  }

  H.decode = {
    fields: walk,
    input(frame) {
      const m = decodeOneof(frame, PREFIX.INPUT, INPUT_KINDS);
      const f = m.fields;
      switch (m.kind) {
        case "mouse_move": m.x = gF(f, 1); m.y = gF(f, 2); break;
        case "mouse_button":
          m.x = gF(f, 1); m.y = gF(f, 2); m.button = gU(f, 3);
          m.pressed = gB(f, 4); m.modifiers = gU(f, 5);
          break;
        case "mouse_wheel":
          m.dx = gF(f, 1); m.dy = gF(f, 2); m.unit = gU(f, 3); m.modifiers = gU(f, 4);
          break;
        case "key":
          m.key = gS(f, 1); m.code = gS(f, 2); m.pressed = gB(f, 3);
          m.repeat = gB(f, 4); m.modifiers = gU(f, 5);
          break;
        case "text": m.text = gS(f, 1); break;
        case "pinch_zoom": m.factor = gF(f, 1); break;
        case "focus": m.focused = gB(f, 1); break;
        default: break;
      }
      return m;
    },
    session(frame) {
      const m = decodeOneof(frame, PREFIX.SESSION, SESSION_KINDS);
      const f = m.fields;
      switch (m.kind) {
        case "viewport_resize": m.lw = gF(f, 1); m.lh = gF(f, 2); m.scale = gF(f, 3); break;
        case "ping": m.nonce = gU(f, 1); break;
        case "set_cadence": m.mode = gU(f, 1); break;
        case "client_hello": m.webcodecs = gB(f, 1); m.label = gS(f, 2); break;
        case "clipboard": m.text = gS(f, 1); break;
        case "decode_capabilities":
          m.codecs = f.filter((x) => x.f === 1).map((x) => {
            const c = walk(x.bytes);
            return { codec: gS(c, 1), supported: gB(c, 2), smooth: gB(c, 3), powerEfficient: gB(c, 4) };
          });
          m.webgl2 = gB(f, 2);
          break;
        case "take_session": m.empty = m.bodyLength === 0; break;
        default: break;
      }
      return m;
    },
  };

  // ---- server frame builders ---------------------------------------------
  const framed = (prefix, body) => {
    const out = new Uint8Array(1 + body.length);
    out[0] = prefix;
    out.set(body, 1);
    return out;
  };
  const u16 = (v) => Math.max(0, Math.min(0xffff, Math.round(v)));

  H.wire = {
    framed: framed,
    hello(o) {
      const h = new Out()
        .uint(1, o.width).uint(2, o.height).f32(3, o.ppp === undefined ? 1 : o.ppp)
        .uint(4, o.cadence || 0).str(5, o.codec === undefined ? "" : o.codec);
      return framed(PREFIX.SESSION, new Out().sub(1, h).bytes());
    },
    // youRole/role: 1 = active, anything else = passive.
    roster(o) {
      const conns = o.connections || [];
      const r = new Out()
        .uint(1, o.youId === undefined ? 1 : o.youId)
        .uint(2, o.youRole || 0)
        .uint(3, o.activeId === undefined ? 0 : o.activeId)
        .uint(4, conns.length)
        .uint(5, o.max === undefined ? 4 : o.max);
      for (const c of conns) {
        r.sub(6, new Out().uint(1, c.id).uint(2, c.role || 0).str(3, c.label || "")
          .bool(4, c.webcodecs === undefined ? true : c.webcodecs));
      }
      return framed(PREFIX.SESSION, new Out().sub(7, r).bytes());
    },
    clipboard(text) {
      return framed(PREFIX.SESSION, new Out().sub(9, new Out().str(1, text)).bytes());
    },
    cursor(shape) {
      return framed(PREFIX.SESSION, new Out().sub(10, new Out().uint(1, shape)).bytes());
    },
    video(o) {
      const data = o.data || new Uint8Array([0, 0, 0, 1, 0x67, 0x42, 0xe0, 0x1e, 0, 0, 0, 1, 0x65, 0x88]);
      const v = new Out()
        .uint(1, o.frameIndex || 0).uint(2, o.timestampMicros || 0)
        .bool(3, o.keyframe === undefined ? true : o.keyframe).raw(4, data);
      return framed(PREFIX.VIDEO, v.bytes());
    },
    // ---- mesh lane (ADR-0128 layouts; subtype 3 is the ADR-0242 addition) --
    mesh: {
      // Body payload as meshRegisterBody parses it: clip u16x4 (1/8 px),
      // tex u32, n_verts u32, idx_width u8, n_idx u32, verts, indices.
      body(o) {
        const verts = o.verts || [];
        const idx = o.indices || [];
        const wide = !!o.wide;
        const n = 8 + 4 + 4 + 1 + 4 + verts.length * 12 + idx.length * (wide ? 4 : 2);
        const b = new Uint8Array(n);
        const dv = new DataView(b.buffer);
        const clip = o.clip || [0, 0, 64, 64];
        let p = 0;
        for (let i = 0; i < 4; i++) { dv.setUint16(p, u16(clip[i] * 8), true); p += 2; }
        dv.setUint32(p, o.texKey === undefined ? 0 : o.texKey, true); p += 4;
        dv.setUint32(p, verts.length, true); p += 4;
        b[p] = wide ? 4 : 2; p += 1;
        dv.setUint32(p, idx.length, true); p += 4;
        for (const v of verts) {
          dv.setUint16(p, u16((v.x || 0) * 8), true);
          dv.setUint16(p + 2, u16((v.y || 0) * 8), true);
          dv.setUint16(p + 4, u16((v.u || 0) * 65535), true);
          dv.setUint16(p + 6, u16((v.v || 0) * 65535), true);
          const c = v.rgba || [255, 255, 255, 255];
          b[p + 8] = c[0]; b[p + 9] = c[1]; b[p + 10] = c[2]; b[p + 11] = c[3];
          p += 12;
        }
        for (const i of idx) {
          if (wide) { dv.setUint32(p, i, true); p += 4; } else { dv.setUint16(p, i, true); p += 2; }
        }
        return b;
      },
      triangle(o) {
        o = o || {};
        return H.wire.mesh.body({
          clip: o.clip, texKey: o.texKey, wide: o.wide,
          verts: [
            { x: 0, y: 0, u: 0, v: 0 },
            { x: 32, y: 0, u: 1, v: 0 },
            { x: 0, y: 32, u: 0, v: 1 },
          ],
          indices: [0, 1, 2],
        });
      },
      // subtype 1: ppp f32, w f32, h f32, order (u64 hashes), bodies.
      frame(o) {
        const order = (o.order || []).map((h) => (typeof h === "bigint" ? h : BigInt(h)));
        const bodies = o.bodies || [];
        let n = 1 + 4 + 4 + 4 + 4 + order.length * 8 + 4;
        for (const b of bodies) n += 8 + 4 + b.bytes.length;
        const out = new Uint8Array(n);
        const dv = new DataView(out.buffer);
        out[0] = 1;
        dv.setFloat32(1, o.ppp === undefined ? 1 : o.ppp, true);
        dv.setFloat32(5, o.w === undefined ? 64 : o.w, true);
        dv.setFloat32(9, o.h === undefined ? 64 : o.h, true);
        let p = 13;
        dv.setUint32(p, order.length, true); p += 4;
        for (const h of order) { dv.setBigUint64(p, h, true); p += 8; }
        dv.setUint32(p, bodies.length, true); p += 4;
        for (const b of bodies) {
          dv.setBigUint64(p, typeof b.hash === "bigint" ? b.hash : BigInt(b.hash), true); p += 8;
          dv.setUint32(p, b.bytes.length, true); p += 4;
          out.set(b.bytes, p); p += b.bytes.length;
        }
        return framed(PREFIX.MESH, out);
      },
      // subtype 2: key u32, full_w, full_h, x, y, w, h, rgba.
      texture(o) {
        const w = o.w === undefined ? (o.fullW || 2) : o.w;
        const h = o.h === undefined ? (o.fullH || 2) : o.h;
        const rgba = o.rgba || new Uint8Array(w * h * 4).fill(255);
        const out = new Uint8Array(29 + rgba.length);
        const dv = new DataView(out.buffer);
        out[0] = 2;
        dv.setUint32(1, o.key, true);
        dv.setUint32(5, o.fullW === undefined ? w : o.fullW, true);
        dv.setUint32(9, o.fullH === undefined ? h : o.fullH, true);
        dv.setUint32(13, o.x || 0, true);
        dv.setUint32(17, o.y || 0, true);
        dv.setUint32(21, w, true);
        dv.setUint32(25, h, true);
        out.set(rgba, 29);
        return framed(PREFIX.MESH, out);
      },
      // subtype 3 (ADR-0242 Surfaces): count u32, count x live texture key u32.
      retire(keys) {
        keys = keys || [];
        const out = new Uint8Array(5 + keys.length * 4);
        const dv = new DataView(out.buffer);
        out[0] = 3;
        dv.setUint32(1, keys.length, true);
        keys.forEach((k, i) => dv.setUint32(5 + i * 4, k, true));
        return framed(PREFIX.MESH, out);
      },
    },
  };

  // ---- clock / scheduling -------------------------------------------------
  H.sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  H.microtask = () => Promise.resolve();
  H.tick = () => new Promise((r) => requestAnimationFrame(() => r()));
  H.ticks = async (n) => { for (let i = 0; i < (n || 1); i++) await H.tick(); };
  H.untilSocketOpen = async (timeoutMs) => {
    const limit = timeoutMs || 1000;
    for (let waited = 0; waited <= limit; waited += 5) {
      if (H.socket && H.socket.readyState === 1) return H.socket;
      await H.sleep(5);
    }
    throw new Error("no open harness socket after " + limit + " ms");
  };
  H.until = async (pred, label, timeoutMs) => {
    const limit = timeoutMs || 1000;
    for (let waited = 0; waited <= limit; waited += 5) {
      if (pred()) return true;
      await H.sleep(5);
    }
    throw new Error("timed out waiting for " + (label || "condition"));
  };

  // ---- captured traffic ---------------------------------------------------
  H.clear = () => { H.sentFrames.length = 0; };
  H.inputs = (kind) => H.sentFrames.filter((f) => f[0] === PREFIX.INPUT)
    .map(H.decode.input).filter((m) => !kind || m.kind === kind);
  H.sessions = (kind) => H.sentFrames.filter((f) => f[0] === PREFIX.SESSION)
    .map(H.decode.session).filter((m) => !kind || m.kind === kind);
  H.rawFrames = (prefix) => H.sentFrames.filter((f) => !prefix || f[0] === prefix);

  // ---- page handles / DOM helpers ----------------------------------------
  H.el = {
    get canvas() { return document.getElementById("screen"); },
    get takeBtn() { return document.getElementById("takeBtn"); },
    get pasteTrap() { return document.getElementById("pasteTrap"); },
    get status() { return document.getElementById("status"); },
    get statusMsg() { return document.getElementById("statusmsg"); },
    get role() { return document.getElementById("role"); },
    get roster() { return document.getElementById("roster"); },
    get cadence() { return document.getElementById("cadence"); },
  };

  // Index into H.gl.contexts of the context the page acquired for an element
  // (the viewer's own canvas by default); -1 when it has none yet.
  H.glContextIndex = (el) => {
    const want = el || H.el.canvas;
    return H.gl.contexts.findIndex((c) => c.real.canvas === want);
  };

  // Computed, laid-out visibility — not a style-attribute reading.
  H.visibility = (el) => {
    const cs = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    return {
      display: cs.display,
      visibility: cs.visibility,
      opacity: Number(cs.opacity),
      width: r.width,
      height: r.height,
      hitTestHit: (() => {
        if (r.width <= 0 || r.height <= 0) return false;
        const node = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return !!node && (node === el || el.contains(node));
      })(),
      onScreen: r.width > 0 && r.height > 0 && r.bottom > 0 && r.right > 0 &&
        r.top < window.innerHeight && r.left < window.innerWidth,
    };
  };

  // DOM MouseEvent.button -> buttons bitmask.
  const BUTTONS_MASK = { 0: 1, 1: 4, 2: 2, 3: 8, 4: 16 };
  H.DOM_BUTTONS = { LEFT: 0, MIDDLE: 1, RIGHT: 2, BACK: 3, FORWARD: 4 };

  H.pointer = (type, o) => {
    o = o || {};
    const target = o.target || H.el.canvas;
    const r = H.el.canvas.getBoundingClientRect();
    const fx = o.fx === undefined ? 0.5 : o.fx;
    const fy = o.fy === undefined ? 0.5 : o.fy;
    const button = o.button === undefined ? 0 : o.button;
    const down = type === "pointerdown";
    const init = {
      pointerId: o.pointerId === undefined ? 1 : o.pointerId,
      pointerType: o.pointerType || "mouse",
      isPrimary: o.isPrimary === undefined ? true : o.isPrimary,
      button: type === "pointermove" && o.button === undefined ? -1 : button,
      buttons: o.buttons === undefined ? (down ? (BUTTONS_MASK[button] || 0) : 0) : o.buttons,
      clientX: o.clientX === undefined ? r.left + fx * r.width : o.clientX,
      clientY: o.clientY === undefined ? r.top + fy * r.height : o.clientY,
      ctrlKey: !!o.ctrlKey, shiftKey: !!o.shiftKey, altKey: !!o.altKey, metaKey: !!o.metaKey,
      bubbles: true, cancelable: true, composed: true, view: window,
    };
    target.dispatchEvent(new PointerEvent(type, init));
    return { clientX: init.clientX, clientY: init.clientY, rect: r };
  };

  H.wheel = (o) => {
    o = o || {};
    const r = H.el.canvas.getBoundingClientRect();
    H.el.canvas.dispatchEvent(new WheelEvent("wheel", {
      deltaX: o.deltaX || 0, deltaY: o.deltaY || 0, deltaZ: 0, deltaMode: o.deltaMode || 0,
      clientX: r.left + r.width / 2, clientY: r.top + r.height / 2,
      ctrlKey: !!o.ctrlKey, shiftKey: !!o.shiftKey, altKey: !!o.altKey, metaKey: !!o.metaKey,
      bubbles: true, cancelable: true, composed: true, view: window,
    }));
  };

  H.key = (type, o) => {
    o = o || {};
    (o.target || H.el.canvas).dispatchEvent(new KeyboardEvent(type, {
      key: o.key, code: o.code === undefined ? "" : o.code, repeat: !!o.repeat,
      ctrlKey: !!o.ctrlKey, shiftKey: !!o.shiftKey, altKey: !!o.altKey, metaKey: !!o.metaKey,
      bubbles: true, cancelable: true, composed: true, view: window,
    }));
  };

  // The page's modifier bit layout (mods(): alt=1, ctrl=2|16, shift=4,
  // meta=8|16 — bit 16 is egui's "command").
  H.MOD = { ALT: 1, CTRL: 2, SHIFT: 4, META: 8, COMMAND: 16 };

  // Logical-point mapping the page documents for MouseMove/MouseButton:
  // client offset within the canvas box, scaled to hello.width/ppp.
  H.expectedLogical = (clientX, clientY, hello) => {
    const r = H.el.canvas.getBoundingClientRect();
    const lw = hello.width / hello.ppp, lh = hello.height / hello.ppp;
    return [(clientX - r.left) * lw / r.width, (clientY - r.top) * lh / r.height];
  };

  // ---- focus / visibility overrides (for the ADR-0242 SD2 surface) -------
  const realHasFocus = document.hasFocus.bind(document);
  H.setHasFocus = (v) => { document.hasFocus = () => !!v; };
  H.restoreHasFocus = () => { document.hasFocus = realHasFocus; };
  H.setHidden = (hidden) => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => !!hidden });
    Object.defineProperty(document, "visibilityState", {
      configurable: true, get: () => (hidden ? "hidden" : "visible"),
    });
    document.dispatchEvent(new Event("visibilitychange"));
  };
  H.windowBlur = () => window.dispatchEvent(new Event("blur"));
  H.windowFocus = () => window.dispatchEvent(new Event("focus"));

  // ---- assertions ---------------------------------------------------------
  const describe = (v) => {
    if (typeof v === "bigint") return v.toString() + "n";
    if (v instanceof Uint8Array) return "bytes[" + Array.from(v).join(",") + "]";
    if (typeof v === "number") return Number.isInteger(v) ? String(v) : v.toFixed(4);
    try { return JSON.stringify(v, (k, x) => (typeof x === "bigint" ? x.toString() + "n" : x)); }
    catch (_) { return String(v); }
  };
  const same = (a, b) => {
    if (typeof a === "bigint" || typeof b === "bigint") return String(a) === String(b);
    if (Array.isArray(a) && Array.isArray(b)) {
      return a.length === b.length && a.every((x, i) => same(x, b[i]));
    }
    if (a && b && typeof a === "object" && typeof b === "object") {
      const ka = Object.keys(b);
      return ka.every((k) => same(a[k], b[k]));
    }
    return a === b;
  };

  let failures = [], diags = [];
  H.fail = (msg) => { failures.push(String(msg)); return false; };
  H.note = (msg) => { diags.push(String(msg)); };
  H.ok = (cond, label) => (cond ? true : H.fail(label + " -- expected truthy, got " + describe(cond)));
  H.eq = (actual, expected, label) =>
    (same(actual, expected) ? true
      : H.fail(label + " -- expected " + describe(expected) + ", got " + describe(actual)));
  H.near = (actual, expected, eps, label) =>
    (Number.isFinite(actual) && Math.abs(actual - expected) <= eps ? true
      : H.fail(label + " -- expected " + describe(expected) + " +/-" + eps + ", got " + describe(actual)));
  H.notEq = (actual, unexpected, label) =>
    (same(actual, unexpected) ? H.fail(label + " -- expected anything but " + describe(unexpected)) : true);

  // ---- test registry / TAP runner ----------------------------------------
  H.tests = [];
  H.test = (name, fn) => H.tests.push({ name: name, fn: fn });
  H.perTestMs = 3000;

  const withTimeout = (p, ms, label) => new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error("case timed out after " + ms + " ms: " + label)), ms);
    Promise.resolve(p).then((v) => { clearTimeout(t); resolve(v); },
      (e) => { clearTimeout(t); reject(e); });
  });

  H.runAll = async () => {
    H.status = "running";
    H.emit("TAP version 13");
    H.emit("1.." + H.tests.length);
    let n = 0, failed = 0;
    for (const t of H.tests) {
      n++;
      failures = [];
      diags = [];
      try {
        await withTimeout(t.fn(), H.perTestMs, t.name);
      } catch (e) {
        failures.push("threw: " + ((e && (e.stack || e.message)) || e));
      }
      if (failures.length) {
        failed++;
        H.emit("not ok " + n + " - " + t.name);
      } else {
        H.emit("ok " + n + " - " + t.name);
      }
      for (const f of failures) H.emit("  # " + f);
      for (const d of diags) H.emit("  # note: " + d);
      H.flushResults();
    }
    H.emit("# cases: " + n + " passed: " + (n - failed) + " failed: " + failed);
    H.status = "completed";
    H.flushResults();
  };

  if (document.readyState === "complete") setTimeout(H.runAll, 0);
  else window.addEventListener("load", () => setTimeout(H.runAll, 0));
})();
