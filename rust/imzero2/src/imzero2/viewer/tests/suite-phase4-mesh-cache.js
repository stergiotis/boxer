// Phase 4 (resource lifetime) regressions for the shipped viewer page, per
// ADR-0242 SD5: the browser retains the last accepted frame's draw-order
// bodies and the explicitly supplied live texture keys, prunes only when told
// to (no independent browser LRU), resends a body that disappeared and came
// back, and disposes its GPU caches on disconnect.
//
// The accounting is real GL: the harness hands the page a WebGL2 context
// wrapped in a counting proxy, so createBuffer/deleteBuffer/createTexture/
// deleteTexture are tracked by object identity — a leak, a double free and a
// premature delete are all visible. A machine without headless WebGL2 cannot
// run these cases; they report that as a failure to run, not as a pass.
"use strict";

(function () {
  const H = window.__imzero2Harness;
  const MESH_HELLO = { width: 256, height: 128, ppp: 1, cadence: 0, codec: "mesh" };
  const TEX_A = 11, TEX_B = 12;
  const BODY_A = 0xa1a1a1a1a1a1a1a1n, BODY_B = 0xb2b2b2b2b2b2b2b2n;
  const BUFFERS_PER_BODY = 3; // vertex + colour + element

  const bodyA = () => ({ hash: BODY_A, bytes: H.wire.mesh.triangle({ texKey: TEX_A, clip: [0, 0, 128, 128] }) });
  const bodyB = () => ({ hash: BODY_B, bytes: H.wire.mesh.triangle({ texKey: TEX_B, clip: [0, 0, 128, 128] }) });

  let started = false;
  async function meshSession() {
    if (started) return;
    await H.untilSocketOpen();
    H.socket.serverMessage(H.wire.hello(MESH_HELLO));
    await H.ticks(2);
    started = true;
  }

  H.test("phase4/mesh-mode-acquires-a-webgl2-context", async () => {
    await meshSession();
    if (!H.eq(H.gl.available, true, "headless WebGL2 is available (else this suite cannot run)")) return;
    H.eq(H.gl.enabled, true, "GL object accounting is installed");
    H.ok(H.glContextIndex() >= 0, "the viewer canvas got a wrapped WebGL2 context");
    H.notEq(H.el.statusMsg.textContent.indexOf("WebGL2 is unavailable"), 0,
      "the page did not fall into its no-WebGL2 branch");
  });

  H.test("phase4/bodies-and-textures-are-cached-once-per-frame", async () => {
    await meshSession();
    const sock = H.socket;
    sock.serverMessage(H.wire.mesh.texture({ key: TEX_A, fullW: 2, fullH: 2 }));
    sock.serverMessage(H.wire.mesh.texture({ key: TEX_B, fullW: 2, fullH: 2 }));
    H.clear();
    sock.serverMessage(H.wire.mesh.frame({
      ppp: 1, w: MESH_HELLO.width, h: MESH_HELLO.height,
      order: [BODY_A, BODY_B], bodies: [bodyA(), bodyB()],
    }));
    await H.ticks(2);
    const c = H.gl.counts();
    H.note("after frame 1: " + JSON.stringify(c));
    H.eq(c.buffers, 2 * BUFFERS_PER_BODY, "three GL buffers per parsed body, two bodies");
    H.eq(c.textures, 2, "one GL texture per texture key");
    H.eq(c.deleteBuffer, 0, "nothing freed while both bodies are live");
    H.eq(c.doubleFrees, 0, "no double free");
    H.ok(H.sessions("ping").length >= 1, "the painted mesh frame was attested with a Ping");
    H.eq(H.el.canvas.width, MESH_HELLO.width, "the frame's own geometry sized the backing store");
    H.eq(H.el.canvas.height, MESH_HELLO.height, "the frame's own geometry sized the backing store");
    // The status line is rewritten at most once a second, so it is reported
    // rather than asserted on.
    H.note("status: " + H.el.statusMsg.textContent);
  });

  // No independent browser LRU (ADR-0242 SD5 alternatives): a frame that simply
  // stops drawing a body does not by itself retire it.
  H.test("phase4/a-frame-alone-retires-nothing", async () => {
    const before = H.gl.counts();
    H.socket.serverMessage(H.wire.mesh.frame({
      ppp: 1, w: MESH_HELLO.width, h: MESH_HELLO.height, order: [BODY_A], bodies: [],
    }));
    await H.ticks(2);
    const after = H.gl.counts();
    H.note("after the A-only frame: " + JSON.stringify(after));
    H.eq(after.buffers, before.buffers, "buffer set unchanged without a retirement message");
    H.eq(after.textures, before.textures, "texture set unchanged without a retirement message");
  });

  // The additive retirement message (mesh subtype 3): keep the preceding
  // frame's draw order and exactly the supplied live texture keys.
  H.test("phase4/retirement-prunes-to-the-last-frame-and-live-keys", async () => {
    H.socket.serverMessage(H.wire.mesh.retire([TEX_A]));
    await H.ticks(2);
    const c = H.gl.counts();
    H.note("after retirement: " + JSON.stringify(c));
    H.eq(c.buffers, BUFFERS_PER_BODY, "only the retained body's buffers survive");
    H.eq(c.textures, 1, "only the live texture key survives");
    H.eq(c.deleteBuffer, BUFFERS_PER_BODY, "the dropped body's buffers were deleted");
    H.eq(c.deleteTexture, 1, "the dead texture was deleted");
    H.eq(c.doubleFrees, 0, "no double free");
  });

  H.test("phase4/a-returning-body-is-accepted-again", async () => {
    const sock = H.socket;
    sock.serverMessage(H.wire.mesh.texture({ key: TEX_B, fullW: 2, fullH: 2 }));
    H.clear();
    sock.serverMessage(H.wire.mesh.frame({
      ppp: 1, w: MESH_HELLO.width, h: MESH_HELLO.height,
      order: [BODY_A, BODY_B], bodies: [bodyB()],
    }));
    await H.ticks(2);
    const c = H.gl.counts();
    H.note("after the resend: " + JSON.stringify(c));
    H.eq(c.buffers, 2 * BUFFERS_PER_BODY, "the resent body is cached again");
    H.eq(c.textures, 2, "its texture is live again");
    H.eq(c.doubleFrees, 0, "no double free");
  });

  // A texture that is live but drawn by nothing this frame stays (SD5), while a
  // retirement that only changes the live set is still work.
  H.test("phase4/unused-but-live-textures-are-retained", async () => {
    H.socket.serverMessage(H.wire.mesh.frame({
      ppp: 1, w: MESH_HELLO.width, h: MESH_HELLO.height, order: [BODY_A, BODY_B], bodies: [],
    }));
    H.socket.serverMessage(H.wire.mesh.retire([TEX_A, TEX_B, 99]));
    await H.ticks(2);
    let c = H.gl.counts();
    H.eq(c.textures, 2, "both live keys retained (an unknown live key is not an error)");
    H.eq(c.buffers, 2 * BUFFERS_PER_BODY, "both drawn bodies retained");
    H.socket.serverMessage(H.wire.mesh.retire([TEX_A]));
    await H.ticks(2);
    c = H.gl.counts();
    H.note("after the retirement-only change: " + JSON.stringify(c));
    H.eq(c.textures, 1, "a retirement-only change is applied, not deduplicated away");
    H.eq(c.doubleFrees, 0, "no double free");
  });

  H.test("phase4/disconnect-disposes-gpu-caches", async () => {
    const before = H.gl.counts();
    H.ok(before.buffers > 0 || before.textures > 0, "there was something cached to dispose");
    const socketsBefore = H.sockets.length;
    H.socket.serverClose(1006);
    await H.ticks(3);
    const c = H.gl.counts();
    H.note("after disconnect: " + JSON.stringify(c));
    H.eq(c.buffers, 0, "no GL buffer survives the disconnect");
    H.eq(c.textures, 0, "no GL texture survives the disconnect");
    H.eq(c.doubleFrees, 0, "no double free");
    await H.until(() => H.sockets.length > socketsBefore, "a reconnect attempt", 4000);
  });

  // A reconnected page must bootstrap from scratch: the server resends bodies
  // and textures, and they are accepted (nothing is assumed still cached).
  H.test("phase4/reconnect-bootstraps-the-cache-again", async () => {
    const sock = await H.untilSocketOpen(3000);
    sock.serverMessage(H.wire.hello(MESH_HELLO));
    await H.ticks(2);
    sock.serverMessage(H.wire.mesh.texture({ key: TEX_A, fullW: 2, fullH: 2 }));
    sock.serverMessage(H.wire.mesh.frame({
      ppp: 1, w: MESH_HELLO.width, h: MESH_HELLO.height, order: [BODY_A], bodies: [bodyA()],
    }));
    await H.ticks(2);
    const c = H.gl.counts();
    H.note("after reconnect bootstrap: " + JSON.stringify(c));
    H.eq(c.buffers, BUFFERS_PER_BODY, "the bootstrapped body is cached on the new connection");
    H.eq(c.textures, 1, "the bootstrapped texture is cached on the new connection");
    H.eq(c.doubleFrees, 0, "no double free");
  });

  // Context loss is the other way the GPU-side cache can vanish under the page.
  H.test("phase4/context-loss-drops-the-cache-without-faulting", async () => {
    const idx = H.glContextIndex();
    if (!H.ok(idx >= 0, "the viewer canvas has a wrapped context to lose")) return;
    let ext = true;
    try {
      H.gl.loseContext(idx);
    } catch (e) {
      ext = false;
      H.note("WEBGL_lose_context unavailable: " + e.message);
    }
    if (!H.ok(ext, "the browser exposes WEBGL_lose_context (else this case cannot run)")) return;
    await H.ticks(4);
    const c = H.gl.counts();
    H.note("after context loss: " + JSON.stringify(c));
    H.eq(c.doubleFrees, 0, "no double free while the context is lost");
    // Frames that arrive while the context is lost must be ignored rather than
    // painted into a dead context.
    const errorsBefore = H.errors.length;
    if (H.socket && H.socket.readyState === 1) {
      H.socket.serverMessage(H.wire.mesh.frame({
        ppp: 1, w: MESH_HELLO.width, h: MESH_HELLO.height, order: [BODY_A], bodies: [bodyA()],
      }));
      await H.ticks(2);
    }
    H.eq(H.errors.length, errorsBefore, "a mesh frame arriving on a lost context raises no page error");
  });
})();
