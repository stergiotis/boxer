-- Arm C variant of queries-facts.sql: identical queries, each with a redundant
-- has()/hasAny() conjunct that a bloom_filter skipping index can serve.
--
-- The conjunct is redundant for correctness — LEEWAY_VALUE_BY_TAG_EQUAL still
-- decides which rows qualify — and exists only to give the index an expression
-- it can prune on. Q1 has no filter and Q2 filters on near-universal values, so
-- neither gains a conjunct; only Q3, Q4 and Q5 do. See arm-c.sh for why the
-- value-by-tag form itself cannot be indexed.

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
WHERE hasAny(symVals, ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like'])
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symM2V) = 'commit'
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
WHERE has(symVals, 'app.bsky.feed.post')
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symM2V) = 'commit'
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
WHERE has(symVals, 'app.bsky.feed.post')
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/kind', symM2V) = 'commit'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/operation', symM2V) = 'create'
  AND LEEWAY_VALUE_BY_TAG_EQUAL(symVals, symTags, '/commit/collection', symM2V) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
