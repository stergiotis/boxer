//! Headless render host (ADR-0024 SD2, Phase 1).
//!
//! Drives the FFFI2 interpreter against a hand-rolled `egui::Context` +
//! `egui_wgpu::Renderer` loop instead of eframe: wgpu Instance/Device/Queue
//! without a Surface, an offscreen BGRA texture
//! (`RENDER_ATTACHMENT | COPY_SRC`), per-frame GPU→CPU readback through a
//! 256-byte-row-aligned staging buffer, and a [`FrameSink`] consuming the
//! resulting tightly-packed BGRA frames. eframe's other responsibilities
//! (HiDPI, multi-monitor, persistence, viewport lifecycle) are non-issues
//! server-side and are not re-implemented.
//!
//! Sinks: PNG dumps (verification), the ffmpeg encoder to a file
//! ([`crate::imzero2::encoderpipe`], SD3), and the WebSocket carrier with
//! its embedded browser viewer ([`crate::imzero2::wscarrier`], SD4/SD6).
//! The interpreter and all context setup are shared verbatim with the
//! desktop host via [`apphost::init_common`].
//!
//! Configuration rides env vars (precedent: IMZERO2_RENDER_CADENCE,
//! IMZERO2_SCREENSHOT_DIR — the Go launcher inherits its environment to
//! the client process, so no Go-side flag plumbing is needed at v1):
//!
//! - `IMZERO2_HEADLESS_FPS` — render cadence in Hz (default 60). The
//!   FFFI2 stream is consumed once per tick; Go's lockstep protocol is
//!   paced by this clock in place of eframe's vsync.
//! - `IMZERO2_HEADLESS_MAX_FRAMES` — stop after N frames (0 = unbounded;
//!   smoke-test hook).
//! - `IMZERO2_HEADLESS_DUMP_DIR` — when set, dump rendered frames as
//!   `frame_NNNNNN.png` into this directory.
//! - `IMZERO2_HEADLESS_DUMP_EVERY` — dump every Nth frame (default 60).
//! - `IMZERO2_HEADLESS_PIXELS_PER_POINT` — HiDPI scale of the offscreen
//!   target (default 1.0). ADR-0024 SD7/SD8: once the protobuf input
//!   channel exists the client's devicePixelRatio replaces this.
//! - `IMZERO2_HEADLESS_H264_OUT` — when set, spawn the ffmpeg encoder
//!   (ADR-0024 SD3) and append the raw Annex-B H.264 byte stream to this
//!   file. Phase 2 verification target; the Phase 4/5 WebSocket carrier
//!   replaces the file writer with a broadcaster.
//! - `IMZERO2_HEADLESS_ENCODER_ARGS` — whitespace-split override of the
//!   encode arguments between the rawvideo input and the `-f h264` output.
//!   Default mirrors ImZero1 (SD3): VAAPI hwupload + `h264_vaapi -bf 0
//!   -qp:v 26`. Example software fallback for boxes without VAAPI encode:
//!   `-c:v libopenh264 -b:v 4M -bf 0`.
//! - `IMZERO2_HEADLESS_LISTEN` — bind address for the WebSocket carrier
//!   (e.g. `127.0.0.1:8089`); the embedded browser viewer page is served
//!   over HTTP on port+1. Unset = remote access disabled.

use crate::imzero2::appconfig::AppConfig;
use crate::imzero2::apphost;
use crate::imzero2::encoderpipe::{EncoderSink, EncoderTarget};
use crate::imzero2::framesink::{FrameSink, NullSink, PngDumpSink};
use crate::imzero2::inputmap::InputTranslator;
use crate::imzero2::interpreter::InterpretError;
use crate::imzero2::wscarrier::WsCarrier;

#[derive(thiserror::Error, Debug)]
pub enum HeadlessError {
    #[error("no wgpu adapter available: {0}")]
    Adapter(#[from] wgpu::RequestAdapterError),
    #[error("wgpu device request failed: {0}")]
    Device(#[from] wgpu::RequestDeviceError),
    #[error("wgpu poll failed: {0}")]
    Poll(#[from] wgpu::PollError),
    #[error("readback buffer mapping failed: {0}")]
    BufferMap(#[from] wgpu::BufferAsyncError),
    #[error("readback channel closed before map completed")]
    MapChannelClosed,
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
}

/// Env-var configuration of the headless host. See the module doc.
struct HeadlessOpts {
    fps: f32,
    max_frames: u64,
    dump_dir: Option<std::path::PathBuf>,
    dump_every: u64,
    pixels_per_point: f32,
    h264_out: Option<std::path::PathBuf>,
    encoder_args: Vec<String>,
    /// WebSocket carrier bind address (e.g. "127.0.0.1:8089"); the viewer
    /// page is served on port+1. None = carrier disabled.
    listen: Option<String>,
}

/// ADR-0024 SD3: encoder arguments mirror ImZero1's validated
/// configuration — VAAPI hardware encode, no B-frames (latency), constant
/// QP 26. Overridable via IMZERO2_HEADLESS_ENCODER_ARGS.
const DEFAULT_ENCODER_ARGS: &[&str] = &[
    "-vaapi_device",
    "/dev/dri/renderD128",
    "-vf",
    "format=nv12,hwupload",
    "-c:v",
    "h264_vaapi",
    "-bf",
    "0",
    "-qp:v",
    "26",
];

impl HeadlessOpts {
    fn from_env() -> Self {
        fn parse<T: std::str::FromStr>(name: &str, default: T) -> T {
            std::env::var(name)
                .ok()
                .and_then(|v| v.parse::<T>().ok())
                .unwrap_or(default)
        }
        fn path_var(name: &str) -> Option<std::path::PathBuf> {
            std::env::var(name)
                .ok()
                .filter(|v| !v.is_empty())
                .map(std::path::PathBuf::from)
        }
        let encoder_args = std::env::var("IMZERO2_HEADLESS_ENCODER_ARGS")
            .ok()
            .filter(|v| !v.trim().is_empty())
            .map(|v| v.split_whitespace().map(str::to_owned).collect())
            .unwrap_or_else(|| DEFAULT_ENCODER_ARGS.iter().map(|s| (*s).to_owned()).collect());
        Self {
            fps: parse("IMZERO2_HEADLESS_FPS", 60.0f32).clamp(1.0, 240.0),
            max_frames: parse("IMZERO2_HEADLESS_MAX_FRAMES", 0u64),
            dump_dir: path_var("IMZERO2_HEADLESS_DUMP_DIR"),
            dump_every: parse("IMZERO2_HEADLESS_DUMP_EVERY", 60u64).max(1),
            pixels_per_point: parse("IMZERO2_HEADLESS_PIXELS_PER_POINT", 1.0f32).clamp(0.25, 4.0),
            h264_out: path_var("IMZERO2_HEADLESS_H264_OUT"),
            encoder_args,
            listen: std::env::var("IMZERO2_HEADLESS_LISTEN")
                .ok()
                .filter(|v| !v.is_empty()),
        }
    }
}

/// Offscreen wgpu state: device/queue, render target, the reused staging
/// buffer for readback, and the egui renderer.
struct Gpu {
    device: wgpu::Device,
    queue: wgpu::Queue,
    texture: wgpu::Texture,
    view: wgpu::TextureView,
    renderer: egui_wgpu::Renderer,
    readback: wgpu::Buffer,
    extent: wgpu::Extent3d,
    unpadded_bytes_per_row: u32,
    padded_bytes_per_row: u32,
    max_texture_side: usize,
}

/// Render target format. `Bgra8Unorm` matches the common native surface
/// format (egui_wgpu selects its gamma-aware shader path from it), so the
/// readback bytes are sRGB-encoded BGRA — exactly what both the PNG dump
/// and the future `rawvideo -pix_fmt bgra` encoder input (ADR-0024 SD3)
/// expect.
const TARGET_FORMAT: wgpu::TextureFormat = wgpu::TextureFormat::Bgra8Unorm;

fn init_gpu(width_px: u32, height_px: u32) -> Result<Gpu, HeadlessError> {
    let instance = wgpu::Instance::new(wgpu::InstanceDescriptor {
        backends: wgpu::Backends::from_env().unwrap_or(wgpu::Backends::PRIMARY | wgpu::Backends::GL),
        flags: wgpu::InstanceFlags::from_build_config().with_env(),
        backend_options: wgpu::BackendOptions::from_env_or_default(),
        memory_budget_thresholds: wgpu::MemoryBudgetThresholds::default(),
        // No display handle: headless offscreen rendering needs none (and
        // having none is what keeps winit out of this build).
        display: None,
    });
    let adapter = pollster::block_on(instance.request_adapter(&wgpu::RequestAdapterOptions {
        power_preference: wgpu::PowerPreference::from_env()
            .unwrap_or(wgpu::PowerPreference::HighPerformance),
        compatible_surface: None,
        force_fallback_adapter: false,
    }))?;
    let info = adapter.get_info();
    tracing::info!(name=%info.name, backend=?info.backend, device_type=?info.device_type, "headless wgpu adapter");
    let (device, queue) = pollster::block_on(adapter.request_device(&wgpu::DeviceDescriptor {
        label: Some("imzero2_headless"),
        ..Default::default()
    }))?;
    let extent = wgpu::Extent3d {
        width: width_px,
        height: height_px,
        depth_or_array_layers: 1,
    };
    let texture = device.create_texture(&wgpu::TextureDescriptor {
        label: Some("imzero2_headless_target"),
        size: extent,
        mip_level_count: 1,
        sample_count: 1,
        dimension: wgpu::TextureDimension::D2,
        format: TARGET_FORMAT,
        usage: wgpu::TextureUsages::RENDER_ATTACHMENT | wgpu::TextureUsages::COPY_SRC,
        view_formats: &[],
    });
    let view = texture.create_view(&wgpu::TextureViewDescriptor::default());
    let renderer = egui_wgpu::Renderer::new(&device, TARGET_FORMAT, egui_wgpu::RendererOptions::default());
    let unpadded_bytes_per_row = width_px * 4;
    let padded_bytes_per_row =
        wgpu::util::align_to(unpadded_bytes_per_row, wgpu::COPY_BYTES_PER_ROW_ALIGNMENT);
    let readback = device.create_buffer(&wgpu::BufferDescriptor {
        label: Some("imzero2_headless_readback"),
        size: padded_bytes_per_row as u64 * height_px as u64,
        usage: wgpu::BufferUsages::COPY_DST | wgpu::BufferUsages::MAP_READ,
        mapped_at_creation: false,
    });
    let max_texture_side = device.limits().max_texture_dimension_2d as usize;
    Ok(Gpu {
        device,
        queue,
        texture,
        view,
        renderer,
        readback,
        extent,
        unpadded_bytes_per_row,
        padded_bytes_per_row,
        max_texture_side,
    })
}

impl Gpu {
    /// Tessellate + render one egui pass into the offscreen target, copy it
    /// to the staging buffer, and read it back as tightly-packed BGRA into
    /// `frame` (reused across calls).
    fn render_and_readback(
        &mut self,
        ctx: &egui::Context,
        out: egui::FullOutput,
        screen: &egui_wgpu::ScreenDescriptor,
        frame: &mut Vec<u8>,
    ) -> Result<(), HeadlessError> {
        for (id, delta) in &out.textures_delta.set {
            self.renderer.update_texture(&self.device, &self.queue, *id, delta);
        }
        let clipped = ctx.tessellate(out.shapes, out.pixels_per_point);
        let mut encoder = self
            .device
            .create_command_encoder(&wgpu::CommandEncoderDescriptor {
                label: Some("imzero2_headless_encoder"),
            });
        let user_cmds =
            self.renderer
                .update_buffers(&self.device, &self.queue, &mut encoder, &clipped, screen);
        {
            let mut pass = encoder
                .begin_render_pass(&wgpu::RenderPassDescriptor {
                    label: Some("imzero2_headless_pass"),
                    color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                        view: &self.view,
                        resolve_target: None,
                        ops: wgpu::Operations {
                            load: wgpu::LoadOp::Clear(wgpu::Color::BLACK),
                            store: wgpu::StoreOp::Store,
                        },
                        depth_slice: None,
                    })],
                    depth_stencil_attachment: None,
                    occlusion_query_set: None,
                    timestamp_writes: None,
                    multiview_mask: None,
                })
                .forget_lifetime();
            self.renderer.render(&mut pass, &clipped, screen);
        }
        encoder.copy_texture_to_buffer(
            self.texture.as_image_copy(),
            wgpu::TexelCopyBufferInfo {
                buffer: &self.readback,
                layout: wgpu::TexelCopyBufferLayout {
                    offset: 0,
                    bytes_per_row: Some(self.padded_bytes_per_row),
                    rows_per_image: None,
                },
            },
            self.extent,
        );
        for id in &out.textures_delta.free {
            self.renderer.free_texture(id);
        }
        self.queue
            .submit(user_cmds.into_iter().chain([encoder.finish()]));

        let slice = self.readback.slice(..);
        let (tx, rx) = std::sync::mpsc::channel();
        slice.map_async(wgpu::MapMode::Read, move |r| {
            let _ = tx.send(r);
        });
        self.device.poll(wgpu::PollType::wait_indefinitely())?;
        rx.recv().map_err(|_| HeadlessError::MapChannelClosed)??;
        {
            let data = slice.get_mapped_range();
            frame.clear();
            frame.reserve(self.unpadded_bytes_per_row as usize * self.extent.height as usize);
            for padded_row in data.chunks(self.padded_bytes_per_row as usize) {
                let row = padded_row
                    .get(..self.unpadded_bytes_per_row as usize)
                    .unwrap_or(padded_row);
                frame.extend_from_slice(row);
            }
        }
        self.readback.unmap();
        Ok(())
    }
}

/// Returns true when any viewport command requests a close — the
/// interpreter signals graceful shutdown this way (matches the desktop
/// host, where eframe acts on the same command).
fn close_requested(out: &egui::FullOutput) -> bool {
    out.viewport_output
        .values()
        .any(|vo| vo.commands.iter().any(|c| matches!(c, egui::ViewportCommand::Close)))
}

/// Round up to the next even number — H.264 4:2:0 (the Phase 2 encoder)
/// requires even frame dimensions; bake that in from the start so the
/// dumped frames and the future encoded stream agree.
fn even_up(v: u32) -> u32 {
    v + (v & 1)
}

pub fn run_main_loop(config: AppConfig) -> Result<(), HeadlessError> {
    let opts = HeadlessOpts::from_env();
    let ppp = opts.pixels_per_point;
    let width_px = even_up(((config.initial_main_window_width * ppp).round() as u32).max(2));
    let height_px = even_up(((config.initial_main_window_height * ppp).round() as u32).max(2));
    tracing::info!(
        args = ?config,
        width_px,
        height_px,
        pixels_per_point = ppp,
        fps = opts.fps,
        max_frames = opts.max_frames,
        dump_dir = ?opts.dump_dir,
        "headless host up and running (ADR-0024 Phase 1)"
    );

    let mut gpu = init_gpu(width_px, height_px)?;
    let screen = egui_wgpu::ScreenDescriptor {
        size_in_pixels: [width_px, height_px],
        pixels_per_point: ppp,
    };

    let ctx = egui::Context::default();
    let (mut fffi, reactive) = apphost::init_common(
        &ctx,
        &config,
        std::io::stdin().lock(),
        std::io::stdout().lock(),
    );
    if reactive {
        // SD9/ADR-0062: the headless host currently paces at a fixed tick;
        // reactive cadence (skip render+encode when nothing changed) is a
        // named follow-up, not v1. Record the request so nobody wonders.
        tracing::info!("IMZERO2_RENDER_CADENCE=reactive noted — headless v1 renders at a fixed tick; reactive cadence lands with SD9 pacing");
    }

    let mut sinks: Vec<Box<dyn FrameSink>> = Vec::new();
    if let Some(dir) = &opts.dump_dir {
        sinks.push(Box::new(PngDumpSink::new(dir.clone(), opts.dump_every)?));
    }
    if let Some(out) = &opts.h264_out {
        sinks.push(Box::new(EncoderSink::new(
            width_px,
            height_px,
            opts.fps,
            opts.encoder_args.clone(),
            EncoderTarget::File(out.clone()),
        )?));
    }
    // The WebSocket carrier (ADR-0024 Phases 4–5) is a sink too: it owns
    // the per-connection encoder so the stream starts at an IDR for every
    // viewer and nothing is encoded while nobody watches.
    let mut carrier: Option<WsCarrier> = match &opts.listen {
        Some(listen) => Some(WsCarrier::start(
            listen,
            width_px,
            height_px,
            ppp,
            opts.fps,
            opts.encoder_args.clone(),
        )?),
        None => None,
    };
    if sinks.is_empty() && carrier.is_none() {
        sinks.push(Box::new(NullSink));
    }

    let frame_dt = std::time::Duration::from_secs_f32(1.0 / opts.fps);
    let start = std::time::Instant::now();
    let mut next_deadline = std::time::Instant::now();
    let mut frame_idx: u64 = 0;
    let mut bgra_frame: Vec<u8> = Vec::new();
    let mut translator = InputTranslator::default();
    let mut wire_events: Vec<crate::imzero2::inputproto::input_event::Event> = Vec::new();
    let mut egui_events: Vec<egui::Event> = Vec::new();
    let screen_rect = egui::Rect::from_min_size(
        egui::Pos2::ZERO,
        egui::vec2(width_px as f32 / ppp, height_px as f32 / ppp),
    );

    loop {
        let now = std::time::Instant::now();
        if now < next_deadline {
            std::thread::sleep(next_deadline - now);
        }
        next_deadline += frame_dt;
        if next_deadline < std::time::Instant::now() {
            // Fell behind (slow frame); skip missed ticks instead of bursting.
            next_deadline = std::time::Instant::now();
        }

        // Remote input (ADR-0024 SD8): drain wire events from the carrier
        // and translate at the host edge; the interpreter sees ordinary
        // egui events.
        egui_events.clear();
        if let Some(c) = &mut carrier {
            wire_events.clear();
            c.drain_events(&mut wire_events);
            for ev in wire_events.drain(..) {
                translator.translate(ev, &mut egui_events);
            }
        }

        let mut raw_input = egui::RawInput {
            screen_rect: Some(screen_rect),
            max_texture_side: Some(gpu.max_texture_side),
            time: Some(start.elapsed().as_secs_f64()),
            predicted_dt: frame_dt.as_secs_f32(),
            focused: true,
            events: std::mem::take(&mut egui_events),
            modifiers: translator.modifiers,
            ..Default::default()
        };
        raw_input
            .viewports
            .entry(egui::ViewportId::ROOT)
            .or_default()
            .native_pixels_per_point = Some(ppp);

        let mut shutdown = false;
        // Mirrors eframe 0.34's epi_integration: `run_ui(raw_input, |ui| {
        // app.logic(ui.ctx(), ..) })` — the interpreter dispatches against
        // the live pass exactly as it does under the desktop host.
        let out = ctx.run_ui(raw_input, |ui| {
            if let Err(e) = fffi.interpret_commands_outer(ui.ctx()) {
                match e {
                    InterpretError::PeerClosed => {
                        tracing::info!("peer closed pipe — initiating graceful shutdown");
                        shutdown = true;
                    }
                    other => {
                        // Mirrors the desktop host: log and continue; transient
                        // vs. fatal classification is future field experience.
                        tracing::error!(error = %other, "interpret error during dispatch");
                    }
                }
            }
        });
        if close_requested(&out) {
            tracing::info!("viewport close requested — shutting down headless host");
            shutdown = true;
        }

        gpu.render_and_readback(&ctx, out, &screen, &mut bgra_frame)?;
        for sink in &mut sinks {
            sink.on_frame(&bgra_frame, width_px, height_px, frame_idx);
        }
        if let Some(c) = &mut carrier {
            c.on_frame(&bgra_frame, width_px, height_px, frame_idx);
        }
        frame_idx += 1;

        if shutdown {
            break;
        }
        if opts.max_frames > 0 && frame_idx >= opts.max_frames {
            tracing::info!(frames = frame_idx, "IMZERO2_HEADLESS_MAX_FRAMES reached — exiting");
            break;
        }
    }
    tracing::info!(frames = frame_idx, elapsed_s = start.elapsed().as_secs_f64(), "headless host done");
    Ok(())
}
