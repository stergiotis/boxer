---
type: adr
status: proposed
date: 2026-09-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; the implementation in the tree follows it and is up for review with it.

# ADR-0235: explaining a clustering — a threshold tree and rank contrasts over the feature matrix, read in the Projection lane

## Context

`play`'s Projection tab ([ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md)
§SD4) turns a result into sixteen shape features per leeway card, builds the
neighbour graph, lays it out, and colours it by HDBSCAN's labels. What the
tab shows is *that* the rows fall into groups; nothing in it says *why*. The
one reading available is "colour by" one feature at a time and looking for
a blob that changes tone — sixteen features, one at a time, by eye.

The features are continuous and the clustering is a density clustering:
there are no centres, a cluster is any shape the neighbour graph allows, a
share of rows is noise, and each member carries a probability. The
question a user asks of a blob is "what is it in these rows that put them
together", and the answer has to be checkable against the picture rather
than trusted — a description that reproduces 60% of the labelling is a
different thing from one that reproduces 98%, and the reader has to be
told which.

The literature on interpretable clustering (Hu et al., *Interpretable
Clustering: A Survey*, 2024) separates methods by stage — a model that is
interpretable while clustering, versus a post-clustering explanation of a
labelling made elsewhere — and by the form the explanation takes: threshold
trees, rules, prototypes, polyhedra, feature descriptions. The
post-clustering threshold tree is the line of Moshkovitz, Dasgupta, Frost
and Rashtchian (ICML 2020) and its followers (ExKMC 2020, the shallow-tree
bounds of Laber and Murtinho 2021, Makarychev and Shan 2021), all of which
are stated for *k*-means: they fit around centres and measure the price of
explainability as a ratio of *k*-means costs. The rule form is the
supervised descriptive rule discovery family — subgroup discovery,
contrast-set mining, emerging patterns (Novak, Lavrač and Webb 2009) —
which mines conjunctions over *discretised* attributes for a class and
scores them by coverage and precision. A 2026 comparison of the post-hoc
tools most used in practice (surrogate forests with permutation importance,
LIME, PCA) finds that none of them reliably recovers the structured
pattern that made a cluster, and asks for methods built for the question
rather than repurposed importance scores.

Constraints carried over from the engine records: results are a function
of the input alone, every function takes a context and returns a
`Truncation` ([ADR-0229](./0229-graph-analytics-engine.md) §SD2, §SD4);
the dependency rule of [why-boxer P1](../explanation/why-boxer.md); the
interactive budget of ten thousand rows on one machine.

## Design space (QOC)

**Question.** In what form does a clustering over a feature matrix get
explained to the person looking at it, and how is the explanation's
faithfulness stated?

**Options.**

- **O1** — Per-cluster feature contrasts: for each cluster and feature, a
  statistic of how the members' values sit against the rest — a rank
  statistic (the Mann–Whitney AUC) and the two medians — ranked by effect.
- **O2** — A threshold tree fitted to the labels (CART over the features
  with the Gini criterion), each leaf read as a rule of single-feature
  thresholds, the share of the labelling it reproduces stated as fidelity.
- **O3** — Class association rules or subgroup discovery over the
  features discretised into bins, one class per cluster, scored by support,
  confidence and lift, pruned for redundancy.
- **O4** — A centre-based explainable tree (IMM / ExKMC): the tree that
  minimises the *k*-means cost of the partition it induces.
- **O5** — A surrogate classifier with feature importance (a forest with
  permutation importance, or SHAP over it).

**Criteria.**

- **C1** — Faithfulness is stated: the explanation comes with a measure of
  how much of the labelling it reproduces, computed, not assumed.
- **C2** — A cluster reads in a sentence: thresholds in the feature's own
  units, a handful of terms, no bins to decode.
- **C3** — Fits the data as it is: continuous features, no centres, noise
  rows, membership probabilities; no discretisation the user did not choose.
- **C4** — Determinism and budget per ADR-0229: a function of the input,
  cancellable, truncation reported.
- **C5** — Lines owned versus dependencies rented (P1).
- **C6** — Interactive at ten thousand rows by sixteen features.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −  | ++ | +  | ++ | −− |
| C2 | +  | ++ | +  | ++ | −  |
| C3 | ++ | ++ | −  | −− | +  |
| C4 | ++ | ++ | +  | ++ | −  |
| C5 | ++ | ++ | +  | +  | −  |
| C6 | ++ | ++ | −  | +  | +  |

O5 is not white-box: an importance score names a feature without saying
which way or where, and the comparison cited above finds it misses the
pattern; it fails the question. O4 is stated for centres and a cost the
clustering here does not have — HDBSCAN's clusters are density level sets,
and fitting a *k*-means cost around their means would explain a clustering
that was not made. O3 answers the same question as O2 in a form that needs
a discretisation first, and the quality of every rule then rests on the
bins; over continuous features a tree chooses its thresholds from the data,
which is the discretisation O3 would have to be given. O3 is not rejected
for what it does best — itemsets over *categorical* content, where there is
nothing to threshold — and is deferred on that trigger (§SD5). O1 and O2
answer different halves: O1 says what is different about a cluster, one
feature at a time, with no claim about sufficiency; O2 says what criterion
reproduces it, with the fidelity stated. Neither replaces the other, and
both are cheap, so the decision takes both.

## Decision

We add a package `explain` under `public/analytics` that reads a labelling
back off the matrix it was made from in two forms — a threshold tree with
its fidelity, and per-label rank contrasts — and the Projection lane shows
both, per cluster, under the graph it colours.

**SD1 — A threshold tree fitted to the labels, CART with the Gini
criterion.** Input is a row-major `float64` matrix, a label per row with
`−1` for noise, `MaxDepth` and `MinLeaf`. Rows with a negative label are
left out of the fit and counted. At each node every feature is sorted, the
candidate thresholds are the midpoints between adjacent distinct values,
the split is the largest impurity decrease that leaves `MinLeaf` rows on
both sides, and ties go to the lower feature then the lower threshold, so
the tree is a function of the input. A node stops at purity, at
`MaxDepth`, under twice `MinLeaf` rows, or when no split lowers the
impurity. Each node carries its depth, parent, the per-label counts of the
rows reaching it and the majority label. A cancelled context stops
splitting — the nodes not yet visited become leaves — and the
`Truncation` says so. Each feature is sorted once over the fitted rows and
a node's order is that order filtered to its rows, so a node costs O(d·n)
and the whole fit a small fraction of the neighbour graph it follows; the
fit is single-threaded, since a parallel split search would only make the
tie-breaking rule harder to state.

**SD1a — One tree per label against the rest, beside the partition.** A
user's question of a blob is "what is in this cluster", which is a binary
question, and a multi-label tree answers a different one — how the space
partitions — in which a small cluster loses its leaf to the larger ones'
splits. So the package also fits, per label, a tree whose positive class is
that label and whose rest is every other row, noise included, since the
rest of the picture is what the blob is read against. Its rules for the
positive class are the cluster's; rules of different clusters may overlap
or leave gaps, which the partition tree's cannot, and each comes with its
own precision and recall. The partition tree stays for the case where the
rules must partition and for the one fidelity figure over the whole
clustering. Thresholds are the shortest decimal in the half-open interval
between the two adjacent values, preferring the one nearest the midpoint,
so a rule reads as `x > 37` rather than a midpoint's digits and its text
partitions the rows exactly as the split did.

**SD2 — The tree is read at a depth, not refitted.** Because the fit is
greedy top-down and every stopping rule is local, the tree cut at depth
*d* is the tree the same fit would grow to depth *d*. So the fit runs once
to a fixed depth and the readings take a cut: `Rules(depth)` yields one rule
per leaf of the cut — the path's terms merged to the tightest bound per
feature and side, with the leaf's label, the rows it covers, the hits
among them, and precision and recall against the label's total —
`Fidelity(depth)` the number of fitted rows whose cut leaf predicts their
label, and `LeafAt(row, depth)` the row's node by ancestry from its
full-depth leaf. A depth slider is therefore a view control with no
recomputation behind it. A rule set is spelled as SQL — terms joined by
`AND`, leaves by `OR`, thresholds in their shortest exact form — because a
rule's use after reading it is a `WHERE`, and a spelling that is not the
one it will be used in is a transcription the reader has to do.

**SD3 — Per-label contrasts by rank.** For each label and feature the
package computes the Mann–Whitney AUC — the probability that a member's
value exceeds a non-member's, ties counting half, from average ranks over
one sort per feature — and the members' quartiles beside the rest's
median, in the matrix's units. Noise rows are part of every label's rest,
because the question is how a blob differs from the rest of the picture,
noise included. The AUC is chosen over a standardised mean difference
because it is rank-based: indifferent to the log transforms the
preprocessing applies before clustering, unmoved by the heavy tails several
features have, and readable as a probability. `Ranked(label)` orders the
features by distance from one half.

**SD4 — The Projection lane shows both under the graph.** The rule
column is the SQL predicate of the cluster's leaves at the current depth,
with a copy action; a toggle chooses each cluster's own tree (the default)
or the partition tree, and the summary line states which is read and, for
the partition, its fidelity. The lane fits
the tree to a fixed depth over the *raw* feature values in slot order — a
threshold tree and a rank statistic are indifferent to the monotone
transforms the preprocessing applies, and the thresholds then read in the
feature's own units — in the projection goroutine after HDBSCAN, with the
smallest leaf sized as a share of the clustered rows, floored. A collapsible
section under the status line shows a summary line — how many clustered
rows the rules at the current depth reproduce, how many leaves — then one
row per cluster: its size, the rule of the leaf that catches most of it
with precision and recall and a count of the other leaves that predict it,
and the strongest contrasts with the two medians, listed only past an
effect floor. The depth slider is live (§SD2). A cluster the cut does not
reach says so rather than showing another cluster's rule. A fit that fails
shows its error in the section; the graph and the clusters above it are
unaffected, and a cancel during the fit truncates rather than fails the
run.

**SD6 — The same labels read against the card's attributes.** The
features say why the rows cluster; a second reading says what the rows
*are*. A second sink over the same card stream turns each entity into a
set of items — its tagged sections and co-section groups, a value of a
tagged section's column when the value is short printable text, a
category rather than content, and its low-cardinality memberships;
high-cardinality memberships and the plain section's values are
identity and are not items — and the vocabulary is
pruned to the items with a support in a band and capped, since an item
everyone has or almost no one has says nothing about a group. Items are
binary, so the readings are the supervised descriptive rule discovery
family (Novak, Lavrač and Webb 2009) rather than the continuous ones:
per label and item, the share of the label's rows holding it against the
rest's, the lift, the weighted relative accuracy, and a two-sided Fisher
exact test whose level is Bonferroni-corrected over every (label, item)
pair, so that a list of "what sets it apart" over hundreds of items does
not fill with chance; per label, the conjunction of at most three
literals — an item held or lacked — with the best weighted relative
accuracy against the rest, noise included, by a beam search that takes
a literal only for a stated relative gain and tie-breaks by fewer
negations then literal order. An item spells as a
predicate over the physical column it came from — a section as its first
value column being non-empty, a value as `has(column, value)` or an
equality on a plain column, a membership ref by the section's
low-cardinality reference column found by its physical name — so the
rule runs against the result as it is, unlike the feature rules of §SD2,
which wait for the feature columns. An item the sink cannot place — a
co-group, a membership whose column is not in the result — leaves the
rule marked rather than silently dropped. The reading is a description,
not the criterion: its precision and recall are stated against the
clustering, and a clean attribute rule for a cluster formed on shape is a
finding about the data, not a proof of the clustering.

**SD5 — Deferred, with triggers.** Probability-weighted fitting, when HDBSCAN's membership
probabilities are read as case weights and the trial shows the bridge
points move a rule. Highlighting a rule's misses on the graph — the rows
a leaf catches from another cluster — through the widget's emphasis pair
(ADR-0224 §SD14), when the lane's read-back seam (ADR-0231) carries a
multi-selection. Running a copied predicate against the result, which
needs the feature columns as columns: the client-call spelling of the
features, when the `ts*`-style vocabulary gains a second family.
Persisting the rules as facts beside the labels, in the follow-up ADR-0229
§SD5 names.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: an `explain` package under [`public/analytics`](../../public/analytics/) — `FitTree`, `Tree`, `Rule`, `Contrasts`, `Contrast` | the Projection lane as first consumer; the package-props table gains its row |
| Exported Go API under `public/` | added: `card.ItemExtractor`, `card.ItemSets`; `explain.ItemContrasts`, `explain.FindSubgroup` (§SD6) | the Projection lane as first consumer |
| `play` Projection tab | added: the "why these clusters" section, its depth slider, the per-cluster and by-attributes toggles; nothing existing reshaped | the help corpus entry for the tab |
| `boxer.facts` schema | unchanged | the fact kind is deferred (§SD5) |

## Alternatives

- **Explain in the preprocessed (log, z-scored) space.** Rejected: the
  tree and the AUC give the same answer there and in the raw space, and a
  threshold of 1.3 standard deviations reads worse than one of 37 bytes.
- **Refit the tree per depth on the render thread.** Rejected: the cut of
  a greedy tree is the shallower fit (§SD2), so the refit would compute
  what is already there, on the wrong thread.
- **One tree per cluster only, no partition tree.** Rejected: the
  per-cluster rules can overlap and leave gaps, so they carry no single
  fidelity over the clustering; the partition is kept beside them (§SD1a).
- **A rule list or decision set (CORELS, interpretable decision sets).**
  Rejected for the first cut: a search over rule space with a hard size
  bound is a heavier fit for a marginal gain in compactness over a cut
  tree, and its objective needs a discretisation first.

## Consequences

### Positive

- A cluster comes with a criterion and a stated fidelity, so a reader can
  tell a clean cut from a smear without trusting the picture.
- The two readings are cheap beside the neighbour graph and deterministic,
  so they can join the fact table later without a redesign.
- The package is general: any labelling over any matrix, not the sixteen
  card features; a consumer with its own columns and HDBSCAN labels reads
  them the same way.

### Negative

- Lines owned: a CART and a rank statistic, with their tests and the
  tie-breaking rules the determinism claim rests on.
- Two trees can disagree: the partition may give a small cluster no leaf
  where its own tree gives a clean rule. The lane shows one at a time and
  names which, rather than merging them.
- The contrasts describe one feature at a time; a cluster that exists only
  in a combination shows "no single feature stands out" beside a rule that
  does explain it — two readings that can disagree, by design.
- The attribute reading (§SD6) runs the exact test over every (label,
  item) pair: at the lane's cap and a few hundred items that is a few
  hundred milliseconds of hypergeometric sums, spent once per run in the
  projection goroutine.

### Neutral

- The explanation is of the labels the lane computed, over the features it
  computed them from. It says nothing about the rows' content beyond those
  features; that reading is §SD5's deferral.

## Migration — Tier 1

Nothing to migrate: the package is new and the lane's change is additive.

## Verification plan — Tier 1

- **Lane.** Default `go test`: a seeded box fixture the tree must recover to
  the threshold; a property test (`rapid`) that fidelity is monotone in the
  cut depth, every fitted row satisfies its cut leaf's rule, no leaf is
  smaller than `MinLeaf`, and the same input gives the same tree; the AUC
  checked against a pair count; the lane's row formatting over a
  two-cluster fixture.
- **What would fail.** A split that depends on row order or on a worker
  count breaks the determinism test; a rule a covered row does not satisfy
  breaks the property test; a cut that is not the shallower fit breaks the
  prefix test.
- **Gap.** No trial yet measures the explanation against a user's reading
  of the picture — whether the top rule is the one a person would have
  named. The fidelity figure is the proxy until one exists.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0229](./0229-graph-analytics-engine.md) — the engine's determinism and budget rules.
- [ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md) — the lane this record extends; HDBSCAN (§SD3).
- Moshkovitz, Dasgupta, Frost, Rashtchian. *Explainable k-Means and k-Medians Clustering.* ICML 2020. [arXiv:2002.12538](https://arxiv.org/abs/2002.12538)
- Frost, Moshkovitz, Rashtchian. *ExKMC: Expanding Explainable k-Means Clustering.* 2020. [arXiv:2006.02399](https://arxiv.org/abs/2006.02399)
- Hu et al. *Interpretable Clustering: A Survey.* 2024. [arXiv:2409.00743](https://arxiv.org/abs/2409.00743)
- Novak, Lavrač, Webb. *Supervised Descriptive Rule Discovery: A Unifying Survey of Contrast Set, Emerging Pattern and Subgroup Mining.* JMLR 2009.
- Lavrač, Kavšek, Flach, Todorovski. *Subgroup Discovery with CN2-SD.* JMLR 2004.
- Bay, Pazzani. *Detecting Group Differences: Mining Contrast Sets.* Data Mining and Knowledge Discovery 2001.
- *Beyond Feature Importance: A Comparative Analysis of Pattern Detection Methods in Cluster Interpretation.* 2026. [arXiv:2608.05880](https://arxiv.org/abs/2608.05880)
- Breiman, Friedman, Olshen, Stone. *Classification and Regression Trees.* 1984.
- Mann, Whitney. *On a Test of Whether one of Two Random Variables is Stochastically Larger than the Other.* 1947.
