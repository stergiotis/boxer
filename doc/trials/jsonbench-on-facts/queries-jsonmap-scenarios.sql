-- Scenario queries against the canonical leeway JSON mapping
-- (mapping.LoadJsonMapping), loaded by `jsonbench jsonmap ingest`.
--
-- queries-jsonmap.sql runs JSONBench's own five queries — the workload the
-- benchmark poses, which touches five known paths and nothing else. This file
-- asks the questions that workload cannot: it was written against a corpus
-- whose schema you already know, and Bluesky's is not one schema but 1,216
-- document shapes at the 10M tier.
--
-- Columns are leeway handles (ADR-0116), expanded by `jsonbench resolve`.
--
-- # What the shape affords
--
-- One row is one document. Each type section carries three lanes that
-- co-index 1:1 with its value lane:
--
--     `<sec>:value`  the values, typed and columnar per type
--     `<sec>:lmv`    the path, verbatim, array positions elided to "_"
--     `<sec>:mvhp`   the elided array indices (membership.AppendParams)
--
-- Two consequences run through every scenario below. First, **the path is
-- data**: it can be grouped, filtered, matched and joined like any other
-- column, so a query can range over paths it does not name. Second, **there is
-- no JSON-typed anything** — these are Array(String) / Array(Int64) /
-- Array(LowCardinality(String)) columns and ordinary array functions, so the
-- same expressions compose with joins, windows and aggregate combinators, and
-- carry to any engine with array support.
--
-- Findings quoted in the comments below are from the 10M tier, where they were
-- first observed; the 100M run reproduces them at scale and its timings and
-- outputs are in runs/2026-08-06-jsonmap-100m/. Costs recorded there are what
-- these queries cost on this table — not a claim about what another system
-- would cost, since this trial loaded no comparison table for these questions.

------------------------------------------------------------------------------
-- A. The corpus describes itself
------------------------------------------------------------------------------

-- A1. Path census. Every path in the corpus, which type section holds it, how
-- many documents carry it and how many times in total. Nothing here names a
-- path; the answer is discovered.
SELECT pc.1 AS path, section, count() AS documents, sum(pc.2) AS occurrences
FROM (
    SELECT section, arrayJoin(arrayMap(k -> (k, countEqual(paths, k)), arrayDistinct(paths))) AS pc
    FROM (
        SELECT 'symbol' AS section, `symbol:lmv` AS paths FROM json
        UNION ALL SELECT 'string',  `string:lmv`  FROM json
        UNION ALL SELECT 'int64',   `int64:lmv`   FROM json
        UNION ALL SELECT 'float64', `float64:lmv` FROM json
        UNION ALL SELECT 'bool',    `bool:lmv`    FROM json
    )
)
GROUP BY path, section
ORDER BY occurrences DESC
LIMIT 20;

-- A2. Polymorphic paths — a path that arrives as more than one type. The
-- sections partition by type, so a path appearing in two of them *is* the
-- finding, and no candidate has to be guessed in advance.
--
-- At 10M this returns exactly one row:
--   /commit/record/skyfeedBuilder/blocks/_/value  ['string','int64']  389 docs
-- A schema-on-write pipeline either coerces that or drops it.
SELECT path, groupArray(section) AS sections, sum(documents) AS documents
FROM (
    SELECT path, section, count() AS documents
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
    GROUP BY path, section
)
GROUP BY path
HAVING length(sections) > 1
ORDER BY documents DESC;

-- A3. How heterogeneous is the corpus, really? Distinct document shapes —
-- a shape being the sorted set of paths a document carries.
SELECT count() AS distinct_shapes, sum(documents) AS documents
FROM (
    SELECT arraySort(arrayDistinct(arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `float64:lmv`, `bool:lmv`))) AS shape,
           count() AS documents
    FROM json GROUP BY shape
);

-- A4. Breadth and depth, straight off the path lane. Depth is the number of
-- path segments; no traversal is involved because the path is already a value.
SELECT round(avg(attrs), 2) AS avg_attributes, max(attrs) AS max_attributes,
       round(avg(depth), 2) AS avg_depth, max(depth) AS max_depth
FROM (
    SELECT length(`symbol:lmv`) + length(`string:lmv`) + length(`int64:lmv`)
         + length(`float64:lmv`) + length(`bool:lmv`) AS attrs,
           arrayMax(arrayMap(p -> length(splitByChar('/', p)) - 1,
               arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `float64:lmv`, `bool:lmv`))) AS depth
    FROM json
);

------------------------------------------------------------------------------
-- B. Path-agnostic search
------------------------------------------------------------------------------

-- B1. Find a value anywhere, and report which paths held it. The needle is
-- drawn from the corpus so the query is self-contained; nothing tells it where
-- to look.
--
-- At 10M one post URI comes back at three paths — 80 occurrences as a
-- like/repost subject, 24 as a reply root, 10 as a reply parent. That is a
-- cross-reference graph recovered without naming an edge.
--
-- Note the sub-selects are repeated rather than bound by `WITH … AS needle`:
-- the ADR-0116 resolver does not descend into a WITH scalar subquery, and
-- passes the handle through unexpanded even under --strict (see the logbook).
SELECT arrayJoin(arrayFilter((p, v) -> v = (SELECT `string:value`[indexOf(`string:lmv`, '/commit/record/reply/root/uri')]
                                            FROM json WHERE has(`string:lmv`, '/commit/record/reply/root/uri') LIMIT 1),
                             `string:lmv`, `string:value`)) AS path,
       count() AS occurrences
FROM json
WHERE has(`string:value`, (SELECT `string:value`[indexOf(`string:lmv`, '/commit/record/reply/root/uri')]
                           FROM json WHERE has(`string:lmv`, '/commit/record/reply/root/uri') LIMIT 1))
GROUP BY path
ORDER BY occurrences DESC;

-- B2. Values that occur at more than one path within the same document —
-- structural redundancy, found without a hypothesis. At 10M this surfaces
-- reply root == parent (top-level replies), /did == /identity/did, and an
-- embedded external URI repeated in the facets.
SELECT arrayJoin(arrayDistinct(arrayFilter((p, v) -> countEqual(`string:value`, v) > 1,
                                           `string:lmv`, `string:value`))) AS path,
       count() AS documents
FROM json
GROUP BY path
ORDER BY documents DESC
LIMIT 10;

-- B3. A whole subtree, without naming its members. Prefix matching on the path
-- lane is the wildcard query a fixed column set cannot express.
SELECT arrayJoin(arrayFilter(p -> startsWith(p, '/commit/record/embed/'),
           arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `bool:lmv`))) AS path,
       count() AS occurrences
FROM json
GROUP BY path
ORDER BY occurrences DESC
LIMIT 10;

------------------------------------------------------------------------------
-- C. Arrays stay addressable
------------------------------------------------------------------------------

-- C1. How many elements does a repeated path carry per document? The array
-- degree is a count over the path lane — the array was never flattened away.
SELECT n AS langs_declared, count() AS posts
FROM (SELECT countEqual(`string:lmv`, '/commit/record/langs/_') AS n FROM json)
WHERE n > 0
GROUP BY n
ORDER BY n;

-- C2. Position is queryable: the *primary* language, not any language. mvhp
-- '0000' is index 0 in the canonical params form.
SELECT arrayFirst((v, p, m) -> (p = '/commit/record/langs/_') AND (m = '0000'),
                  `string:value`, `string:lmv`, `string:mvhp`) AS primary_lang,
       count() AS posts
FROM json
WHERE has(`string:lmv`, '/commit/record/langs/_')
GROUP BY primary_lang
ORDER BY posts DESC
LIMIT 10;

-- C3. Nested arrays keep both coordinates. /commit/record/facets/_/features/_
-- elides two indices, and mvhp carries the pair — decoded here with built-ins
-- (unhex + reinterpretAsUInt16), no UDF.
SELECT coord,
       arrayMap(t -> reinterpretAsUInt16(reverse(unhex(t))), splitByChar('.', coord)) AS facet_feature,
       count() AS occurrences
FROM (
    SELECT arrayJoin(arrayFilter((m, p) -> p = '/commit/record/facets/_/features/_/$type',
                                 `symbol:mvhp`, `symbol:lmv`)) AS coord
    FROM json
)
GROUP BY coord
ORDER BY occurrences DESC
LIMIT 8;

------------------------------------------------------------------------------
-- D. Schema inference and absence
------------------------------------------------------------------------------

-- D1. Infer the schema of every record type from the data. For each
-- collection: how many documents, how many distinct paths its record body
-- uses, and the most common ones with their coverage.
--
-- This is the query the benchmark's DDL assumes the answer to. At 10M the
-- spread is the whole point — app.bsky.graph.block uses 3 record paths,
-- app.bsky.feed.post uses 208 — and two coverage figures round to 0.0 %:
-- /commit/record/type (a typo for $type) and /commit/record/subject/quoteCount.
WITH docs AS (
    SELECT `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] AS collection, count() AS documents
    FROM json GROUP BY collection HAVING collection != ''
)
SELECT collection, documents, count() AS record_paths,
       arraySlice(arraySort(x -> -x.2, groupArray((path, pct))), 1, 6) AS top_record_paths
FROM (
    SELECT p.collection AS collection, p.path AS path, d.documents AS documents,
           round((100. * p.cnt) / d.documents, 1) AS pct
    FROM (
        SELECT collection, path, count() AS cnt
        FROM (
            SELECT `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] AS collection,
                   arrayJoin(arrayDistinct(arrayFilter(p -> startsWith(p, '/commit/record/'),
                       arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `float64:lmv`, `bool:lmv`)))) AS path
            FROM json
        )
        WHERE collection != ''
        GROUP BY collection, path
    ) AS p
    INNER JOIN docs AS d USING (collection)
)
GROUP BY collection, documents
ORDER BY documents DESC
LIMIT 8;

-- D2. The long tail the benchmark's `max_dynamic_paths = 0` throws into shared
-- data: paths carried by a vanishing fraction of documents. Each is still a
-- first-class, queryable attribute here.
--
-- The 10M result carries the sharpest finding in this file. Among the
-- single-document paths are ones like
--
--     /commit/record/skeetsAppHistory/data/_/2024-11-21T16:31:48.241Z
--
-- — a client writing *timestamps as object keys*. The corpus therefore has no
-- finite path vocabulary at all: the path space grows with the data. That is
-- the property a closed, registered path vocabulary cannot represent, and it
-- is why this mapping's memberships are verbatim rather than Ref-shaped
-- (trial ledger row 3).
SELECT path, documents, round((100. * documents) / (SELECT count() FROM json), 6) AS pct_of_corpus
FROM (
    SELECT arrayJoin(arrayDistinct(arrayConcat(`symbol:lmv`, `string:lmv`, `int64:lmv`, `float64:lmv`, `bool:lmv`))) AS path,
           count() AS documents
    FROM json GROUP BY path
)
WHERE documents < 100
ORDER BY documents ASC
LIMIT 15;

-- D3. Absence as a first-class question: within a collection, which documents
-- lack a path most of their siblings carry? `has` on the path lane answers it
-- for any path, including ones with no dedicated column anywhere.
--
-- Restricted to create operations on purpose. A delete carries no record body
-- at all, so counting its absences would report the operation vocabulary as a
-- data-quality problem. With that removed, one real signal survives at 10M:
-- app.bsky.actor.profile omits /commit/record/createdAt in 1,210 of 57,530
-- creates, and is the only collection that omits it at all — while never
-- omitting $type.
SELECT `symbol:value`[indexOf(`symbol:lmv`, '/commit/collection')] AS collection,
       count() AS created,
       countIf(NOT has(`string:lmv`, '/commit/record/createdAt')) AS missing_created_at,
       countIf(NOT has(`symbol:lmv`, '/commit/record/$type')) AS missing_type
FROM json
WHERE `symbol:value`[indexOf(`symbol:lmv`, '/commit/operation')] = 'create'
GROUP BY collection
HAVING (collection != '') AND ((missing_created_at > 0) OR (missing_type > 0))
ORDER BY created DESC
LIMIT 10;
