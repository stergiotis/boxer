use std::collections::HashMap;
use std::hash::{Hash, Hasher};

/// A single colored section within a code view.
/// Corresponds to one syntax-highlighted token span.
#[derive(Clone, Debug)]
pub struct Section {
    pub byte_start: u32,
    pub byte_stop: u32,
    pub color: egui::Color32,
}

/// Accumulated data from the CodeViewJob evaluated argument.
/// Populated by the FFFI interpreter, then consumed by the CodeView widget.
#[derive(Clone, Debug, Default)]
pub struct CodeViewJobData {
    pub text: String,
    pub sections: Vec<Section>,
}

/// Cache entry storing a pre-built LayoutJob to avoid reconstruction each frame.
struct CacheEntry {
    text_hash: u64,
    sections_len: usize,
    layout_job: egui::text::LayoutJob,
}

/// Content-addressed cache for LayoutJob instances.
/// Keyed by a hash of (text, sections) to skip LayoutJob construction
/// when the highlighted content hasn't changed.
pub struct CodeViewCache {
    entries: HashMap<u64, CacheEntry>,
}

impl CodeViewCache {
    pub fn new() -> Self {
        Self {
            entries: HashMap::new(),
        }
    }
}

/// Compute a content hash over text and section descriptors.
fn content_hash(job: &CodeViewJobData) -> u64 {
    let mut hasher = std::hash::DefaultHasher::new();
    job.text.hash(&mut hasher);
    job.sections.len().hash(&mut hasher);
    for s in &job.sections {
        s.byte_start.hash(&mut hasher);
        s.byte_stop.hash(&mut hasher);
        let c = s.color.to_array();
        c.hash(&mut hasher);
    }
    hasher.finish()
}

/// Build a LayoutJob from text and colored sections.
fn build_layout_job(job: &CodeViewJobData, ctx: &egui::Context) -> egui::text::LayoutJob {
    let font_id =
        egui::FontId::monospace(ctx.style().text_styles[&egui::TextStyle::Monospace].size);

    let mut layout_job = egui::text::LayoutJob {
        text: job.text.clone(),
        break_on_newline: true,
        wrap: egui::text::TextWrapping::no_max_width(),
        ..Default::default()
    };

    if job.sections.is_empty() {
        // No sections — render entire text as default monospace
        layout_job.sections.push(egui::text::LayoutSection {
            leading_space: 0.0,
            byte_range: 0..job.text.len(),
            format: egui::TextFormat::simple(font_id, ctx.style().visuals.text_color()),
        });
        return layout_job;
    }

    layout_job.sections.reserve(job.sections.len());
    for section in &job.sections {
        let start = section.byte_start as usize;
        let stop = (section.byte_stop as usize).min(job.text.len());
        if start >= stop {
            continue;
        }
        layout_job.sections.push(egui::text::LayoutSection {
            leading_space: 0.0,
            byte_range: start..stop,
            format: egui::TextFormat {
                font_id: font_id.clone(),
                color: section.color,
                ..Default::default()
            },
        });
    }

    layout_job
}

/// Returns a (potentially cached) LayoutJob for the given CodeViewJobData.
///
/// Cache lookup is O(1) by content hash. On miss, builds the LayoutJob
/// and stores it. On hit, clones the cached job (LayoutJob is a plain
/// data struct, cloning is cheap compared to font resolution).
pub fn get_or_build_layout_job(
    cache: &mut CodeViewCache,
    job: &CodeViewJobData,
    ctx: &egui::Context,
) -> egui::text::LayoutJob {
    let hash = content_hash(job);

    if let Some(entry) = cache.entries.get(&hash) {
        if entry.text_hash == hash && entry.sections_len == job.sections.len() {
            return entry.layout_job.clone();
        }
    }

    let layout_job = build_layout_job(job, ctx);
    cache.entries.insert(
        hash,
        CacheEntry {
            text_hash: hash,
            sections_len: job.sections.len(),
            layout_job: layout_job.clone(),
        },
    );
    layout_job
}
