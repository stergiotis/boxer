// Package streamenc encodes leeway entities as the canonical wire (ADR-0210)
// from any Arrow batch a streamreadaccess.Driver can read — no generated
// per-table classes, only the table description the driver was built with.
// It is the route ADR-0219 SD3 adds for a consumer, like the play app, that
// reconstructs a table description at run time and has no generated code for
// it.
//
// Encoder is a streamreadaccess sink with the MembershipSinkI, ArrowValueSinkI
// and CoSectionTagSinkI capabilities — the shape canonform.Encoder has — and
// is driven the same way:
//
//	enc, _ := streamenc.NewEncoder(&tblDesc, ir)
//	err := driver.DriveRecordBatch(enc, rec)
//	// enc.Bytes() is the CBOR sequence; enc.Entity(i) one entity item
//
// The bytes are the ones the generated encoder of the same table emits with a
// nil tagger — the parity tests under this package pin that over every table
// in canonwire/example. Two things the generated encoder can do and this one
// states rather than supplies: no TaggerI, so an ambiguous source slot
// encodes without an SD5 discriminator; and the driver hands over whatever
// the Arrow lane holds, a zero ref included — the wire is lossless and does
// not second-guess a stored value.
//
// The slot table is canonwire.BuildSlotTable's (SD2); the entity, attribute
// and value forms are the runtime's writers, so the form has one
// implementation and this package only maps driver frames onto it: driven
// columns are matched to key positions by (section, column) name, and a
// co-section's tags to their membership group by section name.
package streamenc
