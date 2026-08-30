package canonform

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stretchr/testify/require"
)

// The single-membership declaration (ADR-0213) is REPRESENTATION: it moves
// the physical layout (the declared channel's cardinality lane is omitted)
// and must not move a single digest — the ADR-0201 SD2 erasure list names
// membership spec and cardinality channels, and the declaration is spelled
// as a section use-aspect no classifier consults.

// singleDeclTable is "id + one tagged section" carrying a low-card-verbatim
// channel (declared single-instance or not — the one difference) beside a
// ragged high-card-ref channel, so the two carriages coexist in one section.
func singleDeclTable(t *testing.T, declared bool) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) {
	t.Helper()
	return buildTable(t, func(manip *common.TableManipulator) {
		if declared {
			manip.SetTableName("smd")
		} else {
			manip.SetTableName("smf")
		}
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		sec := manip.TaggedValueSection("tags").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim, common.MembershipSpecHighCardRef)
		if declared {
			sec.AddSectionSingleMembership(common.MembershipSpecLowCardVerbatim)
		}
		sec.TaggedValueColumn("value", ctabb.S)
	})
}

// singleDeclBatch is two entities of identical content for both variants —
// the declared IR simply never asks for the lv cardinality column, the flat
// one gets the all-ones lane the declaration replaces.
func singleDeclBatch(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) arrow.RecordBatch {
	t.Helper()
	return buildBatch(t, ir, conv, 2, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		if cc.PlainItemType == common.PlainItemTypeEntityId {
			return plainU64(1, 2)
		}
		switch role {
		case common.ColumnRoleValue:
			return listBin([]string{"a", "b"}, []string{"c"})
		case common.ColumnRoleLowCardVerbatim:
			return listBin([]string{"/x", "/y"}, []string{"/z"})
		case common.ColumnRoleLowCardVerbatimCardinality:
			return listU64([]uint64{1, 1}, []uint64{1})
		case common.ColumnRoleHighCardRef:
			return listU64([]uint64{7}, []uint64{})
		case common.ColumnRoleHighCardRefCardinality:
			return listU64([]uint64{1, 0}, []uint64{0})
		}
		return nil
	})
}

// TestSingleMembershipDeclarationIsRepresentation: identical content through
// the declared and the undeclared twin digests identically, record for
// record.
func TestSingleMembershipDeclarationIsRepresentation(t *testing.T) {
	tblD, irD, convD := singleDeclTable(t, true)
	tblF, irF, convF := singleDeclTable(t, false)
	d := digestsOf(t, &tblD, irD, singleDeclBatch(t, irD, convD), Options{})
	f := digestsOf(t, &tblF, irF, singleDeclBatch(t, irF, convF), Options{})
	require.Len(t, d, 2)
	require.Len(t, f, 2)
	require.Equal(t, hexs(f[0]), hexs(d[0]), "the declaration and its omitted cardinality lane are representation, never content")
	require.Equal(t, hexs(f[1]), hexs(d[1]), "an entity without ragged memberships digests identically too")
}

// tagFrameCountSink checks the stream contract the encoder relies on: every
// tag frame announces (BeginTags) exactly as many memberships as it emits —
// including the one a declared card-less channel contributes per attribute
// beside a carded channel's counted ones.
type tagFrameCountSink struct {
	*streamreadaccess.DebugSink
	announced  int
	added      int
	totalAdded int
	mismatches []string
}

func (inst *tagFrameCountSink) BeginTags(nTags int) {
	inst.announced = nTags
	inst.added = 0
	inst.DebugSink.BeginTags(nTags)
}
func (inst *tagFrameCountSink) EndTags() {
	if inst.added != inst.announced {
		inst.mismatches = append(inst.mismatches, fmt.Sprintf("announced %d, emitted %d", inst.announced, inst.added))
	}
	inst.DebugSink.EndTags()
}
func (inst *tagFrameCountSink) AddMembershipRef(lowCard bool, ref uint64) {
	inst.added++
	inst.totalAdded++
	inst.DebugSink.AddMembershipRef(lowCard, ref)
}
func (inst *tagFrameCountSink) AddMembershipVerbatim(lowCard bool, value string) {
	inst.added++
	inst.totalAdded++
	inst.DebugSink.AddMembershipVerbatim(lowCard, value)
}

// TestSingleMembershipTagFramesConsistent: a declared (card-less) channel
// beside a carded one — three attributes carrying one lv membership each,
// one of them an hr membership too — announces and emits matching counts.
func TestSingleMembershipTagFramesConsistent(t *testing.T) {
	tbl, ir, conv := singleDeclTable(t, true)
	rec := singleDeclBatch(t, ir, conv)
	d, err := streamreadaccess.NewDriver(&tbl, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)
	sink := &tagFrameCountSink{DebugSink: streamreadaccess.NewStructuredOutputRecorder()}
	require.NoError(t, d.DriveRecordBatch(sink, rec))
	require.Empty(t, sink.mismatches, "BeginTags must announce what the frame emits")
	require.Equal(t, 4, sink.totalAdded, "three lv memberships plus one hr membership")
}
