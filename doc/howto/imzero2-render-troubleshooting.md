---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified end to end; do not cite as
> authoritative.

# How to triage janky / laggy imzero2 rendering

Diagnose uneven frame pacing — choppy scrolling or stuttering animation — in an
imzero2 desktop app, and tell apart the factors you *can* change (present mode,
CPU governor) from the environmental floor you can't (the compositor's own
present pacing). Out of scope: a fully frozen app (see
[how-to: triage a frozen imzero2 demo](../skills/imzero2/howto-triage-hang.md))
and genuine render-work overruns (those trip the slow-frame warning — see
[ADR-0062](../adr/0062-imzero2-render-cadence.md)).

## When to use this recipe

Scrolling or animation feels uneven, but the window is *not* frozen and the app
is *not* burning CPU. The tell is in the frame-pacing distribution: the median
frame rate is fine yet the worst-case is roughly half of it — the app misses a
display refresh every so often (a "60→30" beat), which reads as micro-stutter.

Measure before you change anything. The knobs below only *explain* a pacing
number; they are not a checklist to apply blind. In particular, the dominant
factor is usually the compositor, which none of the app-side knobs touch.

## Prerequisites

- A running imzero2 app (e.g. `./rust/imzero2/hmi.sh`) on a Wayland or X11
  session.
- For the deep measurement: `WAYLAND_DEBUG=1`, plus `python3` (or `awk`) to
  reduce the log. Wayland only.
- For the OS knobs in step 4: root (they write `sysfs`).

## Steps

1. **Read the metrics overlay — is it a pacing problem at all?** The bottom
   status bar shows a windowed frame-rate distribution, e.g.
   `n=240  p0 … p25 … p50 … p75 … p100 fps`, and a per-frame split
   `Go …ms  Rust …ms  vsync …ms`.

   - If `p50` is at your refresh rate (≈60) but `p0` is roughly **half** of it,
     you have missed-refresh doubling — this recipe applies.
   - If `Go + Rust` are both ≪ the `vsync` figure, the app is **not** overrunning
     its work budget; the frame time is display-wait. That is expected — see
     [ADR-0062 §SD3](../adr/0062-imzero2-render-cadence.md). Do not go looking
     for a slow widget.
   - If `p50` itself is low, this is a different problem (work overrun or a
     reactive-cadence idle window); stop here and see Troubleshooting.

2. **(Optional) Measure the pacing distribution directly.** The overlay is a
   summary; for hard numbers, count how far apart the client commits frames to
   the compositor:

   ```bash
   WAYLAND_DEBUG=1 VSYNC=on ./rust/imzero2/hmi.sh 2> /tmp/wl.log   # Ctrl-C after ~10 s
   python3 - /tmp/wl.log <<'PY'
   import re, sys, statistics as st
   ts = re.compile(r'^\[\s*(\d+)\.(\d+)\]')            # libwayland stamps are ms
   t = []
   for l in open(sys.argv[1], errors='replace'):
       m = ts.match(l)
       if m and '->' in l and '.commit()' in l:
           t.append(int(m.group(1)) + int(m.group(2))/1000)
   d = sorted(t[i+1]-t[i] for i in range(len(t)-1))
   n = len(d); p = lambda q: d[min(n-1, int(q*n))]
   dbl = sum(1 for x in d if 1.5/60*1000 <= x < 2.5/60*1000)  # frames near 2× refresh
   print(f"n={n} p50={p(.5):.1f} p90={p(.9):.1f} p99={p(.99):.1f} "
         f"std={st.pstdev(d):.1f}ms  doubled≈{100*dbl/n:.0f}%")
   PY
   ```

   The `doubled≈` figure is your missed-refresh rate. Re-run with `VSYNC=off`
   to compare (step 3).

3. **Try the one app-side lever — present mode.** With `vsync` on, a frame whose
   present lands just after the refresh deadline slips a whole refresh (the
   doubling). Turning it off removes that hard deadline; under a compositor there
   is no tearing, because the compositor composites regardless.

   ```bash
   VSYNC=off ./rust/imzero2/hmi.sh          # demo; per-app: the client's -vsync off flag
   ```

   Re-measure (step 1 or 2). The doubling rate typically drops. This is the only
   knob imzero2 itself owns.

4. **Check the OS levers (root, per-machine — they widen/narrow the tail, not the
   beat).** A render loop that sleeps most of each frame pays wake-up latency on
   a power-saving CPU, which inflates the worst-case tail (not the steady
   doubling):

   ```bash
   cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor        # e.g. powersave
   cat /sys/devices/system/cpu/cpu0/cpufreq/energy_performance_preference
   # test the effect (reversible):
   echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference
   ```

   `performance` measurably tightens the tail; it does **not** remove the
   doubling. `/dev/cpu_dma_latency` (PM QoS) is a finer knob for C-state exit
   latency. These help a dev box feel smoother but are not a product fix — they
   won't reach an end user who hasn't set them.

5. **Recognise the environmental floor.** If a residual doubling survives steps
   3–4, it is the **compositor + toolkit present-pacing floor**, not an imzero2
   bug. A bare single-process `eframe`/`wgpu` app on the same session exhibits the
   same doubling — so the Go↔Rust lock-step is not the cause. Don't chase an
   app-side fix for this part; the only remedies are `-vsync off` (step 3) or the
   compositor itself.

## Verification

After `-vsync off` (and optionally the `performance` governor), re-read the
overlay: `p0` should move up toward `p50` and the worst-case tail should shrink.
A small residual doubling is the compositor floor and is expected — the goal is
to confirm *which* factors moved the number, not to reach a perfect 60.

## Troubleshooting

- **Symptom:** `p50` itself (not just `p0`) is well below the refresh rate.
  **Cause:** real work overrun (`Go + Rust` large in the overlay), or reactive
  cadence on a visible-but-idle window.
  **Fix:** for overruns, look at the slow-frame warning and profile with puffin;
  for cadence, note `IMZERO2_RENDER_CADENCE=reactive` drops idle windows to a
  heartbeat by design ([ADR-0062](../adr/0062-imzero2-render-cadence.md)).

- **Symptom:** the window drops to ~1 fps when it loses focus or is covered.
  **Cause:** the compositor throttles occluded windows' frame callbacks; with
  `vsync` the lock-stepped loop inherits that wait.
  **Fix:** expected, not a bug ([ADR-0062 Context](../adr/0062-imzero2-render-cadence.md)).
  It returns to full rate on focus.

- **Symptom:** scroll itself registers inconsistently, not just unevenly.
  **Cause:** would point at the egui-side scroll path rather than pacing — not
  observed for a stock `ScrollArea`. Drive it with
  [egui_mcp](egui-mcp.md) and confirm the offset tracks the injected delta
  exactly before suspecting the widget.
