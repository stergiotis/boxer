-- Check 2 — ttl_only_drop_parts = 1: do regular merges still remove expired
-- rows, and what is the drop latency?
--
-- Two arms. The first is the store's own shape: a partition whose rows all
-- expire together (PARTITION BY expiry day). The second is the shape the
-- store must never produce: a part holding expired and live rows at once.

DROP TABLE IF EXISTS boxerm0.t_ttl_wholeday;
CREATE TABLE boxerm0.t_ttl_wholeday (id UInt64, e DateTime64(9,'UTC'), v String)
ENGINE = MergeTree() PARTITION BY toYYYYMMDD(e) ORDER BY id TTL e
SETTINGS ttl_only_drop_parts = 1, merge_with_ttl_timeout = 1;
INSERT INTO boxerm0.t_ttl_wholeday SELECT number, toDateTime64('2020-01-01 00:00:00',9,'UTC'), 'old' FROM numbers(1000);
INSERT INTO boxerm0.t_ttl_wholeday SELECT number, toDateTime64('2030-01-01 00:00:00',9,'UTC'), 'new' FROM numbers(1000);
-- expect: the expired partition's part is replaced by an empty one, the live
-- partition untouched.
SELECT partition, name, rows, active FROM system.parts
WHERE database = 'boxerm0' AND table = 't_ttl_wholeday' ORDER BY partition, name;

DROP TABLE IF EXISTS boxerm0.t_ttl_partial;
CREATE TABLE boxerm0.t_ttl_partial (id UInt64, e DateTime64(9,'UTC'), v String)
ENGINE = MergeTree() PARTITION BY tuple() ORDER BY id TTL e
SETTINGS ttl_only_drop_parts = 1, merge_with_ttl_timeout = 1;
INSERT INTO boxerm0.t_ttl_partial
SELECT number, if(number < 500, toDateTime64('2020-01-01 00:00:00',9,'UTC'),
                                toDateTime64('2030-01-01 00:00:00',9,'UTC')), 'x'
FROM numbers(1000);
-- expect after a few seconds: still 1000 — a background merge does NOT remove
-- expired rows from a partially expired part under ttl_only_drop_parts = 1.
-- OPTIMIZE TABLE boxerm0.t_ttl_partial FINAL then leaves 500.
SELECT count() FROM boxerm0.t_ttl_partial;

-- The control: the same table with ttl_only_drop_parts = 0 drops to 500 on
-- the background merge.
DROP TABLE IF EXISTS boxerm0.t_ttl_partial0;
CREATE TABLE boxerm0.t_ttl_partial0 (id UInt64, e DateTime64(9,'UTC'), v String)
ENGINE = MergeTree() PARTITION BY tuple() ORDER BY id TTL e
SETTINGS ttl_only_drop_parts = 0, merge_with_ttl_timeout = 1;
INSERT INTO boxerm0.t_ttl_partial0
SELECT number, if(number < 500, toDateTime64('2020-01-01 00:00:00',9,'UTC'),
                                toDateTime64('2030-01-01 00:00:00',9,'UTC')), 'x'
FROM numbers(1000);
SELECT count() FROM boxerm0.t_ttl_partial0;

-- The latency knob, for the record.
SELECT name, value FROM system.merge_tree_settings WHERE name = 'merge_with_ttl_timeout';
