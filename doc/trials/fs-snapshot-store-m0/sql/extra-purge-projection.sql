-- The per-mount purge of SD1 (a lightweight DELETE), and what a projection
-- does to it. Not one of the eleven checks; run because SD10's fleet profile
-- proposes a by_path projection and SD1 promises the purge.
DELETE FROM boxerm0.fsmeta WHERE "id:id:u64:47::0:" = 1000005;
SELECT count() FROM boxerm0.fsmeta WHERE "id:id:u64:47::0:" = 1000005;

ALTER TABLE boxerm0.fsmeta ADD PROJECTION by_path
  (SELECT * ORDER BY "id:naturalKey:y:4::0:", "id:id:u64:47::0:", "ts:ts:z64:47::0:");
ALTER TABLE boxerm0.fsmeta MATERIALIZE PROJECTION by_path;
-- Throws: DELETE is not allowed while a projection exists and
-- lightweight_mutation_projection_mode is THROW. Passing the setting on the
-- statement does not help — getSetting() reads back 'rebuild' and the guard
-- still fires. The TABLE-level setting is what it reads.
DELETE FROM boxerm0.fsmeta WHERE "id:id:u64:47::0:" = 1000006;
ALTER TABLE boxerm0.fsmeta MODIFY SETTING lightweight_mutation_projection_mode = 'rebuild';
DELETE FROM boxerm0.fsmeta WHERE "id:id:u64:47::0:" = 1000006;
SELECT count() FROM boxerm0.fsmeta WHERE "id:id:u64:47::0:" = 1000006;
