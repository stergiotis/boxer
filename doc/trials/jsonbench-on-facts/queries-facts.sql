-- Q1-Q5 of JSONBench against the boxer.facts shape produced by
-- `jsonbench ingest` (apps/jsonbench), written in the leeway query vocabulary
-- rather than open-coded lane arithmetic.
--
-- Columns are written as leeway handles — `section:column` (ADR-0116) — not
-- as physical names. `jsonbench resolve` expands them against the target
-- table's schema before execution, which is what measure.sh does; play applies
-- the same pass at StagePreExecute. Support columns resolve alongside value
-- columns, so the membership lanes are reachable by name too.
--
-- Three primitives do the work, and none is this trial's invention. Install
-- them with `jsonbench chpack` plus `readback.HelperUDFsSQL()`.
--
--   RAGGED_PARENT_IDS(card)                            [ADR-0162 pack]
--       membership index -> owning attribute index, from `lmrcard`. Computed
--       once per row and handed to the two lookups below.
--
--   LEEWAY_VALUE_BY_TAG_EQUAL(vals, tags, tag, m2v)    [ADR-0066 read-back]
--       the *scalar* value whose membership equals `tag`. Resolving through
--       m2v rather than assuming the value and tag lanes are co-indexed is
--       what makes it correct here: this table's symbol section holds an
--       attribute with zero memberships on the mrhp channel (the kind tag
--       rides `lr`), so the lanes are NOT aligned and the pack's CO_LOOKUP —
--       which is `lane[indexOf(keys, k)]` — silently returns the wrong
--       attribute for every row.
--
--   LEEWAY_LIST_BY_TAG_EQUAL(flat, len, tags, tag, m2v)   [ADR-0066]
--       the *slice* for `tag` out of an array-valued section, whose values
--       are flattened across attributes with per-attribute lengths in `len`.
--       It combines the membership indirection with the value lane's own
--       raggedness in one call.
--
-- The symbol section is scalar and takes the first form; stringArray and
-- i64Array are array-valued and take the second. `[1]` on those is an
-- explicit choice — /did and /time_us carry exactly one element each — and
-- not the silent truncation an earlier revision of this file had, where
-- CO_GATHER(vals, RAGGED_STARTS(len)) dropped every element after the first
-- without saying so.
--
-- Read discipline: raw append-only reads. Bluesky events are immutable and the
-- upstream benchmark is read-only analytics, so no argMax/workingset collapse
-- is applied.

-- Q1 — event counts by collection
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', RAGGED_PARENT_IDS(`symbol:lmrcard`)) AS event,
       count() AS count
FROM facts
GROUP BY event
ORDER BY count DESC;

-- Q2 — counts and distinct users per collection
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', RAGGED_PARENT_IDS(`symbol:lmrcard`)) AS event,
       count() AS count,
       uniqExact(LEEWAY_LIST_BY_TAG_EQUAL(`stringArray:value`,
                                          `stringArray:len`,
                                          `stringArray:mrhp`, '/did', RAGGED_PARENT_IDS(`stringArray:lmrcard`))[1]) AS users
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
GROUP BY event
ORDER BY count DESC;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', RAGGED_PARENT_IDS(`symbol:lmrcard`)) AS event,
       toHour(fromUnixTimestamp64Micro(
         LEEWAY_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                  `i64Array:len`,
                                  `i64Array:mrhp`, '/time_us', RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1])) AS hour_of_day,
       count() AS count
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', RAGGED_PARENT_IDS(`symbol:lmrcard`))
      IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT LEEWAY_LIST_BY_TAG_EQUAL(`stringArray:value`,
                                `stringArray:len`,
                                `stringArray:mrhp`, '/did', RAGGED_PARENT_IDS(`stringArray:lmrcard`))[1]::String AS user_id,
       min(fromUnixTimestamp64Micro(
         LEEWAY_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                  `i64Array:len`,
                                  `i64Array:mrhp`, '/time_us', RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1])) AS first_post_ts
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 — three longest activity spans
SELECT LEEWAY_LIST_BY_TAG_EQUAL(`stringArray:value`,
                                `stringArray:len`,
                                `stringArray:mrhp`, '/did', RAGGED_PARENT_IDS(`stringArray:lmrcard`))[1]::String AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(
                   LEEWAY_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                            `i64Array:len`,
                                            `i64Array:mrhp`, '/time_us', RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1])),
                 max(fromUnixTimestamp64Micro(
                   LEEWAY_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                            `i64Array:len`,
                                            `i64Array:mrhp`, '/time_us', RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1]))) AS activity_span
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
