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
//! Text stays authoritative in the TextEdit; everything here is presentation.
//! Galley memoisation is egui's own (`Fonts` caches by job hash) — no cache
//! lives here.

use crate::imzero2::code_view;

/// Builds the `TextEdit::layouter` closure for one apply. The job is moved in;
/// the closure reads the live buffer through its `&dyn TextBuffer` parameter.
pub fn make_layouter(
    job: code_view::CodeViewJobData,
) -> impl FnMut(&egui::Ui, &dyn egui::TextBuffer, f32) -> std::sync::Arc<egui::Galley> {
    move |ui, buf, wrap_width| layout_reconciled(ui, &job, buf.as_str(), wrap_width)
}

fn layout_reconciled(
    ui: &egui::Ui,
    job: &code_view::CodeViewJobData,
    live: &str,
    wrap_width: f32,
) -> std::sync::Arc<egui::Galley> {
    let reconciled = reconcile_sections(&job.text, live, &job.sections);
    let resolved = resolve_sections(live, &reconciled);

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
        layout_job.sections.push(egui::text::LayoutSection {
            leading_space: 0.0,
            byte_range: egui::text::ByteIndex(r.start)..egui::text::ByteIndex(r.stop),
            format: egui::TextFormat {
                font_id: font_id.clone(),
                color: r.color.unwrap_or(default_color),
                ..Default::default()
            },
        });
    }

    ui.fonts_mut(|f| f.layout_job(layout_job))
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
    let job_edit_end = a.len() - s; // >= p
    let live_edit_end = b.len() - s; // >= p
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
        assert_eq!(res[0], Resolved { start: 0, stop: 7, color: Some(egui::Color32::from_gray(1)) });
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

    #[test]
    fn multiline_insert_shifts_following_lines() {
        let job = "SELECT a\nFROM t";
        let live = "SELECT a, b\nFROM t";
        let secs = vec![sec(0, 6, 1), sec(7, 8, 2), sec(9, 13, 4), sec(14, 15, 5)];
        let rec = reconcile_sections(job, live, &secs);
        let res = resolve_sections(live, &rec);
        assert_covering(live, &res);
        // "FROM" keeps its color, shifted by +3
        assert!(res
            .iter()
            .any(|r| r.start == 12 && r.stop == 16 && r.color == Some(egui::Color32::from_gray(4))));
    }
}
