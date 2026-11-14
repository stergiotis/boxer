// Audio lane (ADR-0372) regressions for the shipped viewer page: the page as
// the session's audio device. It announces itself, makes its AudioContext
// only when Go wants one, schedules chunks back to back on the context's
// timeline, and reports what it played.
//
// The AudioContext is a fake owned by this file, installed before the page
// makes one (which it does lazily, on the lane's WANT message): the cases
// move its clock by hand and read what the page scheduled on it. Nothing here
// plays a sound.
//
// The fixture below is the wire contract. The same bytes are checked by the
// Go codec's test (public/science/audio/sink/lanesink), which reads this file:
// a layout changed on one side fails on the other.
"use strict";

(function () {
  const H = window.__imzero2Harness;

  const FIXTURE = /* lane-fixture-begin */ [
    {"name": "want", "hex": "05"},
    {"name": "open", "hex": "01 07000000 03000000 80bb0000 02 01", "stream": 7, "epoch": 3, "sampleRate": 48000, "channels": 2},
    {"name": "chunk", "hex": "02 07000000 03000000 0000 ff7f 0180 0040", "stream": 7, "epoch": 3, "samples": [0, 32767, -32767, 16384]},
    {"name": "flush", "hex": "03 07000000 04000000", "stream": 7, "epoch": 4},
    {"name": "close", "hex": "04 07000000", "stream": 7},
    {"name": "device", "hex": "81 02 80bb0000", "state": 2, "sampleRate": 48000},
    {"name": "played", "hex": "82 07000000 04000000 0004000000000000", "stream": 7, "epoch": 4, "frames": 1024}
  ] /* lane-fixture-end */;

  const fx = (name) => {
    const e = FIXTURE.find((x) => x.name === name);
    const hex = e.hex.replace(/\s+/g, "");
    const b = new Uint8Array(hex.length / 2);
    for (let i = 0; i < b.length; i++) b[i] = parseInt(hex.substr(2 * i, 2), 16);
    return b;
  };
  const toPage = (body) => H.socket.serverMessage(H.wire.framed(H.PREFIX.AUDIO, body));
  const fromPage = () => H.rawFrames(H.PREFIX.AUDIO).map((f) => f.subarray(1));
  const hexOf = (b) => Array.from(b).map((x) => x.toString(16).padStart(2, "0")).join("");
  // A chunk of `frames` stereo frames of one value, in the fixture's layout.
  const chunk = (stream, epoch, frames, value) => {
    const b = new Uint8Array(9 + frames * 4), d = new DataView(b.buffer);
    b[0] = 0x02; d.setUint32(1, stream, true); d.setUint32(5, epoch, true);
    for (let i = 0; i < frames * 2; i++) d.setInt16(9 + 2 * i, value, true);
    return b;
  };

  // ---- the fake context ---------------------------------------------------
  const FAKE = { contexts: [], allowResume: true, initialState: "suspended" };
  class FakeAudioContext {
    constructor() {
      this.sampleRate = 48000;
      this.state = FAKE.initialState;
      this.currentTime = 0;
      this.onstatechange = null;
      this.destination = {};
      this.started = [];
      FAKE.contexts.push(this);
    }
    resume() {
      if (!FAKE.allowResume) return Promise.reject(new Error("not allowed"));
      if (this.state !== "running") {
        this.state = "running";
        if (this.onstatechange) this.onstatechange();
      }
      return Promise.resolve();
    }
    createBuffer(channels, frames, rate) {
      const data = [];
      for (let c = 0; c < channels; c++) data.push(new Float32Array(frames));
      return { numberOfChannels: channels, length: frames, sampleRate: rate, getChannelData: (c) => data[c] };
    }
    createBufferSource() {
      const ctx = this;
      const n = {
        buffer: null, onended: null, when: null, stopped: false, connected: false,
        connect() { n.connected = true; },
        disconnect() { n.connected = false; },
        start(when) { n.when = when; ctx.started.push(n); },
        stop() { n.stopped = true; },
      };
      return n;
    }
  }
  const ctx = () => FAKE.contexts[FAKE.contexts.length - 1];

  // ---- session ------------------------------------------------------------
  const HELLO = { width: 800, height: 600, ppp: 1, cadence: 0, codec: "" };
  const ME = 3, OTHER = 5;
  let started = false;
  async function setRole(active) {
    if (!started) {
      await H.untilSocketOpen();
      H.socket.serverMessage(H.wire.hello(HELLO));
      await H.ticks(2);
      started = true;
    }
    H.socket.serverMessage(H.wire.roster({
      youId: ME, youRole: active ? 1 : 0, activeId: active ? ME : OTHER, max: 4,
      connections: [{ id: ME, role: active ? 1 : 0, label: "harness" }, { id: OTHER, role: active ? 0 : 1, label: "other" }],
    }));
    await H.ticks(2);
    await H.sleep(30);
  }

  H.test("phase5/promotion-announces-a-suspended-device-and-makes-no-context", async () => {
    window.AudioContext = FakeAudioContext;
    H.clear();
    await setRole(true);
    const sent = fromPage();
    H.eq(sent.length, 1, "one lane message on promotion");
    if (sent.length) H.eq(hexOf(sent[0]), "810100000000", "a suspended device with no rate yet");
    H.eq(FAKE.contexts.length, 0, "no AudioContext before Go wants one");
  });

  H.test("phase5/want-makes-the-context-and-a-gesture-starts-it", async () => {
    FAKE.allowResume = false;
    H.clear();
    toPage(fx("want"));
    await H.ticks(2);
    H.eq(FAKE.contexts.length, 1, "WANT makes the context");
    let sent = fromPage();
    H.eq(sent.length, 0, "still suspended: nothing new to announce");

    // The browser lets it start once the person acts on the page.
    FAKE.allowResume = true;
    H.clear();
    H.key("keydown", { key: "a", code: "KeyA" });
    await H.ticks(2);
    sent = fromPage();
    H.eq(sent.length, 1, "one announcement for the start");
    if (sent.length) H.eq(hexOf(sent[0]), hexOf(fx("device")), "running, in the fixture's layout");

    // A second WANT for a state Go already knows restarts nothing.
    H.clear();
    toPage(fx("want"));
    await H.ticks(2);
    H.eq(fromPage().length, 0, "no second announcement");
    H.eq(FAKE.contexts.length, 1, "one context for the session");
  });

  H.test("phase5/chunks-are-scheduled-back-to-back", async () => {
    const c = ctx();
    c.currentTime = 10;
    toPage(fx("open"));
    toPage(fx("chunk"));
    await H.ticks(1);
    H.eq(c.started.length, 1, "the fixture's chunk is one buffer source");
    const first = c.started[0];
    H.near(first.when, 10.03, 1e-9, "a stream with nothing scheduled starts a little ahead of now");
    H.eq(first.buffer.length, 2, "two frames");
    H.eq(first.buffer.numberOfChannels, 2, "stereo");
    H.eq(first.buffer.sampleRate, 48000, "at the stream's rate");
    H.ok(first.connected, "connected to the destination");
    const l = first.buffer.getChannelData(0), r = first.buffer.getChannelData(1);
    H.near(l[0], 0, 1e-9, "L0");
    H.near(r[0], 32767 / 32768, 1e-6, "R0 is full scale");
    H.near(l[1], -32767 / 32768, 1e-6, "L1");
    H.near(r[1], 0.5, 1e-9, "R1");

    toPage(chunk(7, 3, 480, 1000));
    await H.ticks(1);
    H.eq(c.started.length, 2, "the second chunk");
    H.near(c.started[1].when, first.when + 2 / 48000, 1e-9, "starts on the sample after the first");
    toPage(chunk(7, 3, 480, 1000));
    await H.ticks(1);
    H.near(c.started[2].when, first.when + 482 / 48000, 1e-9, "positions are frames since the anchor");
  });

  H.test("phase5/a-flush-drops-what-is-scheduled-and-reports-follow-the-clock", async () => {
    const c = ctx();
    const before = c.started.slice();
    toPage(fx("flush"));
    await H.ticks(1);
    H.ok(before.every((n) => n.stopped), "every scheduled source is stopped");

    // A chunk still on its way for the old epoch is not played.
    toPage(chunk(7, 3, 480, 1000));
    await H.ticks(1);
    H.eq(c.started.length, before.length, "the old epoch's chunk is ignored");

    c.currentTime = 20;
    toPage(chunk(7, 4, 2048, 1000));
    await H.ticks(1);
    const node = c.started[c.started.length - 1];
    H.near(node.when, 20.03, 1e-9, "the new epoch starts ahead of now");

    H.clear();
    c.currentTime = node.when + 1024 / 48000 + 1e-6;
    await H.sleep(60);
    const reports = fromPage().filter((b) => b[0] === 0x82);
    H.ok(reports.length >= 1, "a played report");
    if (reports.length) H.eq(hexOf(reports[reports.length - 1]), hexOf(fx("played")), "1024 frames of epoch 4, in the fixture's layout");

    // No change on the clock, no further report.
    H.clear();
    await H.sleep(60);
    H.eq(fromPage().length, 0, "a count that did not change is not reported again");
  });

  H.test("phase5/a-stream-that-ran-dry-restarts-ahead-of-now", async () => {
    const c = ctx();
    c.currentTime = 30; // long past the 2048 frames scheduled at 20.03
    H.clear();
    await H.sleep(60);
    let reports = fromPage().filter((b) => b[0] === 0x82);
    H.ok(reports.length >= 1, "the last frames are reported");
    if (reports.length) H.eq(new DataView(reports[reports.length - 1].buffer, reports[reports.length - 1].byteOffset).getUint32(9, true), 2048, "all 2048 played");

    toPage(chunk(7, 4, 960, 1000));
    await H.ticks(1);
    const node = c.started[c.started.length - 1];
    H.near(node.when, 30.03, 1e-9, "not scheduled in the past");
    H.clear();
    c.currentTime = 30.03 + 480 / 48000 + 1e-6;
    await H.sleep(60);
    reports = fromPage().filter((b) => b[0] === 0x82);
    H.ok(reports.length >= 1, "a report after the restart");
    if (reports.length) H.eq(new DataView(reports[reports.length - 1].buffer, reports[reports.length - 1].byteOffset).getUint32(9, true), 2048 + 480, "the count carries on across the gap");
  });

  H.test("phase5/close-and-demotion-stop-the-device", async () => {
    const c = ctx();
    const n = c.started.length;
    toPage(fx("close"));
    await H.ticks(1);
    H.ok(c.started[n - 1].stopped, "close stops what was scheduled");
    toPage(chunk(7, 4, 480, 1000));
    await H.ticks(1);
    H.eq(c.started.length, n, "a chunk for a closed stream is ignored");

    toPage(fx("open"));
    toPage(fx("chunk"));
    await H.ticks(1);
    H.eq(c.started.length, n + 1, "reopened");
    H.clear();
    await setRole(false);
    H.ok(c.started[n].stopped, "demotion stops the stream");
    H.eq(fromPage().length, 0, "and says nothing: the carrier tells Go");
    toPage(fx("open"));
    toPage(fx("chunk"));
    await H.ticks(1);
    H.eq(c.started.length, n + 1, "a passive page plays nothing");

    // Promoted again it announces what its context is now.
    H.clear();
    await setRole(true);
    const sent = fromPage();
    H.eq(sent.length, 1, "one announcement on promotion");
    if (sent.length) H.eq(hexOf(sent[0]), hexOf(fx("device")), "running at the context's rate");
  });
})();
