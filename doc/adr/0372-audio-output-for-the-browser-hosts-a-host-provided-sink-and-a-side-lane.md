---
type: adr
status: proposed
date: 2026-11-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0372: Audio output for the browser hosts — a host-provided sink, a side lane beside FFFI2, one player in the viewer page

## Context

[ADR-0208](./0208-audio-waveform-player-widget.md) put everything about sound
in Go: decoding, playback rate, gain and the position all live behind
`sink.SinkI`, and its SD6 named that interface as the place where a
host-brokered output would attach. It built one device, the pulse-protocol
sink, and left the brokered output as a deferral.

Nothing uses the seam. As observed on 2026-11-14, the apps that play sound
open the pulse sink by name and fall back to `sink.Null` when that fails. The
consequence differs per host:

- **Desktop.** Sound reaches the machine the person sits at.
- **Headless host with a remote viewer** (ADR-0024). The Go process runs on
  the serving machine, so the pulse socket it finds, if any, is that
  machine's. The person at the browser hears nothing. ADR-0024, ADR-0086,
  ADR-0087 and ADR-0088 each list audio as out of scope.
- **Browser tab** (ADR-0263, ADR-0278). A wasip1 module has no sockets, so
  the open fails and every app is silent.

Four properties of the two browser hosts constrain an answer.

**The headless host has one wire.** Go reaches its Rust client over the FFFI2
pipes, and the carrier owns one WebSocket whose messages carry a one-byte
prefix. ADR-0024 SD6 chose the single socket so that one TLS-terminating proxy
and one authentication boundary (ADR-0082) cover the whole session, and
ADR-0088 SD13 kept to it when the codec control plane was added.

**FFFI2 is lock-stepped to the frame.** Go writes to it only while a pass is
being built. Under the reactive cadence passes can be a heartbeat apart, and a
slow pass delays everything behind it.

**The tab has one thread.** The Go module is a reactor (ADR-0263 SD1):
goroutines run only inside an export the worker calls. A device callback that
runs on a goroutine of its own, which is how the pulse sink is fed, has
nothing to run it. A Web Worker also has no audio device; the `AudioContext`
belongs to the page.

**The viewer page is shared.** The tab reuses the remote viewer's page with a
port in place of the socket (ADR-0263 SD3), so whatever plays sound in the
page can serve both hosts.

Scope: output only. Capture is a deferral.

## Design space (QOC)

**Question.** By what route do the samples a Go sink produces reach an audio
device in the browser, for the headless host and for the tab?

**Options.**

- **O1 — In-band FFFI2.** New IDL commands carry stream control and PCM; the
  Rust host relays them to the carrier, and the browser host hands them to the
  worker as it hands over the mesh.
- **O2 — A side lane beside FFFI2.** A second channel carries a small device
  protocol. Headless: a pipe pair between Go and its client, relayed by the
  carrier under a new prefix on the existing WebSocket. Tab: host imports
  between the Go module and the worker, which is how the tab's HTTP crosses.
  One message format and one page-side player for both. (chosen)
- **O3 — A Go-side endpoint.** The Go process serves audio on a socket of its
  own and the viewer connects to it.
- **O4 — A server-side device and an encoder.** The app plays into a null
  sink of an audio server on the serving machine; an `ffmpeg` subprocess
  captures and encodes it; the carrier ships the result.
- **O5 — A media element.** The recording itself goes to the browser, which
  decodes and plays it; Go sends transport commands and reads the element's
  clock.

**Criteria.**

- **C1 — One socket, one authentication boundary.** ADR-0024 SD6 and
  ADR-0082 hold without a second listener or route.
- **C2 — Independence from the frame.** A slow or absent pass does not
  interrupt sound.
- **C3 — Both hosts.** The same mechanism, or at least the same wire and
  player, serves the headless host and the tab.
- **C4 — Go keeps owning audio.** One playback engine; a position that is a
  frame index into a `pcm.SourceI`; sources that are not files (ADR-0208's
  2026-08-28 update).
- **C5 — Lane discipline.** No IDL change and no audio state in the Rust
  host, the terms on which ADR-0208 rejected a host-owned engine.
- **C6 — Runtime dependencies.** What the serving machine must have
  installed.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 in-band | O2 side lane | O3 Go endpoint | O4 server device | O5 media element |
|----|----|----|----|----|----|
| C1 | ++ | ++ | −− | +  | −  |
| C2 | −− | +  | ++ | ++ | ++ |
| C3 | ++ | +  | −− | −− | +  |
| C4 | ++ | ++ | ++ | +  | −− |
| C5 | −− | +  | ++ | +  | −  |
| C6 | ++ | ++ | +  | −− | +  |

**Assessment notes.** O1 is the only option that is one mechanism for both
hosts, and it loses on the two criteria that decided ADR-0208: it puts audio
into the IDL, and it ties sound to passes, so a pass that runs long is heard.
O2 scores `+` rather than `++` on C2 because the tab still runs Go only
inside an export; SD4 gives it an export that is not a pass. On C3 it is two
carriers under one format. On C5 the carrier gains a relay that does not read
what it forwards.

## Decision

We will make the sink the host's to give: an app asks the runtime for an
audio output and receives a `sink.SinkI`, and the host binary decides what
device is behind it. For the two browser hosts the device is **a side lane
beside FFFI2 (O2)** that carries 16-bit PCM and a small flow-control protocol
to **one player in the viewer page**, with the page's audio clock as the
master. Go keeps the transport, the rate, the gain and the position.

### Subsidiary design decisions

- **SD1 — An app opens its output through the runtime.** ✓ The manifest
  declares a capability subject, `audio.out.open`, with a reason, as it
  declares `fs.dialog.read`. An opener (`audioout.Open`) takes the app's bus
  client and a `pcm.SourceI`, makes the request on it, and redeems the grant
  in the reply, in process, for a sink on the host's device. The request is
  what the bus's capability check and its audit see; samples never travel on
  the bus. Where the service is not running, the request is refused or the
  device does not open, the opener returns `sink.Null` and says why, so the
  app shows ADR-0208's "no output device" state instead of failing. The
  opener lives on the keelson side of the tree: `track` and `sink` keep
  importing nothing from it.

- **SD2 — The service is opt-in, and the binary picks the device.** ✓
  `hostboot.Services` and `tabhost.Services` each gain an audio-output field,
  per ADR-0211 SD2 and ADR-0278 SD2. The desktop host backs the service with
  the pulse device, the headless host and the tab with the lane (SD4). Where
  `hostboot.Options` names no device, the choice follows where the person
  is: the lane when `IMZERO2_HEADLESS_LISTEN` names a carrier, the pulse
  device otherwise. A host
  that leaves the field unset serves no subject and its apps get the null
  sink. `AllServices()` sets it, so a scene run through the carousel has the
  machine's sound server behind it where there is one, as it had while apps
  opened the pulse sink themselves; a scene asserts readouts that hold with
  or without a device. The tab report (ADR-0278 SD7) lists the subject like
  any other.

- **SD3 — One engine, devices under it.** ✓ What the pulse sink does that is
  not pulse — the transport state, the cursor, the forward-only reads with
  held lookahead, the rate resampler, the gain, the position bookkeeping,
  waiting while not playing — becomes an engine that implements
  `sink.SinkI` (`playout.Engine`) and hands a device its frames: the device
  states the rate it takes them at, asks for them, and says how many it
  holds unplayed. A device type wraps an engine and adds what only it does
  to a transport call — the pulse sink its stream's one start, the lane a
  flush on a seek. The engine is tested against a scripted device in the
  default lane.

- **SD4 — The lane is a device protocol with two carriers.** ✓ The messages
  are the same bytes on both, and their layout is `lanesink`'s `wire.go`.
  Toward the device: *open* and *close* for a stream, *chunk* for its
  frames, *flush* to drop what the device holds and start a new epoch, and
  *want*. From the device: an *announcement* of its state and rate, and
  *played*, its frame count for a stream's epoch. An announcement voids
  every stream of an earlier one on both sides, so a page that reconnects,
  or a context that restarts, begins clean.
  - *Headless.* A FIFO pair named in the client's environment, the shape of
    the wake channel (ADR-0363, proposed, §M4), each message behind its
    length. The carrier forwards lane messages under prefix `0x05` in both
    directions without reading them. The one message it writes itself tells
    Go that the device went away, on every change of owner.
  - *Tab.* Host imports of the Go module, as the HTTP transport has, and one
    more export beside `frame`: a pump the worker calls when the page's
    device has sent something, which tops the streams up and returns without
    building a pass. The worker posts lane messages on the port the mesh
    already uses. The Rust browser host is not involved.

- **SD5 — The device pulls; its clock is the master.** ✓ The page reports,
  per stream, how many frames it has played. Go sends only enough to keep a
  target buffer ahead of that count, so the amount in flight is bounded by
  what the device consumed and the two clocks cannot drift apart. The target
  is the device's option, as latency is the pulse sink's, and it is a floor:
  it grows to cover the interval between two reports where they come slower
  than it allows for — a page whose timers the browser has slowed, a pass
  that kept a tab's worker busy — and comes back down when they speed up.
  `Position` is the last reported count, advanced by wall clock between
  reports and never moved backwards. A seek sends a flush, which drops what
  the page holds for that stream. A pause sends nothing: what the page
  holds, one target at most, plays out, as the pulse sink's buffer does.

- **SD6 — One sample format and one rate.** ✓ Chunks are interleaved
  little-endian signed 16-bit, mono or stereo. The device announces one
  sample rate and every stream is delivered at it; the engine's resampler
  covers the ratio together with the playback rate. A format field in the
  stream's open message leaves room for others (see Deferred).

- **SD7 — One player in the page.** ✓ The viewer page plays lane messages
  the same way whether they arrive on the socket or the worker's port: one
  `AudioContext`, each chunk scheduled on its timeline as a buffer source,
  placed by a count of frames since an anchor so that two chunks meet on a
  sample. This works in a page served without TLS, where an `AudioWorklet`
  is not available, and needs no second file beside the single-file page.
  The context is made only when Go says a stream is waiting (*want*), so a
  session in which no app asks for an output never touches the browser's
  audio. A browser starts a context suspended until the person acts on the
  page; the page tries to resume it on *want* and on each input after it,
  and announces the device as suspended until then.

- **SD8 — The device belongs to the active viewer.** ✓ Lane messages go to,
  and are accepted from, the connection that owns input (ADR-0086), which is
  the scope ADR-0088 SD12 gave the codec. A takeover moves the device: Go is
  told the old one is gone, the new page announces itself, and each open
  stream continues from its current position. Passive viewers hear nothing.
  Nothing is sent toward a connection until it has announced a device — Go
  sends only to an announced device, and the carrier holds back from a
  connection that has not announced — so a viewer that cannot play never
  sees the prefix.

- **SD9 — With no device, a playing sink free-runs.** ✓ No viewer, a
  suspended context and a passive-only session are the same state to a
  stream: it is `sink.Null`, which advances the position by wall clock and
  reads no frames. `SinkI` says the position advances while playing, and a
  playhead that stops because a tab closed would break that. An announcement
  moves the stream onto an engine at its position, state, rate and volume,
  and a device that goes away moves it back. The state is readable
  (`audioout.Audible`), so an app says that nothing is heard, and why.

### Milestones

- **M1 — Engine and opener.** ✓ SD1–SD3: the engine out of the pulse sink, the
  service with the pulse device behind it, the capability subject, the apps
  on the opener. No change in what is heard on a desktop.
- **M2 — The tab.** ✓ SD4's tab carrier, SD5–SD7, SD9.
- **M3 — The headless host.** ✓ SD4's headless carrier, SD8.
- **M4 — A trial.** ✓ Underrun rate against the target buffer, and the
  distance between `Position` and the device, on both carriers:
  [audio-lane-playout](../trials/audio-lane-playout/README.md), whose §0 is
  the only place to quote its figures from. Its first run is loopback in a
  muted headless browser, the easiest condition the lane will meet; it gave
  no reason to move the 150 ms default and is not evidence for a lower one.
  The distance to what is *heard* needs a real output device and stays
  unmeasured.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Carrier wire (`boxer/imzero2/v1`, one-byte prefix) | added: `0x05`, the audio lane, both directions | the viewer page; SD8's rule that a viewer which announced no device is sent none, since the native Windows viewer (ADR-0243) rejects a prefix it does not know |
| Audio lane message format (`lanesink`'s `wire.go`) | added: open, chunk, flush, close, want toward the device; announcement and played report toward Go | the Go codec and the page's reader, held together by the fixture in the verification plan |
| Capability subjects (ADR-0026 §SD3) | added: `audio.out.open` | manifests of the apps that play sound; the tab report |
| `hostboot.Services`, `tabhost.Services` | added: an audio-output field each; `hostboot.Options` names the device | `AllServices()` |
| Environment-variable registry (ADR-0009) | added: `IMZERO2_AUDIO_LANE_TO_DEVICE`, `IMZERO2_AUDIO_LANE_FROM_DEVICE`, which the host sets for its client; `BOXER_AUDIO_LANE_TARGET`; `BOXER_AUDIO_LANE_TRACE` and `BOXER_WAVEFORM_DEMO_AUTOPLAY`, which the trial uses | `doc/env-vars.md` regenerates |
| Reactor ABI of the tab module | added: the lane's host imports and the pump export | `worker.mjs`, `bridge.mjs` |
| Exported Go API under `public/science/audio/sink` | added: `playout`, the engine; `lanesink`, the lane's device and codec; `pulsesink.Open` stays | nothing |
| Exported Go API under `public/keelson/runtime` | added: `audioout`, the service, the opener and the device interface | nothing |
| Exported Go API under `public/thestack/imzero2` | added: `hostaudio`, which joins the device and the application's pipes; `browserhost.AudioLane` and `MountOn` | nothing |
| `rust/imzero2` headless carrier | added: the relay (`audiolane`) and prefix `0x05` | nothing: the browser host's digest did not move (built before and after, 2026-11-14) |

## Alternatives

- **O1 — In-band FFFI2.** Rejected on C2 and C5: an IDL change for something
  that is not a widget, and sound that stops when a pass runs long.
- **O3 — A Go-side endpoint.** Rejected on C1 and C3: a second listener to
  bind, proxy and authenticate, and nothing for the tab, which has no
  server.
- **O4 — A server-side device and an encoder.** Rejected on C6 and C3: it
  needs an audio server on the serving machine, which the appliance image
  leaves out by design (ADR-0208 §Consequences), and has no tab form. Its
  encoder half survives as a deferral: ADR-0024's encoder-as-subprocess
  practice applies to an Opus lane over O2.
- **O5 — A media element.** Rejected on C4: a second playback engine whose
  rate, seek and position would have to be kept equal to Go's; the whole
  recording leaves the serving machine where only the played part needs to;
  and a source that is rows in a database, or synthesised, has no file to
  send. The browser's decoders remain of interest for the tab, which has no
  `ffmpeg`; that is a decoding question and not this one.
- **An `AudioWorklet` as the page's device.** A ring fed on the audio thread
  gives a shorter buffer than scheduled sources do. Not taken first because
  it needs a secure context and a module of its own; it is the recorded
  upgrade if M4 shows the scheduled player's buffer is too long.
- **The page resamples.** Opening each stream at its source's rate and
  letting the context resample per buffer is what the pulse sink asks of its
  server. Rejected because each scheduled buffer would be resampled on its
  own, with no history across the boundary between two of them.
- **Wall-clock push.** Go sends at the stream's rate and the page absorbs
  the difference. Rejected: the two clocks differ, so the page's buffer
  grows or empties over a long recording and something has to drop or
  stretch samples.
- **A process-wide opener with no capability.** The shape the tab's HTTP
  transport has. Rejected as the end state because the runtime could then
  not tell which app is making sound; the bus request costs one round trip
  per open.

## Consequences

### Positive

- An app that plays sound is written once and is audible on the desktop, at
  a remote viewer and in a tab.
- The single WebSocket, its proxy story and its authentication boundary are
  unchanged.
- A seek on the lane drops the device's buffer, which the pulse sink cannot
  do without restarting its stream (ADR-0208, 2026-11-13 update).
- The runtime learns which apps make sound, from manifests and from the
  requests themselves.

### Negative

- Latency is a buffer chosen to cover the network on one carrier and the
  longest export on the other, and scheduled sources add the page's own
  scheduling margin. The default target is 150 ms against the pulse sink's
  60 ms; what it comes to on a real network and a real device is not
  measured (the trial's later milestones).
- A pause is heard up to one target late, and the position is what the
  page's context has consumed, which runs ahead of what is heard by the
  browser's own output latency.
- In the tab the engine reads its source inside an export. A source whose
  read waits on something only another export can deliver stalls the tab,
  so a tab's sources must answer from memory.
- The linear resampler now runs for every source whose rate is not the
  device's, where the pulse sink left that conversion to its server.
  ADR-0208 recorded the resampler's quality as a deferred concern; this
  gives it a second reason to be revisited.
- Sixteen-bit samples without compression are about 1.5 Mbit/s for 48 kHz
  stereo on the headless carrier.
- A background tab's timers may be slowed by the browser, and the page's
  reports with them. The target grows to cover slow reports (SD5), which
  the Go tests hold against a scripted device; no browser run has shown it
  yet.
- Two carriers and a page player to keep in step, with the browser half
  outside the default test lane.

### Neutral

- The Rust host holds no audio state; a host restart or a viewer reconnect
  is a device that went away and came back.
- Passive viewers and a second audible device are a session-model question
  the lane leaves open, not one it forecloses.

## Migration — Tier 1

- **Breaks.** Nothing: every surface above is additive, and
  `pulsesink.Open` keeps working for a caller without a runtime.
- **Path.** An app replaces its direct open with the opener and adds the
  capability subject to its manifest. An app that does neither keeps
  today's behaviour.
- **Regeneration.** `doc/env-vars.md`; the package-props table for the new
  packages; `tabhost/browserhost.sum` if the browser host's digest moves.
- **Old shape.** Kept: the direct open is how a tool without a runtime
  plays sound.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the engine over a scripted device — the
  position never goes back, the source is read forwards at every rate, the
  frames in flight never exceed the target, a missing device free-runs — and
  the transport invariants the null sink is held to. `audioout`'s tests hold
  the opener: a grant per open, the null sink without the subject, without
  a service and over a device that does not open. A fixture of encoded lane
  messages, kept in the viewer page's audio suite under `viewer/tests`, is
  read by the Go codec's test too, so a change to one reader without the
  other fails. `lanesink`'s tests play a scripted remote device against the
  flow control, the free-run and the switch between them; the carrier's and
  the two pipe ends' tests hold the relay. Integration lane: the pulse
  device, on its own and through the service. Opt-in browser lane: the
  viewer page's suite, which needs Chromium.
- **What would fail.** A lane message whose layout changed on one side
  fails the fixture in the other's suite; an engine that sends ahead of the
  device's reports fails the in-flight bound; an opener that hands a device
  to an app whose manifest does not declare the subject fails `audioout`'s
  test of the refusal.
- **Gap.** That sound is heard, and that it is the right sound, is checked
  by ear on both carriers, as in ADR-0208. The page's suite runs against a
  fake `AudioContext`; a real one is exercised only by M4's trial and by
  hand. A takeover between two real viewers is covered on each side of the
  wire and not across it.

## Deferred

- **Capture** — a microphone is the lane in the other direction plus a
  permission prompt, and its own decision.
- **Passive viewers** — a copy of the stream without flow control, when a
  second listener is wanted.
- **A compressed lane** — Opus from an `ffmpeg` subprocess, decoded by the
  page, selected through the format field of SD6.
- **The native Windows viewer** (ADR-0243) playing prefix `0x05`.
- **Compartments** (ADR-0207, proposed) — how a granted stream's samples
  cross a compartment boundary; the grant already crosses as a subject.
- **A hold message** — so that a pause stops the page at once instead of
  letting it play out what it holds.
- **An `AudioWorklet` player, a better resampler, more than two channels.**

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0208](./0208-audio-waveform-player-widget.md) — the audio subtree, `sink.SinkI` and the seam this decision uses; it takes a dated update when M1 lands.
- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the single WebSocket and its prefixes.
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) — the authentication boundary the lane stays inside.
- [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md), [ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md) — the active viewer and the scope SD8 follows.
- [ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md), [ADR-0278](./0278-tab-mode-for-downstream-apps.md) — the tab, its reactor and its services struct.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — capability subjects.
- [ADR-0211](./0211-hostboot-runtime-bootstrap.md) — the native host's services struct.
- [ADR-0363](./0363-porting-from-an-end-user-egui-application.md) (proposed) — the wake channel whose shape the headless carrier's pipe pair takes.
- [audio-lane-playout](../trials/audio-lane-playout/README.md) — the trial; quote figures from its §0 only.
- [app-audio-output](../howto/app-audio-output.md) — the app's side.
