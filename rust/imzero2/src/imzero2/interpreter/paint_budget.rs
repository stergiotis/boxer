//! The painter lane's geometry budget and its batched meshes (ADR-0329).
//!
//! egui-wgpu uploads a frame's tessellated vertices into one GPU buffer and
//! grows it by doubling. A buffer past the device's `max_buffer_size` is a
//! wgpu validation error, which wgpu treats as fatal — so a frame with enough
//! geometry takes the whole host down rather than drawing slowly. The budget
//! here keeps the painter lane under the line: every `PaintCmd` is estimated
//! before it is realized, and a command that would cross the budget is cut
//! short (a batch) or skipped (anything else) and counted.
//!
//! The estimates follow epaint's own tessellation closely enough to bound it
//! — they are not exact, and do not need to be: the budget leaves a quarter of
//! the half-buffer to the widgets egui draws itself, which this does not see.

use super::PaintCmd;

/// wgpu's default `max_buffer_size` (256 MiB): the limit a device has unless
/// the host asked the adapter for more.
pub const DEFAULT_MAX_BUFFER_SIZE: u64 = 256 << 20;

const VERTEX_BYTES: u64 = std::mem::size_of::<egui::epaint::Vertex>() as u64;

/// The painter lane's vertex budget on a device whose largest buffer is
/// `max_buffer_size` bytes: half of it, because a doubling growth from just
/// under half overshoots, and three quarters of that half.
pub fn paint_vertex_budget_for(max_buffer_size: u64) -> u64 {
    max_buffer_size / 2 / VERTEX_BYTES / 4 * 3
}

/// The path points epaint lays a circle of `radius` out with (the cutoffs of
/// `Path::add_circle`).
fn epaint_circle_points(radius: f32) -> u64 {
    if radius <= 2.0 {
        8
    } else if radius <= 5.0 {
        16
    } else if radius < 18.0 {
        32
    } else if radius < 50.0 {
        64
    } else {
        128
    }
}

/// A feathered convex fill of `points` path points: an inner and an outer
/// ring.
fn feathered_fill(points: u64) -> u64 {
    2 * points
}

/// A feathered closed stroke of `points` path points: up to four rings.
fn feathered_stroke(points: u64) -> u64 {
    4 * points
}

/// A feathered line segment.
const LINE_VERTICES: u64 = 8;

impl PaintCmd {
    /// The command's items and the vertices each costs. A batch (markers,
    /// segments, triangles, rects) reports its length so an over-budget batch
    /// can be cut short rather than dropped; everything else is one item.
    pub(crate) fn budget_items(&self) -> (usize, u64) {
        match self {
            Self::CircleFilled { radius, .. } => (1, feathered_fill(epaint_circle_points(*radius))),
            Self::CircleStroke { radius, .. } => {
                (1, feathered_stroke(epaint_circle_points(*radius)))
            }
            Self::RectFilled { rounding, .. } => (
                1,
                if *rounding > 0.0 {
                    feathered_fill(4 * 8)
                } else {
                    8
                },
            ),
            Self::RectStroke { rounding, .. } => (
                1,
                if *rounding > 0.0 {
                    feathered_stroke(4 * 8)
                } else {
                    16
                },
            ),
            Self::Line { .. } => (1, LINE_VERTICES),
            Self::DashedLine {
                from_x,
                from_y,
                to_x,
                to_y,
                dash_len,
                gap_len,
                ..
            } => {
                let len = (to_x - from_x).hypot(to_y - from_y);
                let period = (dash_len + gap_len).max(1.0);
                let dashes = (len / period).ceil();
                let dashes = if dashes.is_finite() {
                    dashes.max(1.0) as u64
                } else {
                    1
                };
                (1, dashes * LINE_VERTICES)
            }
            Self::Arrow { .. } => (1, 3 * LINE_VERTICES),
            Self::Text { text, .. } => (1, 4 * text.chars().count() as u64),
            Self::Polyline { points, .. } => (1, 4 * points.len() as u64),
            Self::PolygonFilled {
                points,
                stroke,
                concave,
                ..
            } => {
                let p = points.len() as u64;
                let fill = if *concave { p } else { feathered_fill(p) };
                let outline = if stroke.width > 0.0 {
                    feathered_stroke(p)
                } else {
                    0
                };
                (1, fill + outline)
            }
            Self::EllipseFilled { rx, ry, .. } => {
                (1, feathered_fill(epaint_circle_points(rx.max(*ry))))
            }
            Self::EllipseStroke { rx, ry, .. } => {
                (1, feathered_stroke(epaint_circle_points(rx.max(*ry))))
            }
            // epaint flattens a cubic to a tolerance; a long curve on a large
            // canvas stays well under this many points.
            Self::CubicBezier { .. } => (1, 4 * 64),
            Self::SenseRegion { .. } | Self::ClipPush { .. } | Self::ClipPop => (0, 0),
            Self::Markers {
                xs,
                ys,
                shape,
                radius,
                color,
                mesh,
                outline,
                ..
            } => {
                let n = xs.len().min(ys.len());
                let per = match shape {
                    1..=6 => 8,
                    7..=9 => 3 * LINE_VERTICES,
                    _ => {
                        let ringed = outline.width > 0.0 && outline.color.a() > 0;
                        if *mesh {
                            disc_mesh_vertices(*radius, outline.width, ringed, color.a() > 0) as u64
                        } else {
                            let pts = epaint_circle_points(*radius);
                            feathered_fill(pts) + if ringed { feathered_stroke(pts) } else { 0 }
                        }
                    }
                };
                (n, per)
            }
            Self::RectsFilled {
                min_xs,
                min_ys,
                max_xs,
                max_ys,
                cols,
            } => (
                min_xs.len().min(min_ys.len()).min(max_xs.len()).min(max_ys.len()).min(cols.len()),
                8,
            ),
            Self::Segments {
                x0s,
                y0s,
                x1s,
                y1s,
                cols,
                tessellated,
                ..
            } => (
                x0s.len().min(y0s.len()).min(x1s.len()).min(y1s.len()).min(cols.len()),
                if *tessellated { LINE_VERTICES } else { 4 },
            ),
            Self::Triangles { xs, ys, cols } => (triangle_count(xs, ys, cols), 3),
            Self::Image { .. } => (1, 4),
        }
    }

    /// Whether a budget cut can shorten this command to its first k items.
    pub(crate) fn is_batch(&self) -> bool {
        matches!(
            self,
            Self::Markers { .. }
                | Self::RectsFilled { .. }
                | Self::Segments { .. }
                | Self::Triangles { .. }
        )
    }
}

pub(crate) fn triangle_count(xs: &[f32], ys: &[f32], cols: &[u32]) -> usize {
    (xs.len() / 3).min(ys.len() / 3).min(cols.len())
}

/// Segments around one mesh disc of `radius` pixels: about one per 2.5 px of
/// circumference, from a hexagon up. epaint's sixteen-point floor is for a
/// feathered path; a fan this small reads as round.
pub(crate) fn disc_segments(radius: f32) -> usize {
    let n = (radius.max(0.0) * std::f32::consts::TAU / 2.5).ceil();
    if n.is_finite() {
        (n as usize).clamp(6, 64)
    } else {
        6
    }
}

/// The unit circle at every segment count `disc_segments` can return,
/// computed once: a batch of discs costs no trigonometry per vertex.
fn unit_circle(n: usize) -> &'static [(f32, f32)] {
    static TABLES: std::sync::OnceLock<Vec<Vec<(f32, f32)>>> = std::sync::OnceLock::new();
    let tables = TABLES.get_or_init(|| {
        (0..=64)
            .map(|n| {
                (0..n)
                    .map(|k| {
                        let (s, c) = (k as f32 * std::f32::consts::TAU / n as f32).sin_cos();
                        (c, s)
                    })
                    .collect()
            })
            .collect()
    });
    &tables[n.min(64)]
}

/// The vertices `push_disc` adds for one disc. A disc with no fill (a ring
/// marker) has no fan.
pub(crate) fn disc_mesh_vertices(
    radius: f32,
    outline_width: f32,
    ringed: bool,
    filled: bool,
) -> usize {
    let n = disc_segments(if ringed {
        radius + outline_width / 2.0
    } else {
        radius
    });
    match (filled, ringed) {
        (true, true) => 1 + 4 * n,
        (true, false) => 1 + 2 * n,
        (false, true) => 3 * n,
        (false, false) => 0,
    }
}

/// Appends one disc to `mesh`: a fan to the fill's edge, the outline as a
/// ring centred on the radius when `outline` has width and colour, and a
/// one-pixel feather outside the outermost edge — the only antialiased edge,
/// which is the one against the background. A transparent fill draws the ring
/// alone, which is how a batch of ring markers costs no fans.
pub(crate) fn push_disc(
    mesh: &mut egui::Mesh,
    center: egui::Pos2,
    radius: f32,
    fill: egui::Color32,
    outline: egui::Stroke,
) {
    if !(center.x.is_finite() && center.y.is_finite() && radius.is_finite()) || radius <= 0.0 {
        return;
    }
    let ringed = outline.width > 0.0 && outline.color.a() > 0;
    let filled = fill.a() > 0;
    if !filled && !ringed {
        return;
    }
    // `edge` is the disc's visible boundary; the feather straddles it, half a
    // pixel either side, as epaint's does.
    let edge = if ringed {
        radius + outline.width / 2.0
    } else {
        radius
    };
    let fill_r = if ringed {
        (radius - outline.width / 2.0).max(0.0)
    } else {
        (radius - 0.5).max(0.0)
    };
    let solid_r = (edge - 0.5).max(fill_r);
    let feather_r = edge + 0.5;
    let n = disc_segments(edge);
    let unit = unit_circle(n);
    let base = mesh.vertices.len() as u32;
    let ring = |mesh: &mut egui::Mesh, r: f32, col: egui::Color32| {
        for &(c, s) in unit {
            mesh.colored_vertex(egui::Pos2::new(center.x + r * c, center.y + r * s), col);
        }
    };
    let n32 = n as u32;
    let strip = |mesh: &mut egui::Mesh, inner: u32, outer: u32| {
        for k in 0..n32 {
            let k1 = (k + 1) % n32;
            mesh.add_triangle(inner + k, outer + k, outer + k1);
            mesh.add_triangle(inner + k, outer + k1, inner + k1);
        }
    };
    let mut outermost = base + 1;
    if filled {
        // The fan to the fill's edge.
        mesh.colored_vertex(center, fill);
        ring(mesh, fill_r, fill);
        for k in 0..n32 {
            mesh.add_triangle(base, base + 1 + k, base + 1 + (k + 1) % n32);
        }
    }
    if ringed {
        // The outline, starting at the fill's edge in its own colour, so the
        // boundary between fill and outline is sharp. With no fill its inner
        // edge is unfeathered: a ring marker is a hairline, where that does
        // not show.
        let r1 = mesh.vertices.len() as u32;
        ring(mesh, fill_r, outline.color);
        let r2 = r1 + n32;
        ring(mesh, solid_r, outline.color);
        strip(mesh, r1, r2);
        outermost = r2;
    }
    // The feather: the outermost colour fading to nothing over a pixel.
    let feather = mesh.vertices.len() as u32;
    ring(mesh, feather_r, egui::Color32::TRANSPARENT);
    strip(mesh, outermost, feather);
}

/// Appends the first `limit` triangles of a `paintTriangles` batch to `mesh`,
/// skipping any with a non-finite corner.
pub(crate) fn push_triangles(
    mesh: &mut egui::Mesh,
    origin: egui::Pos2,
    xs: &[f32],
    ys: &[f32],
    cols: &[u32],
    limit: usize,
    color: impl Fn(u32) -> egui::Color32,
) {
    let n = triangle_count(xs, ys, cols).min(limit);
    mesh.vertices.reserve(3 * n);
    mesh.indices.reserve(3 * n);
    for ((tx, ty), &c) in xs.chunks_exact(3).zip(ys.chunks_exact(3)).zip(cols).take(n) {
        let corners = [(tx[0], ty[0]), (tx[1], ty[1]), (tx[2], ty[2])];
        if corners.iter().any(|(x, y)| !x.is_finite() || !y.is_finite()) {
            continue;
        }
        let col = color(c);
        let base = mesh.vertices.len() as u32;
        for (x, y) in corners {
            mesh.colored_vertex(egui::Pos2::new(origin.x + x, origin.y + y), col);
        }
        mesh.add_triangle(base, base + 1, base + 2);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn budget_is_half_the_buffer_less_a_quarter() {
        // 256 MiB / 2 / 20 B = 6.7 M vertices; three quarters of that.
        assert_eq!(paint_vertex_budget_for(DEFAULT_MAX_BUFFER_SIZE), 5_033_163);
    }

    #[test]
    fn disc_vertex_count_matches_the_estimate() {
        for fill in [egui::Color32::RED, egui::Color32::TRANSPARENT] {
            for (r, w) in [(1.0, 0.0), (5.0, 1.5), (5.0, 0.0), (40.0, 2.0)] {
                let mut mesh = egui::Mesh::default();
                let stroke = egui::Stroke::new(w, egui::Color32::WHITE);
                push_disc(&mut mesh, egui::Pos2::new(10.0, 10.0), r, fill, stroke);
                let want = disc_mesh_vertices(r, w, w > 0.0, fill.a() > 0);
                assert_eq!(mesh.vertices.len(), want, "r={r} w={w} fill={fill:?}");
                assert!(mesh.is_valid());
            }
        }
    }

    #[test]
    fn a_small_mesh_disc_is_cheaper_than_the_feathered_shapes() {
        let mesh = disc_mesh_vertices(5.0, 1.5, true, true) as u64;
        let shapes =
            feathered_fill(epaint_circle_points(5.0)) + feathered_stroke(epaint_circle_points(5.0));
        assert!(mesh * 3 < shapes * 2, "mesh {mesh} vs shapes {shapes}");
    }

    #[test]
    fn triangles_skip_non_finite_corners_and_honour_the_limit() {
        let xs = [0.0, 1.0, 0.0, f32::NAN, 1.0, 0.0, 0.0, 1.0, 0.0];
        let ys = [0.0, 0.0, 1.0, 0.0, 0.0, 1.0, 0.0, 0.0, 1.0];
        let cols = [0xff0000ff, 0x00ff00ff, 0x0000ffff];
        let mut mesh = egui::Mesh::default();
        push_triangles(&mut mesh, egui::Pos2::ZERO, &xs, &ys, &cols, 2, |_| {
            egui::Color32::WHITE
        });
        assert_eq!(
            mesh.indices.len(),
            3,
            "the second is non-finite, the third past the limit"
        );
    }
}
