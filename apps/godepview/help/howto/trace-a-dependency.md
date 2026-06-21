---
type: how-to
audience: end-user
status: draft
title: Trace why you depend on a module
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Trace why you depend on a module

A third-party module showed up in your build and you want to know why. This
guide finds the import chain that pulls it in.

## Steps

1. Set the **view** switch to **Modules**.
2. Find the module in the table. Sort by **Fan-in** (how many of your packages
   import it) or **Blast** (how many would be affected by a change), and read the
   **Use** column — **direct** means one of your packages imports it straight;
   **transitive** means it is pulled in only through other modules.
3. Click the module's row. The detail pane shows its footprint — package count,
   fan-in, blast radius — and two lists: its **direct first-party importers** and
   its **blast radius** (everything transitively affected).
4. In the blast-radius list, click the package you are curious about. A
   **Why … depends on …** section appears with the shortest import chain from
   that package down to the module, one hop per line.
5. Click any hop to open that package in the **Packages** view.

## Tips

- The **Use = transitive** modules are the ones you never imported directly —
  often the most surprising entries. The witness path shows which dependency
  dragged each one in.
- A high **Blast** with **Fan-in = 0** means nothing imports the module directly,
  yet a change still ripples widely through a transitive chain.
- "Direct" here is derived from the import graph (some first-party package
  imports the module), not read from `go.mod`'s `require` directives — so it
  answers "do I actually import this?" rather than "is it a direct require?".
