---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** This directory and the convention
> described here were introduced 2026-07-31 and have not been reviewed.

# ADR background work

Analyses that feed decisions without being one. A page lands here when it
surveys a design space, measures how something currently behaves, or costs out
options — material an ADR's Context and QOC sections lean on, but too long or
too provisional to sit inside the record itself.

What separates this directory from its neighbours:

- [`doc/adr/`](../adr/) holds decisions. A page here is not a decision and
  carries no status lifecycle beyond its own front matter.
- [`doc/explanation/`](../explanation/) explains the system as it is, for
  someone trying to understand it. A page here is written for whoever has to
  take a particular decision, and it ages out once that decision is taken.

Consequences of that framing:

- Pages are dated and provenance-marked, so a later reader can tell measured
  claims from estimates and knows how stale both are.
- They are not maintained against the tree. Once the ADR they informed is
  accepted, the ADR is authoritative and the background page is a snapshot of
  the reasoning, not a description of current behaviour.
- A page whose decision was taken can stay as-is, get a pointer to the
  resulting ADR, or be deleted — whichever leaves the record clearest.
