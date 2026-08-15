---
type: how-to
audience: engineer or operator diagnosing a running box
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to query the appliance topology

You want to know what is running on a box, what should be running, which
process owns a socket, or what a component depends on — as queries, not
archaeology. [ADR-0126](../adr/0126-appliance-topology-as-data.md) exposes the
topology through five `keelson.*` tables; this page collects the canonical
queries. Every later diagnosis or detection rule should build on these shapes
rather than re-derive them.

Caveats up front: the observed side exists only while a scraper publishes
(rows are empty, not absent, without one), holds the *latest* snapshot only
(no history until the ADR-0090 P5 tee exists), and cannot distinguish a
`failed` unit from an absent one — that blind spot is recorded in
[ADR-0126 §SD6](../adr/0126-appliance-topology-as-data.md). Marks are
cooperative identity, not a security boundary; `cgroup_unit` and `uid`
corroborate.

## The tables

| table | side | one row is |
|---|---|---|
| `keelson.components` | declared | one registry component: `token`, `role`, `needs` |
| `keelson.procs` | observed | one process: identity, `component`, `cgroup_unit`, resources, staleness stamps |
| `keelson.sockets` | observed | one listening socket: `proto`, `addr`, `port`, `pid`, `inode` |
| `keelson.topology_nodes` | both | one graph node: `kind`, `key`, `host`, `origin`, `source` |
| `keelson.topology_edges` | both | one directed edge: `edge_kind`, `src_key`, `dst_key`, `host`, `origin`, `source` |

Node keys are `kind:name` strings (`component:caddy`, `proc:4711`,
`sock:tcp/127.0.0.1:8123`, `app:imztop`, `subject:sysmetrics.>`,
`host:demo-box-1`); both halves emit identical keys, which is what makes
drift a single-table query. Reach the tables from `apps/play` (or any
in-process consumer) via the `keelson('…')` macro, or from an external
ClickHouse via `url()` against the introspection HTTP source
([ADR-0094](../adr/0094-keelson-introspection-tables.md)).

## Drift: declared vs observed

Components declared in the inventory with no live marked process:

```sql
SELECT key
FROM keelson('topology_nodes')
WHERE kind = 'component'
GROUP BY key
HAVING NOT has(groupArray(origin), 'observed')
ORDER BY key
```

The inverse — marks observed on the box that no registry entry declares
(a stray or misspelled mark):

```sql
SELECT key, any(host) AS host
FROM keelson('topology_nodes')
WHERE kind = 'component'
GROUP BY key
HAVING NOT has(groupArray(origin), 'declared')
```

Interpretation note: until the R1 desired-state store exists, "declared"
means "in the compiled-in inventory", not "must run on this box" — a
declared-but-absent component may simply not be deployed here
(ADR-0126 §SD6).

## Who owns this socket?

From the typed tables (richest detail):

```sql
SELECT s.proto, s.addr, s.port, p.pid, p.name, p.component, p.cgroup_unit, p.user
FROM keelson('sockets') AS s
LEFT JOIN keelson('procs') AS p ON s.pid = p.pid AND s.host = p.host
WHERE s.port = 8089
```

Or over the graph rows alone (listener edge → containment edge):

```sql
SELECT c.dst_key AS component
FROM keelson('topology_edges') AS l
INNER JOIN keelson('topology_edges') AS c
  ON l.src_key = c.src_key AND l.host = c.host
WHERE l.edge_kind = 'proc-listens'
  AND l.dst_key = 'sock:tcp/127.0.0.1:8089'
  AND c.edge_kind = 'proc-in-component'
```

A socket row with `pid = 0` is published but unattributed — the owner's
fd table was unreadable (a privilege boundary), not proof nothing owns it.

## What does a component depend on?

Direct declared dependencies are `component-needs` edges; the closure is
a recursive walk:

```sql
WITH RECURSIVE closure AS (
  SELECT dst_key
  FROM keelson('topology_edges')
  WHERE edge_kind = 'component-needs' AND src_key = 'component:caddy'
  UNION ALL
  SELECT e.dst_key
  FROM keelson('topology_edges') AS e
  INNER JOIN closure AS c ON e.src_key = c.dst_key
  WHERE e.edge_kind = 'component-needs'
)
SELECT DISTINCT dst_key FROM closure
```

## What runs inside a component?

Processes (children included — environment marks inherit):

```sql
SELECT pid, ppid, name, cmd, cgroup_unit, cpu_percent, rss_bytes
FROM keelson('procs')
WHERE component = 'imzero2-demo'
ORDER BY pid
```

A `component` that is empty on a process you expected marked usually
means one of: the process was started outside a mark-injecting
supervisor or launcher, a spawner scrubbed `Env`, or the scraper lacks
the privilege to read that uid's environ (then `cgroup_unit` still
identifies systemd-managed processes).

## Which apps touch a subject?

The declared bus graph, from the manifest caps:

```sql
SELECT edge_kind, src_key
FROM keelson('topology_edges')
WHERE edge_kind IN ('app-pub', 'app-sub')
  AND dst_key = 'subject:sysmetrics.>'
```

Edge patterns are the manifests' filter strings verbatim; overlap
between a pub pattern and a sub pattern is not computed here.

## What does an open window hold right now?

The declared graph above says what an app *may* do. Three live tables say
what each running instance has actually acquired
([ADR-0188 §SD4](../adr/0188-app-instance-effect-tracking.md)):
`keelson.subscriptions` (bus subscriptions), `keelson.client_caps` (the
permission set of each live bus client — manifest caps plus runtime
grants), and `keelson.tasks` (in-flight tasks). All three carry the
window's instance key, so they join `keelson.windows`. The rows are exactly
what the host releases when the window closes, which makes the tables the
check on that release: a reaped window leaves no row.

Everything one window holds:

```sql
WITH w AS (SELECT key, app_id, title FROM keelson('windows'))
SELECT * FROM (
    SELECT w.title AS title, 'subscription' AS effect, s.pattern AS what
    FROM keelson('subscriptions') AS s
    JOIN w ON s.instance_key = w.key
    UNION ALL
    SELECT w.title, 'task', concat(t.kind, ' ', t.task_id)
    FROM keelson('tasks') AS t
    JOIN w ON t.owner_instance_key = w.key
)
ORDER BY title, effect, what
```

(ClickHouse binds a trailing `ORDER BY` to the last `SELECT` of a
`UNION ALL`, hence the wrapping subquery.)

Grants that no manifest declares — the runtime-granted surface, per window:

```sql
SELECT app_id, instance_key, pattern, direction, reason
FROM keelson('client_caps')
WHERE NOT declared
ORDER BY app_id, instance_key, pattern
```

Subscriptions attributed to no window (services, the CLI, or a client
minted without an instance key) show `instance_key = 0`; reply inboxes of
in-flight requests are subscriptions too and are flagged `is_inbox`.

## Visualize: the play Network tab

The layered-graph result panel
([ADR-0129](../adr/0129-play-layered-graph-panel.md)) draws any query with
an `edges` CTE (`source`/`target` columns) and an optional `vertices` CTE
(`id`, `label`, `group`, `shape`) as a node-link diagram. The topology
graph is a natural fit — this query draws the component graph with drift
as colour (the `group` column is the origin set, so a declared-but-absent
component is tinted differently from a running one) and each component's
listeners as ellipses:

```sql
WITH
  comp AS (
    SELECT key,
           arrayStringConcat(arraySort(groupArray(DISTINCT origin)), '+') AS origins
    FROM keelson('topology_nodes')
    WHERE kind = 'component'
    GROUP BY key
  ),
  lst AS (
    SELECT c.dst_key AS source, l.dst_key AS target
    FROM keelson('topology_edges') AS l
    INNER JOIN keelson('topology_edges') AS c
      ON l.src_key = c.src_key AND l.host = c.host
    WHERE l.edge_kind = 'proc-listens'
      AND c.edge_kind = 'proc-in-component'
  ),
  vertices AS (
    SELECT key AS id, substring(key, 11) AS label, origins AS `group`, 'box' AS shape
    FROM comp
    UNION ALL
    SELECT DISTINCT target AS id, substring(target, 6) AS label, 'listener' AS `group`, 'ellipse' AS shape
    FROM lst
  ),
  edges AS (
    SELECT src_key AS source, dst_key AS target, 'needs' AS label
    FROM keelson('topology_edges')
    WHERE edge_kind = 'component-needs'
    UNION ALL
    SELECT source, target, 'listens' AS label
    FROM lst
  )
SELECT * FROM edges
```

Point play at the introspection endpoint first (the toolbar's Endpoint →
"Keelson introspection" preset, or launch with `CLICKHOUSE_URL` set to the
`/query` URL — that is the variable play reads, not `CLICKHOUSE_ENDPOINT`).
The panel caps at a few hundred vertices, so keep raw `proc` nodes out of
the drawing on a busy box — the query above deliberately collapses
processes away, walking listeners straight to their component.

## Staleness

The observed tables carry their provenance clocks — check them before
trusting a quiet table:

```sql
SELECT host,
       max(sampled_at_unix_ms)  AS sampled,
       max(received_at_unix_ms) AS received
FROM keelson('procs')
GROUP BY host
```

## Further reading

- [ADR-0126](../adr/0126-appliance-topology-as-data.md) — the vocabulary,
  its limits, and the deferred supervisor collector
- [ADR-0094](../adr/0094-keelson-introspection-tables.md) — how the
  `keelson.*` tables are served and reached
- [ADR-0090](../adr/0090-sysmetrics-pubsub-data-plane.md) — the metric
  plane the observed side rides
- [aiops-operability](../explanation/aiops-operability.md) — where the
  topology layer sits in the operability map
