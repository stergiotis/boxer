// Package agentrunstore is the store half of the run journal (ADR-0308
// §SD1): two generated record stores on store-owned tables in the house
// layout — one row per run, latest-wins through the state view, and one
// row per entry, append-only — in the shape watchbill's tables take
// (ADR-0223 §SD1, ADR-0105 D3a). The journal is not a kind on
// `boxer.facts`: a run's entries are deleted with the run, never by the
// trail's expiry, and deleting a run must not touch the audit.
//
// Consumers open the pair through [NewStores] with a [Layout] naming their
// database and provision through [ProvisionIn]; the seam a writer uses is
// the parent package's StoreI.
package agentrunstore
