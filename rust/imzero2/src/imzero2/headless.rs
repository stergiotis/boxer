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
//! - `IMZERO2_HEADLESS_PIXELS_PER_POINT` — initial HiDPI scale of the
//!   offscreen target (default 1.0). A connected viewer's reported
//!   viewport + pixel scale (devicePixelRatio × zoom) take over via
//!   ViewportResize; this only covers the pre-connect default.
//! - `IMZERO2_HEADLESS_H264_OUT` — when set, spawn the ffmpeg encoder
//!   (ADR-0024 SD3) and append the raw Annex-B H.264 byte stream to this
//!   file. Phase 2 verification target; the Phase 4/5 WebSocket carrier
//!   replaces the file writer with a broadcaster.
//! - `IMZERO2_HEADLESS_ENCODER_ARGS` — whitespace-split override of the
//!   encode arguments between the rawvideo input and the `-f h264` output.
//!   Default mirrors ImZero1 (SD3): VAAPI hwupload + `h264_vaapi -bf 0
//!   -qp:v 26 -g 100000`. Software fallback for boxes without VAAPI
//!   encode: `-c:v libopenh264 -rc_mode off -bf 0 -g 100000` (constant
//!   quality + infinite GOP — measured frame-stable, see
//!   DEFAULT_ENCODER_ARGS).
//! - `IMZERO2_HEADLESS_LISTEN` — bind address for the WebSocket carrier
//!   (e.g. `127.0.0.1:8089`); the embedded browser viewer page is served
//!   over HTTP on port+1. Unset = remote access disabled.

use crate::imzero2::appconfig::AppConfig;
use crate::imzero2::apphost;
use crate::imzero2::codeclane::{CodecLane, VideoCodec};
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
    lane: CodecLane,
    /// WebSocket carrier bind address (e.g. "127.0.0.1:8089"); the viewer
    /// page is served on port+1. None = carrier disabled.
    listen: Option<String>,
}

/// ADR-0024 SD3: encoder arguments mirror ImZero1's validated
/// configuration — VAAPI hardware encode, no B-frames (latency), constant
/// QP 26 — plus an effectively infinite GOP: periodic IDR refresh
/// re-quantizes the whole (mostly static) screen every interval, which
/// reads as a visible color pulse (measured RMSE 316 at each IDR vs 0
/// between P-frames, 2026-06-12). Every viewer connection starts a fresh
/// encoder (= leading IDR), and the viewer reconnects on decode errors,
/// so mid-stream key frames buy nothing here. Overridable via
/// IMZERO2_HEADLESS_ENCODER_ARGS.
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
    "-g",
    "100000",
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
        Self {
            fps: parse("IMZERO2_HEADLESS_FPS", 60.0f32).clamp(1.0, 240.0),
            max_frames: parse("IMZERO2_HEADLESS_MAX_FRAMES", 0u64),
            dump_dir: path_var("IMZERO2_HEADLESS_DUMP_DIR"),
            dump_every: parse("IMZERO2_HEADLESS_DUMP_EVERY", 60u64).max(1),
            pixels_per_point: parse("IMZERO2_HEADLESS_PIXELS_PER_POINT", 1.0f32).clamp(0.25, 4.0),
            h264_out: path_var("IMZERO2_HEADLESS_H264_OUT"),
            lane: build_codec_lane(),
            listen: std::env::var("IMZERO2_HEADLESS_LISTEN")
                .ok()
                .filter(|v| !v.is_empty()),
        }
    }
}

/// Select the startup codec lane (ADR-0088 SD4). `IMZERO2_HEADLESS_CODEC`
/// picks `h264` | `vp9` | `av1`; the runtime switch is a later phase. For
/// H.264 the legacy `IMZERO2_HEADLESS_ENCODER_ARGS` override (or the VAAPI
/// `DEFAULT_ENCODER_ARGS`) still applies, so an existing deployment with no
/// codec var set behaves exactly as before.
fn build_codec_lane() -> CodecLane {
    fn h264_args() -> Vec<String> {
        std::env::var("IMZERO2_HEADLESS_ENCODER_ARGS")
            .ok()
            .filter(|v| !v.trim().is_empty())
            .map(|v| v.split_whitespace().map(str::to_owned).collect())
            .unwrap_or_else(|| DEFAULT_ENCODER_ARGS.iter().map(|s| (*s).to_owned()).collect())
    }
    match std::env::var("IMZERO2_HEADLESS_CODEC")
        .ok()
        .and_then(|v| VideoCodec::parse(&v))
    {
        Some(VideoCodec::Vp9) => CodecLane::software(VideoCodec::Vp9),
        Some(VideoCodec::Av1) => CodecLane::software(VideoCodec::Av1),
        Some(VideoCodec::H264) | None => CodecLane::h264(h264_args()),
    }
}

/// ADR-0088: combine the host-encode probe with the viewer's reported decode
/// capabilities into the (codecId, flags) packing `fetchVideoCapabilities`
/// hands to Go. codecId 0=H.264, 1=VP9, 2=AV1; flags bit0=host-encode,
/// bit1=decode-supported, bit2=smooth, bit3=power-efficient.
fn build_video_caps(
    host: &[(VideoCodec, bool)],
    decode: Option<&crate::imzero2::inputproto::DecodeCapabilities>,
) -> Vec<(u8, u32)> {
    let codec_id = |c: VideoCodec| match c {
        VideoCodec::H264 => 0u8,
        VideoCodec::Vp9 => 1,
        VideoCodec::Av1 => 2,
    };
    let mut out: Vec<(u8, u32)> = host
        .iter()
        .map(|&(c, enc)| (codec_id(c), u32::from(enc)))
        .collect();
    if let Some(dc) = decode {
        for cap in &dc.codecs {
            let id = if cap.codec.starts_with("avc") {
                0u8
            } else if cap.codec.starts_with("vp9") || cap.codec.starts_with("vp09") {
                1
            } else if cap.codec.starts_with("av1") || cap.codec.starts_with("av01") {
                2
            } else {
                continue;
            };
            if let Some(entry) = out.iter_mut().find(|(cid, _)| *cid == id) {
                if cap.supported {
                    entry.1 |= 2;
                }
                if cap.smooth {
                    entry.1 |= 4;
                }
                if cap.power_efficient {
                    entry.1 |= 8;
                }
            }
        }
    }
    out
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
    let renderer = egui_wgpu::Renderer::new(
        &device,
        TARGET_FORMAT,
        egui_wgpu::RendererOptions {
            // Dithering exists to hide banding on a directly-viewed
            // display; here the frames feed a video encoder, where the
            // sub-quantization noise only adds bitrate and re-encode
            // shimmer on gradients (window shadows). The desktop host
            // keeps eframe's default (on).
            dithering: false,
            ..Default::default()
        },
    );
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
    /// Consume a pass's texture deltas without rendering. Used when no
    /// sink wants pixels (no viewer, no dump): deltas are incremental, so
    /// dropping them would permanently corrupt the renderer's texture
    /// state for a viewer that connects later; the render pass and the
    /// readback are the only parts that can be skipped.
    fn apply_textures_only(&mut self, out: egui::FullOutput) {
        for (id, delta) in &out.textures_delta.set {
            self.renderer.update_texture(&self.device, &self.queue, *id, delta);
        }
        for id in &out.textures_delta.free {
            self.renderer.free_texture(id);
        }
    }

    /// Recreate the offscreen target and staging buffer at a new physical
    /// size (viewport resize). Device, queue and the egui renderer carry
    /// over — the renderer is size-agnostic (the per-frame
    /// `ScreenDescriptor` carries dimensions) and its texture cache
    /// survives, so only the attachments are rebuilt.
    fn resize(&mut self, width_px: u32, height_px: u32) {
        self.extent = wgpu::Extent3d {
            width: width_px,
            height: height_px,
            depth_or_array_layers: 1,
        };
        self.texture = self.device.create_texture(&wgpu::TextureDescriptor {
            label: Some("imzero2_headless_target"),
            size: self.extent,
            mip_level_count: 1,
            sample_count: 1,
            dimension: wgpu::TextureDimension::D2,
            format: TARGET_FORMAT,
            usage: wgpu::TextureUsages::RENDER_ATTACHMENT | wgpu::TextureUsages::COPY_SRC,
            view_formats: &[],
        });
        self.view = self.texture.create_view(&wgpu::TextureViewDescriptor::default());
        self.unpadded_bytes_per_row = width_px * 4;
        self.padded_bytes_per_row =
            wgpu::util::align_to(self.unpadded_bytes_per_row, wgpu::COPY_BYTES_PER_ROW_ALIGNMENT);
        self.readback = self.device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("imzero2_headless_readback"),
            size: self.padded_bytes_per_row as u64 * height_px as u64,
            usage: wgpu::BufferUsages::COPY_DST | wgpu::BufferUsages::MAP_READ,
            mapped_at_creation: false,
        });
    }

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

/// Render cadence of the headless host (ADR-0062 brought to ADR-0024's
/// host). Initial mode comes from IMZERO2_RENDER_CADENCE — the same
/// variable the Go-side decorator reads at startup — and can be switched
/// at runtime per the wire's SetCadence message.
///
/// Continuous: fixed tick at the configured fps (the original behavior).
/// Reactive: a pass runs when egui schedules a repaint (animations,
/// caret blink, Go-side RequestRepaint opcodes), when wire activity
/// arrives (input, resize, connect), or at a 1 s idle heartbeat —
/// whichever is soonest, floored by the fps cap. Note: full idle savings
/// require the *server launched* reactive, because the Go decorator
/// requests an immediate repaint every frame in continuous mode and a
/// runtime switch cannot reach it; in that configuration reactive still
/// avoids encoding (frame dedup) but not rendering.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
enum Cadence {
    Continuous,
    Reactive,
}

/// Idle repaint cadence in reactive mode: the only path by which
/// Go-side data changes surface when egui itself is idle (mirrors the
/// desktop host's heartbeat).
const IDLE_HEARTBEAT: std::time::Duration = std::time::Duration::from_secs(1);

/// Sleep until `deadline`, returning early — but never before
/// `min_next` (the fps cap) — when the carrier signals activity.
/// Pending wake tokens are drained so a burst counts once.
fn sleep_until_or_wake(
    deadline: std::time::Instant,
    min_next: std::time::Instant,
    waker: &std::sync::mpsc::Receiver<()>,
) {
    loop {
        let now = std::time::Instant::now();
        if now >= deadline {
            return;
        }
        match waker.recv_timeout(deadline - now) {
            Ok(()) => {
                while waker.try_recv().is_ok() {}
                let now = std::time::Instant::now();
                if now < min_next {
                    std::thread::sleep(min_next - now);
                }
                return;
            }
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => return,
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                // No carrier holds a sender (host keeps one, so this is
                // defensive): plain sleep.
                std::thread::sleep(deadline - now);
                return;
            }
        }
    }
}

/// Validate and clamp a viewer resize request into applicable physical
/// geometry. Returns None when the request is invalid or changes nothing.
fn clamp_resize(
    req: &crate::imzero2::inputproto::ViewportResize,
    max_texture_side: usize,
    cur_w: u32,
    cur_h: u32,
    cur_ppp: f32,
) -> Option<(u32, u32, f32)> {
    if !(req.logical_width.is_finite() && req.logical_height.is_finite() && req.pixel_scale.is_finite()) {
        return None;
    }
    let ppp = req.pixel_scale.clamp(0.25, 4.0);
    let max_side = (max_texture_side as u32).min(8192);
    let w = even_up(((req.logical_width.max(1.0) * ppp).round() as u32).clamp(16, max_side));
    let h = even_up(((req.logical_height.max(1.0) * ppp).round() as u32).clamp(16, max_side));
    if w == cur_w && h == cur_h && (ppp - cur_ppp).abs() < 0.001 {
        return None;
    }
    Some((w, h, ppp))
}

pub fn run_main_loop(config: AppConfig) -> Result<(), HeadlessError> {
    let opts = HeadlessOpts::from_env();
    // Initial geometry only: a connected viewer's reported viewport and
    // pixel scale take over via ViewportResize (clamped per tick below).
    let mut ppp = opts.pixels_per_point;
    let mut width_px = even_up(((config.initial_main_window_width * ppp).round() as u32).max(2));
    let mut height_px = even_up(((config.initial_main_window_height * ppp).round() as u32).max(2));
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
    let mut screen = egui_wgpu::ScreenDescriptor {
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
    let mut cadence = if reactive { Cadence::Reactive } else { Cadence::Continuous };
    tracing::info!(?cadence, "render cadence (IMZERO2_RENDER_CADENCE; runtime-switchable via the wire)");

    let mut sinks: Vec<Box<dyn FrameSink>> = Vec::new();
    if let Some(dir) = &opts.dump_dir {
        sinks.push(Box::new(PngDumpSink::new(dir.clone(), opts.dump_every)?));
    }
    if let Some(out) = &opts.h264_out {
        sinks.push(Box::new(EncoderSink::new(
            width_px,
            height_px,
            opts.fps,
            opts.lane.clone(),
            EncoderTarget::File(out.clone()),
        )?));
    }
    // The waker lets carrier activity (input, resize, cadence change,
    // connect) interrupt a reactive sleep; the host keeps one sender so
    // the channel never disconnects.
    let (waker_tx, waker_rx) = std::sync::mpsc::channel::<()>();
    let _waker_keepalive = waker_tx.clone();
    // The WebSocket carrier (ADR-0024 Phases 4–5) owns the per-connection
    // encoder so the stream starts at an IDR for every viewer and nothing
    // is encoded while nobody watches.
    let mut carrier: Option<WsCarrier> = match &opts.listen {
        Some(listen) => Some(WsCarrier::start(
            listen,
            width_px,
            height_px,
            ppp,
            cadence as u32,
            opts.fps,
            opts.lane.clone(),
            waker_tx,
        )?),
        None => None,
    };
    // SD5: probe which codecs this host can actually encode (a real
    // probe-encode, not a listing — catches VAAPI ENOSYS). Fed to the Go
    // control via fetchVideoCapabilities so unavailable codecs aren't offered.
    let host_encode_caps = crate::imzero2::codeclane::probe_host_encode();
    tracing::info!(
        encodable = ?host_encode_caps.iter().filter(|(_, ok)| *ok).map(|(c, _)| c.as_str()).collect::<Vec<_>>(),
        "host video-encode probe"
    );
    if sinks.is_empty() && carrier.is_none() {
        sinks.push(Box::new(NullSink));
    }

    let frame_dt = std::time::Duration::from_secs_f32(1.0 / opts.fps);
    let start = std::time::Instant::now();
    let mut next_deadline = std::time::Instant::now();
    // Reactive bookkeeping: when the last pass ran, and how soon egui
    // asked to be run again (repaint_delay of the last pass).
    let mut last_pass = std::time::Instant::now() - frame_dt;
    let mut repaint_hint = std::time::Duration::ZERO;
    let mut frame_idx: u64 = 0;
    let mut bgra_frame: Vec<u8> = Vec::new();
    let mut translator = InputTranslator::default();
    let mut wire_events: Vec<crate::imzero2::inputproto::input_event::Event> = Vec::new();
    let mut egui_events: Vec<egui::Event> = Vec::new();
    let mut screen_rect = egui::Rect::from_min_size(
        egui::Pos2::ZERO,
        egui::vec2(width_px as f32 / ppp, height_px as f32 / ppp),
    );

    loop {
        match cadence {
            Cadence::Continuous => {
                let now = std::time::Instant::now();
                if now < next_deadline {
                    // Fixed tick: wake tokens are drained but do not move
                    // the deadline (predictable cadence).
                    sleep_until_or_wake(next_deadline, next_deadline, &waker_rx);
                }
                next_deadline += frame_dt;
                if next_deadline < std::time::Instant::now() {
                    // Fell behind (slow frame); skip missed ticks instead of bursting.
                    next_deadline = std::time::Instant::now();
                }
            }
            Cadence::Reactive => {
                // Next pass when egui asked for one (animations, scheduled
                // repaints, Go-side RequestRepaint) or at the idle
                // heartbeat — whichever is sooner, floored by the fps cap.
                // Wire activity (input/resize/connect) wakes us early.
                let min_next = last_pass + frame_dt;
                let deadline = (last_pass + repaint_hint.min(IDLE_HEARTBEAT)).max(min_next);
                sleep_until_or_wake(deadline, min_next, &waker_rx);
                // Keep the continuous anchor fresh so a runtime switch
                // doesn't burst to catch up.
                next_deadline = std::time::Instant::now() + frame_dt;
            }
        }
        last_pass = std::time::Instant::now();

        // Runtime cadence switch (wire SetCadence — the viewer's toggle).
        if let Some(c) = &mut carrier {
            if let Some(req) = c.take_cadence() {
                let new = if req == 1 { Cadence::Reactive } else { Cadence::Continuous };
                if new != cadence {
                    cadence = new;
                    c.set_hello_cadence(cadence as u32);
                    next_deadline = std::time::Instant::now();
                    tracing::info!(?cadence, "render cadence switched at runtime");
                }
            }
        }

        // Viewport resize: apply the viewer's reported geometry — rebuild
        // the offscreen target, update egui's scale, re-announce the hello
        // and restart the encoder (fresh SPS/PPS + IDR at the new size).
        if let Some(c) = &mut carrier {
            if let Some(req) = c.take_resize() {
                if let Some((nw, nh, nppp)) = clamp_resize(&req, gpu.max_texture_side, width_px, height_px, ppp) {
                    gpu.resize(nw, nh);
                    width_px = nw;
                    height_px = nh;
                    ppp = nppp;
                    screen = egui_wgpu::ScreenDescriptor {
                        size_in_pixels: [nw, nh],
                        pixels_per_point: nppp,
                    };
                    screen_rect = egui::Rect::from_min_size(
                        egui::Pos2::ZERO,
                        egui::vec2(nw as f32 / nppp, nh as f32 / nppp),
                    );
                    c.apply_geometry(nw, nh, nppp);
                    tracing::info!(width_px = nw, height_px = nh, pixels_per_point = nppp, "viewport resize applied");
                }
            }
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
        // ADR-0088: publish current video-pipeline capabilities for the Go
        // control to fetch while it builds this frame (must precede dispatch).
        if let Some(c) = &carrier {
            // Publish capabilities only while a viewer is connected, so the Go
            // control self-hides when there is no remote sink.
            let caps = if c.connected() {
                build_video_caps(&host_encode_caps, c.decode_caps().as_ref())
            } else {
                Vec::new()
            };
            fffi.set_video_capabilities(&caps);
        }
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
        repaint_hint = out
            .viewport_output
            .get(&egui::ViewportId::ROOT)
            .map(|v| v.repaint_delay)
            .unwrap_or(std::time::Duration::ZERO);

        // ADR-0088: apply a runtime codec switch the Go control requested
        // (drained after dispatch; the carrier re-points the encoder).
        if let Some(req) = fffi.take_video_pipeline_request() {
            if let Some(c) = &mut carrier {
                c.set_video_codec(VideoCodec::from_u8(req));
                next_deadline = std::time::Instant::now();
            }
        }
        let need_pixels = !sinks.is_empty()
            || carrier.as_ref().map(WsCarrier::connected).unwrap_or(false);
        if need_pixels {
            gpu.render_and_readback(&ctx, out, &screen, &mut bgra_frame)?;
            for sink in &mut sinks {
                sink.on_frame(&bgra_frame, width_px, height_px, frame_idx);
            }
            if let Some(c) = &mut carrier {
                c.on_frame(&bgra_frame, width_px, height_px, frame_idx);
            }
        } else {
            // Nobody consumes pixels: skip tessellation, the render pass
            // and the readback, but keep the renderer's texture state
            // current. The empty on_frame lets the carrier reap a
            // just-disconnected session's encoder promptly (a race to
            // connected here only feeds a zero-length frame, which the
            // encoder ignores).
            gpu.apply_textures_only(out);
            if let Some(c) = &mut carrier {
                c.on_frame(&[], width_px, height_px, frame_idx);
            }
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
