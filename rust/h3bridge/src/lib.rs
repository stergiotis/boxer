//! H3 bridge — linear-memory ABI over [`h3o`] intended for the
//! `wasm32-unknown-unknown` target. The Go host (`public/science/geo/h3`)
//! allocates buffers via [`ext_alloc`] / [`ext_free`] and calls the `h3_*`
//! entry points to transform Struct-of-Arrays batches.
//!
//! See `doc/adr/0003-h3-wasm-bridge.md` in the parent repository for the
//! design; the ABI shape (fixed-arity vs. CSR + one-retry grow protocol) is
//! fixed there and must be kept in lock-step with the Go side.

#![deny(unsafe_op_in_unsafe_fn)]

use core::slice;

use h3o::{
    geom::{ContainmentMode, TilerBuilder},
    CellIndex, LatLng, Resolution,
};

// Custom getrandom for wasm32-unknown-unknown. ahash (transitively pulled by
// h3o[geo]) needs a runtime seed. We return a deterministic constant stream
// to preserve byte-reproducibility of the committed wasm artifact; no code
// path here uses this for cryptographic purposes.
fn boxer_fixed_getrandom(buf: &mut [u8]) -> Result<(), getrandom::Error> {
    for (i, b) in buf.iter_mut().enumerate() {
        *b = (i as u8).wrapping_mul(0x9b);
    }
    Ok(())
}
getrandom::register_custom_getrandom!(boxer_fixed_getrandom);

// Per-element status byte. Kept in sync with `StatusE` in the Go package.
pub const STATUS_OK: u8 = 0;
pub const STATUS_INVALID_LATLNG: u8 = 1;
pub const STATUS_INVALID_CELL: u8 = 2;
pub const STATUS_INVALID_RESOLUTION: u8 = 3;
pub const STATUS_INVALID_STRING: u8 = 4;
pub const STATUS_INTERNAL: u8 = 5;

// Variable-arity return code.
const GROW_OK: u32 = 0;
const GROW_NEED_MORE: u32 = 1;
const GROW_BAD_RESOLUTION: u32 = 2;

// Polyfill-specific return codes. 0/1 match GROW_OK/GROW_NEED_MORE.
const POLYFILL_BAD_MODE: u32 = 2;
const POLYFILL_BAD_GEOMETRY: u32 = 3;

// Compact return codes.
const COMPACT_OK: u32 = 0;
const COMPACT_MIXED_RESOLUTION: u32 = 1;
const COMPACT_DUPLICATE: u32 = 2;
const COMPACT_INVALID_CELL: u32 = 3;

#[no_mangle]
pub extern "C" fn ext_alloc(n: u32) -> u32 {
    let mut v: Vec<u8> = Vec::with_capacity(n as usize);
    let ptr = v.as_mut_ptr() as u32;
    core::mem::forget(v);
    ptr
}

#[no_mangle]
pub extern "C" fn ext_free(off: u32, n: u32) {
    if n == 0 {
        return;
    }
    unsafe {
        drop(Vec::<u8>::from_raw_parts(off as *mut u8, 0, n as usize));
    }
}

fn resolution_from_u32(res: u32) -> Option<Resolution> {
    u8::try_from(res).ok().and_then(|r| Resolution::try_from(r).ok())
}

unsafe fn as_f64_slice<'a>(ptr: u32, n: u32) -> &'a [f64] {
    unsafe { slice::from_raw_parts(ptr as *const f64, n as usize) }
}

unsafe fn as_u64_slice<'a>(ptr: u32, n: u32) -> &'a [u64] {
    unsafe { slice::from_raw_parts(ptr as *const u64, n as usize) }
}

unsafe fn as_f64_slice_mut<'a>(ptr: u32, n: u32) -> &'a mut [f64] {
    unsafe { slice::from_raw_parts_mut(ptr as *mut f64, n as usize) }
}

unsafe fn as_u64_slice_mut<'a>(ptr: u32, n: u32) -> &'a mut [u64] {
    unsafe { slice::from_raw_parts_mut(ptr as *mut u64, n as usize) }
}

unsafe fn as_i32_slice_mut<'a>(ptr: u32, n: u32) -> &'a mut [i32] {
    unsafe { slice::from_raw_parts_mut(ptr as *mut i32, n as usize) }
}

unsafe fn as_u8_slice_mut<'a>(ptr: u32, n: u32) -> &'a mut [u8] {
    unsafe { slice::from_raw_parts_mut(ptr as *mut u8, n as usize) }
}

unsafe fn as_u8_slice<'a>(ptr: u32, n: u32) -> &'a [u8] {
    unsafe { slice::from_raw_parts(ptr as *const u8, n as usize) }
}

unsafe fn as_i32_slice<'a>(ptr: u32, n: u32) -> &'a [i32] {
    unsafe { slice::from_raw_parts(ptr as *const i32, n as usize) }
}

// --- latlng <-> cell ------------------------------------------------------

#[no_mangle]
pub extern "C" fn h3_latlng_to_cell(
    lats_ptr: u32,
    lngs_ptr: u32,
    n: u32,
    res: u32,
    cells_ptr: u32,
    status_ptr: u32,
) {
    let lats = unsafe { as_f64_slice(lats_ptr, n) };
    let lngs = unsafe { as_f64_slice(lngs_ptr, n) };
    let cells = unsafe { as_u64_slice_mut(cells_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    let r = match resolution_from_u32(res) {
        Some(r) => r,
        None => {
            for i in 0..n as usize {
                cells[i] = 0;
                status[i] = STATUS_INVALID_RESOLUTION;
            }
            return;
        }
    };

    for i in 0..n as usize {
        match LatLng::new(lats[i], lngs[i]) {
            Ok(ll) => {
                cells[i] = u64::from(ll.to_cell(r));
                status[i] = STATUS_OK;
            }
            Err(_) => {
                cells[i] = 0;
                status[i] = STATUS_INVALID_LATLNG;
            }
        }
    }
}

#[no_mangle]
pub extern "C" fn h3_cell_to_latlng(
    cells_ptr: u32,
    n: u32,
    lats_ptr: u32,
    lngs_ptr: u32,
    status_ptr: u32,
) {
    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let lats = unsafe { as_f64_slice_mut(lats_ptr, n) };
    let lngs = unsafe { as_f64_slice_mut(lngs_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    for i in 0..n as usize {
        match CellIndex::try_from(cells[i]) {
            Ok(c) => {
                let ll = LatLng::from(c);
                lats[i] = ll.lat();
                lngs[i] = ll.lng();
                status[i] = STATUS_OK;
            }
            Err(_) => {
                lats[i] = 0.0;
                lngs[i] = 0.0;
                status[i] = STATUS_INVALID_CELL;
            }
        }
    }
}

// --- hierarchy ------------------------------------------------------------

#[no_mangle]
pub extern "C" fn h3_cell_to_parent(
    cells_ptr: u32,
    n: u32,
    res: u32,
    parents_ptr: u32,
    status_ptr: u32,
) {
    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let parents = unsafe { as_u64_slice_mut(parents_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    let r = match resolution_from_u32(res) {
        Some(r) => r,
        None => {
            for i in 0..n as usize {
                parents[i] = 0;
                status[i] = STATUS_INVALID_RESOLUTION;
            }
            return;
        }
    };

    for i in 0..n as usize {
        match CellIndex::try_from(cells[i]) {
            Ok(c) => match c.parent(r) {
                Some(p) => {
                    parents[i] = u64::from(p);
                    status[i] = STATUS_OK;
                }
                None => {
                    parents[i] = 0;
                    status[i] = STATUS_INVALID_RESOLUTION;
                }
            },
            Err(_) => {
                parents[i] = 0;
                status[i] = STATUS_INVALID_CELL;
            }
        }
    }
}

#[no_mangle]
pub extern "C" fn h3_cell_to_children(
    cells_ptr: u32,
    n: u32,
    res: u32,
    children_ptr: u32,
    offsets_ptr: u32,
    cap: u32,
    needed_ptr: u32,
    status_ptr: u32,
) -> u32 {
    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    let r = match resolution_from_u32(res) {
        Some(r) => r,
        None => return GROW_BAD_RESOLUTION,
    };

    let mut total: u64 = 0;
    for i in 0..n as usize {
        match CellIndex::try_from(cells[i]) {
            Ok(c) => {
                if let Some(p) = c.resolution().succ() {
                    let _ = p; // keep type inference happy
                }
                if r < c.resolution() {
                    status[i] = STATUS_INVALID_RESOLUTION;
                } else {
                    total = total.saturating_add(c.children_count(r));
                    status[i] = STATUS_OK;
                }
            }
            Err(_) => {
                status[i] = STATUS_INVALID_CELL;
            }
        }
    }

    let total_u32 = u32::try_from(total).unwrap_or(u32::MAX);
    if total_u32 > cap {
        unsafe {
            *(needed_ptr as *mut u32) = total_u32;
        }
        return GROW_NEED_MORE;
    }

    let children = unsafe { as_u64_slice_mut(children_ptr, total_u32) };
    let offsets = unsafe { as_i32_slice_mut(offsets_ptr, n + 1) };
    offsets[0] = 0;
    let mut w: usize = 0;
    for i in 0..n as usize {
        if status[i] == STATUS_OK {
            if let Ok(c) = CellIndex::try_from(cells[i]) {
                for child in c.children(r) {
                    children[w] = u64::from(child);
                    w += 1;
                }
            }
        }
        offsets[i + 1] = w as i32;
    }
    unsafe {
        *(needed_ptr as *mut u32) = total_u32;
    }
    GROW_OK
}

// --- traversal ------------------------------------------------------------

#[no_mangle]
pub extern "C" fn h3_grid_disk(
    cells_ptr: u32,
    n: u32,
    k: u32,
    out_ptr: u32,
    offsets_ptr: u32,
    cap: u32,
    needed_ptr: u32,
    status_ptr: u32,
) -> u32 {
    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    // Max ring size for non-pentagonal origin: 3*k*(k+1) + 1.
    // Pentagonal origins are smaller, so this is a safe upper bound.
    let per_cell_max: u64 = 3u64.saturating_mul(k as u64).saturating_mul(k as u64 + 1) + 1;
    let total_upper: u64 = per_cell_max.saturating_mul(n as u64);
    let total_upper_u32 = u32::try_from(total_upper).unwrap_or(u32::MAX);
    if total_upper_u32 > cap {
        unsafe {
            *(needed_ptr as *mut u32) = total_upper_u32;
        }
        // Mark per-cell status as OK in principle; caller will retry.
        for s in status.iter_mut() {
            *s = STATUS_OK;
        }
        return GROW_NEED_MORE;
    }

    let out = unsafe { as_u64_slice_mut(out_ptr, total_upper_u32) };
    let offsets = unsafe { as_i32_slice_mut(offsets_ptr, n + 1) };
    offsets[0] = 0;
    let mut w: usize = 0;
    for i in 0..n as usize {
        match CellIndex::try_from(cells[i]) {
            Ok(c) => {
                for neighbour in c.grid_disk::<Vec<CellIndex>>(k) {
                    out[w] = u64::from(neighbour);
                    w += 1;
                }
                status[i] = STATUS_OK;
            }
            Err(_) => {
                status[i] = STATUS_INVALID_CELL;
            }
        }
        offsets[i + 1] = w as i32;
    }
    unsafe {
        *(needed_ptr as *mut u32) = w as u32;
    }
    GROW_OK
}

// --- string ---------------------------------------------------------------

#[no_mangle]
pub extern "C" fn h3_cell_to_string(
    cells_ptr: u32,
    n: u32,
    buf_ptr: u32,
    offsets_ptr: u32,
    cap: u32,
    needed_ptr: u32,
    status_ptr: u32,
) -> u32 {
    // H3 hex strings are 15 chars. We over-provision to 16 for safety.
    const MAX_PER_CELL: u32 = 16;

    let upper = MAX_PER_CELL.saturating_mul(n);
    if upper > cap {
        unsafe {
            *(needed_ptr as *mut u32) = upper;
        }
        return GROW_NEED_MORE;
    }

    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };
    let buf = unsafe { as_u8_slice_mut(buf_ptr, upper) };
    let offsets = unsafe { as_i32_slice_mut(offsets_ptr, n + 1) };
    offsets[0] = 0;
    let mut w: usize = 0;
    for i in 0..n as usize {
        match CellIndex::try_from(cells[i]) {
            Ok(c) => {
                let s = format!("{c}");
                let bytes = s.as_bytes();
                buf[w..w + bytes.len()].copy_from_slice(bytes);
                w += bytes.len();
                status[i] = STATUS_OK;
            }
            Err(_) => {
                status[i] = STATUS_INVALID_CELL;
            }
        }
        offsets[i + 1] = w as i32;
    }
    unsafe {
        *(needed_ptr as *mut u32) = w as u32;
    }
    GROW_OK
}

#[no_mangle]
pub extern "C" fn h3_string_to_cell(
    buf_ptr: u32,
    offsets_ptr: u32,
    n: u32,
    cells_ptr: u32,
    status_ptr: u32,
) {
    let offsets = unsafe { as_i32_slice(offsets_ptr, n + 1) };
    let total_bytes = offsets[n as usize] as u32;
    let buf = unsafe { as_u8_slice(buf_ptr, total_bytes) };
    let cells = unsafe { as_u64_slice_mut(cells_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    for i in 0..n as usize {
        let start = offsets[i] as usize;
        let end = offsets[i + 1] as usize;
        let bytes = &buf[start..end];
        match core::str::from_utf8(bytes) {
            Ok(s) => match s.parse::<CellIndex>() {
                Ok(c) => {
                    cells[i] = u64::from(c);
                    status[i] = STATUS_OK;
                }
                Err(_) => {
                    cells[i] = 0;
                    status[i] = STATUS_INVALID_STRING;
                }
            },
            Err(_) => {
                cells[i] = 0;
                status[i] = STATUS_INVALID_STRING;
            }
        }
    }
}

// --- validate / introspect ------------------------------------------------

#[no_mangle]
pub extern "C" fn h3_are_valid(cells_ptr: u32, n: u32, valid_ptr: u32) {
    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let valid = unsafe { as_u8_slice_mut(valid_ptr, n) };
    for i in 0..n as usize {
        valid[i] = if CellIndex::try_from(cells[i]).is_ok() {
            1
        } else {
            0
        };
    }
}

#[no_mangle]
pub extern "C" fn h3_get_resolution(
    cells_ptr: u32,
    n: u32,
    res_ptr: u32,
    status_ptr: u32,
) {
    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let res = unsafe { as_u8_slice_mut(res_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };
    for i in 0..n as usize {
        match CellIndex::try_from(cells[i]) {
            Ok(c) => {
                res[i] = u8::from(c.resolution());
                status[i] = STATUS_OK;
            }
            Err(_) => {
                res[i] = 0;
                status[i] = STATUS_INVALID_CELL;
            }
        }
    }
}

// --- polyfill -------------------------------------------------------------

fn containment_from_u32(mode: u32) -> Option<ContainmentMode> {
    match mode {
        0 => Some(ContainmentMode::ContainsCentroid),
        1 => Some(ContainmentMode::ContainsBoundary),
        2 => Some(ContainmentMode::IntersectsBoundary),
        3 => Some(ContainmentMode::Covers),
        _ => None,
    }
}

#[no_mangle]
pub extern "C" fn h3_polygon_to_cells(
    lats_ptr: u32,
    lngs_ptr: u32,
    ring_offsets_ptr: u32,
    ring_count: u32,
    res: u32,
    mode: u32,
    out_ptr: u32,
    cap: u32,
    needed_ptr: u32,
) -> u32 {
    let r = match resolution_from_u32(res) {
        Some(r) => r,
        None => return POLYFILL_BAD_MODE,
    };
    let containment = match containment_from_u32(mode) {
        Some(m) => m,
        None => return POLYFILL_BAD_MODE,
    };
    if ring_count == 0 {
        return POLYFILL_BAD_GEOMETRY;
    }

    let ring_offsets = unsafe { as_i32_slice(ring_offsets_ptr, ring_count + 1) };
    let total_verts = ring_offsets[ring_count as usize] as usize;
    let lats = unsafe { as_f64_slice(lats_ptr, total_verts as u32) };
    let lngs = unsafe { as_f64_slice(lngs_ptr, total_verts as u32) };

    // Build geo_types::Polygon from the rings. First ring is exterior, rest are holes.
    // geo uses (x=lng, y=lat) for its Coord convention.
    let mut ring_line_strings: Vec<geo_types::LineString<f64>> = Vec::with_capacity(ring_count as usize);
    for i in 0..ring_count as usize {
        let start = ring_offsets[i] as usize;
        let end = ring_offsets[i + 1] as usize;
        if end < start || end > total_verts {
            return POLYFILL_BAD_GEOMETRY;
        }
        let mut coords: Vec<geo_types::Coord<f64>> = Vec::with_capacity(end - start);
        for j in start..end {
            coords.push(geo_types::Coord {
                x: lngs[j],
                y: lats[j],
            });
        }
        ring_line_strings.push(geo_types::LineString::new(coords));
    }
    let exterior = ring_line_strings.remove(0);
    let holes = ring_line_strings;
    let polygon = geo_types::Polygon::new(exterior, holes);

    let mut tiler = TilerBuilder::new(r).containment_mode(containment).build();
    if tiler.add(polygon).is_err() {
        return POLYFILL_BAD_GEOMETRY;
    }
    let cells: Vec<CellIndex> = tiler.into_coverage().collect();

    let total = cells.len() as u32;
    unsafe {
        *(needed_ptr as *mut u32) = total;
    }
    if total > cap {
        return GROW_NEED_MORE;
    }

    let out = unsafe { as_u64_slice_mut(out_ptr, total) };
    for (i, c) in cells.iter().enumerate() {
        out[i] = u64::from(*c);
    }
    GROW_OK
}

// --- compact / uncompact --------------------------------------------------

#[no_mangle]
pub extern "C" fn h3_compact_cells(
    cells_ptr: u32,
    n: u32,
    out_ptr: u32,
    out_count_ptr: u32,
) -> u32 {
    let raw = unsafe { as_u64_slice(cells_ptr, n) };

    let mut cells: Vec<CellIndex> = Vec::with_capacity(n as usize);
    for &c in raw {
        match CellIndex::try_from(c) {
            Ok(ci) => cells.push(ci),
            Err(_) => return COMPACT_INVALID_CELL,
        }
    }

    match CellIndex::compact(&mut cells) {
        Ok(()) => {
            let out = unsafe { as_u64_slice_mut(out_ptr, n) };
            let count = cells.len().min(n as usize);
            for (i, c) in cells.iter().take(count).enumerate() {
                out[i] = u64::from(*c);
            }
            unsafe {
                *(out_count_ptr as *mut u32) = count as u32;
            }
            COMPACT_OK
        }
        Err(h3o::error::CompactionError::HeterogeneousResolution) => COMPACT_MIXED_RESOLUTION,
        Err(h3o::error::CompactionError::DuplicateInput) => COMPACT_DUPLICATE,
        // Exhaustiveness is future-proofed: h3o may add error variants.
        #[allow(unreachable_patterns)]
        Err(_) => COMPACT_MIXED_RESOLUTION,
    }
}

// --- cell boundaries ------------------------------------------------------

#[no_mangle]
pub extern "C" fn h3_cell_to_boundary(
    cells_ptr: u32,
    n: u32,
    lats_ptr: u32,
    lngs_ptr: u32,
    offsets_ptr: u32,
    cap: u32,
    needed_ptr: u32,
    status_ptr: u32,
) -> u32 {
    let cells = unsafe { as_u64_slice(cells_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    // First pass: validate and compute total vertex count.
    let mut total: u64 = 0;
    for i in 0..n as usize {
        match CellIndex::try_from(cells[i]) {
            Ok(c) => {
                total = total.saturating_add(c.boundary().len() as u64);
                status[i] = STATUS_OK;
            }
            Err(_) => {
                status[i] = STATUS_INVALID_CELL;
            }
        }
    }

    let total_u32 = u32::try_from(total).unwrap_or(u32::MAX);
    unsafe {
        *(needed_ptr as *mut u32) = total_u32;
    }
    if total_u32 > cap {
        return GROW_NEED_MORE;
    }

    let lats = unsafe { as_f64_slice_mut(lats_ptr, total_u32) };
    let lngs = unsafe { as_f64_slice_mut(lngs_ptr, total_u32) };
    let offsets = unsafe { as_i32_slice_mut(offsets_ptr, n + 1) };
    offsets[0] = 0;
    let mut w: usize = 0;
    for i in 0..n as usize {
        if status[i] == STATUS_OK {
            if let Ok(c) = CellIndex::try_from(cells[i]) {
                for v in c.boundary().iter() {
                    lats[w] = v.lat();
                    lngs[w] = v.lng();
                    w += 1;
                }
            }
        }
        offsets[i + 1] = w as i32;
    }
    GROW_OK
}

#[no_mangle]
pub extern "C" fn h3_uncompact_cells(
    cells_ptr: u32,
    n: u32,
    res: u32,
    out_ptr: u32,
    cap: u32,
    needed_ptr: u32,
    status_ptr: u32,
) -> u32 {
    let r = match resolution_from_u32(res) {
        Some(r) => r,
        None => return GROW_BAD_RESOLUTION,
    };
    let raw = unsafe { as_u64_slice(cells_ptr, n) };
    let status = unsafe { as_u8_slice_mut(status_ptr, n) };

    // First pass: validate and compute total output size.
    let mut total: u64 = 0;
    for i in 0..n as usize {
        match CellIndex::try_from(raw[i]) {
            Ok(c) => {
                if r < c.resolution() {
                    status[i] = STATUS_INVALID_RESOLUTION;
                } else {
                    total = total.saturating_add(c.children_count(r));
                    status[i] = STATUS_OK;
                }
            }
            Err(_) => {
                status[i] = STATUS_INVALID_CELL;
            }
        }
    }

    let total_u32 = u32::try_from(total).unwrap_or(u32::MAX);
    unsafe {
        *(needed_ptr as *mut u32) = total_u32;
    }
    if total_u32 > cap {
        return GROW_NEED_MORE;
    }

    let out = unsafe { as_u64_slice_mut(out_ptr, total_u32) };
    let mut w: usize = 0;
    for i in 0..n as usize {
        if status[i] != STATUS_OK {
            continue;
        }
        let c = CellIndex::try_from(raw[i]).expect("validated");
        for child in c.children(r) {
            out[w] = u64::from(child);
            w += 1;
        }
    }
    GROW_OK
}
