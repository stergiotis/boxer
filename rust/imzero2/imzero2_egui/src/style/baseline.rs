//! One baseline for a row of mixed sizes.
//!
//! epaint places a row's glyphs from the row's top: a glyph's `y` is its
//! own font's ascent, plus the format's `valign` share of the difference
//! between the row's height and the section's line height. Two sections
//! at one line height — prose and the inline code span set at 0.9 of it,
//! both carrying the design system's leading — therefore share a top and
//! not a baseline, and the smaller run sits a few pixels high. There is
//! no baseline alignment to ask for; `valign` only moves a section whose
//! line height differs from the row's.
//!
//! [`align`] makes that the case on purpose. The section with the tallest
//! line height is the row's reference; every other section is set to
//! `valign: Max` with a line height short of the reference's by the
//! difference of the two ascents, so the bottom-aligned shorter box lands
//! its baseline on the reference's. A job whose sections share one font
//! is left alone.
//!
//! A row made of shorter sections alone — a wrapped row that is one code
//! span — is as tall as their line height, a few pixels short of the
//! reference's; the line step of that one row is the cost of a baseline
//! on every other.

use egui::text::LayoutJob;
use egui::{Align, FontId, Ui};

/// Aligns the baselines of `job`'s sections to the section with the
/// tallest line height. See the module doc.
pub fn align(ui: &Ui, job: &mut LayoutJob) {
    if job.sections.len() < 2 {
        return;
    }
    let first = &job.sections[0].format.font_id;
    if job.sections.iter().all(|s| s.format.font_id == *first) {
        return;
    }
    let ppp = ui.pixels_per_point();
    let metrics: Vec<(f32, f32)> = ui.fonts_mut(|fonts| {
        job.sections
            .iter()
            .map(|s| {
                let (ascent, row_height) = ascent_and_row_height(fonts, &s.format.font_id, ppp);
                (ascent, s.format.line_height.unwrap_or(row_height))
            })
            .collect()
    });
    let Some((reference, _)) = metrics.iter().enumerate().max_by(|a, b| a.1.1.total_cmp(&b.1.1))
    else {
        return;
    };
    let (ascent_ref, lh_ref) = metrics[reference];
    for (i, section) in job.sections.iter_mut().enumerate() {
        let (ascent, lh) = metrics[i];
        if (ascent - ascent_ref).abs() < 0.01 && (lh - lh_ref).abs() < 0.01 {
            continue;
        }
        section.format.valign = Align::Max;
        section.format.line_height = Some(lh_ref - (ascent_ref - ascent));
    }
}

fn ascent_and_row_height(
    fonts: &mut egui::epaint::text::FontsView<'_>,
    id: &FontId,
    ppp: f32,
) -> (f32, f32) {
    let m = fonts.fonts.font(&id.family).styled_metrics(ppp, id.size, &Default::default());
    (m.ascent, m.row_height)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::style::pill::section_runs;
    use egui::{FontFamily, TextFormat};

    fn mixed_job() -> LayoutJob {
        let body = TextFormat {
            font_id: FontId::new(13.0, FontFamily::Proportional),
            line_height: Some(20.0),
            ..Default::default()
        };
        let code = TextFormat {
            font_id: FontId::new(11.7, FontFamily::Monospace),
            line_height: Some(20.0),
            ..Default::default()
        };
        let mut job = LayoutJob::default();
        job.append("prose and ", 0.0, body.clone());
        job.append("code", 0.0, code);
        job.append(" after", 0.0, body);
        job
    }

    fn baselines(ctx: &egui::Context, job: LayoutJob, aligned: bool) -> Vec<f32> {
        let mut out = Vec::new();
        let output = ctx.run_ui(egui::RawInput::default(), |ui| {
            let mut job = job.clone();
            if aligned {
                align(ui, &mut job);
            }
            let galley = ui.fonts_mut(|f| f.layout_job(job));
            out = section_runs(&galley, egui::Pos2::ZERO).iter().map(|r| r.baseline).collect();
        });
        output.drop_without_applying_deltas();
        out
    }

    #[test]
    fn a_smaller_run_lands_on_the_row_baseline() {
        let ctx = egui::Context::default();
        let before = baselines(&ctx, mixed_job(), false);
        assert_eq!(before.len(), 3);
        // With egui's bundled test faces the gap is one pixel; with the
        // host's faces at the body sizes it is two or three.
        assert!(
            (before[1] - before[0]).abs() >= 1.0,
            "epaint sets the smaller run high: {before:?}"
        );
        // The aligned run's `y` is the reference's ascent exactly, so the
        // rounded baselines coincide.
        let after = baselines(&ctx, mixed_job(), true);
        assert_eq!(after.len(), 3);
        assert!(
            (after[1] - after[0]).abs() < 0.5 && (after[2] - after[0]).abs() < 0.5,
            "one baseline: {after:?}"
        );
    }

    #[test]
    fn a_job_in_one_font_is_left_alone() {
        let ctx = egui::Context::default();
        // Two sections of one font: `append` merges equal formats, so the
        // second differs in colour alone.
        let mut job = LayoutJob::default();
        job.append("one ", 0.0, TextFormat::default());
        job.append(
            "font",
            0.0,
            TextFormat {
                color: egui::Color32::RED,
                ..Default::default()
            },
        );
        assert_eq!(job.sections.len(), 2);
        let before = job.clone();
        ctx.run_ui(egui::RawInput::default(), |ui| align(ui, &mut job))
            .drop_without_applying_deltas();
        assert_eq!(
            job.sections[1].format.line_height,
            before.sections[1].format.line_height
        );
        assert_eq!(
            job.sections[1].format.valign,
            before.sections[1].format.valign
        );
    }
}
