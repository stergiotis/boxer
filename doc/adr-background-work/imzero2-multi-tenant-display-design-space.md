---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-20 from a design
> dialogue. Nothing here is a decision and nothing in §5 is built. Provenance
> is four-tiered: (a) claims about this repository were checked against the
> working tree on the compile date; (b) §6, §9 and §10 summarize measurements
> taken 2026-09-24 to -26 on two AMD APUs with integrated encode blocks — a
> handheld-class one that was loaded throughout, and a workstation-class one —
> one or two launches per configuration, one recorded workload for the
> encoders. They are stated as directions and orders of magnitude; the raw
> data is not in the tree, so none of it is a figure to plan capacity on;
> (c) figures quoted
> from ADRs and trials keep the date and conditions of their source, which is
> linked; (d) §7 is a **clean-room** survey: it rests on product documentation,
> protocol-specification prose, engineering blogs and published papers, read on
> the compile date by delegated passes that opened no source file, repository
> code view, diff or package of any surveyed project, and that tagged every
> claim as read on a fetched page, seen only in a search snippet, recalled, or
> inferred. Only *read* claims appear as fact in §7; the rest are marked. Most
> pages reached the passes through a fetch tool that summarises, so a quotation
> in §7 is that tool's rendering of the page unless marked *read directly* (a
> PDF's own text) — check the wording at the link before relying on it.

# Rendering, encoding and presenting imzero2 across two machines — a design space

## 1 Question and scope

Many imzero2 applications, each a **hostile tenant**, run as gokrazy guests
under QEMU. One **media host** faces the viewers and owns the only GPU. What
should cross from a guest to the media host, what should cross from the media
host to a viewer, and what sits in between?

The project owner set four constraints in the dialogue this page records:

1. Guests are hostile: they run code and SQL the operator does not trust.
2. The guests' hypervisor and the media host are not always the same machine;
   when they differ, a LAN is between them.
3. Viewers are on the LAN and on a WAN, in a browser or in a native client.
4. As many sessions as the hardware carries: the design is about resource
   efficiency and about isolation between tenants.

Out of scope: the appliance image itself
([ADR-0206](../adr/0206-gokrazy-appliance-image.md)), authentication design
([ADR-0082](../adr/0082-imzero2-remote-session-auth-tls.md), accepted and
unbuilt), and guest egress policy, which §11 lists as open.

## 2 What the tree already has

[ARCHITECTURE §2](../ARCHITECTURE.md#2-operation-modes) describes the modes.
The ones this page builds on:

- The **mesh lane** ([ADR-0128](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md),
  code shipped): tessellated meshes and texture deltas,
  content-addressed per connection, to a WebGL2 painter. The tree serializes
  this wire (`meshlane.rs`) and has no Rust decoder for it; the viewer page is
  its only consumer.
- The **CPU rasterizer host** ([ADR-0205](../adr/0205-imzero2-cpu-rasterized-pixel-host.md))
  and the **encoder pipeline** through an `ffmpeg` subprocess
  ([ADR-0088](../adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)).
- The **carrier** (`WsCarrier`): roster and takeover
  ([ADR-0086](../adr/0086-imzero2-active-passive-viewers-and-roster.md)),
  joining and stream lifetimes
  ([ADR-0242](../adr/0242-imzero2-session-and-stream-lifetimes.md)), codec
  negotiation. Its producer-facing surface — `on_meshes`, `on_frame`,
  `ingest_textures`, `drain_input`, `take_resize` — does not depend on how
  frames are produced; the render thread is its only producer. Input items
  keep their wire protobuf form.
- The lane is **session-global**; per-viewer mixing is ADR-0128's unlanded M4.

## 3 The design space

Every frame passes the same stages. The space is which stage boundary crosses
from the guest to the media host (**cut α**) and which crosses from the media
host to the viewer (**cut β**).

```text
 stage                         what comes out                     size per frame, source
 ─────────────────────────────────────────────────────────────────────────────────────────────
 1 app logic (Go)              FFFI2 opcodes                      250–330 KB, every frame, plus
                                                                  ~14 blocking fetches (ADR-0077)
 2 egui pass (layout, text)    shapes
 3 tessellate                  meshes + texture deltas            33–138 KiB full, ~85 B idle
                                                                  (ADR-0128, UI screens) — §6
                                                                  measures where this breaks
 4 rasterize                   pixels                             4 MB at 1280×800
 5 encode                      video access units                 encoder-paced
 6 decode + present
```

### 3.1 Cut α — guest to media host

| | Crosses | Guest runs | Media host must | For | Against |
| --- | --- | --- | --- | --- | --- |
| α1 | FFFI2 | Go only | run the whole Rust host per guest | leanest guest; the existing GPU host unchanged; over vsock the round-trip objection of ADR-0024 O5 does not apply | the media host interprets the full opcode set from a tenant — a protocol written for a trusted parent and child, where an id-stack mismatch panics at render; continuous traffic |
| α2 | shapes | up to egui | tessellate onward | — | dominated by α3; rejected in ADR-0128 |
| α3 | meshes | the mesh-only host | decode, then render or pass through | small fixed-shape data-only format; LAN-cheap **for UI screens** | a second consumer of the wire; §6 |
| α4 | raw pixels | through the rasterizer | encode only | narrowest surface: width × height × 4 | rasterizer memory in every guest; 4 MB per changed frame rules out a LAN |
| α5 | encoded video | everything | relay bytes | no new rendering code | software encode in every guest; no use of the GPU |

### 3.2 Rendering and encoding on the media host

- **Rasterize** on the GPU (wgpu) or on the CPU. Both are about a millisecond
  per UI frame (ARCHITECTURE §2.8, dated figures), so rasterization is not
  where a GPU earns its place. The CPU rasterizer keeps tenant geometry away
  from a GPU driver.
- **Encode** on the GPU's fixed-function encoder block through the existing
  `ffmpeg` subprocess (`h264_vaapi` and siblings), one process per stream. The
  block is separate silicon, not general-purpose compute. A zero-copy path
  from render target to encoder needs a linked `libva` and is a deferral.
- **Feed the encoder on change only** (ADR-0242), which is what lets many
  mostly-static screens share one block.

### 3.3 Cut β — media host to viewer

| | Viewer needs | Character |
| --- | --- | --- |
| mesh | WebGL2, or a native painter | nothing rendered or encoded on the media host; bursty |
| video over WebSocket | WebCodecs | the existing `0x01` lane; encoder-paced |
| video over WebRTC or WebTransport | a browser | UDP or QUIC congestion behaviour; more machinery, paid once on the media host |
| images | anything | too weak for a session (ADR-0024 O4); fits an overview page of many sessions |
| a standard stream (HLS, RTSP, SRT) | players, walls, recorders | view-only |

### 3.4 The multiplexing layer

Two separable pieces were conflated in the first form of the idea:

- a **multiplexer** — routing, authentication and TLS once, guests with no
  listener — which carries the isolation argument and needs no GPU;
- a **transcoder** — mesh to pixels to video — which carries the hardware
  argument and is needed only by viewers that take video.

## 4 What the constraints eliminate

| Option | Eliminated by | Kill-reason |
| --- | --- | --- |
| α1 FFFI2 | hostile guests | the opcode interpreter becomes the tenant-facing attack surface |
| α4 raw pixels | a LAN between the machines | 4 MB per changed frame |
| α5 guest-side encode, as the default | density | 14–18 ms of guest CPU per frame at 1280×800 @ 30 fps (ADR-0128 §Context, 2026-07-18); kept as the control arm and, after §6, as a fallback |
| α2 shapes | — | ADR-0128's rejection stands |

α3 is what remains as the default cut. §6 shows it cannot be the only one.

## 5 The shape that remains

```text
   hypervisor X — one machine or several; every guest is a hostile tenant in its own VM
   ┌──────────────────────────────────────────────────────────┐   ┌────────────────────────┐
   │ guest A₁ — gokrazy under QEMU; listens on nothing        │   │ guests A₂ … A_N        │
   │ ┌──────────────────┐ FFFI2 ┌───────────────────────────┐ │   │ the same image,        │
   │ │ Go host          │ ◀───▶ │ Rust host, `headless`     │ │   │ another tenant each    │
   │ │ apps · keelson   │ pipes │ egui pass ─▶ tessellate   │ │   │                        │
   │ │ chlocal pool     │       │ ─▶ hash bodies ─▶ carrier │ │   │                        │
   │ │ (stays in the VM)│       │ with exactly one client   │ │   │                        │
   │ └────────┬─────────┘       └─────────────┬─────────────┘ │   │                        │
   └──────────┼───────────────────────────────┼───────────────┘   └───────────┬────────────┘
              │ HTTP, the tenant's own        │ dials OUT over virtio-vsock,  │
              ▼ connection string             ▼ then serves the carrier       ▼
   ┌─────────────────────────────┐   ┌───────────────────────────────────────────────────┐
   │ ClickHouse — per tenant:    │   │ uplink relay on X: is B itself when X = B;        │
   │ a server of its own, or a   │   │ otherwise mTLS across the LAN. Asserts the guest's│
   │ user + database + quota on  │   │ identity (X, vsock CID) — the guest cannot forge  │
   │ a multi-tenant server       │   │ it. Wire: 0x04 meshes ─▶ · ◀─ 0x02 input · 0x03   │
   └─────────────────────────────┘   └─────────────────────────┬─────────────────────────┘
                                                               │ UI screens: 20 kbit/s idle
                                                               ▼ → 1–2 Mbit/s; see §6
   media host B — trusted, holds the GPU, the only address a viewer ever reaches
   ┌───────────────────────────────────────────────────────────────────────────────────────┐
   │ Go front (CGO=0): TLS · auth · route by tenant · supervise workers · park the uplinks │
   │ of unwatched sessions (the guest then idles at its 1 s heartbeat). Parses no frames.  │
   │                                                                                       │
   │ worker_i — one sandboxed Rust process per *watched* session                           │
   │ ┌───────────────────────────────────────────────────────────────────────────────────┐ │
   │ │ upstream mesh client: parse framing (hash list · bodies · texture deltas),        │ │
   │ │ re-hash bodies, enforce caps                                                      │ │
   │ │   ├─ for mesh viewers:  SerializedFrame ─▶ carrier          (no geometry decoded) │ │
   │ │   └─ for video viewers: decode geometry, check indices ─▶ CPU rasterizer ─▶ BGRA  │ │
   │ │                         ─▶ mailbox ─▶ ffmpeg h264_vaapi ─▶ NUT demux ─▶ carrier   │ │
   │ │ carrier = the guest's own WsCarrier: roster · takeover · join · codec negotiation │ │
   │ │ active viewer's input · resize · clipboard ─▶ re-encoded upstream                 │ │
   │ └───────────────────────────────────────────────────────────────────────────────────┘ │
   │ GPU encoder block: shared by one ffmpeg per video stream, fed only on change; it sees │
   │ pixels at sizes B chose, never a tenant's geometry                                    │
   └─────────────────────────────────────────▲─────────────────────────────────────────────┘
        one connection per viewer: WebSocket now; WebTransport or WebRTC later, B-side only
                                             │
      ┌──────────────────────────┬───────────┴──────────────┬──────────────────────────────┐
      │ LAN browser              │ WAN browser              │ native viewer                │
      │ 0x04 meshes ─▶ WebGL2    │ 0x01 video ─▶ WebCodecs  │ 0x04 meshes ─▶ the worker's  │
      │ painter, viewer's DPR    │ decoder, encoder-paced   │ mesh decoder + a wgpu window │
      └──────────────────────────┴──────────────────────────┴──────────────────────────────┘
```

Why each piece looks the way it does:

- **The roster runs on the media host, as the same code.** The owner's rule
  was: where it gives the most flexibility without duplication. The worker
  drives the existing carrier from a second kind of producer — an upstream mesh
  client instead of an egui pass — so roster, takeover, joining and the viewer
  page run there unchanged, and the guest runs the identical carrier with one
  client. The alternative, one upstream connection per viewer with the roster
  in the guest, makes guest cost grow with viewers, leaves a hostile guest
  arbitrating who controls a session, and still needs join and keyframe logic
  in the worker for video fan-out — which is the duplication.
- **The transport seam is on the media host only.** The guest and the uplink
  stay a framed byte stream; what the viewer leg rides can change without
  touching a guest.
- **The guest dials out.** It listens on nothing, which removes the posture
  ADR-0206 §SD5 had to excuse. An idle, unwatched guest already falls to a one
  second heartbeat. The stock gokrazy amd64 kernel builds no vsock transport
  (checked 2026-09-24 against the kernel package's image): the uplink needs a
  kernel configured with `CONFIG_VIRTIO_VSOCKETS`, or virtio-serial, which it
  carries.
- **The Go front parses no carrier protocol.** For mesh viewers the media host
  is a relay; the worker parses framing only and re-hashes bodies, so upstream
  and downstream names are independent and a guest cannot name a body falsely.
- **Geometry is decoded in one place**, where a video viewer needs pixels, with
  caps on vertices, textures and live bodies and with indices checked against
  the vertex count.
- **The native viewer is a mesh viewer**: the worker's decoder in a window.
- **ClickHouse is per tenant** — a server, or a user with database and quota on
  a multi-tenant server — reached by the tenant's own connection string. The
  `clickhouse-local` pool stays inside the VM (§9).

One carrier change serves this shape and the standalone host alike: per-viewer
lane mixing (ADR-0128 M4).

## 6 Measurement — the mesh lane under dense geometry

The owner's doubt: data-visualization widgets have pathological cases that
matter to this project, and the mesh lane's measurements (ADR-0128) cover a
launcher, a widget gallery and a treemap. A 2 000- and a 20 000-node graph, a
country map with particle trails, and the flow-particles trial's animation
were put on the mesh lane and on H.264 — VAAPI with the host's arguments and
`libopenh264` — held still, panned and animated, at 1100 × 900 and 30 Hz, with
a passive second viewer counting what the carrier sent.

*Correction.* This section first held a table from an informal run on
2026-09-20 whose video column was labelled `libopenh264`. The host had
replaced that encoder, which the machine's ffmpeg lacks, with VAAPI — a
fallback it takes with only a log warning. The table is removed; it is in git
history at `36ca229a`.

**What it says.**

- **Mesh cost follows what moves, not what is shown.** Held still, the dense
  graph and the map cost the mesh lane about what video costs — around a tenth
  of a megabyte per second — because content addressing re-sends only what
  changed. Panned or animated they cost one to three orders of magnitude more
  than either H.264 lane, and a frame is hundreds of kilobytes to megabytes.
  That is the wire format's arithmetic: a segment or a marker is a quad, 72 B
  once a mesh passes 65 535 vertices, and a moving view changes every vertex.
- **Video cost follows pixel count and motion**, and stayed below a megabyte
  per second in every case.
- ADR-0128's deferred frame compression (zstd, 3.2× measured there on UI
  frames) would not change the class *(inference — not measured on this
  content)*.
- The doubt was right for moving dense geometry and wrong for held dense
  geometry.

**What it changes in §5.**

- On one machine the uplink is a local channel, and a heavy session is a CPU
  cost, not a link cost: the guest serializes and hashes megabytes per frame,
  the worker parses and rasterizes them, and the viewer gets video. The media
  host absorbs the pathology for the viewer leg, which needs a **measured
  trigger** — mesh bytes per second over a window — to move a session's viewers
  to video. Every held state measured sat more than an order of magnitude below
  the lightest moving one, so the trigger has a wide band to sit in. That is
  ADR-0128's M4 bandwidth guard, decided on the media host.
- Across a LAN a heavy session wants tens to hundreds of megabits on the
  uplink. There the guest needs its own pixel path — the rasterizer and a
  software encoder in the image, α5 — for the sessions that trip the guard,
  and the media host relays. That path uses no GPU.
- Widgets decide how often the guard trips. Dense marks drawn as an image — a
  density texture computed where the data is — ship once as a texture and
  deduplicate; a particle cap on the mesh lane is cheap.
- A `segments` primitive in the wire — endpoints, width and colour, expanded to
  quads by the consumer — would cut moving frames by three to six times
  *(inference from the flow-particles trial's 20 B per segment across FFFI2)*
  and still leave them well over an order above video. It also grows the
  vocabulary; §7.3 has what the surveyed protocols did about that.

## 7 What others do

Sources are keyed in §12. A claim without a mark was *read*; *(snippet)*,
*(recalled)* and *(inference)* mark the rest.

### 7.1 Cloud game streaming

- **The cut is always finished, encoded video.** No surveyed service streams
  draw commands or geometry *(inference from the whole pass)* — a game is an
  opaque GPU program, so there is no earlier cut to take.
- **The encoder is separate silicon, fed without a copy.** NVIDIA describes
  NVENC as "independent of graphics/CUDA cores" [NV-app]; its low-latency
  1080p H.264 throughput is listed from "667 fps (Pascal) to 977 fps
  (Blackwell)" [NV-app], about a millisecond per frame *(inference)*. Parsec:
  "the raw frame never leaves the GPU" [Parsec-tech]. NETINT's variant puts
  the encoder on its own card behind peer-to-peer DMA from the GPU
  [NETINT, read directly].
- **Tuned for latency.** NVIDIA's guide names "Ultra-low latency, with CBR" for
  constrained channels, describes intra refresh — "consecutive sections of the
  frames to be encoded using intra macroblocks" — in place of periodic
  keyframes, and reference invalidation, by which a client that lost a frame
  stops the encoder from "using the current frame as reference" [NV-guide].
- **Chroma.** NVENC lists 4:4:4 for H.264 and HEVC and 4:2:0 for AV1 [NV-matrix];
  NETINT's card lists 4:2:0 only [NETINT, read directly]. Moonlight added 4:4:4
  "for improved text clarity during remote desktop usage" *(snippet)*.
- **Media rides UDP.** Stadia used "WebRTC with no substantial modifications";
  GeForce NOW set a session up over TLS on TCP and then used "multiple UDP
  channels" carrying RTP, input on a dedicated flow; PlayStation Now a custom
  single UDP flow [Carlucci-2020, a 2020 measurement paper]. Parsec built its
  own protocol on UDP with DTLS and a congestion control whose stated priority
  is "latency, frame rates, and then video quality" [Parsec-net]. No
  documented TCP fallback for media was found. That each viewer has its own
  encoder follows from per-viewer rate adaptation and is stated nowhere
  *(inference)*.
- **Density is a few sessions per GPU.** NVIDIA's RTX server: 40 GPUs for "up
  to 160 PC games to be run concurrently" [NV-rtx, read directly]. Amazon
  GameLift Streams publishes one to twelve sessions per GPU by stream class
  [GLS-groups]. The Xbox Series X chip can "run four Xbox One S game sessions
  simultaneously" [TC-xbox].
- **The broker is documented in one place.** GameLift Streams has always-on,
  on-demand and target-idle capacity, the last held "in anticipation of future
  activity"; a disconnected session waits for its client, "Default: 120
  seconds" [GLS-groups, GLS-sessions].
- **Hardware sold for encode density** is fixed-function, not general-purpose
  compute. NVIDIA's matrix gives GeForce cards "12" concurrent sessions and
  professional cards "Unrestricted" [NV-matrix]; its application note still
  says "8 per system" [NV-app] — the two disagree and no dated note was found.
  Intel: a Flex 170 reaches "up to 68 streams of 720p30", measured on two
  Android game titles [Intel-flex, read directly]. AMD's Alveo MA35D: "up to
  32x 1080p60 streams per card" [AMD-ma35d]. NETINT's Quadra T1U: "Up to 32x
  1080p30" at 17 W [NETINT, read directly].

### 7.2 Desktop-as-a-service

All *read* on the vendor documentation linked in §12 unless marked.

- **Reverse connect.** Azure Virtual Desktop's session host keeps an outbound
  channel to the broker and, on a connection, dials the same gateway instance
  the client reached; the transport "doesn't use a TCP listener to receive
  incoming RDP connections". Citrix Rendezvous has the agent connect outward as
  well. Amazon DCV's Connection Gateway is the other shape: a single access
  point that resolves a session id and dials in.
- **The gateway relays; policy sits at the host.** In Azure Virtual Desktop a
  client-to-host TLS session is nested inside the relay, so the gateway cannot
  see content; clipboard direction and data types are session-host settings,
  and watermarking is configured on the host and enforced by the client.
- **Hybrid, not pure video.** RDP runs "image processors, a classifier, and a
  codec" per frame; its mixed mode "separates text, image and video encoding
  using different codecs", is "only available with software encoding", and
  exists because "approximately 80% of the graphics data for a remote session
  is text". Full-screen video coding "performs worse than mixed-mode encoding
  when the screen content is largely text based". Citrix uses a video codec
  "only in the part of the screen where the image is moving", codes text
  losslessly, and "sharpens to pixel perfect (lossless) when activity stops".
- **Hardware encode costs text quality in these products.** RDP's mixed mode
  is lost under hardware encoding and 4:4:4 chroma falls back to 4:2:0 under
  hardware HEVC; Citrix states "Lossless text is not compatible with NVENC
  hardware encoding". Software encoding is RDP's default even with a GPU
  present.
- **Command remoting and its caches.** RDP's drawing orders filled client-side
  bitmap caches; revision 2 caches persist to disk under "a 64-bit key (derived
  from a cryptographic hash of the bitmap contents)" and the client sends its
  key list on reconnect. SPICE forwards driver-level draw commands, drops those
  hidden by later ones, and "heuristically identifies video areas" to send them
  as a video stream instead. Every order set and cache revision was a
  negotiated capability *(inference)*; no vendor statement on why the industry
  moved to bitmaps was found.
- **Transport.** RDP Shortpath starts on the TCP reverse connection, then tries
  UDP, and "if the UDP connection succeeds the TCP connection drops". Citrix
  attempts its UDP transport and TCP in parallel and keeps retrying UDP.
- **Density.** Microsoft's multi-session guidance runs from six light users per
  vCPU to one power user, notes "a 15-20% capacity cost" for running on virtual
  machines, and deallocates idle hosts by a capacity threshold. Shared
  read-only base images with per-machine difference disks are the norm
  *(snippet for Citrix and Horizon)*.

### 7.3 Systems that ship commands or geometry, and where they stop

This is the pass that bears on §6.

**Where they cut.**

- **Cloudflare's Network Vector Rendering** "intercepts the remote Chromium
  browser's Skia draw commands, tokenizes and compresses them" [CF-WP, read
  directly] and replays them in a WebAssembly Skia in the viewer's browser
  [CF-blog]; images and fonts "are only transferred once and stored in a cache
  identified by an identifier" [NVR-pat]. The security claim is that no web
  code reaches the client; nothing read discusses a client parser consuming
  commands derived from a hostile page *(inference from absence)*.
- **Menlo** mirrors or reconstructs a DOM, and is the one vendor found naming
  the hostile-stream threat: "protocol checking and enforcement" so that the
  endpoint "is not fooled… by malformed updates coming from an infected
  isolated browser", with updates held to "a canonical format" [Menlo, read
  directly].
- **THINC** cut at the video driver with five commands, of which RAW pixels are
  "invoked as a last resort"; a higher cut "requires replicating… a great deal
  of functionality" on the client, a lower one "has lost all semantic
  information". Its server may "coalesce, clip, and discard updates" when the
  client cannot keep up [pTHINC, read directly].
- **Apache Guacamole** splits an authenticating web application that "does not
  understand any remote desktop protocol at all" from `guacd`, a daemon that
  translates RDP, VNC or SSH into a small drawing protocol; the stated reason
  is protocol agnosticism, not security [G-arch].
- **GTK Broadway** moved from image frames to diffed DOM nodes and found "text
  is clearly the weak point" [Bway].

**Where they stop — every one has a pixel path, and most switch by
measurement.**

- **ParaView** has the clearest form. Its Remote Render Threshold sets "the
  data size at which to render remotely in parallel or to render locally":
  under it geometry ships and the client renders, over it the server renders
  and images ship [PV-ref]; the default is 20 MB *(snippet)*. A second
  threshold swaps in decimated geometry while interacting. Its tutorial's rule
  of thumb: "it is always faster to ship images than to ship data" [PV-tut].
- **VirtualGL** states the invariant §6 measured: with server rendering,
  "Images can be delivered at the same frame rate regardless of how big the 3D
  data was", which "converts the 3D performance problem into a 2D performance
  problem" [VGL].
- **Lai and Nieh measured it** across thin-client systems: "higher-level
  display encodings are not necessarily more bandwidth efficient than
  lower-level primitives"; those encodings "were optimized for text-based
  displays"; and where latency rather than bandwidth binds, "the simpler
  pixel-based encoding approaches provide better overall performance"
  [Lai-Nieh, read directly].
- **Cloudflare** keeps the door open in its patent — "where the tradeoffs
  indicate that pixel data is the best option, for example for performance or
  quality, pixel pushing can be resorted to" [NVR-pat]. Until 2026 it
  rasterized canvas content on the server; canvas remoting now sends vector
  commands for "2D Canvas contexts only", "WebGL and 3D graphics applications
  continue using bitmap rendering", and a per-page switch turns it off
  [CF-canvas, CF-chg]. No Cloudflare figure for dense or fast-changing scenes
  was found.
- **NoMachine** defaults to an X11 vector mode and for multimedia says "it's
  suggested to disable" it for standard video codecs [NoMachine].
- **Rate-based switches on the pixel side:** SPICE "heuristically identifies
  video areas… updated with high rate" [SPICE]; KasmVNC enters a video mode
  when change covers "45"% of the screen for "5" seconds [KasmVNC].
- **Datashader** is the plotting field's answer to the same limit — "Browsers
  can't handle 1 million points!" — with data "binned into a fixed-size 2D
  array automatically on every zoom or pan event" so that "the full data is not
  in the browser" [HV].

**What the authors say about versions and vocabulary.**

- Cloudflare builds the client renderer "with the same settings that would have
  been used on the server" and pushes it per session, so the two never differ
  [NVR-pat].
- Guacamole negotiates "the highest supported version" in its handshake; older
  peers "silently ignore" what they do not know, and its growth has been in
  control instructions, not drawing ones [G-proto].
- "Because X is an application-level protocol, its performance depends heavily
  on what X primitives an application is programmed to use" [Lai-Nieh, read
  directly]. Wayland left rendering out of the protocol altogether: clients
  "render into a shareable buffer" [Wayland].

## 8 What transfers

- **Owning the toolkit is what makes a cut before pixels possible at all**, and
  §6 with §7.3 says how far to trust it: every surveyed system that ships
  commands or geometry keeps a pixel path, and the ones that say how they
  choose, choose by measurement — ParaView by data size, SPICE and KasmVNC by
  change rate and area. The owner's doubt in §6 is the field's settled
  position, measured by Lai and Nieh and stated as an invariant by VirtualGL:
  image cost is bounded by resolution and geometry cost is not. A mesh lane
  with a measured fallback to pixels is ParaView's design with a byte budget
  for a threshold.
- **Dense marks belong in an image before they reach the wire.** Datashader's
  practice — aggregate where the data is, ship a fixed-size array — is the
  widget-side half of the same answer, and here it has a natural home: the
  aggregation is a ClickHouse query and the result a texture that ships once.
- **Validate the stream on the trusted side.** Menlo is the only vendor found
  that names a hostile rendering stream; its answer — a canonical format,
  checked — is §5's re-hash, caps and index check, and argues for checking
  frames the media host only relays as well.
- **Version coupling has two known answers, and §5 uses both.** The viewer page
  comes from the media host with every session, which is Cloudflare's; the
  uplink between separately deployed binaries needs Guacamole's — a version in
  the handshake, unknown instructions ignored, growth kept out of the drawing
  vocabulary.
- **When the consumer falls behind, drop; do not queue.** THINC's server
  coalesces and discards. Under §6's load the uplink needs the same
  latest-wins behaviour the frame mailbox already has.
- **The mesh lane is this project's lossless text path.** The desktop products
  give up text quality when they take hardware encode. Here a hardware video
  lane would be 4:2:0 as well, so the reason to keep viewers on meshes wherever
  the guard allows is fidelity as much as cost — which makes mesh over a WAN
  (the guard, compression) worth more than it first seemed.
- **Reverse connect, and a relay that authenticates and routes**, are standard
  practice and match §5. What §5 lacks is the **broker**: starting guests on
  demand, reconnecting a viewer to a running session, and deallocating
  unwatched guests — by the desktop products' own account the main cost lever.
- **Persistent client caches** keyed by content hash are prior art for keeping
  the font atlas and mesh bodies in the browser across reconnects.
- **Keep TCP as the fallback; add UDP or QUIC beside it.** The desktop products
  do exactly that. The game services found no use for TCP media at all, and
  their loss handling — intra refresh, reference invalidation — presumes a
  transport that can lose a frame, so those encoder settings arrive with the
  transport change, not before it.
- **Encode density is bought as fixed-function hardware**, a consumer card caps
  concurrent sessions in its driver, and the cards sold for density are 4:2:0.
  The encoder measurement (§10) ran on integrated APU blocks, not on the
  hardware a media host would use.
- **The broker is a product in its own right** in both fields — capacity held
  idle in anticipation, a reconnect window, scale-down that waits for a host to
  empty.
- **Policy placement differs.** The desktop products enforce clipboard policy
  and watermarking at the session host because their gateway cannot see inside
  the session. Here the guest is hostile and the media host terminates the
  session, so that is where per-tenant policy and recording would sit.

## 9 Where the cost of a session sits

Placement of rendering and encoding moves hundreds of mebibytes and
milliseconds per session. The guest itself costs more. `gok vm run` defaults to
1 GiB, and the image that carries ClickHouse is documented at 3 GiB
([showcase/gokrazy](../../showcase/gokrazy/README.md)) because its
`clickhouse-local` pool keeps warm workers. Booted under QEMU at decreasing
memory, both images served a watched session in about a third of a GiB — below
that they did not crash but thrashed and served nothing — and the Go app held
most of it, the Rust host a small part. `play` starts its pools on first use,
so that floor has no workers in it; an idle worker is on the order of
150 MiB, most of it the binary's pages, which the workers of one guest share.
At this size memory, not the placement of rendering, sets the density; the
guest's own heap is what to shrink.

A pool shared across guests is structurally easy — workers are one-shot
([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md)), fungible until handed
SQL, and their input tables already travel as Arrow files with the request
([ADR-0094](../adr/0094-keelson-introspection-tables.md) §SD4) — and wrong for
hostile tenants unless each worker is as isolated as a guest: a worker runs
tenant SQL with `file()`, `url()` and `s3()` inside a large C++ program.

| | Isolation | Cost |
| --- | --- | --- |
| per-guest pool with `MinIdle` 0 or 1 | the VM's | a cold spawn per scratch query: 41.3 ms against 7.8 ms warm (ADR-0028 M0, 2026-05-14) |
| shared pool, OS-sandboxed workers | the sandbox's — the design's weakest link | medium |
| shared pool of snapshot-restored micro-VMs | a VM's | infrastructure this tree does not have |
| tenant scratch SQL on the tenant's server, inputs as external data | the server's, already per tenant | loses scratch `file()` and `url()`, arguably intended |

Memory deduplication across guests (KSM) is a known side channel between
hostile tenants and should stay off.

## 10 What the measurements showed

What §6, §9 and the encoder and whole-session runs of 2026-09-24 to -26
taught, and what it means for §5. The encoder figures are from integrated APU
encode blocks, one recorded workload, and a full headless host standing in
for the worker §5 describes; they bound the design, they do not size it.

**Findings.**

- **Held content is cheap on the mesh lane, moving dense content is not** (§6).
  The gap between the two is wide enough that a byte-rate trigger separates
  them without tuning.
- **An integrated encode block carries on the order of a hundred 1100 × 900
  streams at 30 fps, and the block itself is the limit** — its media engine is
  saturated there, while the upload path keeps up well beyond. Past that point
  latency jumps past a second instead of frame rates falling, because frames
  queue inside the encoder process.
- **Whole sessions run out of CPU well before that.** On a 32-thread
  workstation, some tens of sessions held 30 fps and the count depended on
  what a session drew: a panned map rasterizes several times slower than an
  animated particle field, and carried about half as many sessions. Past the
  knee frame rates collapse and sessions fail to start.
- **Two costs were configuration, not design.** The host's VAAPI argument list
  left `h264_vaapi` at an `async_depth` of 2, which emits a frame only when the
  next arrives — a frame of latency always, and, since the host feeds only
  changed frames, the settled screen's last frame held until the next change;
  the depth is now 1, at no measured cost in throughput. The same list converts
  BGRA to NV12 on the CPU, which costs about as much as rasterizing the frame
  and is most of what hardware encoding costs the host; converting on the GPU
  removes most of it. The host now converts on the GPU where a check at
  start-up finds the driver converting to BT.709, and says so in the stream;
  the GPU conversion as first measured wrote full range and declared the
  samples RGB, and the driver, not the filter's options, picks the matrix.
- **A software encoder is affordable per session** — a fraction of a core at
  this size, a few milliseconds of latency — and runs out with the CPU.
- **Guests are small** (§9), and **the stock gokrazy kernel has no vsock** (§5).

**Strengths of §5 that the measurements support.**

- Mesh as the default viewer lane fits UI: most screen time is held content,
  where mesh costs what video does, stays lossless for text, and needs no
  encoder.
- The guard to video is cheap and stable, because held and moving content are
  far apart; this is the settled practice of §7.3, confirmed on this project's
  own widgets.
- Density is plausible: guests are a fraction of a GiB and one encode block is
  not the bottleneck.
- The isolation boundary costs nothing measured: the hostile guest never
  touches the GPU or the encoder, and what crosses to the media host is
  geometry it validates.

**Weaknesses and risks the measurements expose.**

- **Rasterizing on the media host is the capacity bottleneck.** Every session
  with a video viewer is rasterized on the shared, trusted machine's CPU, at a
  cost set by tenant content the host does not control. That wants a CPU
  budget per tenant, or GPU rasterization on the media host — which §5 avoided
  for isolation.
- **Overload is a cliff, not a slope,** for the encode block and for the CPU
  alike. The media host needs admission control, which makes the broker §8
  found missing more urgent.
- **Moving content over a LAN uplink is expensive,** tens to hundreds of
  megabits a session, so α5 — guest-side rasterizing and software encoding for
  sessions that trip the guard — is required rather than optional wherever the
  hypervisor and the media host are separate machines.
- **Widgets become an architectural concern.** Chrome that changes every frame
  defeats sending only on change, and dense marks drawn as geometry make every
  pan expensive. Dense data drawn as a texture, capped particle counts and
  throttled live counters keep sessions on the cheap side of the guard.
- **The uplink needs a kernel decision** — vsock in a custom kernel
  configuration, or virtio-serial.
- **Video quality was not measured.** For text-heavy UI the 4:2:0 hardware lane
  may read worse than mesh, which argues for keeping viewers on mesh wherever
  the guard allows.

**What it changes in §5.** The shape stands: mesh-only guests, a trusted media
host, mesh to viewers by default and video past a measured trigger. Capacity
is planned in sessions per CPU core by what the sessions draw, not in streams
per encoder; admission control and the broker are components of the design,
not later additions; guest-side encoding is a supported path; and GPU colour
conversion (done where the driver's matrix is verified) and the uplink's
transport are the cheap fixes to make first.

## 11 Open questions

1. The §5 worker itself — mesh parsing and rasterizing without a UI pass — and
   what it costs next to the whole host measured here.
2. A discrete GPU's encoder, and GPU rasterization on the media host with what
   it does to isolation.
3. Video quality for text-heavy UI, lane against lane.
4. The guest floor with warm `clickhouse-local` workers, measured rather than
   added up; a realistic mix of mostly idle sessions.
5. Guest egress policy; the broker role (§8); whether the media host validates
   frames it only relays.

## 12 References

In-tree: [ARCHITECTURE §2](../ARCHITECTURE.md#2-operation-modes) ·
[ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md) ·
[ADR-0077](../adr/0077-keelson-browser-wasm-execution.md) ·
[ADR-0082](../adr/0082-imzero2-remote-session-auth-tls.md) ·
[ADR-0086](../adr/0086-imzero2-active-passive-viewers-and-roster.md) ·
[ADR-0088](../adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md) ·
[ADR-0128](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md) ·
[ADR-0205](../adr/0205-imzero2-cpu-rasterized-pixel-host.md) ·
[ADR-0206](../adr/0206-gokrazy-appliance-image.md) ·
[ADR-0242](../adr/0242-imzero2-session-and-stream-lifetimes.md) ·
[flow-particles trial](../trials/flow-particles-frame-cost/README.md).

Cloud game streaming, read 2026-09-20:

- [NV-app] [NVENC application note](https://docs.nvidia.com/video-technologies/video-codec-sdk/13.0/nvenc-application-note/index.html) ·
  [NV-guide] [NVENC programming guide](https://docs.nvidia.com/video-technologies/video-codec-sdk/13.0/nvenc-video-encoder-api-prog-guide/index.html) ·
  [NV-matrix] [encode and decode support matrix](https://developer.nvidia.com/video-encode-and-decode-gpu-support-matrix-new) ·
  [NV-rtx] [RTX server datasheet](https://www.nvidia.com/content/dam/en-zz/Solutions/Data-Center/cloud-gaming-server/geforce-now-rtx-server-gaming-datasheet.pdf)
- [Parsec-tech] [a description of Parsec technology](https://parsec.app/blog/description-of-parsec-technology-b2738dcc3842) ·
  [Parsec-net] [Parsec's networking protocol](https://parsec.app/blog/a-networking-protocol-built-for-the-lowest-latency-interactive-game-streaming-1fd5a03a6007)
- [Carlucci-2020] [a network analysis of Stadia, GeForce NOW and PS Now](https://ar5iv.labs.arxiv.org/html/2012.06774)
- [GLS-groups] [GameLift Streams stream groups](https://docs.aws.amazon.com/gameliftstreams/latest/developerguide/stream-groups.html) ·
  [GLS-sessions] [stream sessions](https://docs.aws.amazon.com/gameliftstreams/latest/developerguide/stream-sessions.html)
- [TC-xbox] [Xbox Series X details](https://techcrunch.com/2020/03/16/microsoft-just-revealed-a-ton-of-new-info-about-the-xbox-series-x)
- [Intel-flex] [Data Center GPU Flex Series announcement](https://download.intel.com/newsroom/archive/2025/en-us-2022-08-24-introducing-intel-data-center-gpu-flex-series-for-the-intelligent-visual-cloud.pdf) ·
  [AMD-ma35d] [Alveo MA35D announcement](https://ir.amd.com/news-events/press-releases/detail/1122/) ·
  [NETINT] [cloud gaming video server brief](https://info.netint.com/hubfs/downloads/NETINT-CloudGaming-VideoServer.pdf)

Command and geometry remoting, read 2026-09-20:

- [CF-WP] [Cloudflare Browser Isolation white paper](https://cf-assets.www.cloudflare.com/slt3lc6tev37/19fjHx0VVV5a8yUooL0tKu/8c6349ce9df80485ec1639c880bfc77b/Cloudflare_Browser_Isolation_White_Paper_-_Q2_2023.pdf) ·
  [CF-blog] [Cloudflare and remote browser isolation](https://blog.cloudflare.com/cloudflare-and-remote-browser-isolation/) ·
  [CF-canvas] [canvas remoting](https://developers.cloudflare.com/cloudflare-one/remote-browser-isolation/canvas-remoting/) ·
  [CF-chg] [canvas remoting changelog](https://developers.cloudflare.com/changelog/post/2026-04-10-canvas-remoting-performance/) ·
  [NVR-pat] [US10452868B1](https://patents.google.com/patent/US10452868B1/en)
- [Menlo] [Adaptive Clientless Rendering white paper](https://em360tech.com/sites/default/files/2021-07/Menlo-Adaptive-Clientless-Rendering-WP.pdf)
- [G-arch] [Guacamole architecture](https://guacamole.apache.org/doc/gug/guacamole-architecture.html) ·
  [G-proto] [Guacamole protocol](https://guacamole.apache.org/doc/gug/guacamole-protocol.html)
- [pTHINC] [Kim, Baratto, Nieh — pTHINC, WWW 2006](https://www.cs.columbia.edu/~nieh/pubs/www2006_fordist.pdf) ·
  [Lai-Nieh] [Lai, Nieh — On the performance of wide-area thin-client computing, ACM TOCS 24(2), 2006](https://www.cs.columbia.edu/~nieh/pubs/tocs2003_i2thin.pdf)
- [PV-ref] [ParaView reference manual, parallel data visualization](https://docs.paraview.org/en/v5.12.1/ReferenceManual/parallelDataVisualization.html) ·
  [PV-tut] [ParaView tutorial, visualizing large models](https://docs.paraview.org/en/latest/Tutorials/SelfDirectedTutorial/visualizingLargeModels.html) ·
  [VGL] [VirtualGL background](https://virtualgl.org/About/Background)
- [HV] [HoloViews, working with large data](https://holoviews.org/user_guide/Large_Data.html) ·
  [Bway] [Broadway adventures in GTK4](https://blogs.gnome.org/alexl/2019/03/29/broadway-adventures-in-gtk4/) ·
  [NoMachine] [knowledge base AR01M00832](https://kb.nomachine.com/AR01M00832) ·
  [KasmVNC] [Xvnc manual](https://kasmweb.com/kasmvnc/docs/latest/man/Xvnc.html) ·
  [SPICE] [Spice for newbies](https://www.spice-space.org/spice-for-newbies.html) ·
  [Wayland] [FAQ](https://wayland.freedesktop.org/faq.html)

Desktop-as-a-service, read 2026-09-20:

- Azure Virtual Desktop: [network connectivity](https://learn.microsoft.com/en-us/azure/virtual-desktop/network-connectivity) ·
  [graphics encoding](https://learn.microsoft.com/en-us/azure/virtual-desktop/graphics-encoding) ·
  [GPU acceleration](https://learn.microsoft.com/en-us/azure/virtual-desktop/graphics-enable-gpu-acceleration) ·
  [RDP Shortpath](https://learn.microsoft.com/en-us/azure/virtual-desktop/rdp-shortpath) ·
  [clipboard transfer direction](https://learn.microsoft.com/en-us/azure/virtual-desktop/clipboard-transfer-direction-data-types) ·
  [watermarking](https://learn.microsoft.com/en-us/azure/virtual-desktop/watermarking) ·
  [autoscale](https://learn.microsoft.com/en-us/azure/virtual-desktop/autoscale-create-assign-scaling-plan) ·
  [session host sizing](https://learn.microsoft.com/en-us/windows-server/remote/remote-desktop-services/session-host-virtual-machine-sizing-guidelines)
- Microsoft Open Specifications: [MS-RDPEGDI bitmap caches](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-rdpegdi/2bf92588-42bd-4527-8b3e-b90c56e292d2)
- Citrix: [Rendezvous](https://docs.citrix.com/en-us/citrix-daas/hdx/rendezvous-protocol.html) ·
  [Thinwire](https://docs.citrix.com/en-us/citrix-virtual-apps-desktops/2407/graphics/thinwire.html) ·
  [adaptive transport](https://docs.citrix.com/en-us/citrix-virtual-apps-desktops/technical-overview/hdx/adaptive-transport.html) ·
  [graphics policy settings](https://docs.citrix.com/en-us/citrix-virtual-apps-desktops/policies/reference/ica-policy-settings/graphics-policy-settings.html)
- Amazon DCV: [what is DCV](https://docs.aws.amazon.com/dcv/latest/adminguide/what-is-dcv.html) ·
  [Connection Gateway](https://docs.aws.amazon.com/dcv/latest/gw-admin/what-is-gw.html)
- SPICE: [Spice for newbies](https://www.spice-space.org/spice-for-newbies.html)
