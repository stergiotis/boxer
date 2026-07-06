//! Spacing tokens (ADR-0032 §SD2).
//!
//! 8-value magnitude ladder per density, all multiples of 2 px (the 2 px
//! grid invariant from ADR-0029 §SD6). Purpose-based public tokens
//! (`Padding.*`, `Gap.*`, `Margin.*`) resolve to the active density's column.
//!
//! Apps reach for `padding_default(density)` rather than typing literals.
//! ADR-0029 §SD8 Tier 1 lint L3 flags literal floats in spacing positions
//! outside the token module.

use super::density::Density;

/// Magnitude ladder. `PX_TABLE[index][density]` — column order matches
/// `Density` discriminants (Tight=0, Standard=1, Roomy=2).
pub const PX_TABLE: [[f32; 3]; 8] = [
    // Tight, Standard, Roomy
    [2.0, 2.0, 4.0],    // Px[0]
    [2.0, 4.0, 6.0],    // Px[1]
    [4.0, 6.0, 8.0],    // Px[2]
    [6.0, 8.0, 12.0],   // Px[3]
    [8.0, 12.0, 16.0],  // Px[4]
    [12.0, 16.0, 24.0], // Px[5]
    [16.0, 24.0, 32.0], // Px[6]
    [24.0, 32.0, 48.0], // Px[7]
];

/// Generic ladder accessor. Most callers should use the purpose-named helpers
/// below; `px(density, idx)` is the escape hatch for cases the named tokens
/// don't cover.
#[inline]
pub fn px(density: Density, idx: usize) -> f32 {
    PX_TABLE[idx][density as usize]
}

// ---- Padding (inside a widget / container) ----

/// Hairline padding (Px[0]). Tight inline content.
#[inline]
pub fn padding_hair(d: Density) -> f32 {
    px(d, 0)
}

/// Inside small widgets (Px[1]). Button text padding, badge interior.
#[inline]
pub fn padding_inner(d: Density) -> f32 {
    px(d, 1)
}

/// Tight container padding (Px[2]).
#[inline]
pub fn padding_tight(d: Density) -> f32 {
    px(d, 2)
}

/// Default control padding, inline gaps (Px[3]).
#[inline]
pub fn padding_default(d: Density) -> f32 {
    px(d, 3)
}

/// Panel inner padding, card content (Px[4]).
#[inline]
pub fn padding_outer(d: Density) -> f32 {
    px(d, 4)
}

/// Generous panel padding, dialog content (Px[5]).
#[inline]
pub fn padding_loose(d: Density) -> f32 {
    px(d, 5)
}

// ---- Gap (between sibling items) ----

/// Between inline items (Px[2]). Chip stacks, label clusters.
#[inline]
pub fn gap_inline(d: Density) -> f32 {
    px(d, 2)
}

/// Between list items (Px[3]). Table rows, menu items.
#[inline]
pub fn gap_items(d: Density) -> f32 {
    px(d, 3)
}

/// Between major sections within a panel (Px[5]).
#[inline]
pub fn gap_sections(d: Density) -> f32 {
    px(d, 5)
}

/// Between panels at the layout level (Px[6]).
#[inline]
pub fn gap_panels(d: Density) -> f32 {
    px(d, 6)
}

// ---- Margin (outside of a panel) ----

/// Outside panel margins; panel-to-window-edge (Px[6]).
#[inline]
pub fn margin_frame(d: Density) -> f32 {
    px(d, 6)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn standard_table_matches_adr_0032_sd2() {
        // Spot-check the Standard column against ADR-0032 §SD2.
        assert_eq!(px(Density::Standard, 0), 2.0);
        assert_eq!(px(Density::Standard, 3), 8.0); // Padding.Default
        assert_eq!(px(Density::Standard, 7), 32.0);
    }

    #[test]
    fn tight_collapses_low_end() {
        // Tight Px[0] and Px[1] both = 2 (already at the 2 px floor).
        assert_eq!(px(Density::Tight, 0), 2.0);
        assert_eq!(px(Density::Tight, 1), 2.0);
    }

    #[test]
    fn roomy_extends_high_end() {
        // Roomy Px[7] = 48, off Standard's 32 ceiling.
        assert_eq!(px(Density::Roomy, 7), 48.0);
    }

    #[test]
    fn all_values_on_2px_grid() {
        for row in PX_TABLE.iter() {
            for &v in row.iter() {
                assert!(
                    (v as i32) % 2 == 0,
                    "spacing {} not a multiple of 2 px (ADR-0029 §SD6 grid invariant)",
                    v
                );
            }
        }
    }

    #[test]
    fn purpose_helpers_match_indices() {
        let d = Density::Standard;
        assert_eq!(padding_hair(d), px(d, 0));
        assert_eq!(padding_inner(d), px(d, 1));
        assert_eq!(padding_tight(d), px(d, 2));
        assert_eq!(padding_default(d), px(d, 3));
        assert_eq!(padding_outer(d), px(d, 4));
        assert_eq!(padding_loose(d), px(d, 5));
        assert_eq!(gap_inline(d), px(d, 2));
        assert_eq!(gap_items(d), px(d, 3));
        assert_eq!(gap_sections(d), px(d, 5));
        assert_eq!(gap_panels(d), px(d, 6));
        assert_eq!(margin_frame(d), px(d, 6));
    }
}
