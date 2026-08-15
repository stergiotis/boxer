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
> authority it is named.

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
survived.

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

## 8 What was measured, and what was not

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
