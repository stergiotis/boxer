---
type: adr
status: accepted
date: 2026-09-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-17
---

# ADR-0243: imzero2 native Windows viewer

## Context

A Windows viewer needs explicit control of video decoding and presentation without a browser runtime. Rust is the implementation language; native Win32 controls avoid maintaining a GUI-toolkit graphics backend. Linux-to-Windows cross-compilation and Wine/Proton execution are desired, but graphics translation does not imply hardware video decoding support.

## Decision

### SD1 — Independent client, shared wire contract

Build a standalone Rust executable against the canonical `boxer.imzero2.v1` protobuf schema. Do not depend on the imzero2 host crate. Retain the existing WebSocket transport, server-authoritative roles and stream announcements. The legacy `ClientHello.webcodecs` flag declares video/takeover capability even for native decoding; mesh capability remains false. This decision adds no authentication mechanism and does not make an unsecured host safe for public exposure. TLS certificate validation remains enabled.

### SD2 — Native shell and separate video surface

Use Win32 controls for connection, menus, status and settings. A dedicated child window hosts D3D11 video presentation; native-control painting never shares its swap-chain surface. Video conversion and scaling are GPU operations; ordinary controls use Windows rendering, without claiming GPU rasterization of each control. Input follows remote logical coordinates, session ownership and the focus-cancellation contract of ADR-0242.

### SD3 — Hardware decoding is a verified path, not a flag

Use FFmpeg in process, with generated bindings and bounded Rust ownership wrappers. On Windows, probe D3D11VA against the actual stream and verify decoded hardware frames. Decode and presentation use the same D3D11 device; GPU-local copying is allowed, CPU readback is not part of the hardware display path. Report decoder and presenter separately. Software fallback is explicit and reported, including under Wine/Proton. The first cut supports eight-bit 4:2:0 H.264, VP9 profile 0 and AV1 Main; other profiles are not advertised.

### SD4 — Independent lifetimes and bounded work

Keep networking, the native message loop and video work independently responsive. The video worker owns decoder and immediate-context operations. Local connection/stream generations invalidate obsolete work. Encoded reference pictures are not disposable: overflow or decoder failure rejoins a stream rather than dropping arbitrary access units and continuing. Presentation may discard superseded decoded pictures. Retain frame ownership until GPU work no longer uses a decoder surface. Rejoining waits for a keyframe; it does not assume that connecting restarts the shared encoder.

### SD5 — Cross-build and compatibility boundaries

Target Windows GNU from Linux using MinGW-w64 and matching, pinned shared FFmpeg libraries, including a software AV1 decoder. Package licenses and corresponding-source/build information. Embed shader sources and compile once on the target with D3DCompile; a missing compiler runtime is an explicit startup error. Native Windows hardware validation and Wine/Proton compatibility are separate test lanes. Wine/Proton may decode in software while presenting on the GPU. Native Linux hardware decoding, HDR, mesh, audio and stored credentials are deferred.

## Surfaces

| Surface | Change | Moves with it |
| --- | --- | --- |
| `boxer.imzero2.v1` | New consumer; no wire changes | Client protobuf generation and lifecycle tests |
| Windows distribution | Rust executable and matching native DLLs | Cross-build preflight, dependency packaging and license material |

## Alternatives

- **C++ with Dear ImGui.** More direct graphics integration, but Rust and native controls were selected for the client; no second GUI implementation is needed.
- **egui/wgpu.** Adds a different graphics backend to the D3D11 decode path or requires a custom renderer. Native menus and dialogs suffice for the local shell.
- **Media Foundation only.** Makes codec availability depend on installed Windows codec components and their compatibility-layer equivalents.
- **Hardware decode required under Wine/Proton.** D3D11 graphics compatibility does not establish D3D11VA support. Software decoding is permitted for the initial version.
- **Native Linux backend immediately.** Adds a second decoder/presenter integration before the Windows path is validated.

## Consequences

The application owns Win32 layout, input translation, graphics recovery and FFmpeg surface lifetimes. The distribution includes native libraries rather than a single dependency-free executable. Software decoding can cost substantial CPU and cannot be described as a hardware-accelerated decode path. Cross-compilation establishes buildability, not runtime or driver correctness.

## Migration

Nothing to migrate in the host or browser. The client consumes the existing protocol and the additive focus event specified by ADR-0242. Older hosts cannot provide that cancellation behavior and should be upgraded rather than relying on synthetic button releases.

## Verification plan

Linux unit tests cover framing, session transitions, geometry and input state without Windows dependencies. Explicit cross-build and Wine lanes fail preflight when their prerequisites are absent. Target runs exercise all three codecs, input, resizing, reconnect and software fallback. Actual Windows hardware decoding and GPU-resident presentation need a Windows runner with a supported adapter; neither Wine nor a successful cross-build substitutes for it.

## Status

Accepted — 2026-09-17, following approval of the implementation plan and its initial compatibility boundary.
