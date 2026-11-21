package example

import (
	"bytes"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	rartime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// The refusals a generated decoder makes against its own table. Each is driven
// from bytes built by hand rather than by the encoder, because the encoder can
// only produce entities the table declares — which is the point: these are the
// failures of a *foreign* producer, and they are what a decoder that trusted
// its input would turn into a corrupt batch.

// buildEntity writes one entity item through the shared CBOR writer and hands
// back its bytes.
func buildEntity(t *testing.T, write func(w *cwruntime.CborWriter)) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := cwruntime.NewCborWriter(&buf)
	require.NoError(t, err)
	write(w)
	require.NoError(t, w.Err())
	return buf.Bytes()
}

// TestDecoderNilDispatcherRefused is the construction check of ADR-0210 SD5:
// the JSON table has two ambiguous signatures, so a decoder for it cannot be
// built without a dispatcher, and the refusal is once and early rather than at
// the first attribute that would have needed the hook.
func TestDecoderNilDispatcherRefused(t *testing.T) {
	dml := NewInEntityJson(memory.DefaultAllocator, 8)
	dec, err := NewCanonWireDecoderJson(dml, nil)
	require.Error(t, err)
	require.Nil(t, dec)
	require.ErrorIs(t, err, cwruntime.ErrDispatch)

	// The tables with no ambiguous signature take nil, which is the other half
	// of the same generated constant.
	other, err := NewCanonWireDecoderPlace(NewInEntityPlace(memory.DefaultAllocator, 8), nil)
	require.NoError(t, err)
	require.NotNil(t, other)
}

// TestDecoderUnknownSignature is the cross-table portability failure made
// explicit: the source declared a type the target does not.
func TestDecoderUnknownSignature(t *testing.T) {
	item := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version)
		w.MapHead(0)
		w.MapHead(1)
		w.WriteTextString("u32") // no slot of place carries it
		w.ArrayHead(1)
		w.ArrayHead(2)
		w.ArrayHead(0)
		w.WriteUint(7)
	})
	dec, err := NewCanonWireDecoderPlace(NewInEntityPlace(memory.DefaultAllocator, 8), nil)
	require.NoError(t, err)
	_, err = dec.DecodeEntity(item)
	require.ErrorIs(t, err, cwruntime.ErrUnknownSlot)
}

// TestDecoderUnknownPlain is the same refusal on the plain side: the item type
// is fixed leeway vocabulary, so the key reads, but the target declares no
// column of it.
func TestDecoderUnknownPlain(t *testing.T) {
	item := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version)
		w.MapHead(1)
		w.WriteUint(uint64(common.PlainItemTypeTransaction))
		w.ArrayHead(1)
		w.WriteUint(1)
		w.MapHead(0)
	})
	dec, err := NewCanonWireDecoderPlace(NewInEntityPlace(memory.DefaultAllocator, 8), nil)
	require.NoError(t, err)
	_, err = dec.DecodeEntity(item)
	require.ErrorIs(t, err, cwruntime.ErrUnknownPlain)
}

// TestDecoderChannelNotAccepted drives the narrowing step of ADR-0210 SD5 on
// an unambiguous slot: place's `tags` section declares LowCardVerbatim only, so
// a membership arriving on the LowCardRef channel has nowhere to be stored even
// though the signature matches.
func TestDecoderChannelNotAccepted(t *testing.T) {
	item := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version)
		w.MapHead(0)
		w.MapHead(1)
		w.WriteTextString(CanonWireSignaturePlaceTags)
		w.ArrayHead(1)
		w.ArrayHead(3)
		w.ArrayHead(1)
		w.WriteMembership(mappingplan.MembershipChannelLowCardRef, 5, nil, nil)
		w.ArrayHead(1) // tag, the `h` co-container
		w.WriteTextString("alpha")
		w.Tag(cwruntime.TagSet) // tag_id, the `m` one
		w.ArrayHead(1)
		w.WriteUint(11)
	})
	dec, err := NewCanonWireDecoderPlace(NewInEntityPlace(memory.DefaultAllocator, 8), nil)
	require.NoError(t, err)
	_, err = dec.DecodeEntity(item)
	require.ErrorIs(t, err, cwruntime.ErrChannelNotAccepted)
}

// TestDecoderCoContainerLength is the one shape the wire admits and the dml
// cannot take: a section's container columns are co-containers, appended one
// element to all of them at once, so an attribute whose `words` and
// `word_length` differ in length has no way in.
func TestDecoderCoContainerLength(t *testing.T) {
	item := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version)
		w.MapHead(0)
		w.MapHead(1)
		w.WriteTextString(CanonWireSignatureTestTableText) // s-sh-u32h
		w.ArrayHead(1)
		w.ArrayHead(4)
		w.ArrayHead(0)
		w.WriteTextString("text")
		w.ArrayHead(3) // words
		w.WriteTextString("a")
		w.WriteTextString("b")
		w.WriteTextString("c")
		w.ArrayHead(2) // word_length — one short
		w.WriteUint(1)
		w.WriteUint(1)
	})
	dec, err := NewCanonWireDecoderTestTable(NewInEntityTestTable(memory.DefaultAllocator, 8), nil)
	require.NoError(t, err)
	_, err = dec.DecodeEntity(item)
	require.ErrorIs(t, err, cwruntime.ErrCoContainerLength)
}

// TestDecoderVersionMismatch: the form is not forwards-compatible by design
// (ADR-0210 SD1), so a decoder that meets a version it does not implement has
// no grounds to guess at the rest.
func TestDecoderVersionMismatch(t *testing.T) {
	item := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version + 1)
		w.MapHead(0)
		w.MapHead(0)
	})
	dec, err := NewCanonWireDecoderPlace(NewInEntityPlace(memory.DefaultAllocator, 8), nil)
	require.NoError(t, err)
	_, err = dec.DecodeEntity(item)
	require.ErrorIs(t, err, cwruntime.ErrVersion)
}

// TestDecoderRecoversAfterRefusal: a refused entity is rolled back, so the
// builder is where it was and the next entity still decodes into it. Without
// that, one bad entity in a sequence would poison the batch.
func TestDecoderRecoversAfterRefusal(t *testing.T) {
	bad := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version)
		w.MapHead(0)
		w.MapHead(1)
		w.WriteTextString("u32")
		w.ArrayHead(1)
		w.ArrayHead(2)
		w.ArrayHead(0)
		w.WriteUint(7)
	})
	good := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version)
		w.MapHead(1)
		w.WriteUint(uint64(common.PlainItemTypeEntityId))
		w.ArrayHead(1)
		w.WriteUint(42)
		w.MapHead(1)
		w.WriteTextString(CanonWireSignaturePlaceTags)
		w.ArrayHead(1)
		w.ArrayHead(3)
		w.ArrayHead(1)
		w.WriteMembership(mappingplan.MembershipChannelLowCardVerbatim, 0, []byte("kind"), nil)
		w.ArrayHead(1) // tag, the `h` co-container
		w.WriteTextString("alpha")
		w.Tag(cwruntime.TagSet) // tag_id, the `m` one
		w.ArrayHead(1)
		w.WriteUint(11)
	})

	dml := NewInEntityPlace(memory.DefaultAllocator, 8)
	dec, err := NewCanonWireDecoderPlace(dml, nil)
	require.NoError(t, err)
	_, err = dec.DecodeEntity(bad)
	require.ErrorIs(t, err, cwruntime.ErrUnknownSlot)
	n, err := dec.DecodeEntity(good)
	require.NoError(t, err)
	require.Equal(t, len(good), n)

	ra := NewReadAccessPlace()
	transfer(t, dml.TransferRecords, ra.LoadFromRecord)
	require.Equal(t, 1, ra.GetNumberOfEntities())
	require.EqualValues(t, 42, ra.EntityId.GetAttrValueId(0))
	require.EqualValues(t, 1, ra.Tags.Attributes.GetNumberOfAttributes(0))
}

// firstCandidateDispatcher takes whichever candidate the narrowing step left
// first. It is the dispatcher a consumer writes when the content really does
// decide nothing — and it is what makes the SD5 expectation visible: without a
// tagger, name-only-distinguished sections are not round-trippable, and the
// decoder lands them all in one section rather than failing.
type firstCandidateDispatcher struct{}

var _ CanonWireDispatcherJsonI = firstCandidateDispatcher{}

func (inst firstCandidateDispatcher) Dispatch(candidates []CanonWireSlotJsonE, attr *cwruntime.AttributeView) (slot CanonWireSlotJsonE, err error) {
	_ = attr
	if len(candidates) == 0 {
		return 0, errors.New("no candidate")
	}
	return candidates[0], nil
}

// TestJsonWithoutTaggerCollapsesAmbiguity is the negative half of the JSON
// story. Encoded without a tagger no attribute carries a discriminator, so the
// built-in ordinal dispatcher has nothing to read and refuses; a dispatcher
// that guesses succeeds, but every attribute of an ambiguity set lands in that
// set's first slot. This is the stated consequence of ADR-0210 SD5, not a bug:
// null and undefined have the same signature *and* the same memberships.
func TestJsonWithoutTaggerCollapsesAmbiguity(t *testing.T) {
	src := NewInEntityJson(memory.DefaultAllocator, 128)
	writeJson(t, src)
	raFirst := NewReadAccessJson()
	transfer(t, src.TransferRecords, raFirst.LoadFromRecord)

	enc, err := NewCanonWireEncoderJson(raFirst, nil)
	require.NoError(t, err)
	var untagged bytes.Buffer
	require.NoError(t, enc.EncodeAll(&untagged))

	// The built-in ordinal dispatcher decides by the discriminator, and there
	// is none.
	strict := NewInEntityJson(memory.DefaultAllocator, 128)
	decStrict, err := NewCanonWireDecoderJson(strict, CanonWireOrdinalDispatcherJson{})
	require.NoError(t, err)
	_, err = decStrict.DecodeAll(untagged.Bytes())
	require.ErrorIs(t, err, cwruntime.ErrDispatch)

	dst := NewInEntityJson(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderJson(dst, firstCandidateDispatcher{})
	require.NoError(t, err)
	decoded, err := dec.DecodeAll(untagged.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, decoded)

	raSecond := NewReadAccessJson()
	transfer(t, dst.TransferRecords, raSecond.LoadFromRecord)

	valueless := func(ra *ReadAccessJson, idx rartime.EntityIdx) (undefined int64, null int64, emptyObject int64, emptyArray int64) {
		return ra.Undefined.Memberships.AccelMixedLowCardVerbatim.GetEntityAttributeCount(int(idx)),
			ra.Null.Memberships.AccelMixedLowCardVerbatim.GetEntityAttributeCount(int(idx)),
			ra.EmptyObject.Memberships.AccelMixedLowCardVerbatim.GetEntityAttributeCount(int(idx)),
			ra.EmptyArray.Memberships.AccelMixedLowCardVerbatim.GetEntityAttributeCount(int(idx))
	}
	for e := range raFirst.GetNumberOfEntities() {
		idx := rartime.EntityIdx(e)
		u0, n0, o0, a0 := valueless(raFirst, idx)
		u1, n1, o1, a1 := valueless(raSecond, idx)
		// Everything value-less landed in the ambiguity set's first slot.
		require.Equal(t, u0+n0+o0+a0, u1)
		require.Zero(t, n1)
		require.Zero(t, o1)
		require.Zero(t, a1)
		// And everything of signature `s` landed in `string`, the first of
		// that set.
		require.Equal(t,
			raFirst.String.Attributes.GetNumberOfAttributes(idx)+raFirst.Symbol.Attributes.GetNumberOfAttributes(idx),
			raSecond.String.Attributes.GetNumberOfAttributes(idx))
		require.Zero(t, raSecond.Symbol.Attributes.GetNumberOfAttributes(idx))
		// A section outside both ambiguity sets is untouched by any of this.
		require.Equal(t, raFirst.Bool.Attributes.GetNumberOfAttributes(idx),
			raSecond.Bool.Attributes.GetNumberOfAttributes(idx))
	}
}

// TestDecoderFixedTextWidthRefused: an sx column's text must be exactly the
// declared width — the encoder reads it from a FixedSizeBinary array, so a
// different length is a value no writer produced (ADR-0210 SD2's typed reads).
func TestDecoderFixedTextWidthRefused(t *testing.T) {
	item := buildEntity(t, func(w *cwruntime.CborWriter) {
		w.ArrayHead(3)
		w.WriteUint(cwruntime.Version)
		w.MapHead(0)
		w.MapHead(1)
		w.WriteTextString(CanonWireSignatureFixedTableCode) // "sx4"
		w.ArrayHead(1)
		w.ArrayHead(2)
		w.ArrayHead(1)
		w.WriteMembership(mappingplan.MembershipChannelLowCardRef, 5, nil, nil)
		w.WriteTextString("abc") // three bytes in a four-byte slot
	})
	dec, err := NewCanonWireDecoderFixedTable(NewInEntityFixedTable(memory.DefaultAllocator, 8), nil)
	require.NoError(t, err)
	_, err = dec.DecodeEntity(item)
	require.ErrorIs(t, err, cwruntime.ErrOutOfRange)
	require.ErrorContains(t, err, "fixed width")
}
