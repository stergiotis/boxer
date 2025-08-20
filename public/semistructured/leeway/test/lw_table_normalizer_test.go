package test

import (
	"bytes"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

func TestNewTableNormalizer(t *testing.T) {
	normalizer1 := common.NewTableNormalizer(common.NamingStyleLowerCamelCase)
	normalizer2 := common.NewTableNormalizer(common.NamingStyleSnakeCase)
	require.False(t, normalizer1.Equal(normalizer2))
	require.True(t, normalizer1.Equal(normalizer1))
	marshaller, err := common.NewTableMarshaller()
	require.NoError(t, err)

	var tbl1, tbl2 common.TableDesc
	var manip *common.TableManipulator
	ctp := canonicalTypes.NewParser()
	manip, err = common.NewTableManipulator()
	require.NoError(t, err)
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "my_id", ctp.MustParsePrimitiveTypeAst("s"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "my_id_two", ctp.MustParsePrimitiveTypeAst("u64"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeTransaction, "my_transaction", ctp.MustParsePrimitiveTypeAst("s"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("my_section", "col1", ctp.MustParsePrimitiveTypeAst("u32"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecE(0).AddLowCardVerbatim(), common.MustBeValidKey("coSectionKey1"), common.MustBeValidKey("streamingGroup1"))
	manip.MergeTaggedValueColumn("my_section", "col2", ctp.MustParsePrimitiveTypeAst("u8"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecE(0).AddLowCardVerbatim(), "", "")
	manip.MergeTaggedValueColumn("my_section_two", "col3", ctp.MustParsePrimitiveTypeAst("u16"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecE(0).AddLowCardVerbatim(), "", "")
	tbl1, err = manip.BuildTableDesc()
	require.NoError(t, err)
	manip.Reset()
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "myIdTwo", ctp.MustParsePrimitiveTypeAst("u64"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "myId", ctp.MustParsePrimitiveTypeAst("s"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeTransaction, "myTransaction", ctp.MustParsePrimitiveTypeAst("s"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("mySectionTwo", "col3", ctp.MustParsePrimitiveTypeAst("u16"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecE(0).AddLowCardVerbatim(), "", "")
	manip.MergeTaggedValueColumn("mySection", "col2", ctp.MustParsePrimitiveTypeAst("u8"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecE(0).AddLowCardVerbatim(), "", "")
	manip.MergeTaggedValueColumn("mySection", "col1", ctp.MustParsePrimitiveTypeAst("u32"), encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecE(0).AddLowCardVerbatim(), "", "")
	tbl2, err = manip.BuildTableDesc()
	require.NoError(t, err)

	_, _, _, err = normalizer1.Normalize(&tbl1)
	require.NoError(t, err)
	var nameChanged, plainChanged, taggedChanged bool
	nameChanged, plainChanged, taggedChanged, err = normalizer1.Normalize(&tbl1)
	require.NoError(t, err)
	require.False(t, nameChanged)
	require.False(t, plainChanged)
	require.False(t, taggedChanged)

	_, _, _, err = normalizer2.Normalize(&tbl2)
	require.NoError(t, err)

	buf1 := bytes.NewBuffer(make([]byte, 0, 4096))
	buf2 := bytes.NewBuffer(make([]byte, 0, 4096))
	err = marshaller.EncodeTableCbor(buf1, &tbl1)
	require.NoError(t, err)
	err = marshaller.EncodeTableCbor(buf2, &tbl2)
	require.NoError(t, err)
	require.NotEqualValues(t, buf1.Bytes(), buf2.Bytes())

	nameChanged, plainChanged, taggedChanged, err = normalizer1.Normalize(&tbl2)
	require.NoError(t, err)
	require.True(t, nameChanged || plainChanged || taggedChanged)
	buf2.Reset()
	err = marshaller.EncodeTableCbor(buf2, &tbl2)
	require.NoError(t, err)
	require.EqualValues(t, tbl1, tbl2)
	require.EqualValues(t, string(buf1.Bytes()), string(buf2.Bytes()))
}
