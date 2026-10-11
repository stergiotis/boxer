// Package anchorstore is the anchor store of ADR-0314 §SD2: the append-only
// record `(cell, run, ordinal, digest, receipt time)` of chain heads an
// organisation holds outside any person's control, so that a journal
// written on a host the person administers can be checked against heads it
// cannot rewrite. The contract is boxer's; the implementation — a memory
// store here, a ClickHouse table on an org host or a write-once object
// store elsewhere — is the deployment's. The package holds the store
// seam, the memory store, the bus service `runtime.anchor.*` with its
// client, the cadence as data (§SD3), the head entry's body, and law L32.
package anchorstore
