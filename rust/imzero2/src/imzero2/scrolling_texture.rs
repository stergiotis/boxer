//! scrollingTexture — ring-buffer pixel widget with caller-owned head cursor.
//!
//! See `doc/adr/0009-imzero2-scrolling-texture-widget.md` for design rationale.
//!
//! Milestone 2.5: all four orientations, hover readout packed into r9_u64,
//! click reported via r10, per-frame tick() wired from the interpreter.
//! `ScrollLeft` uses `painter.image` with axis-aligned UV rects (forward
//! ring walk in both screen-x and texture-u). The other three orientations
//! require a walk direction that `painter.image`'s UV rect can't express,
//! so they use `egui::Mesh` with explicit per-vertex UVs: `ScrollRight` is
//! a horizontal backward walk, `ScrollUp` is a vertical forward walk, and
//! `ScrollDown` is a vertical backward walk. All four produce a smooth
//! monotonic age gradient across the widget rect.

#![allow(dead_code)]

use std::collections::HashMap;

use egui::{
    Color32, ColorImage, Context, Mesh, Pos2, Rect, Sense, TextureHandle, TextureId,
    TextureOptions, pos2, vec2,
};

// Orientation wire values — mirror OrientationE in the Go IDL.
pub const ORIENTATION_SCROLL_LEFT: u8 = 0;
pub const ORIENTATION_SCROLL_RIGHT: u8 = 1;
pub const ORIENTATION_SCROLL_UP: u8 = 2;
pub const ORIENTATION_SCROLL_DOWN: u8 = 3;

// Filter wire values — mirror FilterE in the Go IDL.
// Maps to egui::TextureOptions::{NEAREST, LINEAR} at upload time.
pub const FILTER_NEAREST: u8 = 0;
pub const FILTER_LINEAR: u8 = 1;

/// Packed "no hover" sentinel for `ScrollingTextureResponse::hover_rc`.
/// (row=u32::MAX, col=u32::MAX) — unambiguous because real (row, col) indices
/// are bounded by height_slots/width_slots, both u32 but always < u32::MAX in
/// any realistic widget.
pub const HOVER_RC_NONE: u64 = u64::MAX;

/// Per-call response carried back from `push_and_draw`. The interpreter's
/// apply-code snippet forwards these to the r9_u64 and r10 registers; Go
/// reads them via the standard databinding / Fetch* path (SD11, SD12).
#[derive(Clone, Copy, Debug)]
pub struct ScrollingTextureResponse {
    /// `(row as u64) << 32 | col as u64` in data-index space, or
    /// `HOVER_RC_NONE` if the pointer is outside the widget rect.
    pub hover_rc: u64,
    /// True on the frame egui recognises a primary click on the widget rect.
    pub clicked: bool,
}

impl ScrollingTextureResponse {
    const fn none() -> Self {
        Self {
            hover_rc: HOVER_RC_NONE,
            clicked: false,
        }
    }
}

pub fn color32_from_rgba_u32(v: u32) -> Color32 {
    let r = ((v >> 24) & 0xff) as u8;
    let g = ((v >> 16) & 0xff) as u8;
    let b = ((v >> 8) & 0xff) as u8;
    let a = (v & 0xff) as u8;
    Color32::from_rgba_unmultiplied(r, g, b, a)
}

pub fn filter_to_options(filter: u8) -> TextureOptions {
    if filter == FILTER_LINEAR {
        TextureOptions::LINEAR
    } else {
        TextureOptions::NEAREST
    }
}

/// Draw a textured quad on `painter` with explicit per-corner UV mapping.
/// Corners are given in order [TL, TR, BR, BL]; used by the vertical
/// orientations to express a 90° rotation that `painter.image` cannot.
fn draw_textured_quad(
    painter: &egui::Painter,
    tex_id: TextureId,
    screen: [Pos2; 4],
    uv: [Pos2; 4],
    tint: Color32,
) {
    let mut mesh = Mesh::with_texture(tex_id);
    for i in 0..4 {
        mesh.vertices.push(egui::epaint::Vertex {
            pos: screen[i],
            uv: uv[i],
            color: tint,
        });
    }
    mesh.indices.extend_from_slice(&[0, 1, 2, 0, 2, 3]);
    painter.add(mesh);
}

struct Entry {
    tex: TextureHandle,
    width_slots: u32,
    height_slots: u32,
    last_touched_frame: u64,
    /// CPU-side mirror of the GPU texture pixels (straight RGBA, row-
    /// major, top-left origin). Maintained alongside `set_partial`
    /// updates so the SVG-export cache can hand the visitor a complete
    /// 2D image even though the GPU side only sees one-column patches.
    rgba: Vec<u8>,
}

#[derive(Default)]
pub struct ScrollingTextureCache {
    entries: HashMap<u64, Entry>,
    frame: u64,
    texture_cache: Option<crate::imzero2::svgexport::TexturePixelCacheHandle>,
}

impl ScrollingTextureCache {
    /// Default eviction threshold: entry not touched for this many frames → dropped.
    /// ~10 s at 60 Hz.
    const MAX_AGE_FRAMES: u64 = 600;

    pub fn new() -> Self {
        Self::default()
    }

    /// Wire the SVG-export texture cache so column-by-column updates
    /// also mirror their pixels for downstream `<image>` embedding.
    pub fn attach_texture_cache(
        &mut self,
        handle: crate::imzero2::svgexport::TexturePixelCacheHandle,
    ) {
        self.texture_cache = Some(handle);
    }

    /// Advance the frame counter and evict stale entries. Called once per
    /// frame by the interpreter's `interpret_commands_outer`; dropped entries
    /// release their `TextureHandle` at the next frame boundary (egui defers
    /// GPU destruction to the end of the frame).
    pub fn tick(&mut self) {
        self.frame = self.frame.wrapping_add(1);
        let frame = self.frame;
        self.entries
            .retain(|_, e| frame.saturating_sub(e.last_touched_frame) < Self::MAX_AGE_FRAMES);
    }

    /// Drop the cache entry (and its GPU texture) for `id`. Invoked from the
    /// `scrollingTextureRelease` opcode.
    pub fn release(&mut self, id: u64) {
        if let Some(entry) = self.entries.remove(&id) {
            if let Some(cache) = &self.texture_cache {
                cache.lock().expect("texture cache poisoned").remove(entry.tex.id());
            }
        }
    }

    /// Allocate or reuse a cache entry with the given shape. Rebuilds if
    /// the shape changed (width/height differ from the stored entry).
    fn ensure_entry(
        &mut self,
        ctx: &Context,
        id: u64,
        width_slots: u32,
        height_slots: u32,
        filter_opts: TextureOptions,
    ) {
        let needs_new = match self.entries.get(&id) {
            None => true,
            Some(e) => e.width_slots != width_slots || e.height_slots != height_slots,
        };
        if needs_new {
            let img = ColorImage::filled(
                [width_slots as usize, height_slots as usize],
                Color32::TRANSPARENT,
            );
            let tex = ctx.load_texture(format!("scrollingTexture:{id}"), img, filter_opts);
            let total = (width_slots as usize) * (height_slots as usize) * 4;
            let rgba = vec![0u8; total]; // matches Color32::TRANSPARENT (all zeros)
            let nearest = filter_opts.magnification == egui::TextureFilter::Nearest;
            // Seed the SVG-export cache with the transparent initial state
            // so a tour that captures the very first frame doesn't lose
            // the texture entry.
            if let Some(cache) = &self.texture_cache {
                cache.lock().expect("texture cache poisoned").insert(
                    tex.id(),
                    width_slots,
                    height_slots,
                    rgba.clone(),
                    nearest,
                );
            }
            self.entries.insert(
                id,
                Entry {
                    tex,
                    width_slots,
                    height_slots,
                    last_touched_frame: self.frame,
                    rgba,
                },
            );
        }
    }

    /// Push `new_count` RGBA columns starting at `head` (mod `width_slots`)
    /// and draw the ring-buffer view inside `ui`. The caller owns `head`;
    /// this function advances nothing on its own. Returns hover (row, col)
    /// packed into u64 plus a click flag; interpreter forwards to r9/r10.
    ///
    /// `display_width_px` and `display_height_px` override the rendered
    /// rect size; 0.0 means "use slot count" (1 slot = 1 px, the
    /// historical default). When the display size differs from slot
    /// count the texture is stretched by `painter.image`'s sampler;
    /// hover coordinates are scaled back to slot units so the (row, col)
    /// readout still names ring positions, not pixels.
    #[allow(clippy::too_many_arguments)]
    pub fn push_and_draw(
        &mut self,
        ui: &mut egui::Ui,
        ctx: &Context,
        id: u64,
        width_slots: u32,
        height_slots: u32,
        orientation: u8,
        filter: u8,
        head: u32,
        new_count: u32,
        new_columns: &[u32],
        display_width_px: f32,
        display_height_px: f32,
    ) -> ScrollingTextureResponse {
        if width_slots == 0 || height_slots == 0 {
            return ScrollingTextureResponse::none();
        }

        let filter_opts = filter_to_options(filter);
        let expected_len = (new_count as usize).saturating_mul(height_slots as usize);
        let payload_valid = new_columns.len() == expected_len;
        if !payload_valid {
            tracing::warn!(
                id = id,
                new_count = new_count,
                height_slots = height_slots,
                got = new_columns.len(),
                expected = expected_len,
                "scrollingTexture: new_columns length mismatch; skipping column upload"
            );
        }

        self.ensure_entry(ctx, id, width_slots, height_slots, filter_opts);
        let entry = self.entries.get_mut(&id).expect("entry just ensured above");
        entry.last_touched_frame = self.frame;

        if payload_valid && new_count > 0 {
            let h = height_slots as usize;
            let w = width_slots as usize;
            for i in 0..(new_count as usize) {
                let col_x = ((head as usize) + i) % w;
                let base = i * h;
                let col_pixels: Vec<Color32> =
                    new_columns[base..base + h].iter().map(|v| color32_from_rgba_u32(*v)).collect();
                let img = ColorImage::new([1, h], col_pixels);
                entry.tex.set_partial([col_x, 0], img, filter_opts);

                // Mirror to the CPU-side rgba buffer so the SVG visitor
                // sees the full 2D state. Source `new_columns` carries
                // straight-alpha RGBA-packed u32 (see
                // `color32_from_rgba_u32`); write straight bytes
                // directly so the embedded PNG matches what the user
                // sees on screen.
                for y in 0..h {
                    let v = new_columns[base + y];
                    let dst = (y * w + col_x) * 4;
                    entry.rgba[dst] = ((v >> 24) & 0xff) as u8;
                    entry.rgba[dst + 1] = ((v >> 16) & 0xff) as u8;
                    entry.rgba[dst + 2] = ((v >> 8) & 0xff) as u8;
                    entry.rgba[dst + 3] = (v & 0xff) as u8;
                }
            }
            if let Some(cache) = &self.texture_cache {
                let nearest = filter_opts.magnification == egui::TextureFilter::Nearest;
                cache.lock().expect("texture cache poisoned").insert(
                    entry.tex.id(),
                    entry.width_slots,
                    entry.height_slots,
                    entry.rgba.clone(),
                    nearest,
                );
            }
        }

        let tex_id = entry.tex.id();
        let draw_head = (head + new_count) % width_slots;

        // Slot-count baseline for the rect; vertical orientations rotate
        // (width_slots is the time axis, height_slots is the bin axis).
        let (base_w, base_h) = match orientation {
            ORIENTATION_SCROLL_UP | ORIENTATION_SCROLL_DOWN => {
                (height_slots as f32, width_slots as f32)
            }
            _ => (width_slots as f32, height_slots as f32),
        };
        // Display overrides: 0 keeps the slot-count default; >0 stretches
        // the rect (and the texture sample) along that axis. Hover
        // coordinates are converted back to slot units below so the
        // returned (row, col) still names ring positions, not pixels.
        let disp_w = if display_width_px > 0.0 {
            display_width_px
        } else {
            base_w
        };
        let disp_h = if display_height_px > 0.0 {
            display_height_px
        } else {
            base_h
        };
        let screen_size = vec2(disp_w, disp_h);
        let sense = Sense::hover().union(Sense::click());
        let (rect, resp) = ui.allocate_exact_size(screen_size, sense);
        let painter = ui.painter_at(rect);
        let tint = Color32::WHITE;
        let rect_w = rect.width();
        let rect_h = rect.height();
        let w = width_slots as f32; // time-axis slot count, used for ring math
        let split = draw_head as f32 / w;

        match orientation {
            ORIENTATION_SCROLL_LEFT => {
                // texture [draw_head..W] → screen left portion (oldest on left)
                // texture [0..draw_head]  → screen right portion (newest on right)
                if draw_head < width_slots {
                    let left_frac = (width_slots - draw_head) as f32 / w;
                    let r1 = Rect::from_min_size(rect.min, vec2(left_frac * rect_w, rect_h));
                    let uv1 = Rect::from_min_max(pos2(split, 0.0), pos2(1.0, 1.0));
                    painter.image(tex_id, r1, uv1, tint);
                }
                if draw_head > 0 {
                    let right_frac = draw_head as f32 / w;
                    let r2 = Rect::from_min_size(
                        rect.min + vec2((1.0 - right_frac) * rect_w, 0.0),
                        vec2(right_frac * rect_w, rect_h),
                    );
                    let uv2 = Rect::from_min_max(pos2(0.0, 0.0), pos2(split, 1.0));
                    painter.image(tex_id, r2, uv2, tint);
                }
            }
            ORIENTATION_SCROLL_RIGHT => {
                // Mirror of ScrollLeft: newest on left, oldest on right, with a
                // smooth age gradient. Walking screen-x left-to-right walks
                // the ring backwards starting at draw_head - 1. `painter.image`
                // can't express that (UV rects are forward-only), so each
                // sub-draw uses a Mesh with a flipped u axis.
                if draw_head > 0 {
                    // Sub-draw 1 (screen left, newer): texture u in [0, split].
                    let left_frac = draw_head as f32 / w;
                    let r1 = Rect::from_min_size(rect.min, vec2(left_frac * rect_w, rect_h));
                    draw_textured_quad(
                        &painter,
                        tex_id,
                        [
                            r1.left_top(),
                            r1.right_top(),
                            r1.right_bottom(),
                            r1.left_bottom(),
                        ],
                        [
                            pos2(split, 0.0),
                            pos2(0.0, 0.0),
                            pos2(0.0, 1.0),
                            pos2(split, 1.0),
                        ],
                        tint,
                    );
                }
                if draw_head < width_slots {
                    // Sub-draw 2 (screen right, older): texture u in [split, 1].
                    let left_frac = draw_head as f32 / w;
                    let right_frac = (width_slots - draw_head) as f32 / w;
                    let r2 = Rect::from_min_size(
                        rect.min + vec2(left_frac * rect_w, 0.0),
                        vec2(right_frac * rect_w, rect_h),
                    );
                    draw_textured_quad(
                        &painter,
                        tex_id,
                        [
                            r2.left_top(),
                            r2.right_top(),
                            r2.right_bottom(),
                            r2.left_bottom(),
                        ],
                        [
                            pos2(1.0, 0.0),
                            pos2(split, 0.0),
                            pos2(split, 1.0),
                            pos2(1.0, 1.0),
                        ],
                        tint,
                    );
                }
            }
            ORIENTATION_SCROLL_UP => {
                // Newest at bottom, smooth gradient top=oldest → bottom=newest.
                // Screen rect is H wide × W tall. screen-x→texture-v (bin axis),
                // screen-y→texture-u (ring axis, forward walk starting at
                // draw_head).
                // Part 1 (screen top, oldest): texture u in [split, 1].
                if draw_head < width_slots {
                    let top_frac = (width_slots - draw_head) as f32 / w;
                    let r1 = Rect::from_min_size(rect.min, vec2(rect_w, top_frac * rect_h));
                    draw_textured_quad(
                        &painter,
                        tex_id,
                        [
                            r1.left_top(),
                            r1.right_top(),
                            r1.right_bottom(),
                            r1.left_bottom(),
                        ],
                        [
                            pos2(split, 0.0),
                            pos2(split, 1.0),
                            pos2(1.0, 1.0),
                            pos2(1.0, 0.0),
                        ],
                        tint,
                    );
                }
                // Part 2 (screen bottom, newer): texture u in [0, split].
                if draw_head > 0 {
                    let top_frac = (width_slots - draw_head) as f32 / w;
                    let bot_frac = draw_head as f32 / w;
                    let r2 = Rect::from_min_size(
                        rect.min + vec2(0.0, top_frac * rect_h),
                        vec2(rect_w, bot_frac * rect_h),
                    );
                    draw_textured_quad(
                        &painter,
                        tex_id,
                        [
                            r2.left_top(),
                            r2.right_top(),
                            r2.right_bottom(),
                            r2.left_bottom(),
                        ],
                        [
                            pos2(0.0, 0.0),
                            pos2(0.0, 1.0),
                            pos2(split, 1.0),
                            pos2(split, 0.0),
                        ],
                        tint,
                    );
                }
            }
            ORIENTATION_SCROLL_DOWN => {
                // Newest at top, smooth gradient top=newest → bottom=oldest.
                // Screen-y walks the ring backwards starting at draw_head - 1.
                // Part 1 (screen top, newer): texture u in [0, split], u axis
                // flipped so screen-y=0 samples u=split (newest).
                if draw_head > 0 {
                    let top_frac = draw_head as f32 / w;
                    let r1 = Rect::from_min_size(rect.min, vec2(rect_w, top_frac * rect_h));
                    draw_textured_quad(
                        &painter,
                        tex_id,
                        [
                            r1.left_top(),
                            r1.right_top(),
                            r1.right_bottom(),
                            r1.left_bottom(),
                        ],
                        [
                            pos2(split, 0.0),
                            pos2(split, 1.0),
                            pos2(0.0, 1.0),
                            pos2(0.0, 0.0),
                        ],
                        tint,
                    );
                }
                // Part 2 (screen bottom, older): texture u in [split, 1],
                // u axis flipped so screen-y=bottom samples u=split (oldest).
                if draw_head < width_slots {
                    let top_frac = draw_head as f32 / w;
                    let bot_frac = (width_slots - draw_head) as f32 / w;
                    let r2 = Rect::from_min_size(
                        rect.min + vec2(0.0, top_frac * rect_h),
                        vec2(rect_w, bot_frac * rect_h),
                    );
                    draw_textured_quad(
                        &painter,
                        tex_id,
                        [
                            r2.left_top(),
                            r2.right_top(),
                            r2.right_bottom(),
                            r2.left_bottom(),
                        ],
                        [
                            pos2(1.0, 0.0),
                            pos2(1.0, 1.0),
                            pos2(split, 1.0),
                            pos2(split, 0.0),
                        ],
                        tint,
                    );
                }
            }
            _ => {
                tracing::warn!(
                    orientation = orientation,
                    "scrollingTexture: unknown orientation value; drawing unrotated texture"
                );
                painter.image(
                    tex_id,
                    rect,
                    Rect::from_min_max(pos2(0.0, 0.0), pos2(1.0, 1.0)),
                    tint,
                );
            }
        }

        // Hover readout (see SD11): `row` is always the screen-y index,
        // `col` is always the screen-x index. For horizontal orientations
        // this maps to (bin, ring_position); for vertical orientations the
        // screen axes are rotated relative to the ring layout, so the
        // mapping swaps to (ring_position, bin). Clamp bounds follow the
        // allocated screen rect — screen-y length for row, screen-x for col.
        let hover_rc = if let Some(hp) = resp.hover_pos() {
            let lx_px = (hp.x - rect.min.x).max(0.0);
            let ly_px = (hp.y - rect.min.y).max(0.0);
            // Convert screen pixels to slot units along each axis. base_w
            // / base_h are the slot-count baselines (already accounting
            // for orientation rotation); rect_w/rect_h are the actual
            // rendered pixels. When display = slot count both ratios are
            // 1.0 and this collapses to the historical pixel→slot mapping.
            let lx = if rect_w > 0.0 {
                lx_px * base_w / rect_w
            } else {
                lx_px
            };
            let ly = if rect_h > 0.0 {
                ly_px * base_h / rect_h
            } else {
                ly_px
            };
            let (row, col) = match orientation {
                ORIENTATION_SCROLL_LEFT => {
                    let col = ((draw_head as f32 + lx).rem_euclid(w)) as u32;
                    let row = ly as u32;
                    (row, col)
                }
                ORIENTATION_SCROLL_RIGHT => {
                    let col = ((draw_head as f32 + w - 1.0 - lx).rem_euclid(w)) as u32;
                    let row = ly as u32;
                    (row, col)
                }
                ORIENTATION_SCROLL_UP => {
                    // Screen-y → ring_pos (forward walk); screen-x → bin.
                    let row = ((draw_head as f32 + ly).rem_euclid(w)) as u32;
                    let col = lx as u32;
                    (row, col)
                }
                ORIENTATION_SCROLL_DOWN => {
                    // Screen-y → ring_pos (backward walk); screen-x → bin.
                    let row = ((draw_head as f32 + w - 1.0 - ly).rem_euclid(w)) as u32;
                    let col = lx as u32;
                    (row, col)
                }
                _ => (0u32, 0u32),
            };
            // Clamp to slot-count bounds so callers see a stable ring
            // (row, col) regardless of display size. base_w/base_h are
            // the slot-count baselines for the current orientation.
            let row_max = (base_h as u32).saturating_sub(1);
            let col_max = (base_w as u32).saturating_sub(1);
            let row = row.min(row_max);
            let col = col.min(col_max);
            ((row as u64) << 32) | (col as u64)
        } else {
            HOVER_RC_NONE
        };

        ScrollingTextureResponse {
            hover_rc,
            clicked: resp.clicked(),
        }
    }
}
