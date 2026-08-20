-- Check 4 — MATERIALIZED tree columns added by ALTER after EnsureTable.
--
-- Four questions: does the ALTER take on a facts-shaped table; may a
-- MATERIALIZED column depend on another (`dir` uses `name`); are they hidden
-- from SELECT * (the store decodes positionally); does a bloom filter over
-- the materialised `dir` prune granules.
--
-- The `ext` expression is NOT the one the compact page drafted. That one gave
-- the root row '.' and read '.gitignore' as its own extension; this one treats
-- a leading dot as part of the name, so `.gitignore` has no extension and
-- `.hidden.txt` has `.txt`.

ALTER TABLE boxerm0.fsmeta
    ADD COLUMN IF NOT EXISTS name  String MATERIALIZED splitByChar('/', "id:naturalKey:y:4::0:")[-1],
    ADD COLUMN IF NOT EXISTS dir   String MATERIALIZED multiIf("id:naturalKey:y:4::0:" = '.', '',
                                        position("id:naturalKey:y:4::0:", '/') = 0, '.',
                                        substring("id:naturalKey:y:4::0:", 1,
                                                  length("id:naturalKey:y:4::0:") - length(name) - 1)),
    ADD COLUMN IF NOT EXISTS depth UInt16 MATERIALIZED if("id:naturalKey:y:4::0:" = '.', 0,
                                        length(splitByChar('/', "id:naturalKey:y:4::0:"))),
    ADD COLUMN IF NOT EXISTS ext   LowCardinality(String) MATERIALIZED
                                        if(position(substring(name, 2), '.') = 0, '',
                                           concat('.', splitByChar('.', name)[-1]));

ALTER TABLE boxerm0.fsmeta ADD CONSTRAINT IF NOT EXISTS valid_path
    CHECK "id:naturalKey:y:4::0:" = '.'
       OR NOT hasAny(splitByChar('/', "id:naturalKey:y:4::0:"), ['', '.', '..']);
ALTER TABLE boxerm0.fsmeta ADD INDEX IF NOT EXISTS ix_dir dir TYPE bloom_filter GRANULARITY 4;

INSERT INTO boxerm0.fsmeta
  ("id:id:u64:47::0:", "ts:ts:z64:47::0:", "id:naturalKey:y:4::0:", "lc:expiresAt:z64:4::0:")
SELECT 1, toDateTime64('2026-08-19 00:00:00',9,'UTC'), p, toDateTime64('2026-08-27 00:00:00',9,'UTC')
FROM (SELECT arrayJoin(['.', 'a', 'a/b.txt', 'a/c/d.tar.gz', 'top.md', 'a/.gitignore', 'a/.hidden.txt']) AS p);

SELECT "id:naturalKey:y:4::0:" AS path, name, dir, depth, ext
FROM boxerm0.fsmeta WHERE "id:id:u64:47::0:" = 1 ORDER BY path;

-- 189 columns in the table, 185 in SELECT * — the positional decode is intact.
-- (DESCRIBE is not composable as a subquery here, so these are two statements.)
SELECT count() AS in_table FROM system.columns WHERE database = 'boxerm0' AND table = 'fsmeta';
DESCRIBE (SELECT * FROM boxerm0.fsmeta);

-- The four are exactly the MATERIALIZED ones — which is also why the generated
-- VerifySchema fails against this table: it compares DESCRIBE TABLE, and
-- DESCRIBE TABLE lists them (check 5, finding G4).
SELECT name, default_type FROM system.columns
WHERE database = 'boxerm0' AND table = 'fsmeta' AND default_kind != '';

-- Pruning. Fill 200k paths over 64 directories first:
-- INSERT INTO boxerm0.fsmeta ("id:id:u64:47::0:", "ts:ts:z64:47::0:",
--   "id:naturalKey:y:4::0:", "lc:expiresAt:z64:4::0:")
-- SELECT 7, toDateTime64('2026-08-19 12:00:00',9,'UTC'),
--        concat('d', leftPad(toString(number % 64), 2, '0'), '/g', toString(number), '.txt'),
--        toDateTime64('2026-08-27 00:00:00',9,'UTC') FROM numbers(200000);
-- OPTIMIZE TABLE boxerm0.fsmeta FINAL;
EXPLAIN indexes = 1 SELECT count() FROM boxerm0.fsmeta WHERE dir = 'd07';
