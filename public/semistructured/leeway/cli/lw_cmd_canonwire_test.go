package cli

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// canonWireTestTableDto is a table small enough to read whole and shaped to
// carry both halves of the SD2 story: two sections that differ only by name
// share the `s` signature, one section is alone under `u64`, and there is a
// plain column so the plains half of the report is exercised.
func canonWireTestTableDto(t *testing.T) []byte {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("wiretest")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "blake3hash", ctabb.Y)
	manip.TaggedValueSection("stringy").
		AddSectionMembership(common.MembershipSpecLowCardRef).
		TaggedValueColumn("value", ctabb.S)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecLowCardRef).
		TaggedValueColumn("value", ctabb.S)
	manip.TaggedValueSection("count").
		AddSectionMembership(common.MembershipSpecHighCardRef).
		TaggedValueColumn("n", ctabb.U64)
	dto, err := manip.BuildTableDescDto()
	require.NoError(t, err)

	marshaller, err := common.NewTableMarshaller()
	require.NoError(t, err)
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	require.NoError(t, marshaller.EncodeDtoCbor(buf, &dto))
	return buf.Bytes()
}

// TestCanonWireSlotsNamesSignatures pins what the operator-facing view is for:
// the wire keys sections by canonical type signature, so the question "which
// key carries my section, and does anything else claim it" has to be
// answerable from the table description alone.
func TestCanonWireSlotsNamesSignatures(t *testing.T) {
	rep, err := canonWireSlots(bytes.NewReader(canonWireTestTableDto(t)), "")
	require.NoError(t, err)

	// No --tableName: the description's own name labels the report.
	require.Equal(t, "wiretest", rep.TableName)

	sigs := make([]string, 0, len(rep.Slots))
	for _, slot := range rep.Slots {
		sigs = append(sigs, slot.Signature)
	}
	require.Equal(t, []string{"s", "s", "u64"}, sigs)

	// The two `s` slots are the ones a decoder cannot resolve on its own.
	require.Equal(t, []string{"stringy"}, rep.Slots[0].Sections)
	require.True(t, rep.Slots[0].Ambiguous)
	require.Equal(t, []string{"symbol"}, rep.Slots[1].Sections)
	require.True(t, rep.Slots[1].Ambiguous)
	require.Equal(t, []string{"count"}, rep.Slots[2].Sections)
	require.False(t, rep.Slots[2].Ambiguous)

	// And the list a reader checks before deciding whether the table needs a
	// dispatcher at all.
	require.Equal(t, []string{"s"}, rep.Ambiguous)

	require.Len(t, rep.Plains, 1)
	require.Equal(t, common.PlainItemTypeEntityId.String(), rep.Plains[0].ItemType)
	require.Equal(t, "y", rep.Plains[0].Group)
}

// TestCanonWireSlotsTableNameOverride: the flag only labels the report, so a
// caller piping a description whose dictionary entry is empty still gets a
// named one.
func TestCanonWireSlotsTableNameOverride(t *testing.T) {
	rep, err := canonWireSlots(bytes.NewReader(canonWireTestTableDto(t)), "elsewhere")
	require.NoError(t, err)
	require.Equal(t, "elsewhere", rep.TableName)
}

// canonWireGoldenEntity is the entity item pinned by the runtime's own golden
// (ADR-0207 §SD1): version 1, one plain entity id, and two slots — `s` and
// `u64` — each with one attribute.
const canonWireGoldenEntity = "8301a10181182aa26173818281820001626869637536348182818201416d07"

func TestCanonWireVerifyGolden(t *testing.T) {
	b, err := hex.DecodeString(canonWireGoldenEntity)
	require.NoError(t, err)

	rep, err := canonWireVerify(b, false)
	require.NoError(t, err)
	require.Equal(t, 1, rep.Entities)
	require.Equal(t, len(b), rep.Bytes)

	// The same bytes twice are a CBOR sequence of two entities (RFC 8742):
	// there is no framing to add.
	rep, err = canonWireVerify(append(bytes.Clone(b), b...), true)
	require.NoError(t, err)
	require.Equal(t, 2, rep.Entities)
	require.Equal(t, 2*len(b), rep.Bytes)
}

// TestCanonWireVerifyRejectsCorrupted covers the two ways bytes stop being
// canonical without stopping being CBOR — a version this build does not
// implement, and a map whose keys are in the wrong order. The second is the
// one a permissive decoder would accept and a canonical form must not: the
// entity means the same thing either way, and only one spelling of it is
// allowed.
func TestCanonWireVerifyRejectsCorrupted(t *testing.T) {
	t.Run("unsupported version", func(t *testing.T) {
		b, err := hex.DecodeString(canonWireGoldenEntity)
		require.NoError(t, err)
		require.EqualValues(t, runtime.Version, b[1], "the second byte is the version")
		b[1] = byte(runtime.Version) + 1

		_, err = canonWireVerify(b, false)
		require.ErrorIs(t, err, runtime.ErrVersion)
	})

	t.Run("slot keys out of order", func(t *testing.T) {
		// The golden's two tagged entries, swapped: "u64" sorts after "s"
		// bytewise, so leading with it is not canonical.
		const swapped = "8301a10181182a" + "a2" +
			"63753634" + "8182818201416d07" +
			"6173" + "818281820001626869"
		b, err := hex.DecodeString(swapped)
		require.NoError(t, err)

		_, err = canonWireVerify(b, false)
		require.ErrorIs(t, err, runtime.ErrNotCanonical)
	})

	t.Run("truncated", func(t *testing.T) {
		b, err := hex.DecodeString(canonWireGoldenEntity)
		require.NoError(t, err)

		_, err = canonWireVerify(b[:len(b)-1], false)
		require.Error(t, err)
	})
}
