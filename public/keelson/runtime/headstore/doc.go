// Package headstore is the runtime's side of the anchor store (ADR-0314
// §SD2; the contract itself is `storage/recordstore/evidence/anchor` since
// ADR-0327 §SD7): the bus service `runtime.anchor.*` the host answers over
// an anchor store, its client, the journal's view of a head — a run's
// ordinal under the journal's scheme — with the head entry that records a
// receipt, the cadence as data (§SD3), law L32 over a run, and the
// `keelson('anchors')` relation. The implementation behind the service —
// a memory store here, a ClickHouse table on an org host or a write-once
// object store elsewhere — is the deployment's.
package headstore
