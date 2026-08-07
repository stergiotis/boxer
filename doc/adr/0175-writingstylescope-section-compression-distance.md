---
type: adr
status: proposed
date: 2026-08-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0175: writingstylescope — section-level compression distance between two documents

## Context

`public/analytics/similarity/compression` measures how much two texts share by
compressing them together: if `C(xy)` is much smaller than `C(x) + C(y)`, one
text told the compressor little it did not already know from the other. The
`stylometry` sub-package wraps that in two measurement modes — *instance*
(per-candidate NCD/CCC against a fixed reference, with streaming stats and
convergence detection) and *profile* (candidates concatenated to the
reference's length, one number out). Both are exercised only by unit tests
today; there is no way to point the machinery at two real documents and look
at the result.

Two forces shape what an interactive surface should be.

**Granularity.** The question a reader actually asks of two documents is not
"are these similar" but "is any part of this one already in that one". A
whole-document NCD answers the first. It is nearly blind to the second: a
single copied section inside an otherwise-original 30-section document moves
the document-level number by roughly its length share, which is noise. The
unit that carries the answer — and that a reader can act on, by opening both
halves side by side — is the section.

**Calibration.** NCD has no absolute scale that survives a change of subject
matter, language, or section length. Two independently written sections of a
technical document routinely sit at 0.75–0.95; two sections of chatty prose
sit lower; a short section against a long one is pulled around by the length
asymmetry in the denominator. Any fixed "below 0.4 means copied" threshold is
a number invented for one corpus and quietly wrong on the next. What *is*
stable is the contrast against the surrounding measurements: within one pair
of documents, the cross-product of all their sections is a background sample,
and a shared section is the value that does not belong to it.

So the surface has to show a distribution, not a verdict — which is what the
`ecdf` widget (simultaneous confidence band, hover readout) already draws, and
what `implot`'s heatmap already draws for the cross-product itself.

## Decision

We will add a windowed app, `apps/writingstylescope`, that takes two pasted
Markdown documents, splits each into leaf sections, sweeps the section ×
section cross-product with NCD, and presents three readings of the same sweep:
a document-level headline from `stylometry`'s profile mode, the cross-matrix
as an `implot` heatmap, and the ECDF of every pairwise NCD as the calibration
against which individual pairs are read.

The app asserts no threshold and returns no verdict. It ranks pairs and shows
where each sits in its own document pair's background distribution; deciding
what that means is the reader's.

### SD1 — One section per heading, sliced flat, with a length floor

A *section* is the span from a heading's line start to the **next heading's**
line start, whatever that heading's level. Sliced this way a section holds
exactly the prose written directly under its own heading and none of its
descendants' — so nothing is double-counted and nothing is dropped, and the
sections that carry real prose are the deep ones. That is the "leaf level"
granularity the app is for, obtained without a leaf test: a nesting heading
simply owns its short lead-in paragraph, or nothing at all.

Slicing by the heading *subtree* instead — each heading owning its
descendants — was rejected: the same prose would then enter the matrix several
times at several lengths, and a copied child section would drag every ancestor
in with it as a partial match.

Text before the first heading becomes a section when non-empty; a document
with no headings is one section. YAML frontmatter is excluded — `type: adr` /
`status: proposed` is boilerplate that would make any two repo documents look
related. Heading text is retained in the section body; a copied section
usually copies its title too.

Sections shorter than a floor (default 200 bytes, adjustable in the app) are
excluded from the sweep and reported as a count. Compression distance between
very short texts is dominated by frame and framing overhead rather than by
content, so those cells would be noise sitting in the same distribution as the
real measurements. Exposing the floor as a control rather than burying it
keeps that sensitivity visible.

Sections are compared on their raw Markdown, not a plain-text rendering.
Stripping markup would be a second, separately-wrong normalisation step, and
shared *formatting* structure is itself evidence.

### SD2 — NCD on the matrix, CCC only as a headline

The matrix and the ECDF carry NCD. CCC (`C(xy) − C(x)`, conditional complexity
of compression) is unnormalised bytes, so across sections of different lengths
it tracks section length far more strongly than it tracks shared content — a
CCC heatmap reads as a map of how long each section is. CCC keeps a place in
the document-level headline, where profile mode has already truncated both
sides to a common length and the units mean something.

### SD3 — The ECDF is the threshold, and it is not a threshold

All `rows × cols` NCD values feed one `ecdf.Renderer` with its simultaneous
confidence band. The bulk of that curve is this document pair's own
background. A shared section shows up as a left-tail point detached from the
bulk; a pair of unrelated documents shows a curve with nothing detached. The
top-ranked pairs are marked on the plot as vertical lines so the reader sees
*where in its own background* each candidate sits, rather than being handed a
score against an invented scale.

The band's coverage guarantee assumes iid observations, and these are not
iid — every section appears in many pairs, so the values are dependent. The
band is therefore drawn and labelled as a **scale reference**, not as a
calibrated coverage statement, and the panel says so. Correcting for the
dependence is out of scope; overstating the band would be worse than drawing
it with the caveat attached.

The exact Berk-Jones band is an O(n²) inversion, and n here is `rows × cols`
(up to 16,384). The panel therefore uses the `ecdf` widget's own documented
warm-up path — grid evaluation, the instant DKW preview band while
`BandReady(n)` is false, and `EnsureBandJob` to warm the exact band with a
progress readout. That is the widget's facility, not a worker this app
builds, and so is not affected by the deferral in SD4.

### SD4 — Synchronous, capped, cached

The sweep runs on an explicit **Analyze** press, not on every keystroke, and
its result is cached until either pane's text changes. Caps: at most 128
sections per document (≤ 16,384 cells) and 1 MiB per pane. Above a cap the app
refuses with a readout naming the limit rather than stalling a frame. Elapsed
sweep time is shown, so the cost stays visible.

A background worker (as `ecdf`'s band job uses) is **deferred**: at these caps
the worst case is a fraction of a second behind a button the user pressed
deliberately, which does not justify worker lifecycle, cancellation, and
staleness handling in the first cut.

### SD5 — Each measurement mode where it is the right tool

The matrix and the document-level headline use different halves of the same
package, and the split is not a matter of taste.

**The matrix uses the exact path**: one `compression.Similarity` for the whole
sweep, `C(a‖b)` measured directly per cell, `C(b_j)` cached once per column.
The obvious-looking alternative — one `stylometry.Analyzer` per A-section, so
`compression`'s raw-dictionary optimisation (`useZstdDictOptimization`) can
preload the row's reference as zstd history — was measured and rejected:

| Shape | Per-row dict `Analyzer` | Single exact `Similarity` |
| --- | --- | --- |
| 128 × 128 cells, 2.5 KiB sections | 194 ms, 2.50 GiB allocated | 80 ms, ~0 allocated |
| 48 × 48 cells, 10 KiB sections | 54 ms, 939 MiB allocated | 12 ms, ~0 allocated |

(One dev box, `go1.26`, `linux/amd64`; total allocator churn, not peak heap —
peak stayed near 24 MiB either way, and neither path leaked goroutines. These
are throwaway-probe numbers establishing an ordering, not committed
measurements.)

The dict optimisation is built for one *fixed* reference against a stream of
candidates. A cross-matrix moves the reference every row, so it pays a fresh
dictionary-encoder construction per row to save on that row's cells, and the
construction dominates. It is also less accurate here: it approximates `C(xy)`
as `C(x) + C_dict(y)`, which cannot share a frame header and re-charges `C(x)`
in full even when `y` is a near-copy, lifting mean NCD from 0.187 to 0.355 at
the first shape above. And it makes the measurement asymmetric, which a matrix
should not be.

**The document-level headline uses the `Analyzer`**, where the reference *is*
fixed — document A as a whole — and the candidates *are* a stream: profile mode
(`MeasureNcdProfile` / `MeasureCccProfile`, both sides truncated to a common
length) and instance mode (`MeasureNcdInstance`, streaming stats plus the
convergence detector's early stop). This is the shape the dict optimisation was
written for, and the app reports instance mode's `count` and `converged` flag
so an early stop is visible rather than silent.

One consequence is recorded rather than worked around: `compression.NewSimilarity`
emits a zerolog `Info` line per construction. With the sweep on the exact path
that is now a handful of lines per Analyze press rather than one per section.

### SD6 — The measurement half touches nothing

Splitting, sweeping and drawing read nothing but the two text buffers and write
nothing outside the process: no bus, no store, no filesystem — the `fibscope`
posture. The only capabilities the app declares belong to SD7, and every one of
them sits behind a button.

### SD7 — The pairs table hands off to `play` as an ad-hoc dataset

The Matrix tab's ranked table is a view of the top of a ranking. A reader who
wants to filter it, aggregate it, or join it against something else needs the
data, not the view, so the table carries an **Open in play** button that
publishes the sweep as an ephemeral dataset (ADR-0134) and asks the window host
for a `play` window seeded with a query over it (ADR-0135).

- **What is published is the whole cross-matrix**, one row per pair
  (`a_idx`, `b_idx`, `a_section`, `b_section`, `a_level`, `b_level`, `a_bytes`,
  `b_bytes`, `ncd`, `quantile`) — not the 25 rows the panel had room for.
  Section titles ride denormalised on every row: a pair means nothing without
  both titles, and the alternative is three datasets to join at a size where
  that is the worse trade. `quantile` carries the app's own reading of where a
  pair sits in its background, so a query can reproduce the panel's "beats"
  column without recomputing the distribution.
- **The seeded buffer reproduces the on-screen table** — same ordering, same
  limit — so what opens matches what was clicked from, with the full dataset
  underneath it.
- **The launched window gets `EndpointIntrospection`**, which is where
  `keelson('<handle>')` resolves; and the SQL names the **handle**, not the
  alias, because the opened window inherits no alias binding (ADR-0134 §SD4).
- **Repeated presses republish under the held handle** rather than minting a
  new dataset each time, and `Unmount` retracts it. The dataset's lifetime is
  the window's.
- **Three capabilities**, all `Pub`: `adhoc.publish`, `adhoc.retract`,
  `windowhost.open`. Both round-trips are synchronous bus calls and run off the
  render thread, with the outcome read back on a later frame.

A host with no bus (the screenshot tour) shows the button disabled with the
reason, rather than failing on press.

### SD8 — Every box follows its pane; the paste panes are pinned

Both plot boxes size from a seq-keyed pane probe rather than from constants.
implot draws its x tick labels *inside* the box along the bottom, so a box
taller than its pane loses them, and the clipped y range then reads as missing
data rather than as a cropped view — a fixed plot height is a latent bug, not a
simplification. Each box therefore takes the pane's width less a slack, and the
lesser of its preferred height and what the pane has left once the chrome below
it is reserved.

The floor is `implot.MinBoxHeight`, which is the widget's own clipping bound
and not a readability number: below it implot lays gutters out larger than the
canvas and the box clips its own labels, so a smaller box is not a smaller plot.
Overflow past the floor spills into the tab's ScrollArea. The probe answers one
frame late and reports a miss on the frame a dock tab returns, so the last good
measurement is held — sizing off the miss would flash both boxes to the floor
on every tab switch.

The paste panes go the other way: a multiline `TextEdit` sizes to
`max(desired_rows, content)`, so an unbounded one grows with the pasted
document and pushes the controls and the readout off the tab. They are pinned
top and bottom at a fixed height and scroll internally, which also keeps the
two panes level with each other whatever their contents.

## Alternatives

- **A `play` panel instead of an app.** Rejected: play's panels are
  SQL-result-shaped and hang off the reactive query graph (ADR-0097). The
  input here is two pasted blobs with no query behind them, and the panel
  contract would have to be bent to carry them.
- **Token/shingle overlap (MinHash, Jaccard, suffix automaton).** For
  detecting *verbatim* copying this would be stronger and would also localise
  the copied span within a section. Rejected for this ADR because it is a
  different engine, not this one — but recorded honestly: compression distance
  is the more general instrument (it survives paraphrase and reordering that
  destroy shingle overlap) and the weaker one at pinpointing an exact quote. A
  later ADR may add a shingle lane beside this one.
- **Whole-document comparison only.** Rejected on the granularity argument in
  Context: it answers a question the reader did not ask.
- **A fixed NCD threshold with a red/green verdict.** Rejected on the
  calibration argument: any such constant is fitted to one corpus and is
  quietly wrong on the next. The ECDF is the honest replacement.
- **Per-cell score standardised against its row and column background.**
  Would separate signal better when one section is uniformly more verbose than
  its neighbours. Deferred: it is a scoring scheme invented here rather than
  one `stylometry` supports, and the ECDF already supplies the contrast the
  first cut needs.

## Consequences

### Positive

- `stylometry`'s two measurement modes get a surface where their difference is
  visible — profile mode as the length-matched headline, the low-level
  `Similarity` sweep as the matrix.
- The heatmap and the ECDF are read together: the matrix shows *which* pair,
  the ECDF shows *how far from ordinary* that pair is for these two documents.
- The measurement half is self-contained and deterministic — no bus, no
  network, no store — so the screenshot tour can render a fixed example pair as
  a stable scene, and the SD7 handover is the only part that needs a runtime.
- The sweep does not have to guess what a reader will want to ask of it: SD7
  hands the whole matrix to SQL, so filtering, aggregating and joining are
  someone else's problem and the panel can stay a panel.

### Negative

- The app can be read as a plagiarism verdict machine, which it is not. Every
  readout is worded to say what was measured, and the ECDF panel carries the
  explanation of why no threshold is offered. This is mitigation, not a fix.
- Cost is quadratic in section count. The caps in SD4 bound it, but a genuinely
  large pair of documents is out of scope for the first cut.
- The app's headline numbers and its matrix come from two different code paths
  in the same package (SD5), so they are not arithmetically reconcilable — the
  headline is length-matched and dictionary-approximated, the matrix is
  neither. They answer different questions and the readouts say which.

### Neutral

- One Analyze press writes a few `Info` log lines from inside
  `compression.NewSimilarity` — one per `Similarity` the sweep and the two
  headline modes construct.
- Section splitting is Markdown-specific. Pasting plain prose yields one
  section per document and the app degenerates to a document-level comparison —
  a legitimate reading, not an error.
- The Distribution panel calls `ecdf.EnsureBandJob`, and the `ecdf` widget
  imports the keelson task API, which puts `net` in this app's call graph. The
  §SD10 app-capability gate therefore reports `CAPABILITY_NETWORK` for an app
  that opens no socket and declares no caps. That gate was already failing at
  the time of writing for nine other apps on a different capability
  (`CAPABILITY_MODIFY_SYSTEM_STATE`), and this ADR does not undertake to fix
  it. Dropping the warm-up to clear the finding was considered and rejected:
  it would trade a real capability the app does not exercise for a permanently
  wider band on every non-trivial matrix.

## Verification plan

- **Lane.** Default `go test` for the pure halves — section splitting
  (nesting, preamble, frontmatter, no-heading, degenerate `##`, `#` inside a
  fence), the sweep's shape and cell indexing, ranking order, the cap
  refusals, the SD8 box-sizing arithmetic, and the SD7 Arrow encoding. The
  demo-registry screenshot tour (ADR-0057) for the three rendered scenes.
- **What would fail.** Splitter tests go red if sections start carrying their
  descendants' text or if bounds drift off the heading line start. A sweep test
  on the built-in pair — one section deliberately shared, one same-topic
  distractor — asserts that the shared cell is the matrix minimum, sits below
  the 5th percentile, and that the distractor stays inside the bulk: the app's
  whole claim, plus the case it must not overcall. `boxSize` tests go red if a
  box can be handed a height under `implot.MinBoxHeight`, which is how the
  tick-label clip returns. `pairsArrow` is checked against
  `adhocdata.StructureFor` — the ADR-0134 publish gate — so a column type
  outside the bounded set fails at build time rather than behind the button.
- **Gap.** Nothing asserts that the *rendered* heatmap colours or the ECDF
  band are correct; those come from `implot` and `ecdf`, which carry their own
  tests, and the tour catches gross breakage visually only. Nothing exercises
  the SD7 round-trip end to end either: the tests cover the Arrow encoding, the
  publish-gate structure, and the no-bus refusal, but a live publish-and-open
  needs the app runtime, and that path is **unverified** as of this writing.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0057](./0057-demo-registry-and-drivers.md) — demo registry and the screenshot tour.
- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the implot port the heatmap and ECDF draw through.
- [ADR-0134](./0134-adhoc-datasets.md) — ephemeral capability-mediated datasets; the SD7 publish path.
- [ADR-0135](./0135-app-launch-requests.md) — app-launch requests; the SD7 open path.
- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — app manifest, topics, and launcher placement.
- `public/analytics/similarity/compression` and its `stylometry` sub-package — the measurement engine.
- `public/thestack/imzero2/egui2/widgets/ecdf` — ECDF with simultaneous confidence band.
