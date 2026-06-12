SELECT plus(1, (SELECT max(v) FROM other)) FROM t
