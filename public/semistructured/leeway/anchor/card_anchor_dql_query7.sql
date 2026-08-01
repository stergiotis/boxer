/* Query 7 — a retrieval read: which rows, and why.

Unlike queries 1–6 (which all compute in their projections), this is a plain
information-retrieval read: one table, a verbatim projection, a filtering
WHERE. The ADR-0117 passthrough classifier triages it as such — see the
analysis artifact, where this is the only query with a passthrough table —
and that is the precondition for the ADR-0121 selection-conditions rewrite
(card_anchor_dql_lwsql.out.md), which turns each OR disjunct below into a
result column reporting which alternative admitted the row.
*/
SELECT
    `id:id`,
    `id:naturalKey`,
    `symbol:value`,
    `timeRange:beginIncl`,
    `geoPoint:h3`
FROM facts
WHERE has(`symbol:value`, 'DELIVERED')
   OR has(`symbol:value`, 'SEISMIC_ANOMALY')
