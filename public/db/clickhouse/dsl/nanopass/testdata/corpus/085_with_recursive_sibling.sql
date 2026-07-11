with recursive seed AS (SELECT 1 AS x), t AS (SELECT x FROM seed UNION ALL SELECT x + 1 FROM t WHERE x < 3) SELECT count() FROM t
