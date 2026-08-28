// Package runtime is the runtime of the leeway canonical wire — ADR-0207:
// the pieces a generated per-table encoder or decoder calls, as
// opposed to the pieces the generator emits. The generator itself, and the
// slot table it builds from a common.TableDesc, are the parent canonwire
// package; nothing here reads a table description.
//
// That is the CBOR writer, taken over from canonform so the two forms share
// one implementation of the RFC 8949 §4.2 core-deterministic rules; a reader
// strict in the same direction, refusing an encoding that writer would not
// have produced; the SD3 value forms over the Go types the generated
// readaccess and dml APIs exchange; the entity, attribute and membership forms
// of SD1–SD5, each with a writer that sorts into canonical order and a
// cursor-style reader that checks the order it finds; the table-free view a
// decoder hands to the SD5 dispatch, and the refusals it makes against its own
// table. VerifyCanonical is the order check without a table, over bytes from
// anywhere.
//
// The writer applies no quotient: canonform layers its own rules on top of it
// (the dCBOR §2.5 numeric reduction, sorted memberships, tag-258 sets), and
// its bytes do not move.
package runtime
