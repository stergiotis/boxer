package registry

import (
	"iter"
	"math/bits"
	"strings"
)

const (
	MembershipValueNone       RegisteredValueFlagsE = 0b0000_0000
	MembershipValueFinal      RegisteredValueFlagsE = 0b0000_0001
	MembershipValueVirtual    RegisteredValueFlagsE = 0b0000_0010
	MembershipValueDeprecated RegisteredValueFlagsE = 0b0000_0100
)

var AllMembershipValues = []RegisteredValueFlagsE{
	MembershipValueNone,
	MembershipValueFinal,
	MembershipValueVirtual,
	MembershipValueDeprecated,
}

func (inst RegisteredValueFlagsE) Count() int {
	return bits.OnesCount8(uint8(inst))
}
func (inst RegisteredValueFlagsE) Iterate() iter.Seq[RegisteredValueFlagsE] {
	return func(yield func(e RegisteredValueFlagsE) bool) {
		for _, m := range AllMembershipValues {
			if (inst&m) != 0 && m != MembershipValueNone {
				if !yield(m) {
					return
				}
			}
		}
	}
}
func (inst RegisteredValueFlagsE) String() string {
	if inst == MembershipValueNone {
		return "none"
	}
	l := inst.Count()
	if l == 1 {
		switch inst {
		case MembershipValueVirtual:
			return "virtual"
		case MembershipValueFinal:
			return "final"
		case MembershipValueDeprecated:
			return "deprecated"
		default:
			break
		}
	}
	s := strings.Builder{}
	i := 0
	for m := range inst.Iterate() {
		if i > 0 {
			_, _ = s.WriteString(" | ")
		}
		_, _ = s.WriteString(m.String())
		i++
	}
	return s.String()
}
func (inst RegisteredValueFlagsE) HasVirtual() bool {
	return inst&MembershipValueVirtual == MembershipValueVirtual
}
func (inst RegisteredValueFlagsE) SetVirtual() RegisteredValueFlagsE {
	return inst | MembershipValueVirtual
}
func (inst RegisteredValueFlagsE) ClearVirtual() RegisteredValueFlagsE {
	return inst & ^MembershipValueVirtual
}
func (inst RegisteredValueFlagsE) HasFinal() bool {
	return inst&MembershipValueFinal == MembershipValueFinal
}
func (inst RegisteredValueFlagsE) SetFinal() RegisteredValueFlagsE {
	return inst | MembershipValueFinal
}
func (inst RegisteredValueFlagsE) ClearFinal() RegisteredValueFlagsE {
	return inst & ^MembershipValueFinal
}
func (inst RegisteredValueFlagsE) HasDeprecated() bool {
	return inst&MembershipValueDeprecated == MembershipValueDeprecated
}
func (inst RegisteredValueFlagsE) SetDeprecated() RegisteredValueFlagsE {
	return inst | MembershipValueDeprecated
}
func (inst RegisteredValueFlagsE) ClearDeprecated() RegisteredValueFlagsE {
	return inst & ^MembershipValueDeprecated
}
