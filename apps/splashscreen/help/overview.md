---
type: explanation
audience: end-user
status: draft
title: About boxer
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# About boxer

boxer is a library, licensed under the MIT License. The program it runs in
may be a different one, with its own copyright and licence. The splash
window keeps the two apart: each copyright holder it credits has a tab of
its own, and that tab shows only what that holder supplied.

The first row of tabs names the credited parties — the program, when it
registered itself, then **boxer** — and **Build**. Under a party, the
second row picks one of two pages: **Splash**, its artwork scaled to the
window, and **About**, its copyright line, its licence and its NOTICE
verbatim. A party without artwork has no Splash page. The window opens on
the first party's artwork.

**Build** belongs to neither party: it describes the running binary. This
page carries each party's About content and the Build fields, read from the
running process when the page is opened, so it says what the tabs say.

## What the Build tab reports

- **Revision** — the version-control revision the binary was built from,
  and whether the tree was clean or modified at build time; "no build
  info" when the binary carries none.
- **Module** — the Go module the binary was built as, which is the
  program's when boxer is linked into one.
- **Go**, **Host**, **PID**, **Run ID**, **Started** — the running
  process's toolchain, host name, process id, run identity and start time,
  when the process initialised its run information; otherwise one line
  says the metadata is unavailable.

boxer's artwork is tinted through the design system's grayscale ramp; when
the easter-egg toggle is on, through viridis instead. A program's artwork
is shown in its own colours, in the version for the light or the dark
theme. A build without boxer's artwork shows "Splash image unavailable."
and the other pages are unaffected.
