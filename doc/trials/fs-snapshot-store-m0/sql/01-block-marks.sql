-- Check 1 — one block is one compressed block.
--
-- 200 rows of incompressible bytes per arm, index_granularity = 1, so one
-- row is one block is one granule. mergeTreeIndex(..., with_marks = true)
-- exposes each mark's (offset_in_compressed_file, offset_in_decompressed_block):
-- as many distinct compressed offsets as marks, all at decompressed offset 0,
-- is one compressed block per block.
--
-- Fill (per arm; 1048576 -> 262144 for the 256 KiB arm):
-- INSERT INTO boxerm0.fsdata_facts_1m
--   ("id:id:u64:47::0:", "ts:ts:z64:47::0:", "id:naturalKey:y:4::0:",
--    "lc:expiresAt:z64:4::0:", "tv:blobArray:value:val:yh:4:::0::data")
-- SELECT 1, toDateTime64('2026-08-19 00:00:00',9,'UTC'),
--        concat('big.bin\0', hex(reinterpretAsString(toUInt32(number)))),
--        toDateTime64('2026-08-26 00:00:00',9,'UTC'), [randomString(1048576)]
-- FROM numbers(200) SETTINGS max_insert_block_size = 64;

SELECT 'facts 1 MiB' AS arm, count() AS marks, uniqExact(m.1) AS compressed_blocks,
       countIf(m.2 = 0) AS marks_at_block_start, max(m.2) AS max_offset_in_block
FROM (SELECT `tv%3AblobArray%3Avalue%3Aval%3Ayh%3A4%3A%3A%3A0%3A%3Adata.mark` AS m
      FROM mergeTreeIndex('boxerm0', 'fsdata_facts_1m', with_marks = true))
UNION ALL
SELECT 'facts 256 KiB', count(), uniqExact(m.1), countIf(m.2 = 0), max(m.2)
FROM (SELECT `tv%3AblobArray%3Avalue%3Aval%3Ayh%3A4%3A%3A%3A0%3A%3Adata.mark` AS m
      FROM mergeTreeIndex('boxerm0', 'fsdata_facts_256k', with_marks = true))
UNION ALL
SELECT 'bespoke 1 MiB', count(), uniqExact(m.1), countIf(m.2 = 0), max(m.2)
FROM (SELECT `data.mark` AS m
      FROM mergeTreeIndex('boxerm0', 'fsdata_bespoke_1m', with_marks = true));

-- The operational form of the same claim: a point read of one block costs one
-- block's compressed bytes. Run each, then read ProfileEvents back:
-- SELECT cityHash64("tv:blobArray:value:val:yh:4:::0::data"[1])
--   FROM boxerm0.fsdata_facts_1m
--  WHERE "id:id:u64:47::0:" = 1
--    AND "id:naturalKey:y:4::0:" = concat('big.bin\0', hex(reinterpretAsString(toUInt32(100))))
--  SETTINGS use_query_cache = 0;
SELECT query_id, read_rows, formatReadableSize(ProfileEvents['ReadCompressedBytes']) AS read_compressed,
       ProfileEvents['SelectedMarks'] AS marks
FROM system.query_log
WHERE query_id LIKE 'm0c1b-%' AND type = 'QueryFinish'
ORDER BY event_time_microseconds;
