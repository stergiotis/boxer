---
type: adr
status: proposed
date: 2026-11-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0375: An app states its maturity — a grade on the manifest, and two columns on keelson.apps

## Context

Apps in one registry promise different things. One exists to find out what
can be built and may be gone next month; another holds state that people and
other apps are built on. Nothing in a manifest says which.

[ADR-0158](./0158-app-classification-topics-keywords-kind.md) gave an app two
classification axes: `Topics`, what it is about, and `Kind`, how it came to
exist. Neither is a promise. `KindDemo` comes closest and says something
else: a demo states what an app is for, and a demo may be dependable while an
ordinary app is a first attempt.

The lack shows in three places.

- **A host cannot leave unfinished work out.** Three predicates over
  `keelson.apps` decide what a host opens, runs as a service and lets ask for
  an agent task grant
  ([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md),
  [ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md),
  [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)).
  A deployment that should show only what it can rely on has no column to
  name.
- **A person cannot tell either.** The launcher lists a first attempt and a
  dependable tool the same way.
- **Review depth has nothing to follow.** Code written to learn and code
  others depend on warrant different scrutiny, and the tree does not say
  which is which.

Two constraints bound the answer. A manifest that fails validation is dropped
with a warning (ADR-0158 §SD9), so a new required field would silently remove
every app that has not yet filled it in. And a grade is typed by a person:
[ADR-0337](./0337-competences-anchored-to-the-tree-and-assessed-by-evidenced-claims.md)
(proposed) argues that a rating without evidence is an affirmation, which
this one is.

## Decision

**SD1 — Four grades, ordered.** `app.MaturityE`, weakest first:

| Grade | Promises |
| --- | --- |
| `unstated` | nothing; nobody has graded the app. The zero value |
| `incubating` | nothing, on purpose: the app exists to learn what can be built. Its operations, its stored state and the app itself may change or go |
| `supported` | other work may depend on it; a change that breaks a caller or a person's stored state is announced |
| `core` | its stored data or its bus contract is what other apps or deployments are built on; changing either needs a migration |

`unstated` and `incubating` are kept apart. Both promise nothing, but one is
a statement and the other is the absence of one: a view of "what is being
incubated" must not list every app nobody has looked at.

**SD2 — The grade is a manifest field, and stating none is valid.**
`Manifest.Maturity` takes the zero value when left out. `Validate` refuses
only a value outside the grades, as it does for `Kind`. An app that forgets
the field ranks below every stated grade, so forgetting under-promises.

**SD3 — `keelson.apps` carries the grade twice.** `maturity` is the grade's
name, for equality. `maturity_rank` is its position in SD1's order, 0 to 3,
because the names do not sort in that order. The predicates of ADR-0272,
ADR-0352 and ADR-0364 read both without any change of their own:

```sql
maturity_rank >= 2                    -- only what may be depended on
maturity = 'incubating' OR shell      -- the incubating apps, and the host's chrome
maturity_rank >= 2 OR kind = 'applet' -- see SD6
```

**SD4 — The launcher filters by grade, and marks the grade that warns.** A
row of chips selects grades, with none selected meaning no restriction: the
topic chips' polarity (ADR-0158 §SD6), since the gesture is "show me only
these". The row is drawn only when the launchable apps carry more than one
grade, so a host where nothing is graded looks as before. A list row carries
a badge only for `incubating`; the detail pane names any stated grade. The
launcher's `set_filter` and `get_view`
([ADR-0345](./0345-system-apps-join-the-operations-contract-the-log-viewer-first.md))
carry the selected grades.

**SD5 — The launch limit stays a limit.** `KEELSON_LAUNCHABLE_APPS_WHERE`
decides what may be opened, by anyone. Naming a grade there removes the other
apps from the host, the launcher and help among them, and any app an admitted
app hands work to. Looking at one grade is SD4's chips; SD3's predicates are
for what a host permits.

**SD6 — A minted app is `unstated`.** No minter reads a grade from its
document, so an applet
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)), a chatlet and a
workstatelet
([ADR-0369](./0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md))
state none. A chatlet starts from the chat's manifest and does not keep the
chat's grade: what a document promises is what its prompt and its
permissions do, and that is not the chat's to vouch for. A host that limits
by rank therefore loses its committed documents unless its predicate names
their kind, as SD3's third line does. A `maturity` key in the documents'
frontmatter is deferred.

**SD7 — The grade is an affirmation, and the runtime does not check it.**
What an app must show to move up a grade (a decision record, a help book, a
scene, no open finding) belongs to the tree that grades it, in a test over
its registered apps, not in `Validate`.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.Manifest` | field `Maturity` added; zero value valid | nothing: every existing manifest reads as `unstated` |
| `keelson.apps` | columns `maturity` (string) and `maturity_rank` (int32) added | the three predicates may name them |
| launcher operations `set_filter`, `get_view` | optional `Maturities` added | nothing: left out, it restricts nothing |

## Alternatives

- **The zero value is `incubating`.** Rejected: on the day the field lands
  every app in every tree would read as incubating, and "show the incubating
  apps" would show all of them (SD1).
- **A required field.** Rejected: a rejected manifest is dropped with a
  warning, so the cost of an unfilled field would be an app missing from the
  launcher and nothing going red.
- **A value of `Kind`.** Rejected: provenance and promise are independent. An
  applet may be incubating and a demo supported, and one field cannot say
  both.
- **The name alone as the column.** Rejected: sorted as text, `core` comes
  before `incubating`, so "this grade or better" cannot be written.
- **A hidden-set for the launcher's filter, as the kind toggles have.**
  Rejected: with four grades the common gesture, one grade alone, would be
  three clicks.
- **Greek letters (alpha, beta, gamma).** Rejected: they say nothing about
  what is promised, and the third has no agreed meaning.
- **An owner on the manifest as well.** Rejected: ownership is a property of
  paths, libraries included, and a handle in a manifest is compiled into
  every build. A code-owners file by path holds it.

## Consequences

### Positive

- A deployment states in one predicate that it shows only what it relies on;
  a headless app is held to the same line before it runs as a service.
- Work done to learn can be marked as such, where the people and the agents
  who open it see the mark.

### Negative

- A grade nobody maintains is worse than none. An app left `supported` after
  its author moved on promises something no one keeps.
- SD6 leaves minted apps ungraded, so a rank predicate has to name their kind
  until the documents can state a grade.
- A host that limits by rank must admit its own chrome. The launcher and help
  are graded like any app, and a predicate that leaves them out leaves the
  host without them (SD5).

### Neutral

- A demo states no grade. Its kind says what it is for, and a rank predicate
  then keeps it off a host that shows only what it relies on.
- A predicate that names `maturity` does not evaluate against a build without
  the column, and a host with such a predicate does not boot. The build goes
  out before the configuration that names the column.

## Migration — Tier 1

Nothing to migrate. No manifest, stored record or bus message changes shape;
the two columns are additions.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the grade's order, parsing and validation in
  `keelson/runtime/app`; the two columns in `introspect/providers`; the
  launcher's filter and operations in `keelson/runtime/launcher`. The launch
  limit's test evaluates predicates over both columns on clickhouse-local,
  and is skipped where the binary is missing.
- **What would fail.** A rank that stops following the order; a predicate
  over `maturity` or `maturity_rank` that stops selecting; a filter command
  that no longer round-trips its grades.
- **Scene.** The launcher's `maturity` scene selects the incubating grade
  and asserts that the list narrows, that a row carries the badge, and that
  clearing the selection restores the list.
- **Gap.** Nothing checks that a grade is deserved (SD7).

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — topics, keywords and kind; §SD9 on a rejected manifest
- [ADR-0214](./0214-launcher-as-app-summary-detail-recall.md) — the launcher as an app
- [ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md) — the launch limit
- [ADR-0352](./0352-services-headless-apps-a-service-host-runs-at-boot-under-a-predicate-composed-with-the-launch-limit.md) — services
- [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) — chatlets and the coordinator predicate
- [ADR-0337](./0337-competences-anchored-to-the-tree-and-assessed-by-evidenced-claims.md) — ratings and evidence
