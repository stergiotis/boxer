//! Hit regions on painted marks (ADR-0149, update of 2026-11-01).
//!
//! `paintHitRect` / `paintHitCircle` / `paintHitPolygon` declare the geometry
//! of a mark the caller painted, under a widget id. The canvas drains them
//! with its paint commands and resolves them here, after the last paint
//! command, so a region's z-order is its order among regions and not its
//! place among the paint commands.
//!
//! Resolution, per canvas and frame:
//!
//! - The hover winner is the LAST declared region whose shape contains the
//!   pointer, inside the clip in effect where it was declared, on the topmost
//!   layer under the pointer. No winner while anything is being dragged.
//! - A labelled or focusable region is registered as a hover-only egui widget
//!   over its visible bounding box, which gives it an accessibility node (role
//!   unknown, its label as name) and, when focusable, a Tab stop. Hover-only
//!   widgets never take a click or a drag, so a canvas or sense region under
//!   the marks keeps its gestures.
//! - The winner, or failing one the focused region, shows its tooltip through
//!   egui's tooltip layer: at the pointer for hover, beside the bounds for
//!   focus. It is not clipped by the canvas.
//! - Every region pushes response flags under its id (r7), so Go reads hover,
//!   click and focus back by the id it emitted, one frame later.

use super::{ImZeroFffi, PaintCmd};
use crate::imzero2::fenums::ResponseFlags;

/// A hit region's geometry, in canvas coordinates.
pub enum HitShape {
    Rect {
        min_x: f32,
        min_y: f32,
        max_x: f32,
        max_y: f32,
    },
    Circle {
        cx: f32,
        cy: f32,
        radius: f32,
    },
    /// A simple polygon, convex or not; tested by the even-odd rule.
    Polygon {
        xs: Vec<f32>,
        ys: Vec<f32>,
    },
}

impl HitShape {
    /// The bounding box, in canvas coordinates. Empty for a degenerate shape.
    pub fn bounds(&self) -> egui::Rect {
        match self {
            Self::Rect {
                min_x,
                min_y,
                max_x,
                max_y,
            } => egui::Rect::from_two_pos(
                egui::Pos2::new(*min_x, *min_y),
                egui::Pos2::new(*max_x, *max_y),
            ),
            Self::Circle { cx, cy, radius } => egui::Rect::from_center_size(
                egui::Pos2::new(*cx, *cy),
                egui::Vec2::splat(2.0 * radius.abs()),
            ),
            Self::Polygon { xs, ys } => {
                let n = xs.len().min(ys.len());
                let mut r = egui::Rect::NOTHING;
                for k in 0..n {
                    r.extend_with(egui::Pos2::new(xs[k], ys[k]));
                }
                r
            }
        }
    }

    /// Whether the canvas-local point (x, y) lies in the shape.
    pub fn contains(&self, x: f32, y: f32) -> bool {
        match self {
            Self::Rect { .. } => self.bounds().contains(egui::Pos2::new(x, y)),
            Self::Circle { cx, cy, radius } => {
                let (dx, dy) = (x - cx, y - cy);
                dx * dx + dy * dy <= radius * radius
            }
            Self::Polygon { xs, ys } => {
                let n = xs.len().min(ys.len());
                if n < 3 {
                    return false;
                }
                let mut inside = false;
                let mut j = n - 1;
                for i in 0..n {
                    let (xi, yi, xj, yj) = (xs[i], ys[i], xs[j], ys[j]);
                    if (yi > y) != (yj > y) && x < (xj - xi) * (y - yi) / (yj - yi) + xi {
                        inside = !inside;
                    }
                    j = i;
                }
                inside
            }
        }
    }

    fn focus_ring(&self, painter: &egui::Painter, origin: egui::Pos2, stroke: egui::Stroke) {
        let o = origin.to_vec2();
        match self {
            Self::Rect { .. } => {
                painter.rect_stroke(
                    self.bounds().translate(o).expand(2.0),
                    2.0,
                    stroke,
                    egui::StrokeKind::Outside,
                );
            }
            Self::Circle { cx, cy, radius } => {
                painter.circle_stroke(egui::Pos2::new(cx + o.x, cy + o.y), radius + 2.0, stroke);
            }
            Self::Polygon { xs, ys } => {
                let n = xs.len().min(ys.len());
                let pts: Vec<egui::Pos2> =
                    (0..n).map(|k| egui::Pos2::new(xs[k] + o.x, ys[k] + o.y)).collect();
                painter.add(egui::Shape::closed_line(pts, stroke));
            }
        }
    }
}

/// A hit region met while draining: its index in the frame's command list
/// and the clip (screen coordinates) in effect where it was declared.
pub struct PendingHit {
    pub idx: usize,
    pub clip: egui::Rect,
}

impl<R: std::io::BufRead, W: std::io::Write> ImZeroFffi<'_, R, W> {
    pub(super) fn resolve_hit_regions(
        &mut self,
        painter: &egui::Painter,
        origin: egui::Pos2,
        ui: &egui::Ui,
        cmds: &[PaintCmd],
        hits: &[PendingHit],
    ) {
        let ctx = ui.ctx().clone();
        let pointer = if ctx.dragged_id().is_some() {
            None
        } else {
            ctx.pointer_hover_pos()
        };

        // Pass 1: the hover winner — the last region under the pointer.
        let mut winner: Option<usize> = None;
        if let Some(p) = pointer {
            for (k, h) in hits.iter().enumerate() {
                let PaintCmd::HitRegion { shape, .. } = &cmds[h.idx] else {
                    continue;
                };
                let visible = shape.bounds().translate(origin.to_vec2()).intersect(h.clip);
                if visible.is_positive()
                    && visible.contains(p)
                    && shape.contains(p.x - origin.x, p.y - origin.y)
                    && ui.rect_contains_pointer(visible)
                {
                    winner = Some(k);
                }
            }
        }
        let (primary, secondary, activate) = ctx.input(|i| {
            (
                i.pointer.primary_clicked(),
                i.pointer.secondary_clicked(),
                i.key_pressed(egui::Key::Enter) || i.key_pressed(egui::Key::Space),
            )
        });
        let focus_stroke = ui.visuals().selection.stroke;

        // Pass 2: accessibility nodes, focus, and the flags of every region.
        let mut focused: Option<(usize, egui::Rect)> = None;
        for (k, h) in hits.iter().enumerate() {
            let PaintCmd::HitRegion {
                id,
                shape,
                label,
                focusable,
                ..
            } = &cmds[h.idx]
            else {
                continue;
            };
            let visible = shape.bounds().translate(origin.to_vec2()).intersect(h.clip);
            let hovered = winner == Some(k);
            let mut flags = ResponseFlags::ENABLED;
            flags.set(ResponseFlags::HOVERED, hovered);
            flags.set(ResponseFlags::CONTAINS_POINTER, hovered);
            flags.set(ResponseFlags::PRIMARY_CLICKED, hovered && primary);
            flags.set(ResponseFlags::SECONDARY_CLICKED, hovered && secondary);
            if visible.is_positive() && (*focusable || !label.is_empty()) {
                let sense = if *focusable {
                    egui::Sense::FOCUSABLE
                } else {
                    egui::Sense::hover()
                };
                let resp = ui.interact(visible, *id, sense);
                resp.widget_info(|| {
                    egui::WidgetInfo::labeled(egui::WidgetType::Other, true, label.as_str())
                });
                flags.set(ResponseFlags::HAS_FOCUS, resp.has_focus());
                flags.set(ResponseFlags::GAINED_FOCUS, resp.gained_focus());
                flags.set(ResponseFlags::LOST_FOCUS, resp.lost_focus());
                if resp.has_focus() {
                    if activate {
                        flags.insert(ResponseFlags::PRIMARY_CLICKED);
                    }
                    shape.focus_ring(&painter.with_clip_rect(h.clip), origin, focus_stroke);
                    focused = Some((k, visible));
                }
            }
            self.r7_push(id.value(), flags);
        }

        // The tooltip: the hover winner at the pointer, else the focused
        // region beside its bounds.
        let target = match (winner, focused) {
            (Some(k), _) => Some((k, egui::PopupAnchor::Pointer)),
            (None, Some((k, rect))) => Some((k, egui::PopupAnchor::ParentRect(rect))),
            (None, None) => None,
        };
        let Some((k, anchor)) = target else {
            return;
        };
        let PaintCmd::HitRegion {
            id,
            label,
            tip,
            tip_body,
            ..
        } = &cmds[hits[k].idx]
        else {
            return;
        };
        if label.is_empty() && tip.is_empty() && tip_body.is_none() {
            return;
        }
        egui::Tooltip::always_open(ctx.clone(), ui.layer_id(), *id, anchor).show(|tui| {
            if !label.is_empty() {
                tui.label(egui::RichText::new(label.as_str()).strong());
            }
            if !tip.is_empty() {
                tui.label(tip.as_str());
            }
            if let Some(block) = tip_body {
                let _ = self.replay_deferred_block_logged(&ctx, tui, block);
            }
        });
    }
}

#[cfg(test)]
mod tests {
    use super::HitShape;

    #[test]
    fn rect_accepts_either_corner_order() {
        let r = HitShape::Rect {
            min_x: 10.0,
            min_y: 20.0,
            max_x: 0.0,
            max_y: 0.0,
        };
        assert!(r.contains(5.0, 10.0));
        assert!(!r.contains(11.0, 10.0));
    }

    #[test]
    fn circle_is_round_not_its_box() {
        let c = HitShape::Circle {
            cx: 0.0,
            cy: 0.0,
            radius: 10.0,
        };
        assert!(c.contains(7.0, 7.0));
        assert!(!c.contains(9.0, 9.0)); // inside the box, outside the disc
    }

    #[test]
    fn polygon_concave_notch_is_outside() {
        // a U: the notch between the arms is outside
        let p = HitShape::Polygon {
            xs: vec![0.0, 30.0, 30.0, 20.0, 20.0, 10.0, 10.0, 0.0],
            ys: vec![0.0, 0.0, 30.0, 30.0, 10.0, 10.0, 30.0, 30.0],
        };
        assert!(p.contains(5.0, 20.0));
        assert!(p.contains(25.0, 20.0));
        assert!(!p.contains(15.0, 20.0));
        assert!(p.contains(15.0, 5.0));
        let degenerate = HitShape::Polygon {
            xs: vec![0.0, 1.0],
            ys: vec![0.0, 1.0],
        };
        assert!(!degenerate.contains(0.5, 0.5));
    }
}
