use crate::imzero2;
use crate::imzero2::appconfig::AppConfig;
use crate::imzero2::apphost;

/// Passes to render back-to-back at startup before dropping to the idle
/// heartbeat. Covers the Wayland/VSYNC `swap_buffers` handshake (see the
/// startup-stall note on `App::new`'s `request_repaint`) and the initial
/// multi-frame layout fit-up. ~16 passes ≈ 0.25 s at 60 Hz.
const WARMUP_PASSES: u32 = 16;

/// Idle repaint cadence once warmed up. egui overrides this with sooner
/// repaints for input, animation and the Go-side `RequestRepaint` opcodes (it
/// keeps the earliest deadline), so it only bounds how often a fully idle
/// window refreshes. Matches the imztop sampler's 1 s tick.
const IDLE_REPAINT_INTERVAL: std::time::Duration = std::time::Duration::from_secs(1);

pub struct App<'a, R: std::io::BufRead, W: std::io::Write> {
    fffi: imzero2::interpreter::ImZeroFffi<'a, R, W>,
    /// Counts down from [`WARMUP_PASSES`]; while > 0, `logic()` forces an
    /// immediate repaint even in reactive mode. See `logic()`.
    warmup_passes: u32,
    /// When true (`IMZERO2_RENDER_CADENCE=reactive`), `logic()` drops to the
    /// idle heartbeat after warmup. When false (continuous, the default) it
    /// requests an immediate repaint every pass. See `logic()`.
    reactive: bool,
    /// `-backgroundColorRGBA`, or `None` to track the theme. Resolved per
    /// call in `clear_color()` rather than at construction so that a later
    /// style change moves the root background with the panels.
    background_color_rgba: Option<egui::Color32>,
}

impl<R: std::io::BufRead, W: std::io::Write> App<'_, R, W> {
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
            background_color_rgba: config.background_color_rgba,
        }
    }
}

impl<R: std::io::BufRead, W: std::io::Write> eframe::App for App<'_, R, W> {
    /// Called by the framework to save state before shutdown.
    fn save(&mut self, _storage: &mut dyn eframe::Storage) {}

    /// The colour the root viewport is cleared to before each frame — every
    /// pixel the UI does not cover.
    ///
    /// This has to be overridden. eframe's default is
    /// `Color32::from_rgba_unmultiplied(12, 12, 12, 180)`, translucent on
    /// purpose so that turning on `ViewportBuilder::with_transparent` shows
    /// an immediate effect. We never request transparency, but whether that
    /// alpha is then dropped or handed to the compositor is platform
    /// business, not the app's: on one compositing X11 setup it came through
    /// and the window read as semi-transparent wherever no panel was
    /// painted, while an Xwayland/wgpu run of the same binary got an
    /// alpha-less depth-24 visual and stayed opaque. Not a difference to
    /// leave a window's appearance resting on.
    ///
    /// Unset (the default), the root follows the active theme's `panel_fill`,
    /// so it matches the panels drawn over it instead of a framework-chosen
    /// grey. `-backgroundColorRGBA` overrides it; an alpha below `ff` there
    /// is an explicit request for a see-through window, and `entry.rs` pairs
    /// it with `with_transparent` so the compositor is actually asked.
    fn clear_color(&self, visuals: &egui::Visuals) -> [f32; 4] {
        self.background_color_rgba.unwrap_or(visuals.panel_fill).to_normalized_gamma_f32()
    }

    /// Called before `ui()` AND whenever the window is hidden but a repaint was
    /// requested. This is the *only* lifecycle hook eframe 0.34 runs while the
    /// root window is still in its startup-hidden state — `ui()` (and the
    /// deprecated `update()`) are both gated on `is_visible` in
    /// `eframe/src/native/epi_integration.rs`. If we only drove the FFFI
    /// interpreter from `ui()`, nothing would read Go's command stream until
    /// the compositor delivered an input event to wake the loop, which is
    /// exactly the "nothing renders until I move the mouse" stall. Driving
    /// the interpreter from `logic()` lets Go's per-frame `RequestRepaint`
    /// reach egui on the very first cycle, egui schedules the next frame,
    /// the first paint happens, and `post_rendering` flips the window
    /// visible. Before eframe 0.34 this wasn't needed because `update()`
    /// was called unconditionally.
    ///
    /// Repaint scheduling depends on the render cadence (`IMZERO2_RENDER_CADENCE`,
    /// read into `self.reactive` in `new`):
    ///   - Continuous (default): request an immediate repaint every pass, so
    ///     the client paints at vsync rate.
    ///   - Reactive: render the first [`WARMUP_PASSES`] passes immediately so the
    ///     Wayland/VSYNC `swap_buffers` startup handshake settles — the Go-side
    ///     `c.RequestRepaint()` historically arrived too late for it, so driving
    ///     the repaint from here sets the flag before the pass ends regardless
    ///     of what Go did or when — then drop to a slow idle heartbeat
    ///     (`request_repaint_after(IDLE_REPAINT_INTERVAL)`). egui still
    ///     schedules sooner repaints for input, animation and Go-side
    ///     `RequestRepaint` opcodes (it keeps the earliest deadline), so
    ///     interaction stays at vsync rate while a visible-but-idle window drops
    ///     to a few fps.
    ///
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
