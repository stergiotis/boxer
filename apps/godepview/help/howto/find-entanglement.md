---
type: how-to
audience: end-user
status: draft
title: Find dependency cycles and coupling
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Find dependency cycles and coupling

How tangled is the code? This guide surfaces the two strongest signals:
group-level dependency cycles and heavily-coupled subsystems.

## Find cycles

1. Set the **view** switch to **Architecture**.
2. Read the legend banner: **⟳ N cycle(s)** means the quotient has dependency
   cycles, drawn in **amber**; **✓ No dependency cycles between groups** means it
   does not.
3. The detail pane's **Dependency cycles** section lists each cycle as a chain of
   groups (`a ⇄ b ⇄ c`). Click a group to focus it and see its members.

Go forbids cycles between *packages*, but **groups** can still cycle: two
subsystems each importing some package from the other. That mutual dependence is
the deepest entanglement — neither group can be understood or moved without the
other.

## Read coupling

In the groups table:

- **Out** — how many other groups this one depends on.
- **In** — how many groups depend on this one.
- **flags** — **⚠** for an app→app violation, **⟳** for a group caught in a cycle.

A group with a high **Out** is fragile — it breaks when many things change; a
high **In** is load-bearing — changing it ripples widely. The **group depth**
slider trades resolution for overview: coarse to read the broad shape, fine to
pin a cycle down to specific subdirectories.
