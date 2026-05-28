//! SVG export for egui pre-tessellation shapes.
//!
//! Reads the current pass's `ClippedShape` buffer via `Context::graphics()`
//! and walks it into a self-contained SVG document. Hooked into the eframe
//! pass via `Plugin::on_end_pass`, which fires after the user closure but
//! before `Context::end_pass()` tessellates — so the shapes we observe are
//! exactly what egui is about to paint to screen, including all interactive
//! state (window positions, collapsing-header open/closed, scroll offsets).
//!
//! Strategy per Shape variant:
//! - `Text` — one `<text>` per glyph at baseline coords; family hint
//!   (sans-serif/monospace) from `FontFamily`; `PLACEHOLDER` resolves via
//!   `TextShape::fallback_color`. Galleys with mixed sections currently use
//!   `sections[0]` (the `pub(crate)` `Glyph::section_index` prevents fast
//!   per-glyph routing — punt).
//! - `Mesh` (untextured) — one `<polygon>` per triangle, `fill =
//!   vertex[0].color`. Gradient triangles flat-shade; documented limitation.
//! - `Mesh` (textured) — emits an HTML comment and reserves no geometry;
//!   real `egui::Image` widgets need a separate strategy.
//! - `Rect / Circle / Ellipse / LineSegment / Path / *Bezier` — direct
//!   mapping to native SVG primitives.
//! - `Callback` — dashed-outline `<rect>` placeholder; wgpu paint callbacks
//!   are not introspectable.
//! - `Noop / Vec` — skipped / recursed.

use std::collections::HashMap;
use std::fmt::Write as _;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

use egui::epaint::{
    CircleShape, ClippedShape, ColorMode, CubicBezierShape, EllipseShape, Mesh, PathShape,
    PathStroke, QuadraticBezierShape, RectShape, Shape, Stroke, TextShape, Vertex,
};
use egui::{Color32, Context, FontFamily, FontId, LayerId, Order, Pos2, Rect, TextureId, Ui};

// ============================================================================
// Font-family resolution (Tier 1)
// ============================================================================

/// Maps `egui::FontFamily` to an ordered chain of loaded faces, mirroring
/// egui's `FontDefinitions.families` fallback order. For each family we
/// keep (a) a CSS `font-family` value naming every face in the chain plus
/// the generic fallback (`'Noto Sans', 'Symbols Nerd Font', 'Noto Sans
/// Mono CJK', sans-serif`), and (b) the raw TTF bytes per face so the
/// Tier-2 subsetter can route each used glyph to the first face whose
/// cmap covers it. If a family hasn't been registered, `for_id` falls
/// back to the bare generic keyword.
#[derive(Default, Debug, Clone)]
pub struct FontResolver {
    chains: HashMap<FontFamily, FontChain>,
}

#[derive(Debug, Clone)]
struct FontChain {
    /// Pre-formatted CSS value: every face's family name (single-quoted),
    /// comma-separated, then the generic. e.g.
    /// `'Noto Sans', 'Symbols Nerd Font', 'Noto Sans Mono CJK', sans-serif`.
    css_family: String,
    entries: Vec<FontEntry>,
}

#[derive(Debug, Clone)]
struct FontEntry {
    /// The face's family name from the TTF `name` table, e.g. `"Noto Sans"`.
    family_name: String,
    /// Bytes of the TTF/TTC. Retained so the Tier-2 subsetter has source
    /// material. Shared via `Arc` because the same bytes can back several
    /// `FontFamily` chains (Proportional + Monospace both lead with "main"
    /// in this repo's load_custom_fonts).
    ttf_bytes: Arc<Vec<u8>>,
    ttf_index: u32,
    /// Variation-axis coordinates to bake into the subset for VFs (Tier-2
    /// instancing). Empty for static fonts. Default-populated from the
    /// font's `fvar` defaults at `register_chain` time so the output
    /// matches what `ab_glyph` renders inside egui.
    variation_axes: Vec<(subsetter::Tag, f32)>,
}

/// A single subset emitted by `subset_chain_for`: one face's worth of
/// glyphs as a self-contained TTF.
#[derive(Debug)]
pub(crate) struct ChainSubset {
    /// 0-based index of the chain entry that produced this subset. Used
    /// by the visitor to build a per-family synthetic name like
    /// `svg-embed-prop-2`.
    pub chain_index: usize,
    pub bytes: Vec<u8>,
}

impl FontResolver {
    pub fn new() -> Self {
        Self::default()
    }

    /// Register an entire fallback chain under `family`. Parses each TTF's
    /// `name` table for the real family name, sniffs variation-axis
    /// defaults, and builds the combined CSS family value. Faces whose
    /// TTF lacks a usable name record are skipped silently (so they
    /// won't end up in the CSS chain or the subset output either).
    pub fn register_chain(
        &mut self,
        family: FontFamily,
        chain: Vec<(Arc<Vec<u8>>, u32)>,
    ) {
        let generic = generic_for(&family);
        let mut entries: Vec<FontEntry> = Vec::with_capacity(chain.len());
        let mut names: Vec<String> = Vec::with_capacity(chain.len());
        for (ttf_bytes, ttf_index) in chain {
            let Some(name) = parse_ttf_family_name(&ttf_bytes, ttf_index) else {
                continue;
            };
            let variation_axes = default_variation_axes(&ttf_bytes, ttf_index);
            names.push(format!("'{name}'"));
            entries.push(FontEntry {
                family_name: name,
                ttf_bytes,
                ttf_index,
                variation_axes,
            });
        }
        if entries.is_empty() {
            return;
        }
        names.push(generic.to_string());
        let css_family = names.join(", ");
        self.chains.insert(family, FontChain { css_family, entries });
    }

    /// Single-face shorthand for `register_chain` with one entry. Kept
    /// for callers that don't want to construct a chain.
    pub fn register(&mut self, family: FontFamily, ttf_bytes: Arc<Vec<u8>>, index: u32) {
        self.register_chain(family, vec![(ttf_bytes, index)]);
    }

    pub fn for_id(&self, id: &FontId) -> String {
        if let Some(c) = self.chains.get(&id.family) {
            return c.css_family.clone();
        }
        generic_for(&id.family).to_string()
    }

    /// Route each char in `chars` to the first chain entry whose cmap
    /// covers it, then subset each contributing entry to just those glyphs.
    /// Returns one [`ChainSubset`] per face that contributed at least one
    /// glyph, in chain order. Glyphs not covered by any face are silently
    /// dropped (they'll tofu in the SVG, matching the egui-side behaviour).
    ///
    /// `subsetter` is designed for PDF embedding and strips the `cmap`
    /// table from its output (PDFs carry an external CMap). For SVG
    /// `@font-face` the cmap has to live inside the font, so this method
    /// rebuilds a minimal cmap from the original face's char→gid lookup
    /// plus the subsetter's gid remapping and injects it back into the
    /// subset bytes.
    pub(crate) fn subset_chain_for(
        &self,
        family: &FontFamily,
        chars: &std::collections::HashSet<char>,
    ) -> Vec<ChainSubset> {
        let Some(chain) = self.chains.get(family) else {
            return Vec::new();
        };
        // Pre-parse each face once so we don't reparse N times in the
        // per-char dispatch loop.
        let faces: Vec<Option<ttf_parser::Face<'_>>> = chain
            .entries
            .iter()
            .map(|e| ttf_parser::Face::parse(&e.ttf_bytes, e.ttf_index).ok())
            .collect();
        // For each chain entry, accumulate (char, old_gid) for chars that
        // hit this entry. Both pieces are needed: old_gid feeds the
        // subsetter, the (char, new_gid) pairs feed the cmap rebuilder.
        let mut per_entry_chars: Vec<Vec<(char, u16)>> = vec![Vec::new(); chain.entries.len()];
        for &c in chars {
            for (i, face_opt) in faces.iter().enumerate() {
                let Some(face) = face_opt else { continue };
                if let Some(gid) = face.glyph_index(c) {
                    per_entry_chars[i].push((c, gid.0));
                    break;
                }
            }
        }
        let mut out = Vec::new();
        for (i, char_gids) in per_entry_chars.into_iter().enumerate() {
            if char_gids.is_empty() {
                continue;
            }
            let entry = &chain.entries[i];
            let mut glyph_ids: Vec<u16> = char_gids.iter().map(|(_, g)| *g).collect();
            glyph_ids.sort_unstable();
            glyph_ids.dedup();
            // GlyphRemapper::new() already seeds .notdef (gid 0).
            let remapper = subsetter::GlyphRemapper::new_from_glyphs_sorted(&glyph_ids);
            let subset = if entry.variation_axes.is_empty() {
                subsetter::subset(&entry.ttf_bytes, entry.ttf_index, &remapper)
            } else {
                subsetter::subset_with_variations(
                    &entry.ttf_bytes,
                    entry.ttf_index,
                    &entry.variation_axes,
                    &remapper,
                )
            };
            let Ok(subset_bytes) = subset else {
                continue;
            };
            // Resolve each char to its new gid through the remapper. Glyphs
            // the closure pass added (e.g. composite refs) get sequential
            // ids after ours; our (char, old_gid) pairs all land at known
            // new gids assigned during `new_from_glyphs_sorted`.
            let char_to_new_gid: Vec<(char, u16)> = char_gids
                .into_iter()
                .filter_map(|(c, old)| remapper.get(old).map(|new| (c, new)))
                .collect();
            let patched = patch_subset_for_browser(
                subset_bytes,
                &entry.ttf_bytes,
                &char_to_new_gid,
            );
            out.push(ChainSubset {
                chain_index: i,
                bytes: patched,
            });
        }
        out
    }
}

fn generic_for(family: &FontFamily) -> &'static str {
    match family {
        FontFamily::Monospace => "monospace",
        // Proportional + any Name(_) routes to sans-serif as the safest
        // generic. Name(_) is rare in our usage; if it shows up and we have
        // a registered entry for it, that wins.
        _ => "sans-serif",
    }
}

/// For variable fonts, return each axis's tag + default value as a
/// (subsetter::Tag, f32) pair so `subset_with_variations` can bake a static
/// instance at the same coordinates `ab_glyph` uses inside egui. For
/// non-VFs the `variation_axes` iterator is empty and we get an empty Vec.
fn default_variation_axes(bytes: &[u8], index: u32) -> Vec<(subsetter::Tag, f32)> {
    let Ok(face) = ttf_parser::Face::parse(bytes, index) else {
        return Vec::new();
    };
    face.variation_axes()
        .into_iter()
        .map(|axis| {
            // ttf_parser::Tag wraps a u32 big-endian-packed FourCC; subsetter::Tag
            // takes a 4-byte array. Round-trip via bytes.
            let tag_u32 = axis.tag.0;
            let bytes = [
                ((tag_u32 >> 24) & 0xff) as u8,
                ((tag_u32 >> 16) & 0xff) as u8,
                ((tag_u32 >> 8) & 0xff) as u8,
                (tag_u32 & 0xff) as u8,
            ];
            (subsetter::Tag::new(&bytes), axis.def_value)
        })
        .collect()
}

/// Restore the tables that `subsetter` strips for its PDF use case but
/// browsers need to accept an `@font-face`:
///  * `cmap` — rebuilt from `mappings`, since subsetter outputs no cmap;
///  * `OS/2` — passed through verbatim from `original_bytes` (the table
///    is font-wide metadata so subsetting doesn't invalidate it).
///
/// The output stays compatible with anything that read the subset before
/// (other tables retain their bytes; only the directory grows). Sfnt-
/// level checksums are left at 0 since browsers and `usvg` ignore them.
fn patch_subset_for_browser(
    subset_bytes: Vec<u8>,
    original_bytes: &[u8],
    mappings: &[(char, u16)],
) -> Vec<u8> {
    let mut out = subset_bytes;
    if !has_table(&out, b"cmap") {
        let cmap_blob = build_cmap_blob(mappings);
        out = splice_in_table(&out, *b"cmap", &cmap_blob);
    }
    if !has_table(&out, b"OS/2") {
        if let Some(os2) = read_table(original_bytes, b"OS/2") {
            out = splice_in_table(&out, *b"OS/2", &os2);
        }
    }
    out
}

fn read_table(bytes: &[u8], tag: &[u8; 4]) -> Option<Vec<u8>> {
    if bytes.len() < 12 {
        return None;
    }
    let num_tables = u16::from_be_bytes([bytes[4], bytes[5]]) as usize;
    for i in 0..num_tables {
        let off = 12 + i * 16;
        if off + 16 > bytes.len() {
            return None;
        }
        if &bytes[off..off + 4] == tag {
            let o = u32::from_be_bytes([bytes[off + 8], bytes[off + 9], bytes[off + 10], bytes[off + 11]]) as usize;
            let l = u32::from_be_bytes([bytes[off + 12], bytes[off + 13], bytes[off + 14], bytes[off + 15]]) as usize;
            if o + l <= bytes.len() {
                return Some(bytes[o..o + l].to_vec());
            }
        }
    }
    None
}

fn has_table(bytes: &[u8], tag: &[u8; 4]) -> bool {
    if bytes.len() < 12 {
        return false;
    }
    let num_tables = u16::from_be_bytes([bytes[4], bytes[5]]) as usize;
    for i in 0..num_tables {
        let off = 12 + i * 16;
        if off + 4 > bytes.len() {
            return false;
        }
        if &bytes[off..off + 4] == tag {
            return true;
        }
    }
    false
}

fn build_cmap_blob(mappings: &[(char, u16)]) -> Vec<u8> {
    let mut bmp: Vec<(u16, u16)> = mappings
        .iter()
        .filter_map(|(c, g)| {
            let u = *c as u32;
            if u <= 0xFFFF { Some((u as u16, *g)) } else { None }
        })
        .collect();
    let supp: Vec<(u32, u16)> = mappings
        .iter()
        .filter_map(|(c, g)| {
            let u = *c as u32;
            if u > 0xFFFF { Some((u, *g)) } else { None }
        })
        .collect();
    bmp.sort_unstable_by_key(|(c, _)| *c);
    let format4 = build_format4(&bmp);
    let format12 = if supp.is_empty() { None } else { Some(build_format12(&supp)) };

    let num_sub: u32 = 1 + format12.is_some() as u32;
    let header_size: u32 = 4 + num_sub * 8;

    let mut out = Vec::new();
    // Cmap header: version=0, numTables.
    out.extend_from_slice(&0u16.to_be_bytes());
    out.extend_from_slice(&(num_sub as u16).to_be_bytes());
    // Encoding records (each 8 bytes): pid, eid, offset.
    let mut sub_off = header_size;
    // Windows Unicode BMP.
    out.extend_from_slice(&3u16.to_be_bytes());
    out.extend_from_slice(&1u16.to_be_bytes());
    out.extend_from_slice(&sub_off.to_be_bytes());
    sub_off += format4.len() as u32;
    if format12.is_some() {
        // Windows Unicode Full.
        out.extend_from_slice(&3u16.to_be_bytes());
        out.extend_from_slice(&10u16.to_be_bytes());
        out.extend_from_slice(&sub_off.to_be_bytes());
    }
    out.extend_from_slice(&format4);
    if let Some(f12) = format12 {
        out.extend_from_slice(&f12);
    }
    out
}

fn build_format4(bmp: &[(u16, u16)]) -> Vec<u8> {
    // One segment per char + one mandatory sentinel at 0xFFFF.
    let mut segs: Vec<(u16, u16, u16)> = bmp
        .iter()
        .map(|&(c, g)| (c, c, g))
        .collect();
    segs.push((0xFFFF, 0xFFFF, 0));

    let seg_count = segs.len() as u16;
    let seg_count_x2 = seg_count * 2;
    let log2 = (seg_count as f32).log2().floor() as u16;
    let search_range = 2 * (1u16 << log2);
    let entry_selector = log2;
    let range_shift = seg_count_x2 - search_range;

    let mut out = Vec::new();
    out.extend_from_slice(&4u16.to_be_bytes()); // format
    let len_pos = out.len();
    out.extend_from_slice(&0u16.to_be_bytes()); // length placeholder
    out.extend_from_slice(&0u16.to_be_bytes()); // language
    out.extend_from_slice(&seg_count_x2.to_be_bytes());
    out.extend_from_slice(&search_range.to_be_bytes());
    out.extend_from_slice(&entry_selector.to_be_bytes());
    out.extend_from_slice(&range_shift.to_be_bytes());
    for (_, e, _) in &segs {
        out.extend_from_slice(&e.to_be_bytes());
    }
    out.extend_from_slice(&0u16.to_be_bytes()); // reservedPad
    for (s, _, _) in &segs {
        out.extend_from_slice(&s.to_be_bytes());
    }
    // idDelta = (gid - char), wrapping as i16.
    for (s, _, g) in &segs {
        let delta = (*g as i32 - *s as i32) as i16;
        out.extend_from_slice(&delta.to_be_bytes());
    }
    for _ in &segs {
        out.extend_from_slice(&0u16.to_be_bytes()); // idRangeOffset=0
    }
    let len = out.len() as u16;
    out[len_pos..len_pos + 2].copy_from_slice(&len.to_be_bytes());
    out
}

fn build_format12(supp: &[(u32, u16)]) -> Vec<u8> {
    let num_groups = supp.len() as u32;
    let mut out = Vec::new();
    out.extend_from_slice(&12u16.to_be_bytes()); // format
    out.extend_from_slice(&0u16.to_be_bytes()); // reserved
    let len_pos = out.len();
    out.extend_from_slice(&0u32.to_be_bytes()); // length placeholder
    out.extend_from_slice(&0u32.to_be_bytes()); // language
    out.extend_from_slice(&num_groups.to_be_bytes());
    for &(c, g) in supp {
        out.extend_from_slice(&c.to_be_bytes()); // start
        out.extend_from_slice(&c.to_be_bytes()); // end
        out.extend_from_slice(&(g as u32).to_be_bytes()); // startGlyphID
    }
    let len = out.len() as u32;
    out[len_pos..len_pos + 4].copy_from_slice(&len.to_be_bytes());
    out
}

/// Rebuild the TTF byte stream with one extra table inserted. Tables are
/// sorted by tag in the directory but laid out in directory order
/// physically; we keep the order stable for the existing entries and
/// place the new table at the end of the file (4-byte aligned).
fn splice_in_table(bytes: &[u8], new_tag: [u8; 4], new_data: &[u8]) -> Vec<u8> {
    if bytes.len() < 12 {
        return bytes.to_vec();
    }
    let num_tables = u16::from_be_bytes([bytes[4], bytes[5]]) as usize;
    // (tag, original_offset, original_length) — checksum is recomputed
    // to 0 (browsers don't validate).
    let mut entries: Vec<([u8; 4], u32, u32)> = Vec::with_capacity(num_tables + 1);
    for i in 0..num_tables {
        let off = 12 + i * 16;
        let mut tag = [0u8; 4];
        tag.copy_from_slice(&bytes[off..off + 4]);
        let o = u32::from_be_bytes([bytes[off + 8], bytes[off + 9], bytes[off + 10], bytes[off + 11]]);
        let l = u32::from_be_bytes([bytes[off + 12], bytes[off + 13], bytes[off + 14], bytes[off + 15]]);
        entries.push((tag, o, l));
    }
    // Collect each table's raw blob from its old position.
    let mut blobs: Vec<([u8; 4], Vec<u8>)> = entries
        .iter()
        .map(|(t, o, l)| (*t, bytes[*o as usize..(*o + *l) as usize].to_vec()))
        .collect();
    blobs.push((new_tag, new_data.to_vec()));
    // Directory entries must be sorted by tag.
    blobs.sort_by(|a, b| a.0.cmp(&b.0));

    let new_num = blobs.len() as u16;
    let new_dir_size = 12 + new_num as usize * 16;

    // Layout: header → directory → tables (each 4-byte aligned).
    let mut dir_offsets: Vec<u32> = Vec::with_capacity(blobs.len());
    let mut cursor = ((new_dir_size + 3) & !3) as u32;
    for (_, blob) in &blobs {
        dir_offsets.push(cursor);
        cursor += blob.len() as u32;
        cursor = (cursor + 3) & !3;
    }
    let total_size = cursor as usize;

    let mut out = vec![0u8; total_size];
    // sfnt version (4 bytes) — keep whatever the input had.
    out[0..4].copy_from_slice(&bytes[0..4]);
    out[4..6].copy_from_slice(&new_num.to_be_bytes());
    // searchRange / entrySelector / rangeShift, mostly cosmetic for browsers
    // but properly computed in case some parser cares.
    let log2 = (new_num as f32).log2().floor() as u16;
    let search_range = (1u16 << log2) * 16;
    let entry_selector = log2;
    let range_shift = new_num * 16 - search_range;
    out[6..8].copy_from_slice(&search_range.to_be_bytes());
    out[8..10].copy_from_slice(&entry_selector.to_be_bytes());
    out[10..12].copy_from_slice(&range_shift.to_be_bytes());

    for (i, ((tag, blob), &off)) in blobs.iter().zip(dir_offsets.iter()).enumerate() {
        let dir_pos = 12 + i * 16;
        out[dir_pos..dir_pos + 4].copy_from_slice(tag);
        // checksum left at 0
        out[dir_pos + 8..dir_pos + 12].copy_from_slice(&off.to_be_bytes());
        out[dir_pos + 12..dir_pos + 16].copy_from_slice(&(blob.len() as u32).to_be_bytes());
        let off_usz = off as usize;
        out[off_usz..off_usz + blob.len()].copy_from_slice(blob);
    }
    out
}

fn parse_ttf_family_name(bytes: &[u8], index: u32) -> Option<String> {
    let face = ttf_parser::Face::parse(bytes, index).ok()?;
    // Prefer typographic family name (id=16) over legacy family (id=1):
    // for variable fonts with multiple sub-families, id=16 names the
    // typographic family (e.g. "Noto Sans") while id=1 may name a
    // weight-specific subfamily (e.g. "Noto Sans Regular"). Fall back to
    // id=1 if id=16 isn't present.
    let mut typographic: Option<String> = None;
    let mut family: Option<String> = None;
    for name in face.names() {
        if !name.is_unicode() {
            continue;
        }
        match name.name_id {
            16 => {
                if typographic.is_none() {
                    typographic = name.to_string();
                }
            }
            1 => {
                if family.is_none() {
                    family = name.to_string();
                }
            }
            _ => {}
        }
    }
    typographic.or(family)
}

// ============================================================================
// Texture pixel cache (Mesh texture embedding)
// ============================================================================

/// CPU-side mirror of texture pixel data, populated by the image /
/// scrolling-texture caches each time they upload to egui. Keyed by the
/// egui `TextureId` that lands on `Mesh.texture_id`, so the SVG visitor
/// can recover the pixels for a textured mesh and embed them as an
/// `<image>` data URL. Wrapped in `Arc<Mutex<_>>` so the writers (which
/// live inside `ImZeroFffi`) and the reader (the `SvgExportPlugin`) can
/// share a handle.
///
/// **Coverage gap — walkers tiles**: the `walkers` crate loads tiles
/// internally via its own `ctx.load_texture` call inside `Tile::new`,
/// with no external hook. Wiring it would require either forking
/// walkers to expose a tile-pixel callback, or implementing the
/// `walkers::Tiles` trait from scratch (intercepting the bytes →
/// `ColorImage` decode before upload). Until then, textured meshes
/// produced by `walkers::HttpTiles` fall through to the visitor's
/// comment-skip path.
#[derive(Default, Debug)]
pub struct TexturePixelCache {
    textures: HashMap<egui::TextureId, CachedTexture>,
}

#[derive(Debug, Clone)]
pub struct CachedTexture {
    pub width: u32,
    pub height: u32,
    /// Tightly packed RGBA bytes, row-major, top-left origin (matches
    /// egui's `ColorImage` layout when iterated linearly).
    pub rgba: Vec<u8>,
    /// `true` when egui asked for nearest-neighbour magnification
    /// (`TextureOptions::NEAREST` family) — the visitor emits
    /// `image-rendering="pixelated"` so browsers don't bilinear-blur
    /// the embedded `<image>` when it's scaled up.
    pub nearest: bool,
}

impl TexturePixelCache {
    pub fn insert(
        &mut self,
        id: egui::TextureId,
        width: u32,
        height: u32,
        rgba: Vec<u8>,
        nearest: bool,
    ) {
        self.textures.insert(
            id,
            CachedTexture { width, height, rgba, nearest },
        );
    }
    pub fn get(&self, id: egui::TextureId) -> Option<&CachedTexture> {
        self.textures.get(&id)
    }
    pub fn remove(&mut self, id: egui::TextureId) {
        self.textures.remove(&id);
    }
}

pub type TexturePixelCacheHandle = Arc<Mutex<TexturePixelCache>>;

// ============================================================================
// Hyperlink zones (Shape::Text → <a href="…">)
// ============================================================================

/// One zone (response rect + URL) per Hyperlink / HyperlinkTo call. Pushed
/// from the widget's IDL apply code, drained per frame by the visitor.
/// The Go side keeps the URL string as a separate field on the IDL call
/// so a future opcode (e.g. a "rewrite URLs for SVG export") can swap it
/// in cheaply without going through the widget rebuild.
///
/// Inline markdown links are also covered by this path: the markdown
/// widget (`thestack/imzero2/egui2/widgets/markdown/render.go`) emits
/// each inline `[text](url)` run as a separate `c.HyperlinkTo(...)` call
/// in flow order, which goes through the same IDL apply and produces a
/// LinkZone per inline link. egui's `TextFormat` carries no URL field,
/// so there is no other "rich text with inline URL" path to plug in.
#[derive(Debug, Clone)]
pub struct LinkZone {
    pub rect: Rect,
    pub url: String,
}

pub type LinkZonesHandle = Arc<Mutex<Vec<LinkZone>>>;

/// Encode an RGBA byte buffer as PNG. None on encoder failure (shouldn't
/// happen for well-formed input; defensive in case of zero-size).
fn rgba_to_png(width: u32, height: u32, rgba: &[u8]) -> Option<Vec<u8>> {
    if width == 0 || height == 0 || rgba.len() != (width as usize) * (height as usize) * 4 {
        return None;
    }
    let mut buf: Vec<u8> = Vec::with_capacity(rgba.len() / 4 + 256);
    {
        let mut encoder = png::Encoder::new(&mut buf, width, height);
        encoder.set_color(png::ColorType::Rgba);
        encoder.set_depth(png::BitDepth::Eight);
        let mut writer = encoder.write_header().ok()?;
        writer.write_image_data(rgba).ok()?;
    }
    Some(buf)
}

// ============================================================================
// Plugin + shared state
// ============================================================================

/// Cross-pass handoff between the FFFI interpreter (writer) and the
/// `on_end_pass` plugin (reader). Wrapped in an `Arc<Mutex<...>>` so both
/// can hold it.
#[derive(Default)]
pub struct ExportState {
    pub pending: Option<ExportRequest>,
    pub last_result: Option<Result<usize, String>>,
}

/// How much of a window to include in the SVG export.
///
/// `Faithful` (M2 / screenshot use): includes the window's full frame
/// — title-bar, body, resize chrome — and uses the area_rect as the
/// viewBox. The output reads as a screenshot of the window as the
/// user sees it.
///
/// `ContentOnly` (M3 / webapp-report use): shrinks the viewBox to the
/// inner content rect (area_rect minus the approximated title-bar +
/// frame margins) and emits `overflow="hidden"` on the root `<svg>`
/// so SVG viewers clip the chrome shapes still present in the layer.
/// Suited for embedding a report's body in HTML without dragging the
/// window chrome along.
#[derive(Debug, Clone, Copy, Default)]
pub enum WindowMode {
    #[default]
    Faithful,
    ContentOnly,
}

/// What part of the egui scene the visitor renders.
///
/// `Viewport` (the default) is the original behaviour: walks every
/// Area-registered layer plus the implicit background and uses
/// `ctx.screen_rect()` as the SVG viewBox.
///
/// `Window { id, mode }` restricts the walk to the single layer at
/// `LayerId::new(Order::Middle, id)` and uses that area's stored rect
/// (via `Memory::area_rect`) as the SVG viewBox. The `id` must match
/// what was passed to `egui::Window::new(...).id(...)` (or, in the
/// FFFI-IDL world, the widget id used to construct the window).
/// `mode` picks between the faithful screenshot and the
/// chrome-stripped report variants — see [`WindowMode`]. Overlays
/// attached to that window — tooltips, combo dropdowns, context
/// menus — live on higher-order layers and are intentionally
/// excluded; they belong to the user-visible composite, not the
/// window-as-document.
#[derive(Debug, Clone, Default)]
pub enum ExportScope {
    #[default]
    Viewport,
    Window { id: egui::Id, mode: WindowMode },
}

/// A queued export. `embed_fonts=false` produces a lightweight SVG that
/// references the loaded font family by name (Tier 1); viewer must have a
/// matching font installed. `embed_fonts=true` subsets each used face to
/// just the glyphs the frame actually paints, base64-embeds them as
/// `@font-face` (Tier 2) — slightly larger, self-contained, pixel-faithful.
///
/// `bg` controls the baseline `<rect>` painted under everything else:
/// `Some(color)` fills it with that color (Faithful-mode default is
/// the dark `VIEWPORT_BG`); `None` omits the rect entirely so the host
/// page or SVG viewer's background shows through (the M3 webapp-report
/// default).
#[derive(Debug, Clone)]
pub struct ExportRequest {
    pub path: PathBuf,
    pub embed_fonts: bool,
    pub scope: ExportScope,
    pub bg: Option<Color32>,
}

pub type ExportStateHandle = Arc<Mutex<ExportState>>;

/// Drains a pending export on every pass. Cheap if nothing pending.
pub struct SvgExportPlugin {
    state: ExportStateHandle,
    fonts: Arc<FontResolver>,
    textures: TexturePixelCacheHandle,
    links: LinkZonesHandle,
}

impl SvgExportPlugin {
    pub fn new(
        state: ExportStateHandle,
        fonts: Arc<FontResolver>,
        textures: TexturePixelCacheHandle,
        links: LinkZonesHandle,
    ) -> Self {
        Self { state, fonts, textures, links }
    }
}

impl egui::plugin::Plugin for SvgExportPlugin {
    fn debug_name(&self) -> &'static str {
        "svg_export"
    }

    fn on_end_pass(&mut self, ui: &mut Ui) {
        let req = {
            let mut s = self.state.lock().expect("svg_export state poisoned");
            s.pending.take()
        };
        let Some(req) = req else {
            return;
        };
        let result = render_svg_to_file(ui.ctx(), &self.fonts, &self.textures, &self.links, &req);
        if let Err(ref e) = result {
            tracing::error!(path = %req.path.display(), embed = req.embed_fonts, error = %e, "svg export failed");
        } else if let Ok(bytes) = result {
            tracing::info!(path = %req.path.display(), embed = req.embed_fonts, bytes, "svg export ok");
        }
        self.state
            .lock()
            .expect("svg_export state poisoned")
            .last_result = Some(result);
    }
}

/// Convenience for callers (FFFI interpreter, tests): queue an export
/// with the given scope.
pub fn request_export(
    state: &ExportStateHandle,
    path: impl Into<PathBuf>,
    embed_fonts: bool,
    scope: ExportScope,
    bg: Option<Color32>,
) {
    state.lock().expect("svg_export state poisoned").pending = Some(ExportRequest {
        path: path.into(),
        embed_fonts,
        scope,
        bg,
    });
}

// ============================================================================
// Top-level rendering
// ============================================================================

pub fn render_svg_to_file(
    ctx: &Context,
    fonts: &FontResolver,
    textures: &TexturePixelCacheHandle,
    links: &LinkZonesHandle,
    req: &ExportRequest,
) -> Result<usize, String> {
    let svg = match &req.scope {
        ExportScope::Viewport => render_svg_from_context(
            ctx,
            fonts,
            textures,
            links,
            req.embed_fonts,
            req.bg,
        ),
        ExportScope::Window { id, mode } => render_svg_window(
            ctx,
            fonts,
            textures,
            links,
            req.embed_fonts,
            *id,
            *mode,
            req.bg,
        )
        .ok_or_else(|| {
            format!(
                "window id {id:?} has no area_rect this pass — collapsed, off-screen, or never opened"
            )
        })?,
    };
    std::fs::write(&req.path, &svg).map_err(|e| format!("write {}: {e}", req.path.display()))?;
    Ok(svg.len())
}

/// In-pass entry point — reads `Context::graphics()` directly. Only valid
/// *inside* `on_end_pass` (or any plugin hook that fires before
/// `Context::end_pass` drains the buffer). After the pass ends, the graphics
/// buffer is empty; use [`render_svg_from_shapes`] against
/// `FullOutput.shapes` instead.
///
/// When `embed_fonts=true`, runs a pre-pass over the shape tree collecting
/// `(FontFamily, char)` usage, subsets each registered TTF down to just
/// those glyphs, and embeds the result as `@font-face` blocks under
/// `<defs><style>`. Output is then viewer-independent.
pub fn render_svg_from_context(
    ctx: &Context,
    fonts: &FontResolver,
    textures: &TexturePixelCacheHandle,
    links: &LinkZonesHandle,
    embed_fonts: bool,
    bg: Option<Color32>,
) -> String {
    let viewport = ctx.screen_rect();
    let weights = ThemeWeights::from_context(ctx);
    // Snapshot link zones for this pass so we don't hold the mutex
    // through the entire shape walk.
    let zones: Vec<LinkZone> = links.lock().map(|g| g.clone()).unwrap_or_default();
    let mut b = SvgBuilder::new(
        viewport,
        fonts.clone(),
        weights,
        Some(textures.clone()),
        zones,
        bg,
    );

    // `Memory::layer_ids` only enumerates Area-registered layers (Windows,
    // SidePanels, named Areas). The implicit background layer used by
    // `run_ui` / `CentralPanel` doesn't go through Areas, so we prepend it
    // explicitly. egui's `GraphicLayers::drain` does the equivalent — walks
    // area_order *and* picks up leftovers — but the leftover iterator isn't
    // exposed publicly.
    let mut layer_ids: Vec<egui::LayerId> = ctx.memory(|m| m.layer_ids().collect());
    let bg = egui::LayerId::background();
    if !layer_ids.contains(&bg) {
        layer_ids.insert(0, bg);
    }
    // egui_snarl, egui_plot zoom, and any caller of `Context::set_transform_layer`
    // attach a TSTransform to a layer; egui's `GraphicLayers::drain` applies it
    // in-place before tessellation. We can't mutate (read-only borrow), so we
    // clone shapes that have a non-identity transform and transform the clone.
    //
    // Resolve transforms BEFORE entering `ctx.graphics()` — calling another
    // `ctx.*` accessor inside the graphics closure re-borrows Context state
    // and deadlocks against the next pass's write lock (cost us 22 minutes
    // of stuck-tour time the first try).
    let xforms: Vec<Option<egui::emath::TSTransform>> = layer_ids
        .iter()
        .map(|id| ctx.layer_transform_to_global(*id))
        .collect();

    // Tier-2 pre-pass: collect every (FontFamily, char) pair the frame
    // actually paints, route each char to the first face in the family's
    // fallback chain that has it, subset each contributing face, and
    // register one `@font-face` per contribution. The visitor's
    // `emit_text` then emits a CSS family chain of all synthetic names
    // for that family so the browser falls through subsets the same way
    // egui's font loader fell through the chain at layout time. Done
    // before the emit walk so the embed map is fully populated when
    // emit_text runs.
    if embed_fonts {
        let used = collect_used_glyphs(ctx, &layer_ids);
        // Sort family iteration so the svg-embed-N synth IDs land in a
        // deterministic order. HashMap iterates in randomised order
        // per-process, which scrambled `svg-embed-0..N` across runs and
        // produced spurious SVG byte diffs that didn't reflect any
        // visual change. Sorting by Debug-fmt is enough — the order
        // only needs to be stable run-to-run, not semantically
        // meaningful.
        let mut families: Vec<&FontFamily> = used.keys().collect();
        families.sort_by_key(|f| format!("{:?}", f));
        for family in families {
            let chars = &used[family];
            let subsets = fonts.subset_chain_for(family, chars);
            if subsets.is_empty() {
                continue;
            }
            let mut synth_names: Vec<String> = Vec::with_capacity(subsets.len());
            for sub in &subsets {
                let synth = format!("svg-embed-{}", b.next_embed_id());
                b.add_embedded_font(&synth, &sub.bytes);
                synth_names.push(format!("'{synth}'"));
            }
            // Append the generic so out-of-coverage chars (e.g. control
            // chars that egui drew as space) don't break rendering.
            synth_names.push(generic_for(family).to_string());
            b.set_embed_for_family(family.clone(), synth_names.join(", "));
        }
    }

    ctx.graphics(|gl| {
        for (layer_id, xform) in layer_ids.iter().zip(xforms.iter()) {
            let Some(paint_list) = gl.get(*layer_id) else {
                continue;
            };
            for clipped in paint_list.all_entries() {
                match xform {
                    None => b.visit_clipped(clipped),
                    Some(t) => {
                        let mut tmp = clipped.clone();
                        tmp.clip_rect = t.mul_rect(tmp.clip_rect);
                        tmp.shape.transform(*t);
                        b.visit_clipped(&tmp);
                    }
                }
            }
        }
    });

    b.finish()
}

/// In-pass entry point for the single-window scope. Walks only the
/// `LayerId::new(Order::Middle, window_id)` paint list and uses the
/// window's stored `area_rect` as the SVG viewBox.
///
/// Returns `None` when the window has no recorded area_rect for this
/// pass (collapsed, off-screen, or never opened). Caller maps that to
/// an `Err` so the FFFI plugin logs the failure instead of writing an
/// empty file.
///
/// Behaves like [`render_svg_from_context`] in every other way:
/// per-layer TS transforms, Tier-2 glyph subsetting, hyperlink-zone
/// wrapping, and the texture-pixel cache are all honoured. Overlays
/// spawned by the window (tooltips, combo dropdowns, context menus)
/// live on higher-order layers and are intentionally excluded — the
/// scope is the window-as-document, not the user-visible composite.
pub fn render_svg_window(
    ctx: &Context,
    fonts: &FontResolver,
    textures: &TexturePixelCacheHandle,
    links: &LinkZonesHandle,
    embed_fonts: bool,
    window_id: egui::Id,
    mode: WindowMode,
    bg: Option<Color32>,
) -> Option<String> {
    let target_layer = LayerId::new(Order::Middle, window_id);
    let viewport = ctx.memory(|m| m.area_rect(window_id))?;
    if !(viewport.width() > 0.0 && viewport.height() > 0.0) {
        return None;
    }
    let weights = ThemeWeights::from_context(ctx);
    let zones: Vec<LinkZone> = links.lock().map(|g| g.clone()).unwrap_or_default();
    let mut b = SvgBuilder::new(
        viewport,
        fonts.clone(),
        weights,
        Some(textures.clone()),
        zones,
        bg,
    );

    // Webapp-embedding affordances: a class hook so host CSS can
    // target this scope, plus a viewBox crop (ContentOnly) that
    // drops the window's title-bar + frame margins from the
    // visible area. `overflow="hidden"` is sufficient — the chrome
    // shapes remain in the layer but lie outside the viewBox, and
    // an `<svg>` with overflow=hidden clips them in every viewer
    // we care about (browsers + resvg).
    b.set_css_class(format!(
        "imzero-svg imzero-window-{:x}",
        window_id.value()
    ));
    if matches!(mode, WindowMode::ContentOnly) {
        let style = ctx.style();
        let spacing = &style.spacing;
        let stroke_w = style.visuals.window_stroke.width;
        // Approximation of egui's `title_bar_height_with_margin`:
        // the title font's row height is internal to the Window
        // builder, but `interact_size.y` is a sane floor and the
        // window-margin's vertical sum lines up with the chrome
        // height we want to drop. Off by a few pixels for very
        // long titles is acceptable for a v1 report mode.
        let title_h = spacing.interact_size.y + spacing.window_margin.sum().y as f32;
        let content_rect = Rect::from_min_max(
            egui::pos2(
                viewport.min.x + stroke_w + spacing.window_margin.leftf(),
                viewport.min.y + title_h,
            ),
            egui::pos2(
                viewport.max.x - stroke_w - spacing.window_margin.rightf(),
                viewport.max.y - stroke_w - spacing.window_margin.bottomf(),
            ),
        );
        b.set_viewbox(content_rect);
        b.set_overflow_hidden(true);
    }

    // Resolve the layer transform up front (same deadlock concern as
    // `render_svg_from_context`: any `ctx.*` call from inside
    // `ctx.graphics` deadlocks against the next pass's write lock).
    let xform = ctx.layer_transform_to_global(target_layer);
    let layer_slice = [target_layer];

    if embed_fonts {
        let used = collect_used_glyphs(ctx, &layer_slice);
        for (family, chars) in &used {
            let subsets = fonts.subset_chain_for(family, chars);
            if subsets.is_empty() {
                continue;
            }
            let mut synth_names: Vec<String> = Vec::with_capacity(subsets.len());
            for sub in &subsets {
                let synth = format!("svg-embed-{}", b.next_embed_id());
                b.add_embedded_font(&synth, &sub.bytes);
                synth_names.push(format!("'{synth}'"));
            }
            synth_names.push(generic_for(family).to_string());
            b.set_embed_for_family(family.clone(), synth_names.join(", "));
        }
    }

    ctx.graphics(|gl| {
        let Some(paint_list) = gl.get(target_layer) else {
            return;
        };
        for clipped in paint_list.all_entries() {
            match xform {
                None => b.visit_clipped(clipped),
                Some(t) => {
                    let mut tmp = clipped.clone();
                    tmp.clip_rect = t.mul_rect(tmp.clip_rect);
                    tmp.shape.transform(t);
                    b.visit_clipped(&tmp);
                }
            }
        }
    });

    Some(b.finish())
}

/// Post-pass entry point — visits a slice of `ClippedShape` directly.
/// Convenient for tests/examples that use `Context::run_ui`'s `FullOutput`
/// after the pass has ended. Pass `&FontResolver::default()` if you don't
/// have one — text will render with generic family hints.
///
/// When `embed_fonts=true`, walks `shapes` twice: once to collect glyph
/// usage, once to emit.
pub fn render_svg_from_shapes<'a, I>(
    viewport: Rect,
    fonts: &FontResolver,
    embed_fonts: bool,
    shapes: I,
    bg: Option<Color32>,
) -> String
where
    I: IntoIterator<Item = &'a ClippedShape> + Clone,
{
    // Caller is responsible for theme-weight matching when it has a
    // `Context` handy; the post-pass path uses defaults (no synth-bold)
    // and has no texture cache (textured meshes still emit skip comments)
    // and no link zones (the FullOutput visitor has no FFFI register
    // history to draw from).
    let mut b = SvgBuilder::new(
        viewport,
        fonts.clone(),
        ThemeWeights::default(),
        None,
        Vec::new(),
        bg,
    );

    if embed_fonts {
        let mut used: HashMap<FontFamily, std::collections::HashSet<char>> = HashMap::new();
        for clipped in shapes.clone() {
            visit_for_chars(&clipped.shape, &mut used);
        }
        for (family, chars) in &used {
            let subsets = fonts.subset_chain_for(family, chars);
            if subsets.is_empty() {
                continue;
            }
            let mut synth_names: Vec<String> = Vec::with_capacity(subsets.len());
            for sub in &subsets {
                let synth = format!("svg-embed-{}", b.next_embed_id());
                b.add_embedded_font(&synth, &sub.bytes);
                synth_names.push(format!("'{synth}'"));
            }
            synth_names.push(generic_for(family).to_string());
            b.set_embed_for_family(family.clone(), synth_names.join(", "));
        }
    }

    for clipped in shapes {
        b.visit_clipped(clipped);
    }
    b.finish()
}

/// Walk every `Shape::Text` reachable from `layer_ids` and accumulate the
/// set of characters used per `FontFamily`. Mirrors what `emit_text` will
/// later visit, but only inspects text. Like the main render pass, this
/// also has to apply layer transforms — actually no, it doesn't: char sets
/// are coordinate-independent. We just need to recurse `Shape::Vec`.
fn collect_used_glyphs(
    ctx: &Context,
    layer_ids: &[egui::LayerId],
) -> HashMap<FontFamily, std::collections::HashSet<char>> {
    let mut used: HashMap<FontFamily, std::collections::HashSet<char>> = HashMap::new();
    ctx.graphics(|gl| {
        for lid in layer_ids {
            let Some(pl) = gl.get(*lid) else {
                continue;
            };
            for clipped in pl.all_entries() {
                visit_for_chars(&clipped.shape, &mut used);
            }
        }
    });
    used
}

fn visit_for_chars(
    shape: &Shape,
    used: &mut HashMap<FontFamily, std::collections::HashSet<char>>,
) {
    match shape {
        Shape::Vec(children) => {
            for c in children {
                visit_for_chars(c, used);
            }
        }
        Shape::Text(t) => {
            // Mirror `emit_text`'s per-section routing: walk glyphs in
            // lockstep with the source text's byte cursor and bucket each
            // char into its section's `FontFamily`. This matters when a
            // single galley mixes families (markdown body + monospace
            // code spans, etc.) — the per-family subset must include the
            // chars each face actually paints.
            let job = &t.galley.job;
            if job.sections.is_empty() {
                return;
            }
            let mut byte_cursor: usize = 0;
            let mut section_idx: usize = 0;
            for row in &t.galley.rows {
                for g in &row.row.glyphs {
                    while section_idx + 1 < job.sections.len()
                        && byte_cursor >= job.sections[section_idx].byte_range.end
                    {
                        section_idx += 1;
                    }
                    let family = job.sections[section_idx].format.font_id.family.clone();
                    byte_cursor += g.chr.len_utf8();
                    if g.uv_rect.is_nothing() || g.chr.is_whitespace() {
                        continue;
                    }
                    used.entry(family).or_default().insert(g.chr);
                }
                if row.ends_with_newline {
                    byte_cursor += 1;
                }
            }
        }
        _ => {}
    }
}

// ============================================================================
// Builder
// ============================================================================

const VIEWPORT_BG: Color32 = Color32::from_rgb(0x1e, 0x1e, 0x1e);

#[derive(Default, Debug, Clone, Copy)]
pub struct Counts {
    pub text: u32,
    pub mesh: u32,
    pub callback: u32,
    pub primitives: u32,
    pub glyphs_emitted: u32,
    pub glyphs_skipped: u32,
    pub triangles_emitted: u32,
    pub triangles_with_gradient: u32,
    pub textured_meshes_skipped: u32,
}

/// Snapshot of the theme colours that govern semantic text weight.
/// egui has no `weight` field on `TextFormat`; instead `RichText::strong()`
/// resolves the section colour to `Visuals::strong_text_color()` and
/// `RichText::weak()` resolves to `Visuals::weak_text_color()`. By the
/// time the galley reaches the visitor, that semantic has been collapsed
/// into the section's `format.color`, so the only way to recover "this
/// text was painted as strong" is to match the resolved colour back
/// against the visuals snapshot. Match → emit `font-weight=bold`; the
/// browser synth-bolds even though our embedded TTF is single-weight.
#[derive(Default, Debug, Clone, Copy)]
pub struct ThemeWeights {
    /// `None` means: don't apply any weight matching (e.g. when called
    /// without a `Context`, or when the theme uses identical strong/normal
    /// colours which would mis-bold everything).
    pub strong: Option<Color32>,
    pub weak: Option<Color32>,
}

impl ThemeWeights {
    fn from_context(ctx: &Context) -> Self {
        // egui::Context::style() returns Arc<Style>; the closure form
        // doesn't exist in this version of egui (the parallel session
        // that landed 260d6bf9 mis-typed the API).
        let style = ctx.style();
        let v = &style.visuals;
        let normal = v.text_color();
        let strong = v.strong_text_color();
        let weak = v.weak_text_color();
        // Only enable matching when the theme actually distinguishes
        // the three values. A theme that collapses strong → normal
        // (e.g. high-contrast forced colour mode) shouldn't see every
        // normal-text glyph synth-bolded.
        Self {
            strong: (strong != normal).then_some(strong),
            weak: (weak != normal).then_some(weak),
        }
    }
}

struct SvgBuilder {
    body: String,
    defs: String,
    style_block: String,
    viewport: Rect,
    open_clip: Option<Rect>,
    clip_ids: HashMap<u64, String>,
    next_clip_idx: u32,
    next_embed_idx: u32,
    /// When a `FontFamily` is in this map, `emit_text` uses the mapped
    /// synthetic name (which points at an embedded `@font-face` in the
    /// style block) instead of falling through to `fonts.for_id`.
    embed_for_family: HashMap<FontFamily, String>,
    counts: Counts,
    fonts: FontResolver,
    weights: ThemeWeights,
    /// Optional handle to the CPU-side texture cache. `None` in the
    /// post-pass test path (FullOutput visitor); `Some` in the
    /// production plugin path. Textured meshes embed as `<image>` when
    /// available; comment-skip otherwise.
    textures: Option<TexturePixelCacheHandle>,
    /// Snapshot of LinkZones pushed by Hyperlink IDL applies on the
    /// current pass. A TextShape (or Image) whose screen bbox is fully
    /// inside one of these zones gets wrapped in `<a href="…">`.
    links: Vec<LinkZone>,
    /// Currently-open `<a>` wrap inside the SVG body — `Some` between an
    /// opener and its closer, `None` otherwise. Tracked so we can close
    /// it cleanly across clip-group transitions.
    open_link: Option<String>,
    /// What `viewBox` to emit on the root `<svg>`. Defaults to
    /// `viewport`; can be shrunk for `WindowMode::ContentOnly` to crop
    /// the chrome out of the visible area.
    viewbox: Rect,
    /// Optional baseline `<rect>` fill. `Some` emits a viewBox-spanning
    /// rect of that colour just before the body shapes; `None` skips
    /// the rect so the host (HTML page / SVG viewer) shows through.
    bg: Option<Color32>,
    /// Optional CSS class attribute on the root `<svg>` (for webapp
    /// embedders to hook with their stylesheet).
    css_class: Option<String>,
    /// Emit `overflow="hidden"` on the root `<svg>` so shapes whose
    /// painted geometry sits outside `viewbox` are clipped — used by
    /// `WindowMode::ContentOnly` to drop the window's title-bar
    /// chrome without filtering shapes at visit time.
    overflow_hidden: bool,
}

impl SvgBuilder {
    fn new(
        viewport: Rect,
        fonts: FontResolver,
        weights: ThemeWeights,
        textures: Option<TexturePixelCacheHandle>,
        links: Vec<LinkZone>,
        bg: Option<Color32>,
    ) -> Self {
        Self {
            body: String::new(),
            defs: String::new(),
            style_block: String::new(),
            viewport,
            open_clip: None,
            clip_ids: HashMap::new(),
            next_clip_idx: 0,
            next_embed_idx: 0,
            embed_for_family: HashMap::new(),
            counts: Counts::default(),
            fonts,
            weights,
            textures,
            links,
            open_link: None,
            viewbox: viewport,
            bg,
            css_class: None,
            overflow_hidden: false,
        }
    }

    fn set_viewbox(&mut self, r: Rect) {
        self.viewbox = r;
    }

    fn set_css_class(&mut self, c: String) {
        self.css_class = Some(c);
    }

    fn set_overflow_hidden(&mut self, b: bool) {
        self.overflow_hidden = b;
    }

    /// Find the Hyperlink URL whose rect fully contains `bbox`, if any.
    /// Iterates a small Vec (≤ 10s of zones per frame in practice) so a
    /// linear scan is fine; reverse order so a later (nested) hyperlink
    /// shadows an earlier (outer) one.
    fn link_for_bbox(&self, bbox: Rect) -> Option<&str> {
        self.links
            .iter()
            .rev()
            .find(|z| z.rect.contains_rect(bbox))
            .map(|z| z.url.as_str())
    }

    /// Open or close an `<a>` wrap to match `want`. Idempotent; called
    /// per emitted text element. Skip if we're between clip groups
    /// (caller is responsible for closing links before swapping clips).
    fn set_link(&mut self, want: Option<&str>) {
        match (self.open_link.as_deref(), want) {
            (Some(cur), Some(new)) if cur == new => {}
            (None, None) => {}
            _ => {
                self.close_link();
                if let Some(url) = want {
                    let _ = writeln!(
                        self.body,
                        "  <a href=\"{}\" target=\"_blank\" rel=\"noopener\">",
                        xml_escape_attr(url)
                    );
                    self.open_link = Some(url.to_string());
                }
            }
        }
    }

    fn close_link(&mut self) {
        if self.open_link.take().is_some() {
            self.body.push_str("  </a>\n");
        }
    }

    fn add_embedded_font(&mut self, synth_name: &str, subset_bytes: &[u8]) {
        use base64::Engine;
        let b64 = base64::engine::general_purpose::STANDARD.encode(subset_bytes);
        let _ = writeln!(
            self.style_block,
            "      @font-face {{ font-family: '{synth_name}'; src: url(data:font/ttf;base64,{b64}) format('truetype'); }}"
        );
    }

    fn set_embed_for_family(&mut self, family: FontFamily, synth_name: String) {
        self.embed_for_family.insert(family, synth_name);
    }

    fn next_embed_id(&mut self) -> u32 {
        let n = self.next_embed_idx;
        self.next_embed_idx += 1;
        n
    }

    fn finish(mut self) -> String {
        self.close_clip_group(); // already closes any open `<a>`
        let vb = self.viewbox;
        let w = vb.width();
        let h = vb.height();
        let class_attr = match &self.css_class {
            Some(c) => format!(" class=\"{}\"", xml_escape_attr(c)),
            None => String::new(),
        };
        let overflow_attr = if self.overflow_hidden {
            " overflow=\"hidden\""
        } else {
            ""
        };

        let cap = self.body.len() + self.defs.len() + self.style_block.len() + 512;
        let mut svg = String::with_capacity(cap);
        let _ = writeln!(svg, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>");
        let _ = writeln!(
            svg,
            "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"{x} {y} {w} {h}\" width=\"{w}\" height=\"{h}\"{class_attr}{overflow_attr}>",
            x = vb.min.x,
            y = vb.min.y,
        );
        if !self.defs.is_empty() || !self.style_block.is_empty() {
            svg.push_str("  <defs>\n");
            svg.push_str(&self.defs);
            if !self.style_block.is_empty() {
                svg.push_str("    <style><![CDATA[\n");
                svg.push_str(&self.style_block);
                svg.push_str("    ]]></style>\n");
            }
            svg.push_str("  </defs>\n");
        }
        if let Some(bg) = self.bg {
            let _ = writeln!(
                svg,
                "  <rect x=\"{x}\" y=\"{y}\" width=\"{w}\" height=\"{h}\" fill=\"{c}\"/>",
                x = vb.min.x,
                y = vb.min.y,
                c = color_hex(bg),
            );
        }
        svg.push_str(&self.body);
        svg.push_str("</svg>\n");
        svg
    }

    // ------------------------------------------------------------------
    // Clip grouping (task #6)
    // ------------------------------------------------------------------

    /// Treat clips that contain the full viewport as "no clip" — most shapes
    /// inherit a screen-spanning clip and would otherwise each open a redundant
    /// `<g>` group.
    fn effective_clip(&self, clip: Rect) -> Option<Rect> {
        if clip.contains_rect(self.viewport) {
            None
        } else {
            Some(clip)
        }
    }

    fn set_clip(&mut self, clip: Rect) {
        let new = self.effective_clip(clip);
        if new == self.open_clip {
            return;
        }
        self.close_clip_group();
        if let Some(rect) = new {
            let id = self.clip_id_for(rect);
            let _ = writeln!(self.body, "  <g clip-path=\"url(#{id})\">");
            self.open_clip = Some(rect);
        }
    }

    fn close_clip_group(&mut self) {
        // Links live inside clip groups — close the `<a>` before the `</g>`
        // so the produced XML stays well-formed.
        self.close_link();
        if self.open_clip.take().is_some() {
            self.body.push_str("  </g>\n");
        }
    }

    fn clip_id_for(&mut self, rect: Rect) -> String {
        let key = clip_key(rect);
        if let Some(id) = self.clip_ids.get(&key) {
            return id.clone();
        }
        let id = format!("c{}", self.next_clip_idx);
        self.next_clip_idx += 1;
        let _ = writeln!(
            self.defs,
            "    <clipPath id=\"{id}\"><rect x=\"{x:.3}\" y=\"{y:.3}\" width=\"{w:.3}\" height=\"{h:.3}\"/></clipPath>",
            x = rect.min.x,
            y = rect.min.y,
            w = rect.width(),
            h = rect.height(),
        );
        self.clip_ids.insert(key, id.clone());
        id
    }

    // ------------------------------------------------------------------
    // Walker
    // ------------------------------------------------------------------

    fn visit_clipped(&mut self, clipped: &ClippedShape) {
        self.set_clip(clipped.clip_rect);
        self.visit_shape(&clipped.shape);
    }

    fn visit_shape(&mut self, shape: &Shape) {
        // Close any open `<a>` wrap before a non-text shape so the link
        // covers only the text it was meant for. Text shapes drive their
        // own open/close inside `emit_text`. `Vec` is a container so we
        // don't close until we hit a real shape.
        if !matches!(shape, Shape::Text(_) | Shape::Vec(_) | Shape::Noop) {
            self.close_link();
        }
        match shape {
            Shape::Noop => {}
            Shape::Vec(children) => {
                for c in children {
                    self.visit_shape(c);
                }
            }
            Shape::Text(text) => {
                self.counts.text += 1;
                self.emit_text(text);
            }
            Shape::Mesh(mesh) => {
                self.counts.mesh += 1;
                self.emit_mesh(mesh);
            }
            Shape::Rect(rect) => {
                self.counts.primitives += 1;
                self.emit_rect(rect);
            }
            Shape::Circle(c) => {
                self.counts.primitives += 1;
                self.emit_circle(c);
            }
            Shape::Ellipse(e) => {
                self.counts.primitives += 1;
                self.emit_ellipse(e);
            }
            Shape::LineSegment { points, stroke } => {
                self.counts.primitives += 1;
                self.emit_line_segment(points, stroke);
            }
            Shape::Path(p) => {
                self.counts.primitives += 1;
                self.emit_path(p);
            }
            Shape::QuadraticBezier(q) => {
                self.counts.primitives += 1;
                self.emit_quad_bezier(q);
            }
            Shape::CubicBezier(c) => {
                self.counts.primitives += 1;
                self.emit_cubic_bezier(c);
            }
            Shape::Callback(cb) => {
                self.counts.callback += 1;
                self.emit_callback_placeholder(cb.rect);
            }
        }
    }

    // ------------------------------------------------------------------
    // Emitters: text
    // ------------------------------------------------------------------

    fn emit_text(&mut self, text: &TextShape) {
        let galley = &text.galley;
        let job = &galley.job;
        let origin = text.pos;
        let opacity = text.opacity_factor.clamp(0.0, 1.0);

        if job.sections.is_empty() {
            return;
        }

        // Wrap in `<a href="…">` if this text shape's screen bbox lies
        // inside a registered hyperlink zone. `galley.rect` is in galley-
        // local coords with top=0; translate by `origin` for screen-space.
        let text_bbox = Rect::from_min_size(origin + galley.rect.min.to_vec2(), galley.rect.size());
        let link_url = self.link_for_bbox(text_bbox).map(|s| s.to_string());
        self.set_link(link_url.as_deref());

        // Syntax-highlighted text (CodeView, json, markdown, ...) uses one
        // `LayoutSection` per token, each carrying its own colour and font.
        // `Glyph::section_index` is `pub(crate)` so we can't read it
        // directly — instead we walk the original `job.text` in lockstep
        // with the glyphs, advancing a byte cursor by `chr.len_utf8()` per
        // glyph (plus one for the trailing `\n` egui drops from each row's
        // glyph list). Then we pick the section whose `byte_range`
        // contains the cursor. The sections list is sorted by byte_range
        // so we can advance a single index instead of scanning.
        let mut byte_cursor: usize = 0;
        let mut section_idx: usize = 0;

        let fallback = text.fallback_color;
        let resolve = |c: Color32| -> Color32 {
            if c == Color32::PLACEHOLDER { fallback } else { c }
        };

        for placed_row in &galley.rows {
            for glyph in &placed_row.row.glyphs {
                while section_idx + 1 < job.sections.len()
                    && byte_cursor >= job.sections[section_idx].byte_range.end
                {
                    section_idx += 1;
                }
                let section = &job.sections[section_idx];
                let format = &section.format;

                let glyph_color = text
                    .override_text_color
                    .unwrap_or_else(|| resolve(format.color));
                let text_alpha = (glyph_color.a() as f32) / 255.0 * opacity;

                let bx = origin.x + placed_row.pos.x + glyph.pos.x;
                let by = origin.y + placed_row.pos.y + glyph.pos.y;
                let em_size = format.font_id.size;
                let advance = glyph.advance_width;

                // Advance the byte cursor regardless of whether we emit;
                // skipping the byte advance would desync section routing
                // for every subsequent glyph in the row.
                byte_cursor += glyph.chr.len_utf8();

                // 1. Background (emit even for whitespace so spaces inside
                //    a styled run stay highlighted). Drawn before the
                //    glyph so the text sits on top.
                let bg = resolve(format.background);
                if bg.a() > 0 && advance > 0.0 {
                    let bg_y = by - glyph.font_ascent;
                    let bg_h = glyph.font_height;
                    let _ = writeln!(
                        self.body,
                        "  <rect x=\"{bx:.2}\" y=\"{bg_y:.2}\" width=\"{advance:.2}\" height=\"{bg_h:.2}\" fill=\"{fill}\" fill-opacity=\"{a:.3}\"/>",
                        fill = color_hex(bg),
                        a = (bg.a() as f32) * opacity / 255.0,
                    );
                }

                // 2. Text — only when there's an actual glyph to draw.
                if !glyph.chr.is_whitespace()
                    && !glyph.uv_rect.is_nothing()
                    && text_alpha > 0.0
                {
                    let family_hint = if let Some(chain) =
                        self.embed_for_family.get(&format.font_id.family)
                    {
                        chain.clone()
                    } else {
                        self.fonts.for_id(&format.font_id)
                    };
                    let fill = color_hex(glyph_color);
                    let style_attr = if format.italics {
                        " font-style=\"italic\""
                    } else {
                        ""
                    };
                    // Recover egui's strong/weak semantic from the resolved
                    // colour: at galley-construction time, RichText::strong
                    // / weak set `format.color` to the matching visuals
                    // colour. We compare back against the snapshot taken at
                    // export start and emit `font-weight` so browsers
                    // synth-bold (or thin) when the embedded subset is
                    // single-weight.
                    let weight_attr =
                        if self.weights.strong == Some(glyph_color) {
                            " font-weight=\"bold\""
                        } else if self.weights.weak == Some(glyph_color) {
                            " font-weight=\"lighter\""
                        } else {
                            ""
                        };
                    let _ = writeln!(
                        self.body,
                        "  <text x=\"{bx:.2}\" y=\"{by:.2}\" font-size=\"{em_size:.2}\" font-family=\"{family_hint}\"{style_attr}{weight_attr} fill=\"{fill}\" fill-opacity=\"{text_alpha:.3}\">{ch}</text>",
                        ch = xml_escape_char(glyph.chr),
                    );
                    self.counts.glyphs_emitted += 1;
                } else {
                    self.counts.glyphs_skipped += 1;
                }

                // 3. Underline — drawn after the glyph so it sits on top.
                //    Width comes from `format.underline.width`; one short
                //    segment per glyph (adjacent glyphs concatenate into a
                //    continuous line). Whitespace inside an underlined run
                //    still gets its segment so spaces don't create gaps.
                if format.underline.width > 0.0 && advance > 0.0 {
                    let ul_color = resolve(format.underline.color);
                    if ul_color.a() > 0 {
                        // Sit the underline a hair below the baseline; the
                        // exact offset is font-specific in real typography
                        // but a small constant looks fine at UI sizes.
                        let ul_y = by + format.underline.width.max(1.0);
                        let _ = writeln!(
                            self.body,
                            "  <line x1=\"{bx:.2}\" y1=\"{ul_y:.2}\" x2=\"{x2:.2}\" y2=\"{ul_y:.2}\" stroke=\"{c}\" stroke-opacity=\"{a:.3}\" stroke-width=\"{w:.2}\"/>",
                            x2 = bx + advance,
                            c = color_hex(ul_color),
                            a = (ul_color.a() as f32) * opacity / 255.0,
                            w = format.underline.width,
                        );
                    }
                }

                // 4. Strikethrough — same construction, runs through the
                //    glyph's x-height. ~0.25 × em-size above the baseline
                //    is a good approximation across Latin / CJK at UI
                //    sizes.
                if format.strikethrough.width > 0.0 && advance > 0.0 {
                    let st_color = resolve(format.strikethrough.color);
                    if st_color.a() > 0 {
                        let st_y = by - em_size * 0.25;
                        let _ = writeln!(
                            self.body,
                            "  <line x1=\"{bx:.2}\" y1=\"{st_y:.2}\" x2=\"{x2:.2}\" y2=\"{st_y:.2}\" stroke=\"{c}\" stroke-opacity=\"{a:.3}\" stroke-width=\"{w:.2}\"/>",
                            x2 = bx + advance,
                            c = color_hex(st_color),
                            a = (st_color.a() as f32) * opacity / 255.0,
                            w = format.strikethrough.width,
                        );
                    }
                }
            }
            // Each `\n` in the source text creates a new PlacedRow but is
            // omitted from the row's `glyphs` (see Galley docs). Skip past
            // it so the next row's first glyph hits the right section.
            if placed_row.ends_with_newline {
                byte_cursor += 1; // `\n` is one UTF-8 byte
            }
        }
    }

    // ------------------------------------------------------------------
    // Emitters: mesh
    // ------------------------------------------------------------------

    fn emit_mesh(&mut self, mesh: &Mesh) {
        if mesh.texture_id != TextureId::default() {
            // Textured mesh: try to embed pixels from the shared texture
            // cache as an `<image>`. The cache is populated by the
            // image-widget upload path (see `ImageCache::upload`); scrolling-
            // texture and walkers don't currently mirror their pixels.
            if self.try_emit_textured_mesh(mesh) {
                self.counts.triangles_emitted += (mesh.indices.len() / 3) as u32;
                return;
            }
            self.counts.textured_meshes_skipped += 1;
            // texture_id is intentionally omitted: it's a TextureId::Managed(N)
            // from the egui texture cache, allocated in insertion order
            // (which depends on network tile fetch order in the walkers /
            // elevation-profile demos). Including it would scramble the
            // SVG output byte-for-byte between tour runs without any visual
            // meaning. The verts/tris counts are deterministic from the
            // geometry and still useful as diagnostic info.
            let _ = writeln!(
                self.body,
                "  <!-- skipped textured mesh: {} verts, {} tris -->",
                mesh.vertices.len(),
                mesh.indices.len() / 3,
            );
            return;
        }
        for [i0, i1, i2] in mesh.triangles() {
            let v0 = &mesh.vertices[i0 as usize];
            let v1 = &mesh.vertices[i1 as usize];
            let v2 = &mesh.vertices[i2 as usize];
            if v0.color != v1.color || v1.color != v2.color {
                self.counts.triangles_with_gradient += 1;
            }
            let alpha = (v0.color.a() as f32) / 255.0;
            let _ = writeln!(
                self.body,
                "  <polygon points=\"{x0:.2},{y0:.2} {x1:.2},{y1:.2} {x2:.2},{y2:.2}\" fill=\"{fill}\" fill-opacity=\"{alpha:.3}\"/>",
                x0 = v0.pos.x, y0 = v0.pos.y,
                x1 = v1.pos.x, y1 = v1.pos.y,
                x2 = v2.pos.x, y2 = v2.pos.y,
                fill = color_hex(v0.color),
            );
            self.counts.triangles_emitted += 1;
        }
    }

    /// Returns `true` if the textured mesh was embedded as an `<image>`.
    ///
    /// For 4-vertex quad meshes (the in-repo case — `painter.image`,
    /// `Image::rotate`, `scrolling_texture::draw_textured_quad`) recovers
    /// the `local → screen` affine and emits `<image>` with
    /// `transform="matrix(...)"`. That preserves rotation from
    /// `Image::rotate` (rotated screen positions, axis-aligned UVs) and
    /// from `scrolling_texture` orientations (axis-aligned screen,
    /// permuted UVs that `painter.image`'s rect-UV API can't express).
    /// Non-quad meshes — and degenerate quads with collinear UVs — fall
    /// back to an axis-aligned `<image>` at the screen AABB: visually
    /// wrong for rotated cases but no worse than the pre-affine
    /// behaviour. In both paths the UV bbox is used to crop the embedded
    /// PNG so the data URL only ships the texture region the mesh
    /// actually samples.
    fn try_emit_textured_mesh(&mut self, mesh: &Mesh) -> bool {
        let Some(cache_handle) = &self.textures else {
            return false;
        };
        if mesh.vertices.is_empty() || mesh.indices.is_empty() {
            return false;
        }
        // Screen-space bbox.
        let mut min_x = f32::INFINITY;
        let mut min_y = f32::INFINITY;
        let mut max_x = f32::NEG_INFINITY;
        let mut max_y = f32::NEG_INFINITY;
        // UV bbox (egui Vertex.uv is normalised 0..1).
        let mut min_u = f32::INFINITY;
        let mut min_v = f32::INFINITY;
        let mut max_u = f32::NEG_INFINITY;
        let mut max_v = f32::NEG_INFINITY;
        for v in &mesh.vertices {
            min_x = min_x.min(v.pos.x);
            min_y = min_y.min(v.pos.y);
            max_x = max_x.max(v.pos.x);
            max_y = max_y.max(v.pos.y);
            min_u = min_u.min(v.uv.x);
            min_v = min_v.min(v.uv.y);
            max_u = max_u.max(v.uv.x);
            max_v = max_v.max(v.uv.y);
        }
        let w = max_x - min_x;
        let h = max_y - min_y;
        if !(w.is_finite() && h.is_finite() && w > 0.0 && h > 0.0) {
            return false;
        }

        // Pull pixels under a short-lived borrow; lock contention is
        // bounded by the export frame.
        let cached = {
            let guard = cache_handle.lock().expect("texture cache poisoned");
            guard.get(mesh.texture_id).cloned()
        };
        let Some(cached) = cached else {
            return false;
        };
        if cached.width == 0 || cached.height == 0 {
            return false;
        }

        // Crop the source pixels to the UV bbox so we don't ship the
        // whole sheet when the mesh only uses a tile.
        let tex_w = cached.width as f32;
        let tex_h = cached.height as f32;
        let crop_x0 = (min_u.clamp(0.0, 1.0) * tex_w).round() as u32;
        let crop_y0 = (min_v.clamp(0.0, 1.0) * tex_h).round() as u32;
        let crop_x1 = (max_u.clamp(0.0, 1.0) * tex_w).round() as u32;
        let crop_y1 = (max_v.clamp(0.0, 1.0) * tex_h).round() as u32;
        let crop_w = crop_x1.saturating_sub(crop_x0).max(1);
        let crop_h = crop_y1.saturating_sub(crop_y0).max(1);
        let png_bytes = if crop_x0 == 0
            && crop_y0 == 0
            && crop_x1 >= cached.width
            && crop_y1 >= cached.height
        {
            // Whole texture — encode in place.
            rgba_to_png(cached.width, cached.height, &cached.rgba)
        } else {
            // Slice rows of RGBA into a tight buffer.
            let mut cropped = Vec::with_capacity((crop_w as usize) * (crop_h as usize) * 4);
            for y in 0..crop_h {
                let src_y = (crop_y0 + y) as usize;
                let row_start = (src_y * cached.width as usize + crop_x0 as usize) * 4;
                let row_end = row_start + (crop_w as usize) * 4;
                if row_end <= cached.rgba.len() {
                    cropped.extend_from_slice(&cached.rgba[row_start..row_end]);
                }
            }
            rgba_to_png(crop_w, crop_h, &cropped)
        };
        let Some(png_bytes) = png_bytes else {
            return false;
        };

        use base64::Engine;
        let b64 = base64::engine::general_purpose::STANDARD.encode(&png_bytes);
        // `image-rendering="pixelated"` keeps nearest-neighbour scaling
        // when the browser upscales the embedded PNG — matches egui's
        // `TextureOptions::NEAREST` choice for checker/heatmap textures.
        // Without this Firefox bilinear-blurs them.
        let render_attr = if cached.nearest {
            " image-rendering=\"pixelated\""
        } else {
            ""
        };
        // 4-vertex quad: recover the local → screen affine so rotation
        // lands in SVG. Catches `Image::rotate` (rotated screen, plain
        // UV) and `scrolling_texture` orientations (plain screen,
        // permuted UV) from the same solve.
        if mesh.vertices.len() == 4 {
            if let Some([a, b, c, d, e, f]) =
                solve_textured_quad_affine(&mesh.vertices, tex_w, tex_h, crop_x0, crop_y0)
            {
                let _ = writeln!(
                    self.body,
                    "  <image x=\"0\" y=\"0\" width=\"{crop_w}\" height=\"{crop_h}\" preserveAspectRatio=\"none\"{render_attr} transform=\"matrix({a:.6} {b:.6} {c:.6} {d:.6} {e:.3} {f:.3})\" href=\"data:image/png;base64,{b64}\"/>",
                );
                return true;
            }
        }

        // Fallback: non-quad mesh, or a quad whose UVs are collinear in
        // every triple we tried. Emit at the screen AABB — wrong for
        // rotated cases but at least visible.
        let _ = writeln!(
            self.body,
            "  <image x=\"{x:.2}\" y=\"{y:.2}\" width=\"{w:.2}\" height=\"{h:.2}\" preserveAspectRatio=\"none\"{render_attr} href=\"data:image/png;base64,{b64}\"/>",
            x = min_x,
            y = min_y,
        );
        true
    }

    // ------------------------------------------------------------------
    // Emitters: primitives
    // ------------------------------------------------------------------

    fn emit_rect(&mut self, r: &RectShape) {
        // SVG `<rect>` strokes are centered; egui's StrokeKind (Inside/Middle/
        // Outside) doesn't have a direct analog. v0 ignores stroke_kind and
        // emits centered strokes — small visual drift at narrow widths.
        // `blur_width` (shadow/glow) is also ignored for v0.
        let fill = fill_attr(r.fill);
        let stroke = stroke_attr(&r.stroke);
        let r_avg = uniform_corner_radius(&r.corner_radius);
        let rx_attr = if r_avg > 0.0 {
            format!(" rx=\"{r_avg:.2}\" ry=\"{r_avg:.2}\"")
        } else {
            String::new()
        };
        let transform = rect_transform(r);
        let _ = writeln!(
            self.body,
            "  <rect x=\"{x:.2}\" y=\"{y:.2}\" width=\"{w:.2}\" height=\"{h:.2}\"{rx}{fill}{stroke}{transform}/>",
            x = r.rect.min.x,
            y = r.rect.min.y,
            w = r.rect.width(),
            h = r.rect.height(),
            rx = rx_attr,
        );
    }

    fn emit_circle(&mut self, c: &CircleShape) {
        let fill = fill_attr(c.fill);
        let stroke = stroke_attr(&c.stroke);
        let _ = writeln!(
            self.body,
            "  <circle cx=\"{cx:.2}\" cy=\"{cy:.2}\" r=\"{r:.2}\"{fill}{stroke}/>",
            cx = c.center.x,
            cy = c.center.y,
            r = c.radius,
        );
    }

    fn emit_ellipse(&mut self, e: &EllipseShape) {
        let fill = fill_attr(e.fill);
        let stroke = stroke_attr(&e.stroke);
        let _ = writeln!(
            self.body,
            "  <ellipse cx=\"{cx:.2}\" cy=\"{cy:.2}\" rx=\"{rx:.2}\" ry=\"{ry:.2}\"{fill}{stroke}/>",
            cx = e.center.x,
            cy = e.center.y,
            rx = e.radius.x,
            ry = e.radius.y,
        );
    }

    fn emit_line_segment(&mut self, points: &[Pos2; 2], stroke: &Stroke) {
        if stroke.width <= 0.0 || stroke.color.a() == 0 {
            return;
        }
        let _ = writeln!(
            self.body,
            "  <line x1=\"{x1:.2}\" y1=\"{y1:.2}\" x2=\"{x2:.2}\" y2=\"{y2:.2}\" stroke=\"{c}\" stroke-opacity=\"{a:.3}\" stroke-width=\"{w:.2}\"/>",
            x1 = points[0].x, y1 = points[0].y,
            x2 = points[1].x, y2 = points[1].y,
            c = color_hex(stroke.color),
            a = (stroke.color.a() as f32) / 255.0,
            w = stroke.width,
        );
    }

    fn emit_path(&mut self, p: &PathShape) {
        if p.points.is_empty() {
            return;
        }
        let mut d = String::new();
        for (i, pt) in p.points.iter().enumerate() {
            let cmd = if i == 0 { 'M' } else { 'L' };
            let _ = write!(d, "{cmd}{:.2},{:.2} ", pt.x, pt.y);
        }
        if p.closed {
            d.push('Z');
        }
        let fill = if p.closed {
            fill_attr(p.fill)
        } else {
            " fill=\"none\"".into()
        };
        let stroke = path_stroke_attr(&p.stroke);
        let _ = writeln!(self.body, "  <path d=\"{d}\"{fill}{stroke}/>");
    }

    fn emit_quad_bezier(&mut self, q: &QuadraticBezierShape) {
        let [p0, p1, p2] = q.points;
        let mut d = format!("M{:.2},{:.2} Q{:.2},{:.2} {:.2},{:.2}", p0.x, p0.y, p1.x, p1.y, p2.x, p2.y);
        if q.closed {
            d.push_str(" Z");
        }
        let fill = if q.closed { fill_attr(q.fill) } else { " fill=\"none\"".into() };
        let stroke = path_stroke_attr(&q.stroke);
        let _ = writeln!(self.body, "  <path d=\"{d}\"{fill}{stroke}/>");
    }

    fn emit_cubic_bezier(&mut self, c: &CubicBezierShape) {
        let [p0, p1, p2, p3] = c.points;
        let mut d = format!(
            "M{:.2},{:.2} C{:.2},{:.2} {:.2},{:.2} {:.2},{:.2}",
            p0.x, p0.y, p1.x, p1.y, p2.x, p2.y, p3.x, p3.y,
        );
        if c.closed {
            d.push_str(" Z");
        }
        let fill = if c.closed { fill_attr(c.fill) } else { " fill=\"none\"".into() };
        let stroke = path_stroke_attr(&c.stroke);
        let _ = writeln!(self.body, "  <path d=\"{d}\"{fill}{stroke}/>");
    }

    // ------------------------------------------------------------------
    // Emitter: callback placeholder (task #8)
    // ------------------------------------------------------------------

    fn emit_callback_placeholder(&mut self, rect: Rect) {
        // Paint callbacks are wgpu closures — we can't introspect their
        // output. Draw a visible "something was here" rectangle so the SVG
        // makes the omission obvious instead of silently dropping content.
        let _ = writeln!(
            self.body,
            "  <rect x=\"{x:.2}\" y=\"{y:.2}\" width=\"{w:.2}\" height=\"{h:.2}\" fill=\"none\" stroke=\"#888\" stroke-width=\"1\" stroke-dasharray=\"4,2\"/><!-- Shape::Callback (wgpu paint) -->",
            x = rect.min.x,
            y = rect.min.y,
            w = rect.width(),
            h = rect.height(),
        );
    }
}

// ============================================================================
// Helpers
// ============================================================================

/// Recover the affine matrix that maps the cropped PNG's local pixel
/// space `(0..crop_w, 0..crop_h)` onto screen space, for a 4-vertex
/// textured quad mesh. Returns `[a, b, c, d, e, f]` in SVG
/// `matrix(a b c d e f)` order:
///     `screen_x = a*lx + c*ly + e`
///     `screen_y = b*lx + d*ly + f`
///
/// Solves the `uv → screen` affine by Cramer's rule from any 3
/// non-collinear vertices, then composes with the known `local → uv`
/// map `(u, v) = ((crop_x0 + lx)/tex_w, (crop_y0 + ly)/tex_h)`. Tries
/// the (0, 1, 2) vertex triple first and falls back to (0, 1, 3) —
/// both `Mesh::add_rect_with_uv` (TL, TR, BL, BR) and
/// `draw_textured_quad` (TL, TR, BR, BL) layouts have non-collinear
/// UVs at (0, 1, 2) in practice; the fallback is defensive. `None`
/// only on a truly degenerate quad.
fn solve_textured_quad_affine(
    vertices: &[Vertex],
    tex_w: f32,
    tex_h: f32,
    crop_x0: u32,
    crop_y0: u32,
) -> Option<[f32; 6]> {
    let solve = |i0: usize, i1: usize, i2: usize|
        -> Option<(f32, f32, f32, f32, f32, f32)>
    {
        let p0 = &vertices[i0];
        let p1 = &vertices[i1];
        let p2 = &vertices[i2];
        let u0 = p0.uv.x;
        let v0 = p0.uv.y;
        let u1 = p1.uv.x;
        let v1 = p1.uv.y;
        let u2 = p2.uv.x;
        let v2 = p2.uv.y;
        let det = u0 * (v1 - v2) - v0 * (u1 - u2) + (u1 * v2 - u2 * v1);
        if det.abs() < 1e-8 {
            return None;
        }
        let inv = 1.0 / det;
        let x0 = p0.pos.x;
        let y0 = p0.pos.y;
        let x1 = p1.pos.x;
        let y1 = p1.pos.y;
        let x2 = p2.pos.x;
        let y2 = p2.pos.y;
        let a = ((v1 - v2) * x0 + (v2 - v0) * x1 + (v0 - v1) * x2) * inv;
        let c = ((u2 - u1) * x0 + (u0 - u2) * x1 + (u1 - u0) * x2) * inv;
        let e = ((u1 * v2 - u2 * v1) * x0
              + (u2 * v0 - u0 * v2) * x1
              + (u0 * v1 - u1 * v0) * x2) * inv;
        let b = ((v1 - v2) * y0 + (v2 - v0) * y1 + (v0 - v1) * y2) * inv;
        let d = ((u2 - u1) * y0 + (u0 - u2) * y1 + (u1 - u0) * y2) * inv;
        let f = ((u1 * v2 - u2 * v1) * y0
              + (u2 * v0 - u0 * v2) * y1
              + (u0 * v1 - u1 * v0) * y2) * inv;
        Some((a, b, c, d, e, f))
    };
    let (a, b, c, d, e, f) = solve(0, 1, 2).or_else(|| solve(0, 1, 3))?;
    let cx0 = crop_x0 as f32;
    let cy0 = crop_y0 as f32;
    Some([
        a / tex_w,
        b / tex_w,
        c / tex_h,
        d / tex_h,
        a * cx0 / tex_w + c * cy0 / tex_h + e,
        b * cx0 / tex_w + d * cy0 / tex_h + f,
    ])
}

fn rect_transform(r: &RectShape) -> String {
    if r.angle == 0.0 {
        String::new()
    } else {
        let cx = r.rect.center().x;
        let cy = r.rect.center().y;
        let deg = r.angle.to_degrees();
        format!(" transform=\"rotate({deg:.3} {cx:.2} {cy:.2})\"")
    }
}

fn uniform_corner_radius(cr: &egui::epaint::CornerRadius) -> f32 {
    let max = cr.nw.max(cr.ne).max(cr.sw).max(cr.se);
    max as f32
}

fn fill_attr(c: Color32) -> String {
    if c.a() == 0 {
        " fill=\"none\"".into()
    } else {
        format!(
            " fill=\"{}\" fill-opacity=\"{:.3}\"",
            color_hex(c),
            (c.a() as f32) / 255.0
        )
    }
}

fn stroke_attr(s: &Stroke) -> String {
    if s.width <= 0.0 || s.color.a() == 0 {
        String::new()
    } else {
        format!(
            " stroke=\"{}\" stroke-opacity=\"{:.3}\" stroke-width=\"{:.2}\"",
            color_hex(s.color),
            (s.color.a() as f32) / 255.0,
            s.width,
        )
    }
}

fn path_stroke_attr(s: &PathStroke) -> String {
    if s.width <= 0.0 {
        return String::new();
    }
    match &s.color {
        ColorMode::Solid(c) => {
            if c.a() == 0 {
                String::new()
            } else {
                format!(
                    " stroke=\"{}\" stroke-opacity=\"{:.3}\" stroke-width=\"{:.2}\"",
                    color_hex(*c),
                    (c.a() as f32) / 255.0,
                    s.width,
                )
            }
        }
        // UV-shaded strokes (gradient along the path) can't be expressed
        // 1:1 in SVG; we drop the stroke and tag the shape.
        ColorMode::UV(_) => format!(
            " stroke=\"none\"/><!-- PathStroke ColorMode::UV unsupported, width={} ",
            s.width,
        ),
    }
}

fn color_hex(c: Color32) -> String {
    // `Color32` stores premultiplied RGBA — every channel has already been
    // multiplied by `a/255`. SVG `fill` / `stroke` use straight alpha and
    // apply `fill-opacity` / `stroke-opacity` separately, so re-dimming
    // the premultiplied RGB by alpha again would render the pixel at
    // ~alpha² instead of alpha (visible as washed-out badge backgrounds
    // for low-alpha colours). Un-premultiply before emitting; the alpha
    // value itself is identical in both representations so callers can
    // keep using `c.a()` for the *-opacity attributes.
    let [r, g, b, _] = c.to_srgba_unmultiplied();
    format!("#{r:02x}{g:02x}{b:02x}")
}

fn xml_escape_attr(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&apos;"),
            _ => out.push(c),
        }
    }
    out
}

fn xml_escape_char(c: char) -> String {
    match c {
        '<' => "&lt;".into(),
        '>' => "&gt;".into(),
        '&' => "&amp;".into(),
        '"' => "&quot;".into(),
        '\'' => "&apos;".into(),
        c => c.to_string(),
    }
}

fn clip_key(r: Rect) -> u64 {
    // Quantise so floating-point jitter doesn't break grouping of clips that
    // are visually identical (egui rounds positions to pixels).
    let q = |f: f32| (f * 100.0).round() as i64;
    let mut h = std::num::Wrapping(1469598103934665603u64); // FNV-1a offset
    for v in [r.min.x, r.min.y, r.max.x, r.max.y] {
        h ^= std::num::Wrapping(q(v) as u64);
        h *= std::num::Wrapping(1099511628211);
    }
    h.0
}

#[cfg(test)]
mod tests {
    use super::*;

    fn vtx(pos: (f32, f32), uv: (f32, f32)) -> Vertex {
        Vertex {
            pos: Pos2::new(pos.0, pos.1),
            uv: Pos2::new(uv.0, uv.1),
            color: Color32::WHITE,
        }
    }

    fn matrix_close(actual: [f32; 6], expected: [f32; 6], tol: f32) -> bool {
        actual
            .iter()
            .zip(expected.iter())
            .all(|(a, e)| (a - e).abs() < tol)
    }

    #[test]
    fn affine_identity_axis_aligned() {
        // Standard `painter.image`/`Mesh::add_rect_with_uv` quad with full
        // UV coverage and the screen rect at (10,20)-(110,80) over a
        // 100x60 texture. Local (lx, ly) → screen (lx+10, ly+20).
        let v = [
            vtx((10.0, 20.0), (0.0, 0.0)),
            vtx((110.0, 20.0), (1.0, 0.0)),
            vtx((10.0, 80.0), (0.0, 1.0)),
            vtx((110.0, 80.0), (1.0, 1.0)),
        ];
        let m = solve_textured_quad_affine(&v, 100.0, 60.0, 0, 0)
            .expect("non-degenerate");
        assert!(
            matrix_close(m, [1.0, 0.0, 0.0, 1.0, 10.0, 20.0], 1e-4),
            "got {:?}",
            m
        );
    }

    #[test]
    fn affine_image_rotate_90_cw() {
        // `Image::rotate(90° CW)` keeps UVs at the rect corners but
        // rotates pos[] around the rect centre. Result should be a 90°-CW
        // matrix.
        let v = [
            vtx((100.0, 0.0), (0.0, 0.0)),
            vtx((100.0, 100.0), (1.0, 0.0)),
            vtx((0.0, 0.0), (0.0, 1.0)),
            vtx((0.0, 100.0), (1.0, 1.0)),
        ];
        let m = solve_textured_quad_affine(&v, 100.0, 100.0, 0, 0)
            .expect("non-degenerate");
        assert!(
            matrix_close(m, [0.0, 1.0, -1.0, 0.0, 100.0, 0.0], 1e-4),
            "got {:?}",
            m
        );
    }

    #[test]
    fn affine_horizontal_flip() {
        // ScrollRight-style: screen rect is axis-aligned but the UVs
        // are mirrored along u — TL gets uv=(1,0), TR gets uv=(0,0).
        let v = [
            vtx((0.0, 0.0), (1.0, 0.0)),
            vtx((100.0, 0.0), (0.0, 0.0)),
            vtx((0.0, 60.0), (1.0, 1.0)),
            vtx((100.0, 60.0), (0.0, 1.0)),
        ];
        let m = solve_textured_quad_affine(&v, 100.0, 60.0, 0, 0)
            .expect("non-degenerate");
        // Local (lx, ly) → screen (100 - lx, ly).
        assert!(
            matrix_close(m, [-1.0, 0.0, 0.0, 1.0, 100.0, 0.0], 1e-4),
            "got {:?}",
            m
        );
    }

    #[test]
    fn affine_scrolling_texture_scrollup() {
        // ScrollUp sub-draw 1 (draw_head=9 of 160): screen rect is
        // axis-aligned 160×151 at the origin, but UVs are permuted so the
        // texture is rotated 90° CW into the panel.
        let split = 9.0 / 160.0;
        let v = [
            vtx((0.0, 0.0), (split, 0.0)),
            vtx((160.0, 0.0), (split, 1.0)),
            vtx((160.0, 151.0), (1.0, 1.0)),
            vtx((0.0, 151.0), (1.0, 0.0)),
        ];
        let m = solve_textured_quad_affine(&v, 160.0, 160.0, 9, 0)
            .expect("non-degenerate");
        // Axis swap: local-x → screen-y, local-y → screen-x.
        assert!(
            matrix_close(m, [0.0, 1.0, 1.0, 0.0, 0.0, 0.0], 1e-3),
            "got {:?}",
            m
        );
        // Sanity: image TR (local 151,0, ie the right edge of the
        // cropped 151×160 PNG) lands at screen v3 = (0, 151).
        let (a, b, c, d, e, f) = (m[0], m[1], m[2], m[3], m[4], m[5]);
        let sx = a * 151.0 + c * 0.0 + e;
        let sy = b * 151.0 + d * 0.0 + f;
        assert!(
            (sx - 0.0).abs() < 1e-2 && (sy - 151.0).abs() < 1e-2,
            "local(151,0) → ({}, {}), want (0, 151)",
            sx,
            sy
        );
    }

    #[test]
    fn affine_cropped_axis_aligned() {
        // UV samples [(0.25,0)..(1,1)] = right 75% of a 100×60 texture
        // onto a screen rect at (100,200)-(400,300). Crop is baked into
        // the matrix; the resulting transform must map local (0,0) and
        // local (crop_w, crop_h) to the screen rect corners.
        let v = [
            vtx((100.0, 200.0), (0.25, 0.0)),
            vtx((400.0, 200.0), (1.0, 0.0)),
            vtx((100.0, 300.0), (0.25, 1.0)),
            vtx((400.0, 300.0), (1.0, 1.0)),
        ];
        let m = solve_textured_quad_affine(&v, 100.0, 60.0, 25, 0)
            .expect("non-degenerate");
        assert!(
            matrix_close(m, [4.0, 0.0, 0.0, 100.0 / 60.0, 100.0, 200.0], 1e-3),
            "got {:?}",
            m
        );
        // Round-trip: local (75, 60) — the crop's bottom-right — must
        // land at screen (400, 300).
        let (a, b, c, d, e, f) = (m[0], m[1], m[2], m[3], m[4], m[5]);
        let sx = a * 75.0 + c * 60.0 + e;
        let sy = b * 75.0 + d * 60.0 + f;
        assert!(
            (sx - 400.0).abs() < 1e-2 && (sy - 300.0).abs() < 1e-2,
            "local(75,60) → ({}, {}), want (400, 300)",
            sx,
            sy
        );
    }

    #[test]
    fn render_svg_window_filters_to_target_layer() {
        // Drive a single pass with two `Window`s. Inside the pass (before
        // `end_pass` drains `ctx.graphics()`), call `render_svg_window`
        // for each id and confirm the output is scoped to that window.
        use egui::{RawInput, Vec2};

        let ctx = Context::default();
        let alpha_id = egui::Id::new("alpha-window");
        let beta_id = egui::Id::new("beta-window");
        let resolver = FontResolver::default();
        let textures: TexturePixelCacheHandle =
            Arc::new(Mutex::new(TexturePixelCache::default()));
        let links: LinkZonesHandle = Arc::new(Mutex::new(Vec::new()));

        let raw = RawInput {
            screen_rect: Some(Rect::from_min_size(Pos2::ZERO, Vec2::new(800.0, 600.0))),
            ..Default::default()
        };
        // Warm-up passes: egui::Window does deferred auto-sizing (reserves
        // Shape::Noop slots on the first pass, fills them on a later pass
        // once outer_rect is known). Two passes get us settled geometry
        // even with max_passes=2 / multipass off.
        let show_ui = |ctx: &Context| {
            egui::Window::new("Alpha")
                .id(alpha_id)
                .default_pos([10.0, 10.0])
                .show(ctx, |ui| {
                    ui.label("ZZZ");
                });
            egui::Window::new("Beta")
                .id(beta_id)
                .default_pos([300.0, 10.0])
                .show(ctx, |ui| {
                    ui.label("QQQ");
                });
        };
        for _ in 0..2 {
            let _ = ctx.run(raw.clone(), |ctx| show_ui(ctx));
        }
        let mut alpha_svg: Option<String> = None;
        let mut beta_svg: Option<String> = None;
        let _ = ctx.run(raw, |ctx| {
            show_ui(ctx);
            alpha_svg = render_svg_window(
                ctx,
                &resolver,
                &textures,
                &links,
                false,
                alpha_id,
                WindowMode::Faithful,
                Some(VIEWPORT_BG),
            );
            beta_svg = render_svg_window(
                ctx,
                &resolver,
                &textures,
                &links,
                false,
                beta_id,
                WindowMode::Faithful,
                Some(VIEWPORT_BG),
            );
        });

        let alpha = alpha_svg.expect("alpha window rendered");
        let beta = beta_svg.expect("beta window rendered");

        // 'Z' is only in alpha's body, 'Q' is only in beta's body. The
        // window titles "Alpha"/"Beta" share no letters with either
        // sentinel, so a per-window filter produces a clean partition.
        assert!(
            alpha.contains(">Z</text>"),
            "alpha SVG should carry the Z glyph from its body"
        );
        assert!(
            !alpha.contains(">Q</text>"),
            "alpha SVG must not leak the Q glyph from beta's body"
        );
        assert!(
            beta.contains(">Q</text>"),
            "beta SVG should carry the Q glyph from its body"
        );
        assert!(
            !beta.contains(">Z</text>"),
            "beta SVG must not leak the Z glyph from alpha's body"
        );
    }

    #[test]
    fn render_svg_window_content_only_shrinks_viewbox_and_strips_bg() {
        // M3 webapp-report defaults: WindowMode::ContentOnly +
        // bg=None. The viewBox should be smaller than the Faithful
        // area_rect (we shrank by chrome margins), no baseline
        // `<rect>` should appear, and the root `<svg>` should
        // carry overflow="hidden" + the imzero-window CSS class.
        use egui::{RawInput, Vec2};

        let ctx = Context::default();
        let win_id = egui::Id::new("report-window");
        let resolver = FontResolver::default();
        let textures: TexturePixelCacheHandle =
            Arc::new(Mutex::new(TexturePixelCache::default()));
        let links: LinkZonesHandle = Arc::new(Mutex::new(Vec::new()));

        let raw = RawInput {
            screen_rect: Some(Rect::from_min_size(Pos2::ZERO, Vec2::new(800.0, 600.0))),
            ..Default::default()
        };
        let show_ui = |ctx: &Context| {
            egui::Window::new("Report")
                .id(win_id)
                .default_pos([10.0, 10.0])
                .show(ctx, |ui| {
                    ui.label("CONTENT");
                });
        };
        for _ in 0..2 {
            let _ = ctx.run(raw.clone(), |ctx| show_ui(ctx));
        }
        let mut faithful: Option<String> = None;
        let mut content_only: Option<String> = None;
        let mut content_only_transparent: Option<String> = None;
        let _ = ctx.run(raw, |ctx| {
            show_ui(ctx);
            faithful = render_svg_window(
                ctx,
                &resolver,
                &textures,
                &links,
                false,
                win_id,
                WindowMode::Faithful,
                Some(VIEWPORT_BG),
            );
            content_only = render_svg_window(
                ctx,
                &resolver,
                &textures,
                &links,
                false,
                win_id,
                WindowMode::ContentOnly,
                Some(Color32::WHITE),
            );
            content_only_transparent = render_svg_window(
                ctx,
                &resolver,
                &textures,
                &links,
                false,
                win_id,
                WindowMode::ContentOnly,
                None,
            );
        });

        let faithful = faithful.expect("faithful rendered");
        let content_only = content_only.expect("content-only rendered");
        let content_only_transparent =
            content_only_transparent.expect("transparent content-only rendered");

        // CSS class hook is present in every Window-scoped export.
        let class_attr = format!("imzero-window-{:x}", win_id.value());
        assert!(
            faithful.contains(&class_attr),
            "faithful SVG missing window class"
        );
        assert!(
            content_only.contains(&class_attr),
            "content-only SVG missing window class"
        );

        // Faithful: opaque dark baseline rect. ContentOnly+white:
        // baseline rect filled with #ffffff. ContentOnly+None: no
        // baseline rect at all.
        assert!(
            faithful.contains("fill=\"#1e1e1e\""),
            "faithful SVG should have dark baseline rect"
        );
        assert!(
            content_only.contains("fill=\"#ffffff\""),
            "content-only+white SVG should have white baseline rect"
        );
        // Be specific: the only `<rect>` candidates for a baseline
        // come from `finish()` and start with `  <rect x=`. The
        // transparent variant should emit none of those.
        // The baseline rect from `finish()` has a distinctive
        // attribute set: just `x y width height fill` — no `rx`,
        // no `fill-opacity`, no `stroke`. Window chrome rects from
        // egui always carry at least one of those extras. Look for
        // a line whose attribute set is exactly the finish() pattern.
        let baseline_rect_present = |svg: &str| -> bool {
            svg.lines().any(|line| {
                line.starts_with("  <rect x=\"")
                    && line.contains(" fill=\"")
                    && line.ends_with("/>")
                    && !line.contains(" rx=\"")
                    && !line.contains(" fill-opacity=\"")
                    && !line.contains(" stroke=\"")
            })
        };
        assert!(
            baseline_rect_present(&content_only),
            "opaque content-only SVG should carry a baseline rect:\n{content_only}"
        );
        assert!(
            !baseline_rect_present(&content_only_transparent),
            "transparent content-only SVG must not emit a baseline rect:\n{content_only_transparent}"
        );

        // ContentOnly should always emit overflow="hidden" on the
        // root <svg> so chrome shapes outside the cropped viewBox
        // get clipped by the viewer.
        assert!(
            content_only.contains("overflow=\"hidden\""),
            "content-only SVG missing overflow=\"hidden\""
        );
        assert!(
            !faithful.contains("overflow=\"hidden\""),
            "faithful SVG should not emit overflow=\"hidden\""
        );

        // ContentOnly viewBox is strictly smaller than Faithful.
        let extract_dims = |svg: &str| -> (f32, f32) {
            let s = svg.split("width=\"").nth(1).unwrap();
            let w: f32 = s.split('"').next().unwrap().parse().unwrap();
            let s = svg.split("height=\"").nth(1).unwrap();
            let h: f32 = s.split('"').next().unwrap().parse().unwrap();
            (w, h)
        };
        let (fw, fh) = extract_dims(&faithful);
        let (cw, ch) = extract_dims(&content_only);
        assert!(
            cw < fw && ch < fh,
            "content-only ({cw}x{ch}) must be strictly smaller than faithful ({fw}x{fh})"
        );
    }

    #[test]
    fn render_svg_window_returns_none_for_missing_window() {
        // No window painted this pass — querying for any id yields None,
        // which `render_svg_to_file` maps to an `Err` so the FFFI
        // plugin logs the failure rather than writing an empty file.
        let ctx = Context::default();
        let resolver = FontResolver::default();
        let textures: TexturePixelCacheHandle =
            Arc::new(Mutex::new(TexturePixelCache::default()));
        let links: LinkZonesHandle = Arc::new(Mutex::new(Vec::new()));
        let raw = egui::RawInput::default();

        let mut result: Option<String> = None;
        let _ = ctx.run(raw, |ctx| {
            result = render_svg_window(
                ctx,
                &resolver,
                &textures,
                &links,
                false,
                egui::Id::new("never-opened"),
                WindowMode::Faithful,
                Some(VIEWPORT_BG),
            );
        });

        assert!(result.is_none());
    }

    #[test]
    fn affine_returns_none_for_collinear_uv() {
        // All four UVs share v=0. Both vertex triples (0,1,2) and
        // (0,1,3) are collinear in UV space; the solver returns None and
        // the caller falls back to the AABB path.
        let v = [
            vtx((10.0, 0.0), (0.0, 0.0)),
            vtx((20.0, 0.0), (0.5, 0.0)),
            vtx((30.0, 0.0), (0.75, 0.0)),
            vtx((40.0, 0.0), (1.0, 0.0)),
        ];
        assert!(solve_textured_quad_affine(&v, 100.0, 100.0, 0, 0).is_none());
    }
}
