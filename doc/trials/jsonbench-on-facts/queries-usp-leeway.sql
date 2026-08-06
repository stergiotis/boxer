-- The leeway half of the head-to-head in leeway-usp-experiments.md.
-- Run against the canonical leeway JSON mapping table, handles expanded by
-- `jsonbench resolve`. The jsonv2 half is queries-usp-jsonv2.sql; the two files
-- are matched statement for statement, U1..U9.
--
--   DB=jsonbench_j_10m TABLE=json QUERIES=./queries-usp-leeway.sql \
--   OUT=<dir> RESOLVE=<jsonbench binary> DROP_CACHES=1 TRIES=3 \
--   CH_SETTINGS="--use_query_condition_cache=0 --min_execution_speed=0" \
--   ./measure.sh bench
--
-- Both settings matter and both were found the hard way; the document's
-- "Fairness controls" section says why.

-- U1 — path census with per-path document counts
SELECT path, count() AS documents
FROM (
    SELECT arrayJoin(arrayDistinct(arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `float64:lmv`, `bool:lmv`))) AS path
    FROM json
)
GROUP BY path ORDER BY documents DESC LIMIT 20;

-- U2 — path x type census (the polymorphic-path scan)
SELECT path, count() AS n
FROM (
    SELECT section, arrayJoin(arrayDistinct(paths)) AS path
    FROM (
        SELECT 'symbol' AS section, `symbol:lmv` AS paths FROM json
        UNION ALL SELECT 'string',  `string:lmv`  FROM json
        UNION ALL SELECT 'int64',   `int64:lmv`   FROM json
        UNION ALL SELECT 'float64', `float64:lmv` FROM json
        UNION ALL SELECT 'bool',    `bool:lmv`    FROM json
    )
)
GROUP BY path, section HAVING n > 0 ORDER BY n DESC LIMIT 20;

-- U3 — value anywhere, exact. No path is named.
SELECT count() FROM json WHERE has(`string:value`, 'did:plc:vwadmn5cx4d2rqxbxbjajzhx');

-- U4 — subtree prefix census
SELECT path, count() AS occurrences
FROM (
    SELECT arrayJoin(arrayFilter(p -> startsWith(p, '/commit/record/embed/'),
               arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `bool:lmv`))) AS path
    FROM json
)
GROUP BY path ORDER BY occurrences DESC LIMIT 10;

-- U5 — sum every integer in the corpus, whatever path it sits at
SELECT sum(arraySum(`int64:value`)) AS total FROM json;

-- U6 — leaf count per document, corpus-wide
SELECT sum(length(`symbol:lmv`) + length(`string:lmv`) + length(`int64:lmv`)
         + length(`float64:lmv`) + length(`bool:lmv`)) AS leaves
FROM json;

-- U7 — presence of one *constant* path. The case jsonv2 is built for; included
-- so the comparison is not one-sided.
SELECT count() AS documents FROM json WHERE has(`string:lmv`, '/commit/record/text');

-- U8 — a numeric predicate across every integer-valued path at once
SELECT count() AS documents FROM json WHERE arrayExists(v -> v > 1700000000000000, `int64:value`);

-- U9 — array degree for *every* array-valued path in the corpus, discovered
SELECT path, round(avg(n), 3) AS avg_elems, max(n) AS max_elems
FROM (
    SELECT pc.1 AS path, pc.2 AS n
    FROM (
        SELECT arrayJoin(arrayMap(k -> (k, countEqual(paths, k)),
                   arrayDistinct(arrayFilter(p -> position(p, '/_') > 0, paths)))) AS pc
        FROM (
            SELECT arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `float64:lmv`, `bool:lmv`) AS paths
            FROM json
        )
    )
)
GROUP BY path ORDER BY avg_elems DESC LIMIT 10;
