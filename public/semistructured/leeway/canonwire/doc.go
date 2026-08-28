// Package canonwire is the leeway canonical wire generator — ADR-0207
// (proposed): the pieces that read a table description at generation time and
// emit a per-table encoder and decoder, as opposed to the pieces those emitted
// classes call at run time.
//
// That is the slot table of SD2 — a common.TableDesc reduced to the CT
// signatures that key its tagged slots, the ambiguity sets SD5's dispatch has
// to resolve, and the channel-to-spec mapping the narrowing step reads — and,
// on top of it, the generator driver that emits one table's canonical-wire
// classes: the signature constants, the slot enum, the SD5 tagger and
// dispatcher interfaces with their built-in ordinal implementations, the
// encoder over the table's generated readaccess classes and the decoder into
// its generated dml builders.
//
// The wire runtime is the runtime subpackage: the CBOR writer and reader, the
// value forms of SD3, the membership form of SD4, the entity form of SD1, the
// AttributeView the dispatch reads and the table-free canonical-order checker,
// none of which see a table description.
//
// Encoding straight from a dml entity under construction, and decoding into a
// readaccess-shaped view without an Arrow batch, are not here: a consumer that
// needs either goes through the runtime directly.
package canonwire
