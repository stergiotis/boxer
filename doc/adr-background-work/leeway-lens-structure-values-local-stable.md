---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** An exploration dated 2026-09-26, done
> with the vizeval harness ([ADR-0266](../adr/0266-vizeval-scored-renderings-of-leeway-batches.md),
> withdrawn). Every quality judgement below is one reader's reading of
> captures plus vizeval's geometry gates, except §Judged rounds, which
> report five task-accuracy rounds by blind agent readers.
>
> **2026-10-07.** The lens has moved into the Projection panel
> ([ADR-0289](../adr/0289-leeway-rows-for-readers-canonical-forms-a-read-model-and-kinds-in-projection.md)):
> its bands are the panel's clusters, its own clustering is gone,
> the archetype form is data in `lwlens` that play's `get_archetypes`
> returns, and the view scrolls. The Experiments pane and vizeval are
> removed. Below, "the Experiments pane" and the lens's K are the state this
> exploration ran under; the first two items of §Open are settled by that
> ADR.

# A lens over leeway rows: structure↔values, local↔stable

The question: can a generic rendering of a leeway batch use the batch's own
structure — sections, memberships — and what the projection panel computes
over it — clusters of slot presence, a threshold tree — to be uncluttered
without being generic mush? And can two reader intents be exposed as
sliders:

- **values** — from *structure* (which attributes a row has, and how that
  sets it apart) to *values* (what the attributes hold, the structure taken
  as known);
- **stable** — from *local* (each row drawn on its own terms, its most
  telling attributes first) to *stable* (every row drawn against one frame,
  so an attribute sits in the same place in every row).

The prototype is the `lens` sink of play's Experiments pane:
[`lwlens`](../../public/semistructured/leeway/lwlens) (model, analysis, plan)
and `LensView` in [leewaywidgets](../../public/thestack/imzero2/egui2/widgets/leewaywidgets)
(drawing). The scenario
`60_mixed_kinds` (a vizeval scenario, removed with the harness) was
written for it; it was also run against the facts store and three other
local leeway tables through scratch scenarios that are not in the tree.

## The unit: a slot

A slot is a (section, primary membership) pair. A row has a slot or lacks
it — that is its structure; the slot's value is its value. In a facts-style
table, where sections are value types and memberships name attributes, the
slot is the attribute; in a table with one section per attribute it is the
section. Plain columns are slots of a pseudo section, every row has them,
and they carry no structure.

## The analysis the lens draws on

The same pieces the projection panel runs, computed from the batch in the
sink's finish step (synchronous; cheap at the sink's row cap of 128):

- slot presence as a binary matrix, a cosine neighbour graph over it
  (`knn.Build`), HDBSCAN over the graph (`algo.HDBSCAN`) — clusters are
  record kinds;
- a one-vs-rest threshold tree per cluster (`explain.FitOneVsRest`), whose
  best rule names the cluster: `has symbol·runtime-kind-audit ∧ lacks
  u64-array·runtime-lifecycle-tile-key`;
- per-slot value statistics: sorted numbers (percentiles), label counts
  (ranks), the shared prefix of a slot's texts, the decimals that tell its
  numbers apart.

Two findings about running that clustering on structure:

- **K has to stay under the smallest cluster.** With the projection panel's
  default of 15 neighbours, a kind of 12 identical rows finds its core
  distance in another kind, every core distance is equal, and HDBSCAN
  returns nothing. The lens ties K to the minimum cluster size. The
  projection panel's structure run may have the same problem on small kinds;
  that was not checked.
- **HDBSCAN keeps outliers as low-probability members.** Over slot presence
  that files a record of one kind in a cluster of another it shares a
  section with, and drawn there it reads as a row with a dozen deviations.
  The lens moves a row out of its cluster when it shares less than half of
  the cluster's template (slots at least half the cluster has).

## How the two intents act

Each intent runs through regimes and slides a threshold inside each, so a
slider position changes the picture a little rather than at a few points
([`plan.go`](../../public/semistructured/leeway/lwlens/plan.go)).

| values | detail | a present cell shows |
| --- | --- | --- |
| < 0.25 | shape | a square in its section's hue |
| < 0.5 | fingerprint | a rank bar (number), a chip (frequent label), a sparkline (numeric array) |
| < 0.75 | gist | a short text over a data bar or beside a chip |
| ≥ 0.75 | values | the text; numbers right-aligned |

| stable | frame | slots a row is drawn against |
| --- | --- | --- |
| < 1/3 | row | its own, ordered by salience below 1/6, canonically above |
| < 2/3 | cluster | the cluster's typical slots — support from 0.5 falling to any |
| ≥ 2/3 | global | the batch's — support from 0.25 falling to any |

Values also enters the salience a local row is ordered by (structural
rarity blended with value surprise), and the distance the focus form picks
peers by (Jaccard distance of slots blended with rank distance of values).

## Forms

- **rows** — every row, in cluster bands, each band headed by its rule.
  At the shape detail, rows with identical slots collapse into one line
  with a count; the batch reads as its kinds and their exceptions.
- **archetypes** — each cluster as one template line (how typical each slot
  is; or its typical value and spread) and under it only the rows that
  break it: missing and unusual slots, and extreme values with a direction.
- **focus** — one row as a vertical list of slots: its value, where the
  value stands among its cluster's, and the same slot for its nearest
  peers. This is the shape the detail pane could take.

## What made the difference

In the order they were found, each from a capture that contradicted the
design before it:

- **Collapse identical rows at the shape detail.** Forty-eight rows of
  four kinds became six lines; the planted deviations were the only
  singletons. On real tables, a hundred rows became four or five kinds plus
  a handful of unclustered records.
- **Name deviations, don't only mark them.** An outlined empty cell says
  something is missing; `−num·disk` says what.
- **Hoist constants.** A slot with one value throughout a cluster is a
  fact about the cluster — `kind track`, `result ok` — said once in its
  header rather than repeated down a column.
- **Elide what every value shares.** Common prefixes of values, of member
  names within a section (pairwise, cut at a separator), of row labels
  within a cluster, and runs of hex digits in unnamed membership refs. In
  facts-style tables a narrow column otherwise shows only the shared part.
- **Resolve ref memberships to names** through the process's membership
  registries (`providers.MembershipRefFormatter`).
- **Per-slot decimals.** Latitudes printed to one decimal were all `47.1`
  while their rank bars differed.
- **Sparklines for numeric arrays.** Arrays of hundreds of values became
  comparable down a column.
- **Seriation.** Within a shared frame, rows are chained by nearest
  neighbour, so like rows sit together and a value column shows runs.
- **The accent for extremes** (outer 7.5% of a slot), so the local end's
  salience order is visibly about something.

Tried and dropped:

- **Ordering a local row purely by salience.** It interleaved sections
  into runs (`num … sym … num`) and the row stopped reading as a record;
  sections are now kept together and ordered by their most salient slot.
- **Ghost marks for every absent cell** at the text details. The block
  structure of a global frame reads from whitespace; the dashes were noise.
- **Underline rank bars under numbers.** They read as underlines; data
  bars behind the text replaced them.
- **Section names in their hue.** Some hues fail contrast on the dark
  surface; names are neutral over a hue rule.

## Judged rounds

### Round 1

2026-09-26, at build `4ebcd397`: scenario `60_mixed_kinds` in a
1500×900 artifact box, its five questions answered per candidate by one
agent reader given only that candidate's judge sheet (ADR-0266 §SD10). A
blurred control sheet was answered "unreadable" throughout, so no reader was
recorded as informed. One reader per candidate means each figure is one
reading, not a mean.

| candidate | task.accuracy | rows.labelled_share |
| --- | --- | --- |
| rows, values 0.5 | 0.8 | 0.79 |
| rows, values 0.9 | 0.8 | 0.79 |
| rows, values 0.1 | 0.6 | 0.46 |
| archetypes, stable 0 | 0.8 | 0.31 |
| archetypes, stable 0.5 | 0.4 | 0.31 |
| archetypes, stable 1 | 0.4 | 0.31 |
| focus (row 0) | 0 | 0.10 |
| topo (reference sink) | 0 | 0 |

Every miss but one was "unreadable", not a wrong answer; the topology spark
counted three kinds for four, by row shape. What the misses point at:

- **No candidate showed the failed job.** The rows form ran out of height
  inside the jobs band ("… 2 more rows") and never drew the fourth cluster;
  the archetype form lists numeric exceptions only, so a rare label value —
  one job in state `failed` among twelve — is not an exception there,
  although it is what the form is for.
- **In the archetype form, `stable` decided whether the missing slot was
  visible.** At stable 0 the host lacking `disk` and the busiest host were
  among the rows shown; at 0.5 and 1 both fell into "2 more rows with
  exceptions". Exceptions are cut by a row budget, and a missing slot does
  not outrank a numeric outlier in it.
- **`values` barely moved the rows form** between 0.5 and 0.9; at 0.1, the
  shape end, the values that two questions need are gone.
- **Labelled share did not separate the archetype candidates**; accuracy
  did (0.8 against 0.4 at one share). Which rows are named mattered more
  than how many.
- **focus** answers questions about one row, which none of the scenario's
  are; its zero says the scenario does not test that form.

### Round 2

Same scenario, box, candidates and protocol, fresh readers, 2026-09-26,
after two changes to the lens (build `f93ab1f3` plus them): the archetype
form ranks its exception lines — a missing or unexpected slot first, then a
rare label value (at most a tenth of its band, against a slot whose band has
a typical value), then numeric outliers — before its per-band cap cuts them,
and lists rare label values at all; and both forms draw a cluster they have
no room for as one line (name, row count, rule, "not drawn") instead of
omitting it. Only the lens's rows and archetype candidates drew differently;
focus and topo are round 1's drawings, re-read.

| candidate | round 1 | round 2 |
| --- | --- | --- |
| rows, values 0.5 | 0.8 | 0.8 |
| rows, values 0.9 | 0.8 | 0.8 |
| rows, values 0.1 | 0.6 | 0.6 |
| archetypes, stable 0 | 0.8 | 1.0 |
| archetypes, stable 0.5 | 0.4 | 0.8 |
| archetypes, stable 1 | 0.4 | 0.8 |

- **The archetype form no longer depends on `stable` for structure.** The
  missing slot and the failed job were read at every stable position.
- **Its remaining miss is the busiest host** at stable 0.5 and 1: numeric
  outliers are still cut in row order, and the host with the top cpu fell
  into "2 more rows with exceptions". At stable 0 the plan's row order put it
  above the cut.
- **The rows form did not move.** It now names the alert cluster it has no
  room for, but the failed job is still among the jobs cut at the band's
  end; the rows form's answer to a rare value is the archetype form.

### Round 3

One further change: within a class, exception lines are ordered by their
most extreme value surprise, so the cap cuts the mildest outliers first.
Only the stable-0 archetype candidate drew differently; the others kept
round 2's drawings, and with them their round-2 replies (judge sheets are
keyed by drawing). Its fresh reader scored 1.0 again; every other figure is
round 2's.

The busiest host stayed cut at stable 0.5 and 1. Value surprise is
two-sided and measured against the slot across the whole batch: a host's
disk at 9.2, cpu at 6.3 and tx at 224 sit at the very bottom of their
slots, while the top host cpu, 98.2, is not the top of a cpu slot that
services share, so the lows rank above it. Ordering by surprise answers
"what is strangest", not "which row holds the maximum".

### Round 4

The archetype form gains one line per band under its template, `extremes`:
for each numeric slot the band mostly has, the rows holding the band's
lowest and highest value (`cpu ↓host-15 6.3 ↑host-06 98.2`), at the gist
detail and above. The three archetype candidates drew differently and got
fresh readers; the others kept round 2's drawings and replies.

| candidate | round 2 | round 3 | round 4 |
| --- | --- | --- | --- |
| archetypes, stable 0 | 1.0 | 1.0 | 1.0 |
| archetypes, stable 0.5 | 0.8 | 0.8 | 1.0 |
| archetypes, stable 1 | 0.8 | 0.8 | 1.0 |

Every archetype candidate now answers every question, at any stable
position. All three readers named the same residual doubt unprompted: the
form shows the failed job as an exception but never says it is the only
one, so "which jobs are failed" is answered by trusting that no "more rows"
line means nothing was cut.

### Round 5

A rare label now carries its count in the band — `state failed (1 of 12
rows)`. The three archetype candidates drew differently and got fresh
readers, and so did the control, whose picture is blurred from a drawing
that changed; the others kept round 2's replies.

Accuracy stayed at 1.0 for all three, so the figures cannot show the
change; the readers' own accounts do. Where all three round-4 readers
inferred the failed job's list was complete from a missing "more rows"
line, two of the three round-5 readers cited the count for it, and none
raised the doubt. The control was again answered unreadable throughout.

## Considered, not built

- A navigator over the threshold tree itself (an icicle of rules with row
  counts) — the archetype form's headers carry the rule of each cluster,
  which covered what was asked of it here.
- A co-occurrence view of slots — the collapsed shape view is already an
  UpSet plot's intersections with their sizes.
- Small multiples of the 2-D embedding per slot — the embedding lives in
  graphview's layout rather than in the analysis result, and was not needed
  for any view above.

## Open

- **A panel of its own.** The lens runs where the Experiments pane runs
  sinks. A play panel reading the Projector's background result, with the
  selection as the focus row, is the next step and wants a design dialogue
  (and likely an ADR); how the lens's clustering differs from the
  projection panel's (K, the template refinement) would be settled there.
- **Vertical budget.** The pane is not scrolled; rows past its height are
  counted, not drawn. The archetype form is the answer for many rows; a
  scroll area is the answer for reading them all.
- **The rows form and rare values.** It still cuts the failed job at the end
  of its band; whether the rows form should hoist rare-value rows, or leave
  that to the archetype form, is undecided.
- **Task accuracy with more readers.** One reader per candidate per round;
  several per candidate, or a vision model as judge, would say how much of
  a 0.2 step is the reader.
- **The light theme** was not captured.
