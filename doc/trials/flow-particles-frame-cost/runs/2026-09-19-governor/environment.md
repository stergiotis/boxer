---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment — 2026-09-19 governor comparison

- **Machine, toolchains, canvas:** as the [first run](../2026-09-19-first-run/environment.md).
- **Build under test:** boxer `961d98c8`; both hosts as built for the first run.
- **Governor:** the `amd-pstate-epp` driver, switched between `performance`
  and `powersave` (the system's default, with the `balance_performance`
  preference) through the operating system's own control, two rounds each,
  interleaved: performance, powersave, performance, powersave.
- **Not idle.** The desktop build of the gallery was running the `flowonmap`
  demo throughout — about one core between its two processes — and a browser
  was open. The one-minute load average at the start of the four rounds was
  2.7, 7.1, 5.0 and 4.7. Under that load the cores were already near their
  top frequency under `powersave`.
- **Cells:** both hosts; `segments-mesh` at 5 000 and 20 000 particles,
  `line-per-segment` at 5 000, and the floor. One launch per cell per round.
  The floor was launched twice per round by the way the script was invoked;
  the first is kept.

## The same demo, profiled live

Two 20-second CPU profiles of the desktop host's Go side showing `flowonmap`,
one under each governor, the view not held still between them: 8.57 s of
samples under `powersave`, 8.68 s under `performance`. Shares of the Go side:
the land overlay's per-frame projection and fill 50 % and 29 %; the flow layer
27 % and 40 %, of which FFFI slice marshalling is four fifths and the particle
simulation between a twentieth and a fifth; the pipe write 17 % and 14 %. The
difference between the two profiles is the view, not the governor.
