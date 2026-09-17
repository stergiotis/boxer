// Phase 1 (interaction) regressions for the shipped viewer page, per the
// repair plan and ADR-0242 SD2: the passive takeover control must actually be
// visible and actually request the session, and the browser's pointer adapter
// must translate DOM button numbering onto the frozen wire numbering
// (0=primary, 1=secondary, 2=middle, 3=extra1, 4=extra2 — input.proto,
// inputmap.rs::pointer_button).
//
// Every case drives the real listeners the page installed and decodes the
// bytes the page handed to WebSocket.send().
"use strict";

(function () {
  const H = window.__imzero2Harness;
  const HELLO = { width: 800, height: 600, ppp: 1, cadence: 0, codec: "" };
  const ME = 7, OTHER = 9;

  let sessionReady = false;
  async function session() {
    if (sessionReady) return;
    await H.untilSocketOpen();
    H.socket.serverMessage(H.wire.hello(HELLO));
    // setupInput() runs on the first hello; give the page a frame to install
    // listeners and start its own rAF flush loop.
    await H.ticks(2);
    sessionReady = true;
  }

  async function setRole(active) {
    await session();
    H.socket.serverMessage(H.wire.roster({
      youId: ME,
      youRole: active ? 1 : 0,
      activeId: active ? ME : OTHER,
      max: 4,
      connections: [
        { id: ME, role: active ? 1 : 0, label: "harness" },
        { id: OTHER, role: active ? 0 : 1, label: "other" },
      ],
    }));
    // A role change can start asynchronous page work (a capability re-probe on
    // promotion), whose frame would otherwise land inside the next case's
    // capture window. Let it settle before the case clears the capture.
    await H.ticks(2);
    await H.sleep(30);
  }

  // Bring the page up and confirm the stubbed transport is really carrying the
  // page's own announcement, so a later empty capture means "page sent
  // nothing", not "harness dropped it".
  H.test("phase1/client-hello-announces-decode-capability", async () => {
    await H.untilSocketOpen();
    await H.ticks(1);
    const hellos = H.sessions("client_hello");
    H.eq(hellos.length, 1, "exactly one client_hello");
    if (hellos.length) {
      H.eq(hellos[0].webcodecs, true, "client_hello.webcodecs");
      H.eq(hellos[0].oneofCount, 1, "client_hello carries a single oneof field");
    }
    await session();
  });

  H.test("phase1/takeover-button-hidden-while-active", async () => {
    await setRole(true);
    const v = H.visibility(H.el.takeBtn);
    H.eq(v.display, "none", "computed display of #takeBtn while active");
    H.eq(v.width === 0 && v.height === 0, true, "#takeBtn has no layout box while active");
  });

  // The defect: renderRoster reset the inline display to "" while the page's
  // own stylesheet says `#takeBtn { display: none }`, so the control stayed
  // invisible for the only role that needs it.
  H.test("phase1/takeover-button-visible-while-passive", async () => {
    await setRole(false);
    const v = H.visibility(H.el.takeBtn);
    H.notEq(v.display, "none", "computed display of #takeBtn while passive");
    H.eq(v.visibility, "visible", "computed visibility of #takeBtn while passive");
    H.ok(v.opacity > 0, "computed opacity of #takeBtn while passive");
    H.ok(v.width > 0 && v.height > 0, "#takeBtn has a layout box while passive (" +
      Math.round(v.width) + "x" + Math.round(v.height) + ")");
    H.eq(v.onScreen, true, "#takeBtn box lies inside the viewport");
    H.eq(v.hitTestHit, true, "#takeBtn is the hit-test target at its own centre");
    H.note("display=" + v.display + " box=" + Math.round(v.width) + "x" + Math.round(v.height));
  });

  H.test("phase1/takeover-click-emits-take-session", async () => {
    await setRole(false);
    H.clear();
    H.el.takeBtn.click();
    await H.ticks(1);
    const ctl = H.sessions();
    H.note("session frames after the click: " + (ctl.map((c) => c.kind).join(",") || "none"));
    H.eq(H.inputs().length, 0, "no input frame produced by the takeover click");
    const take = ctl.filter((c) => c.kind === "take_session");
    H.eq(take.length, 1, "exactly one TakeSession request");
    if (take.length) {
      H.eq(take[0].field, 8, "SessionControl oneof field number");
      H.eq(take[0].bodyLength, 0, "TakeSession body is empty");
      H.eq(take[0].oneofCount, 1, "single oneof field set");
      H.eq(take[0].frameBytes, 3, "TakeSession frame is prefix + tag + length");
    }
    const takeRaw = H.rawFrames().filter((f) => f[0] === H.PREFIX.SESSION && f.length === 3);
    if (takeRaw.length) H.eq(Array.from(takeRaw[0]), [H.PREFIX.SESSION, 0x42, 0x00], "TakeSession frame bytes");
  });

  // A visible takeover control must not come with input leaking from a passive
  // viewer (ADR-0086 SD3, still gated in sendFramed).
  H.test("phase1/passive-viewer-emits-no-input", async () => {
    await setRole(false);
    H.clear();
    H.pointer("pointerdown", { button: 0, pointerId: 31 });
    H.pointer("pointerup", { button: 0, pointerId: 31 });
    H.pointer("pointermove", { fx: 0.3, fy: 0.4, pointerId: 31 });
    H.key("keydown", { key: "a", code: "KeyA" });
    await H.ticks(2);
    H.eq(H.inputs().length, 0, "input frames emitted while passive");
    H.note("frames while passive: " + H.rawFrames().length);
  });

  // The defect: the adapter forwarded DOM MouseEvent.button unchanged, so a
  // middle click (DOM 1) arrived as egui Secondary and a right click (DOM 2)
  // as Middle. Wire numbering is frozen; the browser adapter is what moves.
  H.test("phase1/dom-mouse-buttons-map-to-wire-numbers", async () => {
    await setRole(true);
    const cases = [
      { dom: H.DOM_BUTTONS.LEFT, wire: 0, name: "left/primary" },
      { dom: H.DOM_BUTTONS.MIDDLE, wire: 2, name: "middle" },
      { dom: H.DOM_BUTTONS.RIGHT, wire: 1, name: "right/secondary" },
      { dom: H.DOM_BUTTONS.BACK, wire: 3, name: "back/extra1" },
      { dom: H.DOM_BUTTONS.FORWARD, wire: 4, name: "forward/extra2" },
    ];
    for (const c of cases) {
      H.clear();
      const at = H.pointer("pointerdown", { button: c.dom, pointerId: 40 + c.dom });
      H.pointer("pointerup", { button: c.dom, pointerId: 40 + c.dom });
      await H.ticks(1);
      const btn = H.inputs("mouse_button");
      if (!H.eq(btn.length, 2, "press+release frames for DOM button " + c.dom + " (" + c.name + ")")) continue;
      H.eq(btn[0].button, c.wire, "wire button for DOM " + c.dom + " press (" + c.name + ")");
      H.eq(btn[1].button, c.wire, "wire button for DOM " + c.dom + " release (" + c.name + ")");
      H.eq(btn[0].pressed, true, "press flag for DOM " + c.dom);
      H.eq(btn[1].pressed, false, "release flag for DOM " + c.dom);
      const want = H.expectedLogical(at.clientX, at.clientY, HELLO);
      H.near(btn[0].x, want[0], 0.05, "logical x for DOM " + c.dom);
      H.near(btn[0].y, want[1], 0.05, "logical y for DOM " + c.dom);
    }
  });

  H.test("phase1/mouse-button-modifier-bits", async () => {
    await setRole(true);
    const M = H.MOD;
    const cases = [
      { opts: {}, bits: 0, name: "none" },
      { opts: { altKey: true }, bits: M.ALT, name: "alt" },
      { opts: { ctrlKey: true }, bits: M.CTRL | M.COMMAND, name: "ctrl(+command)" },
      { opts: { shiftKey: true }, bits: M.SHIFT, name: "shift" },
      { opts: { metaKey: true }, bits: M.META | M.COMMAND, name: "meta(+command)" },
      { opts: { ctrlKey: true, shiftKey: true }, bits: M.CTRL | M.COMMAND | M.SHIFT, name: "ctrl+shift" },
    ];
    for (const c of cases) {
      H.clear();
      H.pointer("pointerdown", Object.assign({ button: 0, pointerId: 60 }, c.opts));
      await H.ticks(1);
      const btn = H.inputs("mouse_button");
      if (!H.eq(btn.length, 1, "one press frame for modifiers " + c.name)) continue;
      H.eq(btn[0].modifiers, c.bits, "modifier bits for " + c.name);
      H.pointer("pointerup", Object.assign({ button: 0, pointerId: 60 }, c.opts));
      await H.ticks(1);
    }
  });

  H.test("phase1/wheel-sign-and-unit-mapping", async () => {
    await setRole(true);
    const cases = [
      { deltaX: 3, deltaY: -4, deltaMode: 0, unit: 0, name: "pixel" },
      { deltaX: 0, deltaY: 2, deltaMode: 1, unit: 1, name: "line" },
      { deltaX: -1, deltaY: 1, deltaMode: 2, unit: 2, name: "page" },
    ];
    for (const c of cases) {
      H.clear();
      H.wheel(c);
      await H.ticks(1);
      const w = H.inputs("mouse_wheel");
      if (!H.eq(w.length, 1, "one wheel frame for deltaMode " + c.deltaMode + " (" + c.name + ")")) continue;
      H.near(w[0].dx, -c.deltaX, 0.001, "wheel dx is the negated DOM delta (" + c.name + ")");
      H.near(w[0].dy, -c.deltaY, 0.001, "wheel dy is the negated DOM delta (" + c.name + ")");
      H.eq(w[0].unit, c.unit, "wheel unit for deltaMode " + c.deltaMode);
    }
  });

  // pointermove is coalesced by the page's own rAF flush; the frame carries the
  // latest position in logical points.
  H.test("phase1/pointer-moves-coalesce-to-one-frame", async () => {
    await setRole(true);
    H.clear();
    H.pointer("pointermove", { fx: 0.2, fy: 0.2, pointerId: 70 });
    H.pointer("pointermove", { fx: 0.4, fy: 0.4, pointerId: 70 });
    const last = H.pointer("pointermove", { fx: 0.75, fy: 0.6, pointerId: 70 });
    await H.ticks(2);
    const moves = H.inputs("mouse_move");
    H.eq(moves.length, 1, "three DOM moves flush as one MouseMove");
    if (moves.length) {
      const want = H.expectedLogical(last.clientX, last.clientY, HELLO);
      H.near(moves[0].x, want[0], 0.05, "flushed MouseMove x is the latest position");
      H.near(moves[0].y, want[1], 0.05, "flushed MouseMove y is the latest position");
      H.ok(moves[0].x >= 0 && moves[0].x <= HELLO.width / HELLO.ppp, "x inside the logical surface");
      H.ok(moves[0].y >= 0 && moves[0].y <= HELLO.height / HELLO.ppp, "y inside the logical surface");
    }
  });

  H.test("phase1/pointer-leave-sends-pointer-gone", async () => {
    await setRole(true);
    H.clear();
    H.pointer("pointerleave", { pointerId: 71 });
    await H.ticks(1);
    H.eq(H.inputs("pointer_gone").length, 1, "one PointerGone on mouse pointerleave");
  });

  H.test("phase1/key-emits-key-and-text-but-not-on-shortcuts", async () => {
    await setRole(true);
    H.clear();
    H.key("keydown", { key: "a", code: "KeyA" });
    await H.ticks(1);
    let keys = H.inputs("key"), texts = H.inputs("text");
    H.eq(keys.length, 1, "one KeyEvent for a plain keydown");
    H.eq(texts.length, 1, "one TextInput for a printable keydown");
    if (keys.length) {
      H.eq(keys[0].key, "a", "KeyEvent.key");
      H.eq(keys[0].code, "KeyA", "KeyEvent.code");
      H.eq(keys[0].pressed, true, "KeyEvent.pressed on keydown");
      H.eq(keys[0].repeat, false, "KeyEvent.repeat");
      H.eq(keys[0].modifiers, 0, "KeyEvent.modifiers");
    }
    if (texts.length) H.eq(texts[0].text, "a", "TextInput.text");

    H.clear();
    H.key("keyup", { key: "a", code: "KeyA" });
    await H.ticks(1);
    keys = H.inputs("key");
    H.eq(keys.length, 1, "one KeyEvent for keyup");
    if (keys.length) H.eq(keys[0].pressed, false, "KeyEvent.pressed on keyup");
    H.eq(H.inputs("text").length, 0, "no TextInput on keyup");

    // Ctrl+C is a shortcut, not typed text: the key travels, the character
    // must not (the host turns it into an egui clipboard action).
    H.clear();
    H.key("keydown", { key: "c", code: "KeyC", ctrlKey: true });
    await H.ticks(1);
    keys = H.inputs("key");
    H.eq(keys.length, 1, "one KeyEvent for ctrl+c");
    if (keys.length) H.eq(keys[0].modifiers, H.MOD.CTRL | H.MOD.COMMAND, "ctrl+c modifier bits");
    H.eq(H.inputs("text").length, 0, "no TextInput for ctrl+c");
    H.key("keyup", { key: "c", code: "KeyC", ctrlKey: true });
    await H.ticks(1);
  });
})();
