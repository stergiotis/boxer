// Smoke-test for `render_svg_window` (M1) with the M3 mode + bg variants.
// Drives two settle passes against a hardcoded `egui::Window` scene, then
// on a third pass calls `render_svg_window` four times — Faithful with
// the dark VIEWPORT_BG, Faithful transparent, ContentOnly opaque,
// ContentOnly transparent — and writes each to /tmp/. Inspect the
// outputs to confirm viewBox crop, baseline-rect omission, and the
// imzero-window CSS class hook.

use egui::{Color32, Context, Id, Pos2, RawInput, Rect, Vec2};

use imzero2::imzero2::svgexport::{
    self, FontResolver, LinkZonesHandle, TexturePixelCache, TexturePixelCacheHandle, WindowMode,
};
use std::sync::{Arc, Mutex};

const VIEWPORT_BG: Color32 = Color32::from_rgb(0x1e, 0x1e, 0x1e);

fn show(ctx: &Context, window_id: Id) {
    egui::Window::new("Q3 Revenue Report").id(window_id).default_pos([40.0, 60.0]).show(
        ctx,
        |ui| {
            ui.heading("Revenue by region");
            ui.label("Americas:  $4.2M  (+12%)");
            ui.label("EMEA:      $3.1M  (+8%)");
            ui.label("APAC:      $2.5M  (+19%)");
            ui.separator();
            ui.label("Year-on-year growth driven by APAC expansion.");
            ui.hyperlink_to("Q4 forecast →", "https://example.com/q4");
        },
    );
}

fn main() {
    let ctx = Context::default();
    let id = Id::new("report-window");
    let fonts = FontResolver::default();
    let textures: TexturePixelCacheHandle = Arc::new(Mutex::new(TexturePixelCache::default()));
    let links: LinkZonesHandle = Arc::new(Mutex::new(Vec::new()));
    let raw = RawInput {
        screen_rect: Some(Rect::from_min_size(Pos2::ZERO, Vec2::new(800.0, 600.0))),
        ..Default::default()
    };

    // Two warm-up passes let `egui::Window` settle its auto-sized rect
    // (first pass reserves Shape::Noop slots that the second pass
    // fills with real Rect/Text shapes).
    for _ in 0..2 {
        let _ = ctx.run(raw.clone(), |ctx| show(ctx, id));
    }

    let mut variants: Vec<(&str, Option<String>)> = Vec::new();
    let _ = ctx.run(raw, |ctx| {
        show(ctx, id);
        variants.push((
            "faithful-dark",
            svgexport::render_svg_window(
                ctx,
                &fonts,
                &textures,
                &links,
                true,
                id,
                WindowMode::Faithful,
                Some(VIEWPORT_BG),
            ),
        ));
        variants.push((
            "faithful-transparent",
            svgexport::render_svg_window(
                ctx,
                &fonts,
                &textures,
                &links,
                true,
                id,
                WindowMode::Faithful,
                None,
            ),
        ));
        variants.push((
            "content-only-white",
            svgexport::render_svg_window(
                ctx,
                &fonts,
                &textures,
                &links,
                true,
                id,
                WindowMode::ContentOnly,
                Some(Color32::WHITE),
            ),
        ));
        variants.push((
            "content-only-transparent",
            svgexport::render_svg_window(
                ctx,
                &fonts,
                &textures,
                &links,
                true,
                id,
                WindowMode::ContentOnly,
                None,
            ),
        ));
    });

    for (tag, maybe_svg) in variants {
        let svg = maybe_svg.expect("window painted");
        let path = format!("/tmp/window_content_only.{tag}.svg");
        std::fs::write(&path, &svg).expect("write svg");
        // Extract the root <svg> opening tag for a quick eyeball.
        let head = svg.lines().find(|l| l.starts_with("<svg ")).unwrap_or("(no svg tag)");
        println!("{tag}: {} bytes — wrote {path}", svg.len());
        println!("  root: {head}");
    }
}
