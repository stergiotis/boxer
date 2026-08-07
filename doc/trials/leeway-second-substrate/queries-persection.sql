-- Q1-Q5 and U1-U9 against arm Y: one table per section, no discriminator
-- column and no unused value lanes. Built by arm-y.sh.
--
-- Two shapes change from queries-exploded.sql, and they are the arm's point:
--
--   single-section  reads the section's own table, no predicate at all
--                   (U5 becomes `SELECT sum(value) FROM int64`)
--   cross-section   becomes a JOIN, where arm X could co-locate every section
--                   in one GROUP BY doc
--
-- The Q2-Q5 forms below are joins rather than a UNION-then-regroup, because a
-- union would put the tagged union back at query time and defeat the layout.
-- Written as semi-joins for the filters and inner joins for the projections,
-- which is what the per-section split makes natural: each leg is a path-pruned
-- scan on the sort key, and only surviving documents are carried forward. That
-- is a genuinely different plan from arm X's 10M-group reassembly, not a
-- transcription of it.
--
-- U1/U2/U4/U6/U9 need UNION ALL across the section tables. Note what that
-- costs a reader: the union can only be written by someone who knows the
-- section roster, and the roster here is fixed by which sections the data
-- populates, not by the mapping.

-- Q1 - event counts by collection
SELECT value AS event, count() AS count
FROM symbol
WHERE path = '/commit/collection'
GROUP BY event
ORDER BY count DESC;

-- Q2 - counts and distinct users per collection
SELECT event, count() AS count, uniqExact(did) AS users
FROM (
    SELECT c.doc AS doc, c.value AS event, d.value AS did
    FROM (SELECT doc, value FROM symbol WHERE path = '/commit/collection') AS c
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/kind' AND value = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/commit/operation' AND value = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, value FROM string WHERE path = '/did') AS d ON c.doc = d.doc
)
GROUP BY event
ORDER BY count DESC;

-- Q3 - hour-of-day histogram for post / repost / like
SELECT event, toHour(fromUnixTimestamp64Micro(ts)) AS hour_of_day, count() AS count
FROM (
    SELECT c.value AS event, t.value AS ts
    FROM (SELECT doc, value FROM symbol WHERE path = '/commit/collection'
            AND value IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']) AS c
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/kind' AND value = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/commit/operation' AND value = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, value FROM int64 WHERE path = '/time_us') AS t ON c.doc = t.doc
)
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 - three earliest posters
SELECT did AS user_id, min(fromUnixTimestamp64Micro(ts)) AS first_post_ts
FROM (
    SELECT d.value AS did, t.value AS ts
    FROM (SELECT doc FROM symbol WHERE path = '/commit/collection' AND value = 'app.bsky.feed.post') AS c
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/kind' AND value = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/commit/operation' AND value = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, value FROM string WHERE path = '/did') AS d ON c.doc = d.doc
    INNER JOIN (SELECT doc, value FROM int64 WHERE path = '/time_us') AS t ON c.doc = t.doc
)
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 - three longest activity spans
SELECT did AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(ts)),
                 max(fromUnixTimestamp64Micro(ts))) AS activity_span
FROM (
    SELECT d.value AS did, t.value AS ts
    FROM (SELECT doc FROM symbol WHERE path = '/commit/collection' AND value = 'app.bsky.feed.post') AS c
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/kind' AND value = 'commit') AS k ON c.doc = k.doc
    INNER JOIN (SELECT doc FROM symbol WHERE path = '/commit/operation' AND value = 'create') AS o ON c.doc = o.doc
    INNER JOIN (SELECT doc, value FROM string WHERE path = '/did') AS d ON c.doc = d.doc
    INNER JOIN (SELECT doc, value FROM int64 WHERE path = '/time_us') AS t ON c.doc = t.doc
)
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;

-- U1 - path census with per-path document counts
SELECT path, uniqExact(doc) AS documents
FROM (
    SELECT doc, path FROM symbol
    UNION ALL SELECT doc, path FROM string
    UNION ALL SELECT doc, path FROM int64
    UNION ALL SELECT doc, path FROM bool
)
GROUP BY path
ORDER BY documents DESC
LIMIT 20;

-- U2 - path x type census. The section is a per-branch constant here rather
-- than a stored column: the layout moved it from the data into the query.
SELECT path, uniqExact(doc) AS n
FROM (
    SELECT doc, path, 'symbol' AS section FROM symbol
    UNION ALL SELECT doc, path, 'string'  FROM string
    UNION ALL SELECT doc, path, 'int64'   FROM int64
    UNION ALL SELECT doc, path, 'bool'    FROM bool
)
GROUP BY path, section
HAVING n > 0
ORDER BY n DESC
LIMIT 20;

-- U3 - value anywhere, exact. No path is named, and no section predicate is
-- needed either: a string value can only be in the string table.
SELECT uniqExact(doc)
FROM string
WHERE value = 'did:plc:vwadmn5cx4d2rqxbxbjajzhx';

-- U4 - subtree prefix census
SELECT path, count() AS occurrences
FROM (
    SELECT doc, path FROM symbol
    UNION ALL SELECT doc, path FROM string
    UNION ALL SELECT doc, path FROM int64
    UNION ALL SELECT doc, path FROM bool
)
WHERE startsWith(path, '/commit/record/embed/')
GROUP BY path
ORDER BY occurrences DESC
LIMIT 10;

-- U5 - sum every integer in the corpus. No predicate: the table is the answer.
SELECT sum(value) AS total
FROM int64;

-- U6 - leaf count, corpus-wide
SELECT (SELECT count() FROM symbol) + (SELECT count() FROM string)
     + (SELECT count() FROM int64)  + (SELECT count() FROM bool) AS leaves;

-- U7 - presence of one *constant* path
SELECT uniqExact(doc) AS documents
FROM string
WHERE path = '/commit/record/text';

-- U8 - a numeric predicate across every integer-valued path at once
SELECT uniqExact(doc) AS documents
FROM int64
WHERE value > 1700000000000000;

-- U9 - array degree for *every* array-valued path in the corpus, discovered
SELECT path, round(avg(n), 3) AS avg_elems, max(n) AS max_elems
FROM (
    SELECT doc, path, count() AS n
    FROM (
        SELECT doc, path FROM symbol
        UNION ALL SELECT doc, path FROM string
        UNION ALL SELECT doc, path FROM int64
        UNION ALL SELECT doc, path FROM bool
    )
    WHERE position(path, '/_') > 0
    GROUP BY doc, path
)
GROUP BY path
ORDER BY avg_elems DESC
LIMIT 10;
