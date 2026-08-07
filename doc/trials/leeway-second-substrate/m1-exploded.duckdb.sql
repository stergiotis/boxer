-- M1 — the exploded rendering on DuckDB. Compare against
-- m1-packed.clickhouse.sql; only Q1 is expected to differ, by the absent-path
-- bucket that has no row in an exploded table.
--
-- This file uses no array function, no lambda and no UDF — which is the point.
-- It is nearly identical to m1-exploded.datafusion.sql; the two diverge only in
-- the temporal functions, and that divergence is the whole dialect cost of the
-- exploded rendering.
--
-- Document reassembly is `max(CASE WHEN path = ... THEN ... END)` rather than
-- ClickHouse's `anyIf`, chosen because both targets have it and it needs no
-- FILTER clause. The mapping is 1:1 per path per document, so max is the value.

CREATE OR REPLACE TEMP VIEW attrs AS SELECT * FROM 'exploded.parquet';
-- @@

-- Q1 — event counts by collection
SELECT sym AS event, count(*) AS cnt
FROM attrs
WHERE path = '/commit/collection'
GROUP BY event
ORDER BY cnt DESC, event;

-- Q2 — counts and distinct users per collection
SELECT event, count(*) AS cnt, count(DISTINCT did) AS users
FROM (
    SELECT doc,
           max(CASE WHEN path = '/commit/collection' THEN sym END) AS event,
           max(CASE WHEN path = '/kind'              THEN sym END) AS kind,
           max(CASE WHEN path = '/commit/operation'  THEN sym END) AS op,
           max(CASE WHEN path = '/did'               THEN str END) AS did
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/did')
    GROUP BY doc
) t
WHERE kind = 'commit' AND op = 'create'
GROUP BY event
ORDER BY cnt DESC, event;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT event, hour(make_timestamp(ts)) AS hour_of_day, count(*) AS cnt
FROM (
    SELECT doc,
           max(CASE WHEN path = '/commit/collection' THEN sym END) AS event,
           max(CASE WHEN path = '/kind'              THEN sym END) AS kind,
           max(CASE WHEN path = '/commit/operation'  THEN sym END) AS op,
           max(CASE WHEN path = '/time_us'           THEN i64 END) AS ts
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/time_us')
    GROUP BY doc
) t
WHERE kind = 'commit' AND op = 'create'
  AND event IN ('app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like')
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT did AS user_id, min(make_timestamp(ts)) AS first_post_ts
FROM (
    SELECT doc,
           max(CASE WHEN path = '/commit/collection' THEN sym END) AS event,
           max(CASE WHEN path = '/kind'              THEN sym END) AS kind,
           max(CASE WHEN path = '/commit/operation'  THEN sym END) AS op,
           max(CASE WHEN path = '/did'               THEN str END) AS did,
           max(CASE WHEN path = '/time_us'           THEN i64 END) AS ts
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/did', '/time_us')
    GROUP BY doc
) t
WHERE kind = 'commit' AND op = 'create' AND event = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC, user_id
LIMIT 3;

-- Q5 — three longest activity spans
SELECT did AS user_id,
       date_diff('millisecond', min(make_timestamp(ts)), max(make_timestamp(ts))) AS activity_span
FROM (
    SELECT doc,
           max(CASE WHEN path = '/commit/collection' THEN sym END) AS event,
           max(CASE WHEN path = '/kind'              THEN sym END) AS kind,
           max(CASE WHEN path = '/commit/operation'  THEN sym END) AS op,
           max(CASE WHEN path = '/did'               THEN str END) AS did,
           max(CASE WHEN path = '/time_us'           THEN i64 END) AS ts
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/did', '/time_us')
    GROUP BY doc
) t
WHERE kind = 'commit' AND op = 'create' AND event = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC, user_id
LIMIT 3;

-- U1 — path census with per-path document counts
SELECT path, count(DISTINCT doc) AS documents
FROM attrs
GROUP BY path
ORDER BY documents DESC, path
LIMIT 20;

-- U2 — path × type census
SELECT path, count(DISTINCT doc) AS n
FROM attrs
GROUP BY path, section
HAVING count(DISTINCT doc) > 0
ORDER BY n DESC, path, section
LIMIT 20;

-- U3 — value anywhere, exact. No path is named.
SELECT count(DISTINCT doc) FROM attrs WHERE str = 'did:plc:vwadmn5cx4d2rqxbxbjajzhx';

-- U4 — subtree prefix census
SELECT path, count(*) AS occurrences
FROM attrs
WHERE starts_with(path, '/commit/record/embed/')
GROUP BY path
ORDER BY occurrences DESC, path
LIMIT 10;

-- U5 — sum every integer in the corpus, whatever path it sits at
SELECT sum(i64) AS total FROM attrs WHERE section = 'int64';

-- U6 — leaf count, corpus-wide. Every row is one leaf.
SELECT count(*) AS leaves FROM attrs;

-- U7 — presence of one *constant* path
SELECT count(DISTINCT doc) AS documents FROM attrs WHERE path = '/commit/record/text';

-- U8 — a numeric predicate across every integer-valued path at once
SELECT count(DISTINCT doc) AS documents
FROM attrs
WHERE section = 'int64' AND i64 > 1700000000000000;

-- U9 — array degree for *every* array-valued path in the corpus, discovered
SELECT path, round(avg(n), 3) AS avg_elems, max(n) AS max_elems
FROM (
    SELECT doc, path, count(*) AS n
    FROM attrs
    WHERE strpos(path, '/_') > 0
    GROUP BY doc, path
) t
GROUP BY path
ORDER BY avg_elems DESC, path
LIMIT 10;
