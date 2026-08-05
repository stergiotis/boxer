---
type: adr
status: proposed
date: 2026-08-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0164: documentation regex search — pattern batteries over the doc corpora

## Context

The runtime carries several markdown corpora and none of them is searchable:
the help books ([help.LibraryI], 16 docs / ~112 KB across four apps today),
play's snippet library (one 42 KB doc rendered with Insert/Replace buttons),
the ADR corpus (already exposed whole-doc via the `adrcontent` introspection
table, ADR-0122 §SD4), sqlapplet definitions, and — server-side —
`system.documentation` (ClickHouse 26.x), which play's Docs pane already
queries by exact name. Finding anything means scrolling.

The corpora are heterogeneous in *where they live* (embedded `fs.FS`, repo
working tree, the ClickHouse server) and in *how much* of them exists at a
given host: an M1 host has no ClickHouse at all, so any search that only
works through the facts plane abandons the help reader exactly where it is
most needed.

A design dialogue settled a ladder of search tiers, cheapest first: a lexical
floor, then LLM-generated *pattern batteries* executed by regex engines, with
embedding-based retrieval deferred behind an evaluation gate. This ADR covers
the first two rungs — the deterministic backbone and its direct user exposure
— and freezes the seam the LLM rung plugs into. Battery *generation*
(text2regex) is a sub-capability of text2dsl (ADR-0139) and is specified here
only as a contract.

## Decision

### §SD1 — the section is the unit of search

Search results are sections, not documents: a hit is `(book, doc, heading
slug)` plus display metadata, i.e. exactly a [help.RefT]. The embedded tier
reuses the existing ref/navigation plumbing (`HelpHost.OpenRef` →
`markdown.WithScrollToSection`) unchanged. Section boundaries come from
heading byte offsets recorded at parse time (`markdown.HeadingInfo.ByteOffset`,
mirrored on `help.SectionInfo`); the region before the first heading is the
doc-level section with slug `""`. Frontmatter is excluded from the scanned
body (its fields are indexed as metadata, not text).

Known wart, accepted: heading slugs are not unique within a doc, and
`WithScrollToSection` lands on the first occurrence. Search inherits that;
fixing slug uniqueness is a markdown-package concern, not a search one.

### §SD2 — the pattern battery is the query model

A query compiles to a **battery**: an ordered set of case-insensitive RE2
patterns plus a mode (`all` for user-typed queries — every pattern must hit
the section; `any` for generated batteries — hit count ranks). One battery
feeds every executor. A token that fails to compile as a regex degrades to a
quoted literal (`regexp.QuoteMeta`) instead of erroring — a half-typed
`quantile(` must keep matching as text, and the degradation is surfaced, not
silent (the battery records which patterns are literal).

Batteries stay inside the RE2 ∩ hyperscan common subset by construction: RE2
rejects backreferences and lookaround at compile time, which is the same
subset ClickHouse's `multiMatch*` (Vectorscan) family accepts. A battery that
compiles in-process is therefore valid for the facts-plane executor without
translation.

### §SD3 — embedded executor: scan, don't index

`help/search` builds a per-library section table (source slices per §SD1) and
answers a battery by scanning it with the compiled patterns. There is no
inverted index, no persistence: the embedded corpus is ~112 KB and RE2 scans
are linear, so a full sweep per *query change* (not per frame) costs well
under a millisecond. Scoring is a deterministic field-weight sum per matched
pattern — doc title ×8, section heading ×4, section body ×1 — ordered by
score, then book/doc/section position. Each hit carries the first matching
body line as a context snippet. If the corpus ever outgrows the scan, the
remedy is the facts-plane lane (§SD5), not an in-process index.

### §SD4 — direct exposure, embedded tier

**HelpHost**: a search field above the nav. A non-empty query flattens the
nav into a ranked cross-book hit list (title, heading, context line); a click
`OpenRef`s the hit and keeps the query. Selecting a doc without a section
scrolls to the top once per doc change (the Docs pane's `scrolled` guard
pattern) so a search jump never inherits the previous doc's scroll offset.

**Snippets tab**: a filter box. Matching sections — expanded to include their
descendant subsections — render; everything else is skipped via a new
`markdown.WithSectionFilter(accept func(slug string) bool)` RenderOpt, with
the doc-level `""` section hidden while a filter is active. Insert/Replace
buttons work unchanged on the filtered view.

Filtered rendering shifts the markdown widget's seq-derived ids (the
documented id-derivation invariant), so the filtered render is wrapped in an
IdScope keyed by the query, deliberately abandoning per-widget egui state on
query change. `WithSectionFilter` and `WithScrollToSection` must not be
combined: skipping headings desynchronises the scroll dispatch's heading
ordinals.

Both boxes are one input widget — `regexedit`, the pattern editor extracted
from regex_explorer (ADR-0015 lexer, ADR-0130 highlight seam), in a token
mode that lexes each whitespace-separated pattern independently so an
unclosed group in one token cannot mis-colour the next. And both pair the
battery with a **selectivity meter**: a byte-share progress bar of how much
of the corpus the battery selects, section counts in an adjacent label
(never on the bar — its own text is illegible at low fractions). The meter
is computed from the uncapped hit set; only the rendered list truncates.

### §SD5 — facts-plane lane: the same battery in SQL

The large corpora get one section-grained surface: `helpsections` (and
`adrsections`) introspection providers following the `adrcontent` precedent,
UNION-viewable with `system.documentation`. A `docsearch('<query>')` nanopass
macro expands the battery to `multiMatchAllIndices` scoring with the same
field weights as §SD3, the pattern array spliced as a typed literal
(nanopass-marshalling). Play users reach it directly in the editor — the
macro *is* the user exposure on this tier — and hits carry a string ref
(`help://app/doc#slug`, `adr://0164#sd5`, `chdoc://quantileTDigest`) whose
format this ADR freezes so rows written early stay navigable later.

The live-provider lane has no predicate pushdown (ADR-0094): acceptable for
the bounded corpora named here, and the recorded boundary — an unbounded
corpus (git log) requires the materialized lane and is deferred (§SD7).

### §SD6 — text2regex: a battery generator under text2dsl

Battery generation from natural language ("deduplicate rows" →
`(?i)dedup|(?i)argMax|(?i)ReplacingMergeTree|…`) is a **sub-capability of
text2dsl (ADR-0139)**: the generation target is a `docsearch(...)` call
carrying an `any`-mode battery, produced and validated like any other
canonical-dialect emission. This ADR contributes only the frozen interface —
the battery type, its compile-degradation rules, and the requirement that
generated batteries pass RE2 compilation before reaching an executor. Model
endpoints, caching of generated batteries, and prompt shape are ADR-0139
concerns. Until that lands, the user's literal tokens are the only battery
source — which is precisely the designed fallback, not a degraded mode.

### §SD7 — deferrals, each with a trigger

- **Commit-message corpus** — needs the materialized-ingest lane
  (covered-through-hash incremental, changelog precedent). Trigger: a user
  actually reaching for `git log --grep` mid-session.
- **Embeddings** — vector + model columns on the sections surface. Trigger:
  a golden-query-set evaluation (30–50 queries, recall@5) showing generated
  batteries materially behind on discovery-class queries.
- **Static thesaurus** — alias expansion from `system.functions.alias_to`
  and manifest keywords, as a zero-LLM battery enricher. Small; first
  candidate after M2.
- **In-body match highlighting** — parse-level feature (atoms are retained
  at parse); re-lowering per query is ~1 ms/10 KB and feasible but breaks
  parse-once/render-many. Trigger: users demonstrably losing the match
  inside long sections.

## Milestones

- **M0** — heading byte offsets (markdown + help); `help/search` package:
  battery compile, section table, executor, ranking, context extraction.
- **M1** — HelpHost search UI; `WithSectionFilter`; snippets filter box.
- **M2** — `helpsections`/`adrsections` providers, `docsearch` macro over
  the UNION with `system.documentation`, string-ref scheme, thesaurus.
- **M3** — text2regex generator under ADR-0139 (step b).
- **M4** — golden query set; evaluation gates the embeddings deferral.

## Surfaces

- `markdown.HeadingInfo.ByteOffset`, `markdown.WithSectionFilter` (widget
  package, additive).
- `help.SectionInfo.ByteOffset`; new package `help/search` (Battery, Index,
  Hit, Coverage).
- New widget `regexedit` (Edit + ErrorLabel), replacing regex_explorer's
  private highlight caches; `regexhighlight.HighlightTokens` and the
  codeview `BuildRegexTokens`/`PrepareRegexTokens` flavour behind it.
- HelpHost search state + nav flattening; play Snippets filter state; a
  coverage meter on both.
- M2 adds: two introspection tables, the `docsearch` macro, the ref-string
  format.

## Alternatives

- **Embeddings first (kelindar/search or similar)** — killed for now: query
  vectors need a model at query time so the floor stays lexical anyway;
  vectors are model-locked schema state with an asset-governance tail
  (pinned GGUF, re-embed lifecycle); and its in-memory vector index
  duplicates what the facts plane does better. Re-enters via §SD7's
  evaluation trigger.
- **RAG-less context-stuffing as search** — killed: bounded slices fit a
  context window but the plane (≥15 MB) never will; returns answers, not
  ranked navigable refs. It remains the *answer* layer's technique
  (ADR-0120/0139 territory), downstream of retrieval.
- **External search engine / fulltext library** — killed: dependency class
  out of proportion to tens of MB of text; the facts plane plus hyperscan
  already covers the scale rung.
- **Substring-only search (launcher-style)** — killed as the end state:
  no synonym rung to grow into and no path to §SD6; kept as the behaviour
  a degenerate battery (one literal) reproduces exactly.
- **In-process inverted index** — killed: complexity without a corpus that
  needs it; the scan is sub-millisecond at embedded scale and the facts
  plane owns the larger corpora.

## Consequences

### Positive

- Every tier degrades to the one below it, down to a literal substring scan
  that works offline on an M1 host.
- One query model (the battery) spans in-process RE2, ClickHouse hyperscan,
  and — later — LLM generation; semantics cannot drift per surface.
- The battery is transparent: users see (and can prune) exactly what
  matched, unlike similarity scores.

### Negative

- Filtered snippet rendering abandons egui widget state per query change
  (query-keyed IdScope) — accepted as the cost of the id-derivation
  invariant.
- Recall on the semantic rung is bounded by vocabulary enumeration until/
  unless the embeddings trigger fires.
- Two executors means two scoring implementations that must be kept
  weight-identical; the golden set (M4) is the drift alarm.

### Neutral

- The embedded tier rescans on every query change; caching is keyed by
  query string only.

## Migration

Additive throughout; no data or id migrations. `HeadingInfo`/`SectionInfo`
gain a field; existing consumers compile unchanged. The snippets tab's
unfiltered render path keeps its current id stream (the filter scope only
wraps the filtered path), so stored egui state survives for the no-filter
case.

## Verification plan

- `help/search`: table-driven tests over `fstest.MapFS` fixtures — battery
  compile/degrade, section slicing (frontmatter skip, preamble, trailing
  section), ranking tiers, RequireAll semantics, context extraction.
- `markdown`: section-membership walk tested as a pure function (segments +
  accept → visibility), including nested headings inside callouts staying
  with their enclosing section.
- HelpHost/snippets: state-level tests (hit → OpenRef plumbing); rendered
  behaviour via the demo/egui-mcp lane as usual.
- M2: schema-parity test between provider output and the `boxer adr` Arrow
  dump precedent; macro expansion goldens under nanopass tests.

## Status

Proposed. M0+M1 implemented alongside this draft for review; M2+ pending
acceptance.

## References

- ADR-0094 (introspection tables), ADR-0122 §SD4 (`adrcontent`), ADR-0125
  (codeview memoisation), ADR-0139 (text2dsl), ADR-0120 (Ask panel),
  ADR-0158 §SD6 (launcher search precedent).
- `public/keelson/runtime/help` (BookI/RefT), `widgets/markdown`
  (EXPLANATION.md — id derivation order), `apps/play/play_docs_clickhouse.go`
  (`system.documentation`).
