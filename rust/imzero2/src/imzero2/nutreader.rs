//! Minimal incremental NUT-container demuxer (ADR-0088 SD4, Phase 0/1).
//!
//! The runtime-selectable codec pipeline muxes ffmpeg's output to the NUT
//! container (`-f nut`) instead of a per-codec elementary stream. NUT was
//! chosen (ADR-0088) because, uniquely among the generic containers, it
//! keeps the native bitstream **in-band** (H.264 stays Annex-B; VP9/AV1
//! stay native frames/temporal units) *and* exposes per-frame boundaries
//! and the keyframe flag in the container layer — so one demuxer serves
//! every codec, present and future, with no per-codec bitstream parsing.
//!
//! This reader is deliberately small: it parses only what ffmpeg's
//! `nutenc` emits for a single video stream. It builds the frame-code
//! table and elision headers from the main header, skips stream/info/
//! index packets and syncpoints by their `forward_ptr`, and yields each
//! coded frame's native payload plus its keyframe flag. Codec parameters
//! travel in-band, so we never touch extradata.
//!
//! ## Licensing
//!
//! NUT is an open, patent-free container format with a public specification
//! and an MIT-licensed reference implementation (`libnut`); see
//! <http://www.nut-container.org/> and <https://github.com/lu-zero/nut>.
//! This module is an **independent Rust implementation** of that open
//! format: the startcodes and frame flags below are constants of the public
//! format, and the parser is original expression. **No FFmpeg source is
//! copied or linked** — FFmpeg is invoked only as an encoder subprocess
//! (ADR-0024 C7) and this reader consumes its NUT output, so the project's
//! clean-redistribution posture is preserved. Correctness is validated
//! against `ffprobe`'s packet view of real ffmpeg output (see tests).

/// NUT startcodes (64-bit, big-endian; every one begins with `'N'`=0x4E).
const MAIN_STARTCODE: u64 = 0x4E4D_7A56_1F5F_04AD;
const STREAM_STARTCODE: u64 = 0x4E53_1140_5BF2_F9DB;
const SYNCPOINT_STARTCODE: u64 = 0x4E4B_E4AD_EECA_4569;
const INDEX_STARTCODE: u64 = 0x4E58_DD67_2F23_E64E;
const INFO_STARTCODE: u64 = 0x4E49_AB68_B596_BA78;

/// NUT frame flags (constants of the NUT format).
const FLAG_KEY: u64 = 1;
const FLAG_CODED_PTS: u64 = 8;
const FLAG_STREAM_ID: u64 = 16;
const FLAG_SIZE_MSB: u64 = 32;
const FLAG_CHECKSUM: u64 = 64;
const FLAG_RESERVED: u64 = 128;
const FLAG_SM_DATA: u64 = 256;
const FLAG_HEADER_IDX: u64 = 1024;
const FLAG_MATCH_TIME: u64 = 2048;
const FLAG_CODED: u64 = 4096;
const FLAG_INVALID: u64 = 8192;

/// `'N'` is reserved as the first byte of every startcode, so its
/// frame-code slot is always marked invalid by the main header.
const STARTCODE_FIRST_BYTE: usize = b'N' as usize;

#[derive(Debug, Clone, Copy)]
pub enum NutError {
    /// A non-startcode frame appeared before the main header.
    FrameBeforeMain,
    /// Frame-code slot is reserved/invalid (`FLAG_INVALID`).
    InvalidFrameCode,
    /// Per-frame side/metadata is not produced by our encode path.
    SmDataUnsupported,
    /// Structurally impossible stream (out-of-range field, etc.).
    Malformed,
    /// A startcode we did not expect mid-stream (would need resync).
    UnexpectedStartcode,
}

impl std::fmt::Display for NutError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let s = match self {
            NutError::FrameBeforeMain => "frame before main header",
            NutError::InvalidFrameCode => "invalid frame code",
            NutError::SmDataUnsupported => "per-frame sm_data unsupported",
            NutError::Malformed => "malformed nut stream",
            NutError::UnexpectedStartcode => "unexpected startcode",
        };
        f.write_str(s)
    }
}

impl std::error::Error for NutError {}

/// One demuxed coded frame: the codec's native payload (with any elision
/// prefix reconstructed) and whether it is a key frame.
pub struct NutFrame {
    pub keyframe: bool,
    pub data: Vec<u8>,
}

#[derive(Clone, Copy, Default)]
struct FrameCode {
    flags: u64,
    pts_delta: i64,
    size_mul: u64,
    size_lsb: u64,
    reserved_count: u64,
    header_idx: u64,
}

/// Incremental NUT demuxer. Feed bytes with [`NutReader::push`]; pull
/// complete frames with [`NutReader::next_frame`] until it returns `None`.
pub struct NutReader {
    buf: Vec<u8>,
    pos: usize,
    id_skipped: bool,
    main_parsed: bool,
    version: u64,
    table: [FrameCode; 256],
    /// Elision header lengths; index 0 is always the empty header.
    header_lens: Vec<usize>,
    headers: Vec<Vec<u8>>,
}

enum Step {
    NeedMore,
    Consumed,
    Frame(NutFrame),
}

impl Default for NutReader {
    fn default() -> Self {
        Self::new()
    }
}

impl NutReader {
    pub fn new() -> Self {
        Self {
            buf: Vec::with_capacity(256 * 1024),
            pos: 0,
            id_skipped: false,
            main_parsed: false,
            version: 0,
            table: [FrameCode::default(); 256],
            header_lens: vec![0],
            headers: vec![Vec::new()],
        }
    }

    /// Append freshly-read ffmpeg-stdout bytes to the parse buffer.
    pub fn push(&mut self, bytes: &[u8]) {
        self.buf.extend_from_slice(bytes);
    }

    /// Return the next complete frame, or `None` if more bytes are needed.
    /// Internally consumes the file-id string, the main header, and any
    /// stream/info/index/syncpoint packets in the way.
    pub fn next_frame(&mut self) -> Result<Option<NutFrame>, NutError> {
        loop {
            match self.step()? {
                Step::Frame(f) => return Ok(Some(f)),
                Step::Consumed => continue,
                Step::NeedMore => {
                    if self.pos > 0 {
                        self.buf.drain(..self.pos);
                        self.pos = 0;
                    }
                    return Ok(None);
                }
            }
        }
    }

    fn step(&mut self) -> Result<Step, NutError> {
        // The file begins with the null-terminated id string
        // "nut/multimedia container\0".
        if !self.id_skipped {
            return match skip_id_string(&self.buf, self.pos) {
                None => Ok(Step::NeedMore),
                Some(np) => {
                    self.pos = np;
                    self.id_skipped = true;
                    Ok(Step::Consumed)
                }
            };
        }

        let Some(&first) = self.buf.get(self.pos) else {
            return Ok(Step::NeedMore);
        };

        if first as usize == STARTCODE_FIRST_BYTE {
            // Assemble the 64-bit startcode and dispatch.
            let mut c = self.pos;
            let Some(sc) = rd_u64be(&self.buf, &mut c) else {
                return Ok(Step::NeedMore);
            };
            match sc {
                MAIN_STARTCODE => match parse_main_header(&self.buf, c)? {
                    None => Ok(Step::NeedMore),
                    Some(parsed) => {
                        self.version = parsed.version;
                        self.table = parsed.table;
                        self.header_lens = parsed.header_lens;
                        self.headers = parsed.headers;
                        self.main_parsed = true;
                        self.pos = parsed.end;
                        Ok(Step::Consumed)
                    }
                },
                STREAM_STARTCODE | INDEX_STARTCODE | INFO_STARTCODE | SYNCPOINT_STARTCODE => {
                    match skip_packet(&self.buf, c) {
                        None => Ok(Step::NeedMore),
                        Some(np) => {
                            self.pos = np;
                            Ok(Step::Consumed)
                        }
                    }
                }
                _ => Err(NutError::UnexpectedStartcode),
            }
        } else {
            if !self.main_parsed {
                return Err(NutError::FrameBeforeMain);
            }
            match parse_frame(&self.buf, self.pos, &self.table, &self.header_lens, &self.headers)? {
                None => Ok(Step::NeedMore),
                Some((np, frame)) => {
                    self.pos = np;
                    Ok(Step::Frame(frame))
                }
            }
        }
    }
}

// ---- byte-cursor helpers (None == not enough bytes buffered) ----

fn rd_u8(b: &[u8], c: &mut usize) -> Option<u8> {
    let v = *b.get(*c)?;
    *c += 1;
    Some(v)
}

/// NUT variable-length unsigned int: big-endian, 7 bits/byte, high bit =
/// "more bytes follow".
fn rd_v(b: &[u8], c: &mut usize) -> Option<u64> {
    let mut val: u64 = 0;
    for _ in 0..10 {
        let byte = rd_u8(b, c)?;
        val = (val << 7) | (byte & 0x7f) as u64;
        if byte & 0x80 == 0 {
            return Some(val);
        }
    }
    None
}

/// NUT signed varint, in terms of `rd_v`.
fn rd_s(b: &[u8], c: &mut usize) -> Option<i64> {
    let v = rd_v(b, c)?.wrapping_add(1);
    Some(if v & 1 == 1 {
        -((v >> 1) as i64)
    } else {
        (v >> 1) as i64
    })
}

fn rd_u64be(b: &[u8], c: &mut usize) -> Option<u64> {
    let mut v = 0u64;
    for _ in 0..8 {
        v = (v << 8) | rd_u8(b, c)? as u64;
    }
    Some(v)
}

fn skip_n(b: &[u8], c: &mut usize, n: usize) -> Option<()> {
    let end = c.checked_add(n)?;
    if end <= b.len() {
        *c = end;
        Some(())
    } else {
        None
    }
}

/// Consume the null-terminated "nut/multimedia container\0" id string.
fn skip_id_string(b: &[u8], pos: usize) -> Option<usize> {
    let nul = b.get(pos..)?.iter().position(|&x| x == 0)?;
    Some(pos + nul + 1)
}

/// Skip a header/syncpoint packet via its `forward_ptr` (NUT packet
/// framing: a 4-byte header checksum precedes the payload only when
/// `forward_ptr` exceeds 4096).
fn skip_packet(b: &[u8], pos: usize) -> Option<usize> {
    let mut c = pos;
    let fwd = rd_v(b, &mut c)? as usize;
    if fwd > 4096 {
        skip_n(b, &mut c, 4)?;
    }
    skip_n(b, &mut c, fwd)?;
    Some(c)
}

struct MainHeader {
    end: usize,
    version: u64,
    table: [FrameCode; 256],
    header_lens: Vec<usize>,
    headers: Vec<Vec<u8>>,
}

/// Parse the main header (frame-code table + elision headers). Requires
/// the whole packet to be buffered — it is small and arrives first — so
/// once `end <= buf.len()` any short read is `Malformed`, not `NeedMore`.
fn parse_main_header(b: &[u8], pos: usize) -> Result<Option<MainHeader>, NutError> {
    let mut c = pos;
    let Some(length) = rd_v(b, &mut c) else {
        return Ok(None);
    };
    let length = length as usize;
    if length > 4096 && skip_n(b, &mut c, 4).is_none() {
        return Ok(None);
    }
    let end = match c.checked_add(length) {
        Some(e) => e,
        None => return Err(NutError::Malformed),
    };
    if end > b.len() {
        return Ok(None);
    }

    let m = NutError::Malformed;
    let version = rd_v(b, &mut c).ok_or(m)?;
    if version > 3 {
        rd_v(b, &mut c).ok_or(m)?; // minor_version
    }
    let _stream_count = rd_v(b, &mut c).ok_or(m)?;
    let _max_distance = rd_v(b, &mut c).ok_or(m)?;
    let time_base_count = rd_v(b, &mut c).ok_or(m)?;
    for _ in 0..time_base_count {
        rd_v(b, &mut c).ok_or(m)?; // num
        rd_v(b, &mut c).ok_or(m)?; // den
    }

    // Decode the 256-entry frame-code table. The NUT format encodes it as
    // runs: each run carries defaults that apply to `run_len` consecutive
    // frame codes, with `size_lsb` incrementing across the run. Run state
    // (pts/mul/header-idx) persists across runs until overridden.
    let mut table = [FrameCode::default(); 256];
    let (mut run_pts, mut run_mul, mut run_hdr_idx) = (0i64, 1u64, 0u64);
    let mut slot = 0usize;
    while slot < 256 {
        let slot_flags = rd_v(b, &mut c).ok_or(m)?;
        let nfields = rd_v(b, &mut c).ok_or(m)?;
        if nfields > 0 {
            run_pts = rd_s(b, &mut c).ok_or(m)?;
        }
        if nfields > 1 {
            run_mul = rd_v(b, &mut c).ok_or(m)?;
        }
        if nfields > 2 {
            rd_v(b, &mut c).ok_or(m)?; // stream id — single stream, unused
        }
        let run_size = if nfields > 3 { rd_v(b, &mut c).ok_or(m)? } else { 0 };
        let run_reserved = if nfields > 4 { rd_v(b, &mut c).ok_or(m)? } else { 0 };
        let run_len = if nfields > 5 {
            rd_v(b, &mut c).ok_or(m)?
        } else {
            run_mul.wrapping_sub(run_size)
        };
        if nfields > 6 {
            rd_s(b, &mut c).ok_or(m)?; // reserved signed field
        }
        if nfields > 7 {
            run_hdr_idx = rd_v(b, &mut c).ok_or(m)?;
        }
        for _ in 8..nfields {
            rd_v(b, &mut c).ok_or(m)?; // forward-compatible reserved fields
        }
        if run_len == 0 || run_len > 256 {
            return Err(NutError::Malformed);
        }
        let mut filled = 0u64;
        while filled < run_len && slot < 256 {
            if slot == STARTCODE_FIRST_BYTE {
                // 'N' begins every startcode, so its slot is reserved: mark
                // it invalid; it consumes a frame code but not a run entry.
                table[slot].flags = FLAG_INVALID;
                slot += 1;
                continue;
            }
            table[slot] = FrameCode {
                flags: slot_flags,
                pts_delta: run_pts,
                size_mul: run_mul,
                size_lsb: run_size + filled,
                reserved_count: run_reserved,
                header_idx: run_hdr_idx,
            };
            slot += 1;
            filled += 1;
        }
    }

    // Elision (compression) headers; present iff there is room before the
    // trailing 4-byte checksum.
    let mut header_lens = vec![0usize];
    let mut headers: Vec<Vec<u8>> = vec![Vec::new()];
    if end > c + 4 {
        let header_count = rd_v(b, &mut c).ok_or(m)?.wrapping_add(1);
        for _ in 1..header_count {
            let len = rd_v(b, &mut c).ok_or(m)? as usize;
            let mut hdr = vec![0u8; len];
            hdr.copy_from_slice(b.get(c..c + len).ok_or(m)?);
            c += len;
            header_lens.push(len);
            headers.push(hdr);
        }
    }
    // version>3 trailing flags + reserved bytes are skipped by jumping to
    // `end` (which also consumes the trailing checksum).

    Ok(Some(MainHeader {
        end,
        version,
        table,
        header_lens,
        headers,
    }))
}

/// Parse one NUT frame header and reconstruct its payload (prepending any
/// elision-header prefix). Returns `Ok(None)` when more bytes are needed;
/// `Err` only on a structurally invalid frame.
fn parse_frame(
    b: &[u8],
    pos: usize,
    table: &[FrameCode; 256],
    header_lens: &[usize],
    headers: &[Vec<u8>],
) -> Result<Option<(usize, NutFrame)>, NutError> {
    macro_rules! need {
        ($e:expr) => {
            match $e {
                Some(v) => v,
                None => return Ok(None),
            }
        };
    }

    let mut c = pos;
    let frame_code = need!(rd_u8(b, &mut c)) as usize;
    let fc = table[frame_code];
    let mut flags = fc.flags;
    let mut size = fc.size_lsb;
    let mut header_idx = fc.header_idx;
    let mut reserved_count = fc.reserved_count;

    if flags & FLAG_INVALID != 0 {
        return Err(NutError::InvalidFrameCode);
    }
    if flags & FLAG_CODED != 0 {
        flags ^= need!(rd_v(b, &mut c));
    }
    if flags & FLAG_STREAM_ID != 0 {
        need!(rd_v(b, &mut c)); // stream id — single stream, ignored
    }
    if flags & FLAG_CODED_PTS != 0 {
        need!(rd_v(b, &mut c)); // coded pts — ignored
    }
    if flags & FLAG_SIZE_MSB != 0 {
        let msb = need!(rd_v(b, &mut c));
        size = size.wrapping_add(fc.size_mul.wrapping_mul(msb));
    }
    if flags & FLAG_MATCH_TIME != 0 {
        need!(rd_s(b, &mut c)); // match_time_delta — ignored
    }
    if flags & FLAG_HEADER_IDX != 0 {
        header_idx = need!(rd_v(b, &mut c));
    }
    if flags & FLAG_RESERVED != 0 {
        reserved_count = need!(rd_v(b, &mut c));
    }
    for _ in 0..reserved_count {
        need!(rd_v(b, &mut c));
    }
    if flags & FLAG_SM_DATA != 0 {
        return Err(NutError::SmDataUnsupported);
    }

    let mut size = size as usize;
    if size > 4096 {
        header_idx = 0; // header elision disabled for large frames
    }
    let hidx = header_idx as usize;
    if hidx >= header_lens.len() {
        return Err(NutError::Malformed);
    }
    let prefix_len = header_lens[hidx];
    if prefix_len > size {
        return Err(NutError::Malformed);
    }
    size -= prefix_len; // bytes to read from the stream after the prefix

    if flags & FLAG_CHECKSUM != 0 && skip_n(b, &mut c, 4).is_none() {
        return Ok(None);
    }
    let payload = match b.get(c..c + size) {
        Some(p) => p,
        None => return Ok(None),
    };

    let mut data = Vec::with_capacity(prefix_len + size);
    data.extend_from_slice(&headers[hidx]);
    data.extend_from_slice(payload);
    c += size;

    Ok(Some((
        c,
        NutFrame {
            keyframe: flags & FLAG_KEY != 0,
            data,
        },
    )))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::process::Command;

    fn have(cmd: &str) -> bool {
        Command::new(cmd)
            .arg("-version")
            .output()
            .map(|o| o.status.success())
            .unwrap_or(false)
    }

    /// Ground truth: ffprobe's per-packet (size, is_keyframe) for the stream.
    fn ffprobe_packets(path: &str) -> Vec<(usize, bool)> {
        let out = Command::new("ffprobe")
            .args([
                "-hide_banner",
                "-loglevel",
                "error",
                "-show_entries",
                "packet=size,flags",
                "-of",
                "csv",
                path,
            ])
            .output()
            .expect("ffprobe run");
        String::from_utf8_lossy(&out.stdout)
            .lines()
            .filter_map(|l| {
                let f: Vec<&str> = l.split(',').collect();
                if f.len() >= 3 && f[0] == "packet" {
                    Some((f[1].parse::<usize>().ok()?, f[2].starts_with('K')))
                } else {
                    None
                }
            })
            .collect()
    }

    fn gen_nut(codec: &str, path: &str) -> bool {
        Command::new("ffmpeg")
            .args([
                "-hide_banner",
                "-loglevel",
                "error",
                "-y",
                "-f",
                "lavfi",
                "-i",
                "testsrc=size=320x240:rate=30",
                "-frames:v",
                "30",
                "-c:v",
                codec,
                "-bf",
                "0",
                "-g",
                "10",
                "-pix_fmt",
                "yuv420p",
                "-flush_packets",
                "1",
                "-f",
                "nut",
                path,
            ])
            .status()
            .map(|s| s.success())
            .unwrap_or(false)
    }

    fn drain_all(reader: &mut NutReader) -> Vec<(usize, bool)> {
        let mut out = Vec::new();
        while let Some(f) = reader.next_frame().expect("nut parse") {
            out.push((f.data.len(), f.keyframe));
        }
        out
    }

    #[test]
    fn nut_matches_ffprobe_across_codecs() {
        if !have("ffmpeg") || !have("ffprobe") {
            eprintln!("ffmpeg/ffprobe absent; skipping NUT validation");
            return;
        }
        for codec in ["libopenh264", "libvpx-vp9", "libsvtav1"] {
            let path = format!(
                "{}/imzero2_nuttest_{}_{}.nut",
                std::env::temp_dir().display(),
                codec.replace('-', "_"),
                std::process::id()
            );
            if !gen_nut(codec, &path) {
                eprintln!("encode with {codec} failed; skipping this codec");
                continue;
            }
            let data = std::fs::read(&path).expect("read nut");
            let expected = ffprobe_packets(&path);
            let _ = std::fs::remove_file(&path);

            // Whole-buffer parse.
            let mut reader = NutReader::new();
            reader.push(&data);
            let got = drain_all(&mut reader);
            assert_eq!(
                got, expected,
                "{codec}: NUT reader frames must match ffprobe packets"
            );
            assert!(got[0].1, "{codec}: first frame must be a key frame");

            // Incremental parse (7-byte chunks) must yield identical frames.
            let mut reader = NutReader::new();
            let mut got_inc = Vec::new();
            for chunk in data.chunks(7) {
                reader.push(chunk);
                while let Some(f) = reader.next_frame().expect("nut parse (chunked)") {
                    got_inc.push((f.data.len(), f.keyframe));
                }
            }
            assert_eq!(
                got_inc, expected,
                "{codec}: chunked NUT parse must match ffprobe packets"
            );
        }
    }
}
