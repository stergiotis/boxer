-- Check 7 — FULL OUTER JOIN default-filling behind the diff idiom.
--
-- The idiom leans on join_use_nulls = 0 (the default): the missing side fills
-- with the type default, and '' is never a valid io/fs path, so it is a safe
-- "absent" marker. Under join_use_nulls = 1 the comparisons go three-valued
-- and the idiom silently stops classifying.
SELECT value FROM system.settings WHERE name = 'join_use_nulls';

INSERT INTO boxerm0.fsmeta
  ("id:id:u64:47::0:", "ts:ts:z64:47::0:", "id:naturalKey:y:4::0:", "lc:expiresAt:z64:4::0:")
SELECT 1, toDateTime64('2026-08-20 00:00:00',9,'UTC'), p, toDateTime64('2026-08-27 00:00:00',9,'UTC')
FROM (SELECT arrayJoin(['.', 'a', 'a/b.txt', 'a/new.txt', 'top.md', 'a/.gitignore', 'a/.hidden.txt']) AS p);

WITH toDateTime64('2026-08-19 00:00:00',9,'UTC') AS s1,
     toDateTime64('2026-08-20 00:00:00',9,'UTC') AS s2
SELECT if(n.path != '', n.path, o.path) AS path,
       multiIf(o.path = '', 'added', n.path = '', 'removed', 'same') AS change
FROM (SELECT "id:naturalKey:y:4::0:" AS path FROM boxerm0.fsmeta
      WHERE "id:id:u64:47::0:" = 1 AND "ts:ts:z64:47::0:" = s2) AS n
FULL OUTER JOIN
     (SELECT "id:naturalKey:y:4::0:" AS path FROM boxerm0.fsmeta
      WHERE "id:id:u64:47::0:" = 1 AND "ts:ts:z64:47::0:" = s1) AS o
ON n.path = o.path
WHERE change != 'same'
ORDER BY path;
