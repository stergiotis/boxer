---
type: adr
status: accepted
date: 2026-09-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-15
---

# ADR-0238: a second feature set for the Projection lane — hashed structural identity, chosen per run

## Context

The Projection lane ([ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md)
§SD4) builds its neighbour graph over the sixteen shape features of
`card.EntityFeatures`: counts, byte totals, Gini and max-to-mean ratios,
tag statistics, role entropy, compression ratios, value lengths and
repetition. They say how big and how skewed a record is. They do not say
which sections or attributes it has.

The explanation step ([ADR-0235](./0235-explaining-a-clustering-threshold-tree-and-rank-contrasts.md))
made the consequence visible on a real table. Over six thousand sailing
events, HDBSCAN found seventy clusters, and the rules read
`total_value_bytes <= 121` for one and `total_value_bytes > 121 AND
total_value_bytes <= 122` for the next, each at full precision and recall:
track points with identical structure, sliced by the printed length of
their coordinates. The same table has nineteen distinct sets of attribute
names, and those nineteen are the kinds a person would name — track point,
motion sample, NMEA message, wind reading, analysed leg, manoeuvre. The
features are not too few; they answer a different question, and a density
clustering over a space where thousands of rows sit on a handful of byte
counts does what it must.

Two questions are worth asking of a result, and they want different
distances: *what kind of record is this* — structural identity — and *how
big or unusual is this record for its kind* — shape. Blending the two
into one vector needs a weight nobody can justify and gives a distance
that answers neither. ADR-0230 §SD4 left "the featurization record" as the
open question; this is its first entry.

## Decision

We add a second feature set — a hashed presence vector of a record's
structural items — and make the set a per-run choice of the lane,
alongside the neighbour count and the minimum cluster size.

**SD1 — `card.StructureMatrix`: signed feature hashing of structural
items.** From the item sets the item sink already extracts (ADR-0235
§SD6), each entity's structural items — its sections, co-section groups
and low-cardinality memberships, never its values — are hashed by name
into a fixed-width vector: the hash picks a column and a sign, the column
accumulates the sign (Weinberger, Dasgupta, Langford, Smola and
Attenberg 2009). Two entities with the same sections and attribute names
get the same row whatever their values or sizes. The width is a constant
of the package, 128, wide enough that a few dozen items per record rarely
collide and small enough that the exact *k*-NN stays under the shape
set's cost; the sign trick makes what collisions there are cancel in
expectation. The hash is FNV-1a, so the matrix is a function of the item
names alone and needs no vocabulary. Values stay out of the vector: a
value belongs to the attribute reading as an item, and in a distance it
would make two records of one kind far apart for carrying different
readings.

**SD2 — The distance follows the set.** The shape set keeps its
preprocessing and Euclidean distance. The structure set runs under
cosine, the distance the producer already offers (ADR-0230 §SD1), since
a signed presence vector is a direction and its norm is a count of
attributes, which the shape set already measures.

**SD3 — A run knob, not a blend.** The lane's `FeatureSet` is chosen in
the toolbar beside the neighbour count and applies on the next Compute;
it is recorded in the run's parameters and shown in the status line. The
shape set stays the default, so no existing capture changes. The
explanation reads the matrix the clustering ran on: the shape features
as thresholds, a binary set — component kinds, or the structural items
of the pruned item sets capped by support — as predicates, a column at
zero spelled as the negation, so a rule is a criterion of the picture
under every set.

**SD4 — Deferred, with triggers.** Values as items in the vector, behind
a support floor, when a consumer's kinds are told apart by a categorical
value rather than by attribute names. A blend of the two sets with a
declared weight, when a trial shows a consumer needs both in one
picture. Recording the choice as a query setting, with the rest of the
lane's parameters, in the client-call spelling ADR-0235 names.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: `card.StructureMatrix`, `card.StructureDims` | the Projection lane as first consumer |
| `play` Projection tab | added: the features picker; the status line names the set; nothing existing reshaped | the help corpus entry for the tab |

## Alternatives

- **Append the identity block to the sixteen.** Rejected: one Euclidean
  distance over presence bits and z-scored counts weights the two by
  accident, and the accident would decide the picture.
- **Drop the byte-scale features instead.** Rejected as the whole answer:
  it softens the slicing but leaves the set blind to identity; kept as a
  follow-up for the shape set on its own terms.
- **Cluster the item sets under Jaccard directly.** Rejected for the first
  cut: the producer's metrics are Euclidean and cosine, and the signed
  hashed vector under cosine is the standard approximation with no new
  producer code.
- **A learned embedding of the structure.** Rejected: a dependency and a
  training step for a question presence bits answer.

## Consequences

### Positive

- Kinds become separable by what they contain; on the sailing table the
  clusters are expected to track the nineteen signatures, which is a
  ground truth the lane can be scored against.
- The two questions get two distances instead of one compromise, and the
  choice is recorded with the run.

### Negative

- One more run knob a user has to know about; the default keeps the old
  picture, so the new one has to be chosen.
- The structure set depends on the item sink, so a result the sink cannot
  read fails the run under that set rather than falling back silently.

### Neutral

- Colour-by stays over the shape features under both sets: they are
  computed either way and a size gradient over structural clusters is a
  useful reading.

## Migration — Tier 1

Nothing to migrate: the default is the set the lane always had.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the structure matrix gives equal rows to
  entities with equal sections and tags and different values, unequal
  rows for one more tag, and is deterministic. A trial under
  `doc/trials/` over the sailing table, where the nineteen signatures are
  known: the clusters under the structure set against the signatures, by
  the attribute rules' precision and recall.
- **What would fail.** A value leaking into the vector breaks the
  equal-rows test; a change of hash or width changes every picture and
  the trial.
- **Gap.** The trial is not yet written; the first live run is recorded
  under Updates until it is.

## Status

Accepted 2026-09-15.

## Updates

### 2026-09-15 — first live run, before the trial

Six thousand rows of the sailing table, the neighbour count and the
minimum cluster size at their defaults. The sample holds ten structural
signatures, eight of them with at least the minimum cluster size of rows.
Under the shape set the lane reports seventy clusters; under the
structure set, seven with no noise, and the attribute reading gives
every one of the seven a single-item rule at full precision and recall —
motion samples, NMEA schedule, wind reading, raw field, track point,
analysed leg, manoeuvre. The one merge is the two manoeuvre signatures,
which differ by a single attribute. The same run showed the item support
floor must not exceed the minimum cluster size, or the items that tell
two small kinds apart are pruned before the search sees them; the lane
now caps the floor there. A condition of this reading: the sample is the
table's first rows, not a uniform draw.

### 2026-09-15 — the run as data: publish as ad-hoc datasets

A **publish as dataset** button in the tab's toolbar writes the current
run as two ad-hoc datasets over ADR-0134's store, the way imzrt
publishes a profile: each dataset is republished onto this projector's
own stable handle, so its revision bumps, and the scaffold names it by
that handle as a literal, `keelson('<handle>')` — no alias is bound into
this play instance, so another instance reads it the same way and two
instances publishing at once do not replace each other. The rows dataset
holds one row per projected entity — the result's row index and its plain
identity columns, the sixteen features in their own units under the names
the rules use, the cluster numbered as the tab shows it with noise at −1,
HDBSCAN's probability, the layout position, the feature set, and the item
set the attribute reading used as an array of item names — and the rules
dataset one row per cluster and reading with the SQL predicate, precision,
recall and coverage. A scaffold query is offered at the caret once per
publish. This is what makes a copied feature rule runnable
today, against the published rows rather than the source, and it is the
recorded, replayable form of what the panel computed, which ADR-0163's
argument against panel-side analysis asks for. A publish replaces the
datasets and bumps their revision, so it is a button rather than a side
effect of every run. The datasets are session-only by the store's design;
persisting the kinds as facts stays ADR-0229 §SD5's follow-up.

### 2026-09-15 — rules over handles, and the archetype as a third set

Two changes after review. First, every predicate the attribute reading
emits is now spelled the way the authoring surface writes it: over a
column handle, `section:column` for a value or a section's presence and
`section:lv` / `section:lr` for a low-cardinality membership (ADR-0116),
which play's handle pass resolves to the physical column before the
statement ships; no physical name appears in a rule. A registered
component spells as `LW_COMPONENT_FILTER('Kind')`, which the component
pass expands to the kind's conformance filter (ADR-0189). Second, the
registered component kinds a row carries — detected per row through the
Detail pane's binders, the same `Detect` the components chapter uses —
join the item sets as `component:<Kind>` items and form a third feature
set, *components*: one column per bound kind, one where the row carries
it, under cosine. It is the archetype of ADR-0146 D5 as a vector, and it
is only defined for a facts-shaped result; under any other result the set
refuses the run and says why, while the items simply do not appear. The
structure set stays the answer for a result no registered component reads.

## References

- [ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md) — the lane and the producer's metrics.
- [ADR-0235](./0235-explaining-a-clustering-threshold-tree-and-rank-contrasts.md) — the explanation that exposed the slicing; the item sink (§SD6).
- Weinberger, Dasgupta, Langford, Smola, Attenberg. *Feature Hashing for Large Scale Multitask Learning.* ICML 2009.
