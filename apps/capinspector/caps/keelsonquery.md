---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the keelsonquery cap.

# keelsonquery — reading one introspection table

[ADR-0253](../../../doc/adr/0253-introspection-table-reads-as-a-bus-capability.md)
gives an app a declared way to read a `keelson()` table
([ADR-0094](../../../doc/adr/0094-keelson-introspection-tables.md)): a
request on the table's own subject, answered by the host over the in-process
engine. Before it, a window read the same table over loopback HTTP on an
endpoint it found through a process-global, which no manifest declared, no
broker prompted for, and no audit row attributed to the window.

An app declares one grant per table it reads and reads through the typed
client:

```go
Caps: keelsonquery.ClientCaps(wb.TableEvent, wb.TableWorker),

func (inst *App) Mount(ctx app.MountContextI) error {
    inst.reads = keelsonquery.NewClient(ctx.Bus())
    return nil
}

rows, err := keelsonquery.Rows[eventRow](ctx, inst.reads, wb.TableEvent,
    "SELECT job_id, at, state FROM keelson('watchbill_event') WHERE job_id = 'j1' ORDER BY at")
```

The statement carries no FORMAT clause; the request names the format, and
`Rows` asks for JSONEachRow and decodes through the row type's `json` tags.

## What the schematic shows

- **Subjects.** `keelson.query.<table>`, request/reply with generated
  codecs. The table in the subject and in the payload must agree.
- **Backends.** One service, under the id `runtime.introspect.query` — the
  identity the HTTP `/query` runner already publishes under — over the
  ADR-0094 §SD4 engine: the referenced table is snapshotted in-process,
  projected to the referenced columns, and run on the chlocal pool.

## What guards it

- **One table per grant.** The service holds the statement to the
  subject's table: no second table, no qualified name, no table function,
  and nothing that is not provably read-only. An unparseable statement is
  refused rather than run, because the engine's fallback for one is to
  snapshot every table.
- **Consent.** `ClientCaps` is sticky: the tables are redacted at the
  provider, and one prompt per table per install is proportionate.
- **Audit.** The read is a request, and the bus records every request
  with the app as sender.

## What stays elsewhere

Joins across tables, ad-hoc datasets and external `url()` consumers keep
the loopback HTTP endpoint; play's dispatcher needs a URL, and a SQL
console is not a fixed statement over one table.
