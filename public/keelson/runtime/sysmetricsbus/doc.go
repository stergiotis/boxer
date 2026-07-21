// Package sysmetricsbus carries system-metrics snapshots over a bus as a
// unidirectional publish/subscribe data plane (ADR-0090). A Producer wraps
// a sysmetrics.Bundle, ticks, and publishes; a Consumer subscribes and
// hands each decoded snapshot to a callback. Both speak app.BusI, so the
// same code runs over inprocbus (co-located, the ADR-0090 "special case")
// and, later, NATS core — the transport is the caller's choice.
//
// Scope of this cut (ADR-0090 phase P2 — "bisect the Sampler over
// inprocbus"). Two SD-level refinements are deliberately deferred so the
// producer/consumer split can land first:
//
//   - SD1 per-domain subjects. P2 publishes the whole BundleSnapshot on a
//     single subject (BundleSubject). The sysmetrics.{host}.{domain}
//     fan-out — granular subscription, per-domain grants — comes later.
//   - SD3 leeway-facts codec. P2 ships CBORCodec (fxamacker/cbor, already a
//     dependency — no new dep). The chosen wire is "metrics are leeway
//     facts, reuse the boxer.facts codec"; CBORCodec is the interim that
//     the Codec interface lets us swap without touching Producer/Consumer.
//
// The package is transport-agnostic on purpose: it imports app (for BusI),
// not inprocbus. The dataflow is strictly one way — there is no
// consumer→producer channel here.
//
// Placement: keelson/runtime, shared (ADR-0090 SD2). P2 kept this app-local
// under apps/imztop while imztop was the only consumer; P3 adds the
// standalone sysmetricsd scraper as a second, cross-process consumer, so the
// shared pieces (Producer, Codec, subjects) were promoted here. Co-located
// consumers (imztop) drive the Producer + Consumer over inprocbus; the
// standalone scraper drives only the Producer over NATS (see runtime/natsbus).
package sysmetricsbus
