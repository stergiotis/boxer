---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to bind query parameters with `chclient`

This recipe covers server-side parameter binding for runtime services that read
ClickHouse over `public/keelson/data/chclient` — passing values (ids, search
terms, id sets) to a statement without concatenating them into the SQL text.

## When to use this recipe

- You are a **runtime service or app** issuing a fixed set of SELECTs, and one
  or more of them takes a value that is not a compile-time constant.
- Any part of that value originates outside your code — a user's search box, a
  selected row id, a set of keys accumulated during a traversal.
- You do **not** want `apps/play`'s `Client`. That one carries the nanopass
  pre-execute pipeline, the Leeway name resolver, Arrow lanes and dataset alias
  rewriting, all of which the playground needs and a fixed-query service does
  not. `chclient` is the small client for exactly this case.

If you are writing SQL *inside* the playground, stop here — `play.Client`
already binds parameters, and it harvests `SET param_*=…` statements out of the
buffer for you (see `apps/play/play_extract_params.go`).

## The mechanism

The ClickHouse HTTP interface reads bound values from URL query fields named
`param_<name>`, and substitutes them into `{<name>:Type}` placeholders in the
statement. The statement text never changes shape, so a value can never be
reinterpreted as SQL syntax — the property that makes untrusted input safe.

`Client.QueryParams` is `Client.Query` plus that channel:

```go
c := chclient.New(chclient.Config{
    URL:      clickhouseenv.Endpoint.Get(),
    User:     clickhouseenv.User.Get(),
    Password: clickhouseenv.Password.Get(),
}, nil)

body, err := c.QueryParams(ctx,
    `SELECT name FROM dspl.facts11
     WHERE positionCaseInsensitiveUTF8(name, {q:String}) > 0
       AND id IN {kids:Array(UInt64)}
     FORMAT JSONEachRow`,
    map[string]string{
        "q":    userTypedSearchTerm, // never concatenated into the SQL
        "kids": "[101,102,103]",
    })
if err != nil {
    return err
}
defer body.Close()
```

Rules for the map:

- **Keys are bare placeholder names.** `params["q"]` binds `{q:String}`; the
  `param_` prefix is added for you and is a URL-side marker only.
- **Values are the raw ClickHouse text form** for the placeholder's declared
  type — an unquoted string for `String` (no quoting, no escaping: the channel
  is not the SQL text), `[1,2,3]` for `Array(UInt64)`, a decimal for `UInt64`.
- **A nil or empty map behaves exactly like `Query`** and leaves the URL
  untouched, so there is no cost to routing every read through `QueryParams`.

For composite values beyond arrays and scalars, build the literal with
`public/db/clickhouse/dsl/marshalling`'s `MarshalTypedLiteralToSQL` rather than
by hand — see [leeway-marshalling.md](./leeway-marshalling.md).

## What is *not* bindable

Parameters bind **values**, never identifiers. A table, database or column name
cannot ride this channel — `FROM {tbl:String}` is not valid ClickHouse. When
the target table is dynamic:

- Prefer resolving it to one of a fixed, code-controlled set and switching on it.
- If it genuinely must be caller-supplied, validate it against a schema query
  before interpolating, and treat that interpolation as the security boundary
  it is.

## Errors and logging

`QueryParams` reports transport failures with the **configured** URL, not the
request URL — the request URL carries the bound values, which are caller data
and must not land in logs. Keep that property if you extend the package: a
bound search term in a log line is the leak this design avoids.

A non-200 from ClickHouse is returned as a structured `eb` error carrying the
status and the response body, which is where ClickHouse names the offending
placeholder if a binding is missing or ill-typed.

## Why this lives in `chclient` and not in the caller

`Client.postSQL` and `Client.queryURL` are unexported, and the request URL is
built inside them. There is no seam through which a consumer of the package can
add URL query fields to a `Query` call — so a caller wanting parameter binding
would have to re-implement the whole POST path (headers, status handling, body
lifetime), which is precisely the duplication `chclient` exists to prevent.
The binding channel therefore belongs to the package that owns the URL.
