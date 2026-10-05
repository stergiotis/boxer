---
type: adr
status: proposed
date: 2026-09-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0262: HTTP egress as a keelson capability — `net.http.fetch.<destination>`

## Context

[ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) O3 named a keelson
HTTP facility as the place tile fetching should end up, and declined to
draw it because "a boundary drawn to fit a basemap's needs is the wrong
boundary". [ADR-0204](./0204-leaflet-map-core-port.md) §SD4 moved tile
fetching into Go and left the facility as Q5. [ADR-0254](./0254-model-inference-as-a-keelson-capability.md)
took model inference out of its scope: a model is a resource with
semantics of its own, gated as `llm.*`.

What is left, surveyed on 2026-09-26: outbound HTTP from code that runs
inside a hosted app is, apart from one consumer, ClickHouse's HTTP
interface — play's client, `chclient` under jackstay, mdedit, tally and
imztop, the query engine's adapter. Those are the `ch.*` family's business
(ADR-0026 §SD3) and are not this decision's. The one consumer that is
plain web egress is portolan's tile loader, reached by play (Map, Graph,
Vector field), terrainscope and the gallery, and pointed at
tile.openstreetmap.org unless `BOXER_MAP_TILE_URL` says otherwise. The
command-line tools that fetch (the swisstopo mirror, the text2sql CLI)
are host-side code and stay as they are.

That consumer has the properties the capability model exists to state:

- **Nothing declares it.** No manifest names a tile server; the capslock
  gate sees nothing because `net/http` is reached through a widget
  package, a non-stdlib hop (ADR-0026 §SD10, 2026-07-15 update).
- **Transport policy lives in a widget.** The TLS knobs
  (`BOXER_MAP_TILE_CA_FILE`, `_INSECURE_TLS`), the user agent, the
  timeout and the body cap are `LoaderOptions` fields, re-expressed per
  consumer — the tax ADR-0165 described, moved from IDL to Go.
- **No record.** Which app fetched what, from where, and whether it
  failed, is a log line at best.
- **The URL is data.** A tile URL carries the viewport; a map fitted to a
  sealed dataset's extent tells the tile server where that data is.

## Design space (QOC)

**Q1 — What does an app's grant name?**

- *O1 — one coarse cap, the service holds a URL allowlist.* Killed: the
  grant says nothing about where the app talks to, and the allowlist is a
  second configuration nobody reads beside the manifest.
- *O2 — the origin in the subject* (`net.http.fetch.<escaped-origin>`).
  Killed: hosts carry dots, so every grant is an escaped string, and TLS
  policy per origin still needs a home the subject does not give it.
- *O3 — named destinations the host registers.* Chosen. The
  `ch.query.{db}` shape: the subject names a host-side resource by a
  short name, and what the name resolves to — URL prefixes, trust, agent,
  limits — is configured once, where transport policy already lives.

**Q2 — May an app fetch a URL no destination covers?**

- *An `open` destination for any https URL.* Deferred (SD7): consent to
  "anything" means something only with a per-request prompt, and the host
  mints manifest caps without one (ADR-0253's live check).
- *No.* Chosen for the first cut.

**Q3 — Does portolan keep a direct `net/http` path for use outside a
host?**

- *Keep a fallback transport.* Killed: the widget package would still
  reach the network, the policy would still be re-expressed in it, and a
  consumer could keep using the fallback indefinitely.
- *Bus only.* Chosen. Every consumer of the loader is a hosted app; a test
  hands the loader a fake fetcher.

| criterion | O1 | O2 | O3 |
| --- | --- | --- | --- |
| the grant names where the app talks to | −− | + | + |
| transport policy stated once | + | − | ++ |
| subjects readable in a manifest | + | −− | ++ |
| new surface | an allowlist | an escaping rule | a registry |

## Decision

### SD1 — One verb per destination: `net.http.fetch.<destination>`

A request/reply family under the `net.` prefix that ADR-0026 §SD10 already
reserved for network capability. The final token is a destination name
(`[a-z0-9_-]+`, no dots). An app declares
`httpegress.ClientCaps(destination, reason)` per destination it uses:
publish direction, sticky — a tile server the deployment configured is
not a per-session consent. The bus's publish check is the enforcement: a
client without the cap cannot put the request on the subject.

The request carries a method, an absolute URL, a purpose and a
sensitivity label; the reply carries the status, the content type and the
body. A non-2xx status is a completed exchange, not a failure: HTTP
semantics are the caller's. Methods are GET and HEAD; request bodies,
request headers beyond what the destination sets, and streaming are
deferred (SD7).

### SD2 — Destinations are a code-registered, host-wide registry

A destination is a name, a description, a set of allowed URL prefixes
(scheme, host and path prefix; a tile template's `{s}` subdomains expand
to one prefix each), a trust policy (a CA file, or insecure with the
floor lowered as portolan's loader does it), a user agent, a timeout and
a body cap. Packages register destinations at init, the way ADR-0009's
registry holds environment variables, each with a resolve function the
host calls once at start — so a destination's configuration comes from
the environment registry and is visible in `boxer env list`.

An app cannot create a destination at run time. It can only name one it
declared, and the service matches the request URL against that
destination's prefixes after normalisation (userinfo refused, `..`
segments refused, scheme and host compared exactly). What a hosted app
can reach is therefore the union of destinations its manifest names, and
the full list of what any app could reach is a table:
`keelson('http_destinations')`.

First registrations: `basemap` from `BOXER_MAP_TILE_*` (the variables keep
their names and meanings, and the TLS knobs keep their rule of applying
only to a deployment's own server), and the gallery's alternative tile
servers as a destination of the gallery's own.

### SD3 — One service holds the transports

`runtime.http` (package `public/keelson/runtime/httpegress`) resolves the
registry at host start, builds one `http.Client` per destination, and
answers the family. It is where `net/http` lives for hosted apps — host
code, where a network capability is expected. A destination that failed
to resolve is refused with the reason, so a consumer is told rather than
left to a timeout, the posture of `llm.*` and `runtime.appstate`.

### SD4 — The sensitivity wall

The same rule as ADR-0254 §SD3 and the query dispatcher: a request labelled
confined (ADR-0145 §SD3) is refused unless the destination is local
(every prefix's host is loopback). The label is the caller's declaration;
a consumer that fetches for a view composed from query results forwards
the run's label.

### SD5 — Every call is a record

The service keeps a bounded in-process record of calls — app, instance,
destination, purpose, method, URL without its query (a tile server's
key rides there), status, bytes, latency, sensitivity, the refusal or
error — served as `keelson('http_calls')`. The bus audits
the request as well. Each call also lands on `boxer.facts` as an
`httpFetch` row of the audit trail ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD2), with the window
that asked and, for agent-caused work, the task and the dispatcher's call.
A screenful of tiles is dozens of rows, so they are flushed behind the
fetches rather than ahead of them; a per-destination rollup is a reader's
query over those rows.

### SD6 — portolan fetches through the bus

The tile loader's HTTP client is replaced by a fetcher it is handed; the
TLS, agent and timeout options leave `LoaderOptions` for the destination.
A hosted app builds a `basemap.Tiles` over its bus — a GET against the
`basemap` destination — and `basemap.PortolanLoader` hands it to the
loader as its fetcher.
The loader keeps its worker pool, byte cache, negative cache and health —
those are about tiles, not transport. Sharing the byte cache across maps
and apps in the service (ADR-0165 Q1) is deferred.

### SD7 — Deferred, recorded

An `open` destination for arbitrary URLs, with a per-request prompt;
request bodies and headers (POST); streaming replies and bodies past the
cap; a shared response cache in the service; the durable call kind;
destinations configured from a file rather than code. In-app ClickHouse
HTTP is outside this ADR: it belongs under `ch.*`, and moving it is its
own decision.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `net.http.fetch.<destination>` subject family | new, request/reply (SD1) | ADR-0026 §SD3 taxonomy; capinspector's registry, classifier and help page |
| `httpegress` runtime package | new registry, service, client, wire (CBOR through `buscodec`) | hostboot `Services.HTTP`; the L12 id allowlist (`runtime.http`) |
| `keelson()` table set | +`http_calls`, +`http_destinations` | introspecthost deps |
| portolan `LoaderOptions` | −`Transport`, −`CAFile`, −`InsecureTLS`, −`UserAgent`; +`Fetcher` | every `portolan.New` caller; the loader tests |
| `basemap` | registers the `basemap` destination; `Tiles` (the app's fetcher) and `ClientCaps`; `PortolanLoader` takes a `Tiles` | play, terrainscope, the gallery demos |
| `Manifest.Caps` of play, terrainscope, the gallery | +`httpegress.ClientCaps("basemap", …)`; the gallery also its own destination | the cap-count pins |

## Alternatives

The QOC section carries the killed options. Two more were weighed:

- **Move in-app ClickHouse HTTP under this family.** Rejected: a query is
  not a fetch. `ch.*` gates by database and statement, and a destination
  grant over a ClickHouse server would admit any statement the server
  accepts — the O4 shape ADR-0253 killed for the introspection endpoint.
- **A loopback forward proxy the apps are pointed at** (ADR-0165 O4).
  Rejected on the same grounds as there: an unauthenticated listening
  socket, and nothing attributes a request to the app that sent it.

## Consequences

### Positive

- An app's tile traffic is declared, audited and attributed to the app,
  and the servers any app can reach are one table.
- Transport policy for a destination is stated once, in host code.
- The sealed-data rule covers the URL as well as a model's context.

### Negative

- A tile costs a bus round trip beside its HTTP round trip — an in-process
  hop and a CBOR copy of a body of at most a few hundred kilobytes.
- A new web consumer needs a destination registered in code before it can
  fetch anything; there is no escape hatch until SD7's `open` exists.
- The gallery's portolan demos need a hosted run; a demo rendered without
  a bus shows no tiles.

### Neutral

- `BOXER_MAP_TILE_*` keep their names and meanings; what changes is the
  code that honours them.
- Without a Mount-time prompt the sticky flag records intent only, as for
  every other cap.

## Verification plan — Tier 1

- **Lane: default `go test`.** The service against an `httptest` server:
  a URL outside the destination's prefixes is refused; `..` and userinfo
  are refused; a confined request is refused against a non-local
  destination and served against a loopback one; a body past the cap
  fails; a call lands one record; an app without the cap cannot publish.
  The loader's tests over a fake fetcher.
- **Lane: capslock gate.** No app package and no widget package reports
  `CAPABILITY_NETWORK` through portolan.
- **Live.** The gallery's portolan demo and play's Map pane show tiles
  under a host; `keelson('http_calls')` shows the rows with the app as
  sender.

## Status

Proposed — 2026-09-26. Answers ADR-0165 O3 and ADR-0204 §SD4 Q5.

Built the same day: `public/keelson/runtime/httpegress` (registry,
service, client, `Getter`, both tables), hostboot `Services.HTTP`,
capinspector's `http` entry; portolan's loader on a fetcher; the `basemap`
destination and `basemap.Tiles`; play's three map panes, terrainscope and
the gallery's map demos (with the `gallery-tiles` destination) declaring
their grants. `TestScenePortolanCamera` (integration lane) passes with
every tile through `net.http.fetch.basemap`. Open: no consumer forwards a
sensitivity label yet — play's map panes fetch as ordinary even over a
confined run (SD4 holds the rule; the panes do not carry the run's label
to their fetcher).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## Update — 2026-10-03: the browser tab fetches its basemap tiles itself

The browser tab (ADR-0263) has no runtime services, so no
`net.http.fetch.basemap` service answers there and every tile waited out the
bus's timeout. In tab builds (`GOOS=wasip1`) `basemap.Tiles` now fetches a
tile itself, over the tab's host transport, and the map panes start with
the basemap on (`basemap.DefaultOn`). That is a deliberate exception to this
ADR's rule that tiles leave only through the egress service: in the tab each
viewer's browser asks the tile server directly, the request carries no
sensitivity label and is not recorded, and the tile server sees the
viewer's own address. It was chosen over a same-origin tile proxy in the tab
server because it needs no server, and over the country-outline stand-in
because the outlines, re-sent as vectors every frame, cost the tab about
180 KB of draw commands per frame at a world view against about 13 KB with
tiles (measured once on 2026-10-03). Native builds are unchanged.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — §SD3 the taxonomy, §SD10 capslock and the `net.` prefix.
- [ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) (superseded) — O3, the facility this draws.
- [ADR-0204](./0204-leaflet-map-core-port.md) — §SD4, the Go tile loader and Q5.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the service shape and the sensitivity wall this copies.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — manifest caps without a Mount prompt.
- [ADR-0145](./0145-sealed-app-data.md) — the confined label.
- [ADR-0009](./0009-environment-variable-registry.md) — the registry destinations resolve from.
