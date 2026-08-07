-- Arm C variant of queries-facts.sql: identical queries, each with a redundant
-- has()/hasAny() conjunct that a bloom_filter skipping index can serve.
-- Written in leeway column handles like its source; see that file's header.

-- Q1 — event counts by collection
SELECT LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) AS event,
       count() AS count
FROM facts
GROUP BY event
ORDER BY count DESC;

-- Q2 — counts and distinct users per collection
SELECT LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) AS event,
       count() AS count,
       uniqExact(LW_LIST_BY_TAG_EQUAL(`stringArray:value`,
                                          `stringArray:len`,
                                          `stringArray:mrhp`, '/did', LW_RAGGED_PARENT_IDS(`stringArray:lmrcard`))[1]) AS users
FROM facts
WHERE LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
GROUP BY event
ORDER BY count DESC;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) AS event,
       toHour(fromUnixTimestamp64Micro(
         LW_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                  `i64Array:len`,
                                  `i64Array:mrhp`, '/time_us', LW_RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1])) AS hour_of_day,
       count() AS count
FROM facts
WHERE hasAny(`symbol:value`, ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like'])
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`))
      IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like']
GROUP BY event, hour_of_day
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT LW_LIST_BY_TAG_EQUAL(`stringArray:value`,
                                `stringArray:len`,
                                `stringArray:mrhp`, '/did', LW_RAGGED_PARENT_IDS(`stringArray:lmrcard`))[1]::String AS user_id,
       min(fromUnixTimestamp64Micro(
         LW_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                  `i64Array:len`,
                                  `i64Array:mrhp`, '/time_us', LW_RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1])) AS first_post_ts
FROM facts
WHERE has(`symbol:value`, 'app.bsky.feed.post')
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY first_post_ts ASC
LIMIT 3;

-- Q5 — three longest activity spans
SELECT LW_LIST_BY_TAG_EQUAL(`stringArray:value`,
                                `stringArray:len`,
                                `stringArray:mrhp`, '/did', LW_RAGGED_PARENT_IDS(`stringArray:lmrcard`))[1]::String AS user_id,
       date_diff('milliseconds',
                 min(fromUnixTimestamp64Micro(
                   LW_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                            `i64Array:len`,
                                            `i64Array:mrhp`, '/time_us', LW_RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1])),
                 max(fromUnixTimestamp64Micro(
                   LW_LIST_BY_TAG_EQUAL(`i64Array:value`,
                                            `i64Array:len`,
                                            `i64Array:mrhp`, '/time_us', LW_RAGGED_PARENT_IDS(`i64Array:lmrcard`))[1]))) AS activity_span
FROM facts
WHERE has(`symbol:value`, 'app.bsky.feed.post')
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/kind', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'commit'
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/operation', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'create'
  AND LW_VALUE_BY_TAG_EQUAL(`symbol:value`, `symbol:mrhp`, '/commit/collection', LW_RAGGED_PARENT_IDS(`symbol:lmrcard`)) = 'app.bsky.feed.post'
GROUP BY user_id
ORDER BY activity_span DESC
LIMIT 3;
