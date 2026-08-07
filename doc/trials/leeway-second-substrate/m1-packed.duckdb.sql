-- M1 — the packed rendering on DuckDB, higher-order form. Compare against
-- m1-packed.clickhouse.sql.
--
-- Three systematic differences from the oracle, each of them a finding rather
-- than a preference:
--
--   coalesce(...)   ClickHouse `indexOf` returns 0 for an absent path and
--                   `arrayElement` at 0 returns the type's default, so a
--                   document lacking a path lands in the '' bucket. DuckDB's
--                   `list_position` returns NULL and `list[NULL]` is NULL. The
--                   coalesce is written deliberately (M0 finding 1); without it
--                   Q1's empty bucket becomes a NULL bucket and Q2-Q5 silently
--                   drop rows the oracle keeps.
--   lambda x: ...   DuckDB 1.5.5 deprecates the `->` arrow the ClickHouse files
--                   use, with removal announced for the next release. The two
--                   engines cannot share lambda syntax even where the function
--                   names match.
--   make_timestamp  DuckDB timestamps are naive. The oracle is run with
--                   session_timezone=UTC to meet it (H4); without that pin Q3
--                   differs by the server's offset and nothing else.
--
-- The prelude below is this port's answer to ADR-0116 column handles: leeway's
-- physical names are unusable inline, and DuckDB keeps no view across CLI
-- invocations, so the aliases are prepended to every statement.

CREATE OR REPLACE TEMP VIEW j AS
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
SELECT coalesce(sym_val[list_position(sym_lmv, '/commit/collection')], '') AS event,
       count(*) AS cnt
FROM j
GROUP BY event
ORDER BY cnt DESC, event;

-- Q2 — counts and distinct users per collection
SELECT coalesce(sym_val[list_position(sym_lmv, '/commit/collection')], '') AS event,
       count(*) AS cnt,
       count(DISTINCT coalesce(str_val[list_position(str_lmv, '/did')], '')) AS users
FROM j
WHERE coalesce(sym_val[list_position(sym_lmv, '/kind')], '') = 'commit'
  AND coalesce(sym_val[list_position(sym_lmv, '/commit/operation')], '') = 'create'
GROUP BY event
ORDER BY cnt DESC, event;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT coalesce(sym_val[list_position(sym_lmv, '/commit/collection')], '') AS event,
       hour(make_timestamp(coalesce(i64_val[list_position(i64_lmv, '/time_us')], 0))) AS hour_of_day,
       count(*) AS cnt
FROM j
WHERE coalesce(sym_val[list_position(sym_lmv, '/kind')], '') = 'commit'
  AND coalesce(sym_val[list_position(sym_lmv, '/commit/operation')], '') = 'create'
  AND coalesce(sym_val[list_position(sym_lmv, '/commit/collection')], '')
      IN ('app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like')
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT coalesce(str_val[list_position(str_lmv, '/did')], '')::VARCHAR AS user_id,
       min(make_timestamp(coalesce(i64_val[list_position(i64_lmv, '/time_us')], 0))) AS first_post_ts
FROM j
WHERE coalesce(sym_val[list_position(sym_lmv, '/kind')], '') = 'commit'
  AND coalesce(sym_val[list_position(sym_lmv, '/commit/operation')], '') = 'create'
  AND coalesce(sym_val[list_position(sym_lmv, '/commit/collection')], '') = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC, user_id
LIMIT 3;

-- Q5 — three longest activity spans
SELECT coalesce(str_val[list_position(str_lmv, '/did')], '')::VARCHAR AS user_id,
       date_diff('millisecond',
                 min(make_timestamp(coalesce(i64_val[list_position(i64_lmv, '/time_us')], 0))),
                 max(make_timestamp(coalesce(i64_val[list_position(i64_lmv, '/time_us')], 0)))) AS activity_span
FROM j
WHERE coalesce(sym_val[list_position(sym_lmv, '/kind')], '') = 'commit'
  AND coalesce(sym_val[list_position(sym_lmv, '/commit/operation')], '') = 'create'
  AND coalesce(sym_val[list_position(sym_lmv, '/commit/collection')], '') = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC, user_id
LIMIT 3;

-- U1 — path census with per-path document counts
SELECT path, count(*) AS documents
FROM (
    SELECT unnest(list_distinct(flatten([sym_lmv, str_lmv, i64_lmv, f64_lmv, bool_lmv]))) AS path
    FROM j
)
GROUP BY path
ORDER BY documents DESC, path
LIMIT 20;

-- U2 — path × type census (the polymorphic-path scan)
SELECT path, count(*) AS n
FROM (
    SELECT section, unnest(list_distinct(paths)) AS path
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
SELECT count(*) FROM j WHERE list_contains(str_val, 'did:plc:vwadmn5cx4d2rqxbxbjajzhx');

-- U4 — subtree prefix census
SELECT path, count(*) AS occurrences
FROM (
    SELECT unnest(list_filter(flatten([sym_lmv, str_lmv, i64_lmv, bool_lmv]),
                              lambda p: starts_with(p, '/commit/record/embed/'))) AS path
    FROM j
)
GROUP BY path
ORDER BY occurrences DESC, path
LIMIT 10;

-- U5 — sum every integer in the corpus, whatever path it sits at
SELECT sum(list_sum(i64_val)) AS total FROM j;

-- U6 — leaf count per document, corpus-wide
SELECT sum(len(sym_lmv) + len(str_lmv) + len(i64_lmv) + len(f64_lmv) + len(bool_lmv)) AS leaves
FROM j;

-- U7 — presence of one *constant* path
SELECT count(*) AS documents FROM j WHERE list_contains(str_lmv, '/commit/record/text');

-- U8 — a numeric predicate across every integer-valued path at once
SELECT count(*) AS documents
FROM j
WHERE list_bool_or(list_transform(i64_val, lambda v: v > 1700000000000000));

-- U9 — array degree for *every* array-valued path in the corpus, discovered
SELECT pc.path AS path, round(avg(pc.n), 3) AS avg_elems, max(pc.n) AS max_elems
FROM (
    SELECT unnest(list_transform(
               list_distinct(list_filter(paths, lambda p: contains(p, '/_'))),
               lambda k: {'path': k, 'n': len(list_filter(paths, lambda p: p = k))}
           )) AS pc
    FROM (
        SELECT flatten([sym_lmv, str_lmv, i64_lmv, f64_lmv, bool_lmv]) AS paths
        FROM j
    )
)
GROUP BY pc.path
ORDER BY avg_elems DESC, path
LIMIT 10;
