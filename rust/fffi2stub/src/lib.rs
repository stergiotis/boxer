//! A stub peer for the FFFI2 command stream.
//!
//! The real peer is the imzero2 interpreter, which reads `u32 len, u32 opcode,
//! payload` messages off a pipe and answers each fetcher opcode with raw
//! values. This crate keeps only the framing: it walks the messages, counts
//! them, and answers every fetcher with zero-valued scalars and empty slices,
//! so the Go frame loop can run to completion with no egui behind it. It is
//! the measuring instrument of the keelson-wasm-frame-cost trial (ADR-0077
//! SD2): the same code is a native pipe peer and a wasm32 module driven by a
//! JS bridge, so the two arms differ only in the transport.
//!
//! The fetcher table is not compiled in: the Go side derives it from the
//! generated bindings and hands it over as text (`load_table`), so the stub
//! never drifts from the tree it measures.

use std::cell::RefCell;
use std::collections::HashMap;

#[derive(Clone, Copy)]
enum Kind {
    B,
    U8,
    U32,
    U64,
    I64,
    F32,
    F64,
    H,
}

impl Kind {
    fn parse(s: &str) -> Option<Kind> {
        Some(match s {
            "b" => Kind::B,
            "u8" => Kind::U8,
            "u32" => Kind::U32,
            "u64" => Kind::U64,
            "i64" => Kind::I64,
            "f32" => Kind::F32,
            "f64" => Kind::F64,
            "h" => Kind::H,
            _ => return None,
        })
    }
    fn width(self) -> usize {
        match self {
            Kind::B | Kind::U8 => 1,
            Kind::U32 | Kind::F32 | Kind::H => 4,
            Kind::U64 | Kind::I64 | Kind::F64 => 8,
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Special {
    None,
    AvailableSize,
    FrameMetrics,
}

struct Fetcher {
    kinds: Vec<Kind>,
    special: Special,
}

/// Counters a host reads back after a run.
#[derive(Default, Clone, Copy)]
pub struct Stats {
    pub messages: u64,
    pub bytes: u64,
    pub frames: u64,
    pub fetches: u64,
    pub max_message: u64,
    pub unknown_fetch: u64,
}

pub struct Stub {
    carry: Vec<u8>,
    reply: Vec<u8>,
    table: HashMap<u32, Fetcher>,
    stage: (f32, f32),
    pass: u64,
    pub stats: Stats,
}

impl Default for Stub {
    fn default() -> Self {
        Self::new()
    }
}

impl Stub {
    pub fn new() -> Self {
        Stub {
            carry: Vec::new(),
            reply: Vec::new(),
            table: HashMap::new(),
            stage: (1024.0, 600.0),
            pass: 0,
            stats: Stats::default(),
        }
    }

    pub fn set_stage(&mut self, w: f32, h: f32) {
        self.stage = (w, h);
    }

    /// Loads the fetcher table. One entry per line: `fetch <opcode> <Name>
    /// <kind>...` names a fetcher and the shape of its reply. Returns the
    /// number of fetchers. Frames are counted on `FetchFrameMetrics`, the
    /// last fetch of every `StateManager.Sync`.
    pub fn load_table(&mut self, text: &str) -> Result<usize, String> {
        for line in text.lines() {
            let mut it = line.split_whitespace();
            match it.next() {
                None => continue,
                Some("fetch") => {
                    let op: u32 = it
                        .next()
                        .ok_or("fetch: missing opcode")?
                        .parse()
                        .map_err(|_| "fetch: bad opcode".to_string())?;
                    let name = it.next().ok_or("fetch: missing name")?;
                    let mut kinds = Vec::new();
                    for k in it {
                        kinds.push(
                            Kind::parse(k)
                                .ok_or_else(|| format!("fetch {name}: unknown kind {k}"))?,
                        );
                    }
                    let special = match name {
                        "FetchR18AvailableSize" => Special::AvailableSize,
                        "FetchFrameMetrics" => Special::FrameMetrics,
                        _ => Special::None,
                    };
                    self.table.insert(op, Fetcher { kinds, special });
                }
                Some(other) => return Err(format!("unknown table line: {other}")),
            }
        }
        Ok(self.table.len())
    }

    /// Feeds bytes from the producer. Whole messages are consumed; a trailing
    /// partial message is carried to the next call. Replies to any fetcher
    /// seen are appended to the reply buffer, which `take_reply` drains.
    pub fn consume(&mut self, bytes: &[u8]) {
        self.stats.bytes += bytes.len() as u64;
        if self.carry.is_empty() {
            let used = self.walk(bytes);
            if used < bytes.len() {
                self.carry.extend_from_slice(&bytes[used..]);
            }
        } else {
            self.carry.extend_from_slice(bytes);
            let carry = std::mem::take(&mut self.carry);
            let used = self.walk(&carry);
            if used < carry.len() {
                self.carry.extend_from_slice(&carry[used..]);
            }
        }
    }

    fn walk(&mut self, buf: &[u8]) -> usize {
        let mut off = 0usize;
        while buf.len() - off >= 4 {
            let len =
                u32::from_ne_bytes([buf[off], buf[off + 1], buf[off + 2], buf[off + 3]]) as usize;
            if buf.len() - off < 4 + len {
                break;
            }
            let msg = &buf[off + 4..off + 4 + len];
            off += 4 + len;
            self.stats.messages += 1;
            self.stats.max_message = self.stats.max_message.max(len as u64);
            if len < 4 {
                continue;
            }
            let op = u32::from_ne_bytes([msg[0], msg[1], msg[2], msg[3]]);
            if let Some(f) = self.table.get(&op) {
                self.stats.fetches += 1;
                let (kinds, special) = (f.kinds.clone(), f.special);
                self.answer(&kinds, special);
            }
        }
        off
    }

    fn answer(&mut self, kinds: &[Kind], special: Special) {
        match special {
            Special::AvailableSize => {
                self.reply.extend_from_slice(&self.stage.0.to_ne_bytes());
                self.reply.extend_from_slice(&self.stage.1.to_ne_bytes());
            }
            Special::FrameMetrics => {
                self.pass += 1;
                self.stats.frames += 1;
                self.reply.extend_from_slice(&0u64.to_ne_bytes());
                self.reply.extend_from_slice(&self.pass.to_ne_bytes());
            }
            Special::None => {
                for k in kinds {
                    // Zero bytes are a zero scalar, `false`, or a slice of
                    // length 0 (the nil sentinel is u32::MAX, never sent).
                    let w = k.width();
                    self.reply.extend(std::iter::repeat_n(0u8, w));
                }
            }
        }
    }

    pub fn take_reply(&mut self) -> Vec<u8> {
        std::mem::take(&mut self.reply)
    }

    pub fn reply(&self) -> &[u8] {
        &self.reply
    }

    pub fn clear_reply(&mut self) {
        self.reply.clear();
    }
}

// ---- wasm32 export surface (plain C ABI, no bindgen) -----------------------
//
// The host copies producer bytes into the buffer `stub_alloc` returns, calls
// `stub_consume`, then copies `stub_reply_len` bytes from `stub_reply_ptr`.

thread_local! {
    static STUB: RefCell<Stub> = RefCell::new(Stub::new());
    static INBUF: RefCell<Vec<u8>> = const { RefCell::new(Vec::new()) };
}

/// Returns a pointer to an input buffer of at least `n` bytes; valid until
/// the next `stub_alloc`.
#[unsafe(no_mangle)]
pub extern "C" fn stub_alloc(n: usize) -> *mut u8 {
    INBUF.with(|b| {
        let mut b = b.borrow_mut();
        if b.len() < n {
            b.resize(n, 0);
        }
        b.as_mut_ptr()
    })
}

/// Consumes `n` bytes from the input buffer. Returns the reply length.
#[unsafe(no_mangle)]
pub extern "C" fn stub_consume(n: usize) -> usize {
    INBUF.with(|b| {
        let b = b.borrow();
        STUB.with(|s| {
            let mut s = s.borrow_mut();
            s.consume(&b[..n]);
            s.reply().len()
        })
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn stub_reply_ptr() -> *const u8 {
    STUB.with(|s| s.borrow().reply().as_ptr())
}

#[unsafe(no_mangle)]
pub extern "C" fn stub_reply_len() -> usize {
    STUB.with(|s| s.borrow().reply().len())
}

#[unsafe(no_mangle)]
pub extern "C" fn stub_reply_clear() {
    STUB.with(|s| s.borrow_mut().clear_reply());
}

/// Loads the fetcher table from `n` bytes of UTF-8 in the input buffer.
/// Returns the fetcher count, or `u32::MAX` on a parse error.
#[unsafe(no_mangle)]
pub extern "C" fn stub_load_table(n: usize) -> u32 {
    INBUF.with(|b| {
        let b = b.borrow();
        let Ok(text) = std::str::from_utf8(&b[..n]) else {
            return u32::MAX;
        };
        STUB.with(|s| match s.borrow_mut().load_table(text) {
            Ok(k) => k as u32,
            Err(_) => u32::MAX,
        })
    })
}

#[unsafe(no_mangle)]
pub extern "C" fn stub_set_stage(w: f32, h: f32) {
    STUB.with(|s| s.borrow_mut().set_stage(w, h));
}

/// 0 messages, 1 bytes, 2 frames, 3 fetches, 4 largest message.
#[unsafe(no_mangle)]
pub extern "C" fn stub_stat(i: u32) -> u64 {
    STUB.with(|s| {
        let st = s.borrow().stats;
        match i {
            0 => st.messages,
            1 => st.bytes,
            2 => st.frames,
            3 => st.fetches,
            4 => st.max_message,
            _ => 0,
        }
    })
}

// ---- pass-through counter ---------------------------------------------------

/// Counts messages of a producer's stream without answering anything: the
/// pass-through mode (`fffi2stub tee`) sits between Go and the real host and
/// keeps this per opcode and per frame. Frames are delimited by one opcode
/// the caller names, normally `FetchFrameMetrics`, the last fetch of a Sync.
pub struct Counter {
    carry: Vec<u8>,
    frame_op: u32,
    /// per-opcode message count and bytes, indexed by opcode
    pub by_op: Vec<(u64, u64)>,
    /// (messages, bytes, largest message) of the frame being counted
    cur: (u64, u64, u64),
    /// closed frames, in order
    pub frames: Vec<(u64, u64, u64)>,
}

impl Counter {
    pub fn new(frame_op: u32) -> Self {
        Counter {
            carry: Vec::new(),
            frame_op,
            by_op: vec![(0, 0); 4096],
            cur: (0, 0, 0),
            frames: Vec::new(),
        }
    }

    pub fn consume(&mut self, bytes: &[u8]) {
        if !self.carry.is_empty() {
            let mut carry = std::mem::take(&mut self.carry);
            carry.extend_from_slice(bytes);
            let used = self.walk(&carry);
            self.carry.extend_from_slice(&carry[used..]);
        } else {
            let used = self.walk(bytes);
            self.carry.extend_from_slice(&bytes[used..]);
        }
    }

    fn walk(&mut self, buf: &[u8]) -> usize {
        let mut off = 0usize;
        while buf.len() - off >= 4 {
            let len =
                u32::from_ne_bytes([buf[off], buf[off + 1], buf[off + 2], buf[off + 3]]) as usize;
            if buf.len() - off < 4 + len {
                break;
            }
            let msg = &buf[off + 4..off + 4 + len];
            off += 4 + len;
            let total = (4 + len) as u64;
            self.cur.0 += 1;
            self.cur.1 += total;
            self.cur.2 = self.cur.2.max(total);
            if len >= 4 {
                let op = u32::from_ne_bytes([msg[0], msg[1], msg[2], msg[3]]);
                if let Some(e) = self.by_op.get_mut(op as usize) {
                    e.0 += 1;
                    e.1 += total;
                }
                if op == self.frame_op {
                    self.frames.push(self.cur);
                    self.cur = (0, 0, 0);
                }
            }
        }
        off
    }
}
