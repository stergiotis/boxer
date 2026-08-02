/* Query 1 — attack types and their target ports, via the pack's raggedNest.

Lists each cyber incident's attack type (the `symbol` section value) together
with its target network ports, which ride the same attributes as low-cardinality
ref memberships (`lr`). raggedNest — from the co/ragged function pack
(ADR-0162) that hosts reconcile at connect — regroups the flat `lr` stream by
the per-attribute `lrcard` counts so it can be ARRAY-JOINed in parallel with
the value column. Nesting at an ARRAY JOIN boundary is exactly the pack's
documented use for raggedNest: the codomain is genuinely nested here.

Column references are friendly leeway handles (`section:column`, ADR-0116); the
nanopass pipeline resolves them to physical names (see the .out.sql neighbour).
`symbol:lr` and `symbol:lrcard` are support columns — handles cover those too.

The pack call sits inline in ARRAY JOIN rather than in a `WITH expr AS name`
clause: the resolve pass walks each SELECT's own subtree, and a query-level
WITH-expression clause is outside it — handles there would pass through
unresolved. (deferred: teach ResolveColumnNames the query-level WITH clause.)
*/
SELECT
    `id:id` AS id,
    `id:naturalKey` AS incident_ticket,
    attack_type,
    target_ports
FROM facts
-- parallel ARRAY JOIN: both lists carry one element per attribute
ARRAY JOIN
    `symbol:value` AS attack_type,
    raggedNest(`symbol:lr`, `symbol:lrcard`) AS target_ports
WHERE has(['DDOS', 'SQL_INJECTION', 'PORT_SCAN'], attack_type)
