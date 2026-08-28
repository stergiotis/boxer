// Package canonform computes the leeway canonical record form — ADR-0201 —
// and the digests over it.
//
// The form is a quotient, not a serialization: one leeway entity is mapped
// into the CBOR data model so that every change the ADR declares content-free
// vanishes (use / value / encoding aspects, numeric width and signedness where
// the value fits, temporal width, fixed vs. variable string width, the
// section an attribute lives in, the order of attributes, memberships and set
// elements, the cardinality channel a membership rides), the items are
// encoded under RFC 8949 §4.2 core deterministic rules plus the dCBOR §2.5
// numeric reduction (3.0 ≡ 3, -0.0 ≡ 0, one NaN), and BLAKE3 is taken over the
// bytes. Only primary memberships (membershiprole) are content. Nothing is
// materialized: every attribute item streams into its own keyed hasher and
// the entity item holds the plains plus the sorted 32-byte leaf digests.
//
// The CBOR writer is canonwire/runtime.CborWriter, shared with the leeway
// canonical wire (ADR-0210). What is the quotient's own — the numeric
// reduction (writeFloatReduced), the sorted memberships, the tag-258 sets —
// stays in this package and layers on top of that writer.
//
// Encoder is a streamreadaccess sink: it implements SinkI together with the
// optional MembershipSinkI, ArrowValueSinkI (Arrow views in place of the text
// lane — the form needs the exact Float32, not its rendering) and
// CoSectionTagSinkI capabilities, and is driven like any other sink:
//
//	enc, _ := canonform.NewEncoder(&tblDesc, ir, canonform.Options{})
//	err := driver.DriveRecordBatch(enc, rec)
//	// enc.NumRecords() digests of enc.DigestSize() bytes each, enc.RecordDigest(i)
//
// The bytes of the items exist only on the way into the hashers; tests and
// debugging capture them with NewRecordingDigester.
package canonform
