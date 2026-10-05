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
  returns it with the JSON Schemas of its arguments and result. `request`,
  `call`, `status`, `cancel`, `read`, `capture`, `list`, `turn`, `detach`
  and `stop` work under a task grant.
- **Events.** `runtime.agent.event.<task>`: what changed in a window the
  task works in, by whom, and whether it paused the task — never the
  content. A coordinator that misses events reads again.
- **Operation subjects.** `app.<alias>.<instance>.op.<name>`: the
  dispatcher's call to one window. Only the host publishes and subscribes
  there; registration refuses an app whose capability could reach them.
- **Backends.** The dispatcher under the id `runtime.agent`; the window
  host's side of the operation subjects under `runtime.windowhost.ops`.

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

## Where it is read

- `keelson('agent_grants')` — every task grant this process issued: the
  actor, the instances and modes, the budget used, and how it ended.
- `keelson('agent_actions')` — one row when the dispatcher decides a call
  and one at its final phase: operation, effect, phase, reason, the budget
  left.

## Where the catalog is read

- `keelson('app_operations')` — every registered catalog, one row per
  operation: class, effect, the resources it reads and writes, whether agents
  may call it, the schemas, and for a withdrawn catalog the diagnostic.
