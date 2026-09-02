package canonform

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// PlainsMask selects which plain values are content (ADR-0201 SD1,
// 2026-09-01 update). The zero value keeps every plain except the entity id —
// the behaviour Options had before the mask existed — so existing digests do
// not move.
//
// The mask is part of the digest's identity: two parties computing digests
// must agree on it as they must on the classifier and the digester. It is
// declarative rather than a callback so it can be validated against the
// table at construction and pinned as a string (CanonicalString,
// Encoder.FormPin); a misspelled exclusion is an error, never a silently
// included column.
type PlainsMask struct {
	// IncludeEntityId opts the entity-id item type in. The id stays a class
	// gate with the opposite default from every other plain: a content hash
	// that becomes the id cannot contain it (SD1). An opted-in entity-id
	// column can still be excluded by name — that is the split between an
	// identity that is content and a key that is not.
	IncludeEntityId bool
	// ExcludeItemTypes excludes every plain column of the listed item types.
	// PlainItemTypeEntityId is not accepted here; its gate is IncludeEntityId.
	ExcludeItemTypes []common.PlainItemTypeE
	// ExcludeNames excludes single plain columns by nominal name.
	ExcludeNames []naming.StylableName
}

// includes reports whether a plain column of the given item type and nominal
// name is content under the mask.
func (inst PlainsMask) includes(itemType common.PlainItemTypeE, name naming.StylableName) bool {
	if itemType == common.PlainItemTypeEntityId && !inst.IncludeEntityId {
		return false
	}
	if slices.Contains(inst.ExcludeItemTypes, itemType) {
		return false
	}
	return !slices.Contains(inst.ExcludeNames, name)
}

// validate checks the mask against the table's IR: every exclusion must name
// something the table declares, so a typo cannot silently widen the digest's
// domain. The entity-id item type is rejected in ExcludeItemTypes because it
// already has its own gate and one spelling per selection keeps
// CanonicalString unique.
func (inst PlainsMask) validate(ir *common.IntermediateTableRepresentation) (err error) {
	declaredTypes := make([]common.PlainItemTypeE, 0, len(ir.PlainValueDesc))
	declaredNames := make([]naming.StylableName, 0, 8)
	for _, p := range ir.PlainValueDesc {
		if p == nil {
			continue
		}
		declaredTypes = append(declaredTypes, p.ItemType)
		for _, cp := range []*common.IntermediateColumnProps{p.Scalar, p.NonScalarHomogenousArray, p.NonScalarSet} {
			if cp == nil {
				continue
			}
			declaredNames = append(declaredNames, cp.Names...)
		}
	}
	for _, it := range inst.ExcludeItemTypes {
		if it == common.PlainItemTypeEntityId {
			return eb.Build().Errorf("canonform: the entity-id item type is gated by IncludeEntityId, not by ExcludeItemTypes")
		}
		if !slices.Contains(declaredTypes, it) {
			return eb.Build().Stringer("itemType", it).Errorf("canonform: the table declares no plain section of the excluded item type")
		}
	}
	for _, n := range inst.ExcludeNames {
		if !slices.Contains(declaredNames, n) {
			return eb.Build().Str("name", string(n)).Errorf("canonform: the table declares no plain column of the excluded name")
		}
	}
	return
}

// CanonicalString is the mask's pinned form: normalized (sorted,
// deduplicated), versioned, and unique per selection. A consumer that stores
// digests stores it — usually via Encoder.FormPin — and two parties agree on
// digests only if their masks render the same string.
func (inst PlainsMask) CanonicalString() string {
	var sb strings.Builder
	sb.WriteString("plains-mask/v1 entity-id=")
	if inst.IncludeEntityId {
		sb.WriteString("in")
	} else {
		sb.WriteString("out")
	}
	types := slices.Clone(inst.ExcludeItemTypes)
	slices.Sort(types)
	types = slices.Compact(types)
	sb.WriteString(" exclude-types=[")
	for i, it := range types {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(it.String())
	}
	sb.WriteString("] exclude-names=[")
	names := slices.Clone(inst.ExcludeNames)
	slices.Sort(names)
	names = slices.Compact(names)
	for i, n := range names {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.Quote(string(n)))
	}
	sb.WriteString("]")
	return sb.String()
}
