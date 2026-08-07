-- M4, arm N-text — DuckDB's JSON type over the raw corpus, one JSON value per
-- row. The counterpart of the sibling trial's jsonv2 arm, re-posed.
--
-- The USP document's thesis rests on two structural facts about ClickHouse's
-- JSON type (its §2a, §2b): an enumerated path cannot be used to *read* the
-- column, and enumeration stops at an array. **Neither holds here.**
--
--   json_extract(doc, p)   takes p as a runtime expression, not a constant
--   json_tree(doc)         walks every node, descending into arrays, and
--                          reports each one's `fullkey` — the path as data
--
-- So every query in this file is natively expressible, including the four the
-- USP document reports as either 676x slower or having no expression at all.
-- What is left to measure is the cost, not the possibility.
--
-- Two comparability notes, both inherited from the USP document:
--   - the two systems do not enumerate the same path set — leeway writes
--     `/commit/record/langs/_`, json_tree writes `$.commit.record.langs[0]` —
--     so per-path *counts* are not comparable across the arms. Runtimes are.
--     Array indices are normalised to `[*]` here so a path means one thing.
--   - absent paths yield NULL, coalesced to '' where the oracle emits the
--     type default (M0 finding 1).

ATTACH 'm4.duckdb' AS m4 (READ_ONLY);
USE m4;
-- @@

-- Q1 — event counts by collection
SELECT coalesce(json_extract_string(doc, '$.commit.collection'), '') AS event, count(*) AS cnt
FROM ntext
GROUP BY 1
ORDER BY cnt DESC, event;

-- Q2 — counts and distinct users per collection
SELECT coalesce(json_extract_string(doc, '$.commit.collection'), '') AS event,
       count(*) AS cnt,
       count(DISTINCT json_extract_string(doc, '$.did')) AS users
FROM ntext
WHERE json_extract_string(doc, '$.kind') = 'commit'
  AND json_extract_string(doc, '$.commit.operation') = 'create'
GROUP BY 1
ORDER BY cnt DESC, event;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT coalesce(json_extract_string(doc, '$.commit.collection'), '') AS event,
       hour(make_timestamp(json_extract(doc, '$.time_us')::BIGINT)) AS hour_of_day,
       count(*) AS cnt
FROM ntext
WHERE json_extract_string(doc, '$.kind') = 'commit'
  AND json_extract_string(doc, '$.commit.operation') = 'create'
  AND json_extract_string(doc, '$.commit.collection')
      IN ('app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like')
GROUP BY 1, 2
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT json_extract_string(doc, '$.did') AS user_id,
       min(make_timestamp(json_extract(doc, '$.time_us')::BIGINT)) AS first_post_ts
FROM ntext
WHERE json_extract_string(doc, '$.kind') = 'commit'
  AND json_extract_string(doc, '$.commit.operation') = 'create'
  AND json_extract_string(doc, '$.commit.collection') = 'app.bsky.feed.post'
GROUP BY 1
ORDER BY first_post_ts ASC, user_id
LIMIT 3;

-- Q5 — three longest activity spans
SELECT json_extract_string(doc, '$.did') AS user_id,
       date_diff('millisecond',
                 min(make_timestamp(json_extract(doc, '$.time_us')::BIGINT)),
                 max(make_timestamp(json_extract(doc, '$.time_us')::BIGINT))) AS activity_span
FROM ntext
WHERE json_extract_string(doc, '$.kind') = 'commit'
  AND json_extract_string(doc, '$.commit.operation') = 'create'
  AND json_extract_string(doc, '$.commit.collection') = 'app.bsky.feed.post'
GROUP BY 1
ORDER BY activity_span DESC, user_id
LIMIT 3;

-- U1 — path census with per-path document counts
SELECT regexp_replace(n.fullkey, '\[[0-9]+\]', '[*]', 'g') AS path,
       count(DISTINCT t.rid) AS documents
FROM (SELECT rowid AS rid, doc FROM ntext) t, json_tree(t.doc) n
WHERE n.atom IS NOT NULL
GROUP BY 1
ORDER BY documents DESC, path
LIMIT 20;

-- U2 — path × type census (the polymorphic-path scan)
SELECT regexp_replace(n.fullkey, '\[[0-9]+\]', '[*]', 'g') AS path,
       count(DISTINCT t.rid) AS n
FROM (SELECT rowid AS rid, doc FROM ntext) t, json_tree(t.doc) n
WHERE n.atom IS NOT NULL
GROUP BY 1, n.type
ORDER BY n DESC, path, n.type
LIMIT 20;

-- U3 — value anywhere, exact. No path is named.
SELECT count(DISTINCT t.rid)
FROM (SELECT rowid AS rid, doc FROM ntext) t, json_tree(t.doc) n
WHERE n.type = 'VARCHAR' AND n.atom::VARCHAR = '"did:plc:vwadmn5cx4d2rqxbxbjajzhx"';

-- U4 — subtree prefix census
SELECT regexp_replace(n.fullkey, '\[[0-9]+\]', '[*]', 'g') AS path, count(*) AS occurrences
FROM ntext t, json_tree(t.doc) n
WHERE n.atom IS NOT NULL AND starts_with(n.fullkey, '$.commit.record.embed.')
GROUP BY 1
ORDER BY occurrences DESC, path
LIMIT 10;

-- U5 — sum every integer in the corpus, whatever path it sits at
SELECT sum(TRY_CAST(n.atom AS BIGINT)) AS total
FROM ntext t, json_tree(t.doc) n
WHERE n.type IN ('UBIGINT', 'BIGINT');

-- U6 — leaf count per document, corpus-wide
SELECT count(*) AS leaves FROM ntext t, json_tree(t.doc) n WHERE n.atom IS NOT NULL;

-- U7 — presence of one *constant* path. The case a JSON column is built for.
SELECT count(*) AS documents FROM ntext WHERE json_exists(doc, '$.commit.record.text');

-- U8 — a numeric predicate across every integer-valued path at once
SELECT count(DISTINCT t.rid) AS documents
FROM (SELECT rowid AS rid, doc FROM ntext) t, json_tree(t.doc) n
WHERE n.type IN ('UBIGINT', 'BIGINT') AND TRY_CAST(n.atom AS BIGINT) > 1700000000000000;

-- U9 — array degree for *every* array-valued path in the corpus, discovered
SELECT path, round(avg(n), 3) AS avg_elems, max(n) AS max_elems
FROM (
    SELECT t.rid, regexp_replace(j.fullkey, '\[[0-9]+\]', '[*]', 'g') AS path, count(*) AS n
    FROM (SELECT rowid AS rid, doc FROM ntext) t, json_tree(t.doc) j
    WHERE j.atom IS NOT NULL AND j.fullkey LIKE '%[%'
    GROUP BY 1, 2
) s
GROUP BY path
ORDER BY avg_elems DESC, path
LIMIT 10;
