-- Check 6 — the materialised view into fssnap under one batched insert
-- carrying many mounts' root rows.
DROP TABLE IF EXISTS boxerm0.fssnap_mv;
CREATE MATERIALIZED VIEW boxerm0.fssnap_mv TO boxerm0.fssnap AS
SELECT * FROM boxerm0.fsmeta WHERE "id:naturalKey:y:4::0:" = '.';

-- one insert, 500 mounts, 10 rows each, every tenth row a root row
INSERT INTO boxerm0.fsmeta
  ("id:id:u64:47::0:", "ts:ts:z64:47::0:", "id:naturalKey:y:4::0:", "lc:expiresAt:z64:4::0:")
SELECT 1000 + intDiv(number, 10),
       toDateTime64('2026-08-19 12:00:00',9,'UTC'),
       if(number % 10 = 0, '.', concat('f', toString(number % 10), '.txt')),
       toDateTime64('2026-08-27 00:00:00',9,'UTC')
FROM numbers(5000);

-- expect: 500 rows in fssnap, one per mount — no loss, no duplication.
SELECT count() AS snap_rows, uniqExact("id:id:u64:47::0:") AS mounts FROM boxerm0.fssnap;
