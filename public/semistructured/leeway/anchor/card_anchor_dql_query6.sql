/* Query 6 — an integrity scanner over leeway's support-column invariants.

Checks the structural consistency of the geoPoint and text sections: base
value arrays and per-attribute cardinality columns must have equal lengths,
and each flattened channel (refs, co-containers) must sum to its cardinality
column. A returned row means a transformation corrupted the shape; extend the
UNION ALL with one branch per further section.

Support columns are addressed through the same friendly handles as value
columns (`geoPoint:hr`, `geoPoint:hrcard`, `text:len`, …) — the resolver knows
every column of a section, value and support alike.
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
    -- geoPoint section
    SELECT
        `id:id` AS id,
        `id:naturalKey` AS naturalKey,
        'GeoPoint' AS section,
        multiIf(
            -- base arrays and cardinality columns must have identical lengths
            length(`geoPoint:pointLat`) != length(`geoPoint:hrcard`), 'Base/Card Length Mismatch',
            length(`geoPoint:pointLat`) != length(`geoPoint:lrcard`), 'Base/Card Length Mismatch',
            -- each flattened ref channel must sum to its cardinality column
            length(`geoPoint:hr`) != toUInt64(arraySum(`geoPoint:hrcard`)), 'High-Card (hr) Integrity Failure',
            length(`geoPoint:lr`) != toUInt64(arraySum(`geoPoint:lrcard`)), 'Low-Card (lr) Integrity Failure',
            length(`geoPoint:lmr`) != toUInt64(arraySum(`geoPoint:lmrcard`)), 'Mixed-Card (lmr) Integrity Failure',
            length(`geoPoint:mrhp`) != toUInt64(arraySum(`geoPoint:lmrcard`)), 'Mixed-Card Payload (mrhp) Integrity Failure',
            'Valid'
        ) AS error_type,
        length(`geoPoint:pointLat`) AS base_attribute_count,
        toUInt64(arraySum(`geoPoint:hrcard`)) AS expected_flattened_length,
        length(`geoPoint:hr`) AS actual_flattened_length
    FROM facts

    UNION ALL

    -- text section (co-containers)
    SELECT
        `id:id` AS id,
        `id:naturalKey` AS naturalKey,
        'Text' AS section,
        multiIf(
            -- support-column lengths must match the base string array
            length(`text:text`) != length(`text:len`), 'Base/Len Length Mismatch',
            -- flattened co-containers must sum to the len column
            length(`text:wordLength`) != toUInt64(arraySum(`text:len`)), 'Co-Container (wordLength) Integrity Failure',
            length(`text:wordBag`) != toUInt64(arraySum(`text:len`)), 'Co-Container (wordBag) Integrity Failure',
            'Valid'
        ) AS error_type,
        length(`text:text`) AS base_attribute_count,
        toUInt64(arraySum(`text:len`)) AS expected_flattened_length,
        length(`text:wordBag`) AS actual_flattened_length
    FROM facts
)
WHERE error_type != 'Valid'
