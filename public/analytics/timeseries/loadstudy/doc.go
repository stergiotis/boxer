// Package loadstudy pulls real recorded load metrics and application events out
// of ClickHouse so the ADR-0150 detectors can be measured against something
// nobody synthesised.
//
// Every accuracy figure in
// [github.com/stergiotis/boxer/public/analytics/timeseries/adscore] and its
// siblings comes from fixtures this repository generated. That is a real gap:
// a generator and a detector written by the same hand can agree with each other
// about a signal that does not exist. This package closes it with data that was
// recorded for other reasons entirely.
//
// # What it reads, and what it does not
//
// Load metrics come from ClickHouse's own `system.asynchronous_metric_log`,
// sampled at 1 Hz: CPU split into user, system and iowait, run-queue depth,
// resident memory, block IO in both directions, inbound network. Per-device
// families are summed across devices, so the channel set is portable across
// machines whose disks and interfaces are named differently.
//
// They do *not* come from
// [github.com/stergiotis/boxer/public/observability/sysmetrics], which would be
// the natural source. That scraper published to NATS and persisted nothing, so
// its data did not exist to analyse — the gap this package's fallback was
// chosen around, and the demand ADR-0184 cites for closing it.
//
// That gap is now closeable: `sysmetricsd --tee` writes the plane into
// `boxer.facts` through
// [github.com/stergiotis/boxer/public/keelson/runtime/sysmtee]. Migrating these
// channels onto it is a separate decision and has not been made — the study's
// published results were computed against `system.asynchronous_metric_log`, and
// swapping the source silently would make old and new runs incomparable.
//
// An earlier version of this comment added that `boxer.facts` "carries no
// numeric payload at all — it is an event log". That was wrong about the
// schema even when written: the table has the full `u8`…`i64Array` set,
// `u32Set`/`u64Set`, and `f32Array`/`f64Array` under an encoding hint chosen
// for slowly-changing series. It was not quite right about the contents
// either — [github.com/stergiotis/boxer/public/gov/capmapfacts] has been
// writing an f64 there, a normalized compression distance, since before any
// of this. What was true, and only this, is that nothing wrote *load
// metrics*; the tee is what changes that.
//
// Events come from `boxer.facts`: application lifecycle, run starts and stops.
// Heartbeats are excluded, because they fire on a timer rather than on anything
// happening.
//
// # The caveat that governs how any result may be read
//
// **Events are not anomaly labels.** An application starting is normal
// behaviour. Scoring a detector against event times measures whether it fires
// when the workload composition changed — useful, and not the same as accuracy.
// Two consequences follow and neither is optional:
//
//   - A detection with no event near it is not necessarily a false positive.
//     The precision term is therefore pessimistic by an unknown amount.
//   - Any figure produced here is meaningful only *relative to* the one-liner
//     baselines in adscore, run over the same series. If a moving-average
//     residual correlates with events just as well, then the correlation is
//     trivial and says nothing about the detector.
//
// [EventLabels] widens each event across a tolerance, because a start changes
// what the machine does over the following seconds rather than in the bin the
// log line landed in. That tolerance is the study's most arguable parameter.
//
// # Gaps
//
// The 1 Hz source is not gap-free: ClickHouse is not always running. Bins with
// no sample are forward-filled and counted in [Series.Gaps]. A large count means
// part of the series is invented, and a report that does not say so is
// misleading.
//
// # Running it
//
// The study itself is an integration test in this package, carrying
// `//go:build integration` because it needs a live server. It skips when
// CLICKHOUSE_ENDPOINT is unset. See
// doc/adr/0150-timeseries-subsequence-anomaly-detection.md for what it was
// built to decide.
package loadstudy
