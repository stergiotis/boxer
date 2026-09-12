---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-12 as follow-on
> material for [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md),
> [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) and
> [ADR-0226](../adr/0226-graph-analytics-engine.md). Nothing here is a
> decision. Provenance: no Graphistry or PyGraphistry source was read,
> fetched, cloned, installed or searched — no source-code host, no package
> archive, no readthedocs source view, no "[source]" link. The inputs are the
> vendor's public documentation as served on the compile date — the hub
> documentation pages, the PyGraphistry API reference as rendered on
> readthedocs (docstrings and signatures), the rendered demo notebooks and
> guides on the same site, and one vendor blog post — plus the third-party
> documentation those pages delegate to (umap-learn, skrub, scikit-learn,
> Sentence Transformers) and the UMAP paper. Behaviour not stated on those
> pages is not asserted; where this page describes what Graphistry does, the
> sentence or signature it rests on is quoted. Claims about this repository
> were checked against the working tree on the compile date. Effort figures
> are estimates, not measurements.

# Graphistry against graphview, nav and the analytics engine — UMAP as a graph, and the feature engineering behind it

## 1 Question, scope and method

The [Cytoscape.js and Ogma analysis](./graph-viewer-gap-analysis-cytoscape-ogma.md)
read two graph *viewers* against the widget. Graphistry is a different kind
of reference: its viewer is a GPU force layout with histograms beside it,
and the part worth reading is upstream of the viewer — a pipeline that turns
a table with no edges into a graph, by running UMAP over automatically
derived features and keeping UMAP's neighbour graph as an edge set. This
page asks two questions of that pipeline: how the dimensionality reduction
is joined to the graph, and what the feature engineering underneath it is;
then where each piece would land here.

The tree this is measured against is the
[graphview widget](../../public/thestack/imzero2/egui2/widgets/graphview/)
and its [nav package](../../public/thestack/imzero2/egui2/widgets/graphview/nav/),
the [graph analytics engine](../../public/analytics/graph/) of ADR-0226,
the `play` graphview panel and the SQL graph contract it reads
([ADR-0225, play panel](../adr/0225-play-graphview-panel.md);
[ADR-0129 §SD2](../adr/0129-play-layered-graph-panel.md)), and — because it
already exists and is the nearest thing to the subject — `play`'s
Projection panel, which runs UMAP client-side over a leeway card
([`apps/play/play_projection.go`](../../apps/play/play_projection.go),
[`leeway_card_features.go`](../../public/semistructured/leeway/card/leeway_card_features.go)).

The classification keeps the four buckets of the sibling analysis and adds
two, because the subject crosses two more boundaries: **covered** (an
existing API is named); a **widget gap** (a change inside graphview); a
**helper gap** (nav or a sibling package above the widget); an **engine
gap** (a change in or beside `public/analytics/graph` under ADR-0226); a
**lane gap** (a change in `play`'s data lanes or the SQL contract); or
**out of scope** with a reason.

**Skipped**, one line each:

- The GPU substrate — `engine='cuml'`, cuDF, cuGraph. The tree's equivalent
  decision is ADR-0226 (Go, in-process, `CGO_ENABLED=0`).
- `memoize` and `inplace` — Python object-lifecycle conveniences.
- The hosted viewer's URL and REST surface, sharing, workbooks.
- The graph neural network route — `embed()`, `build_gnn()`,
  `predict_links()` — read in §2.4 for shape only; a torch dependency is
  outside the dependency rule (why-boxer P1) and is not weighed further.
- Graphviz, igraph and cuGraph layout plugins — the layered reading is the
  Network tab's ([ADR-0129](../adr/0129-play-layered-graph-panel.md)).

## 2 Graphistry — UMAP as a graph

### 2.1 Two directions: coordinates in, edges out

The `umap()` docstring is the whole design in one sentence: "UMAP the
featurized nodes or edges data, or pass in your own X, y (optional)
dataframes of values". The documented signature:

```
umap(X=None, y=None, kind='nodes', scale=1.0, n_neighbors=12, min_dist=0.1,
     spread=0.5, local_connectivity=1, repulsion_strength=1,
     negative_sample_rate=5, n_components=2, metric='euclidean', suffix='',
     play=0, encode_position=True, encode_weight=True, dbscan=False,
     engine='auto', feature_engine='auto', inplace=False, memoize=True,
     umap_kwargs={}, umap_fit_kwargs={}, umap_transform_kwargs={},
     **featurize_kwargs)
```

Two parameters carry the join to the graph, and the docstring is explicit
about both:

- **Coordinates as positions.** `encode_position`: "whether to set default
  plotting bindings – positions x,y from umap for .plot(), default True".
  `suffix`: "optional suffix to add to x, y attributes of umap". The
  positions are then ordinary bound columns, and the Plotter reference
  states the contract for any bound position: `point_x` is an "Attribute
  overriding node's initial x position. Combine with
  ".settings(url_params={'play': 0}))" to create a custom layout". `play`
  on `umap()` is "Graphistry play parameter, default 0, how much to evolve
  the network during clustering. 0 preserves the original UMAP layout", and
  the URL reference defines it as "Number of milliseconds to run layout on
  pageload."
- **The neighbour graph as edges.** `encode_weight`: "if True, will set new
  edges_df from implicit UMAP, default True". `scale`: "multiplicative scale
  for pruning weighted edge DataFrame gotten from UMAP, between [0, ..) with
  high end meaning keep all edges". The pruning helper is documented
  separately, `prune_weighted_edges_df_and_relabel_nodes(wdf, scale=0.1,
  index_to_nodes_dict=None)` — "Prune the weighted edge DataFrame so to
  return high fidelity similarity scores", with `scale` "lower values means
  less edges > (max - scale * std)" — and `filter_weighted_edges(scale=1.0,
  …)` is "Filter edges based on _weighted_edges_df (ex: from .umap())". The
  10-minute guide says what the user sees: "UMAP will identify nodes that
  are similar across their different attributes", and "PyGraphistry records
  and renders the similarity edges between similar entities", with the
  example `g1 = graphistry.umap(X=['attackerIP', 'victimIP', 'vulnName'])`
  followed by `print('# similarity edges', len(g1._edges))`.

So a table with no edges becomes a graph, and — since `umap()` is a method
on a graph object that may already carry edges — an existing graph gets a
similarity edge set that replaces its `_edges` for plotting. Whether the
original edges survive beside the new ones is not stated on the pages read
and is not asserted here.

The layout catalogue lists UMAP as a layout in its own right: "Reduces
high-dimensional data into a 2D layout based on similarity, best for
complex datasets needing dimensionality reduction."

### 2.2 What the neighbour graph is

The pages above call the edge set "implicit UMAP" and "similarity edges"
without saying what it contains. umap-learn's own documentation and the
paper do, and they are what make it a graph rather than a scatter with
lines drawn on it.

The construction, from *How UMAP Works*: the algorithm uses "a
*k*-neighbor graph instead of using balls of some fixed radius to define
connectivity"; each point's local metric is stretched so that "a unit ball
about a point stretches to the *k*-th nearest neighbor of the point"; the
nearest neighbour is connected with certainty — "we should have complete
confidence that the open set extends as far as the closest neighbor of
each point" — and membership beyond it decays with distance, "the fuzzy
confidence decay in terms of distance *beyond* the first nearest neighbor".
The directed *k*-NN graph is symmetrised as a fuzzy union: "if we want to
merge together two disagreeing edges with weight *a* and *b* then we should
have a single edge with combined weight a + b − a·b". The layout is then
"minimizing the cross entropy as a kind of force directed graph layout
algorithm", where "The first term … provides an attractive force between
the points … the second term … provides a repulsive force between the ends
of e whenever w_h(e) is small."

The parameters page gives the two knobs their meaning. `n_neighbors`
"controls how UMAP balances local versus global structure in the data":
"Low values of `n_neighbors` will force UMAP to concentrate on very local
structure (potentially to the detriment of the big picture), while large
values will push UMAP to look at larger neighborhoods of each point". And
`min_dist` "controls how tightly UMAP is allowed to pack points together"
— a layout parameter, which does not enter the graph.

Two consequences for reading Graphistry's edge set. It is a
*k*-nearest-neighbour graph with locally scaled fuzzy membership weights,
symmetrised — degree bounded by roughly `n_neighbors` per vertex, weights
in (0, 1], undirected after the union. And the picture drawn with `play=0`
is that graph laid out by UMAP's own cross-entropy optimiser, while with
`play>0` it is the same graph re-laid-out by the viewer's force layout from
those positions. The paper (McInnes, Healy & Melville 2018) is the citation
for the construction.

### 2.3 Clusters, new batches, targets

**DBSCAN over the embedding.** `dbscan(min_dist=0.2, min_samples=1,
cols=None, kind='nodes', fit_umap_embedding=True, target=False, …)`:
"DBSCAN clustering on cpu or gpu infered automatically. Adds a _dbscan
column to nodes or edges." `fit_umap_embedding` is "whether to use UMAP
embeddings or features dataframe to cluster DBSCAN"; `min_dist` is "The
maximum distance between two samples for them to be considered as in the
same neighborhood." The documented use is a colour: "Enriching the graph
with cluster labels from UMAP is useful for visualizing clusters in the
graph by color, size, etc, as well as assessing metrics per cluster", and
the example ends `g2.plot() # color by `_dbscan` column`. The same page
documents the chaining shapes: `g.umap(kind=kind).dbscan(kind=kind)`,
`g.umap(dbscan=True, min_dist=1.2, min_samples=2, **kwargs)`, and
`g.featurize().dbscan(**kwargs)` — "here dbscan is infered from features,
not umap embeddings".

**Placing a new batch against a fitted model.** `transform_umap(df, y=None,
kind='nodes', min_dist='auto', n_neighbors=7, merge_policy=False,
sample=None, *, return_graph=True, fit_umap_embedding=True, …)`:
"Transforms data into UMAP embedding". `merge_policy`: "if True, use
previous graph, adding new batch to existing graph's neighbors useful to
contextualize new data against existing graph. If False, sample is
irrelevant." `sample`: "Sample number of existing graph's neighbors to use
for contextualization – helps make denser graphs". `min_dist` here is
"Epsilon for including neighbors in infer_graph", and `fit_umap_embedding`
"Whether to infer graph from the UMAP embedding on the new data, default
True". `transform_dbscan` is the same idea one step on: it "generates a
graph with the minibatch and the original graph, with edges between the
minibatch and the original graph inferred from the umap embedding or
features dataframe", with the caveat "infered graph supports kind='nodes'
only" and "currently unsupported on GPU". The GFQL guide's example is the
canonical shape: `g1 = graphistry.nodes(df_sample).umap(X=['col_1', ...,
'col_n'], y='col_m')` then `g2 = g1.transform_umap(batch_df,
return_graph=True)`.

**Supervised UMAP.** `y` on `umap()` is "either an dataframe ndarray of
targets, or column names to featurize targets"; the featurizer encodes it
with its own thresholds (§3.3). The rendered botnet notebook is the one
worked example on the pages read: `g.umap(kind='edges',
X=good_cols_with_edges, y=['bot'], use_scaler='quantile',
cardinality_threshold=20, n_topics=20, n_topics_target=7, n_neighbors=12)`.
What the target does to the neighbour graph is not stated on Graphistry's
pages; umap-learn's `y` semantics are the reference and are not restated
here.

**The other embedding route.** `embed(relation, proto='DistMult',
embedding_dim=32, …)` trains a knowledge graph embedding — the cheatsheet:
"Train a RGCN model and predict" — with `predict_links_all()`, which
"Predicts absent links across the entire knowledge graph efficiently". Out
of scope; noted so the two routes are not confused. One is a metric
embedding of rows that yields a similarity graph; the other is a learned
embedding of an existing graph that yields link scores.

### 2.4 Layouts that consume analytics

**Group-in-a-box.** The vendor's blog describes the algorithm in three
steps — it "Finds groups in the graph using community detection", "Places
each group into an grid cell (box) ordered via a *treemap*", and "Lays out
the nodes in each group with a graph layout algorithm" — and records that
the original "was designed to handle graphs with hundreds of nodes, and
would take hours or even crash on bigger graphs". The API reference:
`group_in_a_box_layout(partition_alg=None, partition_params=None,
layout_alg=None, layout_params=None, x=0, y=0, w=None, h=None,
encode_colors=True, colors=None, partition_key=None, engine='auto')`,
"supporting both CPU and GPU execution modes"; `partition_key` names a
node column already holding the partition, so the community step can be
supplied rather than computed.

**Modularity-weighted.** `modularity_weighted_layout(g, community_col=None,
community_alg=None, community_params=None, same_community_weight=2.0,
cross_community_weight=0.3, edge_influence=2.0, engine=…)`; the catalogue:
"Lays out clusters based on modularity, optimizing for visualizing
community structures." From the signature: a community label per node
(given or detected), an edge weight of 2.0 inside a community and 0.3
across, and the viewer's edge-influence knob raised so the weights bite.

**Rings.** Three siblings with one shape. `ring_categorical_layout` is a
"Radial graph layout where nodes are positioned based on a categorical
column ring_col"; `ring_continuous_layout` the same "based on a
numeric-typed column ring_col", with `v_start`/`v_end` ("Value at innermost
axis (at min_r)" / "at max_r"), `num_rings`, `ring_step` ("Distance between
rings in terms of pixels") and `axis` labels; `time_ring_layout` the same
"based on a datetime64-typed column time_col" with `time_unit`. Each takes
`play_ms`, "Initial layout time in milliseconds". The mechanism is stated
by the `layout_settings` example, titled "Animated radial layout": nodes
carry `x`/`y`, and `.layout_settings(locked_r=True, play=2000)` runs the
force layout with the radius frozen — the URL reference's `lockedX`,
`lockedY`, `lockedR`: "Prevent a point from moving based on x position, y
position, and radius." So a ring layout is a radius per node from a
column, and a force layout free in the angle. (The ring pages disagree
with themselves on one default: the signatures print `play_ms=0` for the
categorical and continuous forms and the parameter text says "default
2000"; the time form prints 2000 in both.)

**The force layout's knobs.** `layout_settings(play, locked_x, locked_y,
locked_r, left, top, right, bottom, lin_log, strong_gravity,
dissuade_hubs, edge_influence, precision_vs_speed, gravity,
scaling_ratio)` — "Set layout options. Additive over previous settings.
Corresponds to options at" the URL reference, which defines them: `linLog`
"Linear/logarithmic scaling controls for graph layout algorithms",
`strongGravity` "Gravitational pull control", `dissuadeHubs` "Reduce hub
dominance", `edgeInfluence` "Determines the impact of edge weight on
layout; higher values give more consideration", `precisionVsSpeed`
"Balances computation between precision and speed; less node movement
yields higher quality", `gravity` "Sets gravitational strength towards
canvas center", `scalingRatio` "Adjusts the scale of nodes and edges in
the graph." The cuGraph page names the model: "Graphistry's default layout
algorithm is already GPU-accelerated FA2 with additional configurations" —
ForceAtlas2 (Jacomy et al. 2014), whose paper is where lin-log, strong
gravity and hub dissuasion come from.

**Edge weight into the layout.** The Plotter binding: `edge_weight` is an
"Attribute overriding edge weight. Default is 1. Advanced layout controls
will relayout edges based on this value." The edge-weights notebook: "Edges
with high edge weights bring their nodes closer together; edges with low
weight allow their nodes to move further appart"; "Edge weight values
automatically normalize between 0 and 1"; and "The edge influence control
guides whether to ignore edge weight (`0`) and use it primarily (`7+`)".
This is the channel the UMAP edge weights, the modularity weights and any
computed relevance all flow through.

### 2.5 The compute surface

`compute_igraph(alg, out_col=None, directed=None, use_vids=False, params={},
…)` — "Enrich or replace graph using igraph methods", "igraph is a CPU-only
library" — and `compute_cugraph(alg, out_col=None, params={}, kind='Graph',
directed=True, G=None)` — "Run cugraph algorithm on graph" — both write a
node column named after the algorithm. The hub pages exercise `pagerank`,
`betweenness_centrality`, `louvain`, `k_core`, `clusters`,
`harmonic_centrality`, `eigenvector_centrality`, `katz_centrality`, BFS and
SSSP. Beside them, first-party: `get_degrees()` "Decorate nodes table with
degree info" ("Self-cycles are currently double-counted");
`get_topological_levels()` "Label nodes on column level_col based on
topological sort depth" with a cycle-breaking option; `hop()` — the
cheatsheet: "enables slightly more complicated edge filters"; `chain()` and
GFQL, serialisable ("`pattern.to_json()`"); `collapse(node, attribute,
column, …)` "Topology-aware collapse by given column attribute starting at
node"; and `hypergraph(raw_events, entity_types=None, direct=False, …)`,
which "creates a node for every unique value in the entity_types columns
(default: all columns). If direct=False (default), every row is also turned
into a node. Edges are added to connect every table cell to its originating
row's node, or if direct=True, to the other nodes from the same row."

### 2.6 The viewer's reading of a column

Histograms are how an analytics column reaches the eye. "A histogram
provides a look at how any attribute of the elements in a graph is
distributed"; "You can filter the graph on-the-fly by manipulating a
histogram. Simply click one of the bins, or click and drag across multiple
bins"; and the settings encode: "Gradient palettes: Values are
interpolated by blending nearby palette colors, similar to a heatmap …
Categorical palettes: Values are assigned exact palette colors in a
round-robin order." The UI index lists the panel's four verbs: "Chart &
analyze", "Filter", "Color", and "Size: Control node size based on
attribute values to understand what is important". The data brush closes
the loop the other way: "click and drag over a collection of nodes and
edges. A region of the screen will be selected, and the histogram panel
will highlight which values they represent." A timebar exists — the
JavaScript API has `toggleTimebars`, "Toggle timebars panel" — but no UI
page read documents what it does, so nothing about it is asserted.

## 3 Graphistry — the feature engineering

Everything in §2 runs over a matrix, and the matrix comes from
`featurize()`: "Featurize Nodes or Edges of the underlying nodes/edges
DataFrames." The hub's framing: it "streamlines the conversion of diverse
data types—like text, numbers, and booleans—into AI-ready formats", and
`umap()` forwards `**featurize_kwargs` to it. The documented signature:

```
featurize(kind='nodes', X=None, y=None, use_scaler=None,
          use_scaler_target=None, cardinality_threshold=40,
          cardinality_threshold_target=400, n_topics=42, n_topics_target=12,
          multilabel=False, embedding=False, use_ngrams=False,
          ngram_range=(1, 3), max_df=0.2, min_df=3, min_words=4.5,
          model_name='paraphrase-MiniLM-L6-v2', impute=True, n_quantiles=100,
          output_distribution='normal', quantile_range=(25, 75), n_bins=10,
          encode='ordinal', strategy='uniform', similarity=None,
          categories='auto', keep_n_decimals=5, remove_node_column=True,
          inplace=False, feature_engine='auto', dbscan=False, min_dist=0.5,
          min_samples=1, memoize=True, verbose=False)
```

### 3.1 Routing by type and cardinality

`X`: "Optional input, default None. If symbolic, evaluated against self
data based on kind. If None, will featurize all columns of DataFrame".
`remove_node_column`: "whether to remove node column so it is not
featurized, default True."

Categorical columns split on a threshold. `cardinality_threshold`: "skrub
threshold on cardinality of categorical labels across columns. If value is
greater than threshold, will run GapEncoder (a topic model) on column. If
below, will one-hot_encode. Default 40." `n_topics`: "the number of topics
to use in the GapEncoder if cardinality_thresholds is saturated. Default is
42, but good rule of thumb is to consult the Johnson-Lindenstrauss Lemma …
or use the simplified random walk estimate => n_topics_lower_bound ~ (pi/2)
* (N-documents)**(1/4)".

The encoders are skrub's, and skrub's documentation says what they do. The
`TableVectorizer` — "Transform a dataframe to a numeric (vectorized)
representation" — routes by the same rule: "String and categorical columns
with a number of unique values strictly smaller than this threshold are
handled by the transformer `low_cardinality`, the rest are handled by the
transformer `high_cardinality`", with `numeric` defaulting to
"passthrough" — "The transformer for numeric columns (floats, ints,
booleans)" — and `datetime` to a `DatetimeEncoder`. The `GapEncoder`:
"Encode string columns by constructing latent topics. This encoder can be
understood as a continuous encoding on a set of latent categories
estimated from the data. The latent categories are built by capturing
combinations of substrings that frequently co-occur." Its `n_components` is
the "Number of latent categories used to model string data." So a
high-cardinality string column becomes `n_topics` non-negative columns
fitted on the data at hand — no downloaded model, but a fit whose axes are
not stable across datasets.

Numeric pass-through and the datetime encoder are skrub's statements, and
skrub's own default for high cardinality is a `StringEncoder` where
Graphistry's docstring names the GapEncoder; whether Graphistry uses the
`TableVectorizer` as such or assembles the pieces itself is not stated on
the pages read.

### 3.2 Text

A column becomes *text* rather than *categorical* by a word count.
`min_words`: "sets threshold on how many words to consider in a textual
column if it is to be considered in the text processing pipeline. Set this
very high if you want any textual columns to bypass the transformer, in
favor of GapEncoder (topic modeling). Set to 0 to force all named columns
to be encoded as textual (embedding)". The default is 4.5 on `featurize()`
and 2.5 on the two `featurize_or_get_*_dataframe_if_X_is_None` helpers on
the same page — a disagreement between two documented defaults. Whether
the count is a mean over rows or something else is not stated.

Text is encoded one of two ways. By default, a sentence-transformer:
`model_name` is the "Sentence Transformer model to use. Default Paraphrase
model makes useful vectors, but at cost of encoding time. If faster
encoding is needed, average_word_embeddings_komninos is useful and produces
less semantically relevant vectors. Please see sentence_transformer
(https://www.sbert.net/) library for all available models." That library
is "the go-to Python module for using and training state-of-the-art
embedding and reranker models", with "over 25,000 pre-trained Sentence
Transformers models … available for immediate use on 🤗 Hugging Face" — a
model download at first use. Or, with `use_ngrams`: "If True, will encode
textual columns as TfIdf Vectors, default, False", tuned by `ngram_range`
("eg: tuple = (1, 3)"), `max_df` ("set max word frequency to consider in
vocabulary eg: max_df = 0.2") and `min_df` ("set min word count to consider
in vocabulary eg: min_df = 3 or 0.00001"). The semantic-search docstring
shows the idiom that forces the text path: `g.featurize(kind='nodes',
X=['text_col_1', ..], min_words=0 # forces all named columns are textually
encoded)`.

### 3.3 Targets

`y`: "Optional Target(s) columns or explicit DataFrame, default None".
Targets get their own thresholds. `cardinality_threshold_target`: "similar
to cardinality_threshold, but for target features. Default is set high
(400), as targets generally want to be one-hot encoded, but sometimes it
can be useful to use GapEncoder (ie, set threshold lower) to create
regressive targets, especially when those targets are textual/softly
categorical and have semantic meaning across different labels. Eg, suppose
a column has fields like ['Application Fraud', 'Other Statuses',
'Lost-Target scaling using/Stolen Fraud', 'Investigation Fraud', …] the
GapEncoder will concentrate the 'Fraud' labels together." `n_topics_target`
defaults to 12. `multilabel`: "if True, will encode a *single* target
column composed of lists of lists as multilabel outputs. This only works
with y=['a_single_col'], default False" — scikit-learn's
`MultiLabelBinarizer`, which "converts between this intuitive format and
the supported multilabel format: a (samples x classes) binary matrix
indicating the presence of a class label."

### 3.4 Imputation and scaling

`impute`: "Whether to impute missing values, default True". `use_scaler`:
"selects which scaler (and automatically imputes missing values using
mean strategy) to scale the data. Please see scikits-learn documentation …
Here 'standard' corresponds to 'StandardScaler' in scikits." The choices
are `'none' | 'kbins' | 'standard' | 'robust' | 'minmax' | 'quantile'`,
default `None` — so by default the matrix is *not* scaled. The parameters
are scikit-learn's and mean what its pages say: `n_quantiles` and
`output_distribution` ("normal", "uniform") for the `QuantileTransformer`,
which "transforms the features to follow a uniform or a normal
distribution … Note that this transform is non-linear"; `quantile_range`
for the `RobustScaler`, which "removes the median and scales the data
according to the quantile range"; `n_bins`, `encode` ("onehot,
onehot-dense, ordinal") and `strategy` ("uniform, quantile, kmeans") for
the `KBinsDiscretizer`; `keep_n_decimals`, "number of decimals to keep".
`scale()` exposes the step on its own, and `get_matrix(columns=None,
kind='nodes', target=False)` reads the result, "Most useful for topic
modeling, where the column names are of the form topic_0: descriptor,
topic_1: descriptor, etc."

The docstring lists `n_quantiles` and `output_distribution` twice with
different wording, and the `featurize()` signature says `n_quantiles=100`
where `scale()`'s says 10. Read the signature of the method you call.

### 3.5 Edges

`kind`: "specify whether to featurize nodes or edges. Edge featurization
includes a pairwise src-to-dst feature block using a MultiLabelBinarizer,
with any other columns being treated the same way as with nodes
featurization." That is the whole statement: an edge's endpoints become a
binary presence vector over the vertex set, beside whatever the edge's own
columns encode to. The botnet notebook is the worked example of
`kind='edges'`.

### 3.6 Presets, engines, and what is not documented

The hub names five presets — "Ngrams Model: For extracting Ngrams from text
data. Topic Model: Reliable topic models for features and targets.
Embedding Model: Useful for text data that you want to paraphrase. Search
Model: For applications where search input is smaller than the encoded
documents. QA Model: Encodings suitable for question answering." — and
says of them only that "The API provides predefined models for different
applications". Which `featurize()` arguments each preset sets is not
documented on the pages read.

`feature_engine`: "How to encode data ("none", "auto", "pandas", "skrub",
"torch")"; the `featurize()` signature also lists `'dirty_cat'` (skrub's
former name). `embedding`: "If True, produces a random node embedding of
size n_topics default, False. If no node features are provided, will
produce random embeddings (for GNN models, for example)". `similarity` and
`categories` ("decides which category to select in Similarity Encoding,
default 'auto'") name a similarity encoder whose behaviour is not described
on the page. `memoize`: "whether to store and reuse results across runs,
default True."

Not documented on the pages read, and therefore not claimed here: how a
column is classified as numeric, categorical or text beyond the two
thresholds; what happens to boolean and datetime columns (skrub's defaults
are the nearest statement); whether the categorical one-hot uses skrub's
`OneHotEncoder` defaults; how the encoded blocks are concatenated and
whether they are weighted against one another; what the default UMAP
`metric='euclidean'` means over a matrix that mixes one-hot, topic and
transformer columns of very different scale when `use_scaler` is `None`.

## 4 The tree as it stands

**The Projection lane.** `play`'s Projection tab is "A 2-D UMAP scatter of
the result's feature columns" (the help corpus). It does not featurize the
result's columns. It drives the result through the leeway card's
`FeatureExtractor`, which computes sixteen fixed features per entity from
the *shape* of a leeway record: scale (attribute count, value bytes),
shape (Gini of attributes per section, max-to-mean ratio), membership
topology (tags per attribute, tag-count variance, untagged fraction, a
Shannon entropy over the ten membership roles), value structure
(non-scalar fraction and mean cardinality), structural complexity
(effective section count, co-group fraction), two zstd compression ratios
(topology bytes, value bytes), and content (mean value length, repetition
ratio). `LogTransformFeature` flags the unbounded ones for `log1p`;
`PreprocessFeatureMatrix` then z-scores every column and zeroes constant
ones; `RunUMAP` calls the library with `n_neighbors=15`, `min_dist=0.1`,
spectral initialisation up to 2 000 rows and random above (the dense
eigendecomposition is the documented reason); the panel subsamples
uniformly above 10 000 rows and says so. The output is coordinates only,
plotted through implot, coloured by any one of the sixteen features in
eight viridis buckets, a click selecting the row. The parameters are shown
in the toolbar as constants; the timeseries survey records the
counter-precedent that they "live only in panel state — not recorded, not
replayable, not addressable by an agent".

**What the library exposes.** `github.com/nozzle/umap-go`, already in
`go.mod`, does build the graph §2.2 describes and does expose it:
`FuzzySimplicialSet(data, nNeighbors, rng, metric, metricKwds,
localConnectivity, setOpMixRatio)` "constructs the fuzzy simplicial set
from raw data" in the documented five steps ("Compute kNN … Smooth kNN
distances to get sigma, rho … Compute membership strengths → sparse matrix
… Symmetrize via fuzzy set union: W + W^T - W*W^T … Reset local
connectivity") and returns the graph as a sparse CSR beside the per-point
`Sigmas` and `Rhos`; a fitted `UMAP` answers `Graph()`, "the fitted fuzzy
simplicial set graph", and `Transform(XNew)` "projects new data into the
existing embedding space"; `FitTransform(X, y)` takes "optional target
labels for supervised mode" with `TargetWeight` and `TargetMetric` in its
options; `Metric` is a string with `MetricKwds`. So the neighbour graph is
one call away in the lane that already runs UMAP; the lane discards it.
This decides the shape of the first gap in §6: the producer is an adapter
over a dependency the tree carries, not a new algorithm — with the P1
question of whether that dependency stays referenced or gets owned left
where it is.

**The engine.** `csr.BuildE(src, dst []uint64, w []float32, Options)`
takes exactly an edge list with optional weights, collapses parallel edges
by summing, and stores undirected graphs in both rows; every `algo`
function returns slot-aligned columns plus a `Truncation` (ADR-0226 §SD4,
§SD5). ADR-0226 §SD3 lists degree, BFS, components, SCC, PageRank, k-core,
triangles, betweenness and maximal cliques; §SD7 defers Louvain/Leiden
"when a consumer asks for clusters rather than cores" and weighted
shortest paths "when a weighted `edges` contract has a consumer".

**The widget and the panel.** graphview accepts a per-edge `Length` and
`Strength` (ADR-0224 §SD13), a declared `Pinned` position and a widget-side
`PinNode` (§SD10), `SetNodePosition` without a pin, `FastForward(steps)`,
`ForceParams{Dt, Damping, Epsilon, KScale, Theta, PauseOnSettle}`, auras by
id (§SD11) and donuts (§SD9). The play panel maps `group` to an aura id and
a palette position, `weight` on a vertex to its radius, `donut` to the
ring, caps at 2 000 vertices and 6 000 edges, and records two deferrals
that this page will name again: "Pins from the query (`pin_x` / `pin_y`
columns)" and "Per-edge force weight" — the latter written against a
widget that has since grown §SD13, so the remaining half is the panel's
mapping.

## 5 Where each capability lands

| Graphistry | Bucket | Rests on |
| --- | --- | --- |
| UMAP neighbour graph as an edge set (`encode_weight`, `scale`) | **engine gap** — a producer beside the graph engine, output an edge list keyed by id; **covered** on the widget (§SD13) and the panel (`weight`) | §5.1 |
| Automatic features from arbitrary columns (`featurize`, `X=`) | **lane gap**, minus the transformer | §5.1, §5.7 |
| UMAP coordinates as positions, `play=0` | **covered**: `Pinned` (§SD10); a small **lane gap** for `x`/`y` columns, already deferred in the panel record | §5.2 |
| `play=N` — evolve from given positions | **covered**: `SetNodePosition` + `FastForward` | §5.2 |
| DBSCAN over the embedding → `_dbscan` → colour | **covered** widget-side (`group` → aura, palette); **engine gap** for the producer, taken as HDBSCAN | §5.3, §5.8 |
| `transform_umap` — a new batch against a fitted model | **lane gap**, the library's `Transform` exists; low leverage until a consumer has batches | §5.3 |
| Supervised `y` | **covered** by the library; **lane gap** to name the column | §5.3 |
| `embed` / GNN link prediction | **out of scope** (dependency rule) | §2.3 |
| Group-in-a-box | **widget gap**, the static cousin of the sibling analysis's open containers; partition from `group` is **covered** | §5.4 |
| Modularity-weighted layout | **covered** on the widget (`Strength`); **engine gap** for communities (ADR-0226 §SD7); **lane gap** to map `weight` to `Strength` | §5.4 |
| Ring layouts (categorical / continuous / time) | **helper gap** for radius-from-column; small **widget gap** for a radius-only lock and axis rings | §5.4 |
| `locked_x` / `locked_y` / `locked_r` | small **widget gap** (partial pins) | §5.4 |
| `gravity`, `scaling_ratio`, `precision_vs_speed`, `edge_influence` | **covered** by `ForceParams` and `Strength`, not one-to-one | §5.4 |
| `lin_log`, `strong_gravity`, `dissuade_hubs` | **not worth doing** as knobs; the model differs | §7 |
| Histograms as filter, colour, size; data brush | **out of scope** for the widget; in `play` the encoders are contract columns and the filter is the query | §5.5 |
| Timebar | not documented; `play` has a Timeline tab | §5.5 |
| `compute_*` pagerank, betweenness, k-core, components, degrees, BFS | **covered** (ADR-0226 §SD3) | §5.6 |
| One force spectrum (Böhm, Berens & Kobak 2022): the t-SNE kernel with an exaggeration knob | small **widget gap**: a second `ForceModel` on the Barnes–Hut tree | §5.8 |
| PaCMAP: mid-near pairs as a sampled, annealed second edge set | variant of the §5.1 producer; **deferred** with a trigger | §5.8 |
| HDBSCAN over the neighbour graph | **engine gap**, displacing DBSCAN; every step a shape the engine owns | §5.8 |
| Louvain / community | **engine gap**, deferred in ADR-0226 §SD7 | §5.6 |
| Closeness, harmonic, eigenvector, Katz | small **engine gaps**, not deferred anywhere | §5.6 |
| `get_topological_levels`, `hop`, `chain` | **covered**: SQL walks, `nav`, `pushoutgraph` | §5.6 |
| `hypergraph` | **covered** by SQL; a recipe, not a package | §5.6 |
| `collapse` | the sibling analysis's closed grouping (**helper gap**) | §5.6 |

### 5.1 UMAP as a graph: which half is which

Graphistry departs from the Projection lane in two places, and they land
in different buckets.

*The neighbour graph is a first-class edge set.* On the widget side nothing
is missing: an edge with a weight in (0, 1] is what `EdgeSpec.Strength` and
`EdgeSpec.Length` were added for (ADR-0224 §SD13), and the play panel's
`edges` contract already carries `weight` per edge (ADR-0129, 2026-08-05
update). What is missing is the producer — a function from a feature
matrix to an edge list `(source id, target id, weight)`, which is the shape
`csr.BuildE` consumes and the shape the `edges` CTE has. That is an
**engine gap** in ADR-0226's sense — plain values in, struct-of-arrays out,
a budget (`n_neighbors`, a row cap) and a truncation flag, IDL-expressible
per §SD8 — but it is not a *graph* algorithm: its input is a matrix, not a
CSR. It belongs beside the graph engine under `public/analytics`, as
`similarity` already does, not inside it. Two cuts exist and the choice is
a P1 question rather than a design one: an adapter over umap-go's
`FuzzySimplicialSet`, whose output is already a CSR with the weights of
§2.2 (a thin translation to ids, an afternoon); or a first-party exact
*k*-NN plus the smooth-kNN scaling and the fuzzy union, which at the
Projection lane's 10 000-row cap is a brute-force O(n²·d) that fits the
interactive budget without an index. The adapter is the light cut; the
first-party one is what the tree does when a dependency stops being cheap
to trust.

A SQL-side producer is the wrong tool: a *k*-NN join is a self-join with a
window over distance, quadratic in ClickHouse without a vector index, and
the fuzzy scaling is a per-row binary search SQL expresses badly.
ADR-0226's division holds — SQL keeps what it carries well, and this is
not that.

*Features derive automatically from arbitrary columns.* This is a **lane
gap**. The Projection lane featurizes the leeway *card*, not the result's
columns; a result with a `score` and a `country` column contributes nothing
of either to the scatter. Graphistry's `X=` is, in `play`, the query's own
`SELECT` list — the user already chose the columns — so the lane's job is
the routing of §3.1 over Arrow columns: numeric pass-through (with the
lane's existing `log1p`-then-z-score on the skewed ones), boolean to 0/1,
temporal to a number, low-cardinality string to one-hot above a threshold
the query can set, high-cardinality string to something that needs no fit
(§5.7). The transformer path for text does not fit the dependency rule and
is said so plainly in §6; the GapEncoder is a fit without a download and
could be ported, but its value over hashed n-grams for the purpose here —
a neighbour graph, not a classifier — is unmeasured, and porting it ahead
of a measurement is the kind of work ADR-0226's C2 counts against.

Both halves feed the existing consumers unchanged: the edge list goes to
the `edges` lane or to `csr.BuildE`, the coordinates to the Projection
scatter or to the graphview panel as positions (§5.2).

### 5.2 Coordinates as positions, and `play`

`play=0` with `encode_position=True` is a declared `Pinned` position per
node (ADR-0224 §SD10): the force step leaves it, a drag moves it for the
gesture and reports where it was left. **Covered** on the widget. In `play`
the vertices contract has no position columns; ADR-0225's panel record
deferred exactly this ("Pins from the query (`pin_x` / `pin_y` columns) and
holding a dropped node … it is the first thing to add once the panel has
been used"). A **lane gap**, small and already on the list; the UMAP
producer would be its first supplier.

`play=N` — "how much to evolve the network" from the given positions — is
`View.SetNodePosition` for every node (the sibling analysis's reading of
Cytoscape's `preset` layout) followed by the simulation with no pin, and a
`FastForward` budget for the part the reader should not watch; the play
panel already spends a budget of node-steps before first paint (ADR-0225
play panel §SD10). **Covered** by composition; nothing to add.

### 5.3 Clusters, batches, targets

DBSCAN labels reach the picture as `_dbscan` → colour. Here the same column
is `group`, and `group` is both the palette position and the aura id
(ADR-0225 play panel §SD4, ADR-0224 §SD11) — a cluster reads as a tinted
node *and* as a blob, which is more than the reference draws. **Covered**
on the widget and the panel. The producer is an **engine gap**: DBSCAN is
not in ADR-0226 §SD3. It is also cheap given §5.1's output, because DBSCAN
over a radius graph is a components pass — every vertex with at least
`min_samples` neighbours within `eps` is a core point, clusters are the
connected components of the core points under edges shorter than `eps`,
and border points attach to a neighbouring core — which is
`algo`'s union–find over a filtered CSR, with the noise label as the
truncation-shaped flag. ADR-0226 §SD7 defers Louvain/Leiden "when a
consumer asks for clusters rather than cores"; DBSCAN-on-embedding is a
different trigger, not the same one — Louvain clusters a topology and
needs no metric, DBSCAN clusters a metric space and needs no topology
beyond the neighbour graph — though both answer the same question a
consumer asks and both land in the same column. A consumer asking for
"clusters" over a result with no edges is asking for DBSCAN; over a result
with edges, for Louvain.

`transform_umap` is `UMAP.Transform(XNew)` in the library, and its
`merge_policy` — new batch edges into the existing graph's neighbours — is
the bipartite membership function the library also exposes
(`ComputeMembershipStrengthsBipartite`). A **lane gap** with no consumer in
the tree: `play` re-runs a query and re-fits, and a fitted model kept
across results is the replayable state the timeseries survey says the
Projection panel lacks. Deferred with that trigger.

Supervised UMAP is `FitTransform(X, y)` with `TargetWeight` — **covered**
by the library; naming the target column is part of the featurization lane
gap.

### 5.4 Layouts that consume analytics

**Group-in-a-box** is a partition, a treemap of the partition's sizes over
the canvas, and a layout per cell. The partition is `group`, **covered** on
the contract. The treemap is in the tree
([the treemap widget](../../public/thestack/imzero2/egui2/widgets/treemap/),
[ADR-0166](../adr/0166-play-treemap-panel.md)). The per-cell layout is the
piece graphview lacks: one `Options.Layout` for the whole declaration, and
no notion of a subset laid out within a rectangle. This is the static,
fixed-cell cousin of the sibling analysis's open containers (its §4,
item 2: "Layout in two levels: members laid out within the container"), and
strictly easier — the cells do not move, so the container does not
participate in an outer layout, does not need picking or edge termination.
A **widget gap**: a `LayoutGroupInABox` taking a group id per node, cells
from a squarified treemap of group sizes, and the force step run per cell
with each cell's centre as its gravity, plus a cell outline as paint. At
this panel's 2 000-vertex cap the per-cell steps are the same work as one
step.

**Modularity-weighted** is a community per node, a weight of 2.0 or 0.3 per
edge by whether its ends share one, and the force layout with those weights
raised to prominence. Every piece but the community is **covered**:
`EdgeSpec.Strength` is the weight, and "raising its prominence" is the
caller choosing the ratio rather than a global `edge_influence`. The
community is the §SD7 **engine gap**. Mapping a contract `weight` on an
edge to `Strength` in the play panel — the deferral its record carries — is
the **lane gap** that makes both this and §5.1 reach the picture, and it is
a few lines.

**Rings** are a radius per node from a column, with the angle left to the
force layout. Two halves. Radius-from-column — categorical order,
continuous range, time buckets, `min_r`/`max_r`, the axis labels — is a
function of the universe alone, so it is a **helper gap** in the sibling
analysis's sense (its §3.6: "computable by the caller before `AddNodes`"),
and small. The other half is what `locked_r` does: a node held on its ring
and free along it, which no constraint in graphview expresses — `Pinned`
fixes both coordinates. A **widget gap**, small: a per-node lock on radius
or on one axis, applied by projecting the position back after each force
step, which is also `locked_x` / `locked_y`; concentric axis rings with
labels are paint that belongs with it. ADR-0225's radial layout is the
*hop-distance* ring — the same picture with the topology as the column —
and is static rather than force-relaxed; a column-driven ring is a
different layout, not a parameter of that one.

**The FA2 knobs.** `gravity` is the centre-gravity variant of §SD2;
`scaling_ratio` is `KScale` in effect if not in formula; `precision_vs_speed`
is Barnes–Hut's `Theta`; `edge_influence` is folded into how the caller
computes `Strength`. **Covered**, none exactly. `lin_log`, `strong_gravity`
and `dissuade_hubs` are ForceAtlas2's alternative force model — a
logarithmic attraction, a gravity that does not fall off, and a repulsion
scaled by degree — and are not knobs on a Fruchterman–Reingold step. §7
takes them; the one with a use is `dissuade_hubs`, and it is the same shape
as the per-node mass the sibling analysis already ranks (its §3.8).

### 5.5 The viewer's reading of a column

Histograms-as-filters, colour and size encoders, and the data brush are the
hosted viewer's way of reaching a column after the fact. graphview has no
such surface and should not: the caller declares colour and radius per node
(ADR-0224 §SD1), and a histogram over a declared attribute is a widget of
its own. **Out of scope** for the widget.

In `play` the equivalents exist in a different shape. The encoders *are*
the contract: `tone`, `group` and `weight` on the vertices and edges,
written in SQL ([ADR-0129 §SD2](../adr/0129-play-layered-graph-panel.md),
[ADR-0167](../adr/0167-layeredgraph-magnitude.md)). A histogram is a `GROUP
BY` drawn in the Chart tab ([ADR-0172](../adr/0172-play-chart-panel.md)),
and a range filter is a `WHERE`, so the click-a-bin loop is a query edit.
What crosses panels is the selection — the graphview panel publishes
`selection_key` (ADR-0225 play panel §SD6), the Projection panel emits the
row selection — over the reactive graph of
[ADR-0097](../adr/0097-play-reactive-query-graph.md); whether a brush over
the scatter should become a filter elsewhere is a `play` question this page
does not design. The timebar is not documented on the pages read; `play`'s
Timeline tab ([ADR-0043](../adr/0043-imzero2-timeline-widget.md)) is the
nearest thing and is not compared further.

### 5.6 The compute surface

Against ADR-0226 §SD3: PageRank, betweenness, k-core, components ("clusters"
in igraph's naming), degrees and BFS are **covered**, with budgets and
truncation the reference does not document. `get_topological_levels` is a
SQL-side walk and `pushoutgraph`'s Kahn sort; `hop` and `chain` are `nav`'s
bounded walk and the applet books' recursive CTEs; both **covered** in the
sense ADR-0226 uses — SQL keeps the bounded walks. `hypergraph` is an
unpivot: a node per distinct value per entity column, a node per row unless
`direct`, edges from cells to rows — a `SELECT … ARRAY JOIN` into the
`edges` / `vertices` contract, **covered** by SQL and worth a book entry
rather than a package. `collapse` — "collapses clusters of nodes that share
the same property so that topology is preserved" — is the closed grouping
of the sibling analysis (its §4), a **helper gap** ranked there.

Not covered: Louvain and every community method (the §SD7 deferral, with
DBSCAN's separate trigger in §5.3); closeness, harmonic, eigenvector and
Katz centrality — the first two are BFS from every vertex, budgeted like
betweenness's pivots, the last two are pull sweeps of PageRank's shape —
small **engine gaps** with no deferral entry, and no consumer asking;
weighted shortest paths (deferred in §SD7); link prediction and the GNN
route (**out of scope**, §2.3).

### 5.7 Sovereignty: features without a model

Graphistry's default text path downloads a sentence-transformer model, and
its categorical path fits a GapEncoder through skrub. Neither crosses the
tree's dependency rule as a *reference* — why-boxer P1 lets a dependency be
"referenced while it stays cheap to trust" — but a model fetched at first
use from a public hub is not something an airgapped build can carry, and
ADR-0226's C3 is the criterion that already ruled on that shape.

What a boxer-side featurizer can rest on without a model: numeric
pass-through with the lane's `log1p` and z-score; one-hot below a
cardinality threshold; above it, hashed character n-grams or a TF-IDF over
word n-grams — the `use_ngrams=True` path Graphistry itself offers, a fit
over the data at hand with no download; a datetime to a number. One more
is a thought rather than a proposal: the tree carries a compression-based
similarity — normalised compression distance in
[`public/analytics/similarity/compression`](../../public/analytics/similarity/compression/)
— a text distance needing no model and no vocabulary, and the leeway card's
feature extractor already spends two of its sixteen features on zstd
ratios. A *k*-NN graph over a text column could take NCD as its metric
directly, skipping the vector space; the cost is a compression per pair,
quadratic, which at a few thousand rows is minutes rather than seconds and
would want the Projection lane's row cap. An option for the metric, not a
decision; a measurement against hashed n-grams would decide it.

### 5.8 The literature beside the product: one force spectrum, PaCMAP, HDBSCAN

Three results outside Graphistry's pages bear on how §5.1 and §5.3 should be
cut, and are recorded here so the design note the first gap wants does not
re-derive them. Added 2026-09-12 with the rest of the page; each rests on
the paper or documentation page named.

**Neighbour embeddings are one force layout with one knob.** Böhm, Berens &
Kobak (JMLR 2022) show that t-SNE, UMAP, ForceAtlas2 and Laplacian
eigenmaps sit on a single *attraction–repulsion spectrum*: "these algorithms
combine an attractive force between neighboring pairs of points with a
repulsive force between all points", and "changing the balance between the
attractive and the repulsive forces in t-SNE using the exaggeration
parameter yields a spectrum of embeddings". The trade-off is stated in one
sentence: "stronger attraction can better represent continuous manifold
structures, while stronger repulsion can better represent discrete cluster
structures and yields higher *k*NN recall." On that axis, "UMAP embeddings
correspond to t-SNE with increased attraction" (exaggeration ρ ≈ 4 in their
figures), and the reason is the optimiser, not the objective: "the negative
sampling optimisation strategy employed by UMAP strongly lowers the
effective repulsion" — they put the effective repulsion weight at roughly
`k·m/n` for *k* neighbours, *m* negative samples and *n* points, so it
shrinks as the dataset grows. ForceAtlas2 "yields embeddings corresponding
to t-SNE with the attraction increased even more" (ρ ≈ 30), and "at the
extreme of this spectrum lie Laplacian Eigenmaps" (ρ → ∞). They also read
early exaggeration as a schedule rather than a trick, and suggest "gradual
annealing of the exaggeration factor ρ from 'infinity' down to its final
desired value" as an optimiser in its own right.

Read against the tree, this collapses two things into one. The Projection
lane's UMAP and graphview's force layout are the same computation on the
same object — a *k*-NN graph with membership weights, laid out by attraction
along its edges and repulsion between all points — differing in the kernel
and in one scalar. The consequences, each a reading rather than a decision:

- **The neighbour graph is the durable object; the picture is a parameter
  of it.** §5.1's producer is the thing to build; the embedding is whatever
  force step is pointed at its output. `n_neighbors` is a graph knob and
  `min_dist` a layout knob, and the paper says the layout knob that matters
  is ρ.
- **graphview is most of the way there, with the wrong kernel.** ADR-0224
  §SD6's step is Fruchterman–Reingold — attraction `d²/k`, repulsion `k²/d`,
  Barnes–Hut above a threshold — and §SD13's `Strength` already puts a
  per-edge weight on the attraction. What the spectrum wants is the t-SNE
  kernel: attraction `w_ij · q_ij` along neighbour edges, repulsion `q_ij²`
  over all pairs with `q_ij = 1/(1 + d²)`, and ρ scaling one against the
  other. That is a second `ForceModel` on the same Barnes–Hut tree — the
  tree already sums a kernel over all pairs; only the kernel changes — and
  a **widget gap**, small, ranked in §6. With it, one slider moves the same
  declaration from a ForceAtlas2-like continuous picture to a t-SNE-like
  clustered one, and the projection becomes an interactive graph — drag,
  pin, auras, selection — rather than a scatter beside one.
- **Deterministic by construction.** Barnes–Hut computes the repulsive sum
  exactly rather than by negative sampling, so the effective repulsion is
  the declared ρ, not `k·m/n`, and the result is the bit-identical
  parallel result ADR-0224 §SD6 and ADR-0226 §SD2 require. umap-go's SGD
  with negative sampling is neither.
- **Annealing replaces the eigensolver.** The Projection lane caps its
  spectral initialisation at 2 000 rows because the dense Laplacian
  eigendecomposition does not scale. The paper's reading — Laplacian
  eigenmaps is the ρ → ∞ end of the same optimiser — means a schedule that
  starts with ρ large and lowers it reaches the same neighbourhood without
  an eigensolver, on any row count the force step can hold. `FastForward`
  is the budget that schedule runs under.
- **ForceAtlas2's lin-log and hub dissuasion** are, in this frame, points
  on the same spectrum rather than knobs to add — the paper's FA2 has
  attraction proportional to distance and a degree-scaled repulsion
  `(h_i+1)(h_j+1)/d²`; §7's ruling stands, with a better reason.

**PaCMAP: global structure from a second, sampled edge set.** Wang, Huang,
Rudin & Shaposhnik (JMLR 2021) start from the same tension — "these methods
can either handle one or the other, but not both" of local and global
structure — and derive that "in order to preserve global structure, we must
have forces on non-neighbors." PaCMAP optimises "three kinds of pairs of
points: neighbor pairs, mid-near pairs, and further pairs": neighbours from
the *k*-NN graph under a locally scaled distance; mid-near pairs, where for
each point one samples six others and keeps "the second closest of the 6";
and random further pairs, with a three-phase weight schedule in which the
mid-near weight starts near 1 000, drops to 3, then to 0, so the method
"dynamically uses a special group of pairs — mid-near pairs, to first
capture global structure and then refine local structure." The kernels are
saturating (`d̃/(10+d̃)` for neighbours, `d̃/(10000+d̃)` for mid-near,
`1/(1+d̃)` repulsion), initialisation is PCA, and one of their findings is
that TriMap's global structure "comes from an unexpected source, namely its
initialization." Against the tree: mid-near pairs are edges too — a
sampled, weak, annealed second edge set — and graphview's declaration can
carry them as edges with a `Strength` the caller schedules, or the second
force model can take them as a term. It needs the same *k*-NN graph as §5.1
plus a seeded sample, so it is a variant of the producer, not a second one;
its PCA initialisation is a small dense computation the tree does not have
and would want beside the producer. Deferred with a trigger in §6: a
consumer that reads *where a cluster sits relative to the others*, which is
the question the Projection panel's scatter cannot currently answer well.

**HDBSCAN: the clustering that needs no ε.** The hdbscan documentation's
account is a graph algorithm end to end: a core distance per point, "the
distance to the *k*th nearest neighbor"; the mutual reachability distance
`max{core_k(a), core_k(b), d(a,b)}`, which spreads sparse points apart and
leaves dense regions alone; "the minimum spanning tree of the graph" under
that distance; the tree converted into "a hierarchy of connected
components" by adding edges in increasing order; the hierarchy condensed
with `min_cluster_size` so that small splits read as "points falling out of
a cluster"; clusters chosen by stability, the sum of `(λ_p − λ_birth)` over
their points; and everything else labelled noise. There is "no epsilon".
That last point is why it should displace DBSCAN in §6: an embedding's
scale is arbitrary, so ε is a parameter a user cannot set well, whereas
`min_cluster_size` is a statement about the data. And every step is a
shape ADR-0226 already owns — the core distance is the *k*th neighbour
weight the producer computes anyway; the MST over the *k*-NN edges is
Kruskal in weight order over the CSR; the hierarchy is union–find with a
merge height; the condensed tree and stability extraction are a walk over
that hierarchy. Over the *k*-NN graph rather than the complete graph, the
MST disconnects only what the *k*-NN graph disconnects — this page's
reading, labelled as such, and the reason the row cap of §5.1 is also this
algorithm's. Its home is the engine, beside components, taking the
producer's CSR.

## 6 The gaps, ordered by what they let a consumer build

Effort is a rough estimate in days, including tests. The first item wants
a short design note before code, because it decides whether umap-go's
graph is referenced or owned.

1. **Engine gap: a *k*-NN + fuzzy-membership neighbour-graph producer** —
   a function from a feature matrix (rows keyed by vertex id) to an edge
   list `(source, target, weight)` with `n_neighbors`, a metric, a row cap
   and a truncation flag, beside the graph engine under `public/analytics`.
   Turns any table into a graph the widget, the panel and `csr.BuildE`
   accept as they are (§5.1). **1 day** as an adapter over umap-go's
   `FuzzySimplicialSet`; **3–4 days** first-party (exact *k*-NN, smooth-kNN
   scaling, fuzzy union), which is the cut if the dependency is to be owned.
2. **Lane gap: automatic featurization of a result's columns** — numeric,
   boolean, temporal, low-cardinality one-hot, high-cardinality hashed
   n-grams, with the target column named, feeding both the Projection
   scatter and item 1; the leeway-card features stay as one more block.
   What makes item 1 reachable from a query rather than from Go (§5.1,
   §5.7). **3–4 days.** Text through a transformer is *not* in this item:
   it is the one part of Graphistry's pipeline that does not fit the
   dependency rule, and a result whose meaning lives in free text will
   project worse here than there.
3. **Lane gap: `weight` on an edge into `EdgeSpec.Strength`** in the play
   graphview panel — the deferral its own record carries, and the line
   that lets items 1 and 5 change the picture rather than only the stroke
   (§5.4). **0.5 days.**
4. **Engine gap: HDBSCAN over the neighbour graph** — core distances from
   the producer's weights, Kruskal over the CSR under mutual reachability,
   union–find with merge heights, the condensed tree and stability
   extraction, noise as a label — the partition that becomes `group` and
   therefore both a tint and an aura (§5.3, §5.8). It takes
   `min_cluster_size`, not ε, which is why it displaces the DBSCAN cut
   Graphistry documents. **2 days** on top of item 1; the ε-DBSCAN of
   §5.3 is a half-day fallback if the hierarchy is not wanted first.
5. **Engine gap: Louvain or Leiden** — the ADR-0226 §SD7 deferral, with
   its trigger restated: a consumer asking for clusters over a graph that
   has edges. Modularity-weighted layout and group-in-a-box both want it
   (§5.4). **3–4 days**, and a dated update to ADR-0226.
6. **Lane gap: `x` / `y` (or `pin_x` / `pin_y`) columns on the vertices
   contract** — declared pins from the query, the deferral in the play
   panel record; item 1's coordinates are the first supplier (§5.2).
   **1 day.**
7. **Widget gap: partial pins** — a per-node lock on radius or on one
   axis, applied after the force step, with concentric axis rings as paint;
   what `locked_r` / `locked_x` / `locked_y` do, and what a column-driven
   ring layout needs (§5.4). **1.5–2 days.**
8. **Helper gap: radius from a column** — categorical order, continuous
   range, time buckets, into positions on rings, ahead of `AddNodes` or as
   a `nav` stage once the sibling analysis's pipeline exists (§5.4).
   **1 day.**
9. **Widget gap: group-in-a-box** — a partition column, a squarified
   treemap of group sizes, the force step per cell with the cell centre as
   gravity, cell outlines as paint; the static cousin of the sibling
   analysis's open containers and separable from it (§5.4). **3–4 days.**
10. **Engine gaps: closeness, harmonic, eigenvector, Katz** — two BFS-all
    sources with a pivot budget, two pull sweeps; nothing asks for them yet
    (§5.6). **1.5–2 days** together.
11. **Widget gap: a neighbour-embedding force model** — the t-SNE kernel
    with an exaggeration knob ρ on the existing Barnes–Hut tree, and an
    annealing schedule under `FastForward`; makes items 1 and 6 draw as an
    interactive graph rather than a scatter, and retires the Projection
    lane's spectral-init cap (§5.8). **2–3 days.** Ranked last only because
    the widget already draws item 1's output with the FR kernel; it is the
    item that turns the projection into the widget.

Deferred, with the trigger rather than an estimate:

- **`transform_umap`** — a fitted model kept across results, new rows
  placed against it and joined to the old neighbours (§5.3). Trigger: a
  consumer with batches, which is the recorded-and-replayable projection
  state the timeseries survey names.
- **A GapEncoder port** for high-cardinality strings (§5.1). Trigger: a
  measurement showing hashed n-grams lose a neighbour structure the topic
  model keeps, on a result the tree cares about.
- **NCD as the *k*-NN metric** for text columns (§5.7). Trigger: the same
  measurement, run the other way.
- **PaCMAP's mid-near pairs** as a second, annealed edge set from the same
  producer (§5.8). Trigger: a consumer that reads where a cluster sits
  relative to the others, which the scatter answers poorly.

## 7 Not worth doing, with the reason

- **A transformer text encoder** (§3.2) — a model download at first use
  and a torch runtime; the dependency rule and ADR-0226 C3 have ruled on
  the shape. Hashed n-grams are the substitute, and the loss is stated
  rather than hidden.
- **`lin_log`, `strong_gravity`, `dissuade_hubs` as knobs** (§5.4) — they
  are ForceAtlas2's force model, not parameters of a Fruchterman–Reingold
  step; adding them means a second model. The one effect a consumer would
  notice, hub dissuasion, is the per-node mass gap already ranked in the
  sibling analysis.
- **The GNN / knowledge-graph embedding route** (§2.3) — torch and DGL, a
  training loop, and a question (link prediction) the tree has not asked.
- **A `hypergraph` package** (§5.6) — an unpivot the applet books express
  in one query against the existing contract.
- **Histograms, brush and encoders inside graphview** (§5.5) — the caller
  declares colour and radius; a histogram is another widget, and in `play`
  the encoders are already the contract's columns.
- **A SQL-side *k*-NN** (§5.1) — a quadratic self-join and a per-row
  binary search, both of which SQL expresses badly and the engine expresses
  in a loop.
- **The five featurization presets** (§3.6) — their contents are not
  documented, so there is nothing to port; the parameters they presumably
  set are the ones item 2 takes directly.
- **`memoize`, `inplace`, `engine='cuml'`** — Python and GPU plumbing.

## 8 References

Tree:

- [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md) — the
  widget: §SD1 declaration, §SD2 layouts and force parameters, §SD6
  Barnes–Hut, §SD9 donuts, §SD10 pins, §SD11 auras, §SD13 edge id, length
  and strength.
- [ADR-0225 (nav)](../adr/0225-graphview-navigation-layer-and-radial-layout.md)
  — the navigation layer and the hop-distance radial layout (§SD6).
- [ADR-0225 (play panel)](../adr/0225-play-graphview-panel.md) — `group`
  as aura id (§SD4), `weight` and `donut` (§SD5), `selection_key` (§SD6),
  caps (§SD9), the step budget (§SD10), and the two deferrals (pins from
  the query, per-edge force weight).
- [ADR-0226](../adr/0226-graph-analytics-engine.md) — the engine: §SD1 CSR,
  §SD3 algorithms, §SD4 budgets, §SD5 columns, §SD7 deferrals, §SD8
  IDL-expressible surface; C3 sovereignty.
- [ADR-0129 §SD2](../adr/0129-play-layered-graph-panel.md) — the `edges` /
  `vertices` contract and its `weight` update.
- [ADR-0172](../adr/0172-play-chart-panel.md), [ADR-0097](../adr/0097-play-reactive-query-graph.md),
  [ADR-0043](../adr/0043-imzero2-timeline-widget.md), [ADR-0166](../adr/0166-play-treemap-panel.md),
  [ADR-0167](../adr/0167-layeredgraph-magnitude.md) — the `play` panels and
  channels §5.5 names.
- [graph analytics engine survey](./graph-analytics-engine-survey.md) — the
  options and criteria behind ADR-0226.
- [play timeseries analysis survey](./play-timeseries-analysis-survey.md) —
  the Projection panel as counter-precedent.
- [Cytoscape.js and Ogma analysis](./graph-viewer-gap-analysis-cytoscape-ogma.md)
  — the sibling analysis whose buckets and open-container list this page
  reuses.
- [why-boxer P1](../explanation/why-boxer.md) — the dependency rule.
- [`apps/play/play_projection.go`](../../apps/play/play_projection.go),
  [`apps/play/play_projection_panel.go`](../../apps/play/play_projection_panel.go),
  [`leeway_card_features.go`](../../public/semistructured/leeway/card/leeway_card_features.go),
  [`feature_projection.go`](../../public/semistructured/leeway/card/feature_projection.go)
  — the Projection lane; `github.com/nozzle/umap-go` read through `go doc`.
- [`public/analytics/graph`](../../public/analytics/graph/) `csr`, `engine`,
  `algo` package docs; [`public/analytics/similarity/compression`](../../public/analytics/similarity/compression/).

Graphistry, read 2026-09-12 (documentation pages only; see the provenance
note):

- `https://hub.graphistry.com/docs/graph-algorithms/dimensionality/` —
  UMAP integration, the `umap()` example.
- `https://hub.graphistry.com/docs/graph-algorithms/automated/` —
  automated feature engineering, the five presets.
- `https://hub.graphistry.com/docs/graph-algorithms/overview/`,
  `…/detection/`, `…/centrality/`, `…/pathfinding/`, `…/layout/`,
  `…/network/`, `…/cugraphex/`, `…/igraph/` — the compute surface and the
  "GPU-accelerated FA2" statement.
- `https://hub.graphistry.com/docs/api/1/rest/url/` — the URL options
  table (`play`, `lockedX/Y/R`, `linLog`, `strongGravity`, `dissuadeHubs`,
  `edgeInfluence`, `precisionVsSpeed`, `gravity`, `scalingRatio`).
- `https://hub.graphistry.com/docs/ui/index/`, `…/ui/histograms/`,
  `…/ui/basics/`, `…/ui/tips/` — histograms, filters, data brush.
- `https://hub.graphistry.com/static/js-docs/jsdocs/global.html` — the
  `toggleTimebars` entry.
- `https://pygraphistry.readthedocs.io/en/latest/api/ai.html` — the
  rendered API reference for `featurize()`, `umap()`, `transform_umap()`,
  `filter_weighted_edges()`, `prune_weighted_edges_df_and_relabel_nodes()`,
  `dbscan()`, `transform_dbscan()`, `scale()`, `get_matrix()`, `embed()`,
  `search()`.
- `https://pygraphistry.readthedocs.io/en/latest/api/plotter.html` — the
  `bind()` (`point_x`, `point_y`, `edge_weight`) and `layout_settings()`
  and `hypergraph()` docstrings.
- `https://pygraphistry.readthedocs.io/en/latest/graphistry.compute.html`
  — `get_degrees()`, `get_topological_levels()`, `collapse()`.
- `https://pygraphistry.readthedocs.io/en/latest/api/plugins/compute/igraph.html`,
  `…/cugraph.html` — `compute_igraph()`, `compute_cugraph()`.
- `https://pygraphistry.readthedocs.io/en/latest/api/index.html`,
  `…/api/layout/index.html`, `…/api/layout/ring.html`,
  `…/api/layout/gib.html`, `…/api/layout/modularity_weighted.html` — the
  layout signatures.
- `https://pygraphistry.readthedocs.io/en/latest/visualization/layout/catalog.html`,
  `…/visualization/layout/intro.html`, `…/visualization/layout/settings.html`
  — the layout catalogue.
- `https://pygraphistry.readthedocs.io/en/latest/10min.html`,
  `…/visualization/10min.html`, `…/cheatsheet.html`, `…/gfql/combo.html`,
  `…/demos/for_analysis.html` — guides; the "similarity edges" sentences
  and the `transform_umap` shape.
- `https://pygraphistry.readthedocs.io/en/latest/notebooks/index.html`,
  `…/notebooks/ai.html` — the rendered notebook index.
- `https://pygraphistry.readthedocs.io/en/latest/demos/ai/cyber/CyberSecurity-Slim.html`
  — the botnet notebook (`kind='edges'`, supervised `y`).
- `https://pygraphistry.readthedocs.io/en/latest/demos/demos_databases_apis/gpu_rapids/part_iv_gpu_cuml.html`
  — the GPU UMAP notebook.
- `https://pygraphistry.readthedocs.io/en/latest/demos/more_examples/graphistry_features/edge-weights.html`
  — the edge-weights notebook.
- `https://pygraphistry.readthedocs.io/en/latest/demos/talks/infosec_jupyterthon2022/rgcn_login_anomaly_detection/intro-story.html`,
  `…/advanced-identity-protection-40m.html` — the RGCN talk pages.
- `https://www.graphistry.com/blog/gpu-group-in-a-box-layout-for-larger-social-media-investigations`
  — the group-in-a-box description.

The "cyber redteam UMAP demo" the API reference mentions by name is not
rendered on the documentation site at the compile date (the candidate page
returns 404), so its pipeline is not quoted here.

Third-party documentation the Graphistry pages delegate to, read the same
day:

- `https://umap-learn.readthedocs.io/en/latest/parameters.html`,
  `…/how_umap_works.html` — the neighbour graph construction and the
  `n_neighbors` / `min_dist` semantics.
- McInnes, Healy & Melville, *UMAP: Uniform Manifold Approximation and
  Projection for Dimension Reduction*, arXiv:1802.03426 (2018).
- Jacomy, Venturini, Heymann & Bastian, *ForceAtlas2, a Continuous Graph
  Layout Algorithm for Handy Network Visualization Designed for the Gephi
  Software*, PLoS ONE 9(6), 2014 — the model behind the FA2 knobs.
- `https://skrub-data.org/stable/reference/generated/skrub.TableVectorizer.html`,
  `…/skrub.GapEncoder.html`.
- `https://www.sbert.net/` — Sentence Transformers.
- `https://scikit-learn.org/stable/modules/generated/sklearn.preprocessing.MultiLabelBinarizer.html`,
  `…/sklearn.preprocessing.QuantileTransformer.html`,
  `…/sklearn.preprocessing.KBinsDiscretizer.html`,
  `…/sklearn.preprocessing.RobustScaler.html`.
- Ester, Kriegel, Sander & Xu, *A Density-Based Algorithm for Discovering
  Clusters in Large Spatial Databases with Noise*, KDD 1996 — DBSCAN, for
  the components reading in §5.3.

Read for §5.8, the same day:

- Böhm, Berens & Kobak, *Attraction-Repulsion Spectrum in Neighbor
  Embeddings*, JMLR 23(95), 2022; arXiv:2007.08902 — the spectrum, the
  exaggeration correspondences, the negative-sampling argument, the
  annealing suggestion.
- Wang, Huang, Rudin & Shaposhnik, *Understanding How Dimension Reduction
  Tools Work: An Empirical Approach to Deciphering t-SNE, UMAP, TriMAP, and
  PaCMAP for Data Visualization*, JMLR 22(201), 2021; arXiv:2012.04456 —
  the three pair kinds, the schedule, the initialisation finding;
  `https://pypi.org/project/pacmap/` for the parameters.
- `https://hdbscan.readthedocs.io/en/latest/how_hdbscan_works.html` — the
  algorithm account quoted; Campello, Moulavi & Sander, *Density-Based
  Clustering Based on Hierarchical Density Estimates*, PAKDD 2013, and
  McInnes & Healy, *Accelerated Hierarchical Density Based Clustering*,
  ICDM Workshops 2017, for the method.
- Cerda, Varoquaux & Kégl, *Similarity encoding for learning with dirty
  categorical variables*, Machine Learning 107, 2018; arXiv:1806.00979, and
  Cerda & Varoquaux, *Encoding high-cardinality string categorical
  variables*, IEEE TKDE 2020; arXiv:1907.01860 — the work behind skrub's
  encoders named in §3.1;
  `https://skrub-data.org/stable/reference/generated/skrub.StringEncoder.html`
  for skrub's current high-cardinality default.
