//! ADR-0130: syntax-colored rendering for an editable `egui::TextEdit`.
//!
//! The Go side ships a [`code_view::CodeViewJobData`] describing the buffer it
//! last saw (one frame stale while the user types); the layouter built here
//! applies those sections to the *live* buffer advisorily:
//!
//! - **reconcile** — when the live buffer differs, a common-prefix/suffix diff
//!   locates the single edit region and shifts section boundaries past it, so
//!   colors do not shimmer during continuous typing;
//! - **normalize** — clamp to the live length, drop inverted ranges, enforce
//!   ascending order, round to UTF-8 boundaries, and gap-fill uncovered bytes
//!   with the default format. A malformed job degrades to plain text — never
//!   to missing text (a `LayoutJob` that skips bytes drops glyphs).
//!
//! A parallel **styled** channel (ADR-0130 L3, `sectionStyled`) rides the same
//! reconcile: sparse overlays — underline, background, strikethrough, italics —
//! that decorate the color tier without moving the color-only `Section` struct
//! the read-only codeview producers share. Styled sections normalize by
//! clamping, dropping inverted ranges and sorting, but deliberately do **not**
//! gap-fill: an uncovered byte simply has no styling.
//!
//! Text stays authoritative in the TextEdit; everything here is presentation.
//! Galley memoisation is egui's own (`Fonts` caches by job hash) — no cache
//! lives here.

use crate::imzero2::code_view;

/// Style bits carried by a [`StyledSection`]. Exactly the vocabulary
/// `egui::TextFormat` expresses natively — a wavy squiggle is not among them
/// and is deferred to a paint-over pass (ADR-0130).
pub const STYLE_UNDERLINE: u32 = 1 << 0;
pub const STYLE_BACKGROUND: u32 = 1 << 1;
pub const STYLE_STRIKETHROUGH: u32 = 1 << 2;
pub const STYLE_ITALICS: u32 = 1 << 3;

/// One sparse style overlay over a byte range of the buffer. `color` is the
/// stroke color for underline/strikethrough and the fill for background;
/// italics ignores it.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct StyledSection {
    pub byte_start: u32,
    pub byte_stop: u32,
    pub flags: u32,
    pub color: egui::Color32,
}

/// Builds the `TextEdit::layouter` closure for one apply. Both lists are moved
/// in; the closure reads the live buffer through its `&dyn TextBuffer`
/// parameter. `no_wrap` makes galley rows equal logical lines, the alignment
/// contract the line-number gutter needs.
pub fn make_layouter(
    job: code_view::CodeViewJobData,
    styled: Vec<StyledSection>,
    no_wrap: bool,
) -> impl FnMut(&egui::Ui, &dyn egui::TextBuffer, f32) -> std::sync::Arc<egui::Galley> {
    move |ui, buf, wrap_width| {
        let w = if no_wrap { f32::INFINITY } else { wrap_width };
        layout_reconciled(ui, &job, &styled, buf.as_str(), w)
    }
}

fn layout_reconciled(
    ui: &egui::Ui,
    job: &code_view::CodeViewJobData,
    styled: &[StyledSection],
    live: &str,
    wrap_width: f32,
) -> std::sync::Arc<egui::Galley> {
    let reconciled = reconcile_sections(&job.text, live, &job.sections);
    let resolved = resolve_sections(live, &reconciled);
    // The styled list describes the same job-space buffer, so it rides the
    // same edit-region shift; normalization keeps it sparse.
    let styled = resolve_styled(live, &reconcile_styled(&job.text, live, styled));

    let font_id = egui::TextStyle::Monospace.resolve(ui.style());
    let default_color = ui.visuals().text_color();

    let mut layout_job = egui::text::LayoutJob {
        text: live.to_owned(),
        break_on_newline: true,
        wrap: egui::text::TextWrapping {
            max_width: wrap_width,
            ..Default::default()
        },
        ..Default::default()
    };
    layout_job.sections.reserve(resolved.len());
    for r in &resolved {
        let mut format = egui::TextFormat {
            font_id: font_id.clone(),
            color: r.color.unwrap_or(default_color),
            ..Default::default()
        };
        apply_styles(&mut format, r.start, r.stop, &styled);
        layout_job.sections.push(egui::text::LayoutSection {
            leading_space: 0.0,
            byte_range: egui::text::ByteIndex(r.start)..egui::text::ByteIndex(r.stop),
            format,
        });
    }

    ui.fonts_mut(|f| f.layout_job(layout_job))
}

/// Merges every styled section overlapping `[start, stop)` into `format`.
///
/// Overlap, not containment: color sections are token-sized and styled
/// sections are token- or statement-sized, so a styled section routinely
/// spans many color sections. Splitting color sections at styled boundaries
/// would be more precise at the edges; overlap-merge keeps the invariant that
/// the color tier alone decides section boundaries — which is what makes the
/// two channels independent.
fn apply_styles(format: &mut egui::TextFormat, start: usize, stop: usize, styled: &[StyledSection]) {
    for s in styled {
        let (ss, se) = (s.byte_start as usize, s.byte_stop as usize);
        if se <= start {
            continue;
        }
        if ss >= stop {
            break; // sorted by start — nothing later can overlap
        }
        if s.flags & STYLE_UNDERLINE != 0 {
            format.underline = egui::Stroke::new(1.0, s.color);
        }
        if s.flags & STYLE_STRIKETHROUGH != 0 {
            format.strikethrough = egui::Stroke::new(1.0, s.color);
        }
        if s.flags & STYLE_BACKGROUND != 0 {
            format.background = s.color;
        }
        if s.flags & STYLE_ITALICS != 0 {
            format.italics = true;
        }
    }
}

/// A normalized render section over the live buffer. `color: None` marks a
/// gap-fill (default text color).
#[derive(Clone, Copy, Debug, PartialEq)]
struct Resolved {
    start: usize,
    stop: usize,
    color: Option<egui::Color32>,
}

/// Maps job-space sections onto the live buffer. `job_text == live` is the
/// per-frame steady state and passes through untouched; otherwise the single
/// edit region (common-prefix/suffix diff) shifts everything after it.
///
/// A section overlapping the edit start is stretched across the replacement,
/// so just-typed characters inherit the color they were typed into until the
/// corrected sections arrive next frame.
fn reconcile_sections(
    job_text: &str,
    live: &str,
    sections: &[code_view::Section],
) -> Vec<code_view::Section> {
    if job_text == live {
        return sections.to_vec();
    }

    let (p, job_edit_end, live_edit_end) = edit_region(job_text, live);
    let delta = live_edit_end as i64 - job_edit_end as i64;

    let shift = |x: u32| -> u32 { (x as i64 + delta).max(0) as u32 };

    let mut out = Vec::with_capacity(sections.len());
    for sec in sections {
        let (start, stop) = (sec.byte_start as usize, sec.byte_stop as usize);
        if stop <= p {
            // untouched prefix
            out.push(sec.clone());
        } else if start >= job_edit_end {
            // untouched suffix — shift
            out.push(code_view::Section {
                byte_start: shift(sec.byte_start),
                byte_stop: shift(sec.byte_stop),
                color: sec.color,
            });
        } else if start < p {
            // overlaps the edit start — stretch across the replacement
            let stop_mapped = if stop >= job_edit_end {
                shift(sec.byte_stop)
            } else {
                live_edit_end as u32
            };
            out.push(code_view::Section {
                byte_start: sec.byte_start,
                byte_stop: stop_mapped,
                color: sec.color,
            });
        } else if stop > job_edit_end {
            // starts inside the edit, survives past it — keep the tail
            out.push(code_view::Section {
                byte_start: live_edit_end as u32,
                byte_stop: shift(sec.byte_stop),
                color: sec.color,
            });
        }
        // else: fully inside the replaced region — dropped; gap-fill covers it
    }
    out
}

/// The styled-channel twin of [`reconcile_sections`], with the same edit-region
/// mapping so an overlay stays on the token it was computed for while the user
/// types. Kept separate rather than generic: `Section` and `StyledSection` are
/// different wire structs on purpose (ADR-0130 §Alternatives), and a trait to
/// unify two ten-line loops would cost more than it saves.
fn reconcile_styled(job_text: &str, live: &str, styled: &[StyledSection]) -> Vec<StyledSection> {
    if styled.is_empty() || job_text == live {
        return styled.to_vec();
    }
    let (p, job_edit_end, live_edit_end) = edit_region(job_text, live);
    let delta = live_edit_end as i64 - job_edit_end as i64;
    let shift = |x: u32| -> u32 { (x as i64 + delta).max(0) as u32 };

    let mut out = Vec::with_capacity(styled.len());
    for s in styled {
        let (start, stop) = (s.byte_start as usize, s.byte_stop as usize);
        if stop <= p {
            out.push(*s);
        } else if start >= job_edit_end {
            out.push(StyledSection {
                byte_start: shift(s.byte_start),
                byte_stop: shift(s.byte_stop),
                ..*s
            });
        } else if start < p {
            let stop_mapped = if stop >= job_edit_end {
                shift(s.byte_stop)
            } else {
                live_edit_end as u32
            };
            out.push(StyledSection {
                byte_start: s.byte_start,
                byte_stop: stop_mapped,
                ..*s
            });
        } else if stop > job_edit_end {
            out.push(StyledSection {
                byte_start: live_edit_end as u32,
                byte_stop: shift(s.byte_stop),
                ..*s
            });
        }
        // else: fully inside the replaced region — dropped. Unlike the color
        // tier there is no gap-fill to cover it; the bytes simply lose their
        // overlay until the next frame's list arrives.
    }
    out
}

/// Common-prefix / common-suffix diff shared by both reconcile passes.
/// Returns `(prefix_len, job_edit_end, live_edit_end)`.
fn edit_region(job_text: &str, live: &str) -> (usize, usize, usize) {
    let a = job_text.as_bytes();
    let b = live.as_bytes();
    let mut p = 0usize;
    let max_p = a.len().min(b.len());
    while p < max_p && a[p] == b[p] {
        p += 1;
    }
    let mut s = 0usize;
    let max_s = (a.len() - p).min(b.len() - p);
    while s < max_s && a[a.len() - 1 - s] == b[b.len() - 1 - s] {
        s += 1;
    }
    (p, a.len() - s, b.len() - s)
}

/// Styled normalization: clamp to the live buffer, round to char boundaries,
/// drop inverted or empty ranges, sort by start. Deliberately **no** gap-fill
/// and no overlap merge — styled sections are sparse overlays, and two
/// overlapping ones are a legitimate composition (an error underline inside a
/// tinted statement).
fn resolve_styled(live: &str, styled: &[StyledSection]) -> Vec<StyledSection> {
    if live.is_empty() {
        return Vec::new();
    }
    let mut out = Vec::with_capacity(styled.len());
    for s in styled {
        let start = floor_char_boundary(live, (s.byte_start as usize).min(live.len()));
        let stop = floor_char_boundary(live, (s.byte_stop as usize).min(live.len()));
        if stop <= start || s.flags == 0 {
            continue;
        }
        out.push(StyledSection {
            byte_start: start as u32,
            byte_stop: stop as u32,
            ..*s
        });
    }
    out.sort_by_key(|s| (s.byte_start, s.byte_stop));
    out
}

/// Rounds `x` down to the nearest UTF-8 char boundary of `live`.
fn floor_char_boundary(live: &str, mut x: usize) -> usize {
    if x >= live.len() {
        return live.len();
    }
    while x > 0 && !live.is_char_boundary(x) {
        x -= 1;
    }
    x
}

/// Normalization: ascending, non-overlapping, char-boundary-aligned sections
/// with default-format gap-fill so every byte of `live` is covered exactly
/// once. Tolerates any garbage input (inverted, overlapping, out of range).
fn resolve_sections(live: &str, sections: &[code_view::Section]) -> Vec<Resolved> {
    if live.is_empty() {
        return Vec::new();
    }
    let mut sorted: Vec<&code_view::Section> = sections.iter().collect();
    sorted.sort_by_key(|s| (s.byte_start, s.byte_stop));

    let mut out = Vec::with_capacity(sorted.len() + 8);
    let mut cursor = 0usize; // always a char boundary
    for sec in sorted {
        let mut start = (sec.byte_start as usize).max(cursor).min(live.len());
        let mut stop = (sec.byte_stop as usize).min(live.len());
        if start >= stop {
            continue;
        }
        start = floor_char_boundary(live, start).max(cursor);
        stop = floor_char_boundary(live, stop);
        if stop <= start {
            continue;
        }
        if start > cursor {
            out.push(Resolved {
                start: cursor,
                stop: start,
                color: None,
            });
        }
        out.push(Resolved {
            start,
            stop,
            color: Some(sec.color),
        });
        cursor = stop;
    }
    if cursor < live.len() {
        out.push(Resolved {
            start: cursor,
            stop: live.len(),
            color: None,
        });
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sec(start: u32, stop: u32, tag: u8) -> code_view::Section {
        code_view::Section {
            byte_start: start,
            byte_stop: stop,
            color: egui::Color32::from_gray(tag),
        }
    }

    /// Full-coverage + boundary invariants every resolve must satisfy.
    fn assert_covering(live: &str, resolved: &[Resolved]) {
        let mut cursor = 0usize;
        for r in resolved {
            assert_eq!(r.start, cursor, "gap or overlap at {}", r.start);
            assert!(r.stop > r.start, "empty section at {}", r.start);
            assert!(live.is_char_boundary(r.start), "start not a boundary");
            assert!(live.is_char_boundary(r.stop), "stop not a boundary");
            cursor = r.stop;
        }
        assert_eq!(cursor, live.len(), "coverage must end at len");
    }

    #[test]
    fn exact_match_passthrough() {
        let text = "SELECT a";
        let secs = vec![sec(0, 6, 1), sec(6, 7, 2), sec(7, 8, 3)];
        let rec = reconcile_sections(text, text, &secs);
        assert_eq!(rec.len(), 3);
        let res = resolve_sections(text, &rec);
        assert_covering(text, &res);
        assert_eq!(res.len(), 3);
        assert!(res.iter().all(|r| r.color.is_some()));
    }

    #[test]
    fn insertion_inside_keyword_stretches_it() {
        // "SELECT a" -> user typed 'X' inside the keyword: "SELEXCT a"
        let job = "SELECT a";
        let live = "SELEXCT a";
        let secs = vec![sec(0, 6, 1), sec(6, 7, 2), sec(7, 8, 3)];
        let rec = reconcile_sections(job, live, &secs);
        let res = resolve_sections(live, &rec);
        assert_covering(live, &res);
        // keyword section stretched over the typed char: 0..7 with tag 1
        assert_eq!(
            res[0],
            Resolved {
                start: 0,
                stop: 7,
                color: Some(egui::Color32::from_gray(1))
            }
        );
        // trailing identifier shifted to 8..9
        assert_eq!(res.last().unwrap().start, 8);
        assert_eq!(res.last().unwrap().color, Some(egui::Color32::from_gray(3)));
    }

    #[test]
    fn append_at_end_gap_fills_tail() {
        let job = "SELECT";
        let live = "SELECT f";
        let secs = vec![sec(0, 6, 1)];
        let rec = reconcile_sections(job, live, &secs);
        let res = resolve_sections(live, &rec);
        assert_covering(live, &res);
        assert_eq!(res[0].stop, 6);
        assert_eq!(res.last().unwrap().color, None); // typed tail is gap-filled
    }

    #[test]
    fn deletion_shifts_suffix() {
        // delete the 'a' of "SELECT ab": job describes 9 bytes, live has 8
        let job = "SELECT ab";
        let live = "SELECT b";
        let secs = vec![sec(0, 6, 1), sec(6, 7, 2), sec(7, 9, 3)];
        let rec = reconcile_sections(job, live, &secs);
        let res = resolve_sections(live, &rec);
        assert_covering(live, &res);
        // identifier section survives, shifted/trimmed into 7..8
        let last = res.last().unwrap();
        assert_eq!((last.start, last.stop), (7, 8));
        assert_eq!(last.color, Some(egui::Color32::from_gray(3)));
    }

    #[test]
    fn utf8_boundary_rounding_never_splits_chars() {
        // live contains a 3-byte char; craft sections that land mid-char
        let live = "a€b"; // bytes: a(1) € (3) b(1) => len 5
        let secs = vec![sec(0, 2, 1), sec(2, 3, 2), sec(3, 5, 3)];
        let res = resolve_sections(live, &secs);
        assert_covering(live, &res);
    }

    #[test]
    fn garbage_sections_degrade_to_coverage() {
        let live = "SELECT 1";
        let secs = vec![sec(5, 2, 1), sec(100, 200, 2), sec(3, 3, 3), sec(4, 90, 4)];
        let res = resolve_sections(live, &secs);
        assert_covering(live, &res);
    }

    #[test]
    fn empty_live_yields_no_sections() {
        assert!(resolve_sections("", &[sec(0, 5, 1)]).is_empty());
        let rec = reconcile_sections("SELECT", "", &[sec(0, 6, 1)]);
        let res = resolve_sections("", &rec);
        assert!(res.is_empty());
    }

    #[test]
    fn empty_job_gap_fills_everything() {
        let live = "SELECT 1";
        let res = resolve_sections(live, &[]);
        assert_covering(live, &res);
        assert_eq!(res.len(), 1);
        assert_eq!(res[0].color, None);
    }

    fn styled(start: u32, stop: u32, flags: u32) -> StyledSection {
        StyledSection {
            byte_start: start,
            byte_stop: stop,
            flags,
            color: egui::Color32::RED,
        }
    }

    // --- styled channel (ADR-0130 L3) ---

    #[test]
    fn styled_shifts_across_an_edit_region() {
        // underline on the trailing identifier of "SELECT a"; a char is typed
        // into the keyword, so the overlay must follow its token right.
        let job = "SELECT a";
        let live = "SELEXCT a";
        let rec = reconcile_styled(job, live, &[styled(7, 8, STYLE_UNDERLINE)]);
        let res = resolve_styled(live, &rec);
        assert_eq!(res.len(), 1);
        assert_eq!((res[0].byte_start, res[0].byte_stop), (8, 9));
        assert_eq!(&live[8..9], "a");
    }

    #[test]
    fn styled_clamps_to_utf8_boundaries() {
        let live = "a€b"; // a(1) €(3) b(1)
        // 2 and 3 both land inside the 3-byte char — floor to 1 and 1 → empty,
        // so a range that only covers a partial char drops rather than splitting.
        let res = resolve_styled(live, &[styled(2, 3, STYLE_UNDERLINE)]);
        assert!(res.is_empty());
        // a range that reaches past the char keeps whole chars only
        let res = resolve_styled(live, &[styled(0, 3, STYLE_UNDERLINE)]);
        assert_eq!((res[0].byte_start, res[0].byte_stop), (0, 1));
        for r in &res {
            assert!(live.is_char_boundary(r.byte_start as usize));
            assert!(live.is_char_boundary(r.byte_stop as usize));
        }
    }

    #[test]
    fn styled_overlapping_the_edit_stretches_across_it() {
        // background tint over the whole statement; the user types inside it
        let job = "SELECT a";
        let live = "SELEXCT a";
        let rec = reconcile_styled(job, live, &[styled(0, 8, STYLE_BACKGROUND)]);
        let res = resolve_styled(live, &rec);
        assert_eq!(res.len(), 1);
        assert_eq!((res[0].byte_start, res[0].byte_stop), (0, 9));
    }

    #[test]
    fn styled_append_past_the_end_is_not_stretched() {
        // Typing past a section's trailing boundary leaves the new bytes
        // unstyled for one frame — the same call the color tier makes for its
        // gap-fill (see append_at_end_gap_fills_tail). Stretching here would
        // grow a statement tint over text the next frame will re-scope anyway.
        let rec = reconcile_styled("SELECT a", "SELECT ab", &[styled(0, 8, STYLE_BACKGROUND)]);
        let res = resolve_styled("SELECT ab", &rec);
        assert_eq!((res[0].byte_start, res[0].byte_stop), (0, 8));
    }

    #[test]
    fn styled_does_not_gap_fill() {
        // one overlay in the middle of an 8-byte buffer yields exactly one
        // section — no default-styled filler either side (the color tier's
        // gap-fill invariant deliberately does not apply here).
        let live = "SELECT 1";
        let res = resolve_styled(live, &[styled(3, 5, STYLE_UNDERLINE)]);
        assert_eq!(res.len(), 1);
        assert_eq!((res[0].byte_start, res[0].byte_stop), (3, 5));
    }

    #[test]
    fn styled_empty_list_is_a_no_op() {
        let live = "SELECT 1";
        assert!(resolve_styled(live, &[]).is_empty());
        assert!(reconcile_styled("SELECT", live, &[]).is_empty());
        // and an empty styled list leaves the color sections' formats untouched
        let mut format = egui::TextFormat::default();
        apply_styles(&mut format, 0, 8, &[]);
        assert_eq!(format, egui::TextFormat::default());
    }

    #[test]
    fn styled_garbage_degrades_to_nothing() {
        let live = "SELECT 1";
        let secs = vec![
            styled(5, 2, STYLE_UNDERLINE),   // inverted
            styled(100, 200, STYLE_ITALICS), // out of range
            styled(3, 3, STYLE_BACKGROUND),  // empty
            styled(2, 4, 0),                 // no flags — nothing to express
        ];
        assert!(resolve_styled(live, &secs).is_empty());
    }

    #[test]
    fn styled_sorted_and_overlaps_compose() {
        // an error underline inside a tinted statement: both survive, sorted,
        // and both reach a color section that lies inside the two.
        let live = "SELECT 1";
        let res = resolve_styled(
            live,
            &[styled(3, 5, STYLE_UNDERLINE), styled(0, 8, STYLE_BACKGROUND)],
        );
        assert_eq!(res.len(), 2);
        assert_eq!(res[0].byte_start, 0);
        let mut format = egui::TextFormat::default();
        apply_styles(&mut format, 3, 5, &res);
        assert_ne!(format.underline, egui::Stroke::NONE);
        assert_eq!(format.background, egui::Color32::RED);
    }

    #[test]
    fn styles_apply_only_to_overlapping_color_sections() {
        let live = "SELECT 1";
        let res = resolve_styled(live, &[styled(0, 6, STYLE_UNDERLINE)]);
        // the keyword's own color section overlaps
        let mut hit = egui::TextFormat::default();
        apply_styles(&mut hit, 0, 6, &res);
        assert_ne!(hit.underline, egui::Stroke::NONE);
        // the trailing literal's does not
        let mut miss = egui::TextFormat::default();
        apply_styles(&mut miss, 7, 8, &res);
        assert_eq!(miss.underline, egui::Stroke::NONE);
    }

    #[test]
    fn styled_inside_the_replaced_region_is_dropped() {
        // the underlined token is entirely retyped
        let job = "SELECT abc";
        let live = "SELECT xyz";
        let rec = reconcile_styled(job, live, &[styled(7, 10, STYLE_UNDERLINE)]);
        assert!(resolve_styled(live, &rec).is_empty());
    }

    #[test]
    fn multiline_insert_shifts_following_lines() {
        let job = "SELECT a\nFROM t";
        let live = "SELECT a, b\nFROM t";
        let secs = vec![sec(0, 6, 1), sec(7, 8, 2), sec(9, 13, 4), sec(14, 15, 5)];
        let rec = reconcile_sections(job, live, &secs);
        let res = resolve_sections(live, &rec);
        assert_covering(live, &res);
        // "FROM" keeps its color, shifted by +3
        assert!(res.iter().any(|r| r.start == 12
            && r.stop == 16
            && r.color == Some(egui::Color32::from_gray(4))));
    }
}
