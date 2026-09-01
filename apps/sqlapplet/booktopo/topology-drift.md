---
type: reference
audience: end-user
status: draft
title: Topology drift
summary: "Compare declared components against what is running"
icon: "⚖"
endpoint: introspection
tabs: [table, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Topology drift

Declared vs observed, one row per component: `declared-only` is in the
compiled-in inventory with no live marked process, `observed-only` is a
mark running on the box that no registry entry declares (stray or
misspelled), `both` is a component running as declared. Drift sorts
first.

Interpretation limits (ADR-0126 §SD6): a `declared-only` component may
simply not be deployed on this box, and a failed unit is
indistinguishable from an absent one. A silent scraper makes *every*
component `declared-only` — check the Plane staleness applet before
reading this table.

```sql
SELECT key,
       arraySort(groupArray(DISTINCT origin)) AS origins,
       multiIf(NOT has(origins, 'observed'), 'declared-only',
               NOT has(origins, 'declared'), 'observed-only',
               'both') AS status,
       anyIf(host, host != '') AS observed_host
FROM keelson('topology_nodes')
WHERE kind = 'component'
GROUP BY key
ORDER BY status DESC, key
```
