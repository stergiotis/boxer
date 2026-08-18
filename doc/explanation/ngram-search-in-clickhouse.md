---
type: explanation
audience: contributor building or evaluating a text-search rung over the facts plane
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Written 2026-08-15 as a worked
> example, not a description of anything the runtime does today: no boxer
> surface uses the patterns below yet. Every SQL snippet was run against a
> local ClickHouse 26.7.3 (`clickhouse local`) on the compile date and the
> outputs shown are what it printed; the retrieval numbers quoted come from
> [Generated-passage query expansion, and what did not work](./generated-passage-query-expansion.md),
> measured the same day. Function semantics were checked by probing, not
> read from source; where the ClickHouse documentation is the better
> authority it is named. §8 was added 2026-08-18 and measured separately —
> ClickHouse 26.7.3.19 over HTTP, on a synthetic corpus it describes.

# Search without a tokenizer — n-grams in ClickHouse

## 1 Why n-grams

Every classical lexical ranker — BM25 included — needs two things: corpus
statistics (how many documents there are, how long they are, how many
contain each term) and a way to cut text into terms. The first is one
automatic pass over the corpus; the statistics for 8.8 million MS MARCO
passages were computed here in twenty seconds of `clickhouse local`. The
second is where the manual, per-language work hides: a tokenizer that knows
where words end, a stemmer that knows how the language inflects, a stopword
list that knows what carries no meaning. English gets all three for free.
Chinese, Japanese and Thai have no word boundaries to split on; agglutinative
languages defeat stemmers; a Swiss-German comment thread defeats every list.
ClickHouse's own `tokens()` shows the shape of the problem:

```sql
SELECT tokens('Der Über-Wagen fährt schnell'), tokens('東京都渋谷区で会議');
-- ['Der','Über','Wagen','fährt','schnell']   ['東京都渋谷区で会議']
```

Character n-grams remove the second requirement without touching the first.
Cut every document into overlapping runs of *n* code points and treat each
run as a term: no dictionary, no stemmer ("rotate", "rotation" and "rotieren"
share `rota`), no stopword list (frequent grams get low IDF by themselves),
and the same code path for every script — only *n* changes. This is not new:
character-n-gram BM25 was shown competitive with stemming across European
languages without language resources (McNamee & Mayfield 2004, from memory),
and Lucene's Chinese/Japanese support is bigram-based for the same reason.

What it costs, measured on TREC DL 2019/2020 passage reranking against word
BM25 with a stemmer and stopwords: with the query expanded by a generated
passage (§7), padded within-token 4-gram BM25 scored 61.2 / 60.6 nDCG@10
against 65.1 / 63.9 for word BM25; on the bare query 45.1 / 46.6 against
52.1 / 51.5. Three to four points on the expansion, more on bare short
queries. Every compression-based similarity trailed by fifteen points or
more in the same study; n-grams are the segmentation-free option that
survived. The dense family — embed both sides, score by angle — is a
different rung and is deferred, not dismissed; §8 records what the engine
can do for it.

## 2 What ClickHouse offers

| function | what it computes | notes (probed on 26.7.3) |
| --- | --- | --- |
| `ngramDistance(hay, needle)`, `…CaseInsensitive`, `…UTF8`, `…CaseInsensitiveUTF8` | symmetric 4-gram distance, 0 identical … 1 disjoint | *n* is fixed at 4; plain variants work on bytes, `UTF8` on code points; a non-constant string over 32 KB makes the result 1 |
| `ngramSearch(hay, needle)` and variants | asymmetric: share of the needle's 4-grams present in the haystack, 0 … 1 | argument order matters — `ngramSearch('…quick brown fox…', 'quick brown fox') = 1`, swapped 0.24 |
| `ngrams(s, N)` | array of the code-point *N*-grams of `s` | `N` must be a constant; `ngrams('日本語テキスト', 2)` → `['日本','本語','語テ','テキ','キス','スト']` |
| `splitByWhitespace(s)`, `lowerUTF8(s)` | whitespace split, case folding | enough to pad grams within tokens (§5); no linguistic knowledge involved |
| `ngramMinHash*`, `ngramSimHash*`, `wordShingle*` | fixed-size sketches for near-duplicate detection | pair with `tupleHammingDistance`; not covered here |
| `tokens(s)`, `splitByNonAlpha(s)` | word tokenizers | the thing n-grams replace; shown above failing on CJK |
| `ngrambf_v1` skip index | bloom filter over n-grams for `LIKE` / `hasToken` pre-filtering | a candidate filter, not a scorer; not used below |
| `estimateCompressionRatio(codec)(col)` | aggregate; per single-row group it yields a compressed length | how compression similarity *can* be computed in SQL; measured slow and worse |

There is no ranking function in the engine — no `bm25()` — and no need for
one: the score is a `sum()` over a join, and the "index" is two ordinary
`MergeTree` tables.

## 3 The corpus for the examples

Seven one-line documents in three scripts, so the segmentation point is
visible in the output:

```sql
CREATE TABLE docs (id UInt32, lang String, body String) ENGINE = MergeTree ORDER BY id;
INSERT INTO docs VALUES
 (1,'en','How to rotate the release signing key and publish the new public key'),
 (2,'en','Build tags must be passed to every go test and go build invocation'),
 (3,'de','Signaturschlüssel für Releases rotieren und den neuen öffentlichen Schlüssel veröffentlichen'),
 (4,'de','Die Build-Tags müssen bei jedem go test und go build übergeben werden'),
 (5,'ja','リリース署名鍵をローテーションし新しい公開鍵を公開する'),
 (6,'ja','ビルドタグはすべての go test と go build に渡す必要がある'),
 (7,'en','Frame pacing and compositor vsync explain most janky rendering');
```

## 4 Shape one — a similarity scan, no index

The smallest possible search: the query is the needle, every row is a
haystack, the engine scans.

```sql
SELECT id, lang, round(ngramSearchCaseInsensitiveUTF8(body, 'rotate signing key'), 3) AS coverage
FROM docs ORDER BY coverage DESC LIMIT 3;
-- 1  en  1
-- 3  de  0.25
-- 7  en  0.188
```

`ngramSearch` is the right built-in for a *query* (how much of what I asked
is in this row); `ngramDistance` for *document against document* (how alike
are these two texts) — its symmetric normalisation penalises length
mismatch, which is what you want between two sections and not what you want
between a five-word query and a section. Both are fixed 4-gram, unweighted,
hashed internally; there is no IDF, so a rare identifier and a common bigram
of the language count alike. Fine for small corpora and near-duplicate work,
and it is what makes shape one a one-liner: no state, nothing to refresh, a
full scan of the ~15 MB of markdown corpora ADR-0164 names costs well under
a second.

## 5 Shape two — BM25 over n-grams

The segmentation-free ranker, in the configuration that measured best:
grams padded within whitespace tokens (`_word_`) for space-delimited
scripts — half a point over unsegmented grams, two on bare queries, more
robust to query over-weighting — and unsegmented bigrams for CJK; lower-case
first. Three tables carry the statistics; a query is a join and a sum.

```sql
-- per-document n-gram multiset. ngrams(s, N) needs a constant N, so a
-- per-script choice is a UNION ALL of constant-n passes.
CREATE TABLE doc_ngram ENGINE = MergeTree ORDER BY (g, id) AS
SELECT id, g, count() AS tf FROM docs
  ARRAY JOIN arrayFlatten(arrayMap(t -> ngrams(concat(' ', t, ' '), 4), splitByWhitespace(lowerUTF8(body)))) AS g
  WHERE lang != 'ja' GROUP BY id, g
UNION ALL
SELECT id, g, count() AS tf FROM docs ARRAY JOIN ngrams(lowerUTF8(body), 2) AS g WHERE lang = 'ja' GROUP BY id, g;

CREATE TABLE ngram_df ENGINE = MergeTree ORDER BY g AS SELECT g, count() AS df FROM doc_ngram GROUP BY g;
CREATE TABLE doc_len  ENGINE = MergeTree ORDER BY id AS SELECT id, sum(tf) AS dl FROM doc_ngram GROUP BY id;
```

The query cuts its inputs the same way and scores each document with the
textbook BM25 form (`k1 = 1.2`, `b = 0.75`, IDF as
`log(1 + (N − df + 0.5) / (df + 0.5))`). It takes two strings — the user's
query `q` and an optional generated passage `p` (§7) — and weights them as
multisets, five to one, before the join; with `p` empty it is plain BM25 on
the query:

```sql
WITH
  (SELECT count() FROM docs) AS N,
  (SELECT avg(dl) FROM doc_len) AS avgdl,
  1.2 AS k1, 0.75 AS b,
  qg AS (SELECT arrayJoin(arrayFlatten(arrayMap(t -> ngrams(concat(' ', t, ' '), 4), splitByWhitespace(lowerUTF8({q:String}))))) AS g),
  pg AS (SELECT arrayJoin(arrayFlatten(arrayMap(t -> ngrams(concat(' ', t, ' '), 4), splitByWhitespace(lowerUTF8({p:String}))))) AS g),
  q  AS (SELECT g, sum(w) AS qtf FROM (SELECT g, 5 AS w FROM qg UNION ALL SELECT g, 1 AS w FROM pg) GROUP BY g)
SELECT d.id, any(docs.lang) AS lang,
  round(sum(q.qtf * log(1 + (N - s.df + 0.5) / (s.df + 0.5)) * d.tf * (k1 + 1) / (d.tf + k1 * (1 - b + b * l.dl / avgdl))), 3) AS bm25
FROM q
JOIN doc_ngram AS d USING g
JOIN ngram_df  AS s USING g
JOIN doc_len   AS l ON l.id = d.id
JOIN docs ON docs.id = d.id
GROUP BY d.id ORDER BY bm25 DESC LIMIT 3;
-- {q:String} = 'how do I rotate the signing key', {p:String} = ''
-- 1  en  146.361
-- 3  de   13.813      ← shares "rotier…"/"release" grams with the English query
-- 7  en    7.968
```

The same statement with `ngrams(…, 2)` and a Japanese query does what no
tokenizer above could:

```sql
-- {qj:String} = '署名鍵 公開'
-- 5  ja  33.189
```

Nothing in either query knows what language it is looking at. Refreshing
the "index" when the corpus changes is a re-`INSERT` of the affected
documents' rows into `doc_ngram` and a recount of `ngram_df` / `doc_len`
(both cheap `GROUP BY`s; a materialized view over `doc_ngram` would keep
them live). At the scale of ADR-0164's corpora — thousands of sections,
hundreds of grams each — the three tables are a few million rows.

## 6 Shape three — "more like this": document-to-document with IDF

`ngramDistance` answers this unweighted; the weighted version is the same
join with the reference document's grams in place of the query, normalised
Dice-style so a long and a short section can both be near:

```sql
WITH
  (SELECT count() FROM docs) AS N,
  x AS (SELECT g, tf AS xtf FROM doc_ngram WHERE id = 1),
  (SELECT sum(xtf * log(1 + (N - df + 0.5) / (df + 0.5))) FROM x JOIN ngram_df USING g) AS xmass,
  m AS (SELECT id, sum(tf * log(1 + (N - df + 0.5) / (df + 0.5))) AS mass FROM doc_ngram JOIN ngram_df USING g GROUP BY id)
SELECT d.id, any(docs.lang) AS lang,
  round(2 * sum(least(x.xtf, d.tf) * log(1 + (N - s.df + 0.5) / (s.df + 0.5))) / (xmass + any(m.mass)), 3) AS dice_idf
FROM x
JOIN doc_ngram AS d USING g
JOIN ngram_df  AS s USING g
JOIN m ON m.id = d.id
JOIN docs ON docs.id = d.id
WHERE d.id != 1
GROUP BY d.id ORDER BY dice_idf DESC LIMIT 3;
-- 3  de  0.103      ← the German signing document, across the language line
-- 2  en  0.045
-- 7  en  0.041
```

A document-to-document similarity is not a query-answering scorer: use
shape two for questions and shape three for neighbours.

## 7 The expansion, and where this plugs in

A short query is the weak point of any lexical ranker, n-gram or word. The
measured remedy (Query2doc; the explanation linked above) is to let an LLM
write a passage that would answer the query and to rank with the query's
grams weighted five times the passage's — the query stays dominant, the
passage supplies the vocabulary the user did not type. Shape two already
has the slot: pass the passage as `{p:String}`.

```sql
-- {q:String} = 'how do I rotate the signing key'
-- {p:String} = 'Rotate the release signing key: generate a new key pair, publish the new
--               public key next to the releases, sign the next release with it and keep
--               the old public key available so older releases stay verifiable.'
-- 1  en  275.873
-- 3  de   42.137
-- 7  en   11.206
```

Feeding the passage *alone* was measured to be worse than the query alone
in the retrieval setting; the five-to-one weight is a plateau (three to
five for n-grams), set on TREC DL and to be confirmed on the repository's
own golden set. Corpus statistics should come from the whole corpus, not a
candidate pool; `k1`/`b` were left at 1.2 / 0.75, untuned.

In the tree, the natural home is [ADR-0164](../adr/0164-documentation-regex-search.md)'s
facts-plane lane: the section corpora already exist as the `helpsections`
and `adrsections` introspection tables, the battery executor is a
`docsearch()` macro, and shape two is the same kind of macro over the same
sections — its `doc_ngram` a materialisation of those tables. Whether such
a rung is worth having is the golden-set question that ADR names, not this
page's.

## 8 A dense alternative — quantised vectors on the same tables

Everything above scores a query and a document by the text they share. The other
retrieval family embeds both as points and scores them by angle;
[ADR-0164](../adr/0164-documentation-regex-search.md) defers it behind an
evaluation gate for reasons that still hold — the query vector needs a model at
query time, so the lexical floor stays whatever else is built, and the vectors
are model-locked schema state with a re-embedding lifecycle. What has changed
since that ADR is the engine. ClickHouse 26.7 carries vector-quantisation
primitives it did not have, and the published algorithm that would use them best
turns out to be mostly expressible in SQL. This section records what was
measured, so that if the gate opens it opens onto numbers.

The occasion for looking was [turbovec](https://github.com/RyanCodrai/turbovec),
an MIT-licensed Rust index implementing Google Research's TurboQuant
(arXiv:2504.19874). The crate is not the proposal here. It is a *flat* index — a
full scan with a good kernel — and a full scan is what ClickHouse already does;
its two additions over a plain scan are a codebook and a SIMD kernel, and the
engine has a kernel. What transfers is the encoding, and the property that
makes it transferable is that TurboQuant is **data-oblivious**: the codebook is
fixed in advance from a known distribution, so there is no training pass and no
fitted state to keep in step with a corpus. Its one optional stage that *is*
fitted, the per-coordinate calibration of §8.2, turns out to be the stage that
matters most here — which is convenient, since fitting it is one aggregate.

> Measured on 2026-08-18 against ClickHouse 26.7.3.19 over the HTTP interface —
> not the `clickhouse local` of the earlier sections. The corpus is synthetic:
> 20 000 vectors at *d* = 512, given transformer-like anisotropy (per-coordinate
> standard deviation spanning 0.159 to 0.0071, a 22× spread, over a non-zero
> per-coordinate mean) and then L2-normalised; 50 queries drawn the same way.
> Ground truth is the exact float32 cosine top-10. §8.6 says what that corpus
> does and does not stand in for.

### 8.1 What the engine offers

| primitive | what it is | probed |
| --- | --- | --- |
| `vector_similarity` index | HNSW over the vectors, GA since 25.8 | accepts `Array(Float32\|Float64\|BFloat16)` only |
| `QBit(T, d)` | bit-transposed storage: the same bit position across all coordinates is one stream, so a query can read the top *b* planes and skip the rest | `Int8`, `BFloat16`, `Float32`, `Float64` |
| `{L2,cosine,dotProduct}Transposed(v, q, b)` | distance against a `QBit` column at a *b*-bit budget | 9 in the `…Transposed` family, 21 vector distances in all |

The two do not compose. Asking for the index on a quantised column is refused:

```sql
CREATE TABLE t (id UInt32, v QBit(Float32,384),
  INDEX idx v TYPE vector_similarity('hnsw','cosineDistance',384) GRANULARITY 1) ENGINE=MergeTree ORDER BY id;
-- Code: 44. Vector similarity index can only be created on columns of type
-- Array(Float32|Float64|BFloat16): When validating secondary index `idx`.
```

So the engine offers indexed-but-uncompressed, or compressed-but-unindexed, and
not both. On the compressed side the useful question is how far the bit budget
can be turned down before the ranking stops being the ranking. Recall of the
true top-10, both as the quantised top-10 and within a quantised top-100
shortlist:

| bits per coordinate | 16 | 12 | 8 | 4 |
| --- | ---: | ---: | ---: | ---: |
| recall@10, quantised top-10 | 0.990 | 0.924 | 0.294 | 0.048 |
| recall@10 within quantised top-100 | 1.000 | 1.000 | 0.700 | 0.234 |

`QBit` truncates IEEE-754 bit planes, so below about 12 bits it is discarding
mantissa and the ordering degrades quickly. Twelve bits is a 2.7× saving over
float32 — real, but not the regime the quantisation literature is about.

### 8.2 TurboQuant, and which of its stages ClickHouse can express

The algorithm is six stages. The question for this page is which have a SQL
form.

| # | stage | ClickHouse form | verdict |
| --- | --- | --- | --- |
| 1 | normalise; keep the length aside | `arrayMap` over `arraySum` | plain SQL |
| 2 | random rotation, so coordinates become identically distributed | sign flip, then log₂*d* Walsh–Hadamard butterfly stages of `arrayMap`/`bitXor` | expressible, slow (§8.4) |
| 3 | per-coordinate calibration (TQ+) | `avgForEach` / `stddevPopForEach`, one row for the corpus | plain SQL, and cheap |
| 4 | Lloyd-Max scalar quantisation | the codebook is derivable by Lloyd iterations in SQL; *scoring* against it needs a per-dimension lookup table | **no SQL form** |
| 5 | bit-packing | `QBit(Int8, d)` with the codes in the high bits | plain DDL |
| 6 | length-renormalised scoring | a `Float32` column multiplied into the score | plain SQL |

Stage 4 is the one that does not fit, and it is worth being precise about why.
Scoring a non-uniform codebook is `Σⱼ LUT[j][cⱼ]` — a per-dimension table lookup
that FastScan-style kernels do with SIMD shuffles. A *uniform* codebook needs no
lookup, because dequantisation is affine: `Σⱼ (cⱼ + ½)·s·qⱼ` is an ordinary
inner product over the codes plus a term that is the same for every document,
and therefore invisible to a ranking. `dotProductTransposed` computes exactly
that. So the SQL-expressible subset is TurboQuant with a uniform quantiser in
place of Lloyd-Max.

How much that substitution costs can be measured directly, on the distribution
the rotation is supposed to produce. Both codebooks were derived in SQL — Lloyd's
algorithm iterated to convergence over two million standard normals for the
non-uniform one, an MSE scan over the step size for the uniform one — and both
reproduce the published tables (Max, 1960):

| levels | optimal uniform step | uniform MSE | Lloyd-Max MSE | Lloyd-Max gain |
| ---: | ---: | ---: | ---: | ---: |
| 16 (4-bit) | 0.335 σ | 0.01155 | 0.00954 | 17 % |
| 4 (2-bit) | 0.995 σ | 0.11896 | 0.11761 | 1.1 % |

Seventeen per cent of mean-squared error at 4 bits, one per cent at 2 — so the
stage with no SQL form is the one carrying the least of the algorithm's value.
§8.3 measures what that is worth in ranking rather than in distortion.

### 8.3 The ladder, measured

Each variant is scored the same way — an inner product against the codes, no
normalisation — so the rows differ only in the stage named. `K′` is the size of
the shortlist handed to an exact float32 rerank; `K′` = 10 is the quantised
ranking used raw.

**4-bit codes**

| | recall@10, `K′`=10 | `K′`=50 | `K′`=100 |
| --- | ---: | ---: | ---: |
| A — uniform quantiser, one global scale | 0.294 | 0.562 | 0.638 |
| B — A + per-coordinate calibration (stage 3) | 0.668 | 0.968 | 0.994 |
| C — A + Hadamard rotation (stage 2) | 0.560 | 0.944 | 0.992 |
| D — C + the Lloyd-Max codebook (stage 4, no SQL form) | 0.634 | 0.978 | 0.998 |
| B + length renormalisation (stage 6) | 0.474 | 0.842 | 0.936 |
| for comparison: `QBit(Float32)` bit planes at 4 bits | 0.048 | — | 0.234 |

**2-bit codes**

| | recall@10, `K′`=10 | `K′`=50 | `K′`=100 |
| --- | ---: | ---: | ---: |
| A — uniform quantiser, one global scale | 0.058 | 0.178 | 0.264 |
| B — A + per-coordinate calibration | 0.268 | 0.522 | 0.680 |
| C — A + Hadamard rotation | 0.210 | 0.476 | 0.626 |
| D — C + the Lloyd-Max codebook | 0.220 | 0.504 | 0.606 |
| B + length renormalisation | 0.284 | 0.558 | 0.680 |

Four things fall out.

- **Four bits with a rerank are enough; two bits are not.** Rows B, C and D at
  `K′` = 100 recover 0.99 of the exact top-10 from a shortlist scored at 4 bits
  per coordinate, where the engine's own 4-bit read recovers 0.234 and `QBit`
  needs 12 bits to get to the same place — a third of the storage for the same
  ranking. At 2 bits nothing here is usable, 0.68 at best. That is the distance
  to turbovec's published 2-bit results, and it is the part of the algorithm this
  SQL subset does not reach.
- **The rotation and per-coordinate calibration are interchangeable here.** They
  are two routes to one end — make every coordinate look alike so a single
  codebook fits — and behind a rerank they arrive within noise of each other
  (0.994 against 0.992). Calibration is ahead when the quantised ranking is used
  raw (0.668 against 0.560) and at 2 bits (0.680 against 0.626). Since
  calibration is one aggregate over the corpus and the rotation costs §8.4's
  throughput, the cheap route is the one to try first — but see §8.6, because
  this corpus is the one calibration is best at.
- **Lloyd-Max earns its 17 % where the shortlist is short, and nowhere else.**
  It is worth 0.560 → 0.634 on the raw quantised ranking and 0.992 → 0.998
  behind a top-100 rerank. The one stage with no SQL form is the one a rerank
  makes very nearly redundant.
- **Length renormalisation did not help.** A wash at 2 bits, a loss at 4
  (0.994 → 0.936). The documents here are exactly unit-norm before encoding, so
  there is no length variation for stage 6 to debias and dividing by the
  dequantised norm only adds noise. A statement about this implementation on
  this corpus, not about the paper's estimator.

### 8.4 Two costs, and one trap

**The rotation is expressible but slow.** A sign flip followed by log₂*d*
Walsh–Hadamard butterfly stages is exactly orthogonal — norm preserved to six
decimal places, and re-ranking the corpus in the rotated space reproduces the
exact ordering, recall 1.000 — and it does what it is for, flattening the
per-coordinate standard deviation spread from 22.2× to 1.1×. One stage is:

```sql
-- <h> is the stage's stride, <d> the dimension; neither is a bindable parameter
arrayMap(i -> if(bitAnd(i, <h>) = 0, x[i+1] + x[bitXor(i, <h>)+1],
                                     x[bitXor(i, <h>)+1] - x[i+1]), range(<d>))
```

chained over `h = 1, 2, 4, … , d/2` in a `WITH` list. In ClickHouse's array
functions this runs at **209 vectors per second** at *d* = 512 — 96 seconds for
the 20 000 vectors here. It is a write-side, once-per-vector cost, so ADR-0164's
4 178 sections would take about 20 seconds and be done; ten million vectors would
take half a day. The random-access subscript inside `arrayMap` is what costs;
nothing here vectorises.

**Storage falls out of the codec, not out of bit-packing.** Codes are stored one
per `Int8` with the low bits zeroed, and the columnar compressor removes the
zeroed planes without being asked:

| | on disk |
| --- | ---: |
| `Array(Float32)`, d = 512 | 39.29 MiB |
| `QBit(Int8)`, 4-bit codes | 5.00 MiB |
| `QBit(Int8)`, 2-bit codes | 2.56 MiB |

**The trap: `dotProductTransposed` casts its query argument to `Int8`.** A query
vector of embedding-scale floats truncates to all zeros and every document
scores exactly 0 — no error, no warning, a uniformly tied ranking that looks
like a bad index rather than a broken call. The query has to be scaled to fill
`[-127, 127]` first. This is the int8 × int4 shape the FastScan kernels use, and
it is not in the documentation.

### 8.5 The recipe

Calibration, encode, and the two-stage query. `emb(id, v Array(Float32))` is the
float32 corpus; the constant 0.335 is §8.2's optimal uniform step for 16 levels.

```sql
-- one row: the corpus's per-coordinate centre and spread
CREATE TABLE calib ENGINE = Memory AS
SELECT avgForEach(v) AS mu, stddevPopForEach(v) AS sd FROM emb;

-- 4-bit codes in the high nibble of an Int8, bit-transposed
CREATE TABLE emb_q4 (id UInt32, v QBit(Int8, 512)) ENGINE = MergeTree ORDER BY id;
INSERT INTO emb_q4 SELECT id,
  arrayMap((x, m, s) -> toInt8(least(greatest(floor((x - m) / (0.335 * s)), -8), 7) * 16),
           v, (SELECT mu FROM calib), (SELECT sd FROM calib))
FROM emb;
```

Stage one carries the same per-coordinate scale into the query, then fills the
`Int8` range — the step §8.4 warns about. The query vector binds as an ordinary
parameter; it does not need splicing:

```sql
WITH
  {q:Array(Float64)}                            AS qv,
  (SELECT sd FROM calib)                        AS sd,
  arrayMap((s, x) -> toFloat64(s) * x, sd, qv)  AS qs,
  arrayMax(arrayMap(y -> abs(y), qs))           AS qmax,
  arrayMap(x -> round(x * 127 / qmax), qs)      AS qi
SELECT id, dotProductTransposed(v, qi, 4) AS score
FROM emb_q4 ORDER BY score DESC LIMIT 100;
```

Stage two reranks that shortlist exactly, against the float32 column.

```sql
SELECT id FROM emb WHERE id IN (<the 100 ids from stage one>)
ORDER BY cosineDistance(v, {q:Array(Float64)}) ASC LIMIT 10;
```

Measured on this corpus, per query:

| | elapsed | read |
| --- | ---: | ---: |
| exact float32 scan, no quantisation | 13 ms | 41.4 MB |
| stage one — the 4-bit scan | 12 ms | 5.4 MB |
| stage two — rerank 100 ids by primary key | 3 ms | 0.2 MB |
| both stages as **one** statement, `id IN (subquery)` | 22 ms | 46.6 MB |

The last row is the practical catch. Written as a single statement the shortlist
does not reach the primary-key filter, the rerank reads the whole float32 column
anyway, and the quantisation buys nothing — it costs. The shortlist has to be
materialised between the two, as two round trips or a spliced id list.

At this corpus size the honest summary is that quantisation buys memory and not
latency: 7.7× less read for the same 12 ms, because 39 MiB is in page cache and
the scan is bound by the kernel rather than by I/O. The latency would follow the
I/O only for a corpus that does not fit in memory.

### 8.6 What this does and does not stand in for

- **The corpus is synthetic, and its anisotropy is axis-aligned by
  construction.** That is the condition per-coordinate calibration is best at,
  so §8.3's finding that stage 3 keeps up with stage 2 is close to the most
  favourable reading calibration could get. The rotation's argument is that it
  is data-oblivious and works against anisotropy in *any* basis; on a corpus
  whose spread is off-axis the two could separate. The comparison wants real
  vectors, which were not to hand.
- **No end-to-end retrieval quality was measured** — no nDCG, no MRR, nothing on
  the scale tabulated in
  [Generated-passage query expansion](./generated-passage-query-expansion.md) §1.1.
  Recall against the exact float32 neighbours measures what
  quantisation costs, not what dense retrieval is worth against the n-gram BM25
  of §5. The two numbers compose only through an evaluation ADR-0164 has not run.
- **The 2-bit gap is unexplained.** turbovec publishes usable 2-bit recall and
  this subset reaches 0.68; whether the difference is the Lloyd-Max codebook, the
  calibration procedure, the length renormalisation done properly, or the
  synthetic corpus was not determined.
- **Function semantics were probed, not read.** The `Int8` query cast, the
  midrise reconstruction at `code·2⁸⁻ᵇ + 2⁷⁻ᵇ`, and the index/`QBit`
  incompatibility are observed behaviour on 26.7.3.19.
- **The dependency question is separate from the technique.** Nothing above
  argues for taking the crate; it argues that most of what the crate implements
  has a SQL form over primitives the engine already ships, which is the shape
  [why-boxer](./why-boxer.md)'s P1 asks for before a dependency is considered.

## 9 What was measured, and what was not

- The TREC DL numbers in §1 are padded within-token 4-grams on English web
  passages with statistics from the candidate pool; word BM25 in the same
  comparison used full-collection statistics, an S-stemmer and Lucene's
  stopwords. Unsegmented byte 4-grams were half a point behind; 5-grams
  lost to 4-grams on these short passages.
- The multilingual behaviour above is demonstrated on seven sentences. No
  CJK or German *quality* number exists here; the claim is only that the
  code path is the same and needs no segmenter.
- The built-in `ngram*` functions hash their grams and were probed, not
  read; treat the semantics table as observed behaviour on one version.
- Cost: shape one is a scan; shape two's query is a join over the query's
  grams (a few hundred rows) against `doc_ngram` sorted by gram, so it is
  proportional to the posting lists touched, as any inverted index is.
- §8 carries its own caveats in §8.6; its corpus is synthetic and it
  measures quantisation loss against exact neighbours, not retrieval
  quality against the rankers above.

## References

- [Generated-passage query expansion, and what did not work](./generated-passage-query-expansion.md)
  — the measurements this page rests on.
- [ADR-0164 — documentation regex search](../adr/0164-documentation-regex-search.md)
  — the search ladder and its facts-plane lane.
- ClickHouse documentation: *String search functions* (`ngramDistance`,
  `ngramSearch`), *Splitting and merging strings* (`ngrams`, `tokens`,
  `splitByWhitespace`), *Hash functions* (`ngramMinHash`, `ngramSimHash`),
  *Skipping indexes* (`ngrambf_v1`).
- McNamee, Mayfield. *Character N-Gram Tokenization for European Language
  Text Retrieval*, Information Retrieval 7, 2004 (from memory — a pointer,
  not a citation).
- ClickHouse documentation: *QBit* data type (bit transposition, the
  per-query bit budget) and *Vector similarity index*.
- Zandieh, Daliri, Rabani et al. *TurboQuant: Online Vector Quantization
  with Near-optimal Distortion Rate*, arXiv:2504.19874 — the algorithm §8.2
  takes apart.
- [turbovec](https://github.com/RyanCodrai/turbovec) — the MIT-licensed Rust
  implementation that prompted §8; a flat index, not an ANN one.
- Gao, Long. *RaBitQ*, SIGMOD 2024, arXiv:2405.12497 — the per-vector length
  renormalisation of stage 6.
- Max. *Quantizing for Minimum Distortion*, IRE Trans. Information Theory 6,
  1960 — the scalar-quantiser tables §8.2's derivation reproduces.
