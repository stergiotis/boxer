-- M2 — the exploded rendering on DuckDB, Q2-Q5 in the JOIN formulation.
--
-- m1-exploded.duckdb.sql reassembles a document with GROUP BY doc and
-- max(CASE WHEN ...). On ClickHouse that formulation was 2-5x slower than
-- semi-joins that prune on the path sort key, and mistaking one for the other
-- is the error README §4 now forbids. This file is the other formulation, so
-- the exploded rendering is measured at its best known form on every engine
-- rather than at the one I happened to write first.
--
-- Only Q2-Q5 have two natural formulations; Q1 and U1-U9 have one each and are
-- measured from m1-exploded.duckdb.sql.

CREATE OR REPLACE TEMP VIEW attrs AS SELECT * FROM 'exploded.parquet';
-- @@

-- Q2
SELECT event, count(*) AS cnt, count(DISTINCT did) AS users
FROM (
    SELECT c.doc AS doc, c.sym AS event, d.str AS did
    FROM (SELECT doc, sym FROM attrs WHERE path = '/commit/collection') AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, str FROM attrs WHERE path = '/did') AS d ON c.doc = d.doc
) t
GROUP BY event
ORDER BY cnt DESC, event;

-- Q3
SELECT event, hour(make_timestamp(ts)) AS hour_of_day, count(*) AS cnt
FROM (
    SELECT c.sym AS event, t2.i64 AS ts
    FROM (SELECT doc, sym FROM attrs WHERE path = '/commit/collection'
            AND sym IN ('app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like')) AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, i64 FROM attrs WHERE path = '/time_us') AS t2 ON c.doc = t2.doc
) t
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4
SELECT did AS user_id, min(make_timestamp(ts)) AS first_post_ts
FROM (
    SELECT d.str AS did, t2.i64 AS ts
    FROM (SELECT doc FROM attrs WHERE path = '/commit/collection' AND sym = 'app.bsky.feed.post') AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, str FROM attrs WHERE path = '/did') AS d ON c.doc = d.doc
    INNER JOIN (SELECT doc, i64 FROM attrs WHERE path = '/time_us') AS t2 ON c.doc = t2.doc
) t
GROUP BY user_id
ORDER BY first_post_ts ASC, user_id
LIMIT 3;

-- Q5
SELECT did AS user_id,
       date_diff('millisecond', min(make_timestamp(ts)), max(make_timestamp(ts))) AS activity_span
FROM (
    SELECT d.str AS did, t2.i64 AS ts
    FROM (SELECT doc FROM attrs WHERE path = '/commit/collection' AND sym = 'app.bsky.feed.post') AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, str FROM attrs WHERE path = '/did') AS d ON c.doc = d.doc
    INNER JOIN (SELECT doc, i64 FROM attrs WHERE path = '/time_us') AS t2 ON c.doc = t2.doc
) t
GROUP BY user_id
ORDER BY activity_span DESC, user_id
LIMIT 3;
