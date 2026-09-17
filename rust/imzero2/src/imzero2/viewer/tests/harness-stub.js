// Pre-page stub for the opt-in viewer browser regression harness (ADR-0242
// "Verification plan"). run.sh injects this file as a classic <script> placed
// immediately BEFORE the viewer page's own inline script, so index.html sees
// these objects at parse time and runs its real code paths against them.
//
// What is substituted, and why:
//
//   WebSocket      — no server. Frames the page sends are captured verbatim
//                    (byte arrays), and suites push server frames in.
//   VideoDecoder   — a headless browser's real decoder depends on the host's
//                    codecs; the stub makes decode progress deterministic and
//                    records configure()/decode() arguments.
//   requestAnimationFrame
//                  — the runner drives the page under Chromium's
//                    --virtual-time-budget, which advances timers but does NOT
//                    keep producing compositor frames, so a real rAF chain
//                    stalls after one callback. The shim re-expresses rAF as a
//                    16 ms timer: the page's own flushMove/rAF code still runs,
//                    on the clock the harness can advance.
//   setPointerCapture
//                  — dispatched pointer events are untrusted, so the browser
//                    has no active pointer to capture; the real call is tried
//                    and its NotFoundError absorbed (see below).
//   getContext     — WebGL2 contexts are handed out wrapped in a counting
//                    proxy (real GL underneath: headless Chromium has WebGL2),
//                    so GPU-object accounting is observable. Disable with
//                    ?harness_gl=0.
//
// Nothing else about the page is replaced: its CSS, layout, listeners, codec
// and protobuf code are the shipped ones.
"use strict";

(function () {
  const params = new URLSearchParams(location.search);
  const numParam = (name, dflt) => {
    const v = Number(params.get(name));
    return Number.isFinite(v) && v > 0 ? v : dflt;
  };

  const H = {
    // --- environment / bookkeeping ---
    suite: params.get("harness_suite") || "unnamed",
    watchdogMs: numParam("harness_watchdog", 9000),
    errors: [],
    // Every frame the page handed to WebSocket.send(), in order, as copies.
    sentFrames: [],
    sockets: [],
    decoders: [],
    decoderProbes: [],
    // Set false to stop the VideoDecoder stub from emitting a frame per decode.
    autoDecodeOutput: true,
    lines: [],
    status: "not-started",
  };
  window.__imzero2Harness = H;

  Object.defineProperty(H, "socket", {
    get() { return H.sockets.length ? H.sockets[H.sockets.length - 1] : null; },
  });

  // ---- results sink -------------------------------------------------------
  // Chromium's --dump-dom is the transport: the report is appended to <head>
  // (never rendered, so page layout and viewport measurements are untouched)
  // and serialized with the DOM. Callers may flush repeatedly; the last flush
  // wins, which is what makes the watchdog able to report a hung suite.
  H.sanitize = (s) =>
    String(s).replace(/&/g, "(amp)").replace(/</g, "(lt)").replace(/>/g, "(gt)").replace(/\r?\n/g, " ");
  H.emit = (line) => { H.lines.push(H.sanitize(line)); };
  H.flushResults = () => {
    let pre = document.getElementById("harness-results");
    if (!pre) {
      pre = document.createElement("pre");
      pre.id = "harness-results";
      (document.head || document.documentElement).appendChild(pre);
    }
    const head = [
      "# suite: " + H.sanitize(H.suite),
      "# user-agent: " + H.sanitize(navigator.userAgent),
      "# secure-context: " + window.isSecureContext,
      "# webgl2: " + H.gl.available,
      "# gl-counters: " + H.gl.enabled,
      "# raf-shim: true",
    ];
    const errs = H.errors.map((e) => "# page-error: " + H.sanitize(e));
    pre.textContent = head.concat(errs, H.lines, ["# status: " + H.status]).join("\n");
  };

  window.addEventListener("error", (e) => {
    H.errors.push((e.message || "error") + " @ " + (e.filename || "?") + ":" + (e.lineno || 0));
    H.flushResults();
  });
  window.addEventListener("unhandledrejection", (e) => {
    const r = e.reason;
    H.errors.push("unhandled rejection: " + ((r && (r.stack || r.message)) || r));
    H.flushResults();
  });
  setTimeout(() => {
    if (H.status !== "completed") {
      H.status = "watchdog-timeout";
      H.flushResults();
    }
  }, H.watchdogMs);

  // ---- pointer capture ----------------------------------------------------
  // A dispatched PointerEvent is untrusted, so the browser holds no "active
  // pointer" for its pointerId and setPointerCapture throws NotFoundError. In
  // the page that throw lands inside the pointerdown listener and swallows the
  // rest of it (the button frame is never sent), which would be a harness
  // artifact masquerading as a viewer defect. Attempt the real call, count it,
  // and absorb only that one error; anything else still propagates.
  H.pointerCapture = { requested: 0, released: 0, absorbed: 0 };
  const realSet = Element.prototype.setPointerCapture;
  const realRelease = Element.prototype.releasePointerCapture;
  Element.prototype.setPointerCapture = function (id) {
    H.pointerCapture.requested++;
    try {
      return realSet.call(this, id);
    } catch (e) {
      if (e && e.name === "NotFoundError") { H.pointerCapture.absorbed++; return undefined; }
      throw e;
    }
  };
  Element.prototype.releasePointerCapture = function (id) {
    H.pointerCapture.released++;
    try {
      return realRelease.call(this, id);
    } catch (e) {
      if (e && e.name === "NotFoundError") { H.pointerCapture.absorbed++; return undefined; }
      throw e;
    }
  };

  // ---- requestAnimationFrame shim ----------------------------------------
  const rafTimers = new Map();
  let rafSeq = 1;
  window.requestAnimationFrame = function (cb) {
    const id = rafSeq++;
    rafTimers.set(id, setTimeout(() => { rafTimers.delete(id); cb(performance.now()); }, 16));
    return id;
  };
  window.cancelAnimationFrame = function (id) {
    const t = rafTimers.get(id);
    if (t !== undefined) { clearTimeout(t); rafTimers.delete(id); }
  };

  // ---- WebSocket stub -----------------------------------------------------
  const CONNECTING = 0, OPEN = 1, CLOSING = 2, CLOSED = 3;
  class HarnessWebSocket {
    constructor(url, protocols) {
      this.url = String(url);
      this.protocols = protocols;
      this.readyState = CONNECTING;
      this.binaryType = "blob";
      this.bufferedAmount = 0;
      this.extensions = "";
      this.protocol = "";
      this.onopen = this.onclose = this.onerror = this.onmessage = null;
      this.sent = [];
      this.listeners = new Map();
      this.closeInfo = null;
      H.sockets.push(this);
      // Open on a macrotask: the page assigns its handlers synchronously after
      // `new WebSocket(...)`, exactly as against a real socket.
      setTimeout(() => this.harnessOpen(), 0);
    }
    addEventListener(type, fn) {
      if (!this.listeners.has(type)) this.listeners.set(type, []);
      this.listeners.get(type).push(fn);
    }
    removeEventListener(type, fn) {
      const l = this.listeners.get(type);
      if (l) this.listeners.set(type, l.filter((f) => f !== fn));
    }
    dispatch(type, ev) {
      ev.type = type;
      ev.target = ev.currentTarget = this;
      const on = this["on" + type];
      if (typeof on === "function") on.call(this, ev);
      for (const fn of this.listeners.get(type) || []) fn.call(this, ev);
    }
    send(data) {
      if (this.readyState !== OPEN) {
        throw new DOMException("send on a socket that is not OPEN", "InvalidStateError");
      }
      let u8;
      if (data instanceof ArrayBuffer) u8 = new Uint8Array(data.slice(0));
      else if (ArrayBuffer.isView(data)) u8 = new Uint8Array(data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength));
      else u8 = new TextEncoder().encode(String(data));
      this.sent.push(u8);
      H.sentFrames.push(u8);
    }
    close(code, reason) {
      if (this.readyState === CLOSING || this.readyState === CLOSED) return;
      this.readyState = CLOSING;
      setTimeout(() => this.harnessClosed({ code: code || 1000, reason: reason || "", wasClean: true, by: "page" }), 0);
    }
    // ---- harness-side controls (a server, and the network, live here) ----
    harnessOpen() {
      if (this.readyState !== CONNECTING) return;
      this.readyState = OPEN;
      this.dispatch("open", {});
    }
    // Deliver one framed server->client message (Uint8Array).
    serverMessage(bytes) {
      if (this.readyState !== OPEN) throw new Error("serverMessage on a socket that is not OPEN");
      const ab = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
      this.dispatch("message", { data: this.binaryType === "arraybuffer" ? ab : new Blob([ab]) });
    }
    // Drop the connection from the far end (no unload packet, ADR-0242 SD2).
    serverClose(code) {
      if (this.readyState === CLOSED) return;
      this.readyState = CLOSING;
      this.harnessClosed({ code: code || 1006, reason: "", wasClean: false, by: "server" });
    }
    harnessError() { this.dispatch("error", {}); }
    harnessClosed(info) {
      if (this.readyState === CLOSED) return;
      this.readyState = CLOSED;
      this.closeInfo = info;
      this.dispatch("close", info);
    }
  }
  HarnessWebSocket.CONNECTING = CONNECTING;
  HarnessWebSocket.OPEN = OPEN;
  HarnessWebSocket.CLOSING = CLOSING;
  HarnessWebSocket.CLOSED = CLOSED;
  HarnessWebSocket.prototype.CONNECTING = CONNECTING;
  HarnessWebSocket.prototype.OPEN = OPEN;
  HarnessWebSocket.prototype.CLOSING = CLOSING;
  HarnessWebSocket.prototype.CLOSED = CLOSED;
  window.WebSocket = HarnessWebSocket;

  // ---- VideoDecoder stub --------------------------------------------------
  // The output frame is a real <canvas>, so the page's ctx2d.drawImage(frame)
  // is the shipped call on a drawable source; close() is the VideoFrame part
  // of the contract the page relies on.
  class HarnessVideoDecoder {
    static isConfigSupported(config) {
      H.decoderProbes.push(config && config.codec);
      return Promise.resolve({ supported: true, config: config });
    }
    constructor(init) {
      this.outputCb = init.output;
      this.errorCb = init.error;
      this.state = "unconfigured";
      this.configs = [];
      this.chunks = [];
      this.decodeQueueSize = 0;
      H.decoders.push(this);
    }
    configure(config) { this.configs.push(config); this.state = "configured"; }
    decode(chunk) {
      this.chunks.push({ type: chunk.type, timestamp: chunk.timestamp, byteLength: chunk.byteLength });
      if (H.autoDecodeOutput) this.harnessEmitFrame();
    }
    harnessEmitFrame(w, h) {
      const f = document.createElement("canvas");
      f.width = w || 16;
      f.height = h || 16;
      f.codedWidth = f.width;
      f.codedHeight = f.height;
      f.close = () => {};
      this.outputCb(f);
    }
    harnessFail(message) { this.errorCb(new Error(message || "harness decoder failure")); }
    flush() { return Promise.resolve(); }
    reset() { this.chunks.length = 0; }
    close() { this.state = "closed"; }
  }
  window.VideoDecoder = HarnessVideoDecoder;

  // ---- WebGL object accounting -------------------------------------------
  // Real GL, counted. Alloc/free are tracked by identity so a double free or a
  // leak is visible, which is what the mesh-lane retention regressions need.
  const realGetContext = HTMLCanvasElement.prototype.getContext;
  H.gl = {
    enabled: params.get("harness_gl") !== "0",
    available: false,
    calls: Object.create(null),
    liveBuffers: new Set(),
    liveTextures: new Set(),
    doubleFrees: 0,
    contexts: [],
    reset() {
      H.gl.calls = Object.create(null);
      H.gl.doubleFrees = 0;
    },
    counts() {
      return {
        buffers: H.gl.liveBuffers.size,
        textures: H.gl.liveTextures.size,
        createBuffer: H.gl.calls.createBuffer || 0,
        deleteBuffer: H.gl.calls.deleteBuffer || 0,
        createTexture: H.gl.calls.createTexture || 0,
        deleteTexture: H.gl.calls.deleteTexture || 0,
        doubleFrees: H.gl.doubleFrees,
      };
    },
    // Real context loss/restore, via the browser's own extension.
    loseContext(i) {
      const c = H.gl.contexts[i || 0];
      if (!c) throw new Error("no wrapped GL context " + (i || 0));
      const ext = c.real.getExtension("WEBGL_lose_context");
      if (!ext) throw new Error("WEBGL_lose_context unavailable");
      ext.loseContext();
    },
    restoreContext(i) {
      const c = H.gl.contexts[i || 0];
      if (!c) throw new Error("no wrapped GL context " + (i || 0));
      const ext = c.real.getExtension("WEBGL_lose_context");
      if (!ext) throw new Error("WEBGL_lose_context unavailable");
      ext.restoreContext();
    },
  };

  function wrapGl(real) {
    const wrapper = new Proxy(real, {
      get(target, prop) {
        const v = target[prop];
        if (typeof v !== "function") return v;
        return function (...args) {
          H.gl.calls[prop] = (H.gl.calls[prop] || 0) + 1;
          const r = v.apply(target, args);
          if (prop === "createBuffer" && r) H.gl.liveBuffers.add(r);
          else if (prop === "createTexture" && r) H.gl.liveTextures.add(r);
          else if (prop === "deleteBuffer" && args[0]) {
            if (!H.gl.liveBuffers.delete(args[0])) H.gl.doubleFrees++;
          } else if (prop === "deleteTexture" && args[0]) {
            if (!H.gl.liveTextures.delete(args[0])) H.gl.doubleFrees++;
          }
          return r;
        };
      },
      set(target, prop, value) { target[prop] = value; return true; },
    });
    H.gl.contexts.push({ real: real, wrapper: wrapper });
    return wrapper;
  }

  HTMLCanvasElement.prototype.getContext = function (kind, attrs) {
    const ctx = realGetContext.call(this, kind, attrs);
    if (ctx && (kind === "webgl2" || kind === "webgl")) {
      H.gl.available = true;
      if (H.gl.enabled) return wrapGl(ctx);
    }
    return ctx;
  };
})();
