---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the http cap.

# http — fetching from a registered destination

[ADR-0262](../../../doc/adr/0262-http-egress-as-a-keelson-capability.md)
makes HTTP egress from an app a declared capability. Before it, the map
widget's tile loader held its own HTTP client, configured through its
options; no manifest said where the app talked to and no audit row named
the app.

A destination is registered in host code — the basemap package registers
`basemap` from `BOXER_MAP_TILE_*` — and an app declares the ones it uses:

```go
Caps: basemap.ClientCaps("terrainscope: basemap tiles"),

func (inst *App) Mount(ctx app.MountContextI) error {
    inst.web = httpegress.NewClient(ctx.Bus())
    return nil
}

res, err := inst.web.Fetch(ctx, "basemap", httpegress.Request{URL: url, Purpose: "tiles"})
```

## What the schematic shows

- **Subjects.** `net.http.fetch.<destination>`, request/reply, one per
  destination. The request carries a method (GET or HEAD) and an absolute
  URL; the reply carries the status, the content type and the body.
- **Backends.** One service, under the id `runtime.http`, holding one
  transport per destination. A destination that failed to resolve stays
  listed and is refused with the reason.

## What guards it

- **The grant names the destination.** The bus refuses a request on a
  destination the app did not declare; an app cannot create a destination.
- **The URL stays inside.** The service matches every URL, and every
  redirect, against the destination's prefixes; userinfo and dot segments
  are refused.
- **The sensitivity wall.** A request composed from sealed data
  (ADR-0145) is refused unless every prefix of the destination is
  loopback. The label is the caller's declaration.

## Where the calls are read

- `keelson('http_destinations')` — every destination any app could name:
  prefixes, trust policy, agent, limits, and why it is unavailable if it is.
- `keelson('http_calls')` — every fetch this process answered or refused:
  app, destination, purpose, URL, status, bytes, elapsed, how it ended.
