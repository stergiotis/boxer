---
type: adr
status: proposed
date: 2026-11-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0377: Query endpoints a process serves to its own apps are listed by name

## Context

A window that runs SQL has one target by default: the ClickHouse the
environment names. The keelson introspection plane added a second, served
by the process itself: a loopback `/query` endpoint that rewrites
`keelson('<name>')` and lets clickhouse-local run the statement
([ADR-0094](./0094-keelson-introspection-tables.md) §SD6). A
co-resident app finds it through one process-wide hook,
`introspect.LocalQueryEndpoint`, and play offers it as a preset; a launch
config and an applet name it as `endpoint: introspection`
([ADR-0135](./0135-app-launch-requests.md),
[ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md)).

The hook holds one URL. A downstream binary now serves an endpoint of the
same shape: a gateway that runs house table functions in Go and forwards
the statement to ClickHouse. Its apps can reach it only by having the
whole process's ClickHouse target swapped for it in the environment, and a
dashboard that reads it cannot say so. A second hook per endpoint would
repeat that for every endpoint and still not say how to authenticate.

Such an endpoint listens on loopback, which every local process and
browser tab shares. The introspection plane asks for no credential; an
endpoint that reaches personal data cannot do the same, and a ClickHouse
user with a password is more than an app in the same process should need.

## Decision

**SD1 — A registry lists the query endpoints this process serves.**
`public/keelson/runtime/queryendpoint` holds, per endpoint, a name, the
loopback URL of an HTTP interface in ClickHouse's dialect, a display name
and a summary, and the credential a co-resident client presents (SD3). It
is process-wide and in memory, like the hook it generalises. The server
publishes its entry once it listens and retracts it when it stops.

**SD2 — A name is what a document names.** Lowercase letters, digits and
hyphens. `default` is reserved for the target the environment names, which
no entry describes. The introspection plane publishes itself as
`introspection`, the word launch configs and applets already use. A name
is published once; a second publish of a live name is refused.

**SD3 — An endpoint may mint a credential for the process.** An endpoint
that must not serve every local caller mints a secret when it starts and
publishes it in its entry as a user and a password. Holding it means
running in this process. The endpoint decides what a caller presenting it
may do and as whom it acts. The secret is never logged, never persisted
and never a column of an introspection table.

**SD4 — The existing hook stays.** `introspect.LocalQueryEndpoint` and the
predicates published beside it keep their callers: play's dispatch reads
them to place a run on the introspection plane and to judge the locality
wall ([ADR-0145](./0145-sealed-app-data.md) §SD5). The registry adds a
list; it does not move those decisions.

**SD5 — Being listed exempts nothing.** A listed endpoint is a target like
a typed URL. One that forwards statements to an engine outside the process
is judged by the locality wall as that engine is: a statement naming
sealed data is refused there unless the target has proven it reaches this
plane.

**SD6 — play lists the endpoints and sends each its own credential.**
play's Endpoint menu offers every listed endpoint after the introspection
preset. play pins a URL, so the credential follows the URL: a request to
an address an entry lists carries that entry's user and password, and a
request to any other address carries the configured ones. The label beside
the menu names the user the pinned target is asked as.

## Alternatives

- **One more process-wide hook per endpoint.** No list for a menu, no
  place for a credential, and a new exported function per endpoint.
- **An introspection table of endpoints.** Fits the runtime's habit of
  showing what is live as rows, and can follow (Deferred). It cannot be
  the registry: a table is readable by everything that can query the
  plane, and the credential must not be.
- **A bus subject that answers the endpoints.** A request and a reply for
  what is a read of process memory; the consumers are in the process by
  definition.
- **The endpoint's own port in the environment.** The port is ephemeral by
  default and known only at run time, which is why the hook exists.

## Consequences

- A window can offer every endpoint the process serves without knowing
  which binary it is linked into; a document can name one. A downstream
  dashboard viewer resolves a datasource reference through the list.
- An endpoint that mints a credential needs no account on the database for
  the process's own apps.
- Anything running in the process can read a minted secret. That is the
  boundary SD3 draws, and it is the one the introspection plane already
  has.
- Two endpoints cannot share a name; the second to start fails its
  publish and says so.

## Deferred

- **Launch configs and applets name any listed endpoint.** Today the
  field admits `default` and `introspection`. Trigger: the first applet
  that reads a downstream endpoint.
- **`keelson('query_endpoints')`**, without the credential. Trigger: the
  first question about which endpoints a running host serves.

## Status

Proposed 2026-11-17.

## References

- [ADR-0094](./0094-keelson-introspection-tables.md): the
  introspection plane and its `/query` endpoint.
- [ADR-0145](./0145-sealed-app-data.md): the locality wall.
- [ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md):
  the playground's launch config and its endpoint field.
