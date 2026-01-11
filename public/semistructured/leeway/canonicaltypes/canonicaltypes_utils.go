package canonicaltypes

import (
	"iter"

	"github.com/rs/zerolog/log"
)

func promoteScalarPrim(s PrimitiveAstNodeI, scalarModifier ScalarModifierE) (out PrimitiveAstNodeI) {
	if s.IsScalar() {
		out = s
		switch ct := out.(type) {
		case MachineNumericTypeAstNode:
			ct.ScalarModifier = scalarModifier
			out = ct
			break
		case StringAstNode:
			ct.ScalarModifier = scalarModifier
			out = ct
			break
		case TemporalTypeAstNode:
			ct.ScalarModifier = scalarModifier
			out = ct
			break
		default:
			log.Panic().Type("type", s).Msg("unable to promote unknown canonical type ast node")
		}
	} else {
		out = s
	}
	return
}
func DemoteToScalar(s PrimitiveAstNodeI) (out PrimitiveAstNodeI) {
	if !s.IsScalar() {
		out = s
		switch ct := s.(type) {
		case MachineNumericTypeAstNode:
			ct.ScalarModifier = ScalarModifierNone
			out = ct
			break
		case StringAstNode:
			ct.ScalarModifier = ScalarModifierNone
			out = ct
			break
		case TemporalTypeAstNode:
			ct.ScalarModifier = ScalarModifierNone
			out = ct
			break
		default:
			log.Panic().Type("type", s).Msg("unable to demote unknown canonical type ast node")
		}
	} else {
		out = s
	}
	return
}
func PromoteScalars(in AstNodeI, scalarModifier ScalarModifierE) (out AstNodeI, modified int, unmodified int) {
	p, isPrim := in.(PrimitiveAstNodeI)
	if isPrim {
		if p.IsScalar() {
			modified = 1
			out = promoteScalarPrim(p, scalarModifier)
		} else {
			unmodified = 1
			out = in
		}
		return
	}

	for c := range in.IterateMembers() {
		if c.IsScalar() {
			modified++
		} else {
			unmodified++
		}
	}
	if modified > 0 {
		members := make([]PrimitiveAstNodeI, 0, modified+unmodified)
		for c := range in.IterateMembers() {
			members = append(members, promoteScalarPrim(c, scalarModifier))
		}
		out = NewGroupAstNode(members)
	} else {
		out = in
	}
	return
}
func DemoteToScalars(in AstNodeI) (out AstNodeI, modified int, unmodified int) {
	p, isPrim := in.(PrimitiveAstNodeI)
	if isPrim {
		if !p.IsScalar() {
			modified = 1
			out = DemoteToScalar(p)
		} else {
			unmodified = 1
			out = in
		}
		return
	}

	for c := range in.IterateMembers() {
		if c.IsScalar() {
			unmodified++
		} else {
			modified++
		}
	}
	if modified > 0 {
		members := make([]PrimitiveAstNodeI, 0, modified+unmodified)
		for c := range in.IterateMembers() {
			members = append(members, DemoteToScalar(c))
		}
		out = NewGroupAstNode(members)
	} else {
		out = in
	}
	return
}
func MergeGroup(l AstNodeI, r AstNodeI) (g GroupAstNode) {
	m := make([]PrimitiveAstNodeI, 0, CountMembers(l)+CountMembers(r))
	for c := range l.IterateMembers() {
		m = append(m, c)
	}
	for c := range r.IterateMembers() {
		m = append(m, c)
	}

	g = GroupAstNode{
		members: m,
		str:     "",
	}
	return
}

func CountMembers(t AstNodeI) (r int) {
	if t.IsPrimitive() {
		r = 1
		return
	}
	for range t.IterateMembers() {
		r++
	}
	return
}
func CountMembersMulti(ts []AstNodeI) (r int) {
	for _, t := range ts {
		r += CountMembers(t)
	}
	return
}
func CountGroupTypeMembers(t AstNodeI) (r int) {
	if t.IsPrimitive() {
		return
	}
	for range t.IterateMembers() {
		r++
	}
	return
}
func CountGroupTypeMembersMulti(ts []AstNodeI) (r int) {
	for _, t := range ts {
		r += CountGroupTypeMembers(t)
	}
	return
}
func CountNonScalarsMulti(ts []AstNodeI) (r int) {
	for _, t := range ts {
		r += CountNonScalars(t)
	}
	return
}
func CountNonScalars(t AstNodeI) (r int) {
	for tp := range t.IterateMembers() {
		if !tp.IsScalar() {
			r++
		}
	}
	return
}
func IteratePrimitiveTypesMulti(ts []AstNodeI) iter.Seq2[int, PrimitiveAstNodeI] {
	return func(yield func(int, PrimitiveAstNodeI) bool) {
		for i, t := range ts {
			for c := range t.IterateMembers() {
				if !yield(i, c) {
					return
				}
			}
		}
	}
}
func IterateGroupIndexedByOccurrence(t AstNodeI, uniqTypeIndex int) iter.Seq2[int, PrimitiveAstNodeI] {
	return func(yield func(int, PrimitiveAstNodeI) bool) {
		if t.IsPrimitive() {
			yield(uniqTypeIndex, t.(PrimitiveAstNodeI))
			return
		}
		m1 := make(map[string]int, CountGroupTypeMembers(t))
		for u := range t.IterateMembers() {
			s := u.String()
			n := m1[s]
			m1[s] = n + 1
		}
		m2 := make(map[string]int, len(m1))
		for u := range t.IterateMembers() {
			s := u.String()
			nTotal := m1[s]
			if nTotal > 1 {
				nIdx := m2[s]
				if !yield(nIdx, u) {
					return
				}
				m2[s] = nIdx + 1
			} else {
				if !yield(uniqTypeIndex, u) {
					return
				}
			}
		}
	}
}
func CastSliceOfPrimitiveAstNodes(s []PrimitiveAstNodeI) (o []AstNodeI) {
	if s == nil {
		return
	}
	o = make([]AstNodeI, 0, len(s))
	for _, t := range s {
		o = append(o, t)
	}
	return
}
