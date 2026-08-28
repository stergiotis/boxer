package runtime

import "errors"

// The refusals a generated decoder makes against its own table (ADR-0210 SD5,
// SD6). They are the class of failure the CborReader and the cursors cannot
// see: the bytes are well-formed, in the deterministic subset and in canonical
// order, but they describe something the target table does not declare, or
// declares in a shape that cannot hold them.
//
// They live in the runtime rather than in the generator so a consumer can
// match on them with errors.Is without importing the generator package.
var (
	// ErrUnknownPlain is a plain item type the target table declares no
	// column of. A plain section is keyed by its PlainItemTypeE ordinal
	// (ADR-0210 SD2, fork 1), which is fixed leeway vocabulary, so the key is
	// readable and the refusal is about the table and not about the bytes.
	ErrUnknownPlain = errors.New("the table declares no plain section of this item type")
	// ErrUnknownSlot is a CT signature no slot of the target table carries.
	// It is the cross-table portability failure the form is designed to make
	// explicit: the source declared a type the target does not.
	ErrUnknownSlot = errors.New("the table has no slot for this canonical type signature")
	// ErrChannelNotAccepted is a membership arriving on a channel the target
	// section's MembershipSpecE does not declare — the narrowing step of
	// ADR-0210 SD5 having eliminated every candidate, or an unambiguous slot
	// whose one section cannot store the carriage.
	ErrChannelNotAccepted = errors.New("the section does not accept the membership channel")
	// ErrCoContainerLength is an attribute whose container columns disagree in
	// length within one section. The DML appends one element to all of a
	// section's containers at once (they are co-containers), so a decoder
	// cannot write an attribute whose `h` and `m` columns differ in length.
	ErrCoContainerLength = errors.New("co-container columns of one section differ in length")
	// ErrDispatch is the pluggable dispatch of ADR-0210 SD5 failing: a
	// decoder constructed without one for a table that has an ambiguous
	// signature, a dispatcher returning a slot that is not among the
	// candidates it was handed, or the built-in ordinal dispatcher meeting an
	// attribute that carries no discriminator.
	ErrDispatch = errors.New("unable to dispatch the attribute to a slot")
)
