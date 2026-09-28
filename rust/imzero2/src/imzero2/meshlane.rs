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
//! - `3` — retirement (ADR-0242): `count u32, count×texture-key u32`. After
//!   drawing, retain only the preceding frame's bodies and these live textures.
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
/// ADR-0242: retire bodies outside the preceding frame and textures outside
/// the supplied live-key set. Existing frame and texture layouts are unchanged.
pub const MESH_MSG_RETIRE: u8 = 3;

pub fn retirement_message(live_keys: &[u32]) -> Vec<u8> {
    let mut msg = Vec::with_capacity(6 + live_keys.len() * 4);
    msg.extend_from_slice(&[PREFIX_MESH, MESH_MSG_RETIRE]);
    msg.extend_from_slice(&(live_keys.len() as u32).to_le_bytes());
    for key in live_keys {
        msg.extend_from_slice(&key.to_le_bytes());
    }
    msg
}

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

/// Clip one polygon (convex, from a triangle) to the square `[0, lim]²`,
/// interpolating uv and colour at the cut (Sutherland–Hodgman, one axis-
/// aligned edge at a time).
fn clip_polygon(poly: Vec<egui::epaint::Vertex>, lim: f32) -> Vec<egui::epaint::Vertex> {
    let lerp = |a: &egui::epaint::Vertex, b: &egui::epaint::Vertex, t: f32| {
        let (ca, cb) = (a.color.to_array(), b.color.to_array());
        let c =
            |i: usize| (f32::from(ca[i]) + (f32::from(cb[i]) - f32::from(ca[i])) * t).round() as u8;
        egui::epaint::Vertex {
            pos: a.pos + (b.pos - a.pos) * t,
            uv: a.uv + (b.uv - a.uv) * t,
            color: egui::Color32::from_rgba_premultiplied(c(0), c(1), c(2), c(3)),
        }
    };
    // Signed distance to each edge, positive inside.
    let edges: [&dyn Fn(egui::Pos2) -> f32; 4] =
        [&|p| p.x, &|p| lim - p.x, &|p| p.y, &|p| lim - p.y];
    let mut poly = poly;
    for dist in edges {
        if poly.is_empty() {
            break;
        }
        let mut out = Vec::with_capacity(poly.len() + 1);
        for (i, cur) in poly.iter().enumerate() {
            let prev = &poly[(i + poly.len() - 1) % poly.len()];
            let (dc, dp) = (dist(cur.pos), dist(prev.pos));
            if dc >= 0.0 {
                if dp < 0.0 {
                    out.push(lerp(prev, cur, dp / (dp - dc)));
                }
                out.push(*cur);
            } else if dp >= 0.0 {
                out.push(lerp(prev, cur, dp / (dp - dc)));
            }
        }
        poly = out;
    }
    poly
}

/// Clip `mesh` to the positions the wire can carry, `[0, lim]²` points. The
/// tessellator keeps a shape's off-screen vertices and leaves the cut to the
/// GPU scissor, but the u16 quantizer would clamp them onto the edge and
/// bend the visible part (a zoomed graph edge ending at the corner). A
/// triangle wholly inside keeps its vertices; one that crosses the range is
/// replaced by its clipped fan.
fn clip_mesh_to_wire_range(mesh: &egui::epaint::Mesh, lim: f32) -> egui::epaint::Mesh {
    let inside = |p: egui::Pos2| p.x >= 0.0 && p.y >= 0.0 && p.x <= lim && p.y <= lim;
    let mut out = egui::epaint::Mesh::with_texture(mesh.texture_id);
    out.vertices.clone_from(&mesh.vertices);
    for tri in mesh.indices.chunks_exact(3) {
        let vs = [
            mesh.vertices[tri[0] as usize],
            mesh.vertices[tri[1] as usize],
            mesh.vertices[tri[2] as usize],
        ];
        if vs.iter().all(|v| inside(v.pos)) {
            out.indices.extend_from_slice(tri);
            continue;
        }
        let poly = clip_polygon(vs.to_vec(), lim);
        if poly.len() < 3 {
            continue;
        }
        let base = out.vertices.len() as u32;
        let n = poly.len() as u32;
        out.vertices.extend(poly);
        for k in 1..n - 1 {
            out.indices.extend([base, base + k, base + k + 1]);
        }
    }
    out
}

/// Serialize tessellated primitives into wire bodies. `ppp` converts egui
/// points into viewer pixels before quantization.
pub fn serialize(clipped: &[egui::ClippedPrimitive], ppp: f32) -> SerializedFrame {
    let q = |v: f32| -> u16 { (v * ppp * 8.0).round().clamp(0.0, 65535.0) as u16 };
    // The largest position a u16 at 1/8 px carries, in points.
    let lim = 65535.0 / (8.0 * ppp);
    let in_range = |p: egui::Pos2| p.x >= 0.0 && p.y >= 0.0 && p.x <= lim && p.y <= lim;
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
        let clipped_mesh;
        let mesh = if mesh.vertices.iter().all(|v| in_range(v.pos)) {
            mesh
        } else {
            clipped_mesh = clip_mesh_to_wire_range(mesh, lim);
            &clipped_mesh
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
    pending_free: Vec<u64>,
}

impl TextureStore {
    /// Apply sets and defer frees until `finish_frame`: egui permits this
    /// frame to use a texture it frees. Returns incremental texture updates.
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
        self.pending_free.extend(delta.free.iter().map(|id| tex_key(*id)));
        msgs
    }

    /// Keys retained after this frame is drawn. Unused but live textures stay.
    pub fn live_keys(&self) -> Vec<u32> {
        let pending: std::collections::HashSet<_> = self.pending_free.iter().copied().collect();
        let mut keys: Vec<_> = self
            .textures
            .keys()
            .filter(|key| !pending.contains(key))
            .map(|key| fold_key(*key))
            .collect();
        keys.sort_unstable();
        keys
    }

    pub fn has_pending_frees(&self) -> bool {
        !self.pending_free.is_empty()
    }

    /// Call after assembling this frame's delivery, including in non-mesh modes.
    pub fn finish_frame(&mut self) {
        for key in self.pending_free.drain(..) {
            self.textures.remove(&key);
        }
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

    #[test]
    fn retirement_message_shape() {
        assert_eq!(
            retirement_message(&[7, 42]),
            vec![4, 3, 2, 0, 0, 0, 7, 0, 0, 0, 42, 0, 0, 0]
        );
        assert_eq!(retirement_message(&[]), vec![4, 3, 0, 0, 0, 0]);
    }

    #[test]
    fn freed_texture_survives_its_frame_bootstrap() {
        let mut store = TextureStore::default();
        store.textures.insert(
            7,
            Tex {
                w: 1,
                h: 1,
                rgba: vec![255; 4],
            },
        );
        store.textures.insert(
            8,
            Tex {
                w: 1,
                h: 1,
                rgba: vec![0; 4],
            },
        );
        let delta = egui::TexturesDelta {
            set: Vec::new(),
            free: vec![egui::TextureId::Managed(7)],
        };
        assert!(store.ingest(&delta).is_empty());
        assert!(store.has_pending_frees());
        assert_eq!(
            store.full_messages().len(),
            2,
            "bootstrap still needs the freed texture"
        );
        assert_eq!(
            store.live_keys(),
            vec![8],
            "retirement preserves the unused live texture"
        );
        store.finish_frame();
        assert_eq!(store.full_messages().len(), 1);
        assert!(!store.has_pending_frees());
    }

    #[test]
    fn partial_update_is_preserved_in_later_bootstrap() {
        let mut store = TextureStore::default();
        store.textures.insert(
            3,
            Tex {
                w: 2,
                h: 1,
                rgba: vec![0; 8],
            },
        );
        let image = egui::ColorImage::filled([1, 1], egui::Color32::WHITE);
        let delta = egui::TexturesDelta {
            set: vec![(
                egui::TextureId::Managed(3),
                egui::epaint::ImageDelta::partial([1, 0], image, egui::TextureOptions::LINEAR),
            )],
            free: Vec::new(),
        };
        let updates = store.ingest(&delta);
        assert_eq!(updates.len(), 1);
        store.finish_frame();
        let full = store.full_messages();
        assert_eq!(&full[0][30..], &[0, 0, 0, 0, 255, 255, 255, 255]);
        assert_eq!(store.live_keys(), vec![3]);
    }

    #[test]
    fn set_use_free_texture_is_in_bootstrap_before_retirement() {
        let mut store = TextureStore::default();
        let image = egui::ColorImage::filled([1, 1], egui::Color32::WHITE);
        let delta = egui::TexturesDelta {
            set: vec![(
                egui::TextureId::Managed(9),
                egui::epaint::ImageDelta::full(image, egui::TextureOptions::LINEAR),
            )],
            free: vec![egui::TextureId::Managed(9)],
        };
        assert_eq!(store.ingest(&delta).len(), 1);
        assert_eq!(store.full_messages().len(), 1);
        assert!(store.live_keys().is_empty());
        store.finish_frame();
        assert!(store.full_messages().is_empty());
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

    /// Decode a body's referenced vertex positions back to points.
    fn decoded_triangles(body: &[u8], ppp: f32) -> Vec<[egui::Pos2; 3]> {
        let n_verts = u32::from_le_bytes(body[12..16].try_into().unwrap()) as usize;
        let wide = body[16] == 4;
        let n_idx = u32::from_le_bytes(body[17..21].try_into().unwrap()) as usize;
        let pos = |i: usize| {
            let v = &body[21 + i * 12..];
            let c = |o: usize| f32::from(u16::from_le_bytes([v[o], v[o + 1]])) / 8.0 / ppp;
            egui::pos2(c(0), c(2))
        };
        let idx_base = 21 + n_verts * 12;
        let idx = |k: usize| {
            if wide {
                u32::from_le_bytes(body[idx_base + k * 4..idx_base + k * 4 + 4].try_into().unwrap())
                    as usize
            } else {
                u16::from_le_bytes([body[idx_base + k * 2], body[idx_base + k * 2 + 1]]) as usize
            }
        };
        (0..n_idx / 3)
            .map(|t| [pos(idx(t * 3)), pos(idx(t * 3 + 1)), pos(idx(t * 3 + 2))])
            .collect()
    }

    /// A triangle reaching off-screen is clipped, not clamped: every vertex
    /// on the wire still lies inside the original triangle, so the visible
    /// part keeps its angle.
    #[test]
    fn off_range_vertices_are_clipped_not_clamped() {
        let tri = [
            egui::pos2(400.0, 300.0),
            egui::pos2(-3000.0, -800.0),
            egui::pos2(400.0, 310.0),
        ];
        let mut mesh = egui::epaint::Mesh::default();
        for p in tri {
            mesh.vertices.push(egui::epaint::Vertex {
                pos: p,
                uv: egui::Pos2::ZERO,
                color: egui::Color32::WHITE,
            });
        }
        mesh.indices.extend([0, 1, 2]);
        let prim = egui::ClippedPrimitive {
            clip_rect: egui::Rect::from_min_max(egui::Pos2::ZERO, egui::pos2(800.0, 600.0)),
            primitive: egui::epaint::Primitive::Mesh(mesh),
        };
        let frame = serialize(&[prim], 1.0);
        let body = &frame.scratch[frame.ranges[0].0..frame.ranges[0].1];
        let tris = decoded_triangles(body, 1.0);
        assert!(!tris.is_empty(), "the on-screen part survives");
        let cross = |o: egui::Pos2, a: egui::Pos2, b: egui::Pos2| {
            (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x)
        };
        let area = cross(tri[0], tri[1], tri[2]);
        for t in &tris {
            for &p in t {
                // Inside every edge, with a 1/8 px quantization slack.
                for (a, b) in [(tri[0], tri[1]), (tri[1], tri[2]), (tri[2], tri[0])] {
                    let tol = 0.125 * (b - a).length();
                    assert!(
                        cross(a, b, p) * area.signum() >= -tol,
                        "decoded vertex {p:?} lies outside the source triangle"
                    );
                }
            }
        }
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
