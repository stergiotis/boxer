package streamenc

// The table descriptions of canonwire/example, copied from
// canonwire/canonwire_generator_test.go because they are unexported there
// (as readaccess's were copied into canonwire). Keep them in step: the
// generated classes under example/ were emitted from these.

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

func idHints() (hints encodingaspects.AspectSet, err error) {
	hints, err = encodingaspects.EncodeAspects(encodingaspects.AspectDeltaEncoding, encodingaspects.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
	}
	return
}

func sampleTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		return
	}
	var hintsId, hintsProc encodingaspects.AspectSet
	if hintsId, err = idHints(); err != nil {
		return
	}
	hintsProc, err = encodingaspects.EncodeAspects(encodingaspects.AspectLightGeneralCompression)
	if err != nil {
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z32, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "proc", ctabb.Z32h, hintsProc, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("geo").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("lat", ctabb.F32)
		sec.TaggedValueColumn("lng", ctabb.F32)
		sec.TaggedValueColumn("h3_res1", ctabb.U64)
		sec.TaggedValueColumn("h3_res2", ctabb.U64)
	}
	{
		sec := manip.TaggedValueSection("text").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("text", ctabb.S)
		sec.TaggedValueColumn("word_length", ctabb.U32h)
		sec.TaggedValueColumn("words", ctabb.Sh)
	}
	return manip.BuildTableDesc()
}

func networkSampleTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		return
	}
	var hintsId encodingaspects.AspectSet
	if hintsId, err = idHints(); err != nil {
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z32, hintsId, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("net").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("ipv4", ctabb.V)
		sec.TaggedValueColumn("ipv6", ctabb.W)
		sec.TaggedValueColumn("ipv4_cidr", ctabb.Vc)
		sec.TaggedValueColumn("ipv6_cidr", ctabb.Wc)
	}
	return manip.BuildTableDesc()
}

func channelTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		return
	}
	var hintsId encodingaspects.AspectSet
	if hintsId, err = idHints(); err != nil {
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("mref").
			AddSectionMembership(common.MembershipSpecMixedLowCardRefHighCardParameters)
		sec.TaggedValueColumn("a", ctabb.U64)
	}
	{
		sec := manip.TaggedValueSection("href").
			AddSectionMembership(common.MembershipSpecHighCardRef)
		sec.TaggedValueColumn("b", ctabb.I64)
	}
	{
		sec := manip.TaggedValueSection("hverb").
			AddSectionMembership(common.MembershipSpecHighCardVerbatim)
		sec.TaggedValueColumn("c", ctabb.S)
	}
	{
		sec := manip.TaggedValueSection("lparam").
			AddSectionMembership(common.MembershipSpecLowCardRefParametrized)
		sec.TaggedValueColumn("d", ctabb.Y)
	}
	{
		sec := manip.TaggedValueSection("hparam").
			AddSectionMembership(common.MembershipSpecHighCardRefParametrized)
		sec.TaggedValueColumn("e", ctabb.F64)
	}
	return manip.BuildTableDesc()
}

func fixedTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		return
	}
	var hintsId encodingaspects.AspectSet
	if hintsId, err = idHints(); err != nil {
		return
	}
	sx4 := canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8, WidthModifier: canonicaltypes.WidthModifierFixed, Width: 4}
	sx3h := canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8, WidthModifier: canonicaltypes.WidthModifierFixed, Width: 3, ScalarModifier: canonicaltypes.ScalarModifierHomogenousArray}
	yx4 := canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes, WidthModifier: canonicaltypes.WidthModifierFixed, Width: 4}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("code").
			AddSectionMembership(common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("c", sx4)
	}
	{
		sec := manip.TaggedValueSection("codes").
			AddSectionMembership(common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("cs", sx3h)
	}
	{
		sec := manip.TaggedValueSection("hash").
			AddSectionMembership(common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("h", yx4)
	}
	return manip.BuildTableDesc()
}

func jsonTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		return
	}
	mapping.LoadJsonMappingLossless(manip)
	return manip.BuildTableDesc()
}

func placeTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		return
	}
	var hintsId encodingaspects.AspectSet
	if hintsId, err = idHints(); err != nil {
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("geo").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			SectionCoSectionGroup("place")
		sec.TaggedValueColumn("lat", ctabb.F32)
		sec.TaggedValueColumn("lng", ctabb.F32)
	}
	{
		sec := manip.TaggedValueSection("h3").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			SectionCoSectionGroup("place")
		sec.TaggedValueColumn("cell", ctabb.U64)
	}
	{
		sec := manip.TaggedValueSection("tags").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim)
		sec.TaggedValueColumn("tag", ctabb.Sh)
		sec.TaggedValueColumn("tag_id", ctabb.U64m)
	}
	return manip.BuildTableDesc()
}
