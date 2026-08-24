-- The jsonv2 half of the head-to-head in README.md §6, matched
-- statement for statement with queries-usp-leeway.sql (U1..U9). Run against the
-- A00 reference — plain `JSON`, engine defaults, ORDER BY tuple().
--
--   DB=jsonbench_a00_10m TABLE=bluesky QUERIES=./queries-usp-jsonv2.sql \
--   OUT=<dir> DROP_CACHES=1 TRIES=3 \
--   CH_SETTINGS="--use_query_condition_cache=0 --min_execution_speed=0" \
--   ./measure.sh bench
--
-- `--min_execution_speed=0` is not a tuning choice: without it the server's own
-- guard aborts U3 with TOO_SLOW at ~66k rows/s against a 250k minimum, so the
-- query has no runtime to report at all.
--
-- Two statements are placeholders (`SELECT 1`) because the question has no
-- jsonv2 form over unknown paths — U5 and U9. They are kept in place so the
-- statement numbering lines up with the leeway file; the document says what is
-- missing and why.

-- U1 — path census with per-path document counts
SELECT path, count() AS documents
FROM (SELECT arrayJoin(JSONAllPaths(data)) AS path FROM bluesky)
GROUP BY path ORDER BY documents DESC LIMIT 20;

-- U2 — path x type census
SELECT pt.1 AS path, count() AS n
FROM (SELECT arrayJoin(JSONAllPathsWithTypes(data)) AS pt FROM bluesky)
GROUP BY path, pt.2 HAVING n > 0 ORDER BY n DESC LIMIT 20;

-- U3 — value anywhere. There is no columnar form: `getSubcolumn(data, <expr>)`
-- requires a *constant* path and `data[<expr>]` is rejected outright, so a
-- value search over unknown paths has to re-serialise each document to text and
-- substring-match it. That is weaker as well as slower — it matches inside
-- other values and cannot report which path held the hit.
SELECT count() FROM bluesky WHERE position(toString(data), 'did:plc:vwadmn5cx4d2rqxbxbjajzhx') > 0;

-- U4 — subtree prefix census
SELECT path, count() AS occurrences
FROM (SELECT arrayJoin(arrayFilter(p -> startsWith(p, 'commit.record.embed.'), JSONAllPaths(data))) AS path FROM bluesky)
GROUP BY path ORDER BY occurrences DESC LIMIT 10;

-- U5 — sum every integer whatever its path: no expression exists.
SELECT 1;

-- U6 — leaf count per document. Note the semantics differ from the leeway U6:
-- JSONAllPaths does not descend into arrays, so an array counts once here and
-- once per element there. Both answer "how wide is a document" in their own
-- vocabulary; the numbers are not meant to match.
SELECT sum(length(JSONAllPaths(data))) AS leaves FROM bluesky;

-- U7 — presence of one constant path, the shape jsonv2 is built for: a typed
-- subcolumn read, no whole-object materialisation.
SELECT count() AS documents FROM bluesky WHERE data.commit.record.text IS NOT NULL;

-- U8 — a numeric predicate across every integer-valued path. As written this is
-- the text fallback again, and it is *not* the same question: it names
-- `time_us`. Asking it over unknown paths would mean running U2 first and
-- emitting a per-path disjunction — a two-step, schema-dependent workaround
-- rather than a query.
SELECT count() AS documents FROM bluesky WHERE JSONExtractInt(toString(data), 'time_us') > 1700000000000000;

-- U9 — array degree for every array-valued path: no form over unknown paths.
-- For a *known* path, `length(data.commit.record.langs)` is available and cheap.
SELECT 1;
