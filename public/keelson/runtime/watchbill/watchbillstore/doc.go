// Package watchbillstore is the store half of watchbill (ADR-0223 §SD1):
// two generated record stores on store-owned tables in the house layout —
// one row per job, updated in place, and one row per transition, appended —
// plus the claim and sweep statements the worker issues against the job
// table.
//
// The job table is not a kind on `boxer.facts`: a claim is a conditional
// lightweight UPDATE of one row (§SD3), which needs an immutable key, the
// block-position columns the feature requires, and the store's own expiry
// — three things a writer on the shared facts table cannot control
// (ADR-0184, Consequences). Every attribute is a section of its own, so a
// claim rewrites one array element and guards on another.
//
// Consumers open the pair through [NewStores] with a [Layout] naming their
// database, and provision through [ProvisionIn]; nothing here touches the
// bus or spawns work.
package watchbillstore
