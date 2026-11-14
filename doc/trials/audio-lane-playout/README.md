---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** One run on one machine, in a muted
> headless browser over loopback. Do not cite as authoritative.

# Audio lane playout — does the lane keep a remote audio device fed

## 0 The claim, and how to cite it

**In a muted headless Chromium on the machine that also ran the Go side, over
loopback, with nothing else loading either end, the audio lane kept a 48 kHz
stereo stream playing for the 20 s each arm was observed without the page
once running dry, at every target buffer in force — 80, 150 and 300 ms — on
both carriers, the browser tab and the headless host's carrier (2026-11-14,
one run per arm, eight arms).** The arms that asked for 40 ms ran at 80 ms:
the lane keeps two and a half report intervals and 30 ms ahead whatever is
asked for.

How far the page's schedule ran ahead of its audio clock, sampled every
37 ms over an arm:

| Target in force | Carrier | Lead, lowest sample | Lead, median |
| --- | --- | --- | --- |
| 80 ms (the floor) | headless | 37 ms | 49 ms |
| 80 ms (the floor) | tab | 59 ms | 70 ms |
| 150 ms (the default) | headless | 107 ms | 118 ms |
| 150 ms (the default) | tab | 118 ms | 139 ms |
| 300 ms | headless | 257 ms | 279 ms |
| 300 ms | tab | 279 ms | 289 ms |

The context's clock, as the page's main thread reads it, moves in steps of
about 10 ms, so a lead is good to that at best.

**What this trial does not say.**

- **Not "80 ms is a safe target."** No dry run in 20 s on loopback, on an
  idle 32-thread machine, is the easiest condition the lane will meet. At
  the floor the headless arm's lowest sample was 37 ms, of which the page
  needs 10 ms to place a chunk behind what it has. The default stays 150 ms.
- **Not a latency figure.** The lead is how much audio is scheduled ahead,
  which is the delay before a pause or a change of rate or volume is heard.
  What the person hears also lags by the browser's output latency, which a
  muted headless browser does not exhibit and this trial does not measure.
- **Nothing about the playhead.** How far the position Go reports is from
  what the page plays is not measured. A first pass quoted a figure for it
  that only showed how unevenly reports arrive; it is withdrawn (§5).
- **Nothing about the resampler.** The page's context ran at 48 kHz, the
  source's rate, so every frame passed through unconverted.
- **Not a claim about networks, loaded machines, phones, background tabs or
  other browsers.** None was an arm. The target's growth under slow reports
  shows only as the floor here; a page whose timers the browser has slowed
  was not run.
- **Not reviewed, not replicated.** One machine, one run per arm, one
  browser build, one operator.

**If you need a number**, take it from `results.jsonl` in the run directory
the [logbook](./logbook.md) names. **No figure from this trial travels
without its carrier, its target and the words "loopback, muted headless
browser".**

## 1 Question and scope

[ADR-0372](../../adr/0372-audio-output-for-the-browser-hosts-a-host-provided-sink-and-a-side-lane.md)
(proposed) plays an app's sound through a viewer's page: Go sends chunks, the
page schedules them and reports what it played, and Go keeps a target ahead
of that count. Its milestone M4 asks of a measurement whether the device
runs dry, at which target buffer, on which carrier. This trial is the
protocol for that.

The domain numbers lead here; the toolbelt side is small and is recorded as
findings. In scope: the two carriers and the target buffer. Out of scope:
audible correctness, the output device, networks, the position readout, and
browsers other than the one named in a run's `env.txt`.

This page is a trial protocol (see the [directory convention](../README.md));
runs append to the [logbook](./logbook.md).

## 2 The workload

The widget gallery's waveform demo with `BOXER_WAVEFORM_DEMO_AUTOPLAY=1`: at
mount it asks the host for an output and plays its synthetic track, 48 kHz
stereo, at rate 1 and full volume. Nothing is clicked, and the gallery's
scene does not change, so the app's own frames cost next to nothing. The
browser is started with `--autoplay-policy=no-user-gesture-required`, so the
page's context starts on the lane's *want* message, and with `--mute-audio`.

## 3 System under test and arms

An arm is a carrier and a target buffer, `BOXER_AUDIO_LANE_TARGET`.

| Arm | Carrier |
| --- | --- |
| `tab-<ms>` | the Go module and the Rust host in one Web Worker, lane messages on the worker's port. The worker also posts one HTTP request per played report, which is how the Go side's trace leaves a tab |
| `headless-<ms>` | the native host, lane messages over the FIFO pair to the headless client's carrier and its WebSocket; mesh codec, 20 Hz render tick |

Targets asked for: 40, 80, 150 (the default) and 300 ms. The target in
force is in each row; it is never below the lane's floor.

## 4 Method

- **Environment.** Recorded per run in `env.txt`: the tree's commit, CPU,
  memory, kernel, toolchains, the browser and its flags, whether the
  binaries were built by the run, and their digests. No hostnames or paths.
- **Per arm**, for the observation time (20 s unless the run says
  otherwise):
  - *Page side* ([`harness/audio-probe.js`](./harness/audio-probe.js),
    placed before the viewer page's script): every buffer source the lane
    schedules — its start, its length, whether it started where the one
    before ended. A source that starts later is counted as an **underrun**:
    the page began a new segment, because the stream had run dry or was
    about to, or because a chunk was lost. Every 37 ms, the **lead**: the
    end of the schedule less the context's clock. A stream that ran dry and
    was never sent another chunk shows as a lead at or below zero, not as an
    underrun.
  - *Go side* (`BOXER_AUDIO_LANE_TRACE=1`): one line per played report with
    the reported count, what was sent and the target in force.
- **Reduction.** `reduce` in [`measure.sh`](./measure.sh) turns an arm's two
  files into one row. The per-report trace is dropped unless `KEEP_TRACE=1`,
  so the target figures of a kept run cannot be recomputed from its
  directory.
- **Reporting.** A run's rows are `results.jsonl` in its directory, a dozen
  numbers per arm. They are not written to the facts store: there is one
  run, and a table that small is read where it lies. A second run is the
  point at which that stops being true.
- **Running it.** `./measure.sh <run-dir>` from this directory builds what
  it needs into a scratch directory and runs every arm.

## 5 What earlier passes got wrong

Two passes on 2026-11-14 preceded the kept run; their rows are not in the
tree.

- **A playhead figure that measured something else.** The first pass
  reported, per played report, the difference between the count Go had
  estimated and the count the page then reported, and called it the
  distance between the position readout and the device. Both counts are
  anchored at the arrival of a report, so a report's transit time cancels
  out: the figure showed how unevenly reports arrive. It is no longer
  computed.
- **A lead read off one phase.** The first pass sampled the lead every
  100 ms, a multiple of the page's 20 ms report timer, so every sample of
  an arm fell on the same point of the cycle.
- **A "busy" arm that was not.** It ran the tab at a continuous 60 Hz over
  a scene that does not change, where a pass costs next to nothing. It is
  removed until there is a workload that loads the worker.
- **A floor that was too low.** With the lane's floor at two and a half
  report intervals, 50 ms, the second pass's headless arm at a 40 ms target
  began a new segment about seven times a second: the page had under 10 ms
  scheduled when a chunk arrived. The earlier pass had not shown it because
  the page then placed such a chunk behind what was left, which plays it
  late. The floor is now 80 ms.

## 6 Findings ledger

Findings follow the [directory convention](../README.md). Pre-registered
candidate: nothing in the toolbelt puts input into a real browser page, so
the workload cannot be clicked into playing.

## 7 Milestone cut

- **M0 — loopback, one browser.** ✓ The arms above. Gate: every arm
  produces a row.
- **M1 — a second machine as the viewer.** The same arms with the browser
  on another host, so the lane crosses a network and an unmuted device.
- **M2 — slowed reports.** A page whose timers the browser has slowed (a
  background tab), which is what the growing target exists for.
- **M3 — the position.** The page's count and Go's readout sampled against
  one clock, and a source at another rate than the device's.

## 8 Open questions

1. What the browser's output latency adds between the count the page
   reports and what is heard, per device class.
2. Whether a backgrounded tab's reports slow enough to matter while its
   audio plays.

Related: [ADR-0372](../../adr/0372-audio-output-for-the-browser-hosts-a-host-provided-sink-and-a-side-lane.md),
[ADR-0208](../../adr/0208-audio-waveform-player-widget.md),
[app-audio-output](../../howto/app-audio-output.md).
