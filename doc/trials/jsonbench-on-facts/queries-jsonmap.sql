-- Q1-Q5 of JSONBench against the canonical leeway JSON mapping
-- (mapping.LoadJsonMapping), loaded by `jsonbench jsonmap ingest`.
--
-- Columns are leeway handles — `section:column` (ADR-0116) — expanded by
-- `jsonbench resolve` before execution, exactly as measure.sh does for the
-- facts arm.
--
-- The whole read vocabulary here is one built-in function:
--
--     value[indexOf(lmv, '<path>')]
--
-- and that is the point of this arm. In the facts arm the same resolution
-- needs LEEWAY_VALUE_BY_TAG_EQUAL / LEEWAY_LIST_BY_TAG_EQUAL over
-- RAGGED_PARENT_IDS, because facts memberships are Ref-shaped (so the path
-- rides the parameter channel of a synthetic ref, and the membership lane does
-- not co-index with the value lane) and its string/int sections are
-- array-valued (so a second cumulative sum over `len` is needed to find an
-- attribute's value). The canonical mapping has neither property: one verbatim
-- membership per attribute, every section scalar, so `lmv[i]` names `value[i]`
-- and `indexOf` is the whole indirection. No UDF is installed to run this file.
--
-- indexOf returns 0 when a path is absent and ClickHouse's arrayElement
-- returns the type's default for index 0, so a document that does not carry
-- the path contributes an empty string / zero rather than an error. Q1's empty
-- bucket is exactly that: `kind=account` and `kind=identity` events carry no
-- /commit/collection.
--
-- Read discipline: raw append-only reads, matching queries-facts.sql — Bluesky
-- events are immutable and the upstream benchmark is read-only analytics.

-- Q1 — event counts by collection
SELECT `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] AS event,
       count() AS count
FROM json
GROUP BY event
ORDER BY count DESC;

-- Q2 — counts and distinct users per collection
SELECT `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] AS event,
       count() AS count,
       uniqExact(`string:value`[indexOf(`string:lmv`, '/did')]) AS users
FROM json
WHERE `symbol:value`[indexOf(`symbol:lmv`, '/kind')] = 'commit'
  AND `symbol:value`[indexOf(`symbol:lmv`, '/commit/operation')] = 'create'
GROUP BY event
ORDER BY count DESC;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] AS event,
       toHour(fromUnixTimestamp64Micro(`int64:value`[indexOf(`int64:lmv`, '/time_us')])) AS hour_of_day,
       count() AS count
FROM json
WHERE `symbol:value`[indexOf(`symbol:lmv`, '/kind')] = 'commit'
  AND `symbol:value`[indexOf(`symbol:lmv`, '/commit/operation')] = 'create'
  AND `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')]
      IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT `string:value`[indexOf(`string:lmv`, '/did')]::String AS user_id,
       min(fromUnixTimestamp64Micro(`int64:value`[indexOf(`int64:lmv`, '/time_us')])) AS first_post_ts
FROM json
WHERE `symbol:value`[indexOf(`symbol:lmv`, '/kind')] = 'commit'
  AND `symbol:value`[indexOf(`symbol:lmv`, '/commit/operation')] = 'create'
  AND `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 — three longest activity spans
SELECT `string:value`[indexOf(`string:lmv`, '/did')]::String AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(`int64:value`[indexOf(`int64:lmv`, '/time_us')])),
                 max(fromUnixTimestamp64Micro(`int64:value`[indexOf(`int64:lmv`, '/time_us')]))) AS activity_span
FROM json
WHERE `symbol:value`[indexOf(`symbol:lmv`, '/kind')] = 'commit'
  AND `symbol:value`[indexOf(`symbol:lmv`, '/commit/operation')] = 'create'
  AND `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
