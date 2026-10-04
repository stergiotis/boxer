---
type: adr
status: proposed
date: 2026-10-03
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0274: SQL applets as windows an agent can operate

## Context

An SQL applet ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) is a
markdown document whose SQL fence is a query, published as an app of its
own: one manifest per applet, a window that embeds play with its editor and
chrome tabs removed, its parameter strip, and the result panes its
frontmatter names. Applets are the repository's curated, parameterised
queries; the books embedded in the build hold several dozen, and the applet
store holds the ones people saved.

The chat app's coordinator ([ADR-0265](./0265-chat-app-over-retained-model-calls.md)
§SD6) drives windows through the operations their apps serve
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).
Play serves a catalog — state, parameters, signals, runs, results, panes —
with limits on what an agent's run may reach
([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md)). An applet
serves none: ADR-0270 has only play's own launcher install the path, and an
app that embeds play serve its own catalog. So a model can find an applet
(`describe_app` with a search, since the update of ADR-0269 of 2026-10-03)
and open it, and then can do nothing in it. The only way to use an applet's
query is to copy its SQL into a play window, which drops what makes it an
applet: the fixed buffer, the attenuated surface and the security class it
was published under.

Where play does serve them, parameters and signals reach a model as text:
`get_state` names each parameter's ClickHouse type, tier and value, and
each signal's value and writer. What play's own widgets know about a slot —
the options of an enumerated parameter, whether a slot still needs a value
before a run — and what the signal declarations know — a signal's type and
the pane that writes it — do not reach the model, so it guesses values and
learns they were wrong from a failed run.

## Design space (QOC)

**Question.** How does an agent use an applet — read what it is, fill its
inputs, run it, read its rows — without the applet ceasing to be one?

**Options.**

- **O1** — applets serve play's catalog, filtered to what an applet is for.
- **O2** — applets serve a catalog of their own, written for applets.
- **O3** — no catalog; the coordinator copies an applet's SQL into a play
  window and drives play.

**Criteria.** One semantics for parameters, signals, runs and limits
wherever play's engine runs; the applet's fixed definition and class kept;
little new surface to review.

O1 meets all three: the handlers already act on the embedded play state
alone. O2 duplicates every handler and lets the two drift. O3 needs nothing
new and keeps nothing of the applet.

## Decision

We will have an applet window serve play's catalog, filtered to an applet's
purpose, add an operation that reads the applet's definition as fields, and
widen the parameter and signal state play reports — in play and in applets
alike — to what its widgets and signal declarations already know.

### SD1 — An applet window serves play's catalog, filtered

Play's operations act on the embedded play state alone; only their
receiver ties them to play's launcher. The catalog is built over that
state, and play exports what an embedder needs: the catalog with a filter,
and the frame wrapper that sets the gesture context and the agent mark
(ADR-0270 §SD3, §SD6). The applet's window serves the filtered catalog and
its manifest declares it.

Served in an applet: `get_state`, `describe_result`, `sample_rows`,
`set_param`, `run`, `list_panes`, `show_pane`, `list_datasets` and the
query machine's `query_state`/`query_machine`; `set_signal` under Q2.

Not served:

- **`set_sql`.** The buffer is the applet's definition. Another query is a
  play window's work, and an agent opens one.
- **The authoring reference** — snippets, functions, the endpoint's schema,
  `validate_sql`, `trace_rewrite`. They help write SQL, which an applet does
  not take; play serves them.
- **`bind_pane`.** The panes and what feeds them are the applet's
  definition (its `tabs`).
- `show_pane` reaches only the tabs the applet kept.

ADR-0270's limits apply unchanged: an agent's run, and a Live rerun after
an agent's write, must classify as a read and name only granted
destinations. An applet whose class is not read cannot be run by an agent
at all, which is what its class says. The person's gestures in an applet go
through the catalog as in play (ADR-0270 §SD6), which amends that SD's
"only play's own launcher installs the path".

### SD2 — `get_definition`: the applet as fields

A query that returns what the applet is, without running it: slug, book,
title, summary, security class, endpoint, the tabs it shows, the datasets it
reads, its SQL, and its inputs — each parameter slot with its type and, from
SD3, its widget and options, and each signal its SQL reads with the
signal's declared type and writer. A model reads this once to know what to
fill and what a run returns.

An applet from a book embedded in the build is the build's text and is not
marked untrusted, as play's snippets are not (ADR-0270, update of
2026-10-02). An applet from the store is text a person saved, from wherever
they took it, and is delimited as untrusted (ADR-0269 §SD7).

### SD3 — Parameters and signals as play's widgets know them

`get_state` reports, in play and in applets alike:

- **per parameter:** besides name, type, tier and value — the widget that
  edits it (text, enumeration, range, date-time pair, SQL expression), the
  options of an enumeration (value and label, from its `-- play: enum`
  line), the bounds of a range, and whether a run still needs a value for
  it (the slots `unfilledInputs` names);
- **per signal:** besides name, value and writer — its declared type, the
  pane that writes it where one does, and whether the buffer reads it.

`set_param` checks a value against the slot's type and options before it
writes, and a refusal carries `next` with the options or the expected form
(the shape of ADR-0265's update of 2026-10-02). `run` already refuses while
an input is unfilled; its refusal names the inputs.

### SD4 — Finding an applet

`describe_app` with a search finds applets by title, summary or keyword,
and lists them only then (ADR-0269, update of 2026-10-03). The coordinator's
system prompt says so. An inventory table of applets, `keelson('applets')`,
is deferred (SD6).

### SD5 — The coordinator

No new fixed tool: `describe_app`, `open_window`, `call_operation` reach an
applet as they reach play. The system prompt says what an applet is for —
a saved query to fill and run, not to edit — and that `get_definition`
comes first.

### SD6 — Deferred, recorded

- **`keelson('applets')`**, the inventory as a table, for SQL as well as for
  agents.
- **Opening an applet with its inputs filled**, a launch config carrying
  parameter values (ADR-0135), so a model opens it ready to run.
- **An applet's result handed back as data** — a published dataset or a
  data handle — beyond `sample_rows`' bound.

## Open questions

- **Q1 — Typed tools per input.** The coordinator could offer each input of
  an applet window as a tool of its own (`w<window>_param_<name>`), with a
  JSON schema from the slot's type and options, as
  `BOXER_CHAT_OPERATION_TOOLS` does for operations. *Recommendation: not
  now.* SD3 puts the same facts in `get_state` and `get_definition`, and
  `set_param` validates against them; per-input tools would change the tool
  list from call to call, which the agent-operations trial recorded against
  operation tools as a default.
- **Q2 — Signal writes in an applet.** In play an agent may set any signal.
  In an applet a signal stands for a gesture on one of its panes.
  *Recommendation: serve `set_signal`, but only for the signals the
  applet's SQL reads* — its live inputs — and refuse the rest by name. A
  write to a signal nothing reads changes nothing the agent can observe and
  would show the person a gesture that did not happen.

## Alternatives

- **O2 — a catalog of the applet's own.** Rejected: every handler written
  twice, and parameters, signals and limits free to drift between play and
  its embedder.
- **O3 — copy the SQL into play.** Rejected as the way, kept as a fallback a
  model can still take: it loses the applet's fixed buffer, its attenuated
  panes and the class it was published under.
- **Expose `set_sql` in an applet.** Rejected: an applet whose buffer an
  agent rewrote is a play window under an applet's name, and its class
  would no longer describe it.

## Consequences

### Positive

- A model uses the curated queries as their authors meant them: filled, run
  and read, not rewritten.
- One parameter and signal semantics, one set of agent limits, wherever
  play's engine runs.
- SD3 improves play's catalog too: a model fills a parameter with a value
  the widget would accept.

### Negative

- Play's catalog becomes a seam embedders depend on; a change to an
  operation is a change for applets as well.
- An applet of a non-read class is visible to an agent and never runnable
  by one; the refusal has to say why.

### Neutral

- Applet documents do not change; nothing is added to their frontmatter.

## Verification plan — Tier 1

- **Lane: default `go test`.** An applet's catalog lacks `set_sql`,
  `bind_pane` and the reference operations, and serves the rest; an agent's
  `run` of a non-read applet is refused with the class; `set_param`
  refuses a value outside an enumeration with `next` naming the options;
  `get_definition` returns the slots and the signals the SQL reads;
  `get_state` reports unfilled inputs. Under Q2's recommendation, a
  `set_signal` to a signal the SQL does not read is refused by name.
- **Lane: scene.** The chat's coordinator against the scripted model over a
  book applet with an enumerated parameter: `describe_app` with a search,
  `request_access` opening it, `get_definition`, `set_param`, `run`,
  `sample_rows`.
- **What would fail.** An applet window still serving no catalog fails the
  scene at its first call; a filter that leaks `set_sql` fails the catalog
  test.

## Milestones

1. Play's catalog over the embedded state, exported with its filter and
   frame wrapper; applets serve it (SD1).
2. `get_definition` (SD2).
3. The parameter and signal state, and `set_param`'s check (SD3).
4. The coordinator's prompt and the scene (SD4, SD5).

## Status

Proposed — awaiting review by the code owner, with Q1 and Q2 open.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — applets, their documents and their attenuated window.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — play's catalog and the limits on an agent's run; SD6 is amended by SD1.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the operations contract, untrusted content, and finding apps by search.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the coordinator that drives applets.
- [ADR-0124](./0124-play-param-editing-widgets.md), [ADR-0187](./0187-play-sql-expression-parameters.md) — parameter widgets and SQL-expression parameters.
- [ADR-0097](./0097-play-reactive-query-graph.md) — signals and the reactive query graph.
- [ADR-0135](./0135-app-launch-requests.md) — launch requests, for SD6's filled-in opening.
