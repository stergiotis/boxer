// The page-side probe of the audio-lane-playout trial. It is placed before
// the viewer page's own script, wraps the AudioContext the page makes, and
// records what the audio lane schedules on it: every buffer source's start
// time and length, whether it met the one before it, and how far ahead of
// the context's clock the schedule runs. After ?probe_ms it posts one JSON
// summary to ./report. It changes nothing the page plays.
//
// It sees a stream that ran dry only when a later source is scheduled after
// the gap; one that ran dry and stayed dry shows as a lead at or below zero.
(function () {
  const params = new URLSearchParams(location.search);
  const observeMs = Number(params.get("probe_ms") || "20000");
  const loadedAt = performance.now();
  const rec = { created: 0, buffers: 0, frames: 0, underruns: 0, overlaps: 0, lateStarts: 0,
                firstAudioMs: null, rate: 0, peak: 0, states: [], errors: [], leadMs: [] };
  window.addEventListener("error", (e) => rec.errors.push(String(e.message)));
  const Real = window.AudioContext;
  let ctxRef = null, lastEnd = null;
  window.AudioContext = function (opts) {
    const ctx = new Real(opts);
    ctxRef = ctx;
    rec.created++; rec.rate = ctx.sampleRate; rec.states.push("new:" + ctx.state);
    ctx.addEventListener("statechange", () => rec.states.push(ctx.state));
    const make = ctx.createBufferSource.bind(ctx);
    ctx.createBufferSource = () => {
      const n = make();
      const start = n.start.bind(n);
      n.start = (when) => {
        const frames = n.buffer.length;
        rec.buffers++; rec.frames += frames;
        if (lastEnd !== null) {
          const d = when - lastEnd;
          // A source that does not start where the last one ended: the
          // stream ran dry and was re-anchored (an underrun), or overlaps.
          if (d > 1e-4) rec.underruns++; else if (d < -1e-4) rec.overlaps++;
        }
        if (when < ctx.currentTime) rec.lateStarts++;
        if (rec.firstAudioMs === null) rec.firstAudioMs = Math.round(performance.now() - loadedAt);
        lastEnd = when + frames / n.buffer.sampleRate;
        const ch = n.buffer.getChannelData(0);
        for (let i = 0; i < ch.length; i += 8) rec.peak = Math.max(rec.peak, Math.abs(ch[i]));
        start(when);
      };
      return n;
    };
    return ctx;
  };
  // How far the schedule runs ahead of the clock. The page reports, and so
  // is topped up, every 20 ms; sampling at an interval that shares no factor
  // with that walks through the phases of the cycle instead of sitting on
  // one of them.
  setInterval(() => {
    if (ctxRef && lastEnd !== null) rec.leadMs.push(Math.round((lastEnd - ctxRef.currentTime) * 1000));
  }, 37);
  setTimeout(() => {
    const lead = rec.leadMs.slice().sort((a, b) => a - b);
    const q = (p) => (lead.length ? lead[Math.min(lead.length - 1, Math.floor(p * lead.length))] : null);
    const out = {
      observeMs, created: rec.created, rate: rec.rate, buffers: rec.buffers, frames: rec.frames,
      scheduledSeconds: rec.rate ? rec.frames / rec.rate : 0,
      contextSeconds: ctxRef ? ctxRef.currentTime : 0,
      underruns: rec.underruns, overlaps: rec.overlaps, lateStarts: rec.lateStarts,
      firstAudioMs: rec.firstAudioMs, peak: rec.peak, states: rec.states, errors: rec.errors,
      leadMinMs: q(0), leadP05Ms: q(0.05), leadMedianMs: q(0.5), leadMaxMs: q(0.999),
      state: ctxRef ? ctxRef.state : "none",
      status: (document.getElementById("statusmsg") || {}).textContent,
    };
    fetch("./report", { method: "POST", body: JSON.stringify(out) });
  }, observeMs);
})();
