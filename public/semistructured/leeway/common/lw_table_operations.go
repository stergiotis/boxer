package common

import (
	"bytes"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

type TableOperations struct {
	manip      *TableManipulator
	normalizer *TableNormalizer
	marshaller *TableMarshaller
	buf        *bytes.Buffer
}

func NewTableOperations() (inst *TableOperations, err error) {
	var manip *TableManipulator
	manip, err = NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	var marshaller *TableMarshaller
	marshaller, err = NewTableMarshaller()
	if err != nil {
		err = eh.Errorf("unable to create table marshaller: %w", err)
		return
	}
	inst = &TableOperations{
		manip:      manip,
		normalizer: NewTableNormalizer(naming.DefaultNamingStyle),
		marshaller: marshaller,
		buf:        nil,
	}
	return
}
func (inst *TableOperations) MergeTables(tbl1, tbl2 *TableDesc) (out *TableDesc, err error) {
	manip := inst.manip
	manip.Reset()
	err = manip.MergeTable(tbl1)
	if err != nil {
		err = eh.Errorf("unable to merge table in manipulator: %w", err)
		return
	}
	err = manip.MergeTable(tbl2)
	if err != nil {
		err = eh.Errorf("unable to merge table in manipulator: %w", err)
		return
	}
	*out, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to get table from manipulator: %w", err)
		return
	}
	return
}

type CriteriaOperationTypeE uint8

const (
	CriteriaTypeWhitelist CriteriaOperationTypeE = 0
	CriteriaTypeBlacklist CriteriaOperationTypeE = 1
)

type TableSubsetPredicateI interface {
	ShouldKeepTagged(sectionName string, columnName string, ct canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, aspectSet useaspects.AspectSet, membership MembershipSpecE) bool
	ShouldKeepPlain(sectionName string, columnName string, ct canonicalTypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, aspectSet useaspects.AspectSet, membership MembershipSpecE) bool
}

type TableSubsetSectionByNamePredicate struct {
	Type         CriteriaOperationTypeE
	SectionNames []string
}
type TableSubsetSectionByUseCriteriaPredicate struct {
	Type        CriteriaOperationTypeE
	UseCriteria useaspects.AspectSet
}
type TableSubsetCriteria struct {
	KeepSectionByName []string
}

func (inst *TableOperations) Subset(tbl *TableDesc, criteria TableSubsetCriteria) (out *TableDesc, err error) {
	return
}
func (inst *TableOperations) MustCompare(tbl1, tbl2 *TableDesc) (r int) {
	var err error
	r, err = inst.Compare(tbl1, tbl2)
	if err != nil {
		log.Panic().Err(err).Msg("unable to compare tables")
	}
	return
}
func (inst *TableOperations) Compare(tbl1, tbl2 *TableDesc) (r int, err error) {
	var tbl1C, tbl2C TableDesc
	tbl1C, err = inst.DeepCopy(tbl1)
	if err != nil {
		err = eh.Errorf("unable to copy tbl1: %w", err)
		return
	}
	tbl2C, err = inst.DeepCopy(tbl2)
	if err != nil {
		err = eh.Errorf("unable to copy tbl1: %w", err)
		return
	}
	normalizer := inst.normalizer
	_, _, _, err = normalizer.Normalize(&tbl1C)
	if err != nil {
		err = eh.Errorf("unable to normalize tbl1: %w", err)
		return
	}
	_, _, _, err = normalizer.Normalize(&tbl2C)
	if err != nil {
		err = eh.Errorf("unable to normalize tbl2: %w", err)
		return
	}
	buf := inst.buf
	if buf == nil {
		buf = bytes.NewBuffer(make([]byte, 0, 2*4096))
		inst.buf = buf
	}
	buf.Reset()
	marshaller := inst.marshaller
	err = marshaller.EncodeTableCbor(buf, &tbl1C)
	if err != nil {
		err = eh.Errorf("unable to marshall tbl1: %w", err)
		return
	}
	l1 := buf.Len()
	err = marshaller.EncodeTableCbor(buf, &tbl2C)
	if err != nil {
		err = eh.Errorf("unable to marshall tbl2: %w", err)
		return
	}
	b := buf.Bytes()
	r = bytes.Compare(b[0:l1], b[l1:])
	return
}
func (inst *TableOperations) DeepCopy(tbl *TableDesc) (out TableDesc, err error) {
	manip := inst.manip
	manip.Reset()
	err = manip.MergeTable(tbl)
	if err != nil {
		err = eh.Errorf("unable to merge table in manipulator: %w", err)
		return
	}
	out, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to get table from manipulator: %w", err)
		return
	}
	return
}
