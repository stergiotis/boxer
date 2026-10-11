// Package anchor is the anchor store contract below the keelson runtime
// (ADR-0327 (proposed) §SD7): the append-only record
// `(cell, scope, ordinal, scheme, digest)` of chain heads and seals an
// organisation holds outside any person's control, answered with a
// receipt, so that a store written on a host the person administers can
// be checked against heads it cannot rewrite. The scope is a journal run,
// a trail seal stream or a downstream apply log; the scheme is recorded
// so a verifier dispatches on it. The first receipt for an ordinal under
// a scheme is the one that counts, and another digest there is refused.
//
// The package holds the contract, the memory store — the test double and
// the store of a host without an organisation's store — and the cadence
// as data. The bus service, its client and the journal's head entry are
// the runtime's (`keelson/runtime/headstore`).
package anchor
