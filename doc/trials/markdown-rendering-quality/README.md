---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** One run on one machine, six
> documents, one viewport, one voter. §0 says what that is worth. Do not
> cite as authoritative.

> **Provenance.** Compiled 2026-11-05 from the first run. Claims about this
> repository were checked against the working tree on that date; the
> reference stylesheets are pinned by file (§3).

# Markdown rendering quality — the native reading surface against HTML references, measured and judged

## 0 The claim, and how to cite it

**Shown the same document at the same 1000-point viewport, blind, left and
right at random, the owner preferred a browser rendering of the package's
HTML to mdedit's reading mode in 17 of 18 pairs and called the other a
tie; against the IDS stylesheet — the one arm with the native arm's own
faces, body size and dark theme — the native arm lost all six, and the IDS
and GitHub stylesheets, one dark and one light, split their six pairs two
wins each and two ties.** So the preference is about layout, not theme or
font: the controlled pair lost as clearly as the uncontrolled ones, and the
two references that differ only in theme and font did not separate.

**The second run, with the reading column the first run's controlled pair
pointed at (`Input.Measure`, 50 em, mdedit's reading mode), brings the
native arm's median line to 98 to 104 characters in a 50 em column, between
the IDS and GitHub references, with the reference arms' numbers unchanged
to the cell; its leading stays 1.38 against the references' 1.50 to 1.60.**
In the second run's vote, blind, the same 24 pairs, the native arm lost 7
and tied 11 where it had lost 17 and tied 1: no reference beat it in more
than half of its six pairs, and the two references tied each other in all
six of theirs, so the sitting tied more overall and the native arm's gain
is partly that. A third run, `2026-11-05-pill`, rounds the native arm's
inline backgrounds (code spans, highlights, tags) at the design system's
chip radius, which the owner named after the second vote; its metrics
repeat the second run's, since a radius moves no glyph, and its vote is
pending. A fourth run, `2026-11-05-leading`, gives prose the design
system's leading (`styletokens.BodyLeading`, 1.6, landing at 1.54 after
rounding to whole points) and a gap after the task box: the native line
step now sits inside the references' 1.50 to 1.60. A fifth run,
`2026-11-05-tables`, renders a flowing table as a grid of wrapping cells
with a body-size header, which the protocol's first viewport never
reaches, so its evidence is a capture outside the protocol (logbook). A
sixth run, `2026-11-06-lists`, renders a list as a two-column grid —
numbers right-aligned in the body face, the items a fraction of a line
apart — and from this run on the metric takes a list's items for one
paragraph in the native arm as it does in the references, so its line
counts are comparable across arms but not with the earlier runs' native
rows (logbook). A seventh run, `2026-11-06-quote`, draws a blockquote as
a left rule rather than a box, and tags list and table rows out of the
paragraph metrics on every arm: under that model the native arm sets 95
to 102 characters per line at 1.54 against the IDS arm's 94 to 97 at
1.60, with the same raggedness (0.02 to 0.03) on four documents and
more (0.14 and 0.25 against 0.02 and 0.03) on two — which the tenth run
found to be the line model's, not the renderer's: a native table's cells
carry no tag, and their short lines were read as a ragged paragraph.
**In the seventh run's vote, blind, the same 24 pairs, the native
arm won none, tied 6 and lost 12 — more losses than the second run's 7
and fewer ties than its 11 — and against the IDS arm it lost 5 of 6
where it had lost 3.** The owner voted the seventh run only, so the five
changes between the second and the seventh (pills, leading, tables,
lists, the quote rule) are not told apart by the votes; the metrics
moved toward the references over those runs and the preference moved
away, which is the trial's reminder that the line model does not measure
what the vote judges. The owner named one candidate after voting: the
inline backgrounds, which epaint sizes to the row's line height and
which the fourth run's leading made taller, where a browser's box hugs
the glyphs. An eighth run, `2026-11-06-pillbox`, sizes the pill to the
font's box and pads it as the IDS stylesheet pads a code span; its line
metrics repeat the seventh run's to within a character, since the side
padding is the only glyph that moved, and its vote is pending. A ninth
run, `2026-11-06-codespan`, sets an inline code span at 0.9 of its run
and gives the highlight pen the warning tint the design system's own
stylesheet gives `mark`; the crop of the same span now differs from the
IDS arm's in the baseline of the smaller run alone (about a pixel), and
the line metrics repeat the eighth run's. Its vote is pending. **A tenth
run, `2026-11-06-links`, sets a paragraph whose text carries links as one
galley — the link's words are sections of the paragraph's own layout,
clicked where they are — and gives the line model blocks and a native
table rule; under that model the native arm's raggedness is 0.01 to 0.04
on every document against the IDS arm's 0.02 to 0.03, in the ninth run's
captures as in the tenth's.** The link change moves no number the model
reads in the first viewport; the crop in the run directory shows what it
moves — a bare URL that ended a row with its comma orphaned at the start
of the next now breaks as text does, the comma with it. Its vote is
pending.

**In the first run, at a 1000-point viewport, standard density and the fonts the headless
host draws with, mdedit's reading mode sets prose across the whole window —
a median of 150 to 155 characters per line, a measure of 73 em at 13 px —
where the same HTML under the IDS stylesheet in Chromium, with the same
faces and the same 13 px body, sets 83 to 96 characters per line in a 47 em
column, at a leading of 1.60 against the native 1.38.** Six documents, every
one the same way: the native arm's line length is outside the 45–75 band on
every full line, and the three reference stylesheets agree with each other on
a bounded column (42 to 56 em) and disagree with the native arm. Vertical
rhythm, heading ratios and contrast are not where the arms differ: the gap
before a heading is 1.9 to 3.4 line heights across the arms, the native heading
ratios (2.00, 1.69, 1.38) sit between the IDS CSS (1.69, 1.46, 1.23) and
GitHub (2.00, 1.50, 1.25), and body contrast is 15.8 to 21 to one everywhere.

**What the first run established about the toolbelt:** the native reading
surface can be captured, measured at glyph level and compared against
browser renderings of the package's own HTML with no change to the tree
and no new dependency — the SVG sidecar of a scene capture, the DOM of a
headless Chromium, and one line model over both. The frictions are in the
logbook; two are findings on the stylesheets themselves (§5).

**What this trial does not say.**

- **Not "17 of 18 is the size of the gap."** One voter, one sitting, one
  question with a tie allowed, no notes left; the ballots say which arm was
  preferred, not by how much or for what. The agent's reading in the
  logbook names candidates for the why; it is not blind and is not evidence
  of it. The Obsidian and GitHub arms carry their own fonts, sizes and a
  light background; only the IDS pair isolates layout.
- **Not a figure for Obsidian the application.** The arm named `obsidian`
  is the obsidian package's own stylesheet in a browser, with a reading
  column added by the harness (§3). Obsidian itself was not run.
- **Not a line-breaking verdict.** Raggedness and widows are reported, but
  at 150 characters a line the native rag is small by construction, and in
  the stress document the metric mistakes a tight list for a paragraph
  (§4, limits). Compare those columns only between arms of similar measure.
- **Not reviewed / not replicated.** One machine, one run, one voter, six
  documents, one viewport width, Chromium geometry only (Firefox is pictures).

**If you need a number**, take it from `runs/<run>/metrics.json`; the tables
in `runs/<run>/metrics.md` are per-arm evidence. **No figure from this trial
travels without the pair of arms it compares.**

## 1 Question and scope

The domain question leads here, by exception to the directory convention;
the toolbelt question is what makes it answerable.

1. **The trial proper.** What does the native markdown reading surface
   (`widgets/markdown`, as mdedit's reading mode and the help books show it)
   look like to a reader beside the renderings the same markdown gets
   elsewhere — the package's own HTML under its two stylesheets, and under
   the stylesheet a GitHub README is read in — and which differences are the
   ones to work on?
2. **The side-product.** Can the toolbelt capture, measure and judge its own
   reading surface: scenes with geometry sidecars, a headless browser,
   one line model, a blind vote — and where does it fall short?

In scope: one viewport, standard density, the first screenful of each
document, line-level typography, a person's pairwise preference. Out of
scope for the first run: interaction (links, folds, hover glosses), images,
narrow and wide viewports, a model as judge, typst as an arm (§6).

## 2 The workload

Six committed markdown documents, chosen for shape rather than count, each
pinned by the commit the logbook names:

| slug | path | why |
| --- | --- | --- |
| `play-overview` | `apps/play/help/overview.md` | a help book page: short sections, lists, a table |
| `mdedit-blocks` | `apps/mdedit/help/blocks.md` | a help page with keys, lists and callouts |
| `keelson-positioning` | `doc/explanation/positioning/keelson.md` | long prose with footnotes |
| `why-boxer` | `doc/explanation/why-boxer.md` | long prose, nested headings, a leading blockquote |
| `adr-0334` | `doc/adr/0334-mdedit-blocks-over-the-source.md` | an ADR: a QOC table, long paragraphs, code spans |
| `stress` | `harness/corpus/stress.md` | the constructs the 2026 rendering review probed: lists at three depths, an eleven-item ordered list, tasks, a table with a long cell, callouts, code, every heading level |

The viewport is 1000 by 1400 — logical points for the host, CSS pixels
for the browsers — at a device scale of one; what is measured and judged is
the first screenful. Frontmatter is parsed and not rendered in any arm,
which is what mdedit's reading mode does.

## 3 System under test and arms

| arm | renderer | stylesheet / theme | fonts | geometry |
| --- | --- | --- | --- | --- |
| `native` | `widgets/markdown` in mdedit's reading mode, headless host, desktop tiled so the window fills the viewport | IDS tokens, dark | the faces the scene runner hands the host: Noto Sans, DejaVu Sans Mono | SVG sidecar, one `x` per glyph |
| `ids-chromium`, `ids-firefox` | obsidian package HTML, `idsweb.Stylesheet()` | IDS lean classless CSS (ADR-0076), dark, 50 em column | **pinned** to the native faces by `@font-face`, 13 px body (a decision of the design dialogue: isolate layout from font choice) | Chromium: injected script, one rect per glyph; Firefox: pixels only |
| `obsidian-chromium`, `obsidian-firefox` | obsidian package HTML, `DefaultStylesheet()` | the package's light theme, plus a 700 px centred column the stylesheet leaves to its host (Obsidian's readable-line-width default) | the stylesheet's own | as above |
| `github-chromium`, `github-firefox` | obsidian package HTML, `harness/vendor/github-markdown-light.css` 5.8.1 | GitHub's README look, light, 980 px container | the stylesheet's own | as above |

The HTML body is the same bytes in every browser arm; only the page shell
differs. Obsidian the application and typst are not arms of this run
(§6, §7).

Standing hypotheses, from the 2026 review and ADR-0180: the native arm's
measure and leading are where it differs most from the references; its
vertical rhythm after ADR-0180 is comparable; its tables (one-line cells,
rune-seeded widths) and list markers are where a reader notices first.

## 4 Method

- **Environment.** Recorded per run in `runs/<run>/environment.md`: the
  commit, whether the markdown widget, the obsidian package, the IDS web CSS
  or the Rust host were modified in the tree, Go, the headless client's
  build time, the browsers' versions, CPU, memory, kernel.
- **One run:** `measure.sh <slug>` runs the steps in order; each can be
  rerun alone. `prepare` writes the HTML arms, the native scenes and the
  pinned fonts under `tmp/trials/markdown-rendering-quality/<slug>/`;
  `scenes` captures the native arm through `scripts/dev/scene.sh` with the
  `svg` and `tree` sidecars; `browsers` screenshots every page in both
  browsers and dumps Chromium's DOM with the geometry block; `metrics`
  reads both geometries into one line model and writes `metrics.json` and
  `metrics.md` to the run directory, plus the cropped comparison pictures;
  `vote` builds the blind page; `tally` joins the exported votes with the
  key and writes `votes.json` and `votes.md`.
- **The line model.** A glyph is a drawn non-whitespace character with a
  position, size, family and colour; a space between two glyphs on one
  baseline counts as a character. Glyphs cluster into baselines; a baseline
  splits into lines at gaps wider than 2.5 em (table cells). Body size is
  the mode over non-mono glyphs. A prose line is unsplit, non-mono and at
  body size. A list row is a prose line that opens with a list glyph —
  the native arm's marker run (a bullet, or digits and a period, which
  the widget draws as a run of its own) or, in a browser, any glyph
  inside a list item — plus, in the native arm, the prose lines at the
  line step after one (a wrapped item's continuation, which carries no
  marker). A table row is a prose line inside a browser table cell that
  did not split — a vertically centred cell beside a taller one sits on
  a baseline of its own, half a line from its neighbour's; a native
  grid's cells share their row's baseline and split. Every glyph belongs
  to a block: the native arm's text shape (one label — a paragraph, a
  cell, an item; `<g class="imz-text">` in the SVG sidecar), a browser's
  nearest block element (`p`, `li`, `td`, a heading, …). A block one of
  whose lines split is a table cell's, which is how a native cell's
  unsplit second line is known for one. Line height is the median step
  between consecutive prose lines no further apart than 1.9 em, list and
  table rows excluded. A paragraph is a run of prose lines of one block
  at that step (within 1.3 line heights), list and table rows excluded;
  its full lines are all but the last. A heading is an unsplit, non-mono
  line more than 1.15 times body. The list rule and the browser table
  rule are the seventh run's; the block rule and the native table rule
  are the tenth run's, and the ninth run's tables were recomputed under
  them (its captures unchanged). The six runs before the seventh carry
  neither column and read a tight list as a paragraph of short lines and
  a centred table cell as a half-line step; the seventh and eighth read
  a native table's cells as ragged paragraphs. Each span of runs
  compares within itself.
- **Metrics per (document, arm):** body size; leading; rows, words and
  list rows in the viewport, table rows in the browser arms; median, p90
  and max characters per full line; the share of
  full lines in 45–75; measure (p95 full-line width, px and em);
  raggedness (mean slack against the paragraph's widest line, paragraphs
  of three lines or more); widows (paragraphs whose last line is one word);
  heading sizes as ratios to body; median gap before a heading, after a
  heading and between paragraphs, in line heights; WCAG contrast of the
  modal body colour against the page.
- **Known limits of the metrics.** A paragraph that follows a list item
  closer than 1.3 line heights would be read as the item's continuation
  in the native arm (none in the corpus does at standard density); a
  space in its own text node between two inline elements is counted in
  neither
  geometry; the native capture holds what the host drew inside the
  viewport, the browser geometry the whole page, so both are cut at the
  viewport before clustering; Firefox contributes pictures only.
- **Judgement.** Blind pairwise: for each document, the native arm against
  each stylesheet under Chromium, and the IDS arm against the GitHub arm;
  left and right drawn at random, pictures under opaque names, the key
  held outside the page. One question — which would you rather read — with
  tie allowed and a free note. The voter is the owner. A vision model as a
  second judge, with the Tier 2 "do not invent rules" prompt form and
  scenario questions with known answers, needs a configured endpoint
  (ADR-0254) and is M1.
- **Idiom rule.** The native arm runs through the scene runner and the
  widget as shipped; nothing in the harness touches the widget. The
  browser arms are reference points and may take any shell the harness
  gives them, recorded in §3.
- **Process measurement.** The harness is committed (`harness/`, one Go
  package driven as integration tests, one script); frictions are
  findings in the logbook, not fixes in the harness.
- **Reporting.** Metrics and votes land under `runs/<run>/` as JSON with a
  markdown rendering beside each; captures, SVGs, DOM dumps and the vote
  page stay under `tmp/` and are regenerated by a rerun.

## 5 Findings ledger

Findings follow the directory convention's classification. Pre-registered
candidates: the headless client's freshness check against a stale build;
snap-confined browsers refusing paths; the preview's scroll following the
seeded caret; a reference stylesheet that renders differently outside the
host it was written for. The first run's findings, with evidence, are the
logbook's 2026-11-05 entry; the two on the tree rather than the harness:

- the obsidian package's `obsidian.css` declares a body font and never
  applies it, so its HTML reads in the browser's default serif;
- the IDS web stylesheet pins nothing about fonts the host has, so what it
  looks like depends on the machine — expected for a lean stylesheet, but
  it is why the IDS arm pins faces by hand.

## 6 Milestone cut (each descope-able)

- **M0 — the first run (this page).** Six documents, seven arms, metrics,
  the vote page built. Gate: every arm captured, metrics for every arm
  with geometry, §0 written from the numbers.
- **M1 — the judgement.** Done 2026-11-05 for the owner's votes
  (`runs/2026-11-05-first/votes.md`; §0 carries them). A model judge, if an
  endpoint is configured, calibrated against those votes, stays open.
- **M1b — the reading column, measured.** Done 2026-11-05 as run
  `2026-11-05-column`: the one variable the controlled pair isolated,
  changed, the protocol repeated, the votes tallied and §0 rewritten.
- **M1c — the inline pills.** Done 2026-11-05 as run `2026-11-05-pill`,
  from the owner's observation; the vote closes it.
- **M1d — leading and the task gap.** Done 2026-11-05 as run
  `2026-11-05-leading`; the vote closes it.
- **M1e — flowing tables as grids.** Done 2026-11-05 as run
  `2026-11-05-tables`; the protocol's viewport does not reach a table, so
  a scene outside it is the evidence (logbook) and the vote cannot see it
  until the protocol gains a second anchor.
- **M1f — lists as grids.** Done 2026-11-06 as run `2026-11-06-lists`:
  numbers right-aligned in the body face, the item step at the inline
  gap; the vote closes it.
- **M1g — the quote rule.** Done 2026-11-06 as run `2026-11-06-quote`:
  a blockquote is a left rule, not a box. Voted: native 0–6–12, the
  seventh run's vote standing for runs three to seven (logbook).
- **M1h — the pill box.** Done 2026-11-06 as run `2026-11-06-pillbox`:
  an inline background is the font's box with the stylesheet's padding,
  not the row's line height; the vote closes it.
- **M1i — the code span's size and the pen's tint.** Done 2026-11-06 as
  run `2026-11-06-codespan`: a code span at 0.9 of its run, the highlight
  pen in the stylesheet's warning tint; the vote closes it.
- **M1j — links in the paragraph's galley.** Done 2026-11-06 as run
  `2026-11-06-links`: a paragraph with links is one label, clicked where
  the link's words are (`labelAtomsLinked`); the line model gained blocks
  and a native table rule in the same run. The vote closes it.
- **M2 — the second width and the typst arm.** A narrow viewport (the
  help pane's) and a wide one; the corpus through a markdown-to-typst
  path as a typographic ceiling, with the research notes on its line
  breaker, hyphenation, shaping and spacing model, and what of it could
  reach the host's text layout. Gate: a logbook entry with the typst
  arm's metrics beside the others.
- **M3 — a tool, if the trial keeps being run.** Promote the harness into
  a subcommand under the one entry point (CODINGSTANDARDS § Entry Points)
  and the metrics into facts. Trigger: a third run.

## 7 Open questions

1. Dark against light: the GitHub stylesheet has a dark variant; should the
   references be matched to the native theme so a vote is about layout?
2. The reading column: is a measure for mdedit's reading mode and the help
   books a widget option, a host decision, or a design-system token?
3. Which model, where, for the second judge — and whether the vizeval
   idea (questions with SQL-computed answers, posed to a reader given only
   the rendering) applies to prose.
4. Whether the line model should take the HTML tag for every block where
   it has one — it takes it for list items since the seventh run — at
   the cost of two code paths.
5. A second anchor per document: the first viewport holds no table in
   any corpus document and no callout in most, so the fifth run's change
   is invisible to the metrics and to the vote. A scene scrolled to a
   named heading per document would put those constructs in a second
   pair, at twice the voting.

Related: [ADR-0180](../../adr/0180-markdown-rendering-fidelity-pass.md) and
its [review](../../adr-background-work/markdown-rendering-review.md),
[ADR-0248](../../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md),
[ADR-0076](../../adr/0076-ids-lean-classless-css-target.md),
[ADR-0266](../../adr/0266-vizeval-scored-renderings-of-leeway-batches.md)
(withdrawn; the judge idea), the
[Tier 2 rubric catalogue](../../design-system/policy/tier2-llm-review.md),
the sibling trial [map-tile-addressing](../map-tile-addressing/README.md)
whose harness shape this one follows.
