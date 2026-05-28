/*
ClickHouse SQL: The Leeway Integrity Scanner

Here is a diagnostic query that checks the consistency of the GeoPoint and Text sections. It uses UNION ALL so you can easily expand it to all 10 sections. If this query returns any rows, your SQL transformation introduced a corruption.
*/
SELECT
    id,
    naturalKey,
    section,
    error_type,
    base_attribute_count,
    expected_flattened_length,
    actual_flattened_length
FROM (
         -- ==========================================
         -- CHECK 1: GeoPoint Section Integrity
         -- ==========================================
         SELECT
             `id:id:u64:2k:0:0:` AS id,
             `id:naturalKey:y:g:0:0:` AS naturalKey,
             'GeoPoint' AS section,
             multiIf(
                 -- Rule 1 & 2: Base arrays and support columns must have identical lengths
                     length(`tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`) != length(`tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo`), 'Base/Card Length Mismatch',
                     length(`tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`) != length(`tv:geoPoint:lrcard:lrcard:u64:4gw:0:0:0::geo`), 'Base/Card Length Mismatch',

                 -- Rule 3: Flattened High-Cardinality References must match the sum of hrcard
                     length(`tv:geoPoint:hr:hr:u64:2k:0:0:0::geo`) != toUInt64(arraySum(`tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo`)), 'High-Card (hr) Integrity Failure',

                 -- Rule 3: Flattened Low-Cardinality References must match the sum of lrcard
                     length(`tv:geoPoint:lr:lr:u64:2q:0:0:0::geo`) != toUInt64(arraySum(`tv:geoPoint:lrcard:lrcard:u64:4gw:0:0:0::geo`)), 'Low-Card (lr) Integrity Failure',

                 -- Rule 3: Mixed Low-Card (lmr) and Hash Payload (mrhp) share the lmrcard length
                     length(`tv:geoPoint:lmr:lmr:u64:2q:0:0:0::geo`) != toUInt64(arraySum(`tv:geoPoint:lmrcard:lmrcard:u64:4gw:0:0:0::geo`)), 'Mixed-Card (lmr) Integrity Failure',
                     length(`tv:geoPoint:mrhp:mrhp:y:g:0:0:0::geo`) != toUInt64(arraySum(`tv:geoPoint:lmrcard:lmrcard:u64:4gw:0:0:0::geo`)), 'Mixed-Card Payload (mrhp) Integrity Failure',

                     'Valid'
             ) AS error_type,
             length(`tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`) AS base_attribute_count,
             toUInt64(arraySum(`tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo`)) AS expected_flattened_length,
             length(`tv:geoPoint:hr:hr:u64:2k:0:0:0::geo`) AS actual_flattened_length
         FROM anchor.facts

         UNION ALL

         -- ==========================================
         -- CHECK 2: Text Section Integrity (Testing Co-Containers)
         -- ==========================================
         SELECT
             `id:id:u64:2k:0:0:` AS id,
             `id:naturalKey:y:g:0:0:` AS naturalKey,
             'Text' AS section,
             multiIf(
                 -- Support columns lengths must match the base string array
                 -- (`text` is the scalar value column on the text section)
                     length(`tv:text:text:val:s:0:0:0:0::`) != length(`tv:text:len:len:u64:28o:0:0:0::`), 'Base/Len Length Mismatch',

                 -- Co-Container Integrity: The flattened word arrays must match the sum of the `len` column
                     length(`tv:text:wordLength:val:u32h:0:0:0:0::`) != toUInt64(arraySum(`tv:text:len:len:u64:28o:0:0:0::`)), 'Co-Container (wordLength) Integrity Failure',
                     length(`tv:text:wordBag:val:sh:0:0:0:0::`) != toUInt64(arraySum(`tv:text:len:len:u64:28o:0:0:0::`)), 'Co-Container (wordBag) Integrity Failure',

                     'Valid'
             ) AS error_type,
             length(`tv:text:text:val:s:0:0:0:0::`) AS base_attribute_count,
             toUInt64(arraySum(`tv:text:len:len:u64:28o:0:0:0::`)) AS expected_flattened_length,
             length(`tv:text:wordBag:val:sh:0:0:0:0::`) AS actual_flattened_length
         FROM anchor.facts
         )
WHERE error_type != 'Valid';
