---
type: adr
status: accepted
date: 2026-06-16
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-16
---

# ADR-0088: ImZero2 runtime-selectable codec pipeline and viewer decode capabilities as first-class Go state

## Context

[ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) shipped the headless pixel-streaming head: the Rust host renders egui to an offscreen BGRA texture, an `ffmpeg` subprocess (`rust/imzero2/src/imzero2/encoderpipe.rs`, `EncoderSink`) encodes it to H.264, and a single-file browser viewer (`rust/imzero2/src/imzero2/viewer/index.html`) decodes via WebCodecs. Two facts about the shipped state set up this ADR:

- **The encoder is fixed at process start.** `IMZERO2_HEADLESS_ENCODER_ARGS` (registered in `imzero2env`, [ADR-0009](./0009-imzero2-env-var-registry.md)) is read once and whitespace-split into the `ffmpeg` argv. Codec is H.264 end-to-end; the viewer derives its `avc1.…` `VideoDecoder` string from the in-band SPS and performs **no** capability detection (`VideoDecoder.isConfigSupported` and `navigator.mediaCapabilities` are unused).
- **Two stream properties are nonetheless already runtime-switchable**, by tearing the encoder down and rebuilding it with a fresh IDR and a re-announced `SessionHello`: **cadence** (`SetCadence`, [ADR-0062](./0062-imzero2-render-cadence.md)) and **geometry** (`ViewportResize`). The resize path (`input.proto:115-138`) establishes the exact mechanism — clamp, rebuild target+encoder, re-announce `SessionHello`, viewer drops its decoder and rejoins at the next key frame — with a *hello-before-IDR* ordering guarantee. The "short downtime while switching is acceptable" requirement is satisfied by this already-shipped shape.

The request has two parts. **(1) Make the video coding pipeline choosable at runtime.** **(2) Make the connected web client's video-playback capabilities first-class state in the imzero2 Go part, so GUI controls can be built to change the output.**

The structural constraint that shapes everything: there is no direct browser↔Go channel. The browser speaks only to the Rust host (WebSocket wire, `proto/boxer/imzero2/v1/input.proto`); Go speaks only to the Rust host (FFFI2 / the egui2 interpreter command stream). The encoder *executes* in Rust; the GUI *renders* in Rust but its widget state is *owned* by Go (the interpreter is Go-driven); the decode *capabilities* live in the browser. Any design that puts the capability model and the controls in Go must route both across the Rust host.

Forces this ADR must respect:

- **ADR-0024 SD8 is mostly preserved, deliberately bent once.** SD8's principle is "the FFFI2 interpreter never learns input is remote." The output-settings feature is the one place that principle must yield: a control that changes the encoder *by definition* needs the interpreter to know a remote sink exists and what it can decode. The bend is scoped to an explicit, optional capability channel; ordinary input stays remote-agnostic.
- **Single active session.** [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) keeps one active encoded session with an explicit take-session handoff and a single re-pointed encoder. This ADR models the capabilities of *the active viewer* — the one being encoded for. Multi-viewer capability reconciliation under [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md)'s roster (proposed) is out of scope here.
- **Shared interpreter, two hosts.** `App::logic` is identical between the desktop (eframe) and headless hosts. New interpreter→host commands and host→interpreter inputs must be benign no-ops in desktop mode (no encoder, no remote sink).
- **Wire additions stay additive.** Per `input.proto`'s versioning policy: new fields and new `oneof` variants only, until a v2 is unavoidable.
- **The auth boundary is untouched.** New messages ride the channels ADR-0082 already authenticates and gates; no new transport, no second auth surface.

## Design space (QOC)

**Question.** Where does the control plane live — who holds the capability model, who owns the selection, and how does a selection reach the Rust encoder — given that capabilities originate in the browser, the GUI state is owned by Go, and the encoder runs in Rust?

**Options.**

- **O1 — Go-centric control plane (chosen).** Browser probes decode support and reports it over the wire; the host forwards it (merged with its own encode probe) into the interpreter as a synthetic platform input; Go holds a first-class capability + pipeline model and renders an imzero2 control widget; selection flows back as a new interpreter→host command that only the headless host acts on, rebuilding the encoder resize-style.
- **O2 — Browser-centric.** HTML controls on the viewer page talk straight to the Rust host over the wire, exactly like today's cadence toggle. Go is uninvolved.
- **O3 — Rust-host-centric authority, Go as read-only mirror.** The Rust host owns the capability + pipeline state; Go renders a mirror and emits intents that Rust adjudicates.
- **O4 — Direct browser→Go control socket.** A second transport from the browser to the Go process carries capabilities and control, bypassing Rust.

**Criteria.**

- **C1 — Capabilities first-class in Go.** The explicit ask: a typed, queryable, testable Go model of what the viewer can play.
- **C2 — Controls are imzero2 widgets.** Composable with the rest of the dashboard UI (gauge/treemap/canonicaltype lineage), not bolted onto the viewer page.
- **C3 — Single source of truth for selection.** No split-brain between where the widget state lives and where the pipeline decision is made.
- **C4 — Minimal new transport / auth surface.** Reuse the single authenticated WebSocket and the existing FFFI2 channel.
- **C5 — Implementation cost.**
- **C6 — Preserves ADR-0024/0082 invariants** (shared interpreter, single active session, resize-shaped switch).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (Go-centric) | O2 (browser-centric) | O3 (Rust authority + mirror) | O4 (browser→Go socket) |
|----|-----------------|----------------------|------------------------------|------------------------|
| C1 | ++              | −−                   | +                            | +                      |
| C2 | ++              | −−                   | +                            | +                      |
| C3 | ++              | +                    | −− (widget state is in Go)   | +                      |
| C4 | ++ (no new transport) | ++             | ++                           | −− (second socket + auth) |
| C5 | −  (two new seams) | ++                 | −                            | −−                     |
| C6 | ++              | +                    | +                            | −                      |

**Verdict.** O1. It is the only option that satisfies C1+C2 (the literal request) without the split-brain O3 incurs: because the control is an egui widget and the interpreter is Go-driven, the widget's state already lives in Go, so the selection authority belongs in Go too — O3 would round-trip every widget tick to Rust to read back state Go already holds. O2 is the cheapest and is the *status quo mechanism* for cadence, but it fails the explicit ask outright. O4 invents a transport and an auth boundary that ADR-0082 just finished closing, for no benefit O1 lacks. O1's cost (two new seams across the Rust host) is the irreducible price of the three-party split; it is bounded, and both seams reuse patterns already in the codebase (the SD8 input edge; the clipboard/screenshot host-command family).

## Decision

Implement **O1**. The Rust host gains a runtime-selectable codec lane — built on a single shared NUT-container demuxer rather than per-codec bitstream parsers (SD4) — and a host-encode capability probe; the browser gains a rich decode-capability probe; the Go side gains a first-class capability + pipeline model and a "video output" control widget; a switch is performed by the resize-shaped teardown/rebuild path. H.264, VP9, and AV1 lanes ship in v1.

### Subsidiary design decisions

- **SD1 — Control-plane topology is Go-centric (the O1 decision).** Capabilities are modeled in Go; the control is an imzero2 widget; the selection decision is made in Go and effected in Rust. Restated as the load-bearing SD because every other SD hangs off it.

- **SD2 — Capabilities in: browser → host → interpreter, as a synthetic platform input.** The browser sends a new `DecodeCapabilities` message (new `SessionControl` `oneof` variant, prefix `0x03`). The headless host merges it with its own host-encode probe (SD5) and injects the combined set into the interpreter command stream as a synthetic *platform input*, extending the ADR-0024 SD8 input edge (the same edge that injects mouse/keyboard/resize). The interpreter reads it into the Go model (SD9). Capabilities are (re)reported on connect and on every resize, because the rich probe (SD8) is geometry-dependent.

- **SD3 — Selection out: a new interpreter→host command, consumed only by the headless host.** Go emits `SetVideoPipeline{ codec, backend, rate_control… }` as a new command in the egui2 interpreter→host vocabulary (declared in the egui2 IDL and regenerated per the egui2 codegen runbook), joining the existing host-side-effect command family (clipboard `CopyTextToClipboard`, screenshot/svgexport). The desktop host ignores it; the headless host applies it (SD7). Go (the widget state) is the single source of truth for the selection. The selection does **not** travel the browser↔Rust wire — the browser learns the new codec only via the re-announced `SessionHello` (SD6).

- **SD4 — One shared NUT-container demuxer, not per-codec bitstream parsers; a "lane" is then just config.** The encoder pipeline muxes `ffmpeg`'s output to the **NUT** container on stdout (`-f nut`, streamed: `-flush_packets 1 -write_index 0`, minimal syncpoints); the host runs a single NUT frame reader and re-wraps each demuxed packet into the existing `VideoChunk` (wire schema and browser viewer unchanged). NUT is chosen over the other generic containers (MKV, fragmented MP4, MPEG-TS, IVF, and a metadata-sidecar — see Alternatives), all of which are host-internal here so browser support is irrelevant, on three properties confirmed empirically (2026-06-16, `ffmpeg` 8.1.1):
  - **It preserves the native elementary bitstream in-band.** H.264 stays Annex-B with in-band SPS/PPS on key frames; VP9 stays native frames; AV1 stays native temporal units — byte-identical to what `VideoChunk.data` carries today and what WebCodecs consumes. (MKV/MP4 rewrite H.264 to length-prefixed AVCC — verified in the probe: `0000 0001 67…` in NUT vs `0000 000e 67…` in MKV — which would force a per-codec reformat or a browser `description`-mode change; NUT needs neither.)
  - **Frame boundaries, the keyframe flag, and codec config sit in the container layer** — read without parsing any codec's bitstream (verified via `ffprobe -show_packets`: per-packet `K__` flags + sizes, uniform across H.264/VP9/AV1).
  - **It is the leanest** candidate (~+1.5% over raw, vs MKV +2.4% / fragmented MP4 +3.7%), is `ffmpeg`-native, and is the very container ImZero1 used through this same interactive pipeline (ADR-0024).

  With the split/keyframe/config logic centralized in that one reader, a **codec lane collapses to declarative config**: an `ffmpeg` argv fragment (hardware + software variants) and a WebCodecs codec-string formatter — no per-codec depacketizer or keyframe predicate. v1 lanes (encoders confirmed present on the dev host):
  - **H.264** — `h264_vaapi` (hw) / `libopenh264` (sw; `libx264` is absent on stock Fedora — rpmfusion split); `avc1.<profile><constraint><level>`.
  - **VP9** — `vp9_vaapi` (hw, where present) / `libvpx-vp9` (sw); `vp09.<profile>.<level>.<bitdepth>…`.
  - **AV1** — `av1_vaapi` (rare hw) / `libsvtav1` (sw, the latency-tolerable choice) / `libaom-av1` (sw, slowest); `av01.<profile>.<level><tier>.<bitdepth>`.

  Cost (see Consequences): the host owns the NUT reader — there is no mature Rust NUT crate (ImZero1 demuxed NUT via libmpv, not a hand-roll), so this is a few hundred lines against a frozen format, targeting `ffmpeg`'s `nutenc` output subset. Gated by the Phase 0 spike (Status). **Licence posture:** NUT is an open, patent-free format with a public specification and an MIT-licensed reference implementation (`libnut`; [nut-container.org](http://www.nut-container.org/), [lu-zero/nut](https://github.com/lu-zero/nut)); the reader is an independent implementation of that format with no FFmpeg (LGPL-2.1+) source copied or linked, so C7 / ADR-0024's subprocess-not-linkage stance is preserved — FFmpeg remains invoked only as an encoder subprocess.

- **SD5 — Host-encode probe is a probe-*encode*, not a capability listing.** `ffmpeg -encoders` is insufficient: the Fedora-mesa gotcha (ADR-0024 acceptance note) is precisely that `h264_vaapi` *opens* and then returns `ENOSYS` at encode time. The host therefore runs a few-frame probe-encode per candidate (codec × backend) at startup, caches the result, and exposes the set that actually encodes. A backend that fails the probe is simply not offered — the runtime failure becomes a never-presented option.

- **SD6 — `SessionHello` carries the active codec string; a description blob is an escape hatch, probably unused.** Add `string codec` (the WebCodecs codec string) to `SessionHello` (`input.proto:120`); the viewer configures `VideoDecoder` from it. Because the NUT path (SD4) keeps codec parameters in-band (H.264 SPS/PPS on key frames; VP9/AV1 sequence headers in their key frames), the codec string alone should suffice to `configure()` all three decoders — so the originally-planned `bytes codec_description` is downgraded to an **optional** field, added only if the spike finds a decoder that refuses to configure without it. H.264 retains SPS-derivation as a fallback. `SessionHello` is re-announced on a codec switch with the same hello-before-IDR ordering as resize, so the viewer reconfigures its decoder before the first frame of the new stream arrives.

- **SD7 — A switch is resize-shaped; no new mechanism.** On `SetVideoPipeline`: drain the frame mailbox, stop `ffmpeg`, swap the lane, respawn with the new argv, force an IDR, re-announce `SessionHello{codec}`, viewer reconfigures and rejoins at the IDR. This is the `ViewportResize` teardown/rebuild path (SD9 mailbox in `encoderpipe.rs`) with the codec lane as an additional thing that changes. The "short downtime" is the same brief gap a resize already incurs.

- **SD8 — Rich decode probe via `mediaCapabilities` + `isConfigSupported`.** The viewer probes each candidate `(codec, config)` at the current geometry/fps with `VideoDecoder.isConfigSupported(...)` (support) and `navigator.mediaCapabilities.decodingInfo(...)` (`smooth`, `powerEfficient`), and reports the matrix in `DecodeCapabilities`. Go uses `supported` to gate options, and `smooth`/`powerEfficient` to order them and to annotate the control — this is what lets the UI flag AV1 as "supported but software-decoded / not power-efficient" on a viewer that lacks hardware AV1 (notably Safari without an AV1-capable GPU), rather than letting the user pick a janky configuration blind.

- **SD9 — A first-class Go model package (`videopipeline`, provisional).** A new non-widget package under `public/thestack/imzero2/` (naming per [ADR-0048](./0048-go-file-package-naming.md)) holds: `DecodeCapabilities` (per-codec support/smooth/power-efficient, plus the probe geometry), `HostEncodeCapabilities` (per-codec available backends from SD5), the `OfferedCodecs` intersection, and `PipelineState` (active + desired codec/backend/rate-control/cadence). It is queryable and unit-testable independent of any widget, and is fed by the SD2 synthetic input.

- **SD10 — A full-pipeline control widget.** A new imzero2 widget (under `egui2/widgets/`) renders, from the SD9 model: a codec picker over `OfferedCodecs` (each annotated by the SD8 signals), an encoder-backend choice (hardware/software, from SD5), a bitrate/quality control, and **cadence folded in** (so the one panel is the whole control plane). The existing viewer-page HTML cadence toggle is retained as a redundant convenience for sessions with no Go panel visible, but the Go widget is authoritative.

- **SD11 — Desktop mode degrades cleanly.** With no remote sink, the SD9 model is empty / "native, N/A", the SD10 widget self-disables (or hides), and `SetVideoPipeline` (SD3) is a no-op. The shared interpreter carries both hosts without branching on host type in app logic.

- **SD12 — Scope is the single active viewer.** Capabilities are those of the viewer currently being encoded for (ADR-0082's single re-pointed encoder). On take-session handoff, re-probe the new active viewer and re-offer. Passive viewers (ADR-0086, proposed) and intersection-across-viewers are deferred — a heterogeneous roster would otherwise force the offered set down to the weakest viewer, a policy question this ADR does not pre-empt.

- **SD13 — Security posture unchanged.** `DecodeCapabilities` rides the ADR-0082-authenticated wire; `SetVideoPipeline` is host-internal over FFFI2. No new transport, no new auth boundary, no new bind surface. Capabilities are non-sensitive; the only new authority is "the authenticated active session can choose its own codec," which is within the session's existing trust envelope.

- **SD14 — Out of scope at v1.** HEVC (`hev1`) lane; per-viewer capability merge for passive viewers; automatic congestion-driven bitrate adaptation (ABR) — the control is *explicit user selection*; congestion is already handled by the SD9 mailbox dropping stale frames pre-encoder, not by re-encoding; audio; replacing the viewer's hand-rolled protobuf codec with `protobuf-es` (ADR-0024's deliberate single-file-viewer choice stands — the viewer hand-encodes the one new message it sends).

## Alternatives

- **O2 — Browser-centric HTML controls.** The status-quo mechanism for cadence: a toggle on the viewer page sends `SetCadence` straight to Rust. Cheapest possible, and a perfectly good *fallback* (retained per SD10). Rejected as the primary design because it satisfies neither half of the request — capabilities never become Go state (C1), and the control is not an imzero2 widget composable with the dashboard (C2). It also cannot reach the host-encode capability set, which lives in Rust and is never surfaced to the page.
- **O3 — Rust holds authority, Go mirrors.** Tidy if the pipeline state were Rust-owned — but it is not, because the *control* is an egui widget and egui widget state is owned by the Go interpreter. Making Rust authoritative means every widget interaction round-trips to Rust and back to stay consistent with the rendered control, reintroducing exactly the split-brain C3 guards against. Held only as the shape to revisit *if* a future non-widget (e.g. a CLI or an HTTP admin endpoint) needs to drive the pipeline without the interpreter in the loop.
- **O4 — Direct browser→Go socket.** Would let capabilities reach Go without traversing Rust, but there is no such channel today and adding one means a second WebSocket, a second auth handshake (re-litigating ADR-0082), and a second framing — a large new surface to avoid one hop through a host that is already forwarding input. Rejected.
- **Listing-based host-encode detection** (instead of SD5's probe-encode). Parsing `ffmpeg -encoders` is cheaper but provably wrong here: the documented Fedora-mesa `h264_vaapi` → `ENOSYS`-at-encode case is invisible to listing. A probe-encode is a few frames at startup and converts a class of runtime failures into never-offered options.
- **SPS/bitstream-only codec signaling** (instead of SD6's explicit `SessionHello.codec`). Works for H.264 (the viewer already does it) but does not generalize: VP9/AV1 codec strings carry profile/level/bit-depth the viewer would have to parse from each codec's header format. Carrying the codec string the host already knows is simpler and uniform; SPS-derivation stays as an H.264 fallback only.
- **Container choice for the SD4 lane mechanism** (why NUT over the alternatives; all are host-internal — ffmpeg muxes, the host demuxes, the browser never sees them — so the usual decider, ecosystem support, does not apply). **MKV / WebM** — clean per-frame keyframe flag and a Rust demux crate exists, but it rewrites H.264 to length-prefixed AVCC (and WebM cannot carry H.264 at all), reintroducing the per-codec reformat the NUT path exists to avoid. **Fragmented MP4 / CMAF** — ubiquitous and carries the decoder config in its init segment, but its one real edge (browser-native consumption) is moot when we demux host-side, and it has the heaviest per-frame overhead and the most demux code. **MPEG-TS** — broadcast-streaming native but has no clean per-frame boundary or reliable keyframe flag (PES reassembly + bitstream parsing) and poor VP9/AV1 carriage. **IVF** — trivial to parse, but carries no H.264 and has no keyframe flag in its frame header (size + pts only), so keyframe detection would fall back to per-codec bitstream parsing. **A `framecrc`/`tee` metadata sidecar** (elementary stream on one pipe, per-frame metadata on another) — avoids a container demuxer entirely, but empirically encodes the keyframe flag awkwardly (the `F=` field present/absent) and demands byte-exact alignment across two coordinated pipes — more fragile than reading one self-framing container. NUT wins because, uniquely, it keeps the native bitstream in-band *and* exposes keyframe/size/config at the container layer.

## Consequences

### Positive

- The two literal requests are met: the codec pipeline is runtime-selectable (SD3/SD4/SD7), and the viewer's decode capabilities are first-class, queryable Go state driving an imzero2 widget (SD9/SD10).
- The switch reuses the shipped resize teardown/rebuild path (SD7) — the "short downtime" requirement needs no new pacing or framing machinery.
- The Fedora-mesa VAAPI failure mode is absorbed by SD5: a broken backend becomes an un-offered option instead of a runtime crash.
- The capability model is the right home for the AV1 caveat: SD8's `smooth`/`powerEfficient` signals let the UI warn instead of letting the user select a configuration their browser will decode in slow software.
- Wire growth is two additive changes (`DecodeCapabilities` variant; `SessionHello.codec` field), consistent with the file's versioning policy; the Rust side regenerates from the proto and cannot drift.

### Negative

- ADR-0024 SD8's "interpreter never learns input is remote" is bent (SD2/SD11). The bend is explicit and scoped, but it is a real coupling: app logic can now branch on remote-sink capabilities, and that capability is a new thing to keep benign in desktop mode.
- The NUT mechanism (SD4) trades three per-codec framing surfaces for **one demuxer the host must own** — there is no mature Rust NUT crate, so it is hand-rolled (a few hundred lines against a frozen format, targeting `ffmpeg`'s `nutenc` subset). The bug surface is centralized rather than multiplied and the marginal codec is nearly free, but the NUT reader is new load-bearing code on the hot path and is the main thing the Phase 0 spike must de-risk. The alternative (MKV with a crate) would re-import a per-codec AVCC↔Annex-B reformat, so owning the demuxer is the deliberate price of keeping the wire and browser untouched.
- AV1 software encode (SD4) has a latency profile worse than H.264; offering it means users can select a configuration that is correct but sluggish on a host without AV1 hardware encode. SD5 (host probe) and SD8 (decode probe) inform but do not prevent this; the control should make the tradeoff legible.
- The control plane now spans three processes (browser probe, Rust lanes+host-command consumer, Go model+widget). A capability change must round-trip browser→Rust→Go to surface and Go→Rust to take effect; debugging crosses two language boundaries.

### Neutral

- The viewer keeps its hand-rolled protobuf codec (SD14) — one more message to hand-encode, consistent with ADR-0024's single-file-viewer stance.
- Cadence control gains a second home (SD10) — the Go panel is authoritative, the HTML toggle a fallback. Two controls for one property is mild redundancy, deliberately kept for the no-Go-panel case.
- `IMZERO2_HEADLESS_ENCODER_ARGS` (ADR-0009) remains the *startup default* selector; the runtime control supersedes it per session. The env var is not removed — it sets the initial lane before any viewer connects and serves headless smoke tests.

## Status

Accepted — 2026-06-16 (reviewed-by p@stergiotis). Implementation phasing: **Phase 0** (de-risk the NUT mechanism — a startup probe on 2026-06-16 already confirmed `ffmpeg` muxes H.264/VP9/AV1 to NUT with container-level keyframe flags and native in-band payloads; the remaining spike is the live-pipe NUT reader proven end-to-end into a WebCodecs decoder at acceptable latency) → **Phase 1** (`CodecLane` config abstraction + the shared NUT reader behind `EncoderSink`, **proven first on the VP9/AV1 lanes** where there is no incumbent to regress, with H.264 left on its shipped Annex-B passthrough) → **Phase 2** (fold H.264 into the NUT path — byte-identical Annex-B, so low-risk — and retire the standalone AUD splitter; SD5 host-encode probe) → **Phase 3** (`SessionHello.codec` + viewer configures from it; resize-shaped switch SD7) → **Phase 4** (`DecodeCapabilities` wire message + browser rich probe SD8) → **Phase 5** (Go `videopipeline` model SD9 + SD2 input edge + SD3 host command) → **Phase 6** (the control widget SD10) → **Phase 7** (end-to-end: switch each codec live, confirm decode resumes, confirm a broken VAAPI backend is un-offered).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — the headless render + ffmpeg + browser WebCodecs foundation this ADR extends; SD3/SD4/SD5 (encoder, Annex-B framing, WebCodecs), SD8 (input edge bent here), SD11 (encoder-backend selection deferred — revisited here), and the acceptance notes (VAAPI ENOSYS, resize teardown/rebuild, SD9 mailbox).
- [ADR-0082](./0082-imzero2-remote-session-auth-tls.md) — single active session, take-session handoff, the authenticated channels the new messages ride.
- [ADR-0062](./0062-imzero2-render-cadence.md) — the cadence control folded into the SD10 panel; `SetCadence` is the runtime-control proof-of-pattern.
- [ADR-0086](./0086-imzero2-active-passive-viewers-and-roster.md) (proposed) — active/passive viewers; SD12 scopes capability modeling to the active viewer and defers multi-viewer reconciliation to this ADR's resolution.
- [ADR-0087](./0087-imzero2-client-compositor-compartmentalization.md) (accepted) — client compositor posture; the browser decode-probe (SD8) lives in the viewer's decode path.
- [ADR-0009](./0009-imzero2-env-var-registry.md) — `IMZERO2_HEADLESS_ENCODER_ARGS` remains the startup default selector.
- [ADR-0048](./0048-go-file-package-naming.md) — naming for the `videopipeline` package (SD9).
- `proto/boxer/imzero2/v1/input.proto` — the wire contract; `SessionHello` (SD6) and the new `DecodeCapabilities` `SessionControl` variant (SD2).
- `rust/imzero2/src/imzero2/encoderpipe.rs` (`EncoderSink`, frame mailbox), `wscarrier.rs` (`SessionControl` handling), `viewer/index.html` (WebCodecs decode + the rich probe of SD8).
- [WebCodecs API](https://www.w3.org/TR/webcodecs/) — `VideoDecoder.isConfigSupported`.
- [Media Capabilities API](https://www.w3.org/TR/media-capabilities/) — `decodingInfo`'s `smooth` / `powerEfficient` (SD8).
- [NUT container format](https://ffmpeg.org/~michael/nut.txt) (also `ffmpeg -h muxer=nut`) — the SD4 host-internal container; `ffmpeg`-native, streaming-designed, used by ImZero1.
- ffmpeg encoders (confirmed present on the dev host, `ffmpeg` 8.1.1): `libvpx-vp9` / `vp9_vaapi` (VP9), `libsvtav1` / `libaom-av1` / `av1_vaapi` (AV1), `libopenh264` / `h264_vaapi` (H.264 — `libx264` is absent on stock Fedora, the rpmfusion split) — the SD4 lane backends.
