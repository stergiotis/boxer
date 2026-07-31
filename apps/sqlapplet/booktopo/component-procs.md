---
type: reference
audience: end-user
status: draft
title: Component processes
icon: "⚙"
endpoint: introspection
tabs: [table, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Component processes

Marked processes grouped by component: identity, systemd cgroup unit,
and resources. The `component` parameter is a SQL LIKE pattern over
component tokens; `%` keeps every marked process.

Unmarked processes are filtered out here. A process you expected marked
was started outside a mark-injecting supervisor, had its environment
scrubbed by a spawner, or runs as a uid whose environ the scraper cannot
read — in the last case `cgroup_unit` in the full `keelson('procs')`
table still identifies systemd-managed processes. Marks inherit, so a
component's child processes attribute automatically.

```sql
SET param_component = '%';
SELECT pid, ppid, name, state, component, cgroup_unit, user,
       cpu_percent, rss_bytes, num_threads, cmd
FROM keelson('procs')
WHERE component != '' AND component LIKE {component:String}
ORDER BY component, pid
```
