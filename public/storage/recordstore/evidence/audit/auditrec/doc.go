// Package auditrec is the recorder of audit events below the keelson
// runtime (ADR-0330): a sealed, buffered writer over a store a
// consumer generates with recordstore/gen, reached through a thin adapter
// ([StoreI], [BatchI]). It owns what every recorder of audit events must
// do the same way — the identity on the context and originBy, the
// audit-invalid row in place of one outside the bounds, a buffer that
// never blocks a writer, a bounded backlog whose loss is written back as
// an audit-gap row, a seal per durable flush chained to the run's last
// durable seal and anchored by scope, write-ahead, reads by subject and
// by principal, the seal law over a run, and forwarding by a prompt path
// and a backstop window — and decides no table, vocabulary, scheme or
// retention.
//
// The store binds audit.Origin and audit.AuditEvent, is generated with
// CanonWire and Evidence{Seals: true}, and its adapter lives in the
// store's package; testdata/example is one, and package auditrectest
// holds the conformance test every adapter runs. The package links no
// keelson, imzero2 or egui package (TestLinksNoRuntime).
package auditrec
