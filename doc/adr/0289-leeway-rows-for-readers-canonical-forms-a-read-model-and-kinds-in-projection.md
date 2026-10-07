---
type: adr
status: proposed
date: 2026-10-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0289: Leeway rows for readers — canonical forms for identity, a read model for rows, kinds in the Projection panel

## Context

A leeway batch reaches a reader — a person at play, or a model through play's
operations ([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md)) —
along about twenty paths. Two of them claim to be the general form:

- **Card JSON** ([ADR-0018](./0018-leeway-card-json-canonical-format.md)),
  designed as a lossless, isomorphic interchange format. No parser was ever
  written, so the isomorphism is untested, and its one reader is the play
  Experiments pane. Its attribute keys come from the membership-role
  classifier ([ADR-0073](./0073-leeway-membership-role.md)) and the default
  membership renderer. A name without a leading `/` — the way a facts-style
  table names its attributes — is classified secondary, and the key falls
  back to a position: `"_unidentified/num/0": {"scalar": 41.2, "labels":
  [{"name": "cpu"}]}`. Ref memberships key as hex ids (`0xd1d`). On the anchor
  fixtures, 90 of 130 attributes came out positional, and the per-entity
  section list was 55 % of the bytes (measured 2026-10-07).
- **The Experiments pane** ([ADR-0266](./0266-vizeval-scored-renderings-of-leeway-batches.md), proposed),
  eleven candidate renderings and the vizeval harness that scores them. On
  2026-10-07 every sink was rendered over the six maintained scenarios and
  over the 48-row mixed-kinds batch. The text sinks (card JSON, box-drawn
  tables, three sparks) and the topology treemap showed one or two entities
  per screen or the shape alone; the chart, graph and hierarchy sinks drew
  well only on batches shaped for them and duplicate panes play already has.
  One sink stood out: the lens, whose archetype form names a batch's kinds,
  what each typically holds, its extremes, and the rows that break it — the
  form its five judged rounds converged on
  ([background](../adr-background-work/leeway-lens-structure-values-local-stable.md)).

The paths disagree with each other. Bytes are spelled base64, hex or as
card JSON's own string; lists as `[len=N]`, `[a, b, …]`, `[i] v` or
`name=value · …`. Membership names appear only in the lens, the one consumer
that injects the registry's ref formatter. The readability aspect is honoured
only by the Detail card. An agent reading `SELECT *` over a leeway table
through `sample_rows` sees list lengths, not values.

The Projection panel ([ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md),
[ADR-0235](./0235-explaining-a-clustering-threshold-tree-and-rank-contrasts.md),
[ADR-0238](./0238-projection-feature-sets-hashed-structural-identity.md)) is
where a batch is summarised: its clusters, and rules that separate them. On
the mixed-kinds batch at its defaults it found no cluster — the core distance
was read at the fifteenth neighbour, past the smallest kind, as the lens
exploration predicted. With structural features it found three kinds and
exact rules for them, but nothing about what each kind holds or which rows
break it.

Two jobs had been folded into one format: a form that is **lossless**, for
identity and interchange, and a form that is **readable**, for a person or a
model. The first exists already in the canonical record form
([ADR-0201](./0201-leeway-canonical-record-form.md), [ADR-0210](./0210-leeway-canonical-wire-generator.md))
and its diagnostic notation ([ADR-0219](./0219-play-canonical-record-identity.md) §SD6).
The second does not.

## Decision

Separate the two jobs. Identity and losslessness are the canonical forms';
reading is one read model's. The batch-level reading is the Projection
panel's, with the lens's archetype form inside it. Everything else on those
paths is removed.

### SD1 — Two jobs, two forms

- **Lossless.** The canonical record form and the canonical wire form, their
  digests, and the RFC 8949 diagnostic notation of their CBOR items. They are
  the forms a reader quotes, compares or stores when exactness matters.
- **Readable.** The read model of SD3: lossy by design, every loss counted.

Card JSON is neither and is removed; this ADR supersedes ADR-0018.

### SD2 — The canonical forms reach agents

The Detail pane already shows both digests and, behind a disclosure, the
items in diagnostic notation (ADR-0219 §SD5, §SD6). An agent gets only the
digests through `get_detail`. A play query operation returns, for one row, the
canonform digest, the canonwire fingerprint, the verdict, and both items in
diagnostic notation under the operations' byte bound, with a flag when the
bound cut them.

### SD3 — One read model for rows

A read model, built by one sink, is what the Detail card draws and what
`get_detail` returns:

- **Names.** An attribute is named by its first membership, rendered through
  the membership registry's formatter, its parameters spelled into the name;
  further memberships are its labels. A section name qualifies an attribute
  only when two sections name one. A positional key is never a name.
- **Values.** One spelling for every text form, each value beside its
  canonical type: strings and numbers as the driver writes them; bytes as
  text when they are printable UTF-8, else 0x-hex; lists as lists, cut with
  an explicit count of what was left out; sets in value order; times in ISO
  8601. Each value keeps the driver's own text too, which a gloss reads.
- **Header.** Per result: each attribute's name, section, type, how many rows
  carry it, and the handle an agent writes SQL with
  ([ADR-0171](./0171-leeway-sql-read-surface.md)) — `LW_GET` naming a ref by
  its registry id, which resolves whatever renderer named the attribute.
- **Omissions.** Columns the readability aspect hides, values cut, attributes
  over the bound: each counted, none dropped silently.

The read model is `lwread`. `Table2CardEmitter` retires as a sink; its
drawing stays as `leewaywidgets.RecordCard`, which lays out an `lwread.Model`
and keeps the card's gloss and block-face seams, fed the driver's text.

### SD4 — The lens lives in the Projection panel

The lens's model, analysis and plan (`lwlens`) and its view
(`leewaywidgets.LensView`) move from the Experiments pane into the Projection
panel:

- **One clustering.** The lens draws its bands from the run's clusters,
  passed in as labels per row. The lens's own clustering is removed. Its
  template refinement stays: a row sharing under half its cluster's template
  is drawn with the unclustered rows, where the graph keeps HDBSCAN's label.
- **One run.** The lens model is collected by a further pass of the run's
  record batch through `lwlens.Sink`, with the registry's ref formatter, over
  the rows the run projected.
- **One panel, four views.** The panel shows the neighbour graph, or the
  lens's archetypes (each cluster as its template, extremes and exceptions),
  its rows (every row in its cluster's band), or one row among its peers —
  the row the shared selection names. The lens's two intents stay as the
  panel's sliders.
- **Agents read the archetypes.** The archetype computation moves out of the
  painter into `lwlens` as data — per cluster its rule, its template, its
  extremes and its exceptions with their counts — so the view paints it and a
  query operation returns it.

### SD5 — Projection defaults that find kinds

- **Features.** Structure is the default feature set: a leeway batch's kinds
  are which attributes its rows carry.
- **Core distance.** HDBSCAN's core distance is read at the
  `min(K, minCluster − 1)`-th neighbour of the run's neighbour lists; the
  graph and its layout keep `K`. A kind of at least `minCluster` identical
  rows then has core distance zero, however many neighbours the layout uses.
- **Minimum cluster.** The default falls from 10 to 5.

### SD6 — What is removed

- In `leeway/card`: `JsonCardEmitter`, `JsonCardSchemaEmitter`,
  `UnicodeCardEmitter`, `TopologySpark`, `BrailleSpark`, `TreemapSpark`, and
  the `lw card inspect` command that drove them. `Driver.DriveSchema`, whose
  one consumer was the schema emitter. The item, structure and shape-feature
  extractors stay: the Projection panel is built on them.
- In `leewaywidgets`: the topology, chart, graph and hierarchy sinks and
  views. The fixture, the lens and `Table2CardEmitter` (until SD3) stay.
- In play: the Experiments pane, `get_experiments` and `set_experiments`,
  `BOXER_PLAY_EXPERIMENTS`, and the pane's scenes.
- vizeval: the harness, judges, geometry metrics, scorecard store, CLI,
  scenarios, script, skill and how-to. Its vocabulary ordinals (122–149 and
  243) stay unused. ADR-0266 is withdrawn.

### SD7 — Milestones

- **M1 — This ADR.** ✓
- **M2 — The lens as data.** ✓ `lwlens.Analyze` takes labels; the archetype
  readings move into `lwlens`; `LensView` paints them.
- **M3 — The lens in the Projection panel.** ✓ The run's lens pass, the four
  views, the archetypes operation, and the SD5 defaults.
- **M4 — Removal.** ✓ SD6.
- **M5 — The canonical operation.** ✓ SD2.
- **M6 — The read model.** ✓ SD3: the sink, the Detail card on it,
  `get_detail` on it, `Table2CardEmitter` removed.

## Alternatives

- **Fix card JSON in place** — a name-first classifier default, the registry
  formatter, the per-entity section list dropped. Rejected: the result is
  still a format whose only justification is losslessness, without the
  parser that would establish it, beside a canonical form that is lossless
  and identity-bearing already.
- **Keep the Experiments pane and vizeval for future rendering work.**
  Rejected: once the sinks that only the pane hosts are gone, nothing is left
  to compare. The scene runner ([ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md))
  captures a panel for a one-off comparison, as it did for this decision.
- **Keep the lens's own clustering beside the panel's.** Rejected: two
  clusterings of one batch in one panel would disagree, and the panel's
  graph, rules and archetypes would describe different groups.
- **A lens panel of its own**, reading the Projector's result (the lens
  exploration's open item). Rejected for now: a second panel over one
  clustering splits the reader's attention between two tabs that must stay
  in step; one panel with views keeps the selection, the clusters and the
  numbering shared.
- **A per-kind markdown projection of rows** for agents reading many rows.
  Not taken: the batch summary is the Projection panel's, and the archetypes
  operation carries it.

## Consequences

### Positive

- An agent reads a batch's kinds, what each holds and which rows break it,
  in one bounded reply, under the cluster numbers the person sees.
- One clustering, one numbering, one selection across the graph, the rules
  and the archetypes.
- The forms a reader sees agree on names and value spelling once SD3 lands.
- About twenty thousand lines leave the tree, half of them a generated
  store.

### Negative

- Exported API is removed (SD6). A downstream module that drives the sparks,
  the Unicode card or card JSON stops compiling at its next boxer bump.
- The renderings vizeval scored can no longer be re-scored, and the
  scorecards in `boxer.facts` lose their reader.
- The Projection panel's defaults change; a person who relied on shape
  features clusters differently until they pick them again.

### Neutral

- The lens exploration's background document stays as the record of the
  judged rounds; the code it describes moved.

## Surfaces

| Surface | Change |
| --- | --- |
| Exported Go API under `public/` | `leeway/card` text emitters, `streamreadaccess.Driver.DriveSchema`, the `leewaywidgets` sinks, the `vizeval` packages removed; `lwlens.AnalyzeOptions` gains labels and loses its own clustering; archetype types added to `lwlens` |
| Named registries | `BOXER_PLAY_EXPERIMENTS` removed; vizeval vocabulary ordinals retired |
| play operations | `get_experiments`, `set_experiments` removed; an archetypes query added (M3), a canonical-forms query (M5); `get_detail` reshaped (M6) |
| CLI | `lw card inspect` and `imzero2 vizeval` removed |

## Migration — Tier 1

- **A caller of the card emitters** reads the canonical forms for exactness
  (`canonform`, `canonwire`, `cbor/diag`) and, after M6, the read model for
  display.
- **A caller of `DriveSchema`** reads the `TableDesc` it was built from, as
  `schemaview` already does.
- **An agent prompt naming `get_experiments`** gets an unknown-operation
  refusal; the archetypes query is its replacement for batch summaries.

## Verification plan — Tier 1

- `lwlens` unit tests over labelled models: bands follow the labels, the
  refinement moves a misfiled row, archetypes carry the planted missing slot,
  the extra slot and the rare label with its count.
- A Projection test over a mixed-kinds model: structural features at the
  defaults find the planted kinds.
- A play scene computes the projection over the mixed-kinds batch, switches
  to the archetypes view and captures it; the archetypes operation is
  exercised in the play ops tests.
- `go build`, `go vet` and the gov gate over the tree, so a dangling
  reference to a removed package fails the build.

## Status

Proposed — 2026-10-07.

## Updates

## References

- [ADR-0018](./0018-leeway-card-json-canonical-format.md) — card JSON, superseded by this ADR.
- [ADR-0266](./0266-vizeval-scored-renderings-of-leeway-batches.md) — vizeval (withdrawn).
- [ADR-0201](./0201-leeway-canonical-record-form.md), [ADR-0210](./0210-leeway-canonical-wire-generator.md), [ADR-0219](./0219-play-canonical-record-identity.md) — the canonical forms.
- [ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md), [ADR-0235](./0235-explaining-a-clustering-threshold-tree-and-rank-contrasts.md), [ADR-0238](./0238-projection-feature-sets-hashed-structural-identity.md) — the Projection panel.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — play's operations.
- [The lens exploration](../adr-background-work/leeway-lens-structure-values-local-stable.md).
