---
type: adr
status: proposed
date: 2026-09-22
---

> **Status: proposed — pre-human-review.** Built alongside the proposal; the
> decision awaits a reader. Do not cite as authoritative.

# ADR-0253: Introspection table reads as a bus capability — `keelson.query.<table>`

## Context

Two windows read `keelson()` tables to do their job: the watchbill window
reads `watchbill_event` and `watchbill_worker` (ADR-0236 §SD1) and the
app-state manager reads `app_state` (ADR-0185 §SD1). Both ADRs chose an
introspection table over a client verb so that the same read would not live
on two surfaces, and both then reached the table the one way there was: the
process-global `introspect.LocalQueryEndpoint()`, a `chclient` over loopback
HTTP, the `/query` handler's `url()` rewrite, and a clickhouse-local worker
that fetched the table back from the same server. Four things about that
route are not what the capability model of ADR-0026 is for:

- **The reader is not attributed.** The `/query` runner publishes to the
  chlocal broker as `runtime.introspect.query`, so the broker's audit row
  names the runner for every read from every app, and the bus audit never
  sees the app at all — the app never touched the bus.
- **Nothing is declared or granted.** Neither manifest names the read, the
  broker cannot prompt for it, and `keelson('client_caps')` cannot list it.
  The capslock gate reported `CAPABILITY_NETWORK` drift for both apps with
  no subject filter to justify it; ADR-0185's build note left that open.
- **Discovery is a process-global.** The endpoint URL is handed to whoever
  asks, the shape ADR-0185 Q2 rejected for the delete seam.
- **Availability rides the socket.** With `KEELSON_INTROSPECT_ENABLE`
  falsey, or the bind failing, the windows say "no endpoint" while the
  providers are alive in the same process.

Two facts about the tree shaped the answer. ADR-0026 §SD3 designed
`ch.query.{db}` as a bus subject and nothing was ever bound to it; the
chlocal broker's `ch.local.exec.<pool>` is the only SQL-over-bus family that
was built. And the ADR-0094 §SD4 in-process engine — bare-name rewrite,
nanopass projection, `InputTables` to the broker — was built and tested but
had no production caller; the HTTP path used the `url()` rewrite instead.

## Design space (QOC)

**Q1 — How does a window reach a table it has a fixed statement over?**

- *O1 — the app runs the §SD4 engine itself*, declaring
  `ch.local.exec.introspect`. Killed: the grant is the raw pool — any SQL,
  any input table — and every app carries nanopass and a registry it
  should not hold.
- *O2 — a request/reply family served by the host over the §SD4 engine.*
  Chosen.
- *O3 — client verbs* (`watchbill.job.events`, `runtime.appstate.list`).
  Killed by the decisions already taken: one read on two surfaces, and a
  codec per table.
- *O4 — keep HTTP, declare a `net.` cap for it.* Killed: the endpoint has
  no authentication, so a grant would enforce nothing; it would only turn
  the capslock finding green.

**Q2 — What is the grant's granularity?**

- *One blanket subject, `ch.query.keelson`.* Killed: `app_state` lists
  every app's keys and `env` its redacted secrets; a window that reads the
  watchbill trail would hold both, and the broker prompt could say nothing
  more specific than "read introspection".
- *One subject per table.* Chosen — SD1. Both windows already issue
  single-table statements, and joins have a home (SD5).

| criterion | O1 | O2 | O3 | O4 |
| --- | --- | --- | --- | --- |
| the app is the audited sender | + | + | + | − |
| declared, prompted, capslock-visible | + | + | + | + (cosmetic) |
| least privilege | −− (the pool) | + (per table) | ++ (per verb) | −− |
| new surface | small | one family, two codecs | one verb per read | none |
| works without the HTTP source | + | + | + | − |

## Decision

### SD1 — One subject per table

`keelson.query.<table>` is a request/reply family, read-only. An app
declares `keelsonquery.ClientCaps(tables...)`: one `CapDirectionPub` filter
per table, **sticky**, since reading a table redacted at its provider
(ADR-0094 §SD3) is the low end of what an app can ask for and one prompt per
table per install is proportionate. The host's service holds `SubjectAll`
(`keelson.query.*`) on the bus identity `runtime.introspect.query`, the
identity the HTTP `/query` runner already publishes under — both are the
same plane answering the same question by two transports.

The table names are exported constants beside their providers
(`watchbill.TableEvent`, `watchbill.TableWorker`, `providers.TableAppState`),
so a manifest and a provider cannot drift apart.

### SD2 — Wire forms

Two generated codecs (ADR-0042), the shape of the appstate seam:
`keelsonQueryRequest` carries the table, the statement and the FORMAT; the
table in the subject and in the payload must agree. `keelsonQueryReply`
carries `Ok`, the shared `reason`, the content type and the body bytes in
the request's FORMAT. Rows are not decoded on the wire: a small reader
asks for JSONEachRow, an Arrow consumer can ask for ArrowStream. Vocabulary:
the `kqReq…` / `kqReply…` cohort in vdd, ordinals 218–223.

### SD3 — The gate

The service holds a statement to its subject's table before the engine
sees it, default-deny throughout (the R5 discipline play's dispatcher
applies, ADR-0141):

- `keelson('x')` macros are expanded to bare names first; an unknown table
  is refused there.
- the statement must classify as provably read-only;
- it must parse, and its relations resolve through scopes so a CTE or a
  subquery is not mistaken for a table;
- every remaining relation must be the subject's table: a second table, a
  qualified name (`system.x` on the worker is not the grant) and a table
  function (`url()`, `file()`, `remote()`) are each refused by kind.

An unparseable statement is refused rather than run, because the engine's
conservative fallback for one is to snapshot every registered table
(ADR-0094 §SD4) — exactly the width a per-table grant exists to prevent.

### SD4 — The service over the in-process engine

`keelsonquery.Service` answers on the ADR-0094 §SD4 engine: the referenced
table is snapshotted in-process, projected to the referenced columns, bound
as an input table and run on the chlocal pool. No socket is involved, so the
reads work wherever the pool does. `introspecthost.Start` stands the service
up when a bus and the chlocal broker are present, before and independently
of the HTTP table source, and folds it into the same stop.

A sealed dataset (ADR-0145) is refused by the engine as before: it is served
by handle on the loopback plane, and a bus read of one would hand back
ciphertext.

### SD5 — What stays on HTTP

The loopback `/query` endpoint, `LocalQueryEndpoint`, and the `url()` table
source are unchanged. They serve the SQL-console class — play, sqlapplet,
the vdd window host — whose contract is "a ClickHouse HTTP endpoint" and
whose ADR-0141 dispatcher routes by URL; they serve joins across tables,
ad-hoc datasets by handle, and external engines. That those consumers
reach the plane without a declared cap is the same gap this ADR closes for
the fixed-statement class, and is recorded rather than closed here.

### SD6 — The two windows

The watchbill window declares `TableEvent` and `TableWorker`; the app-state
manager declares `TableAppState`. Each reads through
`keelsonquery.Rows[T]` over the bus the host minted for it, keeps its row
types and statements, and shows "no bus to read through" only where the
host minted none. `apps/capinspector` gains the family's description and
its audit classifier.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| vdd vocabulary registry | `kqReqTable`…`kqReplyBody` (218–223) | the assignments golden |
| Codec kinds | `keelsonQueryRequest`, `keelsonQueryReply` | kindcheck; `scripts/dev/generate.sh`; the goldens |
| `keelson.query.>` subject family | new, read-only (SD1) | ADR-0026 §SD3 taxonomy; capinspector's registry, classifier and help page |
| `Manifest.Caps` of `apps/watchbill`, `apps/appstate` | +one sticky grant per table read | the broker prompt copy; the capslock app set (findings removed) |
| `introspecthost.Start` | +the service, before the HTTP source | nothing — the host's wiring call is unchanged |
| Provider table names | exported constants | any reader spelling a name by hand |

## Alternatives

The QOC section carries the killed options. One more was weighed and left
for later: giving `Engine.Query` the `param_*` channel (ADR-0133) so a
request could bind placeholders instead of escaping inline. Neither window
needs it, and the wire has room for it.

## Consequences

### Positive

- A read is a declared, prompted, audited request with the app as sender,
  and the capslock gate has nothing to report for either window.
- The grant names one table, so the prompt says what is read.
- The §SD4 engine has a production caller, and a window reads without a
  socket: the HTTP source can be off and the read still answers.

### Negative

- One transport bound: a bus request is one reply, not a stream, and the
  default request timeout (5 s) is the reader's ceiling; a mid-flight
  cancel does not reach the broker (the ExecOnPool contract).
- The gate lives on the bus path only. The HTTP endpoint still runs any
  statement a loopback client sends it; SD5 records that as the standing
  gap it is.
- A statement over two introspection tables has no bus route. That is by
  design (SD1), and the refusal says so.

### Neutral

- The bus dispatches a handler inline in the requester's goroutine, so a
  read costs the requester the engine's run; both windows already read on
  an off-frame poller.
- Sealed datasets remain HTTP-only, as the engine already required.

## Migration — Tier 1

The two windows are the only readers and are moved here; nothing else
called `newEndpointClient` / `newEndpointReader`. A window test that stood
up an `httptest` server now subscribes a stub on the bus that answers
`keelson.query.*`, which also exercises the request's table and format
fields.

## Verification plan — Tier 1

- **Lane: default `go test`.** `introspect/keelsonquery`: the gate admits
  both spellings, a CTE and a subquery over the granted table, and refuses
  a second table, a qualified name, a table function, a mutation, an
  unknown table and an unparseable statement, each with its reason; over a
  real chlocal broker (skipped without a clickhouse binary) a granted
  reader gets rows and an audit record with itself as sender, a refusal
  comes back as a reply with the reason, and a missing grant fails at the
  transport. The two windows read through a stub service; the manifests
  declare the grants.
- **Lane: capslock gate.** `TestAnalyse_MatchesBaseline` no longer lists
  `apps/appstate` or `apps/watchbill`.
- **Live.** Not yet done: the broker's Mount prompt for a sticky table
  grant, and both windows reading on the desktop host.

## Status

Proposed 2026-09-22; built the same day, all six SDs.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — §SD3 the subject taxonomy this family joins, §SD7 the broker that prompts, §SD10 the capslock cross-check.
- [ADR-0094](./0094-keelson-introspection-tables.md) — §SD1 the providers, §SD3 the HTTP source that stays, §SD4 the engine this serves.
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — the chlocal broker the engine runs on.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the R5 default-deny discipline the gate copies.
- [ADR-0145](./0145-sealed-app-data.md) — why a sealed dataset is refused on this path.
- [ADR-0185](./0185-durable-app-state-manager.md) — §SD1 the read this routes; the build note that left the capability open.
- [ADR-0236](./0236-watchbill-management-app.md) — §SD1 the two reads this routes.
- [ADR-0042](./0042-keelson-leeway-codec-soa-generator.md) — the codec generator.
