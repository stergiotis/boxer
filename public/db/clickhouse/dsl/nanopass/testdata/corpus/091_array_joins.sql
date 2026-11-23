SELECT k, x, y, z FROM t LEFT ARRAY JOIN a AS x, b AS y ARRAY JOIN arrayFilter(i -> i IN (SELECT k FROM u), c) AS z INNER ARRAY JOIN [1, 2] AS w WHERE x > 0 ORDER BY k
