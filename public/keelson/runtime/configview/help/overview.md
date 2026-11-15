---
type: explanation
audience: operator
status: draft
title: Reading the config inspector
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading the config inspector

The inspector lists every environment variable this process declares in its
registry, with the value in force. It changes nothing: there is no way to
set a variable from here. A variable is set before the process starts, in
its environment or through the command-line flag the row names.

A variable the process reads without declaring it does not appear. The
registry is the list, not the environment.

## Filter row

- **Search** — narrows to the variables whose name or description contains
  the text, without regard to case. A section with no match is left out.
- **Only set** — keeps the variables that are set.
- **Reveal sensitive** — shows the values of sensitive variables in this
  window. See *Sensitive values* below before switching it on.

When nothing matches, the list says so in one line.

## Sections

Variables are grouped by category. A section's row carries its name and how
many of its variables are set, as `(set / total)`, so a closed section still
says whether anything in it was changed. The counts follow the filter: with
a search active, the total is the number of matches in that section.

## A variable's row

From left to right:

- **A dot** — filled in the accent colour when the variable is set, muted
  when it is not.
- **A type chip** — string, bool, number, duration, path or one of a fixed
  set of words. Hover it for the full type name.
- **A lock** — the variable is sensitive.
- **The name.**
- **The value** — as the process reads it. `<unset>` means the variable is
  not set, which is not the same as set to an empty string. A long value is
  cut, with its full length in characters after the cut. Hover the value for
  the default and the package that declares the variable.
- **A flag name**, after the value, when a command-line flag sets the same
  variable.
- **The description** — cut to its column, in full on hover.

## Sensitive values

A sensitive variable's value shows as `********`, always eight characters,
so the mask does not give the length away. **Reveal sensitive** shows the
values in this window only, until it is switched off or the window closes.
It also reveals a sensitive default in the hover text. Switch it off before
sharing the screen or taking a screenshot.

An agent never receives a sensitive value, whatever this window shows.

## What an agent can do here

The window offers operations to a chat's model. It can list the variables
matching a search, read one variable, and read which sections are open. It
can set the search and **Only set**, highlight a variable's row, and open or
close a section, which you see happen. It cannot reveal sensitive values and
it cannot set a variable.
