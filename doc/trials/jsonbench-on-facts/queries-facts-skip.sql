-- Arm C variant of queries-facts.sql: identical queries, each with a redundant
-- has()/hasAny() conjunct that a bloom_filter skipping index can serve.
--
-- The conjunct is redundant for correctness — LEEWAY_VALUE_BY_TAG_EQUAL still
-- decides which rows qualify — and exists only to give the index an expression
-- it can prune on. Q1 has no filter and Q2 filters on near-universal values, so
-- neither gains a conjunct; only Q3, Q4 and Q5 do. See arm-c.sh for why the
-- value-by-tag form itself cannot be indexed.

-- Q1 — event counts by collection
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) AS event,
       count() AS count
FROM facts
GROUP BY event
ORDER BY count DESC;

-- Q2 — counts and distinct users per collection
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     coGather(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
              raggedStarts(`tv:stringArray:len:len:u64:28o:0:0:0::data`)) AS strVals,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data`      AS strTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strIdx
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
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     coGather(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
              raggedStarts(`tv:i64Array:len:len:u64:28o:0:0:0::data`)) AS intVals,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data`         AS intTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) AS event,
       toHour(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx))) AS hour_of_day,
       count() AS count
FROM facts
WHERE hasAny(symVals, ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like'])
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symIdx) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symIdx) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx)
      IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     coGather(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
              raggedStarts(`tv:stringArray:len:len:u64:28o:0:0:0::data`)) AS strVals,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data`      AS strTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strIdx,
     coGather(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
              raggedStarts(`tv:i64Array:len:len:u64:28o:0:0:0::data`)) AS intVals,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data`         AS intTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(strVals, strTags, '/did', strIdx)::String AS user_id,
       min(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx))) AS first_post_ts
FROM facts
WHERE has(symVals, 'app.bsky.feed.post')
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symIdx) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symIdx) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 — three longest activity spans
WITH `tv:symbol:value:val:s:m:0:24:0::data`          AS symVals,
     `tv:symbol:mrhp:mrhp:y:g:0:0:0::data`           AS symTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS symIdx,
     coGather(`tv:stringArray:value:val:sh:g:0:x2:0::data`,
              raggedStarts(`tv:stringArray:len:len:u64:28o:0:0:0::data`)) AS strVals,
     `tv:stringArray:mrhp:mrhp:y:g:0:0:0::data`      AS strTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS strIdx,
     coGather(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
              raggedStarts(`tv:i64Array:len:len:u64:28o:0:0:0::data`)) AS intVals,
     `tv:i64Array:mrhp:mrhp:y:g:0:0:0::data`         AS intTags,
     LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data`) AS intIdx
SELECT LEEWAY_VALUE_BY_TAG_EQUAL(strVals, strTags, '/did', strIdx)::String AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx))),
                 max(fromUnixTimestamp64Micro(LEEWAY_VALUE_BY_TAG_EQUAL(intVals, intTags, '/time_us', intIdx)))) AS activity_span
FROM facts
WHERE has(symVals, 'app.bsky.feed.post')
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symIdx) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symIdx) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symIdx) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
