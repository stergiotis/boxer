//! Type scale (ADR-0030 §SD3) + FontDefinitions wiring (§SD7) +
//! Body.Mono / Body.Numeric pairings (§SD6).
//!
//! Five size tokens at Standard density: Display / Heading / Body /
//! Caption / Micro. Density scaling (§SD3) is ±1 pt around Standard,
//! floored at 9 pt.
//!
//! Font binaries are embedded via `include_bytes!` at compile time; the
//! actual `FontDefinitions` registration happens once at startup via
//! `apply_fonts`.

use std::sync::Arc;

use egui::{Context, FontData, FontDefinitions, FontFamily, FontId};

use super::density::Density;

// ---- Type-scale point sizes (Standard density) ----

/// App-level title, prominent panel headers.
pub const DISPLAY_PT: f32 = 22.0;
/// Sub-panel header, dialog title.
pub const HEADING_PT: f32 = 16.0;
/// Default UI text, button/menu labels, table rows.
pub const BODY_PT: f32 = 13.0;
/// Plot axis labels, secondary text, badge content.
pub const CAPTION_PT: f32 = 11.0;
/// Fine print, status-bar metrics, watermark.
pub const MICRO_PT: f32 = 9.0;

/// Density scaling per ADR-0030 §SD3: Tight subtracts 1 pt (floored at
/// 9), Roomy adds 1 pt.
#[inline]
pub fn scaled(base: f32, density: Density) -> f32 {
    match density {
        Density::Tight => (base - 1.0).max(9.0),
        Density::Standard => base,
        Density::Roomy => base + 1.0,
    }
}

// ---- Font family names (used by FontId constructors) ----

/// Default proportional family — Iosevka Aile (ADR-0030 §SD2).
pub const FAMILY_PROPORTIONAL: &str = "iosevka-aile";

/// Default monospace family — IDS Mono (ADR-0030 §SD1).
pub const FAMILY_MONO: &str = "ids-mono";

/// Icon family — Phosphor Regular (ADR-0044 §SD1).
/// Covers UI affordances (gear, search, chart, file, …) and the
/// Phosphor brand-mark subset (Linux, GitHub, Apple, Google, …).
pub const FAMILY_ICONS_PHOSPHOR: &str = "phosphor";

// ---- Embedded font bytes ----

/// Iosevka Aile Regular — proportional default.
///
/// M0a ships Regular only; Medium / SemiBold / Bold + italics are
/// M0b-deferred pending a subsetting / size-budget decision (per
/// ADR-0034 SD3 amendment — actual TTF size ~10 MB/style vs ADR-0030
/// §SD7 estimate of ~200 KB/style).
pub const IOSEVKA_AILE_REGULAR: &[u8] = include_bytes!(
    "../../../../assets/fonts/iosevka-aile/IosevkaAile-Regular.ttf"
);

/// Phosphor Regular — icon font (ADR-0044 §SD1).
pub const PHOSPHOR_REGULAR: &[u8] = include_bytes!(
    "../../../../assets/fonts/phosphor/Phosphor.ttf"
);

/// IDS Mono Regular — monospace default (ADR-0030 §SD1).
///
/// M0a ships Regular only; Medium / Bold + italics are vendored in
/// `assets/fonts/ids-mono/` but not yet embedded (same size-budget
/// constraint as Aile — see [`IOSEVKA_AILE_REGULAR`]).
pub const IDS_MONO_REGULAR: &[u8] = include_bytes!(
    "../../../../assets/fonts/ids-mono/IDSMono-Regular.ttf"
);

/// Register IDS fonts into `egui::FontDefinitions`. Call once at startup
/// before any frame; the `apply_fonts` entry combines this with the
/// `set_fonts` upload.
pub fn install_fonts(defs: &mut FontDefinitions) {
    // Proportional: Iosevka Aile Regular.
    defs.font_data.insert(
        FAMILY_PROPORTIONAL.to_string(),
        Arc::new(FontData::from_static(IOSEVKA_AILE_REGULAR)),
    );
    // Monospace: IDS Mono Regular.
    defs.font_data.insert(
        FAMILY_MONO.to_string(),
        Arc::new(FontData::from_static(IDS_MONO_REGULAR)),
    );
    // Phosphor — icon font (ADR-0044).
    defs.font_data.insert(
        FAMILY_ICONS_PHOSPHOR.to_string(),
        Arc::new(FontData::from_static(PHOSPHOR_REGULAR)),
    );

    // Wire Aile as the first Proportional family entry so it wins over
    // egui's default; Phosphor falls through as the icon-codepoint
    // fallback per ADR-0044 §SD5.
    defs.families
        .entry(FontFamily::Proportional)
        .or_default()
        .insert(0, FAMILY_PROPORTIONAL.to_string());
    defs.families
        .entry(FontFamily::Proportional)
        .or_default()
        .push(FAMILY_ICONS_PHOSPHOR.to_string());
    // Wire IDS Mono as the first Monospace family entry; Phosphor falls
    // through as the icon-codepoint fallback per ADR-0044 §SD5.
    defs.families
        .entry(FontFamily::Monospace)
        .or_default()
        .insert(0, FAMILY_MONO.to_string());
    defs.families
        .entry(FontFamily::Monospace)
        .or_default()
        .push(FAMILY_ICONS_PHOSPHOR.to_string());
}

/// One-shot startup helper. Calls `install_fonts` against a fresh
/// `FontDefinitions::default()` and uploads via `ctx.set_fonts`.
pub fn apply_fonts(ctx: &Context) {
    let mut defs = FontDefinitions::default();
    install_fonts(&mut defs);
    ctx.set_fonts(defs);
}

// ---- Type-scale FontId accessors ----

#[inline]
fn proportional(size: f32) -> FontId {
    FontId::new(size, FontFamily::Proportional)
}

#[inline]
fn monospace(size: f32) -> FontId {
    FontId::new(size, FontFamily::Monospace)
}

/// `Display` token at the active density. Proportional Aile.
#[inline]
pub fn display(d: Density) -> FontId {
    proportional(scaled(DISPLAY_PT, d))
}

/// `Heading` token at the active density. Proportional Aile.
#[inline]
pub fn heading(d: Density) -> FontId {
    proportional(scaled(HEADING_PT, d))
}

/// `Body` token at the active density. Proportional Aile.
#[inline]
pub fn body(d: Density) -> FontId {
    proportional(scaled(BODY_PT, d))
}

/// `Caption` token at the active density. Proportional Aile.
#[inline]
pub fn caption(d: Density) -> FontId {
    proportional(scaled(CAPTION_PT, d))
}

/// `Micro` token at the active density. Proportional Aile.
#[inline]
pub fn micro(d: Density) -> FontId {
    proportional(scaled(MICRO_PT, d))
}

/// `Body.Mono` token (ADR-0030 §SD6): code, identifiers, numeric tables.
#[inline]
pub fn body_mono(d: Density) -> FontId {
    monospace(scaled(BODY_PT, d))
}

/// `Caption.Mono` token (ADR-0030 §SD6): mono numeric in caption contexts.
#[inline]
pub fn caption_mono(d: Density) -> FontId {
    monospace(scaled(CAPTION_PT, d))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn type_scale_matches_adr_0030_sd3() {
        assert_eq!(DISPLAY_PT, 22.0);
        assert_eq!(HEADING_PT, 16.0);
        assert_eq!(BODY_PT, 13.0);
        assert_eq!(CAPTION_PT, 11.0);
        assert_eq!(MICRO_PT, 9.0);
    }

    #[test]
    fn density_scaling_tight_floors_at_9() {
        // Micro at Tight: 9 - 1 = 8, but floor at 9 kicks in.
        assert_eq!(scaled(MICRO_PT, Density::Tight), 9.0);
    }

    #[test]
    fn density_scaling_roomy_adds_one() {
        assert_eq!(scaled(BODY_PT, Density::Roomy), 14.0);
        assert_eq!(scaled(MICRO_PT, Density::Roomy), 10.0);
    }

    #[test]
    fn embedded_aile_is_nonempty() {
        // Smoke test: include_bytes! resolved a real font.
        assert!(IOSEVKA_AILE_REGULAR.len() > 100_000,
            "Iosevka Aile Regular bytes too small ({} B) — check repo state",
            IOSEVKA_AILE_REGULAR.len());
    }

    #[test]
    fn embedded_phosphor_is_nonempty() {
        assert!(PHOSPHOR_REGULAR.len() > 100_000,
            "Phosphor Regular bytes too small ({} B) — check repo state",
            PHOSPHOR_REGULAR.len());
    }

    #[test]
    fn embedded_ids_mono_is_nonempty() {
        assert!(IDS_MONO_REGULAR.len() > 100_000,
            "IDS Mono Regular bytes too small ({} B) — check repo state",
            IDS_MONO_REGULAR.len());
    }
}
