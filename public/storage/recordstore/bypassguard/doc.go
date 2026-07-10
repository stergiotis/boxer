// Package bypassguard proves, from outside the store's package tree, that a
// generated store's dml builder keeps its frame control walled off
// (ADR-0100 SD6).
//
// It is rooted under recordstore/, not under example/, so Go's internal rule
// forbids it from importing example/internal/lowlevel — and the builder's
// control methods (BeginEntity/CommitEntity/RollbackEntity/TransferRecords/
// the plain setters/Builder) are emitted unexported, so no interface
// assertion can name them either. The two halves of the proof:
//
//   - TestRawControlIsWalled (runtime): a Raw() builder does not satisfy any
//     interface naming a control method, while the safe section surface stays
//     reachable. If a refactor re-exported a control method, an assertion
//     there would start succeeding and fail the test.
//   - bypass_attempt.go + TestControlBypassDoesNotCompile (compile time): the
//     bypass an external caller would write, which must fail to compile.
package bypassguard
