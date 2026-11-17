---
type: adr
status: proposed
date: 2026-11-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0376: A deployment restates app properties by a SQL override over keelson.apps

## Context

An app's manifest is written by its author and compiled into every binary
that links it. A downstream binary links apps it did not write, and an
operator runs binaries they did not build. Both sometimes need an app
described differently from how its author described it: a grade that holds
in one deployment and not in another
([ADR-0375](./0375-an-app-states-its-maturity-a-grade-on-the-manifest-and-two-columns-on-keelson-apps.md)
leaves the grade an affirmation nobody checks), a topic that files the app
where this deployment's people look for it, a name in their vocabulary.

Three predicates over `keelson.apps` decide what a host opens, runs as a
service, and lets ask for an agent task grant
([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md),
[ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md),
[ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)).
They can only select by what the manifests say. A deployment that wants
"supported apps only" for an app its author left ungraded has to name the
app by id in each predicate, which is the list ADR-0272 rejected.

The launcher, the agent's `describe` and the launch-kind resolution read
manifests from the registry in Go, not from the table. An override that
reached only SQL would leave them disagreeing with `keelson.apps`.

## Decision

**SD1 — The override is a `SELECT` over `keelson.apps`.** It returns an
`id` column and any of the writable columns (SD3). Each row restates that
app; an app the statement does not return keeps its values, and a column
it does not return keeps its value. Both shapes work:

```sql
-- name the apps changed
SELECT id, 'supported' AS maturity FROM apps WHERE id IN ('…/a', '…/b')
-- restate every app by expression
SELECT id, if(has(topics, 'sql'), arrayPushBack(keywords, 'query'), keywords) AS keywords FROM apps
```

A statement can read other tables clickhouse-local can reach, such as a
`file()` of overrides kept beside the deployment. It cannot remove an app:
selecting which apps run stays with the predicates.

**SD2 — Two sources, applied in order.** `hostboot.Options.AppsOverrideSql`
is the downstream binary's override, set in its wiring.
`KEELSON_APPS_OVERRIDE_SQL` is the operator's. The second is evaluated
over the table as the first left it, so an operator refines what a binary
ships rather than replacing it.

**SD3 — Only descriptive columns are writable.** `display`, `summary`,
`icon`, `topics`, `keywords` and `maturity`: what the app is called and how
it is filed and graded. Two columns derive from them, `title` and
`maturity_rank`; the registry recomputes them, and a statement that
changes them is refused unless the value is the one its override implies.
Every other column must come back with the value it had. That excludes:

- `launchable` and `coordinator`, which the host decides and which the
  service and coordinator predicates conjoin as their ceiling;
- `caps`, because the bus enforces the declared ones, and a different
  value in the table would only mislead;
- `kind`, `surface`, `registration`, `launch_kind` and the origin columns,
  which state what the app is and where it came from (ADR-0158 §SD5,
  ADR-0339) rather than how a deployment describes it.

A column `keelson.apps` does not have is refused, so a misspelt name is
not read as nothing to change.

**SD4 — Evaluated once, at boot, before the launch limit.** On
clickhouse-local, through the same table registry the predicates use, so
all three predicates select by the overridden values. As with ADR-0272
§SD2, an app registered later keeps its declared values. Unset, nothing is
evaluated and clickhouse-local is not used. Set and not applicable — no
clickhouse-local, a statement that does not run, a row that names no
registered app or names one twice, a forbidden or unknown column, an
overridden manifest that fails `Manifest.Validate` — the host does not
boot. A batch is applied whole or not at all.

**SD5 — The override lands on the registry.** `Registry.OverrideManifests`
replaces the fields in the manifests the registry hands out, so the
launcher, the agent surfaces and `keelson.apps` show the same values.
`AppI.Manifest`, which an app answers itself, is not reached.

**SD6 — The table says what was overridden.** `keelson.apps.overridden`
lists the columns whose values an override replaced, so
`WHERE notEmpty(overridden)` shows what a deployment changed, and a
predicate can tell an author's grade from a deployment's.

## Alternatives

- **Rewrite each query that reads `keelson.apps`.** Serving the table as
  `WITH apps AS (<override>) …` on every read would also cover apps
  registered later. It puts a second statement into every query, and the
  launcher, which reads manifests in Go, would still not see it. The
  predicates run once at boot; the override matching them is the simpler
  contract.
- **An override struct in Go only.** It serves a downstream binary and not
  an operator, and it cannot express a rule over the table ("every app
  with topic `sql`").
- **Any column writable.** Overriding `launchable` would let the override
  undo the ceiling the service and coordinator predicates rely on, and
  overriding `caps` or provenance would make the table state something the
  runtime does not do.
- **Require one row per registered app.** Returning every row is the
  natural shape for an expression, but naming the apps changed is the
  natural shape for a table of overrides; omitted rows keeping their values
  allows both, and removal stays impossible either way.

## Consequences

- A grade, topic or name in a running process may not be what the source
  says. `overridden` and the boot log record it; the source does not.
- The grammar the introspection engine parses with does not accept the
  `* REPLACE (…)` and `* EXCEPT (…)` column transformers, so a statement
  lists its columns. Omitted columns keep their values, so nothing is lost
  but brevity.
- The statement is spliced into a query and carries the trust of any other
  environment variable the process reads, as the predicates do.
- A deployment with an override and a predicate pays two extra
  clickhouse-local round trips at boot (the table before, the statement's
  result), and two more for the second source.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — topics, keywords and kind
- [ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md) — the launch limit, and the evaluate-once-at-boot shape this follows
- [ADR-0339](./0339-app-origin-observed-at-registration.md) — origin columns
- [ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md) — services
- [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) — the coordinator predicate
- [ADR-0375](./0375-an-app-states-its-maturity-a-grade-on-the-manifest-and-two-columns-on-keelson-apps.md) — maturity
