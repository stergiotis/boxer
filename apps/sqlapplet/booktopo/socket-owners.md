---
type: reference
audience: end-user
status: draft
title: Socket owners
icon: "🔌"
endpoint: introspection
tabs: [table, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Socket owners

Listening sockets joined to their owning processes: which component
answers on which port. The `port` parameter narrows to one port; `0`
keeps every listener.

A socket row with pid 0 is published but unattributed — the collector
could not read the owner's fd table (a privilege boundary), not proof
that nothing owns it — and its process columns render empty. Rows exist
only while a scraper publishes on the metric plane.

```sql
SET param_port = 0;
SELECT s.host, s.proto, s.addr, s.port, s.pid,
       p.name, p.component, p.cgroup_unit, p.user
FROM keelson('sockets') AS s
LEFT JOIN keelson('procs') AS p ON s.pid = p.pid AND s.host = p.host
WHERE {port:Int32} = 0 OR s.port = {port:Int32}
ORDER BY s.host, s.port
```
