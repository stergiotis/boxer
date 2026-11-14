---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** One run on one machine, in a muted
> headless browser over loopback. Do not cite as authoritative.

# Audio lane playout — does the lane keep a remote audio device fed, and how far is the playhead from it

## 0 The claim, and how to cite it

**In a muted headless Chromium on the machine that also ran the Go side, over
loopback, with nothing else loading either end, the audio lane played 20 s of
a 48 kHz stereo stream without one underrun at every target buffer tried —
40, 80, 150 and 300 ms — on both carriers, the browser tab and the headless
host's carrier (2026-11-14, one run per arm).** At the default target of
150 ms the page's schedule ran 139 ms ahead of its audio clock in the tab and
107 ms ahead through the headless carrier (medians). With the target set to
40 ms the device raised it to about 50 ms by itself, and the schedule ran
29 ms ahead on both carriers at the median, 18 ms at its lowest through the
headless carrier. A lead is good to a report interval at best: an earlier
pass over the same arms the same hour, whose figures are not kept, put
several of them 10 to 20 ms away.

At each arrival of a played report, the Go side's estimate of the device's
frame count differed from the count reported by 1.4 ms at the median and
9.4 ms at the 95th percentile, in every arm.

**What this trial does not say.**

- **Not "40 ms is a safe target."** No underrun in 20 s on loopback, on an
  idle 32-thread machine, is the easiest condition the lane will meet. The
  18 ms of margin in the headless arm is less than one report interval. The
  default stays 150 ms; nothing here argues for lowering it.
- **Not a latency figure.** The lead is how much audio is scheduled ahead,
  which is the delay before a change of rate or volume is heard. What the
  person hears also lags by the browser's output latency, which a muted
  headless browser does not exhibit and this trial does not measure.
- **Not "the playhead is within 10 ms of what is heard."** The residual
  compares Go's estimate with the page's count of frames its context
  consumed, at the moment the report arrives. It includes the time the
  report took to arrive and excludes the output latency.
- **Not a claim about networks, loaded machines, phones, background tabs or
  other browsers.** None was an arm. The growing target that covers slow
  reports is exercised only by the Go tests' scripted device.
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
of that count. Its milestone M4 asks two things of a measurement, and this
trial is the protocol for both:

1. **Does the device run dry**, at which target buffer, on which carrier?
2. **How far is the position readout from the device**, since the playhead
   is drawn from Go's estimate between reports?

The domain numbers lead here; the toolbelt side is small and is recorded as
findings. In scope: the two carriers, the target buffer, a tab whose worker
is also building frames. Out of scope: audible correctness, the output
device, networks, and browsers other than the one named in a run's
`env.txt`.

This page is a trial protocol (see the [directory convention](../README.md));
runs append to the [logbook](./logbook.md).

## 2 The workload

The widget gallery's waveform demo with `BOXER_WAVEFORM_DEMO_AUTOPLAY=1`: at
mount it asks the host for an output and plays its synthetic track, 48 kHz
stereo, at rate 1 and full volume. Nothing is clicked. The browser is
started with `--autoplay-policy=no-user-gesture-required`, so the page's
context starts on the lane's *want* message, and with `--mute-audio`.

## 3 System under test and arms

An arm is a carrier and a target buffer, `BOXER_AUDIO_LANE_TARGET`.

| Arm | Carrier | What differs |
| --- | --- | --- |
| `tab-<ms>` | the Go module and the Rust host in one Web Worker, lane messages on the worker's port | reactive cadence: the worker builds a frame only when asked |
| `tab-150-busy` | as above | the worker ticks at a continuous 60 Hz, so the thread that pumps the lane is also building frames |
| `headless-<ms>` | the native host, lane messages over the FIFO pair to the headless client's carrier and its WebSocket | mesh codec, 20 Hz render tick |

Targets: 40, 80, 150 (the default) and 300 ms.

## 4 Method

- **Environment.** Recorded per run in `env.txt`: commit, CPU, memory,
  kernel, toolchains, the browser and its flags. No hostnames or paths.
- **Per arm**, for the observation time (20 s unless the run says
  otherwise):
  - *Page side* ([`harness/audio-probe.js`](./harness/audio-probe.js),
    placed before the viewer page's script): every buffer source the lane
    schedules — its start, its length, whether it started where the one
    before ended. A source that starts later is an **underrun**: the stream
    ran dry and was re-anchored. Ten times a second, the **lead**: the end
    of the schedule less the context's clock.
  - *Go side* (`BOXER_AUDIO_LANE_TRACE=1`): one line per played report with
    the reported count, the count Go had estimated just before, what was
    sent and the target in force. The **residual** is the absolute
    difference of the first two, in milliseconds at the device's rate.
- **Reduction.** `reduce` in [`measure.sh`](./measure.sh) turns an arm's two
  files into one row. The per-report trace is dropped unless
  `KEEP_TRACE=1`.
- **Reporting.** A run's rows are `results.jsonl` in its directory, a dozen
  numbers per arm. They are not written to the facts store: there is one
  run, and a table that small is read where it lies. A second run is the
  point at which that stops being true.
- **Running it.** `./measure.sh <run-dir>` from this directory builds what
  it needs into a scratch directory and runs every arm.

## 5 Findings ledger

Findings follow the [directory convention](../README.md). Pre-registered
candidate: nothing in the toolbelt puts input into a real browser page, so
the workload cannot be clicked into playing.

## 6 Milestone cut

- **M0 — loopback, one browser.** ✓ The arms above. Gate: every arm
  produces a row.
- **M1 — a second machine as the viewer.** The same arms with the browser
  on another host, so the lane crosses a network and an unmuted device.
- **M2 — slowed reports.** A page whose timers the browser has slowed (a
  background tab), which is what the growing target exists for.

## 7 Open questions

1. What the browser's output latency adds between the count the page
   reports and what is heard, per device class.
2. Whether a backgrounded tab's reports slow enough to matter while its
   audio plays.

Related: [ADR-0372](../../adr/0372-audio-output-for-the-browser-hosts-a-host-provided-sink-and-a-side-lane.md),
[ADR-0208](../../adr/0208-audio-waveform-player-widget.md),
[app-audio-output](../../howto/app-audio-output.md).
