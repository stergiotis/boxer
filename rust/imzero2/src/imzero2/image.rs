//! image — RGBA8 pixel-data widget with Go-controlled content version.
//!
//! Sibling of `scrolling_texture` (ADR-0009): same texture-cache plumbing,
//! same hover-rc convention, but no ring-buffer. Pixels arrive complete each
//! upload (Go controls when to upload via `content_version`); when the
//! version matches the cached entry Go ships an empty slice — we reuse the
//! cached `TextureHandle` and don't even visit the GPU.
//!
//! Wire mapping:
//!   - `pixels.is_empty()` + cached entry at `content_version` → draw cached
//!   - `pixels.is_empty()` + no cached entry → no draw (allocates 0×0 rect)
//!   - non-empty `pixels` with `pixels.len() == w * h` → upload + draw
//!   - non-empty `pixels` with mismatched length → log + draw cached if any
//!
//! Hover readout follows ADR-0009 SD11: `(row << 32) | col` in
//! **image-pixel space** (so callers don't have to invert the fit math),
//! sentinel `u64::MAX` when the pointer is outside the widget rect.

#![allow(dead_code)]

use std::collections::HashMap;

use egui::{Color32, ColorImage, Context, Rect, Sense, TextureHandle, TextureOptions, pos2, vec2};

use crate::imzero2::scrolling_texture::{HOVER_RC_NONE, color32_from_rgba_u32, filter_to_options};

// Fit wire values — mirror FitE in the Go IDL.
pub const FIT_NATIVE: u8 = 0;
pub const FIT_FIXED: u8 = 1;
pub const FIT_FILL_RECT: u8 = 2;
pub const FIT_ASPECT_MAX: u8 = 3;

struct Entry {
    tex: TextureHandle,
    w: u32,
    h: u32,
    content_version: u64,
    last_touched_frame: u64,
}

#[derive(Default)]
pub struct ImageCache {
    entries: HashMap<u64, Entry>,
    frame: u64,
    /// Optional CPU mirror of uploaded pixels. Populated on every
    /// `upload` so the SVG exporter can embed image-widget textures as
    /// `<image>` data URLs. Defaults to `None` (no mirroring) for
    /// non-exporting callers and unit tests; production wires it up in
    /// `ImZeroFffi::new` via `attach_texture_cache`.
    texture_cache: Option<crate::imzero2::svgexport::TexturePixelCacheHandle>,
}

impl ImageCache {
    /// Default eviction threshold — matches `ScrollingTextureCache::MAX_AGE_FRAMES`
    /// (~10 s at 60 Hz). Entries not touched within this window get dropped.
    const MAX_AGE_FRAMES: u64 = 600;

    pub fn new() -> Self {
        Self::default()
    }

    /// Wire the SVG-export texture cache so subsequent `upload`s mirror
    /// the RGBA pixels for downstream `<image>` embedding. Called once
    /// from `ImZeroFffi::new`. Safe to leave unset.
    pub fn attach_texture_cache(
        &mut self,
        handle: crate::imzero2::svgexport::TexturePixelCacheHandle,
    ) {
        self.texture_cache = Some(handle);
    }

    /// Advance the per-frame counter and evict stale entries. Called once per
    /// real frame from `interpret_commands_outer`.
    pub fn tick(&mut self) {
        self.frame = self.frame.wrapping_add(1);
        let frame = self.frame;
        self.entries
            .retain(|_, e| frame.saturating_sub(e.last_touched_frame) < Self::MAX_AGE_FRAMES);
    }

    /// Drop the cache entry (and its GPU texture) for `id`. Invoked from the
    /// `imageRelease` opcode.
    pub fn release(&mut self, id: u64) {
        if let Some(entry) = self.entries.remove(&id) {
            if let Some(cache) = &self.texture_cache {
                cache.lock().expect("texture cache poisoned").remove(entry.tex.id());
            }
        }
    }

    /// Re-upload pixels into the cached texture (or allocate a new one).
    /// Called only when `pixels` is non-empty and the length checks out.
    fn upload(
        &mut self,
        ctx: &Context,
        id: u64,
        w: u32,
        h: u32,
        content_version: u64,
        filter_opts: TextureOptions,
        pixels: &[u32],
    ) {
        let color_pixels: Vec<Color32> = pixels.iter().map(|v| color32_from_rgba_u32(*v)).collect();
        let img = ColorImage::new([w as usize, h as usize], color_pixels.clone());
        let tex = ctx.load_texture(format!("image:{id}"), img, filter_opts);

        // Mirror to the SVG-export pixel cache when one is attached. Done
        // before inserting the entry so the cache always sees the same
        // (TextureId, pixels) tuple that production paint will use.
        // Read directly from the source u32 stream (straight RGBA) rather
        // than from Color32 (premultiplied) so the embedded PNG matches
        // the user-visible image even at partial alpha.
        if let Some(cache) = &self.texture_cache {
            let rgba: Vec<u8> = pixels
                .iter()
                .flat_map(|&v| {
                    [
                        ((v >> 24) & 0xff) as u8,
                        ((v >> 16) & 0xff) as u8,
                        ((v >> 8) & 0xff) as u8,
                        (v & 0xff) as u8,
                    ]
                })
                .collect();
            let nearest = filter_opts.magnification == egui::TextureFilter::Nearest;
            cache.lock().expect("texture cache poisoned").insert(tex.id(), w, h, rgba, nearest);
        }

        self.entries.insert(
            id,
            Entry {
                tex,
                w,
                h,
                content_version,
                last_touched_frame: self.frame,
            },
        );
    }

    /// Upload-if-needed and return the cached texture id **without drawing**.
    /// Mirrors `show`'s cache logic for callers that paint the texture
    /// themselves — e.g. the walkers `mapRaster` overlay, which projects the
    /// texture onto a geographic quad rather than the ui cursor. Returns
    /// `None` when there's nothing to show (no pixels and no cached entry).
    #[allow(clippy::too_many_arguments)]
    pub fn ensure(
        &mut self,
        ctx: &Context,
        id: u64,
        w: u32,
        h: u32,
        content_version: u64,
        filter_opts: TextureOptions,
        pixels: &[u32],
    ) -> Option<egui::TextureId> {
        let cached_version = self.entries.get(&id).map(|e| e.content_version);
        let cached_shape = self.entries.get(&id).map(|e| (e.w, e.h));
        let needs_upload = match (cached_version, cached_shape) {
            (Some(cv), Some(cs)) => cv != content_version || cs != (w, h),
            _ => true,
        };
        if needs_upload && !pixels.is_empty() {
            let expected = (w as usize).saturating_mul(h as usize);
            if pixels.len() == expected {
                self.upload(ctx, id, w, h, content_version, filter_opts, pixels);
            } else {
                tracing::warn!(
                    id = id,
                    w = w,
                    h = h,
                    got = pixels.len(),
                    expected = expected,
                    "image ensure: pixels length mismatch; skipping upload"
                );
            }
        }
        let entry = self.entries.get_mut(&id)?;
        entry.last_touched_frame = self.frame;
        Some(entry.tex.id())
    }

    /// Compute the screen-space size for the allocated rect given the fit mode
    /// and native texture dims. `fixed_w/fixed_h` are inputs for FIXED and
    /// ASPECT_MAX modes; `available` is `ui.available_size()`, used by
    /// FILL_RECT and as the ASPECT_MAX bounding box when a fixed dimension is 0.
    fn compute_size(
        fit: u8,
        native_w: u32,
        native_h: u32,
        fixed_w: u32,
        fixed_h: u32,
        available: egui::Vec2,
    ) -> egui::Vec2 {
        match fit {
            FIT_FIXED => vec2(fixed_w as f32, fixed_h as f32),
            FIT_FILL_RECT => available,
            FIT_ASPECT_MAX => {
                if native_w == 0 || native_h == 0 {
                    return vec2(0.0, 0.0);
                }
                // A zero fixed_w / fixed_h means "fit to ui.available_size()":
                // scale aspect-preserved into the local available rect. This
                // lets a caller fill its layout slot without shipping a box
                // size from the host, whose only available-size channel is a
                // single global register that reads wrong when several windows
                // render in one frame.
                let fw = if fixed_w == 0 {
                    available.x
                } else {
                    fixed_w as f32
                };
                let fh = if fixed_h == 0 {
                    available.y
                } else {
                    fixed_h as f32
                };
                if fw <= 0.0 || fh <= 0.0 {
                    return vec2(0.0, 0.0);
                }
                let nw = native_w as f32;
                let nh = native_h as f32;
                let s = (fw / nw).min(fh / nh);
                vec2(nw * s, nh * s)
            }
            // FIT_NATIVE and unknown fall through to native pixel size.
            _ => vec2(native_w as f32, native_h as f32),
        }
    }

    /// Show an image at the current ui cursor.
    ///
    /// Returns `(Response, hover_rc, starved)`. `hover_rc` packs `(row, col)`
    /// in **image-pixel space** (`HOVER_RC_NONE` if the pointer is outside
    /// the allocated widget rect). The Response is always returned — even
    /// when nothing is drawn — so the caller can populate r7 flags uniformly.
    /// `starved` is true when nothing could be drawn because there is no
    /// usable cache entry AND the payload carried no pixels to build one —
    /// the state a send-once uploader lands in when its full send went into
    /// a skipped region (hidden dock tab) or the idle LRU evicted the entry.
    /// Callers report it on the starved-textures register so the sender
    /// re-ships (fetchR22StarvedTextures).
    #[allow(clippy::too_many_arguments)]
    pub fn show(
        &mut self,
        ui: &mut egui::Ui,
        ctx: &Context,
        id: u64,
        w: u32,
        h: u32,
        content_version: u64,
        fit: u8,
        fixed_w: u32,
        fixed_h: u32,
        filter: u8,
        tint_rgba: u32,
        pixels: &[u32],
    ) -> (egui::Response, u64, bool) {
        let filter_opts = filter_to_options(filter);

        // Decide whether to upload. Three cases:
        //   1. cache miss → must upload from `pixels`
        //   2. cache hit, version unchanged → reuse (Go is supposed to send
        //      empty pixels in this case, but we tolerate either)
        //   3. cache hit, version changed → upload to refresh
        let cached_version = self.entries.get(&id).map(|e| e.content_version);
        let cached_shape = self.entries.get(&id).map(|e| (e.w, e.h));
        let needs_upload = match (cached_version, cached_shape) {
            (Some(cv), Some(cs)) => cv != content_version || cs != (w, h),
            _ => true,
        };

        if needs_upload {
            if pixels.is_empty() {
                // No cached entry (or stale) and Go didn't ship pixels — can't
                // draw. Allocate 0×0 so the widget id still gets a Response,
                // and report starvation so the sender re-ships. A stale-entry
                // hit (version moved, empty pixels) still draws the old frame
                // below rather than starving.
                if let Some(e) = self.entries.get_mut(&id) {
                    e.last_touched_frame = self.frame;
                    // fall through: draw the stale entry
                } else {
                    let (_rect, resp) = ui
                        .allocate_exact_size(vec2(0.0, 0.0), Sense::hover().union(Sense::click()));
                    return (resp, HOVER_RC_NONE, true);
                }
            } else {
                let expected = (w as usize).saturating_mul(h as usize);
                if pixels.len() != expected {
                    tracing::warn!(
                        id = id,
                        w = w,
                        h = h,
                        got = pixels.len(),
                        expected = expected,
                        "image: pixels length mismatch; skipping upload"
                    );
                    // Fall through to draw whatever was cached (may be stale or
                    // absent — handled below).
                } else {
                    self.upload(ctx, id, w, h, content_version, filter_opts, pixels);
                }
            }
        }

        // Either we just uploaded, or we're reusing. If still no entry, draw
        // a placeholder (0×0).
        let entry = match self.entries.get_mut(&id) {
            Some(e) => e,
            None => {
                // Reachable only via the length-mismatch fall-through with no
                // prior entry — a resend can heal it, so report starved too.
                let (_rect, resp) =
                    ui.allocate_exact_size(vec2(0.0, 0.0), Sense::hover().union(Sense::click()));
                return (resp, HOVER_RC_NONE, true);
            }
        };
        entry.last_touched_frame = self.frame;

        let native_w = entry.w;
        let native_h = entry.h;
        let tex_id = entry.tex.id();

        let size = Self::compute_size(
            fit,
            native_w,
            native_h,
            fixed_w,
            fixed_h,
            ui.available_size(),
        );
        let sense = Sense::hover().union(Sense::click());
        let (rect, resp) = ui.allocate_exact_size(size, sense);

        let painter = ui.painter_at(rect);
        let tint = if tint_rgba == 0xFFFFFFFFu32 {
            Color32::WHITE
        } else {
            color32_from_rgba_u32(tint_rgba)
        };
        let full_uv = Rect::from_min_max(pos2(0.0, 0.0), pos2(1.0, 1.0));
        if rect.width() > 0.0 && rect.height() > 0.0 {
            painter.image(tex_id, rect, full_uv, tint);
        }

        // Hover readout in image-pixel space — invert the screen→pixel map
        // regardless of fit mode. row = pixel-y, col = pixel-x.
        let hover_rc = if let Some(hp) = resp.hover_pos() {
            if rect.width() > 0.0 && rect.height() > 0.0 && native_w > 0 && native_h > 0 {
                let lx = (hp.x - rect.min.x).clamp(0.0, rect.width());
                let ly = (hp.y - rect.min.y).clamp(0.0, rect.height());
                let col = ((lx / rect.width()) * native_w as f32) as u32;
                let row = ((ly / rect.height()) * native_h as f32) as u32;
                let col = col.min(native_w.saturating_sub(1));
                let row = row.min(native_h.saturating_sub(1));
                ((row as u64) << 32) | (col as u64)
            } else {
                HOVER_RC_NONE
            }
        } else {
            HOVER_RC_NONE
        };

        (resp, hover_rc, false)
    }
}
