use std::io::Read;
use crate::fffi::common::{FffiError,FffiResult};

/// Dense block map: all block data in a single contiguous slab, indexed by
/// a flat `Vec<(u32, u32)>` of (offset, length) pairs. Lookup is a single
/// multiply-add — no hashing, no pointer chasing.
///
/// Use for tables where every (row, col) cell is populated.
/// Falls back gracefully for sparse data (unpopulated cells return empty slices).
pub struct DenseBlockMap {
    /// Contiguous slab holding all block opcode bytes.
    slab: Vec<u8>,
    /// Flat index: `entries[row * col_count + col] = (offset, length)` into slab.
    /// (0, 0) means no block at this position.
    entries: Vec<(u32, u32)>,
    col_count: usize,
}

impl DenseBlockMap {
    /// Look up block data for (row, col). Returns empty slice if not populated.
    #[inline(always)]
    pub fn get(&self, row: u64, col: u32) -> &[u8] {
        let idx = row as usize * self.col_count + col as usize;
        if idx < self.entries.len() {
            let (off, len) = self.entries[idx];
            if len > 0 {
                return &self.slab[off as usize..(off + len) as usize];
            }
        }
        &[]
    }
}

/// Lightweight replay reader that reuses its buffer across calls.
/// After the first frame, `load()` is a memcpy into existing capacity — zero allocations.
struct ReplayReader {
    buf: Vec<u8>,
    pos: usize,
}

impl ReplayReader {
    fn new() -> Self {
        Self { buf: Vec::new(), pos: 0 }
    }

    /// Copy block data into the reusable buffer and reset read position.
    /// If `self.buf` already has enough capacity (from a prior replay),
    /// this is a plain memcpy with no allocation.
    fn load(&mut self, data: &[u8]) {
        self.buf.clear();
        self.buf.extend_from_slice(data);
        self.pos = 0;
    }

    #[inline(always)]
    fn read_exact(&mut self, out: &mut [u8]) -> Result<(), std::io::Error> {
        let end = self.pos + out.len();
        if end > self.buf.len() {
            return Err(std::io::Error::from(std::io::ErrorKind::UnexpectedEof));
        }
        out.copy_from_slice(&self.buf[self.pos..end]);
        self.pos = end;
        Ok(())
    }

    #[inline(always)]
    fn read(&mut self, out: &mut [u8]) -> Result<usize, std::io::Error> {
        let available = self.buf.len() - self.pos;
        let n = std::cmp::min(out.len(), available);
        out[..n].copy_from_slice(&self.buf[self.pos..self.pos + n]);
        self.pos += n;
        Ok(n)
    }
}

pub struct ImZeroFffiIo<R: std::io::BufRead, W: std::io::Write> {
    pub r: R,
    pub w: W,
    pub written_bytes_count: usize,
    pub read_bytes_count: usize,
    pub flush_count: usize,

    /// Pool of replay readers, indexed by nesting depth.
    /// Readers beyond `replay_depth` are inactive but retain their buffer
    /// allocations for reuse in future frames — this is the key optimization.
    replay_readers: Vec<ReplayReader>,
    /// Current nesting depth (0 = reading from pipe, 1+ = replaying).
    replay_depth: usize,
    /// Stack of saved `read_bytes_count` values, one per active replay level.
    /// Restored in `end_replay()` so replay reads don't inflate pipe accounting.
    replay_saved_read_bytes_counts: Vec<usize>,
}
impl<R: std::io::BufRead,W: std::io::Write> ImZeroFffiIo<R,W> {
    pub fn new(r: R, w: W) -> Self {
        return Self {
            r,
            w,
            written_bytes_count: 0,
            read_bytes_count: 0,
            flush_count: 0,
            replay_readers: Vec::new(),
            replay_depth: 0,
            replay_saved_read_bytes_counts: Vec::new(),
        }
    }
    pub fn reset_counts(&mut self) {
        self.written_bytes_count = 0;
        self.read_bytes_count = 0;
        self.flush_count = 0;
    }

    // =========================================================================
    // Deferred block replay support
    // =========================================================================

    /// Returns true if we are currently replaying a deferred block.
    #[inline(always)]
    pub fn is_replaying(&self) -> bool {
        self.replay_depth > 0
    }

    /// Push an overlay reader to replay a deferred opcode block.
    /// Supports nesting: multiple begin_replay calls stack, each end_replay pops one.
    /// Saves `read_bytes_count` so replay reads don't pollute pipe accounting.
    ///
    /// On the first call at a given depth, allocates a ReplayReader + buffer.
    /// On subsequent calls at the same depth, reuses the existing reader and
    /// its buffer capacity — the `load()` call is a memcpy, not an allocation.
    pub fn begin_replay(&mut self, block: &[u8]) {
        self.replay_saved_read_bytes_counts.push(self.read_bytes_count);
        let depth = self.replay_depth;
        if depth < self.replay_readers.len() {
            // Reuse existing reader at this depth — zero allocation
            self.replay_readers[depth].load(block);
        } else {
            // First time at this depth — allocate reader (once per depth level)
            let mut reader = ReplayReader::new();
            reader.load(block);
            self.replay_readers.push(reader);
        }
        self.replay_depth += 1;
    }

    /// Pop the top overlay reader, restoring the previous replay level (or pipe).
    /// The reader is NOT deallocated — its buffer stays for reuse next frame.
    /// Restores `read_bytes_count` to its pre-replay value.
    pub fn end_replay(&mut self) {
        assert!(self.replay_depth > 0, "end_replay without matching begin_replay");
        self.replay_depth -= 1;
        self.read_bytes_count = self.replay_saved_read_bytes_counts.pop()
            .expect("end_replay without matching begin_replay");
    }

    // =========================================================================
    // Internal: active reader dispatch
    // =========================================================================

    /// Read exact bytes from the active reader (replay overlay or pipe).
    #[inline(always)]
    fn read_exact_active(&mut self, buf: &mut [u8]) -> Result<(), FffiError> {
        self.read_bytes_count += buf.len();
        if self.replay_depth > 0 {
            self.replay_readers[self.replay_depth - 1].read_exact(buf)?;
        } else {
            self.r.read_exact(buf)?;
        }
        Ok(())
    }

    /// Read from the active reader (replay overlay or pipe).
    /// Returns the number of bytes read.
    #[inline(always)]
    fn read_active(&mut self, buf: &mut [u8]) -> Result<usize, std::io::Error> {
        if self.replay_depth > 0 {
            self.replay_readers[self.replay_depth - 1].read(buf)
        } else {
            self.r.read(buf)
        }
    }

    // =========================================================================
    // Write methods (unchanged)
    // =========================================================================

    pub fn flush(&mut self) -> Result<(),FffiError> {
        self.flush_count += 1;
        self.w.flush()?;
        return Ok(());
    }
    pub fn write_all(&mut self, buf: &[u8]) -> Result<(),FffiError> {
        self.w.write_all(buf)?;
        self.written_bytes_count += buf.len();
        return Ok(());
    }
    pub fn write_plain_u8(&mut self,v: u8) -> Result<(),FffiError> {
        let buffer = [v];
        self.write_all(&buffer)?;
        return Ok(());
    }
    pub fn write_plain_u16(&mut self,v: u16) -> Result<(),FffiError> {
        let buffer = v.to_le_bytes();
        self.write_all(&buffer)?;
        return Ok(());
    }
    pub fn write_plain_u32(&mut self,v: u32) -> Result<(),FffiError> {
        let buffer = v.to_le_bytes();
        self.write_all(&buffer)?;
        return Ok(());
    }
    pub fn write_plain_u64(&mut self,v: u64) -> Result<(),FffiError> {
        let buffer = v.to_le_bytes();
        self.write_all(&buffer)?;
        return Ok(());
    }
    pub fn write_plain_i64(&mut self,v: i64) -> Result<(),FffiError> {
        let buffer = v.to_le_bytes();
        self.write_all(&buffer)?;
        return Ok(());
    }
    pub fn write_plain_s(&mut self,v: String) -> Result<(),FffiError> {
        let l = v.len();
        self.write_plain_u32(l as u32)?;
        if l > 0 {
            self.write_all(v.as_bytes())?;
        }
        return Ok(());
    }
    pub fn write_plain_u32h(&mut self,len: usize,it: impl IntoIterator<Item=u32>) -> Result<(),FffiError> {
        self.write_plain_u32(len as u32)?;
        for e in it.into_iter() {
            self.write_plain_u32(e)?;
        }
        return Ok(());
    }
    pub fn write_plain_u64h(&mut self,len: usize, it: impl IntoIterator<Item=u64>) -> Result<(),FffiError> {
        self.write_plain_u32(len as u32)?;
        for e in it.into_iter() {
            self.write_plain_u64(e)?;
        }
        return Ok(());
    }
    pub fn write_plain_f32h(&mut self,len: usize, it: impl IntoIterator<Item=f32>) -> Result<(),FffiError> {
        self.write_plain_u32(len as u32)?;
        for e in it.into_iter() {
            self.write_plain_f32(e)?;
        }
        return Ok(());
    }
    pub fn write_plain_f64h(&mut self,len: usize, it: impl IntoIterator<Item=f64>) -> Result<(),FffiError> {
        self.write_plain_u32(len as u32)?;
        for e in it.into_iter() {
            self.write_plain_f64(e)?;
        }
        return Ok(());
    }
    pub fn write_plain_i64h(&mut self,len: usize, it: impl IntoIterator<Item=i64>) -> Result<(),FffiError> {
        self.write_plain_u32(len as u32)?;
        for e in it.into_iter() {
            self.write_plain_i64(e)?;
        }
        return Ok(());
    }
    pub fn write_plain_sh(&mut self,len: usize, it: impl IntoIterator<Item=String>) -> Result<(),FffiError> {
        self.write_plain_u32(len as u32)?;
        for e in it.into_iter() {
            self.write_plain_s(e)?;
        }
        return Ok(());
    }
    pub fn write_plain_f32(&mut self,v: f32) -> Result<(),FffiError> {
        return self.write_plain_u32(v.to_bits());
    }
    pub fn write_plain_f64(&mut self,v: f64) -> Result<(),FffiError> {
        return self.write_plain_u64(v.to_bits());
    }
    pub fn write_plain_b(&mut self,v: bool) -> Result<(),FffiError> {
        return self.write_plain_u8(if v { 1u8 } else { 0u8 });
    }

    // =========================================================================
    // Read methods (updated to use active reader dispatch)
    // =========================================================================

    pub fn read_plain_u8(&mut self) -> FffiResult<u8> {
        let mut buffer: [u8; 1] = [0; 1];
        self.read_exact_active(&mut buffer)?;
        return Ok(u8::from_le_bytes(buffer));
    }
    pub fn read_plain_u16(&mut self) -> FffiResult<u16> {
        let mut buffer: [u8; 2] = [0; 2];
        self.read_exact_active(&mut buffer)?;
        return Ok(u16::from_le_bytes(buffer));
    }
    pub fn read_plain_u32(&mut self) -> FffiResult<u32> {
        let mut buffer: [u8; 4] = [0; 4];
        self.read_exact_active(&mut buffer)?;
        return Ok(u32::from_le_bytes(buffer));
    }
    pub fn read_plain_u64(&mut self) -> FffiResult<u64> {
        let mut buffer: [u8; 8] = [0; 8];
        self.read_exact_active(&mut buffer)?;
        return Ok(u64::from_le_bytes(buffer));
    }
    pub fn read_plain_i8(&mut self) -> FffiResult<i8> {
        let mut buffer: [u8; 1] = [0; 1];
        self.read_exact_active(&mut buffer)?;
        return Ok(i8::from_le_bytes(buffer));
    }
    pub fn read_plain_i16(&mut self) -> FffiResult<i16> {
        let mut buffer: [u8; 2] = [0; 2];
        self.read_exact_active(&mut buffer)?;
        return Ok(i16::from_le_bytes(buffer));
    }
    pub fn read_plain_i32(&mut self) -> FffiResult<i32> {
        let mut buffer: [u8; 4] = [0; 4];
        self.read_exact_active(&mut buffer)?;
        return Ok(i32::from_le_bytes(buffer));
    }
    pub fn read_plain_i64(&mut self) -> FffiResult<i64> {
        let mut buffer: [u8; 8] = [0; 8];
        self.read_exact_active(&mut buffer)?;
        return Ok(i64::from_le_bytes(buffer));
    }
    pub fn read_plain_f32(&mut self) -> FffiResult<f32> {
        let u = self.read_plain_u32()?;
        return Ok(f32::from_bits(u));
    }
    pub fn read_plain_f64(&mut self) -> FffiResult<f64> {
        let u = self.read_plain_u64()?;
        return Ok(f64::from_bits(u));
    }
    pub fn read_plain_s(&mut self) -> FffiResult<String> {
        let len_offset = self.read_bytes_count;
        let len = self.read_plain_u32()?;
        let body_offset = self.read_bytes_count;
        let mut buffer: Vec<u8> = vec![0; len as usize];
        // read_bytes_count already updated by read_plain_u32 above;
        // now account for the string body bytes
        self.read_bytes_count += len as usize;
        if self.replay_depth > 0 {
            self.replay_readers[self.replay_depth - 1].read_exact(&mut buffer)?;
        } else {
            self.r.read_exact(&mut buffer)?;
        }
        match String::from_utf8(buffer) {
            Ok(s) => Ok(s),
            Err(e) => {
                // Non-UTF-8 string on the wire. The Go-side encoder treats
                // `string` as raw bytes — if a cell value came from an Arrow
                // column whose contents aren't valid UTF-8 (e.g. a Binary
                // column formatted via `ValueStr` instead of hex, or a
                // String column carrying raw bytes), those bytes end up
                // here. Log the offending bytes so the operator can match
                // them against the source column, then return a lossy
                // decoded String so the FFFI wire stays in sync (returning
                // Err would propagate through a `let _ =
                // self.interpret_outer(...)` closure and desync the pipe).
                let bad = e.as_bytes();
                let preview_len = bad.len().min(64);
                let hex_preview: String = bad[..preview_len]
                    .iter()
                    .map(|b| format!("{:02x}", b))
                    .collect::<Vec<_>>()
                    .join(" ");
                let ascii_preview: String = bad[..preview_len]
                    .iter()
                    .map(|&b| if (0x20..0x7f).contains(&b) { b as char } else { '·' })
                    .collect();
                tracing::error!(
                    str_len = len,
                    str_len_offset = len_offset,
                    str_body_offset = body_offset,
                    str_bytes_hex = %hex_preview,
                    str_bytes_ascii = %ascii_preview,
                    truncated = bad.len() > preview_len,
                    utf8_error = %e.utf8_error(),
                    "read_plain_s: non-UTF-8 bytes on wire — substituting replacement chars and \
                     continuing so the protocol stays in sync. The Go-side encoder wrote raw \
                     bytes where a UTF-8 string was expected; fix the source column (most \
                     often an Arrow type whose `*.Value()` / `ValueStr()` returned binary \
                     content that wasn't routed through hex encoding)."
                );
                Ok(String::from_utf8_lossy(bad).into_owned())
            }
        }
    }
    pub fn read_plain_f32h(&mut self) -> FffiResult<Vec<f32>> {
        let len = self.read_plain_u32()? as usize;
        let mut v = Vec::with_capacity(len);
        for _ in 0..len {
            v.push(self.read_plain_f32()?);
        }
        Ok(v)
    }
    pub fn read_plain_f64h(&mut self) -> FffiResult<Vec<f64>> {
        let len = self.read_plain_u32()? as usize;
        let mut v = Vec::with_capacity(len);
        for _ in 0..len {
            v.push(self.read_plain_f64()?);
        }
        Ok(v)
    }
    pub fn read_plain_u64h(&mut self) -> FffiResult<Vec<u64>> {
        let len = self.read_plain_u32()? as usize;
        let mut v = Vec::with_capacity(len);
        for _ in 0..len {
            v.push(self.read_plain_u64()?);
        }
        Ok(v)
    }
    pub fn read_plain_i64h(&mut self) -> FffiResult<Vec<i64>> {
        let len = self.read_plain_u32()? as usize;
        let mut v = Vec::with_capacity(len);
        for _ in 0..len {
            v.push(self.read_plain_i64()?);
        }
        Ok(v)
    }
    pub fn read_plain_u32h(&mut self) -> FffiResult<Vec<u32>> {
        let len = self.read_plain_u32()? as usize;
        let mut v = Vec::with_capacity(len);
        for _ in 0..len {
            v.push(self.read_plain_u32()?);
        }
        Ok(v)
    }
    pub fn read_plain_u8h(&mut self) -> FffiResult<Vec<u8>> {
        let len = self.read_plain_u32()? as usize;
        let mut v = Vec::with_capacity(len);
        for _ in 0..len {
            v.push(self.read_plain_u8()?);
        }
        Ok(v)
    }
    pub fn read_plain_sh(&mut self) -> FffiResult<Vec<String>> {
        let len = self.read_plain_u32()? as usize;
        let mut v = Vec::with_capacity(len);
        for _ in 0..len {
            v.push(self.read_plain_s()?);
        }
        Ok(v)
    }
    pub fn read_plain_b(&mut self) -> FffiResult<bool> {
        let v = self.read_plain_u8()?;
        return Ok(v != 0)
    }
    pub fn skip(&mut self, skip: usize) -> FffiResult<()> {
        // see https://github.com/rust-lang/rust/issues/53294
        if skip == 0 {
            return Ok(())
        }

        let mut buf = [0u8; 1024];
        let mut total = 0;
        while total < skip {
            let len = std::cmp::min(skip - total, buf.len());
            match self.read_active(&mut buf[..len]) {
                Ok(0) => break,
                Ok(n) => {
                    total += n;
                    self.read_bytes_count += n;
                }
                Err(ref e) if e.kind() == std::io::ErrorKind::Interrupted => {}
                Err(e) => return Err(FffiError::Io(e)),
            };
            debug_assert!(total <= skip);
        }
        return Ok(())
    }

    // =========================================================================
    // Deferred block map deserialization helpers
    // =========================================================================

    /// Read a deferred block map with key type (u64, u32) from the active reader.
    ///
    /// Wire format:
    ///   u32: block_count
    ///   for each block:
    ///     u64: key_part_0
    ///     u32: key_part_1
    ///     u32: block_byte_length
    ///     [u8; block_byte_length]: raw opcodes
    pub fn read_deferred_block_map_u64_u32(
        &mut self,
    ) -> FffiResult<std::collections::HashMap<(u64, u32), Vec<u8>>> {
        let count = self.read_plain_u32()? as usize;
        let mut map = std::collections::HashMap::with_capacity(count);
        for _ in 0..count {
            let key_0 = self.read_plain_u64()?;
            let key_1 = self.read_plain_u32()?;
            let buf_len = self.read_plain_u32()? as usize;
            let mut buf = vec![0u8; buf_len];
            self.read_exact_active(&mut buf)?;
            map.insert((key_0, key_1), buf);
        }
        Ok(map)
    }

    /// Read a deferred block map with key type (u64, u32) into a dense flat layout.
    ///
    /// Same wire format as `read_deferred_block_map_u64_u32`, but stores all block
    /// data in a single contiguous slab with O(1) indexed lookup instead of a HashMap.
    /// `num_rows` and `col_count` define the flat index dimensions.
    ///
    /// For a 10k x 3 table this eliminates:
    ///   - 30,000 individual `Vec<u8>` allocations (one slab instead)
    ///   - HashMap bucket array + entry overhead
    ///   - SipHash computation per lookup
    pub fn read_deferred_block_map_dense_u64_u32(
        &mut self,
        num_rows: u64,
        col_count: usize,
    ) -> FffiResult<DenseBlockMap> {
        let count = self.read_plain_u32()? as usize;
        let total = num_rows as usize * col_count;
        let mut entries = vec![(0u32, 0u32); total];
        // Pre-size slab: average ~40 bytes per block is a reasonable estimate.
        // extend_from_slice will grow if needed, but this avoids most reallocations.
        let mut slab = Vec::with_capacity(count * 40);
        for _ in 0..count {
            let row = self.read_plain_u64()?;
            let col = self.read_plain_u32()?;
            let buf_len = self.read_plain_u32()? as usize;
            let offset = slab.len() as u32;
            slab.resize(slab.len() + buf_len, 0u8);
            self.read_exact_active(&mut slab[offset as usize..offset as usize + buf_len])?;
            let idx = row as usize * col_count + col as usize;
            if idx < total {
                entries[idx] = (offset, buf_len as u32);
            }
        }
        Ok(DenseBlockMap { slab, entries, col_count })
    }

    /// Skip a deferred block map with key type (u64, u32) without deserializing.
    pub fn skip_deferred_block_map_u64_u32(&mut self) -> FffiResult<()> {
        let count = self.read_plain_u32()? as usize;
        for _ in 0..count {
            let _key_0 = self.read_plain_u64()?;
            let _key_1 = self.read_plain_u32()?;
            let buf_len = self.read_plain_u32()? as usize;
            self.skip(buf_len)?;
        }
        Ok(())
    }

    /// Read a deferred block map with key type (u32, u32) from the active reader.
    pub fn read_deferred_block_map_u32_u32(
        &mut self,
    ) -> FffiResult<std::collections::HashMap<(u32, u32), Vec<u8>>> {
        let count = self.read_plain_u32()? as usize;
        let mut map = std::collections::HashMap::with_capacity(count);
        for _ in 0..count {
            let key_0 = self.read_plain_u32()?;
            let key_1 = self.read_plain_u32()?;
            let buf_len = self.read_plain_u32()? as usize;
            let mut buf = vec![0u8; buf_len];
            self.read_exact_active(&mut buf)?;
            map.insert((key_0, key_1), buf);
        }
        Ok(map)
    }

    /// Skip a deferred block map with key type (u32, u32) without deserializing.
    pub fn skip_deferred_block_map_u32_u32(&mut self) -> FffiResult<()> {
        let count = self.read_plain_u32()? as usize;
        for _ in 0..count {
            let _key_0 = self.read_plain_u32()?;
            let _key_1 = self.read_plain_u32()?;
            let buf_len = self.read_plain_u32()? as usize;
            self.skip(buf_len)?;
        }
        Ok(())
    }

    /// Read a deferred block map with key type (u32) from the active reader.
    pub fn read_deferred_block_map_u32(
        &mut self,
    ) -> FffiResult<std::collections::HashMap<u32, Vec<u8>>> {
        let count = self.read_plain_u32()? as usize;
        let mut map = std::collections::HashMap::with_capacity(count);
        for _ in 0..count {
            let key = self.read_plain_u32()?;
            let buf_len = self.read_plain_u32()? as usize;
            let mut buf = vec![0u8; buf_len];
            self.read_exact_active(&mut buf)?;
            map.insert(key, buf);
        }
        Ok(map)
    }

    /// Skip a deferred block map with key type (u32) without deserializing.
    pub fn skip_deferred_block_map_u32(&mut self) -> FffiResult<()> {
        let count = self.read_plain_u32()? as usize;
        for _ in 0..count {
            let _key = self.read_plain_u32()?;
            let buf_len = self.read_plain_u32()? as usize;
            self.skip(buf_len)?;
        }
        Ok(())
    }

    /// Read a deferred block map with key type (u64) from the active reader.
    pub fn read_deferred_block_map_u64(
        &mut self,
    ) -> FffiResult<std::collections::HashMap<u64, Vec<u8>>> {
        let count = self.read_plain_u32()? as usize;
        let mut map = std::collections::HashMap::with_capacity(count);
        for _ in 0..count {
            let key = self.read_plain_u64()?;
            let buf_len = self.read_plain_u32()? as usize;
            let mut buf = vec![0u8; buf_len];
            self.read_exact_active(&mut buf)?;
            map.insert(key, buf);
        }
        Ok(map)
    }

    /// Skip a deferred block map with key type (u64) without deserializing.
    pub fn skip_deferred_block_map_u64(&mut self) -> FffiResult<()> {
        let count = self.read_plain_u32()? as usize;
        for _ in 0..count {
            let _key = self.read_plain_u64()?;
            let buf_len = self.read_plain_u32()? as usize;
            self.skip(buf_len)?;
        }
        Ok(())
    }
    /// Read exact bytes from the active reader into the provided buffer.
    /// Public alias for `read_exact_active` — used by generated code.
    #[inline(always)]
    pub fn read_exact(&mut self, buf: &mut [u8]) -> FffiResult<()> {
        self.read_exact_active(buf)
    }
    /// Alias for `skip` — used by generated code.
    #[inline(always)]
    pub fn skip_bytes(&mut self, n: usize) -> FffiResult<()> {
        self.skip(n)
    }
}
