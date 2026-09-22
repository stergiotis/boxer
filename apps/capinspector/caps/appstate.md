---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the appstate cap. Refine once the manager window that
> holds it lands.

# appstate — clearing what other apps stored

[ADR-0185](../../../doc/adr/0185-durable-app-state-manager.md) gives a user a
way back from state an app kept: a persisted value, a saved workingset, a
column dragged to a width. Every kind lives on one store, `boxer.persiststate`
([ADR-0105](../../../doc/adr/0105-keelson-adopts-generated-record-stores.md)),
and this capability is the only path by which one app clears another's
entries there.

A manager app declares the client set and clears through the typed client:

```go
Caps: appstate.ClientCaps(),

func (inst *App) Mount(ctx app.MountContextI) error {
    inst.state = appstate.NewClient(ctx.Bus())
    return nil
}

res, err := inst.state.Delete(appId, "column_width", "instance/master/table/ab12", "")
res, err = inst.state.Forget(appId)
```

An entry is named the way `keelson('app_state')` shows it: its kind and its
key, or its store key for a row of a kind the host does not know. A delete
that does not address a live entry of that app and kind is refused.

## What the schematic shows

- **Subjects.** `runtime.appstate.delete` and `runtime.appstate.forget`,
  request/reply with generated codecs. There is no list verb: reading is
  the introspection table, so the same read does not live on two surfaces.
- **Backends.** One service, under the id `runtime.appstate`, over the
  durable state backend. With no durable store it answers every request
  with a refusal that says so.

## What guards it

- **Consent.** `ClientCaps()` is not sticky, so the broker asks on every
  Mount — a remembered, silent grant to clear every app's state is what
  should not exist.
- **Audit.** The verbs are requests, and the bus records every request,
  a refused one included. A publish would go unrecorded.
- **No early stop.** `forget` attempts every entry and reports per kind
  what was cleared and what failed, so a partial clear is visible as one.

A clear is a tombstone, not erasure: the superseded rows stay in the trail.
Erasure is the pushout work's vocabulary, and nothing here is wired to it.

## Where the rows are read

- `keelson('app_state')` — every live entry of every kind, with its size
  and its writer, never its bytes.
- The `appstate` applet book.
