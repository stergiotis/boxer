package canonicaltypes

import (
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/exp/constraints"
)

func formatGoRune[R ~rune](val R) string {
	if val == 0 {
		return "0"
	} else {
		return "'" + string(val) + "'"
	}
}

func addIfNonzero[R ~rune](s *strings.Builder, r R) {
	if r != 0 {
		s.WriteRune(rune(r))
	}
}
func addNumber[N constraints.Unsigned](s *strings.Builder, num N) {
	ns := strconv.FormatUint(uint64(num), 10)
	s.WriteString(ns)
}
func addIfNonzeroNumber2[R ~rune, N constraints.Unsigned](s *strings.Builder, r R, num N) {
	if r != 0 {
		s.WriteRune(rune(r))
		ns := strconv.FormatUint(uint64(num), 10)
		s.WriteString(ns)
	}
}

func (inst StringAstNode) IsStringNode() bool {
	return true
}
func (inst StringAstNode) IsValid() bool {
	return (inst.WidthModifier == WidthModifierNone && inst.Width == 0) ||
		(inst.WidthModifier == WidthModifierFixed && inst.Width > 0)
}

func (inst StringAstNode) IsTemporalNode() bool {
	return false
}

func (inst StringAstNode) IsMachineNumericNode() bool {
	return false
}
func (inst StringAstNode) IsPrimitive() bool {
	return true
}
func (inst StringAstNode) IsSignature() bool {
	return false
}

func (inst StringAstNode) String() string {
	s := &strings.Builder{}
	addIfNonzero(s, inst.BaseType)
	addIfNonzeroNumber2(s, inst.WidthModifier, inst.Width)
	addIfNonzero(s, inst.ScalarModifier)
	return s.String()
}
func (inst StringAstNode) IsScalar() bool {
	return inst.ScalarModifier == ScalarModifierNone
}
func (inst StringAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] {
	return func(yield func(PrimitiveAstNodeI) bool) {
		yield(inst)
	}
}
func (inst StringAstNode) MarshalCBOR() (data []byte, err error) {
	return cbor.Marshal(inst.String())
}

func (inst StringAstNode) GenerateGoCode(w io.Writer) (err error) {
	_, err = fmt.Fprintf(w, "StringAstNode{BaseType: %s,WidthModifier: %s, Width: %d, ScalarModifier: %s}",
		formatGoRune(inst.BaseType), formatGoRune(inst.WidthModifier), inst.Width, formatGoRune(inst.ScalarModifier))
	return
}

func (inst TemporalTypeAstNode) IsStringNode() bool {
	return false
}

func (inst TemporalTypeAstNode) IsTemporalNode() bool {
	return true
}

func (inst TemporalTypeAstNode) IsMachineNumericNode() bool {
	return false
}
func (inst TemporalTypeAstNode) IsPrimitive() bool {
	return true
}

func (inst TemporalTypeAstNode) String() string {
	s := &strings.Builder{}
	addIfNonzero(s, inst.BaseType)
	addNumber(s, inst.Width)
	addIfNonzero(s, inst.ScalarModifier)
	return s.String()
}
func (inst TemporalTypeAstNode) GenerateGoCode(w io.Writer) (err error) {
	_, err = fmt.Fprintf(w, "TemporalTypeAstNode{BaseType: %s, Width: %d, ScalarModifier: %s}",
		formatGoRune(inst.BaseType), inst.Width, formatGoRune(inst.ScalarModifier))
	return
}
func (inst TemporalTypeAstNode) IsValid() bool {
	return true
}
func (inst TemporalTypeAstNode) IsScalar() bool {
	return inst.ScalarModifier == ScalarModifierNone
}
func (inst TemporalTypeAstNode) IsSignature() bool {
	return false
}
func (inst TemporalTypeAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] {
	return func(yield func(PrimitiveAstNodeI) bool) {
		yield(inst)
	}
}
func (inst TemporalTypeAstNode) MarshalCBOR() (data []byte, err error) {
	return cbor.Marshal(inst.String())
}

func (inst MachineNumericTypeAstNode) IsStringNode() bool {
	return false
}

func (inst MachineNumericTypeAstNode) IsTemporalNode() bool {
	return false
}

func (inst MachineNumericTypeAstNode) IsMachineNumericNode() bool {
	return true
}
func (inst MachineNumericTypeAstNode) IsPrimitive() bool {
	return true
}

func (inst MachineNumericTypeAstNode) IsValid() bool {
	return true
}
func (inst MachineNumericTypeAstNode) IsSignature() bool {
	return false
}
func (inst MachineNumericTypeAstNode) String() string {
	s := &strings.Builder{}
	addIfNonzero(s, inst.BaseType)
	addNumber(s, inst.Width)
	addIfNonzero(s, inst.ByteOrderModifier)
	addIfNonzero(s, inst.ScalarModifier)
	return s.String()
}
func (inst MachineNumericTypeAstNode) GenerateGoCode(w io.Writer) (err error) {
	_, err = fmt.Fprintf(w, "MachineNumericTypeAstNode{BaseType: %s, Width: %d, ByteOrderModifier: %s, ScalarModifier: %s}",
		formatGoRune(inst.BaseType), inst.Width, formatGoRune(inst.ByteOrderModifier), formatGoRune(inst.ScalarModifier))
	return
}
func (inst MachineNumericTypeAstNode) IsScalar() bool {
	return inst.ScalarModifier == ScalarModifierNone
}
func (inst MachineNumericTypeAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] {
	return func(yield func(PrimitiveAstNodeI) bool) {
		yield(inst)
	}
}
func (inst MachineNumericTypeAstNode) MarshalCBOR() (data []byte, err error) {
	return cbor.Marshal(inst.String())
}

func (inst GroupAstNode) IsStringNode() bool {
	return false
}

func (inst GroupAstNode) IsTemporalNode() bool {
	return false
}

func (inst GroupAstNode) IsMachineNumericNode() bool {
	return false
}
func (inst GroupAstNode) IsPrimitive() bool {
	return false
}
func (inst GroupAstNode) IsSignature() bool {
	return false
}
func (inst GroupAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] {
	return func(yield func(PrimitiveAstNodeI) bool) {
		ms := inst.members
		if len(ms) == 0 {
			return
		}
		for _, m := range ms {
			if !yield(m) {
				return
			}
		}
	}
}
func (inst GroupAstNode) IsValid() bool {
	valid := true
	for _, m := range inst.members {
		valid = valid && m.IsValid()
	}
	return valid
}

func (inst GroupAstNode) String() string {
	l := len(inst.members)
	if l == 0 {
		return "<invalid:empty>"
	}

	if inst.str == "" {
		s := strings.Builder{}
		s.Grow(l * 6) // estimate size
		for i, m := range inst.members {
			s.WriteString(m.String())
			if i < l-1 {
				s.WriteString(GroupSeparator)
			}
		}
		// cache, note that ASTs are "immutable" (as far as easily possible in go *sigh*)
		inst.str = s.String()
	}
	return inst.str
}
func (inst GroupAstNode) MarshalCBOR() (data []byte, err error) {
	return cbor.Marshal(inst.String())
}

func (inst Width) String() string {
	return strconv.FormatUint(uint64(inst), 10)
}

func (inst SignatureAstNode) IsSignature() bool {
	return true
}

func (inst SignatureAstNode) IsPrimitive() bool {
	return false
}

func (inst SignatureAstNode) IsValid() bool {
	v := len(inst.members) > 0
	for _, m := range inst.members {
		v = v && m.IsValid()
	}
	return v
}

func (inst SignatureAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] {
	return func(yield func(PrimitiveAstNodeI) bool) {
		for _, m := range inst.members {
			for mm := range m.IterateMembers() {
				if !yield(mm) {
					return
				}
			}
		}
	}
}
func (inst SignatureAstNode) IterateGroupMembers() iter.Seq[AstNodeI] {
	return func(yield func(i AstNodeI) bool) {
		for _, m := range inst.members {
			for mm := range m.IterateMembers() {
				if !yield(mm) {
					return
				}
			}
		}
	}
}

func (inst SignatureAstNode) String() string {
	l := len(inst.members)
	if l == 0 {
		return "<invalid:empty>"
	}

	if inst.str == "" {
		s := strings.Builder{}
		s.Grow(l * 6) // estimate size
		for i, m := range inst.members {
			s.WriteString(m.String())
			if i < l-1 {
				s.WriteString(SignatureSeparator)
			}
		}
		// cache, note that ASTs are "immutable" (as far as easily possible in go *sigh*)
		inst.str = s.String()
	}
	return inst.str
}
func (inst SignatureAstNode) MarshalCBOR() (data []byte, err error) {
	return cbor.Marshal(inst.String())
}

func NewGroupAstNode(members []PrimitiveAstNodeI) GroupAstNode {
	return GroupAstNode{
		members: members,
		str:     "",
	}
}
