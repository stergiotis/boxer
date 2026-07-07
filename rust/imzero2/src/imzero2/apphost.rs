//! Host-independent application setup shared by the eframe desktop host
//! (`app.rs`) and the headless render host (`headless.rs`, ADR-0024).
//!
//! Everything here touches only `egui::Context` + [`AppConfig`] — no
//! eframe, no winit, no wgpu — so the FFFI2 interpreter behaves
//! identically regardless of which host drives the frame loop (ADR-0024
//! SD1: "the interpreter body is shared verbatim; only the host loop
//! differs").

use crate::imzero2;
use crate::imzero2::appconfig::{AppConfig, FontTweakConfig};

fn apply_tweak(data: &mut egui::FontData, tweak: &FontTweakConfig) {
    data.tweak.scale = tweak.scale;
    data.tweak.y_offset_factor = tweak.y_offset_factor;
    data.tweak.y_offset = tweak.y_offset;
}

pub fn load_custom_fonts(
    ctx: &egui::Context,
    config: &AppConfig,
) -> imzero2::svgexport::FontResolver {
    let mut fonts = egui::FontDefinitions::default();
    let mut resolver = imzero2::svgexport::FontResolver::new();

    // Slots for the bytes that back each loaded face. Captured here so we
    // can hand the whole chain to the SVG resolver after the egui side
    // has chosen its family fallback order. Dropped after `register_chain`
    // wraps them in Arcs and returns.
    let mut main_bytes: Option<(Vec<u8>, u32)> = None;
    let mut mono_bytes: Option<(Vec<u8>, u32)> = None;
    let mut phosphor_bytes: Option<(Vec<u8>, u32)> = None;
    let mut fallback_bytes: Option<(Vec<u8>, u32)> = None;

    let mut load_font = |path: &str,
                         name: &str,
                         tweak: &FontTweakConfig,
                         capture: Option<&mut Option<(Vec<u8>, u32)>>|
     -> bool {
        if path.is_empty() {
            return false;
        }
        match std::fs::read(path) {
            Ok(data) => {
                if let Some(slot) = capture {
                    *slot = Some((data.clone(), 0));
                }
                let mut font_data = egui::FontData::from_owned(data);
                apply_tweak(&mut font_data, tweak);
                fonts.font_data.insert(name.to_owned(), font_data.into());
                tracing::info!(
                    path,
                    name,
                    scale = tweak.scale,
                    y_offset = tweak.y_offset,
                    y_offset_factor = tweak.y_offset_factor,
                    "loaded font"
                );
                true
            }
            Err(e) => {
                tracing::error!(path, name, error=%e, "failed to load font");
                false
            }
        }
    };

    // ADR-0030 split: `main` is the FontFamily::Proportional primary;
    // `mono` is the FontFamily::Monospace primary.
    // ADR-0044: `phosphor` is the single icon font (Phosphor regular),
    // registered as a fallback in both family chains.
    //
    // Backward-compat: when `mono` is not supplied, `main` doubles as
    // the monospace primary too — preserves the pre-split default UX
    // where a single MAIN_FONT served every text style. Pass
    // --monoFontTTF (or MONO_FONT in hmi.sh) to override.
    let has_main = load_font(
        &config.main_font_ttf,
        "main",
        &config.main_font_tweak,
        Some(&mut main_bytes),
    );
    let has_mono = load_font(
        &config.mono_font_ttf,
        "mono",
        &config.mono_font_tweak,
        Some(&mut mono_bytes),
    );
    let has_phosphor = load_font(
        &config.phosphor_font_ttf,
        "phosphor",
        &config.phosphor_font_tweak,
        Some(&mut phosphor_bytes),
    );
    let has_fallback = load_font(
        &config.fallback_font_ttf,
        "fallback",
        &config.fallback_font_tweak,
        Some(&mut fallback_bytes),
    );

    if !has_main && !has_mono && !has_phosphor && !has_fallback {
        return resolver; // no custom fonts, keep egui defaults; empty resolver
    }

    let prop_primary: Option<&'static str> = has_main.then_some("main");
    let mono_primary: Option<&'static str> = if has_mono {
        Some("mono")
    } else if has_main {
        Some("main")
    } else {
        None
    };
    for (family, primary) in [
        (egui::FontFamily::Proportional, prop_primary),
        (egui::FontFamily::Monospace, mono_primary),
    ] {
        let defaults = fonts.families.get(&family).cloned().unwrap_or_default();
        let mut chain = Vec::new();
        if let Some(name) = primary {
            chain.push(name.to_owned());
        }
        if has_phosphor {
            chain.push("phosphor".to_owned());
        }
        if has_fallback {
            chain.push("fallback".to_owned());
        }
        chain.extend(defaults);
        fonts.families.insert(family, chain);
    }

    // Register per-family chains with the SVG resolver so Tier 1 emits the
    // right CSS family value and Tier 2 routes each glyph to the first
    // chain entry whose cmap covers it. Proportional uses
    // main → phosphor → fallback; Monospace uses
    // mono → phosphor → fallback (or main → ... when no explicit mono
    // was provided, matching the egui fallback above).
    let phosphor_arc = phosphor_bytes.take().map(|(b, i)| (std::sync::Arc::new(b), i));
    let fallback_arc = fallback_bytes.take().map(|(b, i)| (std::sync::Arc::new(b), i));
    let main_arc = main_bytes.take().map(|(b, i)| (std::sync::Arc::new(b), i));
    let mono_arc =
        mono_bytes.take().map(|(b, i)| (std::sync::Arc::new(b), i)).or_else(|| main_arc.clone());
    let build_svg =
        |primary: Option<(std::sync::Arc<Vec<u8>>, u32)>| -> Vec<(std::sync::Arc<Vec<u8>>, u32)> {
            let mut out = Vec::new();
            if let Some(pair) = primary {
                out.push(pair);
            }
            if let Some(pair) = phosphor_arc.clone() {
                out.push(pair);
            }
            if let Some(pair) = fallback_arc.clone() {
                out.push(pair);
            }
            out
        };
    let prop_chain = build_svg(main_arc);
    if !prop_chain.is_empty() {
        resolver.register_chain(egui::FontFamily::Proportional, prop_chain);
    }
    let mono_chain = build_svg(mono_arc);
    if !mono_chain.is_empty() {
        resolver.register_chain(egui::FontFamily::Monospace, mono_chain);
    }

    ctx.set_fonts(fonts);

    if config.main_font_size > 0.0 {
        let size = config.main_font_size;
        ctx.all_styles_mut(|style| {
            for (_text_style, font_id) in style.text_styles.iter_mut() {
                font_id.size = size;
            }
        });
    }

    resolver
}

/// One-time context + interpreter setup shared by every host. Returns the
/// interpreter and the render-cadence choice (`true` = reactive).
///
/// This is the body that used to live in `App::new` (desktop), minus the
/// eframe-specific repaint scheduling state — moved here so the headless
/// host configures fonts, IDS overlay, single-pass pinning and the
/// SVG-export plugin identically (ADR-0024 SD1).
pub fn init_common<'a, R: std::io::BufRead, W: std::io::Write>(
    ctx: &egui::Context,
    config: &AppConfig,
    r: R,
    w: W,
) -> (imzero2::interpreter::ImZeroFffi<'a, R, W>, bool) {
    let font_resolver = std::sync::Arc::new(load_custom_fonts(ctx, config));

    // IDS overlay (ADR-0029 §SD2). Density from IMZERO2_DENSITY env.
    // Set IMZERO2_IDS_FONTS=on to swap in the IDS font bundle
    // (Iosevka Aile + Symbols Nerd Font Mono) on top of the style
    // overlay — used for ADR-0030 §SD10 Aile hinting evaluation.
    // Default keeps the carousel's env-configured fonts.
    let density = imzero2_egui::style::tokens::density::from_env();
    let use_ids_fonts =
        std::env::var("IMZERO2_IDS_FONTS").map(|v| v.eq_ignore_ascii_case("on")).unwrap_or(false);
    if use_ids_fonts {
        imzero2_egui::style::apply(ctx, density);
        tracing::info!(?density, "applied IDS style overlay + IDS fonts");
    } else {
        imzero2_egui::style::apply_style_only(ctx, density);
        tracing::info!(?density, "applied IDS style overlay (host fonts)");
    }
    // When the tour is active (IMZERO2_SCREENSHOT_DIR set) collapse
    // hover/active widget strokes onto the inactive stroke so that
    // racy compositor focus delivery and cursor warping can't paint
    // an accent stroke into the first few captures. See the function
    // doc on apply_tour_neutral_overrides for the underlying race.
    let in_tour = std::env::var("IMZERO2_SCREENSHOT_DIR").map(|v| !v.is_empty()).unwrap_or(false);
    if in_tour {
        imzero2_egui::style::apply_tour_neutral_overrides(ctx);
        tracing::info!("applied tour-neutral hover/active stroke override");
    }
    // IMZERO2_RENDER_CADENCE selects idle repaint behaviour; the Go-side
    // registry (imzero2env.RenderCadence) documents the values and the Go
    // decorator mirrors the choice. "reactive" drops to an idle heartbeat
    // after warmup; anything else (incl. unset) is continuous, the default.
    let reactive = std::env::var("IMZERO2_RENDER_CADENCE")
        .map(|v| v.eq_ignore_ascii_case("reactive"))
        .unwrap_or(false);
    // Schedule an initial repaint so logic() (and then ui()) actually fires
    // without waiting for a user event. See the desktop logic() impl for why
    // this matters — tl;dr: reactive eframe + hidden startup window + Go
    // writing commands through a pipe produces a chicken-and-egg stall.
    // Harmless under the headless host (it paces itself).
    ctx.request_repaint();
    // Pin egui to single-pass rendering. Multipass (egui Options::max_passes)
    // re-runs the UI closure when widgets call `ctx.request_discard(...)`
    // for first-frame fitup (egui_snarl's SnarlState / NodeState do this).
    // That doesn't compose with the FFFI streaming protocol — the second
    // pass would re-enter the interpreter but the per-frame opcode stream has
    // already been consumed by the first pass, so widgets that rely on
    // multipass quietly draw nothing on the second pass and the output
    // ends up blank. State that snarl stores via `cx.data_mut(...)`
    // survives the discarded pass, so subsequent FFFI frames render
    // correctly without needing multipass at all.
    ctx.options_mut(|o| o.max_passes = std::num::NonZeroUsize::new(1).expect("1 is non-zero"));
    let fffi = imzero2::interpreter::ImZeroFffi::new(r, w);
    // SVG export plugin — registered once at host init. Drains
    // ImZeroFffi::export_state during each on_end_pass; cheap when no
    // export is pending. See imzero2::svgexport for the visitor.
    ctx.add_plugin(imzero2::svgexport::SvgExportPlugin::new(
        fffi.export_state.clone(),
        font_resolver,
        fffi.texture_cache.clone(),
        fffi.link_zones.clone(),
    ));
    (fffi, reactive)
}
