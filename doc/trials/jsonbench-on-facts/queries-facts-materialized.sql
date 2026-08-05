-- Arm D variant: the same five queries against the five backbone fields
-- exposed as ClickHouse MATERIALIZED columns, so the membership path is
-- resolved once per part at merge time instead of once per row per query.
-- The column definitions are in arm-d.sh; the reconstruction expression they
-- materialise is exactly the one queries-facts.sql evaluates inline.
SELECT commit_collection AS event, count() AS count FROM facts GROUP BY event ORDER BY count DESC;
SELECT commit_collection AS event, count() AS count, uniqExact(did) AS users FROM facts WHERE kind = 'commit' AND commit_operation = 'create' GROUP BY event ORDER BY count DESC;
SELECT commit_collection AS event, toHour(time_us) AS hour_of_day, count() AS count FROM facts WHERE kind = 'commit' AND commit_operation = 'create' AND commit_collection IN ['app.bsky.feed.post', 'app.bsky.feed.repost', 'app.bsky.feed.like'] GROUP BY event, hour_of_day ORDER BY hour_of_day, event;
SELECT did::String AS user_id, min(time_us) AS first_post_ts FROM facts WHERE kind = 'commit' AND commit_operation = 'create' AND commit_collection = 'app.bsky.feed.post' GROUP BY user_id ORDER BY first_post_ts ASC LIMIT 3;
SELECT did::String AS user_id, date_diff('milliseconds', min(time_us), max(time_us)) AS activity_span FROM facts WHERE kind = 'commit' AND commit_operation = 'create' AND commit_collection = 'app.bsky.feed.post' GROUP BY user_id ORDER BY activity_span DESC LIMIT 3;
