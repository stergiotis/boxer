---
type: explanation
audience: end-user
status: draft
title: About boxer
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# About boxer

boxer is licensed under the MIT License; the project NOTICE, reproduced
below, names the third-party software it incorporates and where the
complete list is kept. The splash window has three tabs: **Splash**, the
bundled artwork scaled to the window; **About**, the identity, licence and
build provenance of the running binary; and **NOTICE**, the project NOTICE
verbatim. This page carries the About tab's content, read from the running
process when the page is opened, so it says what the tab says.

## What the Build section reports

- **Revision** — the version-control revision the binary was built from,
  and whether the tree was clean or modified at build time; "no build
  info" when the binary carries none.
- **Module** — the Go module and version the binary reports.
- **Go**, **Host**, **PID**, **Run ID**, **Started** — the running
  process's toolchain, host name, process id, run identity and start time,
  when the process initialised its run information; otherwise one line
  says the metadata is unavailable.

The splash artwork is tinted through the design system's grayscale ramp;
when the easter-egg toggle is on, through viridis instead. A build without
the artwork shows "Splash image unavailable." and the other tabs are
unaffected.
