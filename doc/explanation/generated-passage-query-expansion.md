---
type: explanation
audience: contributor deciding how the documentation search ladder gains a semantic rung, or weighing a text-similarity measure for it
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Written 2026-08-15 from one day of
> measurement; nothing here is implemented in the runtime, and no ADR
> exists for it — this page is the record of what was learned so a future
> decision starts from numbers. Provenance: the numbers were measured that
> day on one machine, with throw-away harnesses outside the tree, on the
> repository's own docs (16 hand-written queries) and on TREC DL 2019/2020
> with published generated passages, against published reference results;
> the two papers quoted were re-read; the rest of the literature is named
> so it can be checked, not re-read. Treat every number as indicative.

# Generated-passage query expansion, and what did not work

## 1 The short version

The documentation search ladder ([ADR-0164](../adr/0164-documentation-regex-search.md))
stops at LLM-generated regex batteries and defers embeddings behind an
evaluation gate. Two ideas were tested for the next rung:

- **HyDE-style search with compression similarity (NCD) instead of an
  embedding.** Not viable. Given the same generated passage, every
  compression-based scorer — LZ or PPM, with or without a background
  dictionary — ranks below plain BM25 on the *raw* query on human data, and
  ~15 points below the expansion; fusing it in loses points. §3.
- **The generated passage as an *expansion* of the query, scored by BM25 —
  over character n-grams so no tokenizer, stemmer or stopword list is
  needed.** Works. On TREC DL it lifts BM25 from 52 to 65 nDCG@10 (words)
  and to 61 (padded 4-grams); on the repository docs it lifts section-level
  MRR from 0.42 to 0.59. §4. The configuration that measured best: keep the
  query and weight it five times the passage, count grams as multisets,
  padded within-token 4-grams for space-delimited scripts, statistics from
  the whole corpus. §5.

Two side conclusions: the language-independence argument for compression is
really an argument for n-grams (§6); and compression similarity remains the
right tool for authorship and verbatim-overlap questions, which are a
different problem (§7).

### 1.1 The scale, in one table

Everything below is TREC DL 2019 / 2020 passage ranking scored with
**nDCG@10**: take the ten top-ranked passages, add up their graded relevance
(NIST judges scored each 0–3), discount each by the logarithm of its rank so
a hit at position 1 counts more than one at position 10, and divide by what
the ideal ordering of the known relevant passages would score. 100 means the
first page is the best possible page in the best order; a system at 50 is
delivering roughly half of the achievable ranked relevance in its top ten;
+5 is a difference a user notices, +15 changes what the tool is.

| system | DL19 / DL20 | source |
| --- | ---: | --- |
| oracle reorder of the BM25 top-1000 (ceiling of the setting) | 93 / 96 | measured |
| NCD, zstd default level (the shipped compression path) | 16 / 8 | measured |
| NCD, best LZ coder (gzip-9), generated passage | 44 / 39 | measured |
| NCD, PPM coder with background dictionary (best compression arm) | 49 / 49 | measured |
| n-gram BM25, raw query | 45 / 47 | measured |
| **classical fulltext:** word BM25 (stemmer, stopwords), raw query | 52 / 51 | measured; papers 51 / 48 |
| n-gram Dice on the generated passage alone | 50 / 50 | measured |
| **n-gram BM25 + generated passage** (5·query + passage) | 61 / 61 | measured |
| **HyDE, dense** — zero-shot Contriever + generated passage | 61 / 58 | HyDE paper |
| **word BM25 + generated passage** (Query2doc form) | 65 / 64 | measured; paper 66 / 63 |
| word BM25 + passage from GPT-4 | 69 / 65 | Query2doc paper |
| supervised dense retriever (DPR / ANCE, trained on MS MARCO) | 65 / 65 | HyDE, Query2doc papers |
| state-of-the-art distilled dense retriever (E5 + KD), and with the passage | 74 / 71 · 75 / 73 | Query2doc paper |

So: compression similarity sits *below* classical fulltext; the generated
passage lifts fulltext by 9–13 points, to the level of zero-shot semantic
search (n-grams) or above it (words); and only dense retrievers *trained*
for the task clearly beat that, by another ten — bringing the model asset,
index and in-domain training that ADR-0164 deferred. Zero-shot Contriever
*without* HyDE scores 45 / 42, i.e. worse than raw BM25 (HyDE paper).

## 2 What was measured

- **Repository probe.** 235 markdown documents (ADRs from 0021, howtos,
  explanations, skills, help books, root docs) split at headings into 4 178
  sections — ADR-0164 §SD1's unit — with sixteen questions whose target
  document is known and a ~650-byte hypothetical passage per question,
  written blind by the analysing model in a generic register. Metric: MRR
  and hits@k of the target document / section. Small, and biased in ways
  the second dataset was chosen to remove: the docs *and* the passages are
  machine-written, and the queries are keyword-rich.
- **TREC DL 2019 / 2020 passage reranking.** The official BM25 top-1000
  candidates per query, NIST graded judgements (43 / 54 queries), and the
  Query2doc release of GPT-3.5 pseudo-documents for exactly those queries
  (`intfloat/query2doc_msmarco`, CC-BY-4.0). Metric `ndcg_cut_10`. BM25
  statistics from the full 8.8 M-passage collection. Calibration against the
  papers, same pseudo-documents:

  | arm | DL19 | published | DL20 | published |
  | --- | ---: | --- | ---: | --- |
  | BM25(query) | 52.1 | 50.6 / 51.2 | 51.5 | 48.0 / 47.7 |
  | BM25(query×5 ⧺ passage) | 65.1 | 66.2 | 63.9 | 62.9 |
  | HyDE with a dense encoder (Contriever), for scale | — | 61.3 | — | 57.9 |

  Within one to three points with a different BM25, a weaker stemmer and a
  reranking rather than retrieval setting: the harness is on scale.

## 3 Compression similarity is not a search scorer

nDCG@10 on TREC DL, generated passage as the input unless noted:

| scorer | DL19 | DL20 |
| --- | ---: | ---: |
| NCD, zstd default level (the shipped `compression.Similarity` path) | 15.6 | 7.8 |
| NCD, gzip-9 · LZMA · brotli-6 | 44.3 · 43.7 · 41.6 | 38.7 · 35.7 · 34.2 |
| NCD with a 24 KB background dictionary, gzip-9 | 45.8 | 42.6 |
| same with an order-5 PPM coder (ideal code length; the family's best) | 47–49 | 48–49 |
| NCD on the raw query, any coder | 6–10 | 6–10 |
| BM25(query) — the floor | 52.1 | 51.5 |
| BM25(query×5 ⧺ passage) | 65.1 | 63.9 |
| RRF(BM25(query×5 ⧺ passage), best compression arm) | 60.0 | 59.8 |

Why, briefly: NCD's max-normalisation rewards candidates whose compressed
size matches the query's (its top-10 clusters at the passage's own length
on both datasets); it has no notion of term rarity (a background dictionary
— compression's IDF — is the one thing that helps, and not enough); its
scores are coarse integers of a few hundred bytes; and the coder is the
estimator — zstd's fast level finds no short shared words at all, deflate-9,
LZMA and brotli-6 land within a few points of each other, PPM adds a few
more. On the repository docs a PPM background-dictionary NCD did reach BM25's
level (0.70 vs 0.73 doc MRR) — machine-written docs *and* passages, 1 KB
sections, a background in the same house style — which is exactly the
register effect the second dataset was there to catch, and it does not
carry over. Cost is 4–20× BM25 per pair regardless. Closed for search.

## 4 The generated passage as expansion

The formulation matters more than the scorer. With BM25, the passage
*alone* — the literal HyDE recipe — is the weak form; keeping the user's
terms and up-weighting them is the strong one (Query2doc's `q×5 ⧺ d`):

| input to BM25 | words, DL19 / DL20 | padded 4-grams, DL19 / DL20 |
| --- | ---: | ---: |
| query | 52.1 / 51.5 | 45.1 / 46.6 |
| passage alone | 56.4 / 54.9 | 53.1 / 50.7 |
| query ⧺ passage | 60.2 / 59.7 | 57.1 / 55.9 |
| **query×5 ⧺ passage** | **65.1 / 63.9** | **61.2 / 60.6** |

The weight is a mixture parameter with no closed form; swept from 0 to 20
it peaks at 5 for words and n-grams alike and is a plateau — 3…8 for words
within two points, 3…5 for n-grams (overlapping grams from one long query
word count as several pieces of evidence, so over-weighting hurts faster).
On the repository docs the same expansion moved BM25's section-level MRR
from 0.42 to 0.59 and 4-gram scoring from 0.46 to 0.77 doc-level.

Two caveats travel with the result. The gain scales with the generating
model — Query2doc's ladder is 1.3 B ≈ +1, 6.7 B ≈ +4, `text-davinci-003`
+15 on DL19 — so a small local model buys little. And a generation call
sits on the query path: seconds, a dependency, nothing on an air-gapped
host — the same shape the battery rung already accepted; the lexical floor
stays.

## 5 The configuration, and where it plugs in

- **Multiset counting.** `qtf(g) = 5·qtf_query(g) + qtf_passage(g)` on the
  gram multisets, not on a concatenated string (identical scores here, but
  no spurious grams across seams and the weight becomes a per-source
  factor).
- **Grams.** Padded within-whitespace-token 4-grams (`_word_`) for
  space-delimited scripts — half a point over unsegmented grams at the
  optimum, two on the bare query, more robust to over-weighting; 4 beat 5
  on these short passages. Unsegmented code-point 2-grams for CJK. Lower-case
  first. No stemmer, no stopword list.
- **Statistics from the whole corpus** — N, average length, document
  frequency per gram: one `GROUP BY`. Pool-only statistics cost word BM25
  three points in the same position.
- **BM25 parameters** untuned on the n-gram side (`k1` 1.2, `b` 0.75);
  word BM25 ran Anserini's 0.9 / 0.4. Worth a point or two, unmeasured.
- **Cost.** A join over the query's few hundred grams against a gram-sorted
  posting table; ClickHouse's built-in `ngram*` functions are a
  fixed-4-gram, unweighted alternative for one-shot scans.

The runnable form is
[Search without a tokenizer — n-grams in ClickHouse](./ngram-search-in-clickhouse.md);
in the tree the natural home is ADR-0164's facts-plane lane, where the
section corpora already exist as the `helpsections` / `adrsections` tables
and the battery executor is a macro over them. On the battery side the same
idea is: the user's tokens as required patterns, the passage's distinctive
tokens as ranked ones. Whether the rung is worth having is the golden-set
question ADR-0164 §SD7 already names; this page fixes the arm it should
test and removes one it should not.

## 6 Segmentation: the argument is right, the remedy is n-grams

BM25 needs corpus statistics *and* a way to cut text into terms. The
statistics are one automatic pass (twenty seconds for 8.8 M passages). The
cutting — tokenizer, stemmer, stopwords — is the manual, per-language part,
and it is real: ClickHouse's `tokens('東京都渋谷区で会議')` is one token.
Compression needs neither, which is its appeal; but character n-grams need
neither either, and n-gram BM25 measured 15+ points above every compression
scorer and 3–4 under word BM25 given the same expansion. Measured on
English; the code path is the same for CJK.

## 7 Where compression similarity still belongs

Authorship attribution and verbatim-overlap detection are a different
problem on every axis that decided the search result: the reference is an
author's whole history (ideally a *trained* dictionary), the candidates are
a closed set, style is the signal rather than the noise, the measure is a
conditional length compared across authors, and term rarity is irrelevant.
Nothing here contradicts the empirical results of identifying forum authors
with zstd; the register effect of §3 is, read the other way, evidence *for*
that use. The concrete upgrade on that path is trained per-author
dictionaries — the vendored `klauspost/compress` has `dict.BuildZstdDict` —
at a level above default, and a PPM coder (a 200-line pure-Go ideal-code-length
model exists as scratch from this work) where statistical rather than
verbatim similarity is wanted. `writingstylescope` and the `stylometry`
package are unaffected by anything above.

## 8 Not measured

The ADR-0164 golden set for the repository's own corpus (the probe's sixteen
queries are not it); the battery executor as an arm; CJK retrieval quality;
a small local model as the generator; the authorship protocol.

## References

- Gao, Ma, Lin, Callan. *Precise Zero-Shot Dense Retrieval without
  Relevance Labels* (HyDE), arXiv 2212.10496 — re-read.
- Wang, Yang, Wei. *Query2doc: Query Expansion with Large Language Models*,
  arXiv 2303.07678 — re-read; the sparse sibling of HyDE and the source of
  the `q×5 ⧺ d` form and of the pseudo-documents used.
- TREC Deep Learning 2019/2020 passage task and NIST qrels; MS MARCO terms
  (non-commercial research; datasets never committed).
- Cilibrasi, Vitányi. *Clustering by Compression*, 2005; Cebrián,
  Alfonseca, Ortega. *Common pitfalls using the normalized compression
  distance*, 2005; McNamee, Mayfield. *Character N-Gram Tokenization for
  European Language Text Retrieval*, 2004 — pointers, not re-read.
- In-tree: [ADR-0164](../adr/0164-documentation-regex-search.md),
  [ADR-0175](../adr/0175-writingstylescope-section-compression-distance.md),
  `public/analytics/similarity/compression`,
  [ngram-search-in-clickhouse](./ngram-search-in-clickhouse.md).
