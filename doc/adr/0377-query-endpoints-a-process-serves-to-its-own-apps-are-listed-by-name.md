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
repeat that for every endpoint.

## Decision

**SD1 — A registry lists the query endpoints this process serves.**
`public/keelson/runtime/queryendpoint` holds, per endpoint, a name, the
loopback URL of an HTTP interface in ClickHouse's dialect, a display name
and a summary. It is process-wide and in memory, like the hook it
generalises. The server
publishes its entry once it listens and retracts it when it stops.

**SD2 — A name is what a document names.** Lowercase letters, digits and
hyphens. `default` is reserved for the target the environment names, which
no entry describes. The introspection plane publishes itself as
`introspection`, the word launch configs and applets already use. A name
is published once; a second publish of a live name is refused.

**SD3 — An entry carries no credential.** A client sends a listed
endpoint the credential it sends the target the environment names. An
endpoint that forwards statements to that target passes it on; one that
needs none, as the introspection plane, ignores it. An endpoint that
wanted callers of its own process told apart from other local callers
would need more than this; none does (Alternatives).

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

**SD6 — play lists the endpoints.** play's Endpoint menu offers every
listed endpoint after the introspection preset, and pins its URL like a
typed one. play's one configured credential goes to whatever is pinned.

## Alternatives

- **One more process-wide hook per endpoint.** No list for a menu, and a
  new exported function per endpoint.
- **An entry that carries a credential the endpoint minted for the
  process.** Built first, for a gateway that served database users by
  name and wanted its own host's windows admitted without one: a secret
  in the entry, and play sending each pinned address the credential of
  its entry. The gateway then stopped asking anything of a caller, and
  the mechanism had no user left; it was removed the same day rather
  than kept for a case nobody has.
- **An introspection table of endpoints.** Fits the runtime's habit of
  showing what is live as rows, and can follow (Deferred). The consumers
  are in the process and want a function call, not a query.
- **A bus subject that answers the endpoints.** A request and a reply for
  what is a read of process memory; the consumers are in the process by
  definition.
- **The endpoint's own port in the environment.** The port is ephemeral by
  default and known only at run time, which is why the hook exists.

## Consequences

- A window can offer every endpoint the process serves without knowing
  which binary it is linked into; a document can name one. A downstream
  dashboard viewer resolves a datasource reference through the list.
- A listed endpoint that forwards to the database is reached with the
  rights the client already has there, no more.
- Two endpoints cannot share a name; the second to start fails its
  publish and says so.

## Deferred

- **Launch configs and applets name any listed endpoint.** Today the
  field admits `default` and `introspection`. Trigger: the first applet
  that reads a downstream endpoint.
- **`keelson('query_endpoints')`**. Trigger: the first question about
  which endpoints a running host serves.

## Status

Proposed 2026-11-17.

## References

- [ADR-0094](./0094-keelson-introspection-tables.md): the
  introspection plane and its `/query` endpoint.
- [ADR-0145](./0145-sealed-app-data.md): the locality wall.
- [ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md):
  the playground's launch config and its endpoint field.
