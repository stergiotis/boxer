---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Markdown rendering quality — logbook

Chronological, append-only record of runs of the
[markdown-rendering-quality](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-11-05 — M0, first run — every arm captured and measured; votes pending

- **Build under test:** boxer e659340f8 with the markdown widget, obsidian
  package, IDS web CSS and Rust host unmodified in the tree; headless
  CPU-rasterizer client built 2026-11-01; Chromium 156, Firefox 149 (both
  snap); corpus at that commit plus `harness/corpus/stress.md` as first
  committed.
- **Environment:** `runs/2026-11-05-first/environment.md` — a 32-thread
  desktop-class CPU, 122 GiB, Linux 7.0; viewport 1000 by 1400, device
  scale 1, standard density.
- **Attempted:** build the harness, capture six documents in seven arms,
  compute the line-level metrics, build the blind vote page. Three capture
  rounds were needed before the native arm showed the document's first
  screenful in a full-size window (findings below); the browser arms were
  taken twice, the second time with the native faces pinned and the
  transparent-background reading fixed.
- **Findings:**
  - **[pain boxer/toolbelt/imzero2/headless-driver-and-scenes / operability / S3]**
    The scene runner refuses a headless client older than the generated
    interpreter sources, and the wgpu build on this machine was; the CPU
    rasterizer build was fresh and the runner picks it by itself, so the
    fix was to stop passing `--clientBinary`. The refusal's message was
    exact; the cost was one failed round (evidence: the runner's refusal
    quoted in this entry's compaction; `measure.sh` now passes the flag
    only when `MDQ_CLIENT` is set).
  - **[pain boxer/toolbelt/imzero2/headless-driver-and-scenes / functional-suitability.appropriateness / S3]**
    A scene's `size` is the desktop, not the app's window: mdedit opened as
    a floating window of its default size inside the 1000 by 1400 host,
    leaving a 620-point preview. `IMZERO2_DESKTOP_LAYOUT: tile` in the
    scene's env makes the one window fill the desktop; the README's scene
    examples do not say so (evidence: the first-round capture, replaced).
  - **[pain boxer/toolbelt/widgets/markdown-editing / operability / S3]**
    mdedit's preview follows the source caret, and a document seeded through
    `BOXER_MDEDIT_DOC` leaves the caret at its end, so the reading mode
    opened scrolled to the last screenful; Ctrl+Home in the source pane did
    not move it, `scroll_into_view` on the first heading in reading mode
    did (evidence: the second-round capture, replaced; the generated scene's
    trace).
  - **[broken boxer/toolbelt/sync-and-markdown/markdown-ingestion → proposed:boxer/toolbelt/sync-and-markdown/markdown-html / functional-correctness / S3]**
    `obsidian.css` declares `--ob-font-body` and no rule reads it, so the
    package's default HTML renders in the browser's default serif under both
    engines (evidence: `captures/stress-obsidian-chromium.png` in the work
    directory; the stylesheet's one occurrence of the variable).
  - **[pain boxer/practice/verifying/headless-scenes / portability / S4]**
    Snap-confined Chromium and Firefox refuse `file://` pages under `/tmp`,
    the agent scratchpad and hidden dot-directories, and Firefox answers
    "already running" while a desktop instance is up; a visible folder
    under the home and `--no-remote --new-instance --profile` fixed both
    (evidence: `measure.sh`'s browsers step and its comment).
  - **[pain proposed:boxer/toolbelt/widgets/markdown-rendering / functional-suitability.completeness / S4]**
    The IDS web stylesheet names no face the host is known to have, so a
    like-for-like comparison with the native arm needs the harness to pin
    Noto Sans and DejaVu Sans Mono by `@font-face` — the faces the scene
    runner hands the headless host, not the Iosevka Aile and IDS Mono the
    desktop build embeds (evidence: `font-family` attributes of
    `captures/stress-native.svg`; `scene/launch.go`'s font flags).
  - **[maturity boxer/toolbelt/imzero2/headless-driver-and-scenes]** Positive:
    the `svg` sidecar carries one `x` per glyph with size, family and fill,
    and the `tree` sidecar located the mode bar, so the native arm's
    geometry needed no change to the host.
  - **[maturity boxer/toolbelt/sync-and-markdown/markdown-ingestion]** Positive:
    the obsidian package rendered all six documents with every wired
    feature on and no errors; frontmatter parsed and left out, as the
    reading mode does.
- **The agent's reading of the captures** (not blind; the owner's votes are
  M1): the native arm sets prose across the whole 950-point column at 150
  characters a line and a tighter leading than any reference; its ordered
  list markers sit in a monospace column with a wide gap to the text, and
  task checkboxes touch their text with no gap; table cells clip to one line
  with an ellipsis ("hover…", "frame…", "this c…") where every reference
  wraps the cell, and the native table header is set larger than its body
  rows; blockquotes are full-width framed groups where every reference
  draws a left bar; footnote markers are bracketed numbers rather than
  superscripts. Heading rhythm, inline styling, code blocks and callouts
  read comparably to the references.
- **Solution size:** `harness/` — four Go files (prepare, metrics, vote,
  harness_test; about 1,400 lines), `geometry.js` (60 lines), one corpus
  document, one vendored stylesheet; `measure.sh` (100 lines). No change to
  the tree outside the trial directory and the third-party notices.
- **Results:** `runs/2026-11-05-first/metrics.json` and `metrics.md`
  (six documents, seven arms, four with geometry). Votes: none this run.
- **Run dir:** `./runs/2026-11-05-first/`.

## 2026-11-05 — M1, the owner's blind votes — the native arm lost 17 of 18 pairs; the controlled pair lost 6 of 6

- **Build under test:** as the M0 entry; the same captures.
- **Environment:** as the M0 entry; the vote page under Firefox on the
  desktop, one sitting.
- **Attempted:** 24 blind pairs — per document, the native arm against each
  stylesheet under Chromium, and IDS against GitHub — one question, tie
  allowed, no notes left.
- **Findings:**
  - **[maturity boxer/practice/measuring/trials]** Positive: the opaque
    picture names, the key held outside the page and the tally step gave a
    blind vote with no manual bookkeeping; the ballots resolved to arms
    without a mismatch.
  - **Result, not a toolbelt finding:** native 0 wins, 1 tie, 17 losses;
    IDS 6–0 over native, GitHub 6–0 over native, Obsidian 5–0–1 over
    native; IDS against GitHub 2–2–2. The IDS pair holds fonts, body size
    and theme equal to the native arm, so the preference is layout; the
    IDS–GitHub split says theme and font did not move the voter.
- **Solution size:** no change.
- **Results:** `runs/2026-11-05-first/votes.json` and `votes.md`.
- **Run dir:** `./runs/2026-11-05-first/`.

## 2026-11-05 — second run, `2026-11-05-column` — the reading column lands; the native measure falls from 73 em to 50 em

- **Build under test:** boxer e659340f8 plus, in the working tree, the
  markdown widget's new `Input.Measure` option and mdedit's reading mode
  passing `ReadingMeasure()` (50 em at the active density, ADR-0076's
  column); otherwise as the first run. The reference arms are unchanged and
  their numbers repeat the first run's to the cell.
- **Environment:** as the first run (`runs/2026-11-05-column/environment.md`
  records the widget as modified in the tree).
- **Attempted:** the one change the first run's controlled pair pointed at —
  bound and centre the native reading column — then the whole protocol
  again, so that the second run differs from the first in that variable
  alone.
- **Findings:**
  - **[maturity boxer/toolbelt/imzero2/widget-contracts]** Positive: the
    column needed no wire change — a centred layout scope, the existing
    max-width op and a left-aligned scope inside it, none of which takes a
    widget id — and the headless render test covers the new option.
  - **[pain boxer/toolbelt/imzero2/widget-contracts / functional-suitability.appropriateness / S4]**
    egui centres an oversized max width by overflowing both edges, and the
    Go side cannot read the available width, so the option's contract puts
    the clamp on the host: mdedit applies the column in the reading mode
    only, and the Split preview keeps its full pane. A Rust-side clamp is
    the deferred fix (evidence: `Input.Measure`'s doc comment).
  - **Result, not a toolbelt finding:** median characters per full line in
    the native arm 98 to 104 (first run 150 to 155), measure 50 em (73),
    rows and words in the viewport within a few percent of the IDS arm's;
    leading unchanged at 1.38 against the references' 1.50 to 1.60. The
    native arm now sits between the IDS column (47 em, 83 to 96
    characters) and GitHub's (55 to 56 em, 108 to 114).
- **Solution size:** `widgets/markdown/markdown.go` +53 lines (the option,
  `ReadingMeasure`, the wrap), `apps/mdedit/mdedit.go` +6,
  `EXPLANATION.md` +10, `render_test.go` +1.
- **Results:** `runs/2026-11-05-column/metrics.json` and `metrics.md`.
- **Run dir:** `./runs/2026-11-05-column/`.

## 2026-11-05 — second run's blind votes — the native arm goes from 17 losses in 18 to 7 losses and 11 ties

- **Build under test:** as the second run; the same captures.
- **Attempted:** the same 24 pairs, the same question, one sitting, the
  day after the first vote's sitting was not possible, so the same day.
- **Findings:**
  - **Result, not a toolbelt finding:** native 0 wins, 11 ties, 7 losses
    (first run 0, 1, 17). Head to head: IDS 3 wins, 3 ties against native
    (was 6–0); GitHub 1 win, 5 ties (was 6–0); Obsidian 3 wins, 3 ties
    (was 5–0–1). IDS against GitHub: 6 ties (was 2–2–2). The voter tied
    more in this sitting overall — the IDS–GitHub pairs went from two ties
    to six — so part of the native arm's gain is the sitting's; the part
    that is not is that no reference beat the native arm in more than half
    its pairs, where every reference beat it in all or all but one before.
  - **[maturity boxer/practice/measuring/trials]** Positive: the second
    vote repeated the first's mechanics unchanged; the tally joined the
    ballots to the arms without a mismatch.
- **Results:** `runs/2026-11-05-column/votes.json` and `votes.md`.
- **Run dir:** `./runs/2026-11-05-column/`.

## 2026-11-05 — third run, `2026-11-05-pill` — inline backgrounds become pills; votes pending

- **Build under test:** the second run's tree plus, in the working tree,
  `imzero2_egui::style::pill::PillLabel` and the `labelAtoms` IDL snippet
  returning it; the headless CPU-rasterizer client rebuilt from that tree
  and the browser-host digest refreshed. Reference arms unchanged.
- **Environment:** as before, but the machine carried a load average above
  140 from other work during the scenes, which took three to four times the
  earlier runs' wall time; nothing in a capture depends on it.
- **Attempted:** the owner's observation after the second vote — the HTML
  arms round the inline code background and the native arm does not. epaint
  paints a section's background as a flat rectangle with no radius, so the
  host now takes the backgrounds out of the layout job and paints them
  itself behind the galley, as rounded rectangles at the design system's
  inline-chip radius (`tokens::rounding::SM`), for every `labelAtoms` label
  with a background section: code spans, the highlight pen, tag pills, and
  any other consumer's coloured text runs. A job without one is the plain
  `Label`. Then the whole protocol again.
- **Findings:**
  - **[pain boxer/toolbelt/imzero2/hosts / functional-suitability.appropriateness / S4]**
    A glyph's section index is private in epaint 0.36, so the host maps
    glyphs to sections by walking byte offsets against the job's section
    ranges, counting the newline a row ends with (evidence: `pill.rs`, its
    comment and tests).
  - **[maturity boxer/toolbelt/imzero2/hosts]** Positive: the change needed
    no IDL method — the construction snippet returns a widget with the same
    `selectable` and `wrap_mode` surface — and the SVG sidecar carries the
    rounded rectangles (26 `rx` attributes in the stress capture).
  - **Result, not a toolbelt finding:** the line-level metrics repeat the
    second run's to the cell, as they should — a corner radius moves no
    glyph. What changed is in the pictures: `stress-native.crop.png`
    against the second run's.
- **Solution size:** `imzero2_egui/src/style/pill.rs` (new, about 170 lines
  with tests), `style/mod.rs` +1, the IDL snippet +4, `interpreter.rs`
  regenerated, `browserhost.sum` refreshed.
- **Results:** `runs/2026-11-05-pill/metrics.json` and `metrics.md`. Votes:
  pending.
- **Run dir:** `./runs/2026-11-05-pill/`.

## 2026-11-05 — fourth run, `2026-11-05-leading` — prose carries the design system's leading; the native line step goes from 1.38 to 1.54

- **Build under test:** the third run's tree plus, in the working tree,
  `styletokens.BodyLeading` (1.6, mirrored as `BODY_LEADING` in the Rust
  token module and pinned by the drift test), the widget applying it as
  the line height of every prose run at lowering time (rounded to an even
  point: 20 at a 13 pt body, so 1.54), the mixed-run row's vertical item
  spacing at zero, and the task box carrying its own trailing space.
  Reference arms unchanged.
- **Environment:** as before; load average above 140 from other work
  throughout, so the per-frame times in the captures' status bars are not
  comparable with the first run's.
- **Attempted:** the largest line-level difference left after the column,
  by the metrics; then the whole protocol.
- **Findings:**
  - **[pain boxer/toolbelt/imzero2/design-system / functional-suitability.completeness / S4]**
    The Go token module had no leading, only sizes; the IDS web stylesheet
    had `--ids-line: 1.6` and nothing read it on the egui side. Added on
    both sides, which the drift test now pins — and the test compared an
    f32 widened to f64 against a value parsed from Rust source, so 1.6 read
    as drift; it compares at f32 width now (evidence:
    `styletokens_drift_test.go`).
  - **[maturity boxer/toolbelt/imzero2/widget-contracts]** Positive: the
    atoms builder already carried a line height per rich-text run, so the
    leading needed no wire change; plain runs route through the styled
    builder to carry it.
  - **Result, not a toolbelt finding:** native leading 1.54 (was 1.38;
    references 1.50 to 1.60); the gap after a heading reads 1.4 of the new
    line height where it read 1.6 of the old, the same points. Rows in the
    viewport fall from 50 to 47 on the first document. A row that begins
    with punctuation after a wrapped link (`, a footnote marker`) is
    visible in the stress capture: the comma is its own run after the link
    run, and the wrapped row breaks between runs — the review's L3 at a
    row boundary, not closed.
- **Solution size:** `visitor.go` +30, `render.go` +8 −3, tokens +9 on
  each side, drift test +3, `EXPLANATION.md` +5.
- **Results:** `runs/2026-11-05-leading/metrics.json` and `metrics.md`.
  Votes: pending; the owner postponed voting.
- **Run dir:** `./runs/2026-11-05-leading/`.

## 2026-11-05 — fifth run, `2026-11-05-tables` — a flowing table is a grid of wrapping cells

- **Build under test:** the fourth run's tree plus, in the working tree,
  `renderTableGrid`: a table of up to the flow threshold's rows is an egui
  `Grid` of wrapping labels, each bounded to the column estimate the table
  op seeds its columns with, with the delimiter row's alignment as the
  cell's cross alignment and a strong body-size header. A longer table
  keeps the table op and its bounded scroll. Reference arms unchanged.
- **Environment:** as before, under the same load.
- **Attempted:** the construct the review's deliberate list named a
  different op the real fix for — fixed one-line cells, a heading-size
  header — without a new op; then the whole protocol.
- **Findings:**
  - **[maturity boxer/toolbelt/imzero2/widget-contracts]** Positive: the
    grid path is bindings that existed — `Grid`, `EndRow`, a layout scope
    with a cross alignment, a bounded width — and no IDL change.
  - **[pain boxer/doc/trials / functional-suitability.appropriateness / S4]**
    The protocol measures the first viewport, and in every corpus document
    the tables sit below it: the run's metrics repeat the fourth run's to
    the cell and cannot see this change. The evidence is a scene outside
    the protocol, scrolled to the stress document's table, kept as
    `runs/2026-11-06-lists/stress-table.native.png` (captured with the
    sixth run's tree, which did not touch tables). A second anchor per
    document — one construct below the fold — is an open question for the
    protocol (README §7).
  - **[pain boxer/toolbelt/imzero2/hosts / functional-suitability.completeness / S4]**
    An egui grid separates its header from its rows by nothing but the
    stripe phase; the three reference stylesheets rule under the header.
    A rule across a grid row is not a cell, so it waits on a painter pass
    or a host-side grid option. Open.
  - **Result, not a toolbelt finding:** in the picture, the long cell
    wraps onto three lines inside its column and the row grows to hold
    it; the header is body-size strong; centre and right columns align;
    the stripes alternate from the first body row.
- **Solution size:** `render.go` +70 −20 (`renderTableGrid`,
  `renderTableCell`), `EXPLANATION.md` table bullets rewritten.
- **Results:** `runs/2026-11-05-tables/metrics.json` and `metrics.md`
  (unchanged from the fourth run, by the finding above). Votes: pending;
  the owner postponed voting.
- **Run dir:** `./runs/2026-11-05-tables/`.

## 2026-11-06 — sixth run, `2026-11-06-lists` — a list is a two-column grid; numbers right-align in the body face

- **Build under test:** the fifth run's tree plus, in the working tree,
  `renderList` as an egui `Grid` of marker and body columns, the marker
  cell right-aligned so the periods of `9.` and `10.` line up without the
  monospace face and the space padding the fidelity pass chose (ADR-0180
  item 4, withdrawn in its Updates); the marker at the body's line
  height, so marker and body share a baseline; no trailing space in the
  marker, the row's item spacing being the whole gap; and the items of a
  list stepping at the inline gap (6 px at standard density) rather than
  the item gap (8 px). Reference arms unchanged.
- **Environment:** load average below 8 for the whole run — the first
  run on a quiet machine since the first — so the per-frame times in the
  captures' status bars are comparable with the first run's again (Go
  about 1 ms, Rust about 1 ms at the first viewport).
- **Attempted:** the gap after an ordered marker and the two faces in one
  line, which the fourth run's pictures showed against the native arm;
  then the whole protocol.
- **Findings:**
  - **[maturity boxer/toolbelt/imzero2/widget-contracts]** Positive: the
    grid, a cross-aligned layout scope and `EndRow` were enough; the
    marker column's width is the grid's own measurement, so no text
    measuring and no padding. The first frame of a new grid lays the body
    column out at egui's minimum cell width, which egui discards and
    re-lays before painting.
  - **[pain boxer/doc/trials / functional-suitability.appropriateness / S4]**
    The metric's paragraph rule — consecutive prose rows at most 1.3 line
    heights apart are one paragraph — now takes a list's items for one
    paragraph in the native arm, as it already did in the reference arms
    (an IDS list item steps 1.13 line heights). So `fullLines` rises
    (stress 14 → 31) and `cplMedian` falls to the width of a list item
    where the viewport is mostly list (stress 101 → 29; play-overview 99
    → 94, adr-0334 101 → 95), and `raggedness` rises with it, with no
    change to any prose line. The number now compares with the references
    on the same footing — both arms' lists are paragraphs to the metric —
    but not with the earlier runs' native rows. Open question 4 (the line
    model taking the HTML tag, and the tree sidecar's roles, where it has
    one) is what closes this; until then a line-length claim excludes the
    list-heavy documents.
  - **Result, not a toolbelt finding:** in the captures, every number of
    the eleven-item list ends on one vertical line, the digits are the
    body's, the bodies start at one x across the ten boundary, and a
    wrapped item wraps under its own first line; the bullet lists step
    like the references' (26 px pitch at a 20 px line against the IDS
    arm's 23 px).
- **Solution size:** `render.go` +45 −40, `visitor.go` −20, `markdown.go`
  −1 field, tests −40 +15, `EXPLANATION.md` +6; the ADR-0180 update.
- **Results:** `runs/2026-11-06-lists/metrics.json` and `metrics.md`;
  `stress-table.native.png` there is the fifth run's evidence (its
  entry). Votes: pending; the owner postponed voting.
- **Run dir:** `./runs/2026-11-06-lists/`.

## 2026-11-06 — seventh run, `2026-11-06-quote` — a blockquote is a left rule; the line model learns list and table rows

- **Build under test:** the sixth run's tree plus, in the working tree,
  `renderBlockquote`: a Frame with neither stroke nor fill and a bar
  along its left edge at the strong stroke width in the neutral border
  colour, the body set in by the default padding — the reference
  stylesheets' `border-left` — through a new Frame method on the wire,
  `edgeBarLeft`, painted by the host after the body (bindings
  regenerated, the headless host rebuilt, `browserhost.sum` refreshed).
  Every corpus document opens with a status quote, so the change is in
  every native capture's first viewport. Reference arms unchanged.
- **Environment:** as the sixth run's, quiet machine.
- **Attempted:** the quote, which the pictures showed boxed like a
  callout; then the whole protocol; then, on the sixth run's finding,
  the line model.
- **Findings:**
  - **[pain boxer/toolbelt/imzero2/hosts / functional-suitability.completeness / S4]**
    `egui::Frame` strokes all four sides or none; a one-sided rule is
    not expressible on the wire. Added as a Frame method rather than a
    widget: the body, margins and corner radii are the Frame's, and the
    bar is one painter call after `show`, inside the outer margin
    (evidence: `egui2_definition_d_blocks.go`, `edgeBarLeft`).
  - **[pain boxer/doc/trials / functional-suitability.appropriateness / S3]**
    The sixth run's list artefact closed by tagging rows: the native
    marker run (a bullet, or digits and a period, which the widget draws
    as a run of its own) and, in a browser, any glyph inside a list
    item, plus a wrapped item's continuation rows; list rows leave the
    paragraph metrics and the leading estimate. Excluding them exposed a
    second artefact: a browser table's vertically centred cells put a
    one-line cell half a line from its two-line neighbour, and with the
    list rows gone those half-line steps became the IDS arm's median on
    mdedit-blocks (leading 0.80, `cpl` 29). Table-cell rows are tagged
    and excluded the same way (`geometry.js` marks `li` and `td`/`th`
    descendants). The stress document's arms now agree: native 101/107,
    IDS 96/100, GitHub 115/118 (median/p90 `cpl`), where the sixth run
    read native 29 and the IDS arm's leading as 1.80. The six earlier
    runs' tables keep the old reading (README §4).
  - **Result, not a toolbelt finding:** with lists and tables out, the
    native arm's raggedness is 0.02 to 0.03 on four documents, like the
    references' — and 0.25 on mdedit-blocks and 0.14 on why-boxer, where
    the IDS arm reads 0.03 and 0.02. Those are the documents whose
    paragraphs carry links and inline code: a paragraph with a link is a
    `HorizontalWrapped` of separate widgets and breaks between them (the
    review's L3), which a run-boundary break makes a short line of. The
    largest line-level gap left; not closed here.
- **Solution size:** the IDL +12, `interpreter.rs` regenerated,
  `render.go` +25 −9, `metrics.go` +60, `geometry.js` +2, README §4
  rewritten for the line model, the ADR-0180 update extended.
- **Results:** `runs/2026-11-06-quote/metrics.json` and `metrics.md`
  (the first tables with the `list` and `table` columns). Votes: the
  next entry.
- **Run dir:** `./runs/2026-11-06-quote/`.

## 2026-11-06 — seventh run's blind votes — the native arm loses 12 of 24 where the second run lost 7

- **Build under test:** as the seventh run; the same captures.
- **Attempted:** the same 24 pairs, the same question, one sitting. The
  owner voted this run only; the third to sixth runs' pages stay
  unvoted, so this vote stands for the five changes between the second
  run and this one together.
- **Findings:**
  - **Result, not a toolbelt finding:** native 0 wins, 6 ties, 12 losses
    (second run 0, 11, 7; first run 0, 1, 17). Head to head: IDS 5 wins,
    1 tie against native (second run 3–3); GitHub 3 wins, 3 ties (was
    1–5); Obsidian 4 wins, 2 ties (was 3–3). IDS against GitHub: GitHub
    3 wins, 3 ties (was 6 ties), so this sitting tied less overall than
    the second, and some of the native arm's losses are the sitting's.
    What is not the sitting's: the controlled pair, IDS against native,
    went from a split to five losses in six, while the metrics over the
    same five runs moved the native arm's line step, column and list
    rhythm toward the IDS arm's. The line model measures line length,
    leading and rag; the vote judged something else.
  - **[pain boxer/toolbelt/imzero2/hosts / functional-suitability.appropriateness / S3]**
    The owner's one remark after voting: the inline backgrounds' box.
    epaint's `Glyph::logical_rect` is the row's line height tall, so a
    code span's pill, which the third run rounded, became a full line
    step (21 px at the 1.54 leading) with no side padding — from the
    cap line nearly to the next line's — where the IDS stylesheet's box
    is the 0.9 em mono face's own box with `0.12em 0.35em` of padding
    (crop: `stress`, the "inline code" span, native against IDS). The
    fourth run's leading made the pill taller without a metric noticing,
    since the line model sees glyphs, not backgrounds. Closed in the
    eighth run.
  - **[maturity boxer/practice/measuring/trials]** Positive: a vote on
    one run out of five is a loss of attribution, not of validity; the
    tally and the key are per run, and the page stores its ballots under
    the run's salt, so nothing from the earlier sittings reached this one.
- **Results:** `runs/2026-11-06-quote/votes.json` and `votes.md`.
- **Run dir:** `./runs/2026-11-06-quote/`.

## 2026-11-06 — eighth run, `2026-11-06-pillbox` — an inline pill is the font's box, padded like the stylesheet

- **Build under test:** the seventh run's tree plus `pill.rs` sizing a
  pill from the glyph's `font_ascent` and `font_height` rather than
  `logical_rect`, padded `0.12em 0.35em` of the section's size
  (rounded to whole points), the side padding added to the section's
  `leading_space` and the next section's.
- **Attempted:** the whole protocol, after the seventh run's vote named
  the box.
- **Findings:**
  - **Result, not a toolbelt finding:** the "inline code" span of the
    stress document, native against IDS
    (`stress-inline-code.native-vs-ids.png`): the pill is now the mono
    face's box plus padding, clear of the next line, where the seventh
    run's reached it. The line metrics repeat the seventh run's within
    a character (`mdedit-blocks` native `cpl` 90/105, rag 0.26; the
    others unchanged), since the padding is the one thing that moved.
  - **[pain boxer/toolbelt/imzero2/hosts / functional-suitability.appropriateness / S2]**
    Two differences the crop still shows, not closed here: the native
    code span is set at the body size where every reference sets it at
    0.9 em (`ids.css`, `code { font-size: 0.9em }`), so the mono face
    reads larger than the prose around it; and the native highlight is
    a saturated lavender where the IDS stylesheet's `mark` is the
    warning-subtle amber at primary text. Both are the widget's and the
    theme's choices, not the host's.
  - **[pain boxer/practice/measuring/trials / functional-suitability.completeness / S2]**
    The line model does not see backgrounds, so neither the fourth
    run's taller pills nor this run's fix registers in a metric; the
    vote and the crops are the evidence. A background-box metric
    (box height over font box, side padding in em, from the scene's
    draw list) would have caught the fourth run's regression.
  - **Friction, environment:** `go run` under the harness failed with
    `link: mapping output file failed: disk quota exceeded` — the
    machine's tmpfs is quota'd per user and shared with other sessions'
    scratch; `GOTMPDIR` on the disk is the workaround (not the trial's).
- **Solution size:** `pill.rs` +40 −12; the host rebuilt,
  `browserhost.sum` refreshed; ADR-0180's update extended.
- **Results:** `runs/2026-11-06-pillbox/metrics.json`, `metrics.md`,
  the crop. Votes: not taken — the owner votes the latest run, the sum of the eighth run onward, as the seventh was voted for the sum of the third to the seventh.
- **Run dir:** `./runs/2026-11-06-pillbox/`.

## 2026-11-06 — ninth run, `2026-11-06-codespan` — a code span at 0.9 of its run; the pen in the stylesheet's tint

- **Build under test:** the eighth run's tree plus `codeSpanFontSize`
  (0.9 of the body, or of the heading the span sits in) and the highlight
  palette moved from the accent fill under extreme-dark text to the
  warning-subtle tint under primary text, which `ids.css` gives `mark`.
  No host change.
- **Attempted:** the whole protocol, on the two differences the eighth
  run's crop left.
- **Findings:**
  - **Result, not a toolbelt finding:** the same span, native against IDS
    (`stress-inline-code.native-vs-ids.png`): the mono run is 11.7 px in
    both arms (the SVG sidecar reports the size), the pill the same
    height, the pen the same tint. The line metrics repeat the eighth
    run's (stress native `cpl` 101/107 for 99/107, the rest within a
    character). What the crop still shows: the smaller run sits about a
    pixel above the body's baseline — epaint positions a row's glyphs by
    their own ascent from the row's top, so two sizes on one row share a
    top, not a baseline — and the native strikethrough run is set in the
    body colour where the stylesheet dims `del`. Neither is closed here.
  - **[maturity boxer/toolbelt/imzero2/hosts / functional-suitability.appropriateness / S2]**
    The two renderings of one design system had drifted in their pen:
    `theme.go` reasoned from ADR-0031's accent role, `ids.css` from the
    warning family, and nothing joined them. The trial's controlled pair
    is what caught it; a token-level check between the widget's palette
    and the stylesheet's rules would catch the next one without a run.
- **Solution size:** `visitor.go` +22, `theme.go` ±10, EXPLANATION ±4,
  one test.
- **Results:** `runs/2026-11-06-codespan/metrics.json`, `metrics.md`, the
  crop. Votes: not taken — the owner votes the latest run, the sum of the eighth run onward, as the seventh was voted for the sum of the third to the seventh.
- **Run dir:** `./runs/2026-11-06-codespan/`.

## 2026-11-06 — tenth run, `2026-11-06-links` — a paragraph with links is one galley; the line model learns blocks

- **Build under test:** the ninth run's tree plus `labelAtomsLinked` on
  the wire (`imzero2_egui::style::linked`): a link is an atom of the
  paragraph's own atoms run, styled in the link tone, and the host lays
  the paragraph out as one galley, matches a hover or a click against
  the link sections' row boxes, underlines the hovered link, opens an
  unclaimed link's url itself and reports every click as the link's
  index through the r9 readout. The widget's `runKindLink` is gone; a
  claimed link's `LinkClick` comes from the readout one frame on. The SVG
  exporter wraps an inline link's text run in `<a>` from the zones the
  label pushes.
- **Attempted:** the whole protocol; then, on the result, the line model.
- **Findings:**
  - **[pain boxer/doc/trials / functional-suitability.appropriateness / S3]**
    The seventh run's reading of the native raggedness was wrong. The
    0.25 on mdedit-blocks and 0.14 on why-boxer were not link-bearing
    paragraphs breaking between runs: those viewports hold a table, a
    native table's cells carry no tag (the browser arms' come from
    `td`), and a cell's lines — short, at the line step — were read as a
    ragged paragraph. Two rules close it: every glyph belongs to a block
    (the native text shape, `<g class="imz-text">` in the sidecar; the
    browser's nearest block element, numbered by `geometry.js`) and a
    paragraph never spans two; and a block one of whose lines split at
    a column gap is a table cell's, which tags a native cell's unsplit
    second line. Under that model the native arm reads 0.01 to 0.04 on
    every document (IDS 0.02 to 0.03), in the ninth run's captures as in
    this run's; the ninth run's tables were recomputed (README §4).
  - **Result, not a toolbelt finding:** the link change moves no number
    the model reads in the first viewport. The crop
    (`stress-inline-mixtures.run9-vs-run10.png`) shows what it moves:
    in the ninth run the bare URL ended the second row and its comma
    opened the third alone (L3's detached punctuation); in this run the
    paragraph breaks as text, the URL and its comma together. A
    multi-word link already wrapped inside itself in the ninth run — a
    `Hyperlink` is a wrapping label — so the row-end slack L3 described
    was the one-word case and the punctuation, not whole links moving.
  - **[maturity boxer/toolbelt/imzero2/hosts / functional-suitability.completeness]**
    Positive: the click reaches the Go side. A maintained scene
    (`apps/mdedit/scenes/links.scene.md`) presses a paragraph that is
    one claimed link at its centre and reads mdedit's status line
    ("opening a.md in a new window"), then presses prose and reads it
    unchanged; the host's own test drives a press and release over a
    link zone and beside it (`linked.rs`). The SVG sidecar's `<a>` wrap
    needed the exporter to look at a text run's box, not the shape's,
    and to keep a run's whitespace out of that box — a link's last word
    and the prose's leading space share a run.
  - **Friction:** the first recompute under the block model read every
    browser row as a table cell: `prepare` writes the geometry script
    into the HTML arms, and only `browsers` had been rerun. A harness
    step that embeds another's output should say so in `measure.sh`;
    noted, not changed.
- **Solution size:** `linked.rs` +200 (with tests), `pill.rs` ±60 (the
  row walk shared), the IDL +110, `svgexport.rs` ±40, `render.go` ±40,
  `visitor.go` ±40, `markdown.go` +30, the harness +40, one scene.
- **Results:** `runs/2026-11-06-links/metrics.json`, `metrics.md`, the
  crop. Votes: not taken — the owner votes the latest run, the sum of the eighth run onward, as the seventh was voted for the sum of the third to the seventh.
- **Run dir:** `./runs/2026-11-06-links/`.

## 2026-11-06 — eleventh run, `2026-11-06-bold` — bold text has a weight

- **Build under test:** the tenth run's tree plus a bold face on the
  host: `--boldFontTTF` registers it as `FontFamily::Name("bold")`
  (`typography::FAMILY_BOLD`) ahead of the proportional chain, the IDS
  font install aliases the family to the proportional chain so it
  resolves everywhere, the atoms protocol gains `bold()`, and the widget
  sets a heading in strong colour and bold face, an inline strong run,
  a table header and a callout title in the bold face (`applyWeight`).
  The scene runner passes the system Noto Sans Bold beside Regular. The
  SVG exporter marks the bold family's runs `font-weight="bold"` and
  declares an embedded subset of it at weight 700.
- **Attempted:** the whole protocol.
- **Findings:**
  - **[pain boxer/toolbelt/imzero2/hosts / functional-suitability.appropriateness / S1]**
    The native arm had no bold. egui carries no weight on a text format
    and epaint synthesises none (a TODO in its font module), and the host
    embeds one weight per family, so `Strong()` was egui's strong text
    colour and nothing else: every heading was a larger regular face,
    `**strong**` was the prose around it, and the IDS arm's weight-600
    headings and weight-700 lead-ins had no counterpart. A reading of
    the tenth run's captures found it before the vote did — it is the one
    difference a reader sees without looking for it, on every document.
    The SVG sidecar had been writing `font-weight="bold"` for strong runs
    all along (from the colour match), so a browser showed the sidecar
    bolder than the host showed the screen.
  - **Result, not a toolbelt finding:** the line metrics repeat the
    tenth run's to within two characters (play-overview native `cpl`
    100/102 for 102/104: a bold run is wider). The crop
    (`adr-0334-leadin.run10-vs-run11-vs-ids.png`) shows the change: the
    bullet lead-ins of ADR-0334 in regular weight, in the bold face, and
    in the IDS arm. What the capture also shows, now that the headings
    carry weight: the native ladder was tuned to compensate for its
    absence (H1/H2 at 2.00/1.69 of body against the IDS arm's
    1.69/1.46, the `headings` column), so a bold native heading is
    heavier than the reference's; the twelfth run takes the ladder down.
  - **[maturity boxer/toolbelt/imzero2/hosts / portability.adaptability / S3]**
    A bold face is a fifth font path on every launch surface
    (`font-resolve.sh`, the scene runner, the Go launcher). The browser
    tab's slot table has four entries and was left alone, so the tab
    shows regular weight until a fifth slot is added; the IDS bundle
    still embeds Regular only under ADR-0030's size budget.
- **Solution size:** `typography.rs` +25, `apphost.rs` +25,
  `appconfig.rs` +10, `svgexport.rs` ±15, the IDL +6, `visitor.go` ±25,
  `render.go` ±2, launchers +12, ADR-0030 and ADR-0180 updates.
- **Results:** `runs/2026-11-06-bold/metrics.json`, `metrics.md`, the
  crop. Votes: not taken — the owner votes the latest run, the sum of the eighth run onward, as the seventh was voted for the sum of the third to the seventh.
- **Run dir:** `./runs/2026-11-06-bold/`.

## 2026-11-06 — twelfth run, `2026-11-06-ladder` — the heading ladder comes down; two inks take their role

- **Build under test:** the eleventh run's tree with the heading ladder
  at the stylesheet's steps (`headingFontSizeBase`: display, 19, heading,
  14, body, caption for H1–H6), air under a heading beyond the item gap
  (`headingGapBelow`, the two tiers of the gap above one step down),
  struck text in the disabled ink and prose in a blockquote in the
  secondary ink (`inkFor`, `quoteDepth` on the lowering context).
- **Attempted:** the whole protocol.
- **Findings:**
  - **Result, not a toolbelt finding:** the `headings` column now reads
    1.69 and 1.46 of body on the native arm as on the IDS arm, where the
    eleventh run read 2.00 and 1.69 (why-boxer's H3 1.23 on both), and
    `gap H↓` reads 1.6 line heights on both where the native arm read
    1.4; `gap ↑H` is 2.3 to 2.5 against the IDS arm's 2.5 (3.4 on
    play-overview, whose first heading follows a quote). The line
    metrics otherwise repeat the eleventh run's. The crop
    (`why-boxer-head.run11-vs-run12-vs-ids.png`) shows the quote's
    words in the secondary ink and the title at the display step in
    the bold face, beside the IDS arm.
  - **[pain boxer/toolbelt/imzero2/widgets / functional-suitability.appropriateness / S3]**
    The ladder had been tuned to do a weight's work: with no bold face
    a heading could only be larger, and 26 points of regular Noto Sans
    read as a title where 22 points of the bold face do. A size that
    compensates for a missing face is a constant that goes wrong the day
    the face arrives; the ladder is now the stylesheet's by token, not by
    number, where the token exists.
  - **[maturity boxer/toolbelt/imzero2/widgets / functional-suitability.completeness]**
    A quote's ink needed the lowering to know it was inside a quote — the
    widget colours runs at lowering time and the render path has no
    scoped text-colour op on the wire. A `quoteDepth` on the lowering
    context does it without a new op; a scoped `override_text_color`
    would be the general tool, and is not needed yet.
- **Solution size:** `visitor.go` ±80 (the emitter folded to one style
  pass over four ink cases), `render.go` +18, `theme.go` +8, tests ±40.
- **Results:** `runs/2026-11-06-ladder/metrics.json`, `metrics.md`, the
  crop. Votes: not taken — the owner votes the latest run, the sum of the eighth run onward, as the seventh was voted for the sum of the third to the seventh.
- **Run dir:** `./runs/2026-11-06-ladder/`.

## 2026-11-06 — thirteenth run, `2026-11-06-tables-lists` — the table's header band; bullets step with depth; the nested indent

- **Build under test:** the twelfth run's tree with a table cell as a
  frame carrying the stylesheet's padding and no column gap, the header
  cells filled with the surface tint (`renderTableCell`,
  `tableHeaderBg`); a bullet that steps disc, circle, square with the
  nesting depth the lowering counts (`bulletGlyph`, `listDepth`), the
  circle and square in the mono face; and the list grid's marker column
  at the outer padding's minimum width rather than egui's interact size.
- **Attempted:** the whole protocol, then the native scenes again after
  two fixes the first capture showed.
- **Findings:**
  - **[pain boxer/toolbelt/imzero2/widgets / functional-suitability.appropriateness / S3]**
    The first capture showed the header band broken into two boxes the
    width of their words, and the ordered markers `10.` and `11.`
    wrapped onto two rows. A frame is as wide as its content, so the
    band needed the cell to claim the column's width (`UiSetMinWidth`
    beside the max); and a wrapping label in a grid column reports its
    wrapped width back to the grid and stays wrapped, so once the
    column's minimum came down the two-digit markers folded — a marker
    now extends (`Extend()`). Both are the kind of thing a capture
    shows and a test of the lowering cannot.
  - **Result, not a toolbelt finding:** the crops
    (`mdedit-blocks-table.run12-vs-run13-vs-ids.png`,
    `stress-lists.run12-vs-run13-vs-ids.png`) show the header band and
    the stepped bullets beside the IDS arm, and a nested item starting
    about 1.5 em in from its parent's text where it started at twice
    that. The line metrics repeat the twelfth run's; the list rows of
    the stress document read 18 on the native arm for 22 on the IDS
    arm, which is the marker column's width moving a row's split.
- **Solution size:** `render.go` ±60, `visitor.go` +8, `markdown.go`
  +1, `theme.go` +4, one test.
- **Results:** `runs/2026-11-06-tables-lists/metrics.json`,
  `metrics.md`, the crops. Votes: not taken — the owner votes the latest run, the sum of the eighth run onward, as the seventh was voted for the sum of the third to the seventh.
- **Run dir:** `./runs/2026-11-06-tables-lists/`.

## 2026-11-06 — fourteenth run, `2026-11-06-baseline` — a row's runs share a baseline

- **Build under test:** the thirteenth run's tree plus
  `imzero2_egui::style::baseline::align`, run by the pill label and the
  linked label on their job before layout: the section with the tallest
  line height is the reference, every other section is bottom-aligned
  with a line height short of the reference's by the difference of the
  ascents, so its baseline lands on the reference's.
- **Attempted:** the whole protocol.
- **Findings:**
  - **[pain boxer/toolbelt/imzero2/hosts / functional-suitability.appropriateness / S2]**
    epaint has no baseline alignment: a glyph's `y` is its own font's
    ascent from the row's top, and `valign` moves a section only by the
    difference between the row's height and the section's line height.
    Prose and the inline code span at 0.9 of it, both at the design
    system's leading, shared a top, and the span sat two to three pixels
    high — logged after the ninth run, named by the owner after the
    thirteenth as a bug. The fix makes `valign` do the work by giving the
    shorter section a line height computed from the two ascents; it is
    the host's, so every job the widget lays out gets it, including a
    paragraph with links. A row made only of shorter runs is a few
    pixels short; none of the corpus documents has one in the first
    viewport.
  - **Result, not a toolbelt finding:** the crop
    (`stress-inline-code.run13-vs-run14-vs-ids.png`) shows the span on
    the baseline, with the pill following it; the line metrics repeat
    the thirteenth run's to the cell, since the row heights are unchanged.
- **Solution size:** `baseline.rs` +150 (with tests), `pill.rs` ±30,
  `linked.rs` ±25, ADR-0180 update.
- **Results:** `runs/2026-11-06-baseline/metrics.json`, `metrics.md`,
  the crop. Votes: the owner votes the latest run; see the thirteenth.
- **Run dir:** `./runs/2026-11-06-baseline/`.

## 2026-11-06 — the fourteenth run's vote: 24 ties

- **What was done:** the owner voted the fourteenth run, `2026-11-06-baseline`,
  blind, the same 24 pairs as the second and seventh runs, and reported
  every pair a tie; no `votes.json` was exported, so the run directory
  carries the report and not a tally. Against the seventh run's vote
  (0 wins, 6 ties, 12 losses) the seven changes between them — the pill
  box, the code span's size and the pen's tint, links in one galley,
  the bold face, the ladder and the inks, the table band and list
  markers, one baseline per row — move the native arm from losing
  twelve of twenty-four to losing none.
- **Findings:**
  - **[maturity boxer/toolbelt/imzero2/widgets / functional-suitability.appropriateness]**
    Positive: a blind vote no longer tells the native reading mode from
    a browser rendering of the same document under the design system's
    own stylesheet, GitHub's or an Obsidian-like one, in the first
    viewport at one width. What the vote cannot say is which of the
    seven changes did it; the crops and metrics of runs eight to
    fourteen are the record of each.
  - **[pain boxer/keelson/designsystem / functional-suitability.appropriateness / S3]**
    The one thing the owner named: the highlight pen's colours read as
    low in contrast. Measured in the captures, the pen is the same in
    the native and the IDS arm — the warning family's subtle tint
    (29, 20, 6) under primary text (229, 232, 235), text over tint at
    14.8 — but the tint against the page (11, 14, 15) is at 1.2, so on
    the dark theme the pen is barely a mark at all; the vote could not
    catch it because both arms share the token. ADR-0031 gives a subtle
    tint L ≈ 0.20 for a tinted row, not for a highlighter, and the
    stylesheet's `mark` and the widget's pen both took it. Open: a
    mark needs a tint that stands off the page in both themes, which
    is a palette question before it is a widget one.

## 2026-11-06 — fifteenth run, `2026-11-06-mark` — the highlight pen stands off the page; the typst arms join the harness

- **Build under test:** the fourteenth run's tree with the highlight pen
  at the warning default over the page at 40% in the widget
  (`theme.go`, `over`, `markPenAlpha`) and in the stylesheet's `mark`
  (`color-mix` of the same tokens), where both had the warning family's
  subtle tint. The harness gained the two typst arms (README §3): a
  `typst` step in `measure.sh`, pandoc templates under `harness/typst/`,
  pixels-only rows and two vote pairs per document that exist only when
  the run captured them.
- **Attempted:** the whole protocol; the browser arms again after the
  first capture showed the stylesheet's mark unchanged; the typst step
  once the owner had installed typst 0.15.1 and pandoc 3.13, then again
  after the first typst capture showed the pinned arm's lines
  overlapping.
- **Findings:**
  - **Result, not a toolbelt finding:** the pen is now (99, 81, 46) in
    both arms, sampled from the captures: primary text over it at 6.2,
    the pen over the page at 2.5, where the fourteenth run's pen sat at
    1.2 over the page. The crop
    (`stress-mark.run14-vs-run15-vs-ids.png`) shows the fourteenth run's
    pen, this run's, and the IDS arm's. The line metrics repeat the
    fourteenth run's to the cell; a tint moves no glyph.
  - **[pain boxer/keelson/designsystem / functional-suitability.appropriateness / S3]**
    The first browser capture carried no mark at all: the stylesheet's
    `mark` had been written against `--ids-warning-default`, and the
    generated palette names a role's default tone by the bare role,
    `--ids-warning`. An unknown custom property makes `color-mix`
    invalid and the declaration is dropped without a word. The harness's
    controlled pair caught it in one run; a CSS variable the palette does
    not emit is the kind of drift `css_drift_test.go` could check for
    the stylesheet's own references, and does not yet.
  - **[maturity boxer/practice/measuring/trials / functional-suitability.completeness]**
    Positive: the typst arms render on the first pass through pandoc
    3.13's typst writer and typst 0.15.1, every corpus document, both
    templates, page one at the viewport. The crop
    (`why-boxer-para.native-vs-typst-ids-vs-typst-book.png`) sets the
    same paragraphs three ways: the native arm; the native spec set by
    typst, justified and hyphenated; and typst's book typography. The
    pinned arm is the comparison M2 asked for — the one variable is the
    paragraph setter — and it shows what a Knuth–Plass breaker buys at
    this measure: an even right edge, no detached punctuation, and the
    same number of lines. The book arm is the ceiling, in a serif at a
    66-character measure; it is not a target for a reading mode set in
    the design system's sans, it is where the eye can go. The vote page
    carries 36 pairs with the two new ones; no vote was taken on them.
  - **Friction:** typst measures a line box from cap height to baseline
    by default (`top-edge`, `bottom-edge`), not ascender to descender
    as CSS and egui do, so a leading computed from the face's span set
    the pinned arm's lines on top of one another; the template sets the
    edges to the face's. The binaries could not be fetched from this
    session (the tool-use policy declined the download) and were
    installed by the owner.
- **Solution size:** `theme.go` ±25, `ids.css` ±4, one test; the harness
  +60 (`measure.sh`, `prepare.go`, `metrics.go`, `vote.go`) and two
  templates.
- **Results:** `runs/2026-11-06-mark/metrics.json`, `metrics.md` (the
  typst arms as pixels-only rows), the two crops. Votes: not taken; the
  pen is a tint the captures measure.
- **Run dir:** `./runs/2026-11-06-mark/`.

## 2026-11-06 — sixteenth run, `2026-11-06-typographer` — typographic punctuation

- **Build under test:** the fifteenth run's tree with goldmark's
  Typographer wired into the obsidian package as `FeatureTypographer`
  (curly quotes and apostrophes, en and em dashes, an ellipsis; no angle
  quotes; the characters themselves rather than entities), opted into by
  the widget's default feature set and the trial's HTML arms, and kept
  out of `FeatureAll`. The resolver folds the typographic characters
  back before sanitising a fragment, and `SlugHeading` folds the same
  way, so a wikilink written from the source meets a typeset heading.
- **Attempted:** the whole protocol, with the typst arms.
- **Findings:**
  - **Result, not a toolbelt finding:** the native arm sets the
    corpus's apostrophes and quotes as the typst arms do; the crop
    (`why-boxer-apostrophe.run15-vs-run16-vs-typst-book.png`) shows one
    passage in the fifteenth run, this run and the book arm. The native
    line metrics repeat the fifteenth run's; the IDS arm's move by a
    character here and there, since it renders the same substituted
    text and a curly quote is not a straight one's width.
  - **[pain boxer/toolbelt/markdown / functional-suitability.appropriateness / S3]**
    The typographer replaces text with `String` nodes that carry no
    source segment. Two things read source bytes through the AST and
    had to be looked at: the widget's heading text collector took only
    `Text` nodes and dropped the substitutions (the outline read "Its
    here" for "It's \"here\""), and a heading's slug, computed from the
    typeset text, no longer met the fragment a wikilink carries from
    the source. The first is a case in the collector; the second is the
    fold in the resolver and the slug, held to the typographer's table
    by a test. It is also why the feature stays out of `FeatureAll`: a
    consumer mapping text to source bytes (mdspan) or indexing it
    (mdextract) would see characters the source does not hold.
- **Solution size:** `obsidian` +40 (the feature, the table, tests),
  `resolver.go` +20, `visitor.go` +6, `markdown.go` +4, the harness +1,
  ADR-0180 update.
- **Results:** `runs/2026-11-06-typographer/metrics.json`,
  `metrics.md`, the crop. Votes: not taken; a substitution the captures
  show.
- **Run dir:** `./runs/2026-11-06-typographer/`.
