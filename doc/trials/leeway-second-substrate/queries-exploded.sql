-- Q1-Q5 and U1-U9 against the exploded rendering (arm X): one row per
-- attribute, built by arm-x.sh with ARRAY JOIN over the canonical mapping.
--
-- The point of this file is what is *absent* from it. There is not one array
-- function, not one lambda, and no UDF: every query is ordinary SQL over a
-- (doc, section, path, value) table. That is the whole reason the arm exists —
-- M0 found DataFusion cannot express the packed form at all, and this is the
-- rendering that needs nothing an engine might not have.
--
-- Two shapes recur, and they are the arm's thesis:
--
--   single-path   WHERE path = '...'          -- a sort-key prefix seek
--   multi-path    GROUP BY doc + anyIf(...)   -- reassembly the packed form
--                                             -- gets for free within a row
--
-- Divergence from queries-jsonmap.sql, deliberate and recorded rather than
-- papered over: the packed Q1 emits an empty-string bucket, because ClickHouse
-- `indexOf` returns 0 for an absent path and `arrayElement` at 0 returns the
-- type default, so documents carrying no /commit/collection (kind=account,
-- kind=identity) land in a '' group. Here such documents have no row at all, so
-- the bucket is absent instead of empty. This is the same absent-path
-- divergence M0 found between ClickHouse and DuckDB, now appearing a third way
-- in a third rendering; matching it would mean contriving a UNION against a
-- document universe, which no consumer would write.

-- Q1 - event counts by collection
SELECT sym AS event, count() AS count
FROM attrs
WHERE path = '/commit/collection'
GROUP BY event
ORDER BY count DESC;

-- Q2 - counts and distinct users per collection
SELECT event, count() AS count, uniqExact(did) AS users
FROM (
    SELECT doc,
           anyIf(sym, path = '/commit/collection') AS event,
           anyIf(sym, path = '/kind')              AS kind,
           anyIf(sym, path = '/commit/operation')  AS op,
           anyIf(str, path = '/did')               AS did
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/did')
    GROUP BY doc
)
WHERE kind = 'commit' AND op = 'create'
GROUP BY event
ORDER BY count DESC;

-- Q3 - hour-of-day histogram for post / repost / like
SELECT event, toHour(fromUnixTimestamp64Micro(ts)) AS hour_of_day, count() AS count
FROM (
    SELECT doc,
           anyIf(sym, path = '/commit/collection') AS event,
           anyIf(sym, path = '/kind')              AS kind,
           anyIf(sym, path = '/commit/operation')  AS op,
           anyIf(i64, path = '/time_us')           AS ts
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/time_us')
    GROUP BY doc
)
WHERE kind = 'commit' AND op = 'create'
  AND event IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 - three earliest posters
SELECT did AS user_id, min(fromUnixTimestamp64Micro(ts)) AS first_post_ts
FROM (
    SELECT doc,
           anyIf(sym, path = '/commit/collection') AS event,
           anyIf(sym, path = '/kind')              AS kind,
           anyIf(sym, path = '/commit/operation')  AS op,
           anyIf(str, path = '/did')               AS did,
           anyIf(i64, path = '/time_us')           AS ts
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/did', '/time_us')
    GROUP BY doc
)
WHERE kind = 'commit' AND op = 'create' AND event = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 - three longest activity spans
SELECT did AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(ts)),
                 max(fromUnixTimestamp64Micro(ts))) AS activity_span
FROM (
    SELECT doc,
           anyIf(sym, path = '/commit/collection') AS event,
           anyIf(sym, path = '/kind')              AS kind,
           anyIf(sym, path = '/commit/operation')  AS op,
           anyIf(str, path = '/did')               AS did,
           anyIf(i64, path = '/time_us')           AS ts
    FROM attrs
    WHERE path IN ('/commit/collection', '/kind', '/commit/operation', '/did', '/time_us')
    GROUP BY doc
)
WHERE kind = 'commit' AND op = 'create' AND event = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;

-- U1 - path census with per-path document counts
SELECT path, uniqExact(doc) AS documents
FROM attrs
GROUP BY path
ORDER BY documents DESC
LIMIT 20;

-- U2 - path x type census (the polymorphic-path scan).
-- Projects `path, n` and not `section`, matching queries-usp-leeway.sql: it
-- groups by both but selects only the path, so a polymorphic path appears once
-- per section it occurs in.
SELECT path, uniqExact(doc) AS n
FROM attrs
GROUP BY path, section
HAVING n > 0
ORDER BY n DESC
LIMIT 20;

-- U3 - value anywhere, exact. No path is named.
SELECT uniqExact(doc)
FROM attrs
WHERE str = 'did:plc:vwadmn5cx4d2rqxbxbjajzhx';

-- U4 - subtree prefix census
SELECT path, count() AS occurrences
FROM attrs
WHERE startsWith(path, '/commit/record/embed/')
GROUP BY path
ORDER BY occurrences DESC
LIMIT 10;

-- U5 - sum every integer in the corpus, whatever path it sits at
SELECT sum(i64) AS total
FROM attrs
WHERE section = 'int64';

-- U6 - leaf count per document, corpus-wide. Every row is one leaf.
SELECT count() AS leaves
FROM attrs;

-- U7 - presence of one *constant* path
SELECT uniqExact(doc) AS documents
FROM attrs
WHERE path = '/commit/record/text';

-- U8 - a numeric predicate across every integer-valued path at once
SELECT uniqExact(doc) AS documents
FROM attrs
WHERE section = 'int64' AND i64 > 1700000000000000;

-- U9 - array degree for *every* array-valued path in the corpus, discovered
SELECT path, round(avg(n), 3) AS avg_elems, max(n) AS max_elems
FROM (
    SELECT doc, path, count() AS n
    FROM attrs
    WHERE position(path, '/_') > 0
    GROUP BY doc, path
)
GROUP BY path
ORDER BY avg_elems DESC
LIMIT 10;
