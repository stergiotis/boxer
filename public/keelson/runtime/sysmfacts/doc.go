// Package sysmfacts holds the component DTOs that model a system-metrics
// sample as `boxer.facts` rows, and (from ADR-0184 M3) the record store
// generated over them.
//
// The DTOs and the generated store share this package by necessity: the
// emitted per-component codec keeps its DTO's own package clause, so a
// component declared elsewhere would land here as a second package.
//
// # Shape
//
// One kind per collector domain, one entity per (host, domain, tick), append
// only (ADR-0184 §SD3). `boxer.facts` binds its lifecycle role to a `z64`
// expiresAt rather than a `u8` marker, so the generated store emits no state
// view at all — there is no Delete and no latest-wins path to misuse, and that
// falls out of the schema rather than out of discipline.
//
// Envelope columns, on every kind:
//
//   - Id — xxh3 of "<host>/<domain>", so a kind's rows for one box form one
//     key and Replay over it is that box's history for that domain.
//   - NaturalKey — the (host, domain) pair, so the digest need not be inverted.
//   - Ts — the sample time, which is the store's Order lane.
//
// # Raw counters
//
// Fields carry what the collector read. Rates, windows and EWMAs are
// consumer-side views (ADR-0090 §SD3): a stored sample stays interpretable
// without knowing what window a writer had in mind, and two consumers may pick
// different ones.
//
// # Why the host repeats per kind
//
// Each kind names its own host membership (`sysmCpuHost`, `sysmMemHost`, …)
// rather than sharing one. A generated store declares a membership's kind
// symbol once per package and refuses two kinds naming the same membership;
// cross-kind sharing needs the reflect path, which a store does not use. See
// [github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab].
package sysmfacts
