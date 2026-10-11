---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0313: The agent schema — generated relations an agent may read in full, a scope it cannot override, withheld marked rather than absent

## Context

On the compile date an agent's SQL is checked by play's agent limits
([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) §SD2): a
statement that classifies as a plain read is allowed against any table of an
endpoint the grant lists, only `keelson()` tables are checked by name, egress
functions are refused, and runs carry `readonly = 2`, which a scratch-server
spike showed still permits `SET`. ADR-0270 itself records that views,
dictionaries and engines reach beyond the endpoint without the statement
naming them, and names "the restricted database user that closes it" without
designing one.

A ClickHouse `ROW POLICY` is Stonebraker and Wong's query modification: the
filtered query runs and returns fewer rows, and a model reading the result
cannot tell "no such row" from "a row withheld" — the *Truman* model in Rizvi
et al.'s terms. Leeway makes that worse than a plain table, because positivity
gives absence a meaning: an instance exists because it is tagged, and an
empty list is not representable
([leeway-query-algebra](../explanation/leeway-query-algebra.md)).

The threat model ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)) settles what the schema is for. Confidentiality inside a
single-person cell is not a security question, so the schema is not access
control against the person. It stays for two reasons: least privilege against
an injected agent — what it cannot read it cannot move to a remote route —
and not misleading the model. The spikes behind this record are in
[agent-harness §4.3 and Appendix A](../adr-background-work/agent-harness.md).

## Design space (QOC)

**Question.** What may an agent's SQL read, and how is a refusal shaped?

**Options.**

- **O1 — Endpoint-wide read grant**, as the compile date.
- **O2 — Row policies** on the base tables, scoped per agent user.
- **O3 — A generated agent schema**: declared, scoped relations the agent
  may read in full; base tables not nameable from its principal.
- **O4 — A validity-inference engine** over arbitrary SQL, Rizvi's
  non-Truman design.

**Criteria.**

- **C1 — Not misleading**: a refusal names a relation, never a row; the
  model never reads an empty answer that means "withheld".
- **C2 — Least privilege** against an injected agent.
- **C3 — Works on both engines**: the server principal and the local worker
  of [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md).
- **C4 — Cost** of building and of each query.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | −− | ++ | ++ |
| C2 | −− | +  | ++ | ++ |
| C3 | ++ | −  | ++ | −  |
| C4 | ++ | +  | −  | −− |

O3 is taken. It is non-Truman without a validity engine because the only
relations the agent can name are ones it may read in full; O2 is kept only
as a backstop that must never fire.

## Decision

We will give agents a **generated agent schema**: declared relations over
the journal and over data an operation exposes, scoped by a setting the
query cannot override, compiled from one declaration both to a server
principal and to the local worker, with withheld content marked rather than
absent. Base tables and `system.*` are not nameable from the agent's
principal, so validity is syntactic.

### SD1 — The relations

| Relation | Scope | Content |
| --- | --- | --- |
| `my_run` | the run the session is pinned to (`custom_agent_run`) | the run's journal entries, every column |
| `my_conversation` | the conversation the run belongs to (`custom_agent_conversation`), the pinned run excepted | earlier runs' entries; bodies included, since inside the cell they are the person's to see, bounded by budget not by label |
| `agent_history` | the owner app (`custom_agent_app`) | content-free: kinds, keys, effects, digests, labels, sizes |
| per-operation relations | what an operation's read exposes, as [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) does for play | the operation's declared columns, through this schema's masking; deferred (SD8) |

These are the agent's view of [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
§SD10's read surface, declared in the `agentschema` package over the
plain-shaped journal tables the introspection registry serves
(`agent_run_entries`, `agent_runs`) and compiled from there; the DDL is
generated and tested, and no host executes it yet. Applying it on a server
means binding the relations to the store's own tables, `boxer.agentrun` and
`boxer.agentrunentry`, or to a plain-shaped view over them (ADR-0309 §SD8),
which is part of the Migration path.

### SD2 — The scope is a constrained setting

The run (or scope) an agent is pinned to is a **custom setting constrained
`READONLY` in the agent principal's settings profile**; `SET` and
`SETTINGS` both fail with a constraint violation, and a re-pin needs a new
connection. The scoped relation is a view with `SQL SECURITY DEFINER`
reading that setting; the agent holds `SELECT` on the view only, and a read
of a base table fails naming the table. The generated principal is a user
`IDENTIFIED WITH no_password HOST LOCAL`, reachable from the host alone;
its lifetime across runs is the deployment's to decide, and the profile is
written `OR REPLACE` so a reused principal cannot keep a stale scope. A
scope setting left without a value is refused at generation, since an
empty pin would open the backstop's other arm. Scope is **never a
caller-supplied parameter**: a parameterised definer view answered zero
rows for an out-of-scope run with no refusal in the spike, which is the
Truman symptom this record exists to avoid.

### SD3 — The backstop that never fires

A `RESTRICTIVE` row policy `TO ALL` per relation on its base table, keyed
on a per-relation marker setting (`custom_agent_relation`) that the
relation's definer view sets, with "this relation's marker is unset" as
its other arm, fires *inside* a definer view written without its scope; a
per-user policy does not, because the caller's own policies are bypassed
under `SQL SECURITY DEFINER`. The marker is what keeps three relations over
one base table apart: restrictive policies on one table combine by AND, so
policies keyed on the scope settings alone would cut `my_conversation` and
`agent_history` down to the pinned run. The design depends on ClickHouse's
`access_control_improvements.users_without_row_policies_can_read_rows`
being on, which a preflight checks. Law **L18**: for every scoped relation
and scope, the agent principal's row count and body hash equal the
definer's under the same setting — the backstop filtered nothing; checked
over the generated DDL in the default lane and against `clickhouse-local`
for the worker views.

### SD4 — Policy on the leeway model, compiled per layout

"Row-level" names two things on leeway: an entity on axis R in the packed
layout, an attribute instance on axis I in the exploded one. Policy is stated
on the model and compiled per layout:

| Aspect | Stated as | Packed | Exploded |
| --- | --- | --- | --- |
| scope | the setting against a backbone column | `WHERE` on the backbone | `WHERE doc IN …` |
| section | passed or dropped | drop the section's columns | `WHERE section = …` |
| membership | withheld by registry id | one keep-mask on axis I applied to the value lane, the `len` lane, the membership stream and the `lrcard` lane | `WHERE NOT has(memberships, …)` |
| value aspect | from the physical name | `secret` → withheld; `application-level-encryption` → opaque bytes; `pseudonymized` → passed, aspect retained | the same on typed value columns |

What a deployment withholds from the model inside its own cell is hygiene —
credentials, data with no business in a model context — not a confidentiality
boundary. As built the rules compile onto the plain shape only, where
memberships are a declared array column; the packed and exploded
compilations wait for the dated Update on ADR-0171 that admits the withheld
section into the view IR (SD8). The value-aspect defaults are declared on
each relation and listed, not yet compiled (SD8).

### SD5 — Withheld, not absent

A withheld instance reappears in a value-less **withheld section** on
every agent relation — the lanes `withheld_memberships` and
`withheld_policy`, the latter naming `withhold:<relation>` — carrying the
instance's original memberships plus the policy that withheld, so a reader
tells "withheld" from "never recorded"; on the plain shape the value is
blanked in its own lane beside them. Laws **L11** (co-alignment: co-lanes
of equal length, each ragged child summing to its lengths lane), **L12**
(no withheld membership survives on a valued section) and **L13** (per
entity, base instances equal kept plus withheld) are generated per
relation and returned no rows in the spike. The projection marks withheld
content in the model's context
([ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md) §SD2).

### SD6 — One declaration, generated views

The schema is generated the way the trail's views are — a Go table of
columns through `LW_GET` and the read-back passes — and same-shape masking
through `clickhouse.ComposeCreateView`; the `withheld` section enters the
view IR so a generator emits its two lanes
([ADR-0171](./0171-leeway-sql-read-surface.md), a dated Update). The
declaration has four rule forms and no value predicates, because a predicate
on values is where a declaration becomes a policy engine:

```text
relation  <name>  OVER <table|view>  SCOPE <setting> = <backbone column>
  section   <s>   { PASS | DROP }
  withhold  <s>   MEMBERSHIP IN (<registry keys…>)
  aspect    secret → WITHHOLD ; application-level-encryption → OPAQUE ; pseudonymized → PASS
```

The value-aspect rules are defaults read from the physical names and need no
per-relation text. One declaration compiles twice: to a ClickHouse user,
profile, views and backstop policies on the server; and to the local worker
with the scope as a constant the single writer ships. As built the
declaration is a Go literal with these forms rather than a parsed text, and
the views are composed over the plain-shaped tables; the `LW_GET` read-back
path for the leeway layouts goes with SD4's deferral.

### SD7 — Budgets

Reads through the schema are to be metered in bytes and in attribute
instances (`arraySum(<channel>card)` per section, computed by the view)
under [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)
§SD5; not metered yet.

### SD8 — Deferred, recorded

- A validity-inference engine over arbitrary SQL (O4).
- Aggregate relations with small-group suppression, which inside a
  single-person cell have no one to hide a group from; they return at the
  sync boundary as the person's act.
- Entity predicates beyond the scope setting.
- Per-operation relations; compiling the rules onto the packed and exploded
  layouts and the value-aspect actions; executing the generated DDL on a
  host and running agent statements as the principal; the `recall` tool
  reading through `my_run` rather than the writer's memory; read budgets.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| ADR-0270 §SD2 agent limits | the statement runs as the agent-schema principal; `readonly = 2` bounded by a constrained profile; a dated Update | play's run path and its agent-limit tests |
| ADR-0171 read surface | the `withheld` section in the view IR; a dated Update | `ComposeCreateView` and its test; the read-back UDFs' goldens |
| ADR-0308 §SD10 read surface | the agent's relations are these views | the journal's generated store |
| the introspection engine and the worker | a second compilation target with the scope as a shipped constant | the chlocal path |
| `keelson('agent_relations')` | added: the declared relations and their rules | the introspection registry |

## Alternatives

O1, O2 and O4 are weighed in the QOC. Two narrower choices:

- **Blanking a withheld value in place** instead of a value-less section.
  Rejected: under positivity a blank is absence for list sections, and the
  reader cannot tell it from "never recorded".
- **Scope as a view parameter.** Rejected by the spike: a Truman surface.

## Consequences

### Positive

- A refusal names a relation; the model is never misled by an empty answer.
- An injected agent reads only what the schema grants, which bounds what it
  can move to a remote route.
- One declaration serves the server and the worker; the backstop is checked
  by a law, not trusted.

### Negative

- An extra value-less section on every agent relation; in the exploded
  layout a row whose value columns are defaults.
- A settings profile, views and policies to generate and keep in step on the
  server; a second compilation for the worker.

### Neutral

- The schema is not access control against the person; nothing it withholds
  is hidden from them elsewhere.

## Migration — Tier 1

- **Breaks.** Nothing yet: the DDL is generated and tested, and no host
  applies it. Once applied, an agent statement that named a base table or a
  `system.*` table is refused by name; the refusal says which relation to
  use.
- **Path.** Apply the schema for play's endpoint first; the coordinator's
  help text lists the relations.
- **Old shape.** The endpoint-wide read grant, until the schema is applied
  on an endpoint.

## Verification plan — Tier 1

- **Lane.** Default: the generated DDL against its golden, one backstop
  per relation, an unpinned scope refused, the relations table. Integration:
  the worker views under `clickhouse-local`. L18 against a scratch server
  per relation and scope, and the constraint-violation and access-denied
  probes of the spike as tests, wait for the DDL to be applied.
- **What would fail.** A withheld membership on a valued section; co-lanes
  that disagree; a backstop that filtered rows; a `SET` of the scope setting
  that succeeded; a base table readable from the agent principal.
- **Gap.** What a deployment chooses to withhold is policy; the lane checks
  that the policy was applied, not that it was wise.

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-11 — `agent_history` passes commitments

The body and calls digests the content-free relation passes are, under
[ADR-0320](./0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md),
commitments under an opening the relation never sees, so passing them
reveals nothing of erased content either. The vault table is not a relation
an agent may read; the journal relations read the ledger's columns, bodies
joined by the introspection table where the relation passes them.


## References

- [ADR-0171](./0171-leeway-sql-read-surface.md) — the read surface the views are generated through.
- [ADR-0270](./0270-play-operations-catalog-and-agent-limits.md) — the agent limits this record replaces at the endpoint.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md), [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md), [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md) — the threat model, the journal, the projection, the budgets.
- [leeway-query-algebra](../explanation/leeway-query-algebra.md), [leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md) — positivity; reading a leeway table.
- [agent-harness](../adr-background-work/agent-harness.md) §4.3 and Appendix A (a), (b), Appendix C — the spikes and their SQL; Stonebraker and Wong 1974; Rizvi, Mendelzon, Sudarshan, Roy 2004.
