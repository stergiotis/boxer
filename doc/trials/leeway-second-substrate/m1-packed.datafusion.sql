-- M1 — the packed rendering on DataFusion 54.1.0. Compare against
-- m1-packed.clickhouse.sql.
--
-- This file exists to correct an overstatement. M0 established that DataFusion
-- has 433 routines and not one higher-order array function, and it was tempting
-- to conclude that leeway's packed layout is unreadable there. It is not: the
-- *higher-order rendering* is inexpressible, the *layout* reads fine. Two
-- non-higher-order builtins carry it —
--
--   array_element(lane, array_position(paths, '/x'))   the whole read idiom
--   unnest(...)                                        for the path-census set
--
-- plus `array_max` standing in for `arrayExists(v -> v > k)`, which works
-- because that particular predicate is a comparison against the maximum.
--
-- Three dialect obligations, each a finding rather than a preference:
--
--   CAST(... AS BIGINT)  array_position returns unsigned and array_element
--                        accepts signed; the composed form is a planning error
--                        without the cast (M0 finding 3).
--   coalesce(...)        absent paths yield NULL here, as on DuckDB, where
--                        ClickHouse yields the type default (M0 finding 1).
--   row_number() OVER () U9 needs a per-document identity and the packed export
--                        carries none — a document *is* a row, so the row
--                        number is the identity.

CREATE VIEW j AS
SELECT "tv:symbol:value:val:s:m:0:0:0::"  AS sym_val,
       "tv:symbol:lmv:lmv:y:m:0:0:0::"    AS sym_lmv,
       "tv:string:value:val:s:g:0:0:0::"  AS str_val,
       "tv:string:lmv:lmv:y:m:0:0:0::"    AS str_lmv,
       "tv:int64:value:val:i64:4o:0:0:0::" AS i64_val,
       "tv:int64:lmv:lmv:y:m:0:0:0::"     AS i64_lmv,
       "tv:float64:lmv:lmv:y:m:0:0:0::"   AS f64_lmv,
       "tv:bool:lmv:lmv:y:m:0:0:0::"      AS bool_lmv
FROM 'packed.parquet';
-- @@

-- Q1 — event counts by collection
SELECT coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/collection') AS BIGINT)), '') AS event,
       count(*) AS cnt
FROM j
GROUP BY event
ORDER BY cnt DESC, event;

-- Q2 — counts and distinct users per collection
SELECT coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/collection') AS BIGINT)), '') AS event,
       count(*) AS cnt,
       count(DISTINCT coalesce(array_element(str_val, CAST(array_position(str_lmv, '/did') AS BIGINT)), '')) AS users
FROM j
WHERE coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/kind') AS BIGINT)), '') = 'commit'
  AND coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/operation') AS BIGINT)), '') = 'create'
GROUP BY event
ORDER BY cnt DESC, event;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/collection') AS BIGINT)), '') AS event,
       date_part('hour', to_timestamp_micros(coalesce(array_element(i64_val, CAST(array_position(i64_lmv, '/time_us') AS BIGINT)), 0))) AS hour_of_day,
       count(*) AS cnt
FROM j
WHERE coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/kind') AS BIGINT)), '') = 'commit'
  AND coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/operation') AS BIGINT)), '') = 'create'
  AND coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/collection') AS BIGINT)), '')
      IN ('app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like')
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT coalesce(array_element(str_val, CAST(array_position(str_lmv, '/did') AS BIGINT)), '') AS user_id,
       min(to_timestamp_micros(coalesce(array_element(i64_val, CAST(array_position(i64_lmv, '/time_us') AS BIGINT)), 0))) AS first_post_ts
FROM j
WHERE coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/kind') AS BIGINT)), '') = 'commit'
  AND coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/operation') AS BIGINT)), '') = 'create'
  AND coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/collection') AS BIGINT)), '') = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC, user_id
LIMIT 3;

-- Q5 — three longest activity spans
SELECT coalesce(array_element(str_val, CAST(array_position(str_lmv, '/did') AS BIGINT)), '') AS user_id,
       (max(coalesce(array_element(i64_val, CAST(array_position(i64_lmv, '/time_us') AS BIGINT)), 0))
      - min(coalesce(array_element(i64_val, CAST(array_position(i64_lmv, '/time_us') AS BIGINT)), 0))) / 1000 AS activity_span
FROM j
WHERE coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/kind') AS BIGINT)), '') = 'commit'
  AND coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/operation') AS BIGINT)), '') = 'create'
  AND coalesce(array_element(sym_val, CAST(array_position(sym_lmv, '/commit/collection') AS BIGINT)), '') = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC, user_id
LIMIT 3;

-- U1 — path census with per-path document counts
SELECT path, count(*) AS documents
FROM (
    SELECT unnest(array_distinct(array_concat(sym_lmv, str_lmv, i64_lmv, f64_lmv, bool_lmv))) AS path
    FROM j
)
GROUP BY path
ORDER BY documents DESC, path
LIMIT 20;

-- U2 — path × type census
SELECT path, count(*) AS n
FROM (
    SELECT section, unnest(array_distinct(paths)) AS path
    FROM (
        SELECT 'symbol' AS section, sym_lmv  AS paths FROM j
        UNION ALL SELECT 'string',  str_lmv  FROM j
        UNION ALL SELECT 'int64',   i64_lmv  FROM j
        UNION ALL SELECT 'float64', f64_lmv  FROM j
        UNION ALL SELECT 'bool',    bool_lmv FROM j
    )
)
GROUP BY path, section
HAVING count(*) > 0
ORDER BY n DESC, path, section
LIMIT 20;

-- U3 — value anywhere, exact. No path is named.
SELECT count(*) FROM j WHERE array_has(str_val, 'did:plc:vwadmn5cx4d2rqxbxbjajzhx');

-- U4 — subtree prefix census
SELECT path, count(*) AS occurrences
FROM (
    SELECT unnest(array_concat(sym_lmv, str_lmv, i64_lmv, bool_lmv)) AS path
    FROM j
)
WHERE starts_with(path, '/commit/record/embed/')
GROUP BY path
ORDER BY occurrences DESC, path
LIMIT 10;

-- U5 — sum every integer in the corpus, whatever path it sits at
SELECT sum(v) AS total FROM (SELECT unnest(i64_val) AS v FROM j);

-- U6 — leaf count per document, corpus-wide
SELECT sum(coalesce(array_length(sym_lmv), 0) + coalesce(array_length(str_lmv), 0)
         + coalesce(array_length(i64_lmv), 0) + coalesce(array_length(f64_lmv), 0)
         + coalesce(array_length(bool_lmv), 0)) AS leaves
FROM j;

-- U7 — presence of one *constant* path
SELECT count(*) AS documents FROM j WHERE array_has(str_lmv, '/commit/record/text');

-- U8 — a numeric predicate across every integer-valued path at once.
-- `array_max(x) > k` is `arrayExists(v -> v > k, x)` for this predicate shape.
SELECT count(*) AS documents FROM j WHERE array_max(i64_val) > 1700000000000000;

-- U9 — array degree for *every* array-valued path in the corpus, discovered
SELECT path, round(avg(n), 3) AS avg_elems, max(n) AS max_elems
FROM (
    SELECT doc, path, count(*) AS n
    FROM (
        SELECT doc, unnest(paths) AS path
        FROM (
            SELECT row_number() OVER () AS doc,
                   array_concat(sym_lmv, str_lmv, i64_lmv, f64_lmv, bool_lmv) AS paths
            FROM j
        )
    )
    WHERE strpos(path, '/_') > 0
    GROUP BY doc, path
)
GROUP BY path
ORDER BY avg_elems DESC, path
LIMIT 10;
