// Smoke test for `imzero2::svgexport`. Drives one headless egui pass against
// a hardcoded scene, then writes /tmp/text_to_svg.svg via the same code path
// the production plugin will use (just bypassing the FFFI opcode + Plugin
// dispatch). Open the SVG in a browser to eyeball the result.

use std::sync::Arc;

use egui::epaint::{Mesh, Shape};
use egui::{Color32, Context, Pos2, RawInput, Rect, Vec2};

use imzero2::imzero2::svgexport;

const W: f32 = 480.0;
const H: f32 = 360.0;

fn main() {
    let ctx = Context::default();
    let raw_input = RawInput {
        screen_rect: Some(Rect::from_min_size(Pos2::ZERO, Vec2::new(W, H))),
        ..Default::default()
    };

    let full = ctx.run_ui(raw_input, |ui| {
        // --- text ---
        ui.heading("Hello, SVG");
        ui.label("normal body text");
        ui.monospace("monospace_line()");
        ui.colored_label(Color32::from_rgb(220, 80, 80), "red callout");
        ui.colored_label(Color32::from_rgb(80, 180, 240), "blue note");
        ui.small("small footer");

        // --- meshes (untextured) ---
        let mut solid = Mesh::default();
        solid.add_colored_rect(
            Rect::from_min_size(Pos2::new(160.0, 140.0), Vec2::new(120.0, 60.0)),
            Color32::from_rgb(60, 160, 90),
        );
        ui.painter().add(Shape::Mesh(Arc::new(solid)));

        let mut grad = Mesh::default();
        grad.colored_vertex(Pos2::new(320.0, 140.0), Color32::from_rgb(220, 60, 60));
        grad.colored_vertex(Pos2::new(420.0, 140.0), Color32::from_rgb(60, 220, 60));
        grad.colored_vertex(Pos2::new(370.0, 220.0), Color32::from_rgb(60, 60, 220));
        grad.add_triangle(0, 1, 2);
        ui.painter().add(Shape::Mesh(Arc::new(grad)));

        // --- primitives via Painter ---
        let painter = ui.painter();

        // Rect with rounded corners + stroke.
        painter.rect(
            Rect::from_min_size(Pos2::new(20.0, 230.0), Vec2::new(120.0, 70.0)),
            8.0,
            Color32::from_rgb(40, 80, 160),
            egui::Stroke::new(2.0, Color32::from_rgb(180, 200, 255)),
            egui::StrokeKind::Inside,
        );

        // Circle, stroked only (transparent fill).
        painter.circle_stroke(
            Pos2::new(200.0, 265.0),
            30.0,
            egui::Stroke::new(3.0, Color32::from_rgb(255, 200, 80)),
        );

        // Line segment.
        painter.line_segment(
            [Pos2::new(250.0, 235.0), Pos2::new(370.0, 305.0)],
            egui::Stroke::new(2.0, Color32::from_rgb(220, 220, 220)),
        );

        // Closed filled triangle path.
        painter.add(Shape::convex_polygon(
            vec![
                Pos2::new(390.0, 240.0),
                Pos2::new(450.0, 240.0),
                Pos2::new(420.0, 300.0),
            ],
            Color32::from_rgb(160, 100, 200),
            egui::Stroke::new(1.5, Color32::from_rgb(240, 220, 255)),
        ));

        // --- ScrollArea: exercises non-viewport clip groups (task #6) ---
        let scroll_origin = egui::Pos2::new(20.0, 130.0);
        let mut child = ui.new_child(
            egui::UiBuilder::new()
                .max_rect(Rect::from_min_size(scroll_origin, Vec2::new(110.0, 90.0))),
        );
        egui::ScrollArea::vertical().auto_shrink([false, false]).show(&mut child, |ui| {
            for i in 0..20 {
                ui.label(format!("scrolled_line_{i:02}"));
            }
        });
    });

    // Standalone smoke test runs *after* `run_ui` returns, by which point
    // `Context::end_pass` has drained `ctx.graphics()` into `FullOutput`.
    // Read from there. The production plugin path uses
    // `render_svg_from_context` inside `on_end_pass`.
    let resolver = svgexport::FontResolver::default();
    let embed_fonts = std::env::args().any(|a| a == "--embed-fonts");
    let svg = svgexport::render_svg_from_shapes(
        ctx.content_rect(),
        &resolver,
        embed_fonts,
        full.shapes.iter(),
        Some(Color32::from_rgb(0x1e, 0x1e, 0x1e)),
    );
    let path = "/tmp/text_to_svg.svg";
    std::fs::write(path, &svg).expect("write svg");
    println!("wrote {path} ({} bytes)", svg.len());
}
