// Package lwread reads leeway records as a person or a model reads them
// (ADR-0289 §SD3): each record as its attributes, each attribute
// named by what it is rather than by where it sits, its values spelled once
// for every reader, and every value the reading leaves out counted.
//
// A Sink collects a Model from a streamreadaccess drive:
//
//   - Names. A tagged attribute is named by its first membership, rendered
//     through the caller's membership.Renderer — a registry-backed one turns
//     a ref into its name — with its parameters spelled in. Its further
//     memberships are its labels. A section whose use-aspects declare every
//     membership secondary names its attributes by the section. A plain
//     column is named by the column. Where two sections name an attribute
//     alike, Model.Qualify prefixes both with their section.
//   - Values. Bytes read as text when they are printable UTF-8 and as 0x-hex
//     otherwise; a set's items are in value order; a list keeps its order and
//     is cut at Options.MaxItems with the count of what was cut. Each item
//     keeps the driver's text beside its spelling, for a gloss that reads it.
//   - Omissions. Value columns the readability aspect hides (machine-readable
//     and not human-readable) and values cut at Options.MaxValueBytes are
//     counted on the row.
//
// Model.Header summarises a batch: each attribute's name, section, type, how
// many records carry it, and the LW_GET handle an agent reads it with in SQL
// (ADR-0171).
//
// The reading is lossy by design. The lossless reading of a record is its
// canonical form (canonform, canonwire, and their diagnostic notation).
package lwread
