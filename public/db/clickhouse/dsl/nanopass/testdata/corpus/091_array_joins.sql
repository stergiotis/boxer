SELECT a, e1, e2, e3 FROM t LEFT ARRAY JOIN arr AS e1, arr AS e2 ARRAY JOIN arrayFilter(i -> i IN (SELECT id FROM t3), arr) AS e3 INNER ARRAY JOIN [1, 2] AS w WHERE e1 > 0 ORDER BY a
