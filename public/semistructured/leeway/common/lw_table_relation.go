package common

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// SubsetMismatchKindE names the reason one element of a candidate subset is not
// satisfied by the superset. It is the machine-readable half of SubsetMismatch;
// a consumer that only renders the mismatch can use the Stringer instead.
type SubsetMismatchKindE uint8

const (
	// SubsetMismatchMissingSection: the tagged section is absent entirely.
	SubsetMismatchMissingSection SubsetMismatchKindE = iota
	// SubsetMismatchMissingColumn: the section exists (or the column is a
	// plain/backbone one) but the named column is absent.
	SubsetMismatchMissingColumn
	// SubsetMismatchType: the column exists but carries a different canonical
	// type. Matching is exact — a u32 column does not satisfy a u64 requirement
	// even though every value would fit.
	SubsetMismatchType
	// SubsetMismatchValueSemantics: the column exists with the right type, but
	// the superset's value semantics do not cover the subset's.
	SubsetMismatchValueSemantics
	// SubsetMismatchEncodingHints: as SubsetMismatchValueSemantics, for the
	// column's encoding hints.
	SubsetMismatchEncodingHints
	// SubsetMismatchUseAspects: the section exists, but the superset's section
	// use-aspects do not cover the subset's.
	SubsetMismatchUseAspects
	// SubsetMismatchMembership: the section exists, but the superset's
	// membership spec does not carry every membership the subset declares.
	SubsetMismatchMembership
	// SubsetMismatchGroup: the subset pins a co-section / streaming group that
	// the superset assigns differently. An unset group in the subset pins
	// nothing and never yields this mismatch.
	SubsetMismatchGroup
)

var _ fmt.Stringer = SubsetMismatchKindE(0)

func (inst SubsetMismatchKindE) String() string {
	switch inst {
	case SubsetMismatchMissingSection:
		return "missing-section"
	case SubsetMismatchMissingColumn:
		return "missing-column"
	case SubsetMismatchType:
		return "type"
	case SubsetMismatchValueSemantics:
		return "value-semantics"
	case SubsetMismatchEncodingHints:
		return "encoding-hints"
	case SubsetMismatchUseAspects:
		return "use-aspects"
	case SubsetMismatchMembership:
		return "membership"
	case SubsetMismatchGroup:
		return "group"
	}
	return "invalid"
}

// SubsetMismatch is one reason a candidate subset is not contained in a
// superset. Section is empty and PlainItemType is set for a plain/backbone
// column; Section is set and PlainItemType is PlainItemTypeNone for a tagged
// one. Want carries what the subset asked for and Got what the superset offers
// — both empty when the element is simply absent.
type SubsetMismatch struct {
	Kind          SubsetMismatchKindE
	Section       naming.StylableName
	PlainItemType PlainItemTypeE
	Column        naming.StylableName
	Want          string
	Got           string
}

var _ fmt.Stringer = SubsetMismatch{}

func (inst SubsetMismatch) String() (s string) {
	where := string(inst.Column)
	if inst.Section != "" {
		where = string(inst.Section) + "." + where
	} else if inst.PlainItemType != PlainItemTypeNone {
		where = inst.PlainItemType.String() + "." + where
	}
	if inst.Kind == SubsetMismatchMissingSection {
		where = string(inst.Section)
	}
	s = inst.Kind.String() + " at " + where
	if inst.Want != "" || inst.Got != "" {
		s += ": want " + quoteEmpty(inst.Want) + ", got " + quoteEmpty(inst.Got)
	}
	return
}

func quoteEmpty(s string) string {
	if s == "" {
		return "<unset>"
	}
	return s
}

// SubsetReport is the outcome of TableOperations.IsSubset: whether containment
// holds and, when it does not, every element that failed. The mismatches are
// exhaustive rather than fail-fast so a consumer can tell a user everything a
// table lacks in one pass instead of one defect per round trip.
//
// The names in a mismatch are normalized (naming.DefaultNamingStyle), not as
// the caller authored them: containment is decided on normalized tables, so a
// section authored "geoPoint" is reported as "geo-point". Rendering a mismatch
// back to the author's styling is naming.ConvertNameStyle's job.
type SubsetReport struct {
	IsSubset   bool
	Mismatches []SubsetMismatch
}

var _ fmt.Stringer = SubsetReport{}

func (inst SubsetReport) String() (s string) {
	if inst.IsSubset {
		return "subset"
	}
	s = "not a subset"
	for i, m := range inst.Mismatches {
		if i == 0 {
			s += ": "
		} else {
			s += "; "
		}
		s += m.String()
	}
	return
}

// TableRelationE is the containment relation between two table descriptions.
type TableRelationE uint8

const (
	// TableRelationDisjoint: the tables name no column in common.
	TableRelationDisjoint TableRelationE = iota
	// TableRelationOverlap: they share at least one column, yet each declares
	// an element the other does not satisfy.
	TableRelationOverlap
	// TableRelationSubset: every element of the first is satisfied by the
	// second, which declares at least one the first does not.
	TableRelationSubset
	// TableRelationSuperset: the mirror of TableRelationSubset.
	TableRelationSuperset
	// TableRelationEqual: each contains the other.
	TableRelationEqual
)

var _ fmt.Stringer = TableRelationE(0)

func (inst TableRelationE) String() string {
	switch inst {
	case TableRelationDisjoint:
		return "disjoint"
	case TableRelationOverlap:
		return "overlap"
	case TableRelationSubset:
		return "subset"
	case TableRelationSuperset:
		return "superset"
	case TableRelationEqual:
		return "equal"
	}
	return "invalid"
}

// IsSubset reports whether every schema element sub declares is satisfied by
// super (sub ⊆ super), and when it is not, why. It is the containment test a
// consumer written against sub needs before it can read a table shaped like
// super: sub is the minimal shape the consumer requires, super the table on
// offer.
//
// Both arguments are deep-copied and normalized before the walk, so naming
// style and column order never decide the answer — a section authored as
// "geoPoint" matches one discovered as "geo-point". Normalization validates,
// so an invalid table is an error rather than a false negative.
//
// The element rules, all of which fall out of "sub states a requirement, super
// must meet it":
//
//   - Plain columns match on (item type, name), tagged columns on (section
//     name, column name) — per the leeway naming contract a tagged column name
//     is unique only within its section, so the section is part of the key.
//   - Canonical types must be equal. Matching is deliberately exact: widening
//     is a policy an individual consumer may or may not tolerate and cannot be
//     decided here.
//   - Aspect sets (use, value semantics, encoding hints) and the membership
//     spec must be covered: every aspect sub asks for must be present in super,
//     which may carry more.
//   - Co-section and streaming groups are compared only when sub pins them. An
//     unset group asks for nothing.
//   - The dictionary entry (table name, comment) is ignored: a required shape
//     does not name the table that satisfies it.
//
// The relation is reflexive and transitive; it is not antisymmetric on tables
// differing only in an unset-versus-set group, which pin-when-set makes
// deliberately asymmetric.
//
// Not safe for concurrent use: like Compare, it borrows the receiver's
// manipulator and normalizer.
func (inst *TableOperations) IsSubset(sub *TableDesc, super *TableDesc) (report SubsetReport, err error) {
	var subC, superC TableDesc
	subC, err = inst.normalizedCopy(sub)
	if err != nil {
		err = eh.Errorf("unable to prepare sub table: %w", err)
		return
	}
	superC, err = inst.normalizedCopy(super)
	if err != nil {
		err = eh.Errorf("unable to prepare super table: %w", err)
		return
	}
	report = subsetOf(&subC, &superC)
	return
}

// Relate classifies how two tables contain one another — the four-way answer
// (equal, subset, superset, overlap, disjoint) built from IsSubset in both
// directions. Overlap and disjoint are separated on whether the tables name any
// column in common, so two tables that each declare columns the other lacks are
// disjoint only when they share nothing at all.
//
// Not safe for concurrent use, see IsSubset.
func (inst *TableOperations) Relate(tbl1 *TableDesc, tbl2 *TableDesc) (rel TableRelationE, err error) {
	var c1, c2 TableDesc
	c1, err = inst.normalizedCopy(tbl1)
	if err != nil {
		err = eh.Errorf("unable to prepare tbl1: %w", err)
		return
	}
	c2, err = inst.normalizedCopy(tbl2)
	if err != nil {
		err = eh.Errorf("unable to prepare tbl2: %w", err)
		return
	}
	fwd := subsetOf(&c1, &c2)
	rev := subsetOf(&c2, &c1)
	switch {
	case fwd.IsSubset && rev.IsSubset:
		rel = TableRelationEqual
	case fwd.IsSubset:
		rel = TableRelationSubset
	case rev.IsSubset:
		rel = TableRelationSuperset
	case sharesColumn(&c1, &c2):
		rel = TableRelationOverlap
	default:
		rel = TableRelationDisjoint
	}
	return
}

// normalizedCopy deep-copies tbl and normalizes the copy, leaving the caller's
// table untouched. It is the preamble Compare, IsSubset and Relate share: every
// structural comparison in this file is defined on normalized tables.
func (inst *TableOperations) normalizedCopy(tbl *TableDesc) (out TableDesc, err error) {
	out, err = inst.DeepCopy(tbl)
	if err != nil {
		err = eh.Errorf("unable to copy table: %w", err)
		return
	}
	_, _, _, err = inst.normalizer.Normalize(&out)
	if err != nil {
		err = eh.Errorf("unable to normalize table: %w", err)
		return
	}
	return
}

type plainColumnKey struct {
	itemType PlainItemTypeE
	name     naming.StylableName
}

// subsetOf walks sub against super and collects every unsatisfied element. Both
// tables must already be normalized.
func subsetOf(sub *TableDesc, super *TableDesc) (report SubsetReport) {
	ms := make([]SubsetMismatch, 0, 8)
	ms = appendPlainMismatches(ms, sub, super)
	ms = appendTaggedMismatches(ms, sub, super)
	if sub.OpaqueStreamingGroup != "" && sub.OpaqueStreamingGroup != super.OpaqueStreamingGroup {
		ms = append(ms, SubsetMismatch{
			Kind: SubsetMismatchGroup,
			Want: string(sub.OpaqueStreamingGroup),
			Got:  string(super.OpaqueStreamingGroup),
		})
	}
	report = SubsetReport{
		IsSubset:   len(ms) == 0,
		Mismatches: ms,
	}
	return
}

func appendPlainMismatches(ms []SubsetMismatch, sub *TableDesc, super *TableDesc) (out []SubsetMismatch) {
	out = ms
	idx := make(map[plainColumnKey]int, len(super.PlainValuesNames))
	for i, name := range super.PlainValuesNames {
		idx[plainColumnKey{itemType: super.PlainValuesItemTypes[i], name: name}] = i
	}
	for i, name := range sub.PlainValuesNames {
		itemType := sub.PlainValuesItemTypes[i]
		j, has := idx[plainColumnKey{itemType: itemType, name: name}]
		if !has {
			out = append(out, SubsetMismatch{
				Kind:          SubsetMismatchMissingColumn,
				PlainItemType: itemType,
				Column:        name,
			})
			continue
		}
		out = appendColumnMismatches(out, columnRef{
			plainItemType: itemType,
			column:        name,
		}, columnFacts{
			ct:             sub.PlainValuesTypes[i],
			encodingHints:  sub.PlainValuesEncodingHints[i],
			valueSemantics: sub.PlainValuesValueSemantics[i],
		}, columnFacts{
			ct:             super.PlainValuesTypes[j],
			encodingHints:  super.PlainValuesEncodingHints[j],
			valueSemantics: super.PlainValuesValueSemantics[j],
		})
	}
	return
}

func appendTaggedMismatches(ms []SubsetMismatch, sub *TableDesc, super *TableDesc) (out []SubsetMismatch) {
	out = ms
	idx := make(map[naming.StylableName]int, len(super.TaggedValuesSections))
	for i, sec := range super.TaggedValuesSections {
		idx[sec.Name] = i
	}
	for _, subSec := range sub.TaggedValuesSections {
		j, has := idx[subSec.Name]
		if !has {
			out = append(out, SubsetMismatch{
				Kind:    SubsetMismatchMissingSection,
				Section: subSec.Name,
			})
			continue
		}
		out = appendSectionMismatches(out, subSec, super.TaggedValuesSections[j])
	}
	return
}

func appendSectionMismatches(ms []SubsetMismatch, subSec TaggedValuesSection, superSec TaggedValuesSection) (out []SubsetMismatch) {
	out = ms
	if !useAspectsCovered(subSec.UseAspects, superSec.UseAspects) {
		out = append(out, SubsetMismatch{
			Kind:    SubsetMismatchUseAspects,
			Section: subSec.Name,
			Want:    string(subSec.UseAspects),
			Got:     string(superSec.UseAspects),
		})
	}
	if subSec.MembershipSpec&^superSec.MembershipSpec != 0 {
		out = append(out, SubsetMismatch{
			Kind:    SubsetMismatchMembership,
			Section: subSec.Name,
			Want:    subSec.MembershipSpec.String(),
			Got:     superSec.MembershipSpec.String(),
		})
	}
	if subSec.CoSectionGroup != "" && subSec.CoSectionGroup != superSec.CoSectionGroup {
		out = append(out, SubsetMismatch{
			Kind:    SubsetMismatchGroup,
			Section: subSec.Name,
			Want:    string(subSec.CoSectionGroup),
			Got:     string(superSec.CoSectionGroup),
		})
	}
	if subSec.StreamingGroup != "" && subSec.StreamingGroup != superSec.StreamingGroup {
		out = append(out, SubsetMismatch{
			Kind:    SubsetMismatchGroup,
			Section: subSec.Name,
			Want:    string(subSec.StreamingGroup),
			Got:     string(superSec.StreamingGroup),
		})
	}

	idx := make(map[naming.StylableName]int, len(superSec.ValueColumnNames))
	for i, name := range superSec.ValueColumnNames {
		idx[name] = i
	}
	for i, name := range subSec.ValueColumnNames {
		j, has := idx[name]
		if !has {
			out = append(out, SubsetMismatch{
				Kind:    SubsetMismatchMissingColumn,
				Section: subSec.Name,
				Column:  name,
			})
			continue
		}
		out = appendColumnMismatches(out, columnRef{
			section: subSec.Name,
			column:  name,
		}, columnFacts{
			ct:             subSec.ValueColumnTypes[i],
			encodingHints:  subSec.ValueEncodingHints[i],
			valueSemantics: subSec.ValueSemantics[i],
		}, columnFacts{
			ct:             superSec.ValueColumnTypes[j],
			encodingHints:  superSec.ValueEncodingHints[j],
			valueSemantics: superSec.ValueSemantics[j],
		})
	}
	return
}

// columnRef locates a column for reporting: either a tagged one (section set)
// or a plain one (plainItemType set).
type columnRef struct {
	section       naming.StylableName
	plainItemType PlainItemTypeE
	column        naming.StylableName
}

// columnFacts is the comparable content of one column, gathered from either the
// plain co-slices or a tagged section's.
type columnFacts struct {
	ct             canonicaltypes.PrimitiveAstNodeI
	encodingHints  encodingaspects.AspectSet
	valueSemantics valueaspects.AspectSet
}

func appendColumnMismatches(ms []SubsetMismatch, ref columnRef, subCol columnFacts, superCol columnFacts) (out []SubsetMismatch) {
	out = ms
	if !canonicalTypeEqual(subCol.ct, superCol.ct) {
		out = append(out, SubsetMismatch{
			Kind:          SubsetMismatchType,
			Section:       ref.section,
			PlainItemType: ref.plainItemType,
			Column:        ref.column,
			Want:          canonicalTypeString(subCol.ct),
			Got:           canonicalTypeString(superCol.ct),
		})
	}
	if !valueAspectsCovered(subCol.valueSemantics, superCol.valueSemantics) {
		out = append(out, SubsetMismatch{
			Kind:          SubsetMismatchValueSemantics,
			Section:       ref.section,
			PlainItemType: ref.plainItemType,
			Column:        ref.column,
			Want:          string(subCol.valueSemantics),
			Got:           string(superCol.valueSemantics),
		})
	}
	if !encodingAspectsCovered(subCol.encodingHints, superCol.encodingHints) {
		out = append(out, SubsetMismatch{
			Kind:          SubsetMismatchEncodingHints,
			Section:       ref.section,
			PlainItemType: ref.plainItemType,
			Column:        ref.column,
			Want:          string(subCol.encodingHints),
			Got:           string(superCol.encodingHints),
		})
	}
	return
}

// canonicalTypeEqual compares two canonical types exactly, through their
// canonical type signature — the whole authored type (base type, width, byte
// order, scalar modifier) renders into it, so string equality is type equality
// and no per-node-kind switch is needed. A nil type (a table built by hand
// rather than through the manipulator) equals only another nil.
func canonicalTypeEqual(a canonicaltypes.PrimitiveAstNodeI, b canonicaltypes.PrimitiveAstNodeI) (equal bool) {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.String() == b.String()
}

func canonicalTypeString(ct canonicaltypes.PrimitiveAstNodeI) (s string) {
	if ct == nil {
		return ""
	}
	return ct.String()
}

// The three aspect vocabularies are distinct types over the same base62-encoded
// bitset, so coverage is the same test three times: an aspect set is a true
// join semilattice under union, hence "sub ∪ super == super" is exactly
// "sub ⊆ super". Both sides are re-encoded through the same union so a
// non-canonical encoding on either cannot skew the comparison. An unset or
// invalid sub asks for nothing and is always covered.

func useAspectsCovered(sub useaspects.AspectSet, super useaspects.AspectSet) (ok bool) {
	if !sub.IsValid() || sub.IsEmptySet() {
		return true
	}
	if !super.IsValid() {
		return false
	}
	return sub.UnionAspectsIgnoreInvalid(super) == super.UnionAspectsIgnoreInvalid(super)
}

func valueAspectsCovered(sub valueaspects.AspectSet, super valueaspects.AspectSet) (ok bool) {
	if !sub.IsValid() || sub.IsEmptySet() {
		return true
	}
	if !super.IsValid() {
		return false
	}
	return sub.UnionAspectsIgnoreInvalid(super) == super.UnionAspectsIgnoreInvalid(super)
}

func encodingAspectsCovered(sub encodingaspects.AspectSet, super encodingaspects.AspectSet) (ok bool) {
	if !sub.IsValid() || sub.IsEmptySet() {
		return true
	}
	if !super.IsValid() {
		return false
	}
	return sub.UnionAspectsIgnoreInvalid(super) == super.UnionAspectsIgnoreInvalid(super)
}

// sharesColumn reports whether the two tables name at least one column in
// common, ignoring everything about it but its identity. It separates overlap
// from disjoint in Relate, where the containment walks have already failed in
// both directions.
func sharesColumn(tbl1 *TableDesc, tbl2 *TableDesc) (shares bool) {
	plain := make(map[plainColumnKey]struct{}, len(tbl1.PlainValuesNames))
	for i, name := range tbl1.PlainValuesNames {
		plain[plainColumnKey{itemType: tbl1.PlainValuesItemTypes[i], name: name}] = struct{}{}
	}
	for i, name := range tbl2.PlainValuesNames {
		if _, has := plain[plainColumnKey{itemType: tbl2.PlainValuesItemTypes[i], name: name}]; has {
			return true
		}
	}
	tagged := make(map[columnRef]struct{}, 16)
	for _, sec := range tbl1.TaggedValuesSections {
		for _, name := range sec.ValueColumnNames {
			tagged[columnRef{section: sec.Name, column: name}] = struct{}{}
		}
	}
	for _, sec := range tbl2.TaggedValuesSections {
		for _, name := range sec.ValueColumnNames {
			if _, has := tagged[columnRef{section: sec.Name, column: name}]; has {
				return true
			}
		}
	}
	return false
}
