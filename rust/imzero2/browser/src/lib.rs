//! The browser host's C ABI: the wasm32 module a JS worker instantiates and
//! drives (ADR-0263). Everything here is a thin export over
//! [`imzero2::imzero2::browser::Host`], which owns the interpreter, egui and
//! the mesh serialization; this crate exists only so the module is a cdylib
//! while the native hosts keep the rlib.
//!
//! The worker copies bytes into a scratch buffer it gets from [`host_alloc`]
//! and reads results from pointers it asks for; one instance per module.
#![allow(unsafe_code)]

use std::cell::RefCell;

use imzero2::imzero2::browser::{FONT_SLOTS, FontBytes, Host};

thread_local! {
    static HOST: RefCell<Option<Host>> = const { RefCell::new(None) };
    static SCRATCH: RefCell<Vec<u8>> = const { RefCell::new(Vec::new()) };
    static REPLY: RefCell<Vec<u8>> = const { RefCell::new(Vec::new()) };
    static FONTS: RefCell<FontBytes> = const { RefCell::new([None, None, None, None]) };
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

/// Stores `n` bytes of the scratch buffer as the font for slot `kind` (an
/// index into [`FONT_SLOTS`]) for the next `host_init`, which takes them.
/// Fonts cross as bytes because a tab has no paths (ADR-0077 SD5); 1 when
/// the slot exists.
#[unsafe(no_mangle)]
pub extern "C" fn host_font(kind: u32, n: usize) -> u32 {
    let i = kind as usize;
    if i >= FONT_SLOTS.len() {
        return 0;
    }
    SCRATCH.with(|s| FONTS.with(|f| f.borrow_mut()[i] = Some(s.borrow()[..n].to_vec())));
    1
}

/// Creates the host for a viewport of `width` × `height` points at `ppp`,
/// with whatever fonts `host_font` stored since the last init.
#[unsafe(no_mangle)]
pub extern "C" fn host_init(width: f32, height: f32, ppp: f32) {
    let fonts = FONTS.with(|f| std::mem::take(&mut *f.borrow_mut()));
    HOST.with(|h| *h.borrow_mut() = Some(Host::new(width, height, ppp, fonts)));
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

/// Takes one session-control message from `n` bytes of the scratch buffer;
/// see [`Host::session`] for the return codes.
#[unsafe(no_mangle)]
pub extern "C" fn host_session(n: usize) -> u32 {
    SCRATCH.with(|s| {
        let s = s.borrow();
        HOST.with(|h| h.borrow_mut().as_mut().map(|h| h.session(&s[..n])).unwrap_or(0))
    })
}

/// The viewport width in points the host renders for.
#[unsafe(no_mangle)]
pub extern "C" fn host_width() -> f32 {
    HOST.with(|h| h.borrow().as_ref().map(|h| h.geometry().0).unwrap_or(0.0))
}

/// The viewport height in points the host renders for.
#[unsafe(no_mangle)]
pub extern "C" fn host_height() -> f32 {
    HOST.with(|h| h.borrow().as_ref().map(|h| h.geometry().1).unwrap_or(0.0))
}

/// The pixel scale the host renders for.
#[unsafe(no_mangle)]
pub extern "C" fn host_ppp() -> f32 {
    HOST.with(|h| h.borrow().as_ref().map(|h| h.geometry().2).unwrap_or(1.0))
}

/// The IDL the interpreter was generated from; the worker compares it with the
/// Go module's before setup and refuses a pair from different generations
/// (ADR-0278 SD6, proposed).
#[unsafe(no_mangle)]
pub extern "C" fn host_idl_fingerprint() -> u64 {
    imzero2::imzero2::enums_out::IDL_FINGERPRINT
}

/// Milliseconds after the last pass at which egui asked to run again: 0
/// means right away (an animation, a repaint request), a large value means
/// nothing is pending. A worker that ticks on demand reads this after each
/// frame.
#[unsafe(no_mangle)]
pub extern "C" fn host_repaint_delay_ms() -> f64 {
    HOST.with(|h| h.borrow().as_ref().map(|h| h.repaint_delay_ms()).unwrap_or(0.0))
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
    HOST.with(|h| h.borrow().as_ref().map(|h| h.mesh_messages().len()).unwrap_or(0))
}

/// Length of pending mesh message `i`.
#[unsafe(no_mangle)]
pub extern "C" fn host_mesh_len(i: usize) -> usize {
    HOST.with(|h| {
        h.borrow().as_ref().and_then(|h| h.mesh_messages().get(i)).map(Vec::len).unwrap_or(0)
    })
}

/// Pointer to pending mesh message `i`; valid until the next clear.
#[unsafe(no_mangle)]
pub extern "C" fn host_mesh_ptr(i: usize) -> *const u8 {
    HOST.with(|h| {
        h.borrow()
            .as_ref()
            .and_then(|h| h.mesh_messages().get(i))
            .map(|m| m.as_ptr())
            .unwrap_or(std::ptr::null())
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn host_mesh_clear() {
    HOST.with(|h| {
        if let Some(h) = h.borrow_mut().as_mut() {
            h.mesh_clear();
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
            9 => s.frames_unchanged,
            10..=13 => u64::from(s.last_sent_bbox[(i - 10) as usize].to_bits()),
            _ => 0,
        }
    })
}

/// Copies the last error into the scratch buffer; returns its length.
#[unsafe(no_mangle)]
pub extern "C" fn host_last_error() -> usize {
    let e =
        HOST.with(|h| h.borrow().as_ref().map(|h| h.last_error().to_owned()).unwrap_or_default());
    SCRATCH.with(|s| {
        let mut s = s.borrow_mut();
        if s.len() < e.len() {
            s.resize(e.len(), 0);
        }
        s[..e.len()].copy_from_slice(e.as_bytes());
    });
    e.len()
}
