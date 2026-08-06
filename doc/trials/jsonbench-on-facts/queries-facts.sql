-- Q1-Q5 of JSONBench against the boxer.facts shape produced by
-- `jsonbench ingest` (apps/jsonbench), written in the leeway query vocabulary
-- rather than open-coded lane arithmetic.
--
-- Two vocabularies do the work, and neither is this trial's invention:
--
--   LEEWAY_VALUE_BY_TAG_EQUAL(vals, tags, tag, membIdxToValIdx)
--       the value whose membership equals `tag`. It resolves in *membership*
--       space — indexOf over the flat tag lane, then back to the owning
--       attribute — so it is correct even when an attribute carries no
--       membership on this channel, which the hand-rolled form was not.
--   RAGGED_PARENT_IDS(card)
--       the membership-index -> attribute-index map, from `lmrcard`. This was
--       LEEWAY_LU_MEMB_IDX_TO_VAL_IDX until ADR-0162 retired it onto the pack;
--       the bodies are identical.
--   CO_GATHER(lane, sel) / RAGGED_STARTS(card)      [ADR-0162 chpack]
--       array-valued sections (stringArray, i64Array) store their values
--       flattened across attributes with per-attribute lengths in `len`, so
--       the value lane does not co-index with the membership lanes.
--       CO_GATHER(vals, RAGGED_STARTS(len)) re-aligns it by taking each
--       attribute's first value.
--
-- The symbol section is scalar and needs no re-alignment; the string and int
-- sections are ragged and do.
--
-- Install the chpack functions with `jsonbench chpack` before running.
--
-- Read discipline: raw append-only reads. Bluesky events are immutable and the
-- upstream benchmark is read-only analytics, so no argMax/workingset collapse
-- is applied.

-- Q1 — event counts by collection
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) AS event,
       count() AS count
FROM facts
GROUP BY event
ORDER BY count DESC;

-- Q2 — counts and distinct users per collection
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     CO_GATHER(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
              RAGGED_STARTS(`tv:stringArray:len:len:u64:28o:0:0:0::data`)) AS strVals,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data`      AS strTags,
     RAGGED_PARENT_IDS(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) AS event,
       count() AS count,
       uniqExact(LEEWAY_VALUE_BY_TAG_EQUAL(strVals, strTags, '/did', strIdx)) AS users
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symIdx) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symIdx) = 'create'
GROUP BY event
ORDER BY count DESC;

-- Q3 — hour-of-day histogram for post / repost / like
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     CO_GATHER(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
              RAGGED_STARTS(`tv:i64Array:len:len:u64:28o:0:0:0::data`)) AS intVals,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data`         AS intTags,
     RAGGED_PARENT_IDS(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) AS event,
       toHour(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx))) AS hour_of_day,
       count() AS count
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symIdx) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symIdx) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx)
      IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     CO_GATHER(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
              RAGGED_STARTS(`tv:stringArray:len:len:u64:28o:0:0:0::data`)) AS strVals,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data`      AS strTags,
     RAGGED_PARENT_IDS(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strIdx,
     CO_GATHER(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
              RAGGED_STARTS(`tv:i64Array:len:len:u64:28o:0:0:0::data`)) AS intVals,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data`         AS intTags,
     RAGGED_PARENT_IDS(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(strVals, strTags, '/did', strIdx)::String AS user_id,
       min(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx))) AS first_post_ts
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symIdx) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symIdx) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 — three longest activity spans
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     RAGGED_PARENT_IDS(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     CO_GATHER(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
              RAGGED_STARTS(`tv:stringArray:len:len:u64:28o:0:0:0::data`)) AS strVals,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data`      AS strTags,
     RAGGED_PARENT_IDS(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strIdx,
     CO_GATHER(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
              RAGGED_STARTS(`tv:i64Array:len:len:u64:28o:0:0:0::data`)) AS intVals,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data`         AS intTags,
     RAGGED_PARENT_IDS(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(strVals, strTags, '/did', strIdx)::String AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx))),
                 max(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx)))) AS activity_span
FROM facts
WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symIdx) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symIdx) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
