---
type: adr
status: proposed
date: 2026-11-17
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0378: Dashboardlets — a kind for a dashboard minted from a committed document

## Context

Three kinds already name a launcher entry minted from a committed document:
an SQL applet ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)), a chatlet
([ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md))
and a workstatelet
([ADR-0369](./0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md)).
Each kind gets a badge in the launcher, a launcher filter and a value in
`keelson('apps').kind`.

A downstream adopter mints one more family: a
Grafana dashboard JSON document, committed beside a short markdown
descriptor, opened in its dashboard viewer and nothing else. Its minted
entries are no more ordinary apps than an applet is. Without a kind of
their own they would be filed under Apps and would show up in an agent's
first `describe`. The viewer and the minter both live in the adopter.
`app.KindE` is a closed enum in boxer, so the adopter cannot add a value
itself.

The adopter's maintainer chose a boxer kind over keeping the entries as
ordinary apps (2026-11-17).

## Decision

### SD1 — `KindDashboardlet`

`app.KindDashboardlet` (ordinal 5, wire token `dashboardlet`) is an app
minted from a committed dashboardlet document. As with the other minted
kinds, the minter stamps it and no manifest writes it by hand. Boxer holds
no minter and no viewer for it; the kind is the whole of boxer's part.

### SD2 — Launcher

A dashboardlet row carries a `dashboardlet` badge with the info tone. The
kind filter offers **Dashboardlets** beside Applets, Chatlets and
Workstatelets.

### SD3 — Agents see one when they name or search for it

`describe` without an app or a search leaves dashboardlets out, as it
leaves out applets and chatlets. Each one carries its viewer's catalog,
and the unfiltered list is the model's first call. Unlike a workstatelet,
a dashboardlet is an app with a window, so a task can open and operate it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.KindE` | `KindDashboardlet` added | `keelson('apps')` `kind` values; readers switching on kind; the launcher's kind checkboxes |
| `runtime.agent` describe | Leaves dashboardlets out of an unfiltered listing | — |

## Alternatives

- **The adopter's entries stay `KindApp`, with a keyword.** Not taken: no
  filter, no badge, and every one is in an agent's first listing.
- **An open, string-valued kind an adopter registers.** Not taken for one
  family. The enum is pinned by tests and switched on in the launcher; a
  registry of kinds becomes worth having when a second adopter asks for
  one.

## Consequences

- Boxer names a kind whose documents and window live in another
  repository; nothing in boxer mints one.
- A reader that switches on kind and has no `KindDashboardlet` case falls
  through to its default, which for `String` and the launcher label is the
  plain app's.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the launcher's kind checkboxes count six;
  `describe` lists a dashboardlet only when it is searched for.
- **Gap.** The badge is not drawn under test.

## Status

Proposed 2026-11-17.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md), [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md), [ADR-0369](./0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md) — the other minted kinds.
- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — kinds as provenance, not sections.
