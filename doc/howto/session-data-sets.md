---
type: how-to
audience: developer of an app that runs SQL for the person or for an agent's task
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to keep named data sets for a task

The task-oriented walk: let the statements your app sends name an
intermediate result once — `session('msgs')` — instead of repeating the
query that makes it, and join an ad-hoc dataset the host holds with server
tables in one statement. Why it is shaped this way is
[ADR-0406](../adr/0406-session-data-sets-named-reads-a-task-keeps-in-a-clickhouse-session-and-makes-again-from-their-definitions.md)
(proposed); the service is
[`public/keelson/runtime/sessiondata`](../../public/keelson/runtime/sessiondata).

One fact carries the page. **The definition is what lasts; the table is a
cache.** A data set is a `SELECT` and a name. The service makes its table in
a ClickHouse session it holds; when the server drops the session, the next
statement that reads the data set makes it again from the definition, in
dependency order, and the trail records the replay.

## 0. What the server needs

The server user the host runs under needs `CREATE TEMPORARY TABLE`. Under a
`readonly=1` profile the server refuses the create; `readonly=2`, which play
sends for an agent's run, admits it. The session timeout
(`BOXER_SESSIONDATA_SESSION_TIMEOUT`, default 15 min) must not exceed the
server's `max_session_timeout`. The endpoint must be one server, not a load
balancer: a session lives on one node.

## 1. Declare the capabilities

```go
// app_register.go
Caps: []app.SubjectFilter{
    sessiondata.DefineCap("let the person and agents keep named data sets"),
    sessiondata.PrepareCap("run statements that read them"),
    sessiondata.ReleaseCap("end a statement's hold on its session"),
    sessiondata.ListCap("show the data sets"),
    // for the person's own work:
    sessiondata.OpenCap("…"), sessiondata.AttachCap("…"), sessiondata.CloseCap("…"),
},
```

## 2. Know whose data session it is

- **An agent's call.** Pass the call's `app.OnBehalfOf` as
  `Caller.OnBehalfOf`. The service attests it and uses the task's data
  session; every window working for the task reaches the same one. No ticket
  is needed and one passed is ignored.
- **The person.** Open a data session for the window and keep its ticket:

  ```go
  ticket, err := sessiondata.Open(ctx, bus)
  ```

  Hand the ticket to another window — a launch argument, a hand-over
  payload — and that window calls `sessiondata.Attach(ctx, bus, ticket)`.
  The session lives while a window holds it. The ticket is a bearer
  capability: show it to nobody you would not let read the data.

A define, a list and a binding carry the data session's ticket, so a window
can let the person attach to the session a task built. A window attached
that way works in it as the person, without the task's grant.

## 3. Define a data set

```go
res, err := sessiondata.Define(ctx, bus, sessiondata.DefineInput{
    Caller:   sessiondata.Caller{OnBehalfOf: obo},  // nil for the person
    Ticket:   ticket,                                // the person's only
    Endpoint: sessiondata.Endpoint{URL: url, User: user, Password: password},
    Name:     "msgs",
    SQL:      "SELECT msg_id, sender, splitByNonAlpha(lower(body)) AS toks FROM enron.message",
    OrderBy:  []string{"msg_id"},
})
```

The `SELECT` may read server tables, other data sets as `session('name')`
and ad-hoc datasets as `keelson('alias')`. It must be a plain read without a
`FORMAT` clause or a `SET` prelude; the service refuses anything else before
sending, and sends it without its comments. The endpoint URL may not carry
`query` or a parameter the service sets (`readonly`, `session_*`,
`log_comment`, `default_format`, `param_*`, `*_format`, `*_structure`,
`*_types`). The table is made at once, so a definition that does not run is
refused here, and `res.Dataset` carries its columns, rows and bytes.
Defining a name again, on the same endpoint and identity, makes a new
revision and leaves the data sets that read it stale; they are rebuilt from
it when next read. To move a data set to another endpoint, drop it first.

For an agent's call the grant must list `clickhouse:<host>` and
`clickhouse-session:<host>`. A refusal for a missing one is a
`*sessiondata.GrantError`; pass its `Destination` to
`app.RefuseForDestinations` so the coordinator can ask the person.

## 4. Run a statement that reads one

`Prepare` before the statement is sent, `Release` after its response is
drained:

```go
b, err := sessiondata.Prepare(ctx, bus, sessiondata.PrepareInput{
    Caller: caller, Ticket: ticket, Endpoint: endpoint,
    SQL: "SELECT sender, count() FROM session('msgs') GROUP BY sender",
})
// … the app's own checks run on b.SQL, as on any statement …
req := queryengine.Request{SQL: b.SQL /* after the app's rewrites */, Settings: map[string]string{"readonly": "2"}}
err = b.Apply(&req)          // session settings, external tables, the label
st, res, err := eng.Deliver(ctx, req)
// … drain or close st …
_ = sessiondata.Release(ctx, bus, b.Lease)
```

- Give `Prepare` the statement as its author wrote it, before your own alias
  binding: data sets as `session('…')`, ad-hoc datasets as
  `keelson('alias')`, not as handles.
- A session runs one statement at a time. Until you release, the next
  `Prepare` in the same data session and endpoint waits; a lease not
  released lapses after `BOXER_SESSIONDATA_LEASE_TIMEOUT`, and one a window
  has out is released when the window closes.
- `b.Replayed` names the data sets made again first. A run that fails with
  `sessiondata.IsSessionLost(err)` met a session the server dropped after the
  prepare; prepare again.
- A statement naming neither macro comes back unchanged and holds nothing,
  so calling `Prepare` on every statement is safe; `sessiondata.References`
  tells you beforehand.

## 5. Join host data with server tables

A statement or a definition naming `keelson('alias')` gets the ad-hoc
dataset attached as external data, read under the dataset service's grant
rules. Ad-hoc datasets are sealed data
([ADR-0145](../adr/0145-sealed-app-data.md)): set
`Endpoint.ServesConfined` only for an endpoint you would let see them — the
same declaration `chserver.Config.ServesConfined` takes — or the statement
is refused. A data set defined from one is confined too, and so is every
statement that reads it. One statement attaches at most
`BOXER_SESSIONDATA_MAX_ATTACH_BYTES` together.

## 6. Look at what there is

`sessiondata.List(ctx, bus, ticket, obo)` returns the session's data sets
with definitions, sizes, table, endpoint and state (`live`, `expired`,
`stale`, `failed`). `keelson('session_datasets')` lists every data session's
data sets for the operator, without definitions or tickets. The trail has
one row per define, replay, expiry, drop, attach and close (domain
`sessiondata`).

## 7. Let go

`sessiondata.Drop` removes a data set; one that others read is refused unless
`Cascade` is set. A task's data session ends with the task; a person's when
its last window closes or one calls `sessiondata.CloseSession`. Ending drops
the tables; the server would drop them at the session timeout anyway.
