-- Arm X, the tagged-union table, with arm Y's join formulation: semi-joins for
-- the filters, inner joins for the projections. This file isolates
-- "join vs GROUP BY doc" from "per-section vs one table", and it is the
-- evidence behind the retraction in the 2026-08-07 arm Y logbook entry: run
-- against arm X it lands on arm Y's numbers, not arm X's, so the Q2-Q5 gain
-- belonged to the formulation and not to the layout.
--
-- Only Q2-Q5 have two natural formulations. Q1 and U1-U9 have one each and
-- live in queries-exploded.sql.
SELECT event, count() AS count, uniqExact(did) AS users
FROM (
    SELECT c.doc AS doc, c.sym AS event, d.str AS did
    FROM (SELECT doc, sym FROM attrs WHERE path = '/commit/collection') AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, str FROM attrs WHERE path = '/did') AS d ON c.doc = d.doc
)
GROUP BY event
ORDER BY count DESC;

SELECT event, toHour(fromUnixTimestamp64Micro(ts)) AS hour_of_day, count() AS count
FROM (
    SELECT c.sym AS event, t.i64 AS ts
    FROM (SELECT doc, sym FROM attrs WHERE path = '/commit/collection'
            AND sym IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']) AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, i64 FROM attrs WHERE path = '/time_us') AS t ON c.doc = t.doc
)
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

SELECT did AS user_id, min(fromUnixTimestamp64Micro(ts)) AS first_post_ts
FROM (
    SELECT d.str AS did, t.i64 AS ts
    FROM (SELECT doc FROM attrs WHERE path = '/commit/collection' AND sym = 'app.bsky.feed.post') AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, str FROM attrs WHERE path = '/did') AS d ON c.doc = d.doc
    INNER JOIN (SELECT doc, i64 FROM attrs WHERE path = '/time_us') AS t ON c.doc = t.doc
)
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

SELECT did AS user_id,
       date_diff('milliseconds', min(fromUnixTimestamp64Micro(ts)), max(fromUnixTimestamp64Micro(ts))) AS activity_span
FROM (
    SELECT d.str AS did, t.i64 AS ts
    FROM (SELECT doc FROM attrs WHERE path = '/commit/collection' AND sym = 'app.bsky.feed.post') AS c
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/kind' AND sym = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM attrs WHERE path = '/commit/operation' AND sym = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, str FROM attrs WHERE path = '/did') AS d ON c.doc = d.doc
    INNER JOIN (SELECT doc, i64 FROM attrs WHERE path = '/time_us') AS t ON c.doc = t.doc
)
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
