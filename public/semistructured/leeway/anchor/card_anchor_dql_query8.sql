/* Query 8 — write-back: a cyber "silver" slice via INSERT … SELECT (ADR-0181 §SD8).

The INSERT wrapper flows through the same pipeline as queries 1-7, and the
constructor mints ADOPT the target: anchor.silver spells its columns in the
fixture's own camelCase convention with aspect hints, and the .out.sql
neighbour shows every LW_TV / LW_TV_MEMB / LW_TV_SUPPORT call resolving to
silver's exact physical names — where the same calls against an unknown
target compose folded, aspect-free fresh-table names instead (compare
query 5's mints).

The target is a scope sink: no handle resolves against it, and the source
handles bind to `facts` exactly as in a bare SELECT. No column list — the
SELECT produces silver's five columns in DDL order, which the target shape
check (LwShapeCheckTarget, exercised in the package tests) verifies by name.

The integration lane creates anchor.silver and executes this statement.
*/
INSERT INTO anchor.silver
SELECT
    `id:id`,
    `id:naturalKey`,

    -- normalise the attack vector for the shared slice
    LW_TV(arrayMap(x -> upper(x), `symbol:value`), 'symbol', 'value', 's'),

    -- the targeted-port tags ride along, lanes intact
    LW_TV_MEMB(`symbol:lr`, 'symbol', 'low-card-ref'),
    LW_TV_SUPPORT(`symbol:lrcard`, 'symbol', 'lrcard')

FROM facts
WHERE hasAny(`symbol:value`, ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
