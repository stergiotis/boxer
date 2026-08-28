---
type: adr
status: accepted
date: 2026-08-28
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-28
---

# ADR-0208: An audio waveform player for imzero2 — peaks pyramid on the painter lane, playback in pure Go

## Context

There is a recurring need to look at an audio recording the way
wavesurfer.js lets a browser page look at one: the waveform drawn across the
width of the pane, a playhead that moves while the file plays, click-to-seek,
zoom from the whole file down to individual samples, and annotation layers
over the same time axis — the output of a voice-activity detector (VAD) is
the canonical example: boolean segments, a probability curve, speaker turns,
markers. Producing such data is not this widget's job; showing any of it,
aligned to the samples, is. The recordings
range from a three-minute track to a twelve-hour capture; the long end is not
an edge case but the reason the tool is wanted, because that is where
scrolling through the file by ear is hopeless.

The repository has no audio code at all: no decoder, no PCM type, no output
device access, no VAD. [ADR-0058](./0058-imzero2-scrolling-texture-widget.md)
named audio spectrograms as the motivating use of `scrollingTexture` and
explicitly left "where the samples come from — Rust (rodio/cpal) or Go" as an
orthogonal, undecided question. This ADR decides it.

Three constraints shape the design more than any feature list does.

**The long file rules out drawing samples.** Twelve hours at 48 kHz is 2.07 G
samples per channel — 8 GB as `float32`, and orders of magnitude more than any
draw path here can take per frame. [ADR-0149](./0149-implot-core-port-painter-lane.md)
records that the implot port has no decimation and that "very large series
need Go-side decimation (min-max/LTTB)"; its §SD5 sanctions routing dense
content to a texture when the vector path is too dense. wavesurfer answers the
same problem with pre-computed *peaks* — per-bin min/max — and lets the caller
supply them so a long file never has to be decoded in the page. A waveform
widget here is therefore mostly a data-structure decision (a multi-resolution
min/max pyramid, built once, cached) with a thin drawing pass over it.

**Sound has to reach a device, and the repository compiles without cgo.**
[ADR-0003](./0003-h3-wasm-bridge.md) fixed the no-cgo stance and made
Rust→wasm32→wazero the route for native libraries; wasm cannot open an audio
device, so that route covers decoding at most, never playback. The imzero2
host's Cargo workspace denies `unsafe_code`, justifies every crate in a
comment, and keeps a `fast_alloc` feature specifically so a C-toolchain
dependency can be switched off for the musl appliance graph
([ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md) M6,
[ADR-0206](./0206-gokrazy-appliance-image.md)). Every Rust audio-output crate
on Linux (`cpal`, `alsa`, `pipewire-rs`) links a C library. On the Go side there is a
way through: `jfreymuth/pulse` (MIT, no dependencies) speaks the PulseAudio
native protocol in pure Go, and PipeWire's pulse compatibility server is what
a current Linux desktop runs. `ebitengine/oto` builds on the same package
and adds a `purego`-loaded `libasound.so.2` fallback plus macOS and Windows
— but only in its v3.5 pre-releases; the released v3.4 still links ALSA
through cgo on Linux. Both were built with `CGO_ENABLED=0` and played
through a PipeWire desktop's pulse socket on 2026-08-28; the stable one is
the one to depend on.

**Widgets live in Go on the painter lane.** [ADR-0149](./0149-implot-core-port-painter-lane.md)
and [ADR-0204](./0204-leaflet-map-core-port.md) both chose to port a core into
Go over binding a Rust crate, and [ADR-0176](./0176-native-tree-widget.md)
retired a binding in favour of a Go widget. Adding an opcode makes this a
Tier 1 change of the IDL ([CODINGSTANDARDS § What triggers an ADR](../../CODINGSTANDARDS.md#what-triggers-an-adr));
staying in Go keeps it a Tier 2 widget decision. The painter lane already has
the shapes a waveform needs — `paintRectsFilled` (one opcode for a screen's
worth of min/max columns, ADR-0149 §SD3), `paintPolyline`, `paintClipPush`,
`paintSenseRegion` — and the input recipe is settled: per-canvas wheel
([ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md)), per-canvas
drag-stable cursor, focus-scoped keys ([ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md)),
sense region emitted last (ADR-0204 §SD6). The `timeline` widget
([ADR-0043](./0043-imzero2-timeline-widget.md)) already carries a playhead, an
anchored zoom, a pan, a range brush on its own strip (§SD16), interval and
point lanes with a LOD index — everything an annotation layer needs except
a time axis that is not the calendar. Its time model is `int64`
milliseconds since the epoch with a calendar tick ladder; audio wants
offsets from the start of a file, and at sample-level zoom a finer unit than
a millisecond.

### What "parity with wavesurfer" means here

wavesurfer.js v7's core is a waveform (`waveColor` / `progressColor`, bars or
continuous, `normalize`, `splitChannels`), a transport (`play`, `pause`,
`seekTo`, `setPlaybackRate`, `zoom` as `minPxPerSec`, `autoScroll`,
`autoCenter`, `dragToSeek`), pre-decoded `peaks` as an input, and plugins:
Regions (drag, resize, loop), Timeline, Minimap, Hover, Zoom, Envelope,
Spectrogram, Record. This ADR takes the core, Regions, Timeline, Minimap,
Hover and Zoom as the target. Spectrogram, Envelope and Record are recorded
as deferrals. The spectrogram is the next lane to be worked out and gets its
own ADR: the FFT is available (gonum's `dsp/fourier` is already a
dependency), but how a twelve-hour STFT is computed, cached and tiled, and
whether it draws through `heatmapscroll`'s live ring or a `paintImage` tile
pyramid, are decisions of their own. This ADR reserves the seams it will
need (SD12) and decides nothing else about it. Recording and envelopes are
different products.

## Design space (QOC)

**Question.** Where do audio decoding and audio output live, given no cgo, a
thin Rust host, and files that may not fit in memory? (How annotations are
drawn is a second, smaller question, answered in prose under SD8.)

**Options.**

- **O1 — Rust host owns audio.** `symphonia` decodes, `cpal` plays, new
  opcodes carry load/play/pause/seek, a register reports the clock.
- **O2 — Go owns audio, pure Go.** `jfreymuth/pulse` for output (the
  PulseAudio native protocol; `oto` later for other platforms); decoding in
  Go — a native RIFF/WAVE and RF64 reader first, other formats through O3
  or O4.
- **O3 — External processes through `extbin`.** `ffmpeg` decodes to raw
  `f32le` on a pipe; a sink process (`pw-play`, `aplay`, `ffplay`) plays
  from a pipe. [ADR-0118](./0118-extbin-external-process-chokepoint.md)
  already governs the spawn.
- **O4 — Decoder as a wasm bridge.** A `symphonia` shim compiled to
  wasm32 and run by wazero, the ADR-0003 pattern; output still needs O2 or O3.

**Criteria.**

- **C1 — Toolchain neutrality.** Builds with no cgo and no C headers;
  degrades rather than fails where no device library exists (appliance).
- **C2 — Lane discipline.** No new opcode, no Rust-side state, no crate
  added to the host — the widget stays a Tier 2 decision.
- **C3 — Format coverage.** WAV/RF64, FLAC, MP3, Opus/Ogg, AAC/M4A out of
  the box.
- **C4 — Clock and seek.** Playhead within a frame of the audible position;
  a seek audible within ~100 ms; no drift over an hour.
- **C5 — Runtime dependencies.** What has to be installed on the machine
  beyond the binary, and whether the airgap bundle already carries it.
- **C6 — Headless and tests.** Works with no device and no external binary
  in the default `go test` lane.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | ++ |
| C2 | −− | ++ | ++ | +  |
| C3 | ++ | −  | ++ | +  |
| C4 | ++ | +  | −  | n/a |
| C5 | +  | +  | −  | ++ |
| C6 | −  | ++ | −  | ++ |

O1 fails the two criteria the repository weights most (C1, C2) for a gain
in coverage that O3 provides at no toolchain cost. O2 is the only output route
that is both pure Go and testable without a device. O3's weakness is
*output*: a pipe-fed sink gives a coarse clock (pipe and device buffers,
tens to hundreds of milliseconds, not queryable) and a seek that means
restarting a process — acceptable for decoding, where the same latency
buys every format ffmpeg knows and a bundled static binary already exists in
the tree. O4 is the right long-term decoder for airgapped targets without
ffmpeg and is deferred, not rejected: it costs a new wasm artifact, a parity
gate and a batch-shaped ABI, and nothing in the first milestones needs it.

## Decision

We will build the player as a **pure-Go widget on the painter lane** over a
new **`public/science/audio`** subtree that owns everything about sound
that is not drawing. **Output is O2** (`ebitengine/oto`); **decoding is O2
for WAV/RF64 and O3 (`ffmpeg` through `extbin`) for everything else**, behind
one `Source` interface so the widget and the peaks builder never know which.
O4 is the recorded fallback decoder. No opcode, no Rust change.

### Subsidiary design decisions

- **SD1 — Name and homes.** The widget is
  `public/thestack/imzero2/egui2/widgets/waveform`, entry type `Player`.
  Audio lives under `public/science/audio/`: `pcm` (sample formats, the
  `Source` interface, a resampler), `wavfile` (RIFF/WAVE + RF64/BW64 reader),
  `decode` (sniff → `wavfile` or `ffmpeg`), `peaks` (the pyramid and its
  cache file), `sink` (device output and a null clock), and `track` (one
  open file: source + peaks + sink + window cache, the "media element"). No
  analysis lives here — a detector that produces annotations is a separate
  package with its own decision when one is wanted. The widget imports
  `track` and nothing below it; `track` imports nothing from imzero2. Anything that wants the waveform of a file without a UI —
  a batch job, a test — uses `peaks` directly.

- **SD2 — The waveform is a min/max pyramid, not samples.** Level 0 holds
  per-bin `int8` min and max for a fixed base bin (default 256 frames, so
  ~5 ms at 48 kHz); each higher level halves the bin count by combining
  pairs. A twelve-hour stereo file is ~26 MB at level 0 and ~52 MB for the
  whole pyramid — held in memory, no mmap (the repository has no Go mmap
  user, and 52 MB does not justify becoming the first). Eight-bit
  quantisation is what wavesurfer exports by default and is invisible at
  the vertical resolution of a pane; RMS and per-level loudness are deferred
  until a consumer asks. A render picks the level whose bin is ≤ one screen
  column and emits one `paintRectsFilled` per channel with per-column colours
  (`progressColor` up to the playhead, `waveColor` beyond), so a screen costs
  one opcode per channel regardless of file length. Bars mode
  (`barWidth`/`barGap`) is the same primitive with fewer rects.

- **SD3 — Below the base bin, samples are fetched on demand.** Zooming past
  level 0 asks `track` for a window of raw frames. Windows are decoded off
  the frame thread and cached (byte-bounded LRU); the frame that asks gets
  `ok=false`, draws level 0 in the meantime, and keeps the loop hot — the
  portolan tile pattern (ADR-0204 §SD4). At ≥ 1 sample per column the window
  is min/max-reduced to columns and drawn as rects; below 1 sample per column
  it is a `paintPolyline` per channel with `paintMarkers` at the samples.
  There is no third drawing mode.

- **SD4 — Built progressively in the background, cached on disk.** `track` learns the frame count first (the WAV header;
  `ffprobe` for the rest), preallocates every level, and a builder goroutine
  fills level 0 in sequential chunks, folding each chunk into the higher
  levels as it lands; it publishes the built prefix as one atomic frame
  count. Readers on the frame thread read `[0, built)` without a lock —
  the arrays never reallocate — and draw the unbuilt remainder as a
  placeholder with the build's progress (`jobprogress`). A finished pyramid
  is written to `$BOXER_AUDIO_PEAKS_CACHE_DIR` (default under the user
  cache directory) keyed by a blake3 of the file's size, modification time
  and head/tail bytes, so the second open of a twelve-hour file is
  instantaneous. `normalize` applies the pyramid's global maximum only once
  the build is complete; until then the gain is 1.

- **SD5 — One `Source` interface, two decoders, sniffed format.** `pcm.Source` is `Info() (Format, frames)` plus positioned
  reads of interleaved `float32` frames. `wavfile` implements it for
  PCM 8/16/24/32, IEEE float, `WAVE_FORMAT_EXTENSIBLE`, and RF64 — a
  twelve-hour stereo 16-bit file is 8.3 GB, past the 4 GB RIFF limit, so
  RF64 is not optional. `decode.Open` sniffs the header and otherwise runs
  `ffmpeg -ss <t> -i <file> -f f32le -ac <n> -ar <rate> -` through two new
  `extbin` declarations (`Ffmpeg`, `Ffprobe`, honouring `IMZERO2_FFMPEG_BIN`
  where set); positioned reads restart the process at the requested offset,
  which is why SD3 caches windows. The peaks build is one sequential pass
  in either case.

- **SD6 — Pulse-protocol output; position from frames delivered.** `sink` defines the interface — play,
  pause, seek, rate, volume, `Position() (frame, playing)` — and its default
  implementation is a `jfreymuth/pulse` playback stream at a fixed device
  rate, fed by a pull callback; files at another rate are resampled in
  `pcm`. The callback counts the frames it hands to the server, and the
  audible position is that count minus the stream's buffer, interpolated by
  wall clock between frames so the playhead does not judder at the poll
  rate; the protocol's latency query is the correction if the estimate
  drifts. A seek stops the stream, moves the source and restarts it, which
  is what bounds seek latency to the configured buffer. Playback rate is a
  resampling ratio (pitch follows rate — wavesurfer's `preservePitch:
  false`); pitch-preserving rate is deferred. `sink.Null` implements the
  same interface with a settable clock, so tests and headless scenes play
  without a device, and a host with no pulse socket gets the null sink and
  a visible "no output device" state rather than a failed open. The sink is
  also the seam for authority: `track` is handed a `sink.Sink` and never
  opens a device itself, so an audio-output capability brokered by the
  keelson runtime — the shape of [ADR-0026](./0026-app-runtime-and-capability-subjects.md)
  §SD7's file-dialog powerbox, and what a compartment boundary
  ([ADR-0207](./0207-keelson-trust-boundaries-compartments.md), proposed)
  would enforce — can be introduced by giving out a different `Sink`
  without touching the player. That capability is a keelson decision, not
  this one. An `oto`-backed sink for macOS, Windows and ALSA-only hosts is
  the recorded follow-up once oto v3.5 is released.

- **SD7 — Interaction is wavesurfer's, else timeline's.** Click seeks. Drag pans (wavesurfer's `dragToSeek` is an
  option, off by default). Wheel scrolls, Ctrl+wheel and pinch zoom about
  the hovered time (per-canvas R23, ADR-0140; never the global wheel).
  Shift+drag creates a region when the host has enabled region editing;
  a region's body drags and its edges resize; a click selects it. Space,
  arrows, Home/End and `+`/`-` arrive through a key-capturing Frame
  (ADR-0177). `autoScroll` pages the view when the playhead leaves it;
  `autoCenter` keeps it centred; both are off while the user is dragging.
  Drag positions are press origin plus offset, never summed deltas
  (ADR-0204 §SD6's lesson). The sense region is emitted last so it wins the
  hit test over the canvas.

- **SD8 — Annotation lanes are `timeline`, extended.** Interval and point annotations — VAD segments, speaker turns,
  markers, editable regions — are drawn by the `timeline` widget stacked
  under the waveform and locked to its view range, so the same LOD index,
  lane packer, selection and brush serve both. That requires two
  extensions to `timeline`, recorded against ADR-0043 when M5 reaches
  them: a **relative axis** (the `int64` is an offset from a zero rather
  than from the epoch; ticks from a duration ladder — `ms … s … min … h` —
  with duration labels, the calendar path untouched when an epoch is set)
  and a **finer time unit** than the millisecond, chosen per instance, so a
  lane boundary does not quantise visibly when the pane spans a few
  milliseconds. What `timeline` does not do stays in the player: a
  **`Curve`** layer (a scalar over time — a VAD probability, a loudness
  envelope — as a polyline in its own strip) and region editing by drag and
  edge-resize on the waveform canvas, reported as `Events` the host applies
  to its own state (the `tree.State` contract, ADR-0176). Layers are
  host-owned and sorted by start; the visible window is a binary search, so
  ten thousand segments over twelve hours cost what the visible ones cost.
  Colour is never the only encoding of a region
  ([ADR-0031](./0031-imzero2-design-system-color.md) §SD5): every region has
  a label or a hover readout.

- **SD9 — Two time bases, one position.** The player's position is a frame
  index; everything shown is derived from it through a `TimeBase` of sample
  rate plus an optional **epoch** — the wall-clock instant of frame 0. With
  no epoch (a track, a clip) every axis, readout and annotation is a
  duration from the start. With an epoch (a capture that began at a known
  time) the same widget shows wall-clock time, accepts annotations given as
  instants, and `timeline` runs in its existing calendar mode; the user can
  flip between the two readings without the view moving. Annotations
  carry which base they were given in, so a segment list produced against
  the file and one produced against a clock line up on the same lane.

- **SD10 — Chrome is composed, not built in.** The time ruler is `axisruler`
  over the same duration ladder SD8 gives `timeline`, or the calendar ladder
  when an epoch is set (SD9). The minimap is the pyramid's
  top level drawn on its **own** canvas with a brush over it, copied from
  timeline's §SD16 — a brush on the main canvas fights the pan gesture. The
  hover readout, transport buttons and rate/volume controls are ordinary
  widgets the demo composes around `Player`; the package exposes the pieces
  and a `Player.RenderDefault` that assembles them.

- **SD11 — Repaint only while something moves.** The player requests a
  repaint at 60 Hz while playing, animating a zoom, or waiting for a build
  chunk or a window; idle it draws once and lets the host go reactive.
  Under a host-skippable region it composes with `lazypane` like any other
  painter widget.

- **SD12 — Seams reserved for a raster lane.** Three commitments so the
  spectrogram ADR adds a lane rather than reshaping the player. The layer
  model has a **raster lane**: a strip the player positions and clips like
  any lane, whose content is supplied by a `LaneRenderer` that receives the
  frame's view range, the pixel rect and the `TimeBase`, and paints —
  nothing in the player knows what it paints. `track`'s **window cache**
  (SD3) is the one source of raw frames for every derived product, so a
  spectrogram computed for the visible span and a sample-level waveform
  read the same bytes once. **Derived products cache separately**: the
  peaks file (SD4) holds peaks and nothing else; a spectrogram's cache is
  its own file under the same directory and the same identity key, so
  neither invalidates the other and the peaks format never grows a
  variant.

### Milestones

- **M1 — Audio core.** `pcm`, `wavfile` (incl. RF64), `peaks` (build,
  query, cache file), `track` (synchronous build), `sink.Null`. Property
  tests over the pyramid; a procedural twelve-hour `Source` for a benchmark.
- **M2 — Widget v1.** Waveform (rects and polyline paths), zoom, scroll,
  click-to-seek, drag-to-pan, playhead, ruler, hover readout; a demo over a
  synthetic in-memory track; a headless scene.
- **M3 — Playback.** `pulse` sink, transport, autoscroll/autocenter, keys,
  rate and volume.
- **M4 — Long files.** Progressive background build with progress, window
  cache for deep zoom, `ffmpeg`/`ffprobe` decoding, the cache-dir variable.
- **M5 — Layers and chrome.** The `timeline` extensions (relative axis,
  finer unit), lanes locked to the waveform, editable regions, curves,
  minimap, the epoch toggle; the demo's annotations are synthetic segments
  and a synthetic probability curve shaped like a detector's output.

### Deferrals, recorded

- **Spectrogram lane** — the next decision, as its own ADR referencing
  SD12: STFT parameters, frequency scale, dynamic range and colormap, the
  long-file computation and cache, and the drawing substrate.
- **Any detector** — VAD, diarisation, onset detection — is a producer of
  annotations and a separate decision; if a model runtime is wanted without
  cgo, a wasm bridge (ADR-0003 pattern) is the route.
- **An audio-output capability in keelson** — the `Sink` seam in SD6 is
  where it attaches; the capability itself belongs to the app-runtime ADRs.
- **`symphonia`-in-wasm decoder (O4)** — when an airgapped or appliance
  target needs compressed formats without ffmpeg.
- **RMS / loudness levels, pitch-preserving rate, recording, envelopes,
  mmap-backed pyramids** — until a consumer asks.

## Surfaces — Tier 1

The decision is Tier 2 (a widget and a leaf library), but it adds members to
two Tier 1 registries; adding a member is not a decision, and the table is
here so the additions are found.

| Surface | Change | Moves with it |
| --- | --- | --- |
| `extbin` program registry | added: `Ffmpeg`, `Ffprobe` | the airgap bundle's binary list; `doc/env-vars.md` regenerates if an override variable is declared |
| Environment-variable registry (ADR-0009) | added: `BOXER_AUDIO_PEAKS_CACHE_DIR` | `doc/env-vars.md` regenerates |
| `go.mod` | added: `github.com/jfreymuth/pulse` (MIT, no transitive dependencies) | license gate SBOM (ADR-0004) |

## Alternatives

- **Rust host owns audio (O1).** Every Linux output crate links a C library,
  which the appliance graph forbids; the widget would become a Tier 1 IDL
  change and add state to a host the architecture keeps thin. Rejected per
  the QOC matrix.
- **Pipe-fed sink process (O3 for output).** No queryable clock and a
  process restart per seek. Kept for decoding, rejected for output.
- **Draw with `implot.Line`.** No decimation exists there and ADR-0149
  says a large series must be decimated in Go first — which is the pyramid.
  Once the pyramid exists, `paintRectsFilled` is the cheaper primitive.
- **Raster the waveform into a texture (`paintImage`).** ADR-0149 §SD5's
  route for dense content, and the recorded fallback if column counts ever
  exceed the rects budget (they do not: a 4K pane is ~4 000 rects per
  channel). Rects keep the played/unplayed split and the hover column free.
- **Draw annotation lanes in the player, copying `timeline`'s shapes.**
  Rejected: it would leave two lane engines, two LOD indexes and two
  selection models a few packages apart; extending `timeline` with a
  relative axis is the smaller change and improves it for every other
  relative-time caller. The waveform itself does stay outside `timeline` —
  its LOD is min/max of a signal, not a count of events per bucket.
- **Store peaks in ClickHouse via a leeway record store.** Nothing queries
  them but the widget; a sidecar cache file is the whole requirement. The
  facts route stays open if a corpus of recordings is ever indexed.
- **Hold `float32` peaks.** Four times the memory of `int8` for precision no
  pane can show.

## Consequences

### Positive

- A twelve-hour file opens in the time it takes to read its header, draws
  what has been built while the rest builds, and opens instantly the second
  time.
- One opcode per channel per frame, whatever the file length; no new
  opcode, no Rust change, no crate added to the host.
- Playback works on any Linux desktop with PipeWire or PulseAudio, with no
  cgo and no build-time headers, for one small dependency; without a pulse
  socket it degrades to a silent clock, not a failed open.
- Every format ffmpeg reads is covered, using a binary the tree already
  bundles.
- The audio library is usable without imzero2 — a peaks file can be built
  in a batch job.
- `timeline` gains a relative-time axis every other duration-shaped caller
  can use; the player reads captures with a known start in wall-clock time
  without a second widget.

### Negative

- Two decoders with different seek costs; a deep zoom into an MP3 waits on
  a process restart the first time, and only the window cache hides it.
- Output is Linux-with-pulse only until the `oto` follow-up; the appliance
  image has no pulse server and stays silent by design.
- One device stream at a fixed rate; every open file resamples to it, and
  the resampler's quality is a deferred concern.
- ffmpeg is a runtime dependency for compressed formats; targets without it
  see WAV only until O4 lands.
- The pyramid's memory is proportional to file length; a twelve-hour stereo
  file costs ~52 MB resident per open track.
- The player's lanes depend on `timeline` changes that its accepted ADR must
  take as dated updates; M5 cannot land before they do.

### Neutral

- Eight-bit peaks are interchangeable with wavesurfer's exported peaks; a
  peaks file could be produced by either side.
- The one-frame lag of every canvas register applies as it does to every
  painter widget; at 60 Hz it is below what a listener notices.

## Migration — Tier 1

Nothing to migrate: every surface change is additive.

## Verification plan — Tier 1

- **Lane.** Default `go test`: property tests (`rapid`) over the pyramid —
  every higher-level bin contains its two children, level 0 contains the
  samples it summarises, quantisation is monotone; `wavfile` round-trips
  through a Go-written WAV and an RF64 header; the duration tick ladder is
  golden-tested; a `timeline` in relative mode and the waveform over the
  same view range map the same instant to the same column. A benchmark over a procedural twelve-hour `Source` bounds
  build time and resident size. A headless scene drives the demo with the
  null sink: seek by click, zoom by wheel, and assert the readout labels.
- **Integration lane.** `ffmpeg` decoding and the `oto` sink, gated on the
  binary and a pulse socket respectively.
- **What would fail.** A build that reallocates a level breaks the
  lock-free read and shows as a race under `-race`; a render that emits
  more than one `paintRectsFilled` per channel shows in the scene's opcode
  count; a pyramid whose child bins escape their parent fails the property.
- **Gap.** Audible correctness — that what plays is what the file holds —
  is checked by ear, and the clock-versus-playhead alignment is a manual
  measurement. Recorded as such.

## Status

Accepted 2026-08-28.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-08-28 — SD2's memory figure was low; M1's audio core is built

SD2 estimated a twelve-hour stereo pyramid at ~26 MB for level 0 and ~52 MB
in all. The arithmetic was wrong by a factor of 1.25: 12 h at 48 kHz is
2,073,600,000 frames, 8,100,000 level-0 bins at a base bin of 256, and at one
byte of min plus one of max per bin per channel that is 32.4 MB at level 0 and
64.8 MB (61.8 MiB) for all 24 levels. The `peaks` package's memory-accounting
test asserts the exact byte count. The decision stands — the pyramid is held
in memory, no mmap — the cost is a fifth higher than stated.

M1 shipped as `public/science/audio/{pcm,wavfile,peaks,sink,track}` with
`pcm/pcmtest` as the shared source-contract check. The
twelve-hour build benchmark over a procedural source runs in about 80 s on a
mobile CPU, of which roughly a quarter is the pyramid fold itself (~94 M
frames/s) and the rest is generating the synthetic signal — a real decoder's
read cost is what will dominate, which is the reason SD4 builds in the
background.

## References

- [ADR-0003](./0003-h3-wasm-bridge.md) — the no-cgo stance and the wasm route for native libraries.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the capability broker an audio-output capability would follow.
- [ADR-0043](./0043-imzero2-timeline-widget.md) — playhead, anchored zoom, brush strip (§SD16), lanes and LOD index; the widget SD8 extends.
- [ADR-0058](./0058-imzero2-scrolling-texture-widget.md) — left the audio-engine question open.
- [ADR-0118](./0118-extbin-external-process-chokepoint.md) — the external-binary chokepoint ffmpeg goes through.
- [ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md), [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — per-canvas wheel and focus-scoped keys.
- [ADR-0149](./0149-implot-core-port-painter-lane.md) — painter-lane porting, `paintRectsFilled`, dense-raster routing.
- [ADR-0204](./0204-leaflet-map-core-port.md) — the sense-region input recipe and the background-loader pattern.
- [ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md), [ADR-0206](./0206-gokrazy-appliance-image.md) — why a C-library dependency is a liability.
- wavesurfer.js v7 — <https://wavesurfer.xyz/> — the feature set this widget targets.
- `jfreymuth/pulse` — <https://github.com/jfreymuth/pulse> — the PulseAudio native protocol in pure Go.
- `ebitengine/oto` — <https://github.com/ebitengine/oto> — the multi-platform follow-up; pure Go on Linux from v3.5.
