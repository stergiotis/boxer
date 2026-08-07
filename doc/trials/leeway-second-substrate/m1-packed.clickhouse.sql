-- M1 oracle — the packed rendering on ClickHouse, physical column names, with
-- deterministic tiebreaks. Every other M1 file is compared against this one.
--
-- Generated from the sibling trial's queries-jsonmap.sql + queries-usp-leeway.sql
-- via `jsonbench resolve` (ADR-0116 handle expansion), then tiebroken. The
-- resolution is done once and committed rather than run per-comparison, because
-- it goes through a ClickHouse parser and the ported files cannot use it — so
-- the oracle has to meet them at physical names.
--
-- The tiebreaks are the point of this file's existence. U1, U4 and U9 are
-- `LIMIT n` over tied values, and arms X and Y already returned different tied
-- rows *on the same engine*; across three engines the set is unusable as an
-- oracle without a total order. Q1/Q2/Q4/Q5 get one too, cheaply, so that
-- comparison can be by bytes rather than by set.
SELECT "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/collection')] AS event,
       count() AS count
FROM json
GROUP BY event
ORDER BY count DESC, event;
SELECT "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/collection')] AS event,
       count() AS count,
       uniqExact("tv:string:value:val:s:g:0:0:0::"[indexOf("tv:string:lmv:lmv:y:m:0:0:0::", '/did')]) AS users
FROM json
WHERE "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/kind')] = 'commit'
  AND "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/operation')] = 'create'
GROUP BY event
ORDER BY count DESC, event;
SELECT "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/collection')] AS event,
       toHour(fromUnixTimestamp64Micro("tv:int64:value:val:i64:4o:0:0:0::"[indexOf("tv:int64:lmv:lmv:y:m:0:0:0::", '/time_us')])) AS hour_of_day,
       count() AS count
FROM json
WHERE "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/kind')] = 'commit'
  AND "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/operation')] = 'create'
  AND "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/collection')]
      IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;
SELECT "tv:string:value:val:s:g:0:0:0::"[indexOf("tv:string:lmv:lmv:y:m:0:0:0::", '/did')]::String AS user_id,
       min(fromUnixTimestamp64Micro("tv:int64:value:val:i64:4o:0:0:0::"[indexOf("tv:int64:lmv:lmv:y:m:0:0:0::", '/time_us')])) AS first_post_ts
FROM json
WHERE "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/kind')] = 'commit'
  AND "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/operation')] = 'create'
  AND "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/collection')] = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC, user_id
LIMIT 3;
SELECT "tv:string:value:val:s:g:0:0:0::"[indexOf("tv:string:lmv:lmv:y:m:0:0:0::", '/did')]::String AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro("tv:int64:value:val:i64:4o:0:0:0::"[indexOf("tv:int64:lmv:lmv:y:m:0:0:0::", '/time_us')])),
                 max(fromUnixTimestamp64Micro("tv:int64:value:val:i64:4o:0:0:0::"[indexOf("tv:int64:lmv:lmv:y:m:0:0:0::", '/time_us')]))) AS activity_span
FROM json
WHERE "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/kind')] = 'commit'
  AND "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/operation')] = 'create'
  AND "tv:symbol:value:val:s:m:0:0:0::"[indexOf("tv:symbol:lmv:lmv:y:m:0:0:0::", '/commit/collection')] = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC, user_id
LIMIT 3;
SELECT path, count() AS documents
FROM (
    SELECT arrayJoin(arrayDistinct(arrayConcat("tv:symbol:lmv:lmv:y:m:0:0:0::", "tv:string:lmv:lmv:y:m:0:0:0::", "tv:int64:lmv:lmv:y:m:0:0:0::", "tv:float64:lmv:lmv:y:m:0:0:0::", "tv:bool:lmv:lmv:y:m:0:0:0::"))) AS path
    FROM json
)
GROUP BY path ORDER BY documents DESC, path LIMIT 20;
SELECT path, count() AS n
FROM (
    SELECT section, arrayJoin(arrayDistinct(paths)) AS path
    FROM (
        SELECT 'symbol' AS section, "tv:symbol:lmv:lmv:y:m:0:0:0::" AS paths FROM json
        UNION ALL SELECT 'string',  "tv:string:lmv:lmv:y:m:0:0:0::"  FROM json
        UNION ALL SELECT 'int64',   "tv:int64:lmv:lmv:y:m:0:0:0::"   FROM json
        UNION ALL SELECT 'float64', "tv:float64:lmv:lmv:y:m:0:0:0::" FROM json
        UNION ALL SELECT 'bool',    "tv:bool:lmv:lmv:y:m:0:0:0::"    FROM json
    )
)
GROUP BY path, section HAVING n > 0 ORDER BY n DESC, path, section LIMIT 20;
SELECT count() FROM json WHERE has("tv:string:value:val:s:g:0:0:0::", 'did:plc:vwadmn5cx4d2rqxbxbjajzhx');
SELECT path, count() AS occurrences
FROM (
    SELECT arrayJoin(arrayFilter(p -> startsWith(p, '/commit/record/embed/'),
               arrayConcat("tv:symbol:lmv:lmv:y:m:0:0:0::", "tv:string:lmv:lmv:y:m:0:0:0::", "tv:int64:lmv:lmv:y:m:0:0:0::", "tv:bool:lmv:lmv:y:m:0:0:0::"))) AS path
    FROM json
)
GROUP BY path ORDER BY occurrences DESC, path LIMIT 10;
SELECT sum(arraySum("tv:int64:value:val:i64:4o:0:0:0::")) AS total FROM json;
SELECT sum(length("tv:symbol:lmv:lmv:y:m:0:0:0::") + length("tv:string:lmv:lmv:y:m:0:0:0::") + length("tv:int64:lmv:lmv:y:m:0:0:0::")
         + length("tv:float64:lmv:lmv:y:m:0:0:0::") + length("tv:bool:lmv:lmv:y:m:0:0:0::")) AS leaves
FROM json;
SELECT count() AS documents FROM json WHERE has("tv:string:lmv:lmv:y:m:0:0:0::", '/commit/record/text');
SELECT count() AS documents FROM json WHERE arrayExists(v -> v > 1700000000000000, "tv:int64:value:val:i64:4o:0:0:0::");
SELECT path, round(avg(n), 3) AS avg_elems, max(n) AS max_elems
FROM (
    SELECT pc.1 AS path, pc.2 AS n
    FROM (
        SELECT arrayJoin(arrayMap(k -> (k, countEqual(paths, k)),
                   arrayDistinct(arrayFilter(p -> position(p, '/_') > 0, paths)))) AS pc
        FROM (
            SELECT arrayConcat("tv:symbol:lmv:lmv:y:m:0:0:0::", "tv:string:lmv:lmv:y:m:0:0:0::", "tv:int64:lmv:lmv:y:m:0:0:0::", "tv:float64:lmv:lmv:y:m:0:0:0::", "tv:bool:lmv:lmv:y:m:0:0:0::") AS paths
            FROM json
        )
    )
)
GROUP BY path ORDER BY avg_elems DESC, path LIMIT 10;
