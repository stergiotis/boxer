---
type: reference
audience: contributor
status: draft
---

> **Status: draft.** Runtime compatibility requires the target checks below.

# imzero2 Windows viewer

A standalone Rust client for the imzero2 WebSocket video/input protocol (ADR-0243). Native Win32 controls surround a dedicated D3D11 video surface. The host crate, browser runtime, egui and Dear ImGui are not linked.

The initial video formats are eight-bit 4:2:0 H.264, VP9 profile 0 and AV1 Main. D3D11VA uses the presentation device when supported; software decoding is the explicit fallback, including under Wine/Proton. GUI controls use Windows painting. Decoder status and GPU presentation status are separate.

## Build on Linux

Prerequisites: Rust and its `x86_64-pc-windows-gnu` target, MinGW-w64 GCC/binutils/headers, Clang/libclang, Meson, Ninja, NASM, Make, pkg-config and tar. `curl` is needed only to fetch sources. Dependencies are not installed by the scripts.

```sh
rustup target add x86_64-pc-windows-gnu --toolchain 1.96.1
scripts/dev/build-viewer-deps.sh --preflight-only
scripts/dev/build-viewer-deps.sh --fetch
scripts/dev/build-viewer.sh
```

The dependency script verifies pinned archives and builds Windows FFmpeg/dav1d shared libraries. Without `--fetch`, it works from previously staged sources. `FFMPEG_DIR` can select a matching Windows development prefix; build-time `CC_x86_64_pc_windows_gnu` selects a MinGW compiler. Set `LIBCLANG_PATH` when libclang is outside the dynamic loader's search path.

The portable directory is `target/windows-portable/` under this crate. Keep the DLLs beside the executable. It includes FFmpeg/dav1d license and source material; distributing modified libraries also requires distributing their corresponding source. Rust dependency license files are collected from the locked package sources. `MINGW_LICENSE_DIR` must point at the toolchain's runtime notices, because the directory also ships MinGW's `libgcc_s` and `libwinpthread`; the build refuses to run without it. `--skip-runtime-licenses` builds without them and marks the result as not redistributable. This is not a dependency-free single-file application.

Shader HLSL is embedded and compiled once on the target using `d3dcompiler_47.dll`. A missing runtime prevents GPU startup; cross-compilation does not run a Windows shader compiler.

## Connect

Launch `imzero2-viewer.exe`, enter a full `ws://` or `wss://` endpoint and select Connect. The endpoint path is preserved, including proxy prefixes. Certificates are verified against the TLS library's public trust roots, not the Windows certificate store, so a private CA or a self-signed certificate is refused. For such a host, enter its certificate's SHA-256 fingerprint in the *Pin SHA-256* field, or pass `--pin-sha256`: that one certificate is then accepted, and no other. Paste the fingerprint as a browser's certificate viewer shows it or as `openssl x509 -noout -fingerprint -sha256` prints it. A mismatch reports the fingerprint the server presented; when the host's certificate is replaced, the pin must be too. `--insecure`, or the *Skip TLS certificate verification* menu item, skips the check entirely for the next connect: any certificate is accepted, and the handshake is still checked against whichever certificate was presented. That is encryption to an unauthenticated peer, and the session carries input and clipboard as well as video, so anyone on the network path can read the session and inject input. An unverified session says so in the title bar and on each status line (ADR-0243, update of 2026-09-19). A pin takes precedence over the menu item; the two flags together are refused. Stored credentials and proxy-specific authentication are not implemented. Do not expose an unauthenticated host publicly merely because this client supports TLS.

Menus provide session takeover, fullscreen, fit/1:1 display, explicit remote resize, and clipboard opt-in. Clipboard transfer is text-only: incoming text is accepted only when enabled and active; outgoing paste requires the Paste clipboard command. F11 switches fullscreen. Input is sent only while active. Losing focus cancels held input using the additive focus message from ADR-0242; use a host implementing that contract.

Command-line options:

```text
--url wss://example.invalid/app/ws
--pin-sha256 FINGERPRINT   accept only the certificate with this SHA-256 (see above)
--insecure                 skip TLS certificate verification (see above)
--software                 force software decoding
--hardware                 require D3D11VA decoding
--capture frame.bmp        save a scripted capture
--exit-after-frames 30      exit after this many decoded frames
--timeout 30               fail the scripted run after this many seconds
```

Combine capture with `--exit-after-frames` and a timeout for bounded automation. Capture intentionally reads back the GPU surface; ordinary hardware presentation does not. Automatic reconnect acquires a new connection role and waits for a keyframe; it does not force the shared encoder to restart. Unsupported profiles fail explicitly rather than advertising decode support.

## Verify

```sh
scripts/ci/viewer_test.sh
```

The default Rust tests run on Linux without Windows/FFmpeg libraries, and the `viewer` job in `.github/workflows/test.yaml` runs exactly this script. Cross-building must separately compile and link the target modules — nothing under `src/win` is compiled by this lane, so a change there is unverified until it is cross-built. `scripts/ci/viewer_wine_smoke.sh` generates independent codec fixtures, runs a local WebSocket peer and the packaged executable, and checks the three captured outputs. Set `WINE` to select a Wine launcher. It tests software decoding and presentation, not predictive-frame recovery or hardware decode. `examples/input_driver.rs`, built for Windows, sends native window messages to a running viewer for focus, mouse, text, fullscreen, resize and close checks. The fixture peer prints received input for inspection. Run the packaged executable under Wine/Proton with `--software` against each real server codec as an additional compatibility check. Native Windows testing is needed to verify D3D11VA on actual adapters; neither Wine nor a successful cross-build establishes hardware decoding correctness.

Check resize, fullscreen, DPI changes, minimise and restore, the display powering off mid-stream (decoding must continue: the frame count keeps rising while the shown count may not), a network stall of a few seconds followed by its backlog (one session throughout: the burst is decoded, and only its newest pictures shown), focus cancellation, passive-to-active takeover, clipboard opt-in, disconnect/reconnect and a fresh keyframe after a stream change. Exercise unsupported profiles and unavailable acceleration as failure paths. Native Linux presentation, Linux hardware decoding, audio, HDR, mesh and persistent credentials are deferred.
