-- M4, arm N-struct — DuckDB's schema-on-read inference over the raw corpus.
-- A third shape, which ClickHouse's comparison had no equivalent of.
--
-- What inference produced over 10M Bluesky documents:
--
--   did      VARCHAR
--   time_us  BIGINT
--   kind     VARCHAR
--   commit   STRUCT(rev, operation, collection, rkey,
--                   record MAP(VARCHAR, JSON),      <-- gave up here
--                   cid)
--   identity STRUCT(did, handle, seq, time)
--
-- **Inference types the backbone and abandons the tail.** `commit.record` — the
-- part that actually varies across collections, and the part leeway shreds like
-- any other — comes back as a map of JSON. So this arm is fastest exactly where
-- the paths are stable and stops being a struct exactly where they are not.
--
-- Consequently only six of the fourteen queries have a native expression here,
-- and the file contains only those six. The mapping to the standard numbering:
--
--   1..5 = Q1..Q5   the benchmark set: every path named, all typed columns
--   6    = U7       presence of one constant path
--
-- The eight that are absent, and why — this is the arm's result, not a gap in
-- the effort:
--
--   U1, U2  path census. The paths are in the *schema*, not the data, so there
--           is nothing to aggregate over. Recovering them means re-serialising
--           to JSON and walking it, which is arm N-text with an extra step.
--   U4      subtree prefix census. Same: `commit.record`'s interior is JSON.
--   U3, U5  value-anywhere and sum-every-integer. Every field would have to be
--   U8      named, and the answer would be wrong the moment a document carries
--           a path the inference did not see.
--   U6, U9  leaf count and array degree. Both need to enumerate inside the
--           tail, which is not enumerable as a struct.
--
-- That is the same verdict the USP document records for ClickHouse's JSON type
-- on U5 and U9 ("no expression exists"), reached for a different reason: there
-- it is the type's addressing model, here it is inference declining to type a
-- heterogeneous subtree.

ATTACH 'm4.duckdb' AS m4 (READ_ONLY);
USE m4;
-- @@

-- Q1 — event counts by collection
SELECT coalesce(commit.collection, '') AS event, count(*) AS cnt
FROM nstruct
GROUP BY 1
ORDER BY cnt DESC, event;

-- Q2 — counts and distinct users per collection
SELECT coalesce(commit.collection, '') AS event, count(*) AS cnt, count(DISTINCT did) AS users
FROM nstruct
WHERE kind = 'commit' AND commit.operation = 'create'
GROUP BY 1
ORDER BY cnt DESC, event;

-- Q3 — hour-of-day histogram for post / repost / like
SELECT coalesce(commit.collection, '') AS event,
       hour(make_timestamp(time_us)) AS hour_of_day,
       count(*) AS cnt
FROM nstruct
WHERE kind = 'commit' AND commit.operation = 'create'
  AND commit.collection IN ('app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like')
GROUP BY 1, 2
ORDER BY hour_of_day, event;

-- Q4 — three earliest posters
SELECT did AS user_id, min(make_timestamp(time_us)) AS first_post_ts
FROM nstruct
WHERE kind = 'commit' AND commit.operation = 'create' AND commit.collection = 'app.bsky.feed.post'
GROUP BY 1
ORDER BY first_post_ts ASC, user_id
LIMIT 3;

-- Q5 — three longest activity spans
SELECT did AS user_id,
       date_diff('millisecond', min(make_timestamp(time_us)), max(make_timestamp(time_us))) AS activity_span
FROM nstruct
WHERE kind = 'commit' AND commit.operation = 'create' AND commit.collection = 'app.bsky.feed.post'
GROUP BY 1
ORDER BY activity_span DESC, user_id
LIMIT 3;

-- U7 — presence of one *constant* path. The case this shape is built for, and
-- the only one of the nine USP queries it can answer natively.
SELECT count(*) AS documents FROM nstruct WHERE map_contains(commit.record, 'text');
