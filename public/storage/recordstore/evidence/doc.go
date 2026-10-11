// Package evidence holds the laws a record store under the evidence
// profile satisfies, and the hash constructions they are stated over
// (ADR-0327 (proposed) §SD1): a chain digest per row over the row's
// canonical item and its predecessor's digest; a commitment per vaulted
// component under an opening that is destroyed with the content; a mark
// that says what an absent component means; heads anchored outside the
// writer's control. The laws are functions over [RowI], so the run
// journal, the trail and a store outside this tree verify through one
// [Verify] and one [VerifyHeads].
//
// The package imports nothing from the keelson runtime, so a process that
// links none of it can write and verify evidence.
package evidence
