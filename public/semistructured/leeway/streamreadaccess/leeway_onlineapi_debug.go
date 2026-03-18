package streamreadaccess

import (
	"bytes"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

type DebugSink struct {
	s *bytes.Buffer
}

func (inst *DebugSink) BeginPlainValue() {
	inst.s.WriteString("\t\t\t\tBeginPlainValue()\n")
}

func (inst *DebugSink) EndPlainValue() (err error) {
	inst.s.WriteString("\t\t\t\tEndTaggedValue()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginTaggedSections() {
	inst.s.WriteString("\t\tBeginTaggedSections()\n")
}

func (inst *DebugSink) EndTaggedSections() (err error) {
	inst.s.WriteString("\t\tEndTaggedSections()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	fmt.Fprintf(inst.s, "\t\tBeginPlainSection(itemType=%s,valueNames=%q,valueCanonicalTypes=%q,nAttrs=%d)\n", itemType, valueNames, valueCanonicalTypes, nAttrs)
	inst.check()
}

func (inst *DebugSink) EndPlainSection() (err error) {
	inst.s.WriteString("\t\tEndPlainSection()\n")
	inst.check()
	return
}

func NewStructuredOutputRecorder() *DebugSink {
	return &DebugSink{
		s: bytes.NewBuffer(nil),
	}
}
func (inst *DebugSink) check() {
	if inst.s.Len() > 1_000_000 {
		fmt.Print(inst.s.String())
		log.Panic().Msg("max buffer size exceeded")
	}
}
func (inst *DebugSink) ensureNewline() {
	s := inst.s.String()
	if s[len(s)-1] != '\n' {
		inst.s.WriteRune('\n')
	}
}
func (inst *DebugSink) Reset() {
	inst.s.Reset()
}
func (inst *DebugSink) Bytes() []byte {
	return inst.s.Bytes()
}
func (inst *DebugSink) String() string {
	return inst.s.String()
}

func (inst *DebugSink) BeginBatch() {
	inst.s.WriteString("BeginBatch()\n")
	inst.check()
}

func (inst *DebugSink) EndBatch() (err error) {
	inst.s.WriteString("EndBatch()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginEntity() {
	inst.s.WriteString("\tBeginEntity()\n")
	inst.check()
}

func (inst *DebugSink) EndEntity() (err error) {
	inst.s.WriteString("\tEndEntity()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginCoSectionGroup(name naming.Key) {
	fmt.Fprintf(inst.s, "\t\t\tBeginCoSectionGroup(%q)\n", name)
	inst.check()
}

func (inst *DebugSink) EndCoSectionGroup() (err error) {
	inst.s.WriteString("\t\t\tEndCoSectionGroup()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	fmt.Fprintf(inst.s, "\t\t\t\tBeginSection(name=%q,valueNames=%q,valueCanonicalTypes=%q,nAttrs=%d)\n", name, valueNames, valueCanonicalTypes, nAttrs)
	inst.check()
}

func (inst *DebugSink) EndSection() (err error) {
	inst.s.WriteString("\t\t\t\tEndSection()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginTaggedValue() {
	inst.s.WriteString("\t\t\t\t\tBeginTaggedValue()\n")
	inst.check()
}

func (inst *DebugSink) EndTaggedValue() (err error) {
	inst.s.WriteString("\t\t\t\t\tEndTaggedValue()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginColumn(colAddr PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\tBeginColumn(colAddr.FullColumnName=%q,name=%q,canonicalType=%q)\n", colAddr.FullColumnName, name, canonicalType)
	inst.check()
}

func (inst *DebugSink) EndColumn() {
	inst.s.WriteString("\t\t\t\t\t\tEndColumn()\n")
	inst.check()
}

func (inst *DebugSink) BeginScalarValue() {
	inst.s.WriteString("\t\t\t\t\t\t\tBeginScalarValue()\n")
	inst.check()
}

func (inst *DebugSink) EndScalarValue() (err error) {
	inst.ensureNewline()
	inst.s.WriteString("\t\t\t\t\t\t\tEndScalarValue()\n")
	inst.check()
	return
}

func (inst *DebugSink) BeginHomogenousArrayValue(card int) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\tBeginHomogenousArrayValue(card=%d)\n", card)
	inst.check()
}

func (inst *DebugSink) EndHomogenousArrayValue() {
	inst.s.WriteString("\t\t\t\t\t\t\tEndHomogenousArrayValue()\n")
	inst.check()
}

func (inst *DebugSink) BeginSetValue(card int) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\tBeginSetValue(card=%d)\n", card)
	inst.check()
}

func (inst *DebugSink) EndSetValue() {
	inst.s.WriteString("\t\t\t\t\t\t\tEndSetValue()\n")
	inst.check()
}

func (inst *DebugSink) BeginValueItem(index int) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\tBeginValueItem(index=%d)\n", index)
	inst.check()
}

func (inst *DebugSink) EndValueItem() {
	inst.ensureNewline()
	inst.s.WriteString("\t\t\t\t\t\t\tEndValueItem()\n")
	inst.check()
}

func (inst *DebugSink) Write(p []byte) (n int, err error) {
	inst.check()
	return inst.s.Write(p)
}

func (inst *DebugSink) WriteString(s string) (n int, err error) {
	inst.check()
	return inst.s.WriteString(s)
}

func (inst *DebugSink) BeginTags(nTags int) {
	inst.s.WriteString("\t\t\t\t\t\t\t\tBeginTags()\n")
	inst.check()
}

func (inst *DebugSink) EndTags() {
	inst.s.WriteString("\t\t\t\t\t\t\t\tEndTags()\n")
	inst.check()
}

func (inst *DebugSink) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\t\t\tAddMembershipRef(lowCard=%v,ref=0x%x,humanReadableRef=%q)\n", lowCard, ref, humanReadableRef)
	inst.check()
}

func (inst *DebugSink) AddMembershipVerbatim(lowCard bool, value string, humanReadableVerbatim string) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\t\t\tAddMembershipVerbatim(lowCard=%v,value=%q,humanReadableVerbatim=%q)\n", lowCard, value, humanReadableVerbatim)
	inst.check()
}

func (inst *DebugSink) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\t\t\tAddMembershipRefParametrized(lowCard=%v,ref=0x%x,humanReadableRef=%q,params=%q,humanReadableParams=%q)\n", lowCard, ref, humanReadableRef, params, humanReadableParams)
	inst.check()
}

func (inst *DebugSink) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\t\t\tAddMembershipMixedLowCardRefHighCardParam(ref=0x%x,humanReadableRef=%q,params=%q,humanReadableParams=%q)\n", ref, humanReadableRef, params, humanReadableParams)
	inst.check()
}

func (inst *DebugSink) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	fmt.Fprintf(inst.s, "\t\t\t\t\t\t\t\t\tAddMembershipMixedLowCardVerbatimHighCardParam(verbatim=%q,humanReadableVerbatim=%q,params=%q,humanReadableParams=%q)\n", verbatim, humanReadableVerbatim, params, humanReadableParams)
	inst.check()
}

var _ OnlineApiSinkI = (*DebugSink)(nil)
