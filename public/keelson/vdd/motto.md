---
type: explanation
audience: keelson/vdd stakeholder reading the framing
status: stable
reviewed-by: "@spx"
reviewed-date: 2026-05-18
---

VDD = VCS managed Dimensional Data.

Central registry of tag values and leeway memberships used across the
keelson runtime layer (runtime.facts producers and consumers, the
forthcoming Go↔leeway codec generator, and any keelson-side fact-kind
that needs a stable membership identity).

Pattern mirrors `src/go/public/boxerstaging/spinnaker/vdd/`. Memberships
are declared as `Memb*` package-level vars built through
`KeelsonHrNkRegistry.MustBegin(...).End()`; their `TaggedId` is stable
across rebuilds and survives in RowBinary/Arrow wire payloads.

Goal is to establish a controlled vocabulary that is concise, malleable and has 100% coverage for `keelson` internal
leeway data.
