package clickhouse

import (
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// Skip-index emission policy (ADR-0181 §SD4): derive data-skipping IndexSpecs
// from the IR per the shape matrix ADR-0066's 2026-06-09 Update verified —
// `has`/`hasAll` with constant needles prune through a bloom_filter index on
// the lane they scan; `countEqual`/`indexOf` shapes are served by set(N)
// while per-granule distinct values stay ≤ N. The guard story's index
// carriers are the membership identity lanes (presence conjuncts) and, for
// const-discriminator kinds, the scalar string value lanes.
//
// Deliberately not aspect-borne: index intent is deployment-scoped and may
// drift from what a deployment actually created — an accepted trade-off
// (ADR-0181 §SD4). The defaults below are starting points, not measurements;
// fp rate and granularity are per-deployment tuning knobs.

// SkipIndexPolicy selects which lanes get which skip index. The zero value
// derives nothing; DefaultSkipIndexPolicy is the documented starting point.
type SkipIndexPolicy struct {
	// MembershipBloom emits a bloom_filter index on every membership lane —
	// the columns the generated Presence conjuncts (`has`, `hasAll`) prune on.
	MembershipBloom bool
	// ValueStringBloom emits a bloom_filter index on scalar utf8-string
	// value lanes — the const-discriminator carriers of ADR-0066's
	// value-side presence terms.
	ValueStringBloom bool
	// BloomFpRate is the bloom_filter false-positive rate; 0 emits the bare
	// `bloom_filter` form (ClickHouse's default rate).
	BloomFpRate float64
	// MembershipSet, when > 0, additionally emits a set(N) index beside each
	// membership bloom filter, serving `countEqual`/`indexOf` shapes while
	// per-granule distinct membership values stay ≤ N. Fits
	// homogeneous-ingest tables; 0 emits none.
	MembershipSet uint32
	// Granularity is the GRANULARITY clause for every derived index; 0 emits
	// none (ClickHouse default).
	Granularity uint32
}

// DefaultSkipIndexPolicy is the documented default: bloom_filter(0.01) on
// membership lanes at GRANULARITY 4, no value-lane blooms, no set indexes.
func DefaultSkipIndexPolicy() SkipIndexPolicy {
	return SkipIndexPolicy{
		MembershipBloom: true,
		BloomFpRate:     0.01,
		Granularity:     4,
	}
}

func (inst SkipIndexPolicy) bloomType() string {
	if inst.BloomFpRate > 0 {
		return "bloom_filter(" + strconv.FormatFloat(inst.BloomFpRate, 'g', -1, 64) + ")"
	}
	return "bloom_filter"
}

// DeriveSkipIndexes walks the IR and returns the IndexSpecs the policy
// selects. Set indexes carry a `_set` name suffix so both indexes of a lane
// coexist.
func DeriveSkipIndexes(ir *common.IntermediateTableRepresentation, policy SkipIndexPolicy) (specs []IndexSpec, err error) {
	for cc, cp := range ir.IterateColumnProps() {
		if cc.SectionName == "" {
			continue
		}
		for i := range cp.Names {
			switch {
			case cc.SubType == common.IntermediateColumnsSubTypeMembership:
				ref := ColumnRef{Section: cc.SectionName, Role: cp.Roles[i]}
				if policy.MembershipBloom {
					specs = append(specs, IndexSpec{Ref: ref, Type: policy.bloomType(), Granularity: policy.Granularity})
				}
				if policy.MembershipSet > 0 {
					specs = append(specs, IndexSpec{
						Ref:         ref,
						Type:        "set(" + strconv.FormatUint(uint64(policy.MembershipSet), 10) + ")",
						Granularity: policy.Granularity,
						Name:        deriveIndexName(ref) + "_set",
					})
				}
			case policy.ValueStringBloom && cc.SubType == common.IntermediateColumnsSubTypeScalar && cp.Roles[i] == common.ColumnRoleValue:
				st, isString := cp.CanonicalType[i].(canonicaltypes.StringAstNode)
				if !isString || st.BaseType != canonicaltypes.BaseTypeStringUtf8 || st.ScalarModifier != canonicaltypes.ScalarModifierNone {
					continue
				}
				specs = append(specs, IndexSpec{
					Ref:         ColumnRef{Section: cc.SectionName, Column: cp.Names[i]},
					Type:        policy.bloomType(),
					Granularity: policy.Granularity,
				})
			}
		}
	}
	if len(specs) == 0 && (policy.MembershipBloom || policy.ValueStringBloom || policy.MembershipSet > 0) {
		err = eb.Build().Errorf("skip-index policy selected lanes but the schema has none (no tagged sections)")
	}
	return
}
