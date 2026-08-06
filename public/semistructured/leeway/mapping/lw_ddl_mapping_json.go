package mapping

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	enchint "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// LoadJsonMapping declares the canonical leeway JSON mapping.
//
// # Encoding hints on the value columns
//
// The bool, float64 and int64 value columns formerly carried no hints, which is
// not the same as carrying a neutral one: a hint-less column emits no CODEC
// clause at all and silently inherits the server default (LZ4 on ClickHouse),
// while the support column beside it in the same section gets `T64, ZSTD(3)`
// from the membership machinery. The hints below close that gap.
//
// The int64 choice is measured, not reasoned, and the measurement contradicted
// the obvious argument. A leeway value lane is flattened across attributes, so
// consecutive elements are different fields — a microsecond timestamp beside an
// image height — which suggests delta encoding should be useless and a
// bit-plane transform like T64 should win. On the JSONBench Bluesky corpus at
// 10M (1.59 GiB of int64 lane) the opposite holds:
//
//	no codec (LZ4 default)   56.67 MiB   2.89x   <- was
//	T64, LZ4                 38.19 MiB   4.29x
//	T64, ZSTD(3)             29.49 MiB   5.56x
//	ZSTD(3)                  29.10 MiB   5.63x
//	Delta, ZSTD(3)           25.00 MiB   6.55x
//	DoubleDelta, ZSTD(3)     23.98 MiB   6.83x   <- is
//
// T64 is *worse* than plain ZSTD(3) here, and DoubleDelta is best — a 2.36x
// reduction on the lane. One corpus is not a proof, and a corpus whose integers
// are not dominated by a per-document timestamp could well invert this again;
// the hint is a default, and a caller with different data should measure.
//
// The float64 hint is **not** validated by that corpus: Bluesky is essentially
// float-free (53.66 KiB of lane against a 348 KiB empty-array baseline), and
// FPC(12), Gorilla and plain ZSTD(3) all landed on byte-identical sizes there,
// which only says the transform had nothing to work on. LightGeneralCompression
// is therefore the conservative choice — it fixes the missing-CODEC defect
// without asserting a float transform this repository has not measured.
func LoadJsonMapping(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "blake3hash", ctabb.Y).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("bool").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.B).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("undefined").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("null").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("string").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression,
			enchint.AspectInterRecordLowCardinality,
			enchint.AspectIntraRecordLowCardinality)
	manip.TaggedValueSection("float64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.F64).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("int64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.I64).
		AddColumnEncodingHints(enchint.AspectDoubleDeltaEncoding,
			enchint.AspectLightGeneralCompression)
}
func LoadJsonMappingLossless(manip common.TableManipulatorFluidI) {
	LoadJsonMapping(manip)
	manip.TaggedValueSection("emptyObject").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("emptyArray").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
}

func NewJsonMapping() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	LoadJsonMapping(manip)
	manip.SetTableName("JsonMapping")
	manip.SetTableComment("canonical leeway json mapping")
	return manip.BuildTableDesc()
}
