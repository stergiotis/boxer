//! GPU-less, SVG-only headless render host (companion to the ADR-0024
//! `headless` host, but with no wgpu / winit / video encoder).
//!
//! Drives the FFFI2 interpreter against a hand-rolled `egui::Context` using
//! `ctx.run_ui` once per frame — exactly like the desktop and headless hosts
//! dispatch (`app.rs` / `headless.rs`) — but the *only* consumer of the pass
//! is the SVG-export plugin registered by [`apphost::init_common`]. That
//! plugin fires in `on_end_pass` whenever the Go side queued an `ExportSvg` /
//! `ExportSvgWindow` opcode during the frame and walks `Context::graphics()`
//! (the pre-tessellation shape buffer) into a self-contained SVG file.
//!
//! Because SVG export reads shapes, not pixels, this host needs no GPU device:
//! there is no `wgpu` instance, no offscreen texture, no readback, no ffmpeg,
//! no WebSocket carrier. That makes it the intended client binary for serving
//! imzero2-rendered views as SVG over HTTP — a plain container with no Vulkan
//! stack can run it. The Go side sets per-request state, renders one window,
//! calls `c.ExportSvgWindow(...)`, and reads the resulting file back.
//!
//! Pacing is Go-driven: `interpret_commands_outer` blocks reading one frame's
//! worth of opcodes from stdin, so the loop advances in lockstep with the Go
//! render loop's `FinishServersideFrame` flushes. There is no fps clock — a
//! frame is produced exactly when Go sends one.

use crate::imzero2::appconfig::AppConfig;
use crate::imzero2::apphost;
use crate::imzero2::interpreter::InterpretError;

/// Cap handed to egui for its font-atlas texture. The desktop/headless hosts
/// read this from the GPU adapter; with no GPU we pick a value large enough
/// that the atlas is never the limiting factor. egui only uses it to bound
/// atlas growth, so an over-estimate is harmless.
const MAX_TEXTURE_SIDE: usize = 8192;

/// Nominal frame delta handed to egui for animation timing only. Wall-clock
/// cadence is set by the Go side (see the module doc on pacing), so this value
/// affects animation interpolation, not how fast the loop spins.
const NOMINAL_DT: f32 = 1.0 / 60.0;

/// Run the SVG-only render loop until the Go peer closes the pipe (graceful
/// shutdown) or an app requests a viewport close.
pub fn run_main_loop(config: AppConfig) -> Result<(), Box<dyn std::error::Error>> {
    // Logical viewport. `ExportSvgWindow` uses the target window's own
    // `area_rect` as the SVG viewBox, so this rect only has to be large
    // enough to contain the rendered window — it is NOT the SVG's size.
    let width = config.initial_main_window_width.max(64.0);
    let height = config.initial_main_window_height.max(64.0);
    let screen_rect = egui::Rect::from_min_size(egui::Pos2::ZERO, egui::vec2(width, height));

    tracing::info!(width, height, "headless_svg host up (GPU-less SVG-only render loop)");

    let ctx = egui::Context::default();
    // Shared, host-independent setup: fonts, IDS style overlay, single-pass
    // pinning, the FFFI interpreter, and — crucially — the SVG-export plugin.
    let (mut fffi, _reactive) = apphost::init_common(
        &ctx,
        &config,
        std::io::stdin().lock(),
        std::io::stdout().lock(),
    );

    let start = std::time::Instant::now();
    let mut frame_idx: u64 = 0;
    loop {
        let raw_input = egui::RawInput {
            screen_rect: Some(screen_rect),
            max_texture_side: Some(MAX_TEXTURE_SIDE),
            time: Some(start.elapsed().as_secs_f64()),
            predicted_dt: NOMINAL_DT,
            focused: true,
            ..Default::default()
        };

        let mut shutdown = false;
        // `run_ui` begins the pass, runs the closure, and ends it. The FFFI
        // interpreter dispatches Go's opcode stream into egui during the
        // closure; the SVG-export plugin's `on_end_pass` then drains any
        // pending export and writes the file. We keep the returned
        // `FullOutput` only to honour an app-driven viewport close — no
        // tessellation, render, or readback happens.
        let out = ctx.run_ui(raw_input, |ui| {
            if let Err(e) = fffi.interpret_commands_outer(ui.ctx()) {
                match e {
                    InterpretError::PeerClosed => {
                        tracing::info!("peer closed pipe — SVG host shutting down");
                        shutdown = true;
                    }
                    other => {
                        // Mirrors the other hosts: log and continue; a future
                        // pass classifies transient vs. fatal from field data.
                        tracing::error!(error = %other, "interpret error during dispatch");
                    }
                }
            }
        });

        // App-driven viewport close (e.g. a File→Quit) also ends the host —
        // mirrors headless.rs `close_requested`.
        let close = out
            .viewport_output
            .values()
            .any(|vo| vo.commands.iter().any(|c| matches!(c, egui::ViewportCommand::Close)));
        if close {
            tracing::info!("viewport close requested — SVG host shutting down");
            shutdown = true;
        }

        frame_idx += 1;
        if shutdown {
            break;
        }
    }
    tracing::info!(frames = frame_idx, "headless_svg host done");
    Ok(())
}
