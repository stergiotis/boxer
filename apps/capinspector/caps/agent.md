---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the agent cap.

# agent — operating apps on the person's behalf

[ADR-0269](../../../doc/adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
lets an app declare the commands and queries it offers, and lets a caller —
a coordinator running a model's tool loop — reach them through the host
rather than through the app's own subjects. An app declares its catalog in
its manifest with the typed helper in `appops`:

```go
var ops = appops.NewSet(func(inst *App) snap { return inst.snapshot() })

func init() {
    ops.Resource("doc", "the document text", func(inst *App) any { return inst.doc })
    appops.Command(ops, app.OperationSpec{Name: "set_doc", Version: 1,
        Summary: "replace the document", Effect: app.OperationEffectDocument,
        Writes: []string{"doc"}, Agents: true}, setDoc)
}

var manifest = app.Manifest{ /* … */ Operations: ops.Catalog() }
```

A caller holds `agent.ClientCaps(reason)` and asks the host:

```go
apps, err := agent.NewClient(ctx.Bus()).Describe(ctx, agent.DescribeRequest{Search: "document"})
```

## What the schematic shows

- **Subjects.** `runtime.agent.<service>`, request/reply. `describe` lists
  the operations agents may call, without schemas; naming one operation
  returns it with the JSON Schemas of its arguments and result.
- **Backends.** One service, under the id `runtime.agent`.

## What guards it

- **The app decides what is exposed.** An operation reaches agents only when
  its declaration says so; everything else stays visible to people in the
  table and invisible to `describe`.
- **A bad catalog costs only the catalog.** Registration validates it — names,
  classes, effects, the resources it names, the argument and result types —
  and withdraws it with a diagnostic; the app still registers.
- **The grant.** Every service past `describe` and `request` is checked
  against a task grant the person approved (ADR-0269 §SD6); the bus grant
  above is necessary and not sufficient.

## Where the catalog is read

- `keelson('app_operations')` — every registered catalog, one row per
  operation: class, effect, the resources it reads and writes, whether agents
  may call it, the schemas, and for a withdrawn catalog the diagnostic.
