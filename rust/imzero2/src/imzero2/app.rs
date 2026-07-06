use crate::imzero2;
use crate::imzero2::appconfig::AppConfig;
use crate::imzero2::apphost;

/// Passes to render back-to-back at startup before dropping to the idle
/// heartbeat. Covers the Wayland/VSYNC `swap_buffers` handshake (see the
/// startup-stall note on `App::new`'s `request_repaint`) and the initial
/// multi-frame layout fit-up. ~16 passes ≈ 0.25 s at 60 Hz.
const WARMUP_PASSES: u32 = 16;

/// Idle repaint cadence once warmed up. egui overrides this with sooner
/// repaints for input, animation and the Go-side RequestRepaint opcodes (it
/// keeps the earliest deadline), so it only bounds how often a fully idle
/// window refreshes. Matches the imztop sampler's 1 s tick.
const IDLE_REPAINT_INTERVAL: std::time::Duration = std::time::Duration::from_secs(1);

pub struct App<'a, R: std::io::BufRead, W: std::io::Write> {
    fffi: imzero2::interpreter::ImZeroFffi<'a, R, W>,
    /// Counts down from [WARMUP_PASSES]; while > 0, `logic()` forces an
    /// immediate repaint even in reactive mode. See `logic()`.
    warmup_passes: u32,
    /// When true (IMZERO2_RENDER_CADENCE=reactive), `logic()` drops to the
    /// idle heartbeat after warmup. When false (continuous, the default) it
    /// requests an immediate repaint every pass. See `logic()`.
    reactive: bool,
}

impl<'a, R: std::io::BufRead, W: std::io::Write> App<'a, R, W> {
    /// Called once before the first frame. The host-independent part of
    /// the setup (fonts, IDS overlay, single-pass pinning, interpreter,
    /// SVG-export plugin) lives in [`apphost::init_common`], shared with
    /// the headless host (ADR-0024 SD1).
    pub fn new(cc: &eframe::CreationContext<'_>, config: &AppConfig, r: R, w: W) -> Self {
        let (fffi, reactive) = apphost::init_common(&cc.egui_ctx, config, r, w);
        Self {
            fffi,
            warmup_passes: WARMUP_PASSES,
            reactive,
        }
    }
}

impl<'a, R: std::io::BufRead, W: std::io::Write> eframe::App for App<'a, R, W> {
    /// Called by the framework to save state before shutdown.
    fn save(&mut self, _storage: &mut dyn eframe::Storage) {}

    /// Called before ui() AND whenever the window is hidden but a repaint was
    /// requested. This is the *only* lifecycle hook eframe 0.34 runs while the
    /// root window is still in its startup-hidden state — `ui()` (and the
    /// deprecated `update()`) are both gated on `is_visible` in
    /// eframe/src/native/epi_integration.rs. If we only drove the FFFI
    /// interpreter from `ui()`, nothing would read Go's command stream until
    /// the compositor delivered an input event to wake the loop, which is
    /// exactly the "nothing renders until I move the mouse" stall. Driving
    /// the interpreter from `logic()` lets Go's per-frame RequestRepaint
    /// reach egui on the very first cycle, egui schedules the next frame,
    /// the first paint happens, and `post_rendering` flips the window
    /// visible. Before eframe 0.34 this wasn't needed because `update()`
    /// was called unconditionally.
    ///
    /// Repaint scheduling depends on the render cadence (IMZERO2_RENDER_CADENCE,
    /// read into `self.reactive` in `new`):
    ///   - Continuous (default): request an immediate repaint every pass, so
    ///     the client paints at vsync rate.
    ///   - Reactive: render the first [WARMUP_PASSES] passes immediately so the
    ///     Wayland/VSYNC `swap_buffers` startup handshake settles — the Go-side
    ///     `c.RequestRepaint()` historically arrived too late for it, so driving
    ///     the repaint from here sets the flag before the pass ends regardless
    ///     of what Go did or when — then drop to a slow idle heartbeat
    ///     (`request_repaint_after(IDLE_REPAINT_INTERVAL)`). egui still
    ///     schedules sooner repaints for input, animation and Go-side
    ///     RequestRepaint opcodes (it keeps the earliest deadline), so
    ///     interaction stays at vsync rate while a visible-but-idle window drops
    ///     to a few fps.
    /// The Go decorator mirrors this cadence; both sides must agree or the
    /// immediate request wins and the loop spins continuously again.
    fn logic(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        if let Err(e) = self.fffi.interpret_commands_outer(ctx) {
            match e {
                imzero2::interpreter::InterpretError::PeerClosed => {
                    // Go side closed the pipe — graceful shutdown. Asking the
                    // viewport to close lets eframe drive the normal teardown
                    // path (Drop impls, save(), end_replay() finalizers) instead
                    // of unwinding through a panic.
                    tracing::info!("peer closed pipe — initiating graceful shutdown");
                    ctx.send_viewport_cmd(egui::ViewportCommand::Close);
                }
                other => {
                    // Frame-stack invariant violations or non-EOF I/O errors:
                    // log with full context and let the loop continue. A future
                    // pass may escalate to graceful shutdown for unrecoverable
                    // variants once we have field experience with which ones
                    // are transient.
                    tracing::error!(error = %other, "interpret error during dispatch");
                }
            }
        }
        if self.reactive && self.warmup_passes == 0 {
            ctx.request_repaint_after(IDLE_REPAINT_INTERVAL);
        } else {
            self.warmup_passes = self.warmup_passes.saturating_sub(1);
            ctx.request_repaint();
        }
    }

    /// No-op: all work happens in `logic()`. We still have to provide `ui()`
    /// because it's a required method on `eframe::App`.
    fn ui(&mut self, _ui: &mut egui::Ui, _frame: &mut eframe::Frame) {}
}
