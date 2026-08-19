// Package sysmreplay reads stored system-metrics history back out of
// `boxer.facts` as [sysmsnap.BundleSnapshot] values — the inverse of
// [github.com/stergiotis/boxer/public/keelson/runtime/sysmtee].
//
// It is ADR-0197 M1: the reassembly half, with no consumer yet. Nothing here
// publishes on the metric plane and nothing here renders; a caller gets the
// same struct a live subscriber receives, and does with it whatever it would
// have done with a live one.
//
// # Two clocks, not one
//
// The tee stamps every kind of one bundle with a single timestamp
// (`sampleTime(snap)`), so the per-tick kinds re-assemble by an exact match on
// the order column rather than a tolerance join. Three kinds are not per-tick
// and would be lost by that match alone:
//
//   - `sysCpuInfo` — the CPU descriptor, written once on first sight of a host.
//   - `sysTopology` — the containment tree, written once on first sight.
//   - `sysSocket` — written only when the sockets collector's own stamp
//     changes, and dated by that stamp rather than by the bundle's.
//
// A live subscriber sees all three on every bundle: the collector restamps the
// descriptor each tick, the scraper stamps the same topology pointer onto every
// snapshot, and consecutive bundles repeat one sockets snapshot until the
// slower collector produces a new one. Replay reproduces that by carrying each
// forward — the most recent row at or before the tick being emitted — rather
// than leaving the domain nil on the ticks that carry no row of their own.
// Without it a replayed run would show a model name and a topology on its first
// frame and neither afterwards, which is not what was recorded.
//
// # What cannot come back
//
// Replay is bounded by what the tee stored, and four things were never stored
// (ADR-0197 §SD8): there is no sensors kind and no container kind, process
// command lines ride an opt-in kind that defaults to off, and
// [sysmsnap.BundleSnapshot.Errors] is not persisted — so a domain that failed
// at scrape time is indistinguishable here from one that was never wired. A
// caller that renders these must say "not recorded" rather than draw an empty
// panel.
//
// Two smaller losses are worth naming because they are silent. Each domain
// snapshot carries its own SampledAtUnixMs, and the tee stores only the
// bundle's, so a replayed domain reports the tick's stamp rather than the
// instant its own collector ran — sub-tick skew between collectors does not
// survive. And a network interface's IPv4/IPv6 lists and a mount's raw options
// string were dropped at write time, so they come back empty rather than as
// they were.
//
// # Alignment is checked here
//
// A kind's per-item arrays live in different leeway sections, so the co-length
// audit of ADR-0181 §SD5 does not cover them and index i meaning the same item
// in all of them is a contract the writer keeps rather than one the schema
// enforces. A violated row reads back plausibly — one interface's name against
// another's counters — so every multi-array decode below refuses a row whose
// arrays disagree instead of truncating to the shortest.
package sysmreplay
