-- Check 3 — a bare DateTime64(9) plain in TTL beside PARTITION BY on the same
-- plain, and now() inside a ROW POLICY condition.
DROP TABLE IF EXISTS boxerm0.t_ttl_dt64;
CREATE TABLE boxerm0.t_ttl_dt64 (id UInt64, e DateTime64(9,'UTC'), v String)
ENGINE = MergeTree() PARTITION BY toYYYYMMDD(e) ORDER BY id TTL e
SETTINGS ttl_only_drop_parts = 1;

DROP ROW POLICY IF EXISTS m0_pol ON boxerm0.t_ttl_dt64;
CREATE ROW POLICY m0_pol ON boxerm0.t_ttl_dt64 FOR SELECT USING e > now() TO ALL;
INSERT INTO boxerm0.t_ttl_dt64 VALUES
  (1, toDateTime64('2030-01-01 00:00:00',9,'UTC'), 'future'),
  (2, toDateTime64('2020-01-01 00:00:00',9,'UTC'), 'past');
-- expect: one row, 'future'.
SELECT id, v FROM boxerm0.t_ttl_dt64 ORDER BY id;
DROP ROW POLICY m0_pol ON boxerm0.t_ttl_dt64;
