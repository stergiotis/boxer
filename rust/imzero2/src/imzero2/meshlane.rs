//! Draw-stream lane wire format (ADR-0128 SD1/SD2).
//!
//! Serializes egui's tessellated [`egui::ClippedPrimitive`]s into the
//! quantized mesh format the browser painter consumes, and mirrors
//! [`egui::TexturesDelta`] into a CPU-side texture store so a joining viewer
//! can bootstrap from the current state (the keyframe analogue — no GOP).
//!
//! Wire framing rides the carrier's one-byte prefix ([`PREFIX_MESH`]); the
//! byte after it selects the message:
//!
//! - `1` — mesh frame: `ppp f32, w_px f32, h_px f32, count u32,
//!   count×hash u64, n_bodies u32, bodies (hash u64, len u32, bytes)…`.
//!   The hash list is the frame's draw order; bodies accompany only hashes
//!   this connection has not been sent (content-addressed — a body is
//!   immutable once named, so the viewer caches it as static GPU buffers).
//! - `2` — texture update: `key u32, full_w u32, full_h u32, x u32, y u32,
//!   w u32, h u32, rgba bytes` (whole when `x=y=0, w/h = full`). Keys use
//!   [`fold_key`]: user-texture ids fold their marker into bit 31, matching
//!   the `u32` texture field of mesh bodies.
//!
//! Mesh body layout: `clip u16×4` (1/8 px), `tex u32`, `n_verts u32`,
//! `idx_width u8` (2|4), `n_idx u32`, vertices n×(`pos u16×2` @ 1/8 px,
//! `uv u16norm×2`, `rgba u32` premultiplied sRGB), indices. Positions are in
//! viewer pixels (`points × pixels_per_point`); u16 at 1/8 px caps the frame
//! at 8191 px per axis, comfortably past the 4K-at-DPR-2 envelope.

use crate::imzero2::inputproto::PREFIX_MESH;
use std::collections::HashMap;
use std::hash::Hasher as _;

pub const MESH_MSG_FRAME: u8 = 1;
pub const MESH_MSG_TEXTURE: u8 = 2;

/// Collapse egui's two-sided texture id into one store key. User-textures set
/// the top bit so they can never collide with managed ids.
pub fn tex_key(id: egui::epaint::TextureId) -> u64 {
    match id {
        egui::epaint::TextureId::Managed(m) => m,
        egui::epaint::TextureId::User(u) => (1u64 << 63) | u,
    }
}

/// The wire form of a texture key: the user/managed marker folds from bit 63
/// into bit 31. Collision-free while managed ids and user ids stay below
/// 2^31 — comfortably true for egui's monotonic managed ids, and no widget
/// in this tree emits `TextureId::User` at all.
fn fold_key(k: u64) -> u32 {
    (k as u32 & 0x7fff_ffff) | (((k >> 63) as u32) << 31)
}

/// One frame's serialized mesh bodies: the concatenated body bytes, each
/// body's range within them, and each body's content hash (the wire name).
pub struct SerializedFrame {
    pub scratch: Vec<u8>,
    pub ranges: Vec<(usize, usize)>,
    pub hashes: Vec<u64>,
    /// `Primitive::Callback` count — content the lane cannot carry (ADR-0128
    /// SD3 sentinel; zero across the shipped apps, measured).
    pub callbacks: usize,
}

/// Serialize tessellated primitives into wire bodies. `ppp` converts egui
/// points into viewer pixels before quantization.
pub fn serialize(clipped: &[egui::ClippedPrimitive], ppp: f32) -> SerializedFrame {
    let q = |v: f32| -> u16 { (v * ppp * 8.0).round().clamp(0.0, 65535.0) as u16 };
    let quv = |v: f32| -> u16 { (v * 65535.0).round().clamp(0.0, 65535.0) as u16 };
    let mut out = SerializedFrame {
        scratch: Vec::new(),
        ranges: Vec::new(),
        hashes: Vec::new(),
        callbacks: 0,
    };
    for cp in clipped {
        let mesh = match &cp.primitive {
            egui::epaint::Primitive::Mesh(m) => m,
            egui::epaint::Primitive::Callback(_) => {
                out.callbacks += 1;
                continue;
            }
        };
        let start = out.scratch.len();
        for v in [
            cp.clip_rect.min.x,
            cp.clip_rect.min.y,
            cp.clip_rect.max.x,
            cp.clip_rect.max.y,
        ] {
            out.scratch.extend_from_slice(&q(v).to_le_bytes());
        }
        out.scratch.extend_from_slice(&fold_key(tex_key(mesh.texture_id)).to_le_bytes());
        out.scratch.extend_from_slice(&(mesh.vertices.len() as u32).to_le_bytes());
        let wide = mesh.vertices.len() > u16::MAX as usize;
        out.scratch.push(if wide { 4 } else { 2 });
        out.scratch.extend_from_slice(&(mesh.indices.len() as u32).to_le_bytes());
        for v in &mesh.vertices {
            out.scratch.extend_from_slice(&q(v.pos.x).to_le_bytes());
            out.scratch.extend_from_slice(&q(v.pos.y).to_le_bytes());
            out.scratch.extend_from_slice(&quv(v.uv.x).to_le_bytes());
            out.scratch.extend_from_slice(&quv(v.uv.y).to_le_bytes());
            out.scratch.extend_from_slice(&v.color.to_array());
        }
        if wide {
            for i in &mesh.indices {
                out.scratch.extend_from_slice(&i.to_le_bytes());
            }
        } else {
            for i in &mesh.indices {
                out.scratch.extend_from_slice(&(*i as u16).to_le_bytes());
            }
        }
        let mut h = std::collections::hash_map::DefaultHasher::new();
        h.write(&out.scratch[start..]);
        out.hashes.push(h.finish());
        out.ranges.push((start, out.scratch.len()));
    }
    out
}

/// Build one framed mesh-frame message for a connection: the full draw order
/// plus the bodies at `missing` (indices into `frame.ranges`).
pub fn frame_message(
    ppp: f32,
    w_px: f32,
    h_px: f32,
    frame: &SerializedFrame,
    missing: &[usize],
) -> Vec<u8> {
    let body_bytes: usize = missing.iter().map(|&i| frame.ranges[i].1 - frame.ranges[i].0).sum();
    let mut m = Vec::with_capacity(22 + frame.hashes.len() * 8 + missing.len() * 12 + body_bytes);
    m.push(PREFIX_MESH);
    m.push(MESH_MSG_FRAME);
    m.extend_from_slice(&ppp.to_le_bytes());
    m.extend_from_slice(&w_px.to_le_bytes());
    m.extend_from_slice(&h_px.to_le_bytes());
    m.extend_from_slice(&(frame.hashes.len() as u32).to_le_bytes());
    for h in &frame.hashes {
        m.extend_from_slice(&h.to_le_bytes());
    }
    m.extend_from_slice(&(missing.len() as u32).to_le_bytes());
    for &i in missing {
        let (s, e) = frame.ranges[i];
        m.extend_from_slice(&frame.hashes[i].to_le_bytes());
        m.extend_from_slice(&((e - s) as u32).to_le_bytes());
        m.extend_from_slice(&frame.scratch[s..e]);
    }
    m
}

struct Tex {
    w: usize,
    h: usize,
    rgba: Vec<u8>,
}

fn texture_message(
    key: u64,
    full: &Tex,
    x: usize,
    y: usize,
    w: usize,
    h: usize,
    data: &[u8],
) -> Vec<u8> {
    let mut m = Vec::with_capacity(30 + data.len());
    m.push(PREFIX_MESH);
    m.push(MESH_MSG_TEXTURE);
    m.extend_from_slice(&fold_key(key).to_le_bytes());
    m.extend_from_slice(&(full.w as u32).to_le_bytes());
    m.extend_from_slice(&(full.h as u32).to_le_bytes());
    for v in [x, y, w, h] {
        m.extend_from_slice(&(v as u32).to_le_bytes());
    }
    m.extend_from_slice(data);
    m
}

/// CPU-side mirror of egui's texture state (font atlas + image uploads).
/// Fed every frame regardless of the active codec so a runtime switch to the
/// mesh lane finds the complete store for joiner bootstrap.
#[derive(Default)]
pub struct TextureStore {
    textures: HashMap<u64, Tex>,
}

impl TextureStore {
    /// Apply one frame's deltas; returns the framed incremental messages
    /// (empty on the vast majority of frames).
    pub fn ingest(&mut self, delta: &egui::TexturesDelta) -> Vec<Vec<u8>> {
        let mut msgs = Vec::new();
        for (id, d) in &delta.set {
            let egui::epaint::ImageData::Color(img) = &d.image;
            let key = tex_key(*id);
            let mut patch = Vec::with_capacity(img.pixels.len() * 4);
            for p in &img.pixels {
                patch.extend_from_slice(&p.to_array());
            }
            match d.pos {
                None => {
                    self.textures.insert(
                        key,
                        Tex {
                            w: img.size[0],
                            h: img.size[1],
                            rgba: patch,
                        },
                    );
                    let t = &self.textures[&key];
                    msgs.push(texture_message(key, t, 0, 0, t.w, t.h, &t.rgba));
                }
                Some([x, y]) => {
                    if let Some(t) = self.textures.get_mut(&key) {
                        for row in 0..img.size[1] {
                            for col in 0..img.size[0] {
                                let src = &patch[(row * img.size[0] + col) * 4..][..4];
                                let (dx, dy) = (x + col, y + row);
                                if dx < t.w && dy < t.h {
                                    let o = (dy * t.w + dx) * 4;
                                    t.rgba[o..o + 4].copy_from_slice(src);
                                }
                            }
                        }
                    }
                    if let Some(t) = self.textures.get(&key) {
                        msgs.push(texture_message(
                            key,
                            t,
                            x,
                            y,
                            img.size[0],
                            img.size[1],
                            &patch,
                        ));
                    }
                }
            }
        }
        for id in &delta.free {
            self.textures.remove(&tex_key(*id));
        }
        msgs
    }

    /// Framed whole-texture messages for the entire store — a fresh
    /// connection's bootstrap, sent before its first frame message.
    pub fn full_messages(&self) -> Vec<Vec<u8>> {
        self.textures
            .iter()
            .map(|(key, t)| texture_message(*key, t, 0, 0, t.w, t.h, &t.rgba))
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn one_triangle(ppp_scaled_max: f32) -> egui::ClippedPrimitive {
        let mut mesh = egui::epaint::Mesh::default();
        for (x, y) in [(0.0, 0.0), (ppp_scaled_max, 0.0), (0.0, ppp_scaled_max)] {
            mesh.vertices.push(egui::epaint::Vertex {
                pos: egui::pos2(x, y),
                uv: egui::pos2(0.25, 0.75),
                color: egui::Color32::from_rgba_premultiplied(10, 20, 30, 40),
            });
        }
        mesh.indices.extend([0, 1, 2]);
        egui::ClippedPrimitive {
            clip_rect: egui::Rect::from_min_max(egui::pos2(1.0, 2.0), egui::pos2(100.0, 200.0)),
            primitive: egui::epaint::Primitive::Mesh(mesh),
        }
    }

    /// Quantization round-trips at 1/8 px and body hashes are stable across
    /// identical content (the dedup contract).
    #[test]
    fn serialize_roundtrip_and_hash_stability() {
        let prims = [one_triangle(100.0)];
        let a = serialize(&prims, 2.0);
        let b = serialize(&prims, 2.0);
        assert_eq!(a.hashes, b.hashes, "identical content, identical names");
        assert_eq!(a.ranges.len(), 1);
        let body = &a.scratch[a.ranges[0].0..a.ranges[0].1];
        // clip.min.x = 1.0 pt × ppp 2 × 8 = 16
        assert_eq!(u16::from_le_bytes([body[0], body[1]]), 16);
        // n_verts at offset 12, idx_width at 16, n_idx at 17
        assert_eq!(u32::from_le_bytes(body[12..16].try_into().unwrap()), 3);
        assert_eq!(body[16], 2, "u16 indices below 64Ki vertices");
        assert_eq!(u32::from_le_bytes(body[17..21].try_into().unwrap()), 3);
        // vertex 1 pos.x = 100 pt × 2 × 8 = 1600; uv.x = 0.25 → 16384
        let v1 = &body[21 + 12..21 + 24];
        assert_eq!(u16::from_le_bytes([v1[0], v1[1]]), 1600);
        assert_eq!(u16::from_le_bytes([v1[4], v1[5]]), 16384);
        // a differing frame gets a different name
        let c = serialize(&[one_triangle(50.0)], 2.0);
        assert_ne!(a.hashes[0], c.hashes[0]);
    }

    /// Frame message carries the full order and only the requested bodies.
    #[test]
    fn frame_message_shape() {
        let frame = serialize(&[one_triangle(10.0), one_triangle(20.0)], 1.0);
        let all = frame_message(1.0, 640.0, 480.0, &frame, &[0, 1]);
        let none = frame_message(1.0, 640.0, 480.0, &frame, &[]);
        assert_eq!(all[0], PREFIX_MESH);
        assert_eq!(all[1], MESH_MSG_FRAME);
        let count = u32::from_le_bytes(all[14..18].try_into().unwrap());
        assert_eq!(count, 2);
        // order list always present; bodies only when missing
        assert_eq!(none.len(), 2 + 12 + 4 + 16 + 4);
        assert!(all.len() > none.len() + frame.scratch.len());
    }
}
