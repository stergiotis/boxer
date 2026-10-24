---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the adhoc cap.

# adhoc — handing a computed table to SQL

[ADR-0240](../../../doc/adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md)
lets a running app hand a table it computed to SQL without creating durable
state: no file with a name, no table in ClickHouse, no plaintext at rest. The
app publishes an Arrow stream under an alias; a play window or an applet reads
it by that alias.

A dataset lives as long as the window that published it, unless the publisher
asks for the app's lifetime instead. Closing the window retracts it; ending the
process frees it.

## What the schematic shows

**Subjects.** `adhoc.publish`, `adhoc.retract`, `adhoc.resolve` and
`adhoc.read`, request/reply; `adhoc.event.*` announces a publish or a
retraction as a hint, and `adhoc.bundle.*` does the same for a named group of
datasets. An app declares the subjects it uses on its manifest.

**Backend.** One service, `adhocdata.Service`, over the `sealed` store: each
dataset is an unnamed file under a key held only in process memory, registered
under an unguessable handle that is a valid `keelson('…')` table name.

## Where to look

- `keelson('adhoc')` — the datasets live in this process: alias, publisher, rows, bytes.
- [How to publish an ad-hoc dataset and query it](../../../doc/howto/adhoc-datasets.md).
- `runtime/adhocdata` — the service, the publisher and the follower.
