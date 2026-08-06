-- Q1-Q5 of JSONBench against the boxer.facts shape produced by
-- `jsonbench ingest` (apps/jsonbench), written in the leeway query vocabulary
-- rather than open-coded lane arithmetic.
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
WITH `tv:symbol:value:val:s:m:0:24:0::data`  AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`   AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symM2V
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symM2V) AS event,
       count() AS count
FROM facts
GROUP BY event
ORDER BY count DESC;

-- Q2 — counts and distinct users per collection
WITH `tv:symbol:value:val:s:m:0:24:0::data`  AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`   AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symM2V,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data` AS strTags,
     RAGGED_PARENT_IDS(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strM2V
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symM2V) AS event,
       count() AS count,
       uniqExact(LEEWAY_LIST_BY_TAG_EQUAL(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
                                          `tv:stringArray:len:len:u64:28o:0:0:0::data`,
                                          strTags, '/did', strM2V)[1]) AS users
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symM2V) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symM2V) = 'create'
GROUP BY event
ORDER BY count DESC;

-- Q3 — hour-of-day histogram for post / repost / like
WITH `tv:symbol:value:val:s:m:0:24:0::data`  AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`   AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symM2V,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data` AS intTags,
     RAGGED_PARENT_IDS(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intM2V
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symM2V) AS event,
       toHour(fromUnixTimestamp64Micro(
         LEEWAY_LIST_BY_TAG_EQUAL(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
                                  `tv:i64Array:len:len:u64:28o:0:0:0::data`,
                                  intTags, '/time_us', intM2V)[1])) AS hour_of_day,
       count() AS count
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symM2V) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symM2V) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symM2V)
      IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
WITH `tv:symbol:value:val:s:m:0:24:0::data`  AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`   AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symM2V,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data` AS strTags,
     RAGGED_PARENT_IDS(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strM2V,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data` AS intTags,
     RAGGED_PARENT_IDS(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intM2V
SELECT LEEWAY_LIST_BY_TAG_EQUAL(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
                                `tv:stringArray:len:len:u64:28o:0:0:0::data`,
                                strTags, '/did', strM2V)[1]::String AS user_id,
       min(fromUnixTimestamp64Micro(
         LEEWAY_LIST_BY_TAG_EQUAL(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
                                  `tv:i64Array:len:len:u64:28o:0:0:0::data`,
                                  intTags, '/time_us', intM2V)[1])) AS first_post_ts
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symM2V) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symM2V) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symM2V) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 — three longest activity spans
WITH `tv:symbol:value:val:s:m:0:24:0::data`  AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`   AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symM2V,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data` AS strTags,
     RAGGED_PARENT_IDS(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strM2V,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data` AS intTags,
     RAGGED_PARENT_IDS(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intM2V
SELECT LEEWAY_LIST_BY_TAG_EQUAL(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
                                `tv:stringArray:len:len:u64:28o:0:0:0::data`,
                                strTags, '/did', strM2V)[1]::String AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(
                   LEEWAY_LIST_BY_TAG_EQUAL(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
                                            `tv:i64Array:len:len:u64:28o:0:0:0::data`,
                                            intTags, '/time_us', intM2V)[1])),
                 max(fromUnixTimestamp64Micro(
                   LEEWAY_LIST_BY_TAG_EQUAL(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
                                            `tv:i64Array:len:len:u64:28o:0:0:0::data`,
                                            intTags, '/time_us', intM2V)[1]))) AS activity_span
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symM2V) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symM2V) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symM2V) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
