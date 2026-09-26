//! The browser host: the shared FFFI2 interpreter and egui, tessellating
//! into the ADR-0128 mesh wire, driven from a JS worker that owns the bytes
//! (ADR-0077 Phase 1; the keelson-wasm-frame-cost trial's M2 arm).
//!
//! Nothing here blocks. The Go module's fd 1 bytes arrive through
//! [`host_write`]; when Go blocks on an fd 0 read the worker calls
//! [`host_step`], which interprets what has arrived and queues the reply
//! Go is waiting for. The lock-step protocol makes that sufficient: Go
//! only blocks on a fetcher, fetchers run only from `StateManager.Sync`, and
//! by then the whole frame has been written (the channel flushes before a
//! blocking read). So at every step the unread bytes are either one fetch
//! message, or a complete frame followed by its first fetch. A frame runs an
//! egui pass; a fetch is answered from the interpreter's registers outside
//! any pass, which the dispatcher allows since a fetcher never touches a
//! `Ui`.
//!
//! The pass's shapes are tessellated and serialized exactly as the mesh
//! appliance does (`meshlane`), so the existing viewer page paints them
//! once the worker posts the bytes to it. Bodies are deduplicated against
//! what the painter holds, which after each frame's retirement is that
//! frame's bodies.
//!
//! Exports are a plain C ABI: the host copies bytes into a scratch buffer
//! it gets from [`host_alloc`] and reads results from pointers it asks for.
#![allow(unsafe_code)]

use std::cell::RefCell;
use std::collections::HashSet;
use std::io::{BufRead, Read, Write};

use crate::imzero2::appconfig::AppConfig;
use crate::imzero2::apphost;
use crate::imzero2::clock::Instant;
use crate::imzero2::enums_out::FuncProcId;
use crate::imzero2::inputmap::InputTranslator;
use crate::imzero2::inputproto as pb;
use crate::imzero2::interpreter::ImZeroFffi;
use crate::imzero2::meshlane;
use prost::Message as _;

const MAX_TEXTURE_SIDE: usize = 8192;
const NOMINAL_DT: f32 = 1.0 / 60.0;

/// The interpreter's reader: whole messages the host admitted, read to a
/// boundary. Empty, it reports `WouldBlock`, which the interpreter maps to
/// "nothing more to interpret now".
#[derive(Default)]
pub struct Inbox {
    buf: Vec<u8>,
    pos: usize,
}

impl Inbox {
    fn push(&mut self, bytes: &[u8]) {
        if self.pos == self.buf.len() {
            self.buf.clear();
            self.pos = 0;
        }
        self.buf.extend_from_slice(bytes);
    }
    fn is_empty(&self) -> bool {
        self.pos >= self.buf.len()
    }
}

impl Read for Inbox {
    fn read(&mut self, out: &mut [u8]) -> std::io::Result<usize> {
        let avail = self.fill_buf()?;
        let n = avail.len().min(out.len());
        out[..n].copy_from_slice(&avail[..n]);
        self.consume(n);
        Ok(n)
    }
}

impl BufRead for Inbox {
    fn fill_buf(&mut self) -> std::io::Result<&[u8]> {
        if self.is_empty() {
            return Err(std::io::Error::from(std::io::ErrorKind::WouldBlock));
        }
        Ok(&self.buf[self.pos..])
    }
    fn consume(&mut self, n: usize) {
        self.pos = (self.pos + n).min(self.buf.len());
    }
}

/// The interpreter's writer: fetch replies, drained by the host.
#[derive(Default)]
pub struct Outbox(Vec<u8>);

impl Write for Outbox {
    fn write(&mut self, b: &[u8]) -> std::io::Result<usize> {
        self.0.extend_from_slice(b);
        Ok(b.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

/// Counters the host reads back.
#[derive(Default, Clone, Copy)]
pub struct Stats {
    pub frames: u64,
    pub bytes_in: u64,
    pub mesh_bytes: u64,
    pub last_interpret_us: u64,
    pub last_tessellate_us: u64,
    pub last_serialize_us: u64,
    pub last_bodies: u64,
    pub last_bodies_sent: u64,
    pub errors: u64,
}

pub struct Host {
    ctx: egui::Context,
    fffi: ImZeroFffi<'static, Inbox, Outbox>,
    /// raw bytes from the producer not yet handed to the interpreter
    pending: Vec<u8>,
    fetch_ops: HashSet<u32>,
    textures: meshlane::TextureStore,
    /// bodies the painter holds: the last frame's, after its retirement
    painter_has: HashSet<u64>,
    /// mesh messages of the frames rendered since the host last cleared them,
    /// one entry per wire message (texture, frame, retirement), so the page
    /// can hand each to the painter as the carrier would over a socket
    mesh_out: Vec<Vec<u8>>,
    /// the page's input, translated as the carrier translates it, waiting
    /// for the next pass
    translator: InputTranslator,
    pending_events: Vec<egui::Event>,
    width: f32,
    height: f32,
    ppp: f32,
    start: Instant,
    last_error: String,
    pub stats: Stats,
}

impl Host {
    pub fn new(width: f32, height: f32, ppp: f32) -> Self {
        let ctx = egui::Context::default();
        let config = AppConfig {
            initial_main_window_width: width,
            initial_main_window_height: height,
            ..AppConfig::default()
        };
        let (fffi, _reactive) =
            apphost::init_common(&ctx, &config, Inbox::default(), Outbox::default());
        let mut fetch_ops = HashSet::new();
        for raw in 0..1024u32 {
            if let Some(id) = FuncProcId::from_repr(raw)
                && format!("{id:?}").starts_with("Fetch")
            {
                fetch_ops.insert(raw);
            }
        }
        Self {
            ctx,
            fffi,
            pending: Vec::new(),
            fetch_ops,
            textures: meshlane::TextureStore::default(),
            painter_has: HashSet::new(),
            mesh_out: Vec::new(),
            translator: InputTranslator::default(),
            pending_events: Vec::new(),
            width,
            height,
            ppp,
            start: Instant::now(),
            last_error: String::new(),
            stats: Stats::default(),
        }
    }

    pub fn write(&mut self, bytes: &[u8]) {
        self.stats.bytes_in += bytes.len() as u64;
        self.pending.extend_from_slice(bytes);
    }

    /// Takes one `InputEvent` as the viewer page encodes it (the wire's
    /// `0x02` prefix already stripped) and queues its egui events for the
    /// next pass.
    pub fn input(&mut self, payload: &[u8]) -> bool {
        match pb::InputEvent::decode(payload) {
            Ok(msg) => {
                if let Some(ev) = msg.event {
                    self.translator.translate(ev, &mut self.pending_events);
                }
                true
            }
            Err(e) => {
                self.stats.errors += 1;
                self.last_error = format!("input: {e}");
                false
            }
        }
    }

    /// Returns the byte length of the complete messages at the front of
    /// `pending` and, for the last of them, its opcode.
    fn complete_messages(&self) -> (usize, Option<u32>) {
        let b = &self.pending;
        let mut off = 0usize;
        let mut last_op = None;
        while b.len() - off >= 4 {
            let len = u32::from_ne_bytes([b[off], b[off + 1], b[off + 2], b[off + 3]]) as usize;
            if b.len() - off < 4 + len {
                break;
            }
            if len >= 4 {
                last_op = Some(u32::from_ne_bytes([
                    b[off + 4],
                    b[off + 5],
                    b[off + 6],
                    b[off + 7],
                ]));
            }
            off += 4 + len;
        }
        (off, last_op)
    }

    /// Interprets what has arrived. Returns true when a frame was rendered
    /// (its mesh is then in the output buffer).
    pub fn step(&mut self) -> bool {
        let (complete, last_op) = self.complete_messages();
        let Some(op) = last_op else { return false };
        if !self.fetch_ops.contains(&op) {
            // Go is not blocked on a fetch: nothing to answer yet.
            return false;
        }
        // split off the trailing fetch message
        let fetch_start = {
            let b = &self.pending;
            let mut off = 0usize;
            let mut start = 0usize;
            while off < complete {
                start = off;
                let len = u32::from_ne_bytes([b[off], b[off + 1], b[off + 2], b[off + 3]]) as usize;
                off += 4 + len;
            }
            start
        };
        let rendered = fetch_start > 0;
        if rendered {
            let frame: Vec<u8> = self.pending[..fetch_start].to_vec();
            self.run_frame(&frame);
        }
        let fetch: Vec<u8> = self.pending[fetch_start..complete].to_vec();
        self.pending.drain(..complete);
        self.fffi.io.r.push(&fetch);
        if let Err(e) = self.fffi.interpret_outer(&self.ctx, &mut None) {
            self.stats.errors += 1;
            self.last_error = format!("fetch: {e}");
        }
        rendered
    }

    fn run_frame(&mut self, frame: &[u8]) {
        self.fffi.io.r.push(frame);
        let raw_input = egui::RawInput {
            screen_rect: Some(egui::Rect::from_min_size(
                egui::Pos2::ZERO,
                egui::vec2(self.width, self.height),
            )),
            max_texture_side: Some(MAX_TEXTURE_SIDE),
            time: Some(self.start.elapsed().as_secs_f64()),
            predicted_dt: NOMINAL_DT,
            focused: true,
            modifiers: self.translator.modifiers,
            events: std::mem::take(&mut self.pending_events),
            ..Default::default()
        };
        let t0 = Instant::now();
        let fffi = &mut self.fffi;
        let mut err = None;
        let out = self.ctx.run_ui(raw_input, |ui| {
            if let Err(e) = fffi.interpret_commands_outer(ui.ctx()) {
                err = Some(format!("frame: {e}"));
            }
        });
        if let Some(e) = err {
            self.stats.errors += 1;
            self.last_error = e;
        }
        if !self.fffi.io.r.is_empty() {
            // the frame's bytes must be consumed to its root End; leftovers
            // mean the split was wrong, and they would corrupt the next step
            self.stats.errors += 1;
            self.last_error = "frame: bytes left after the pass".into();
            self.fffi.io.r = Inbox::default();
        }
        self.stats.last_interpret_us = t0.elapsed().as_micros() as u64;

        let t1 = Instant::now();
        for m in self.textures.ingest(&out.textures_delta) {
            self.mesh_out.push(m);
        }
        let clipped = self.ctx.tessellate(out.shapes, out.pixels_per_point);
        self.stats.last_tessellate_us = t1.elapsed().as_micros() as u64;

        let t2 = Instant::now();
        let frame = meshlane::serialize(&clipped, out.pixels_per_point);
        let missing: Vec<usize> = (0..frame.hashes.len())
            .filter(|&i| !self.painter_has.contains(&frame.hashes[i]))
            .collect();
        let w_px = self.width * self.ppp;
        let h_px = self.height * self.ppp;
        let msg = meshlane::frame_message(out.pixels_per_point, w_px, h_px, &frame, &missing);
        self.mesh_out.push(msg);
        self.textures.finish_frame();
        self.mesh_out.push(meshlane::retirement_message(&self.textures.live_keys()));
        // after the retirement the painter keeps exactly this frame's bodies
        self.painter_has.clear();
        self.painter_has.extend(frame.hashes.iter().copied());
        self.stats.last_serialize_us = t2.elapsed().as_micros() as u64;
        self.stats.last_bodies = frame.hashes.len() as u64;
        self.stats.last_bodies_sent = missing.len() as u64;
        self.stats.mesh_bytes = self.mesh_out.iter().map(|m| m.len() as u64).sum();
        self.stats.frames += 1;
    }

    pub fn take_replies(&mut self) -> Vec<u8> {
        std::mem::take(&mut self.fffi.io.w.0)
    }
}

// ---- C-ABI surface for the JS worker --------------------------------------

thread_local! {
    static HOST: RefCell<Option<Host>> = const { RefCell::new(None) };
    static SCRATCH: RefCell<Vec<u8>> = const { RefCell::new(Vec::new()) };
    static REPLY: RefCell<Vec<u8>> = const { RefCell::new(Vec::new()) };
}

/// A scratch buffer of at least `n` bytes for the host to fill; valid until
/// the next call.
#[unsafe(no_mangle)]
pub extern "C" fn host_alloc(n: usize) -> *mut u8 {
    SCRATCH.with(|s| {
        let mut s = s.borrow_mut();
        if s.len() < n {
            s.resize(n, 0);
        }
        s.as_mut_ptr()
    })
}

/// Creates the host for a viewport of `width` × `height` points at `ppp`.
#[unsafe(no_mangle)]
pub extern "C" fn host_init(width: f32, height: f32, ppp: f32) {
    HOST.with(|h| *h.borrow_mut() = Some(Host::new(width, height, ppp)));
}

/// Admits `n` bytes of the producer's stream from the scratch buffer.
#[unsafe(no_mangle)]
pub extern "C" fn host_write(n: usize) {
    SCRATCH.with(|s| {
        let s = s.borrow();
        HOST.with(|h| {
            if let Some(h) = h.borrow_mut().as_mut() {
                h.write(&s[..n]);
            }
        });
    });
}

/// Queues one input event from `n` bytes of the scratch buffer; 1 if it
/// decoded.
#[unsafe(no_mangle)]
pub extern "C" fn host_input(n: usize) -> u32 {
    SCRATCH.with(|s| {
        let s = s.borrow();
        HOST.with(|h| h.borrow_mut().as_mut().map(|h| u32::from(h.input(&s[..n]))).unwrap_or(0))
    })
}

/// Interprets what has arrived; 1 when a frame was rendered, else 0.
#[unsafe(no_mangle)]
pub extern "C" fn host_step() -> u32 {
    HOST.with(|h| h.borrow_mut().as_mut().map(|h| u32::from(h.step())).unwrap_or(0))
}

/// Moves the pending fetch replies into the reply buffer; returns its length.
#[unsafe(no_mangle)]
pub extern "C" fn host_reply_take() -> usize {
    let r = HOST.with(|h| h.borrow_mut().as_mut().map(|h| h.take_replies()).unwrap_or_default());
    REPLY.with(|b| {
        *b.borrow_mut() = r;
        b.borrow().len()
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn host_reply_ptr() -> *const u8 {
    REPLY.with(|b| b.borrow().as_ptr())
}

/// The number of mesh messages pending since the last clear.
#[unsafe(no_mangle)]
pub extern "C" fn host_mesh_count() -> usize {
    HOST.with(|h| h.borrow().as_ref().map(|h| h.mesh_out.len()).unwrap_or(0))
}

/// Length of pending mesh message `i`.
#[unsafe(no_mangle)]
pub extern "C" fn host_mesh_len(i: usize) -> usize {
    HOST.with(|h| h.borrow().as_ref().and_then(|h| h.mesh_out.get(i)).map(|m| m.len()).unwrap_or(0))
}

/// Pointer to pending mesh message `i`; valid until the next clear.
#[unsafe(no_mangle)]
pub extern "C" fn host_mesh_ptr(i: usize) -> *const u8 {
    HOST.with(|h| {
        h.borrow()
            .as_ref()
            .and_then(|h| h.mesh_out.get(i))
            .map(|m| m.as_ptr())
            .unwrap_or(std::ptr::null())
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn host_mesh_clear() {
    HOST.with(|h| {
        if let Some(h) = h.borrow_mut().as_mut() {
            h.mesh_out.clear();
        }
    });
}

/// 0 frames, 1 bytes in, 2 mesh bytes pending, 3 last interpret µs,
/// 4 last tessellate µs, 5 last serialize µs, 6 last bodies, 7 last bodies
/// sent, 8 errors.
#[unsafe(no_mangle)]
pub extern "C" fn host_stat(i: u32) -> u64 {
    HOST.with(|h| {
        let h = h.borrow();
        let Some(h) = h.as_ref() else { return 0 };
        let s = h.stats;
        match i {
            0 => s.frames,
            1 => s.bytes_in,
            2 => s.mesh_bytes,
            3 => s.last_interpret_us,
            4 => s.last_tessellate_us,
            5 => s.last_serialize_us,
            6 => s.last_bodies,
            7 => s.last_bodies_sent,
            8 => s.errors,
            _ => 0,
        }
    })
}

/// Copies the last error into the scratch buffer; returns its length.
#[unsafe(no_mangle)]
pub extern "C" fn host_last_error() -> usize {
    let e = HOST.with(|h| h.borrow().as_ref().map(|h| h.last_error.clone()).unwrap_or_default());
    SCRATCH.with(|s| {
        let mut s = s.borrow_mut();
        if s.len() < e.len() {
            s.resize(e.len(), 0);
        }
        s[..e.len()].copy_from_slice(e.as_bytes());
    });
    e.len()
}

// ---- randomness for the graph layouts on wasm32 ---------------------------
//
// A xorshift generator seeded from the clock: not for anything that needs
// unpredictability, and nothing in this host does (getrandom serves
// egui_graphs's layout seeds). Registered for both getrandom majors in the
// tree, see Cargo.toml.
#[cfg(target_arch = "wasm32")]
mod randomness {
    use std::cell::Cell;

    thread_local! {
        static STATE: Cell<u64> = const { Cell::new(0) };
    }

    fn fill(dest: &mut [u8]) {
        STATE.with(|st| {
            let mut x = st.get();
            if x == 0 {
                x = (crate::imzero2::clock::Instant::now().elapsed().as_nanos() as u64)
                    ^ 0x9E37_79B9_7F4A_7C15;
                if x == 0 {
                    x = 0x2545_F491_4F6C_DD1D;
                }
            }
            for chunk in dest.chunks_mut(8) {
                x ^= x << 13;
                x ^= x >> 7;
                x ^= x << 17;
                let b = x.to_le_bytes();
                chunk.copy_from_slice(&b[..chunk.len()]);
            }
            st.set(x);
        });
    }

    #[allow(clippy::unnecessary_wraps)] // the signature getrandom 0.2 registers
    fn fill_v02(dest: &mut [u8]) -> Result<(), getrandom02::Error> {
        fill(dest);
        Ok(())
    }
    getrandom02::register_custom_getrandom!(fill_v02);

    /// getrandom 0.3's custom backend entry point.
    ///
    /// # Safety
    /// `dest` must point to `len` writable bytes, which the caller guarantees.
    #[unsafe(no_mangle)]
    #[allow(clippy::unnecessary_wraps)] // the signature getrandom 0.3 links against
    pub unsafe extern "Rust" fn __getrandom_v03_custom(
        dest: *mut u8,
        len: usize,
    ) -> Result<(), getrandom::Error> {
        // SAFETY: the contract above.
        let slice = unsafe { std::slice::from_raw_parts_mut(dest, len) };
        fill(slice);
        Ok(())
    }
}
