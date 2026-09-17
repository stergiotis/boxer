// Phase 2 (session guarantees) regressions for the shipped viewer page, per
// ADR-0242 SD2: focus is an ordered input event, losing it cancels held input
// instead of completing a click, canvas <-> paste trap is an internal transfer
// rather than leaving the remote input surface, and demotion/disconnect stop
// the page driving the session without relying on an unload packet.
//
// The browser side of that decision is what these cases exercise: the focus
// InputEvent (oneof field 9), the local pointer/pending-move state, the cursor,
// the clipboard round trip through the paste trap, and role reset on close.
//
// Note on simulating focus loss: a dispatched window "blur" does not change
// document.hasFocus(), so each case that stands for "the window lost focus"
// also overrides hasFocus for the duration — that pairing is what a real blur
// looks like to the page.
"use strict";

(function () {
  const H = window.__imzero2Harness;
  const HELLO = { width: 800, height: 600, ppp: 1, cadence: 0, codec: "" };
  const ME = 3, OTHER = 5;

  let started = false;
  async function session() {
    if (started) return;
    await H.untilSocketOpen();
    H.socket.serverMessage(H.wire.hello(HELLO));
    await H.ticks(2);
    started = true;
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
    await H.ticks(2);
    await H.sleep(30);
  }

  const focusFrames = () => H.inputs("focus");

  // Focus is reported as a state transition, so the case drives a real one in
  // each direction. (Back-to-back blur+focus is deliberately NOT a transition:
  // the page settles the state on a microtask, which is what keeps an internal
  // canvas <-> paste-trap transfer off the wire — the next case.)
  H.test("phase2/focus-transitions-are-input-events-on-the-wire", async () => {
    await setRole(true);
    H.el.canvas.focus();
    await H.ticks(2);
    H.clear();

    H.setHasFocus(false);
    H.windowBlur();
    await H.ticks(2);
    let f = focusFrames();
    H.note("on blur: " + (f.map((x) => x.focused).join(",") || "none"));
    H.eq(f.length, 1, "one focus frame for losing the window");
    if (f.length) {
      H.eq(f[0].field, 9, "focus rides InputEvent oneof field 9");
      H.eq(f[0].prefix, H.PREFIX.INPUT, "focus is an input frame, not session control");
      H.eq(f[0].focused, false, "reported state on blur");
      H.eq(f[0].oneofCount, 1, "single oneof field set");
    }

    H.clear();
    H.setHasFocus(true);
    H.windowFocus();
    H.el.canvas.focus();
    await H.ticks(2);
    f = focusFrames();
    H.note("on refocus: " + (f.map((x) => x.focused).join(",") || "none"));
    H.eq(f.length, 1, "one focus frame for regaining the window");
    if (f.length) H.eq(f[0].focused, true, "reported state on refocus");

    // No repeat traffic for a state that did not change.
    H.clear();
    H.el.canvas.focus();
    await H.ticks(2);
    H.eq(focusFrames().length, 0, "re-focusing an already-focused surface sends nothing");
  });

  // ADR-0242 SD2: "Moving focus between the canvas and its paste trap does not
  // leave the remote input surface."
  H.test("phase2/canvas-to-paste-trap-is-not-focus-loss", async () => {
    await setRole(true);
    H.el.canvas.focus();
    await H.ticks(2);
    H.clear();
    H.el.pasteTrap.focus();
    await H.ticks(2);
    H.eq(document.activeElement, H.el.pasteTrap, "paste trap took DOM focus");
    let lost = focusFrames().filter((x) => x.focused === false);
    H.eq(lost.length, 0, "no focus-lost frame while focus sits on the paste trap");
    H.el.canvas.focus();
    await H.ticks(2);
    lost = focusFrames().filter((x) => x.focused === false);
    H.eq(lost.length, 0, "no focus-lost frame across the whole trap round trip");
    H.note("frames during the round trip: " + H.inputs().map((x) => x.kind).join(",") || "none");
  });

  H.test("phase2/window-blur-reports-focus-loss-without-manufacturing-a-click", async () => {
    await setRole(true);
    H.el.canvas.focus();
    await H.ticks(2);
    H.clear();
    H.pointer("pointerdown", { button: 0, pointerId: 11 });
    await H.ticks(1);
    H.eq(H.inputs("mouse_button").length, 1, "the held press was forwarded");
    H.clear();
    H.setHasFocus(false);
    H.windowBlur();
    await H.ticks(2);
    const f = focusFrames();
    H.ok(f.some((x) => x.focused === false), "window blur reports focus loss");
    const btns = H.inputs("mouse_button");
    H.eq(btns.length, 0, "cancellation does not manufacture a button release (that is the host's job)");
    H.note("frames on blur: " + (H.inputs().map((x) => x.kind).join(",") || "none"));
    H.setHasFocus(true);
    H.windowFocus();
    await H.ticks(2);
  });

  // clearInputState: a pointermove captured but not yet flushed must not be
  // delivered after focus is gone.
  H.test("phase2/pending-move-is-dropped-on-focus-loss", async () => {
    await setRole(true);
    H.el.canvas.focus();
    await H.ticks(2);
    H.clear();
    H.pointer("pointermove", { fx: 0.6, fy: 0.6, pointerId: 12 });
    H.setHasFocus(false);
    H.windowBlur();
    await H.ticks(3);
    H.eq(H.inputs("mouse_move").length, 0, "the unflushed move is discarded, not delivered after blur");
    H.setHasFocus(true);
    H.windowFocus();
    await H.ticks(2);
  });

  H.test("phase2/page-visibility-loss-reports-focus-loss", async () => {
    await setRole(true);
    H.el.canvas.focus();
    await H.ticks(2);
    H.clear();
    H.setHidden(true);
    await H.ticks(2);
    H.ok(focusFrames().some((x) => x.focused === false), "hiding the page reports focus loss");
    H.setHidden(false);
    await H.ticks(2);
  });

  // Demotion: the host stops addressing this page, so the cursor it set must go
  // back to the plain arrow and no queued local state may keep sending.
  H.test("phase2/demotion-resets-cursor-and-stops-input", async () => {
    await setRole(true);
    H.socket.serverMessage(H.wire.cursor(17)); // grabbing
    await H.ticks(1);
    H.eq(getComputedStyle(H.el.canvas).cursor, "grabbing", "host-set cursor applied while active");
    H.pointer("pointermove", { fx: 0.7, fy: 0.7, pointerId: 13 });
    await setRole(false);
    H.clear();
    await H.ticks(3);
    H.eq(getComputedStyle(H.el.canvas).cursor, "default", "cursor reset on demotion");
    H.eq(H.inputs().length, 0, "no input frame survives demotion");
    H.pointer("pointerdown", { button: 0, pointerId: 14 });
    H.pointer("pointerup", { button: 0, pointerId: 14 });
    await H.ticks(2);
    H.eq(H.inputs().length, 0, "no input accepted after demotion");
  });

  // A dropped connection is not an orderly goodbye: the page must come back as
  // passive and send nothing until a roster says otherwise.
  H.test("phase2/reconnect-starts-passive-and-reannounces", async () => {
    await setRole(true);
    const before = H.sockets.length;
    H.clear();
    H.socket.serverClose(1006);
    await H.until(() => H.sockets.length > before, "a reconnect socket", 4000);
    const sock = await H.untilSocketOpen(2000);
    await H.ticks(2);
    H.eq(H.sessions("client_hello").length, 1, "the reconnected page re-announces its capabilities");
    H.clear();
    H.pointer("pointerdown", { button: 0, pointerId: 15 });
    H.pointer("pointerup", { button: 0, pointerId: 15 });
    H.key("keydown", { key: "b", code: "KeyB" });
    await H.ticks(2);
    H.eq(H.inputs().length, 0, "no input before the new roster confirms the role");
    // Re-establish the session on the new socket for the remaining cases.
    sock.serverMessage(H.wire.hello(HELLO));
    await H.ticks(2);
    sock.serverMessage(H.wire.roster({
      youId: ME, youRole: 1, activeId: ME, max: 4,
      connections: [{ id: ME, role: 1, label: "harness" }],
    }));
    await H.ticks(2);
    await H.sleep(30);
    H.clear();
    H.el.canvas.focus();
    H.pointer("pointerdown", { button: 0, pointerId: 16 });
    H.pointer("pointerup", { button: 0, pointerId: 16 });
    await H.ticks(2);
    H.eq(H.inputs("mouse_button").length, 2, "input flows again once the new roster makes us active");
  });

  // Ctrl/Cmd+V round trip (ADR-0082 SD6 clipboard, ADR-0242 SD2 focus): the
  // shortcut hands focus to the trap, the paste is forwarded once, focus comes
  // back to the canvas, and the trip is not reported as leaving the surface.
  H.test("phase2/paste-shortcut-round-trip-sends-clipboard-once", async () => {
    await setRole(true);
    H.el.canvas.focus();
    await H.ticks(2);
    H.clear();
    H.key("keydown", { key: "v", code: "KeyV", ctrlKey: true });
    await H.ticks(1);
    H.eq(document.activeElement, H.el.pasteTrap, "ctrl+v hands focus to the paste trap");
    const dt = new DataTransfer();
    dt.setData("text/plain", "harness-paste");
    H.el.pasteTrap.dispatchEvent(new ClipboardEvent("paste", {
      clipboardData: dt, bubbles: true, cancelable: true, composed: true,
    }));
    await H.ticks(3);
    const clip = H.sessions("clipboard");
    H.eq(clip.length, 1, "exactly one ClipboardData frame for one paste");
    if (clip.length) H.eq(clip[0].text, "harness-paste", "pasted text on the wire");
    H.eq(focusFrames().filter((x) => x.focused === false).length, 0,
      "the paste round trip is not reported as focus loss");
    await H.until(() => document.activeElement === H.el.canvas, "focus back on the canvas", 1000)
      .catch(() => H.fail("focus returns to the canvas after the paste"));
  });
})();
