-- The four-criteria filter-and-retrieve query, in each layout. One statement
-- per arm, same predicate, same projection: given kind / operation /
-- collection / did, return each matching event's timestamp and user.
--
-- Measured at 100M in runs/2026-08-07-m6-100m/retrieve-4criteria-100m.tsv,
-- both with the exploded table sorted (path, doc) and with every arm
-- ORDER BY tuple(). The density is varied by swapping the did literal, or by
-- dropping the did predicate entirely for the 8,377,929-event point.

-- === exploded: one row per attribute ==========================================
-- Each criterion is an independent selection over the same table; the joins
-- intersect the surviving document ids. Sorted (path, doc), every WHERE is a
-- primary-key range seek. Unsorted, every one is a full scan of 1.2e9 rows.
SELECT t.i64 AS time_us, d.str AS did
FROM      (SELECT doc, str FROM attrs WHERE path = '/did'
             AND str = 'did:plc:yj3sjq3blzpynh27cumnp5ks')                     AS d
INNER JOIN (SELECT doc      FROM attrs WHERE path = '/kind'
             AND sym = 'commit')                                               AS k ON d.doc = k.doc
INNER JOIN (SELECT doc      FROM attrs WHERE path = '/commit/operation'
             AND sym = 'create')                                               AS o ON d.doc = o.doc
INNER JOIN (SELECT doc      FROM attrs WHERE path = '/commit/collection'
             AND sym = 'app.bsky.feed.post')                                   AS c ON d.doc = c.doc
INNER JOIN (SELECT doc, i64 FROM attrs WHERE path = '/time_us')                AS t ON d.doc = t.doc;

-- === packed: one row per document, leeway lanes ===============================
-- Every predicate is a linear search of the path lane, per row, per criterion.
-- No index is possible: the path is a value inside an array.
SELECT `tv:int64:value:val:i64:4o:0:0:0::`[indexOf(`tv:int64:lmv:lmv:y:m:0:0:0::`,  '/time_us')] AS time_us,
       `tv:string:value:val:s:g:0:0:0::` [indexOf(`tv:string:lmv:lmv:y:m:0:0:0::`,  '/did')]     AS did
FROM json
WHERE `tv:string:value:val:s:g:0:0:0::`[indexOf(`tv:string:lmv:lmv:y:m:0:0:0::`, '/did')]
        = 'did:plc:yj3sjq3blzpynh27cumnp5ks'
  AND `tv:symbol:value:val:s:m:0:0:0::`[indexOf(`tv:symbol:lmv:lmv:y:m:0:0:0::`, '/kind')]              = 'commit'
  AND `tv:symbol:value:val:s:m:0:0:0::`[indexOf(`tv:symbol:lmv:lmv:y:m:0:0:0::`, '/commit/operation')]  = 'create'
  AND `tv:symbol:value:val:s:m:0:0:0::`[indexOf(`tv:symbol:lmv:lmv:y:m:0:0:0::`, '/commit/collection')] = 'app.bsky.feed.post';

-- === JSON type, nothing declared (arm A00) ====================================
-- Subcolumns are Dynamic, so each needs an explicit cast; ORDER BY tuple().
SELECT data.time_us::Int64 AS time_us, data.did::String AS did
FROM bluesky
WHERE data.did::String                = 'did:plc:yj3sjq3blzpynh27cumnp5ks'
  AND data.kind::String               = 'commit'
  AND data.commit.operation::String   = 'create'
  AND data.commit.collection::String  = 'app.bsky.feed.post';

-- === JSON type, typed hints + clustered index (arm A) =========================
-- Same query, no casts needed. ORDER BY (kind, operation, collection, did,
-- time_us) makes all four criteria a primary-key prefix: 5 granules of 1225.
SELECT data.time_us AS time_us, data.did AS did
FROM bluesky
WHERE data.did               = 'did:plc:yj3sjq3blzpynh27cumnp5ks'
  AND data.kind              = 'commit'
  AND data.commit.operation  = 'create'
  AND data.commit.collection = 'app.bsky.feed.post';
