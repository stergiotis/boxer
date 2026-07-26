package queryengine

import (
	"hash/fnv"
	"slices"
)

// member.go — the place R4's affinity token gets judged.
//
// The dispatch seam (ADR-0141) threads an affinity token through every
// resolver and nothing judges it, because boxer's own endpoints have no
// members to choose between. That is fine until an engine has replicas:
// a reactive query graph re-runs dependent queries, and if two queries of
// one evaluation generation land on replicas with different replication
// lag, co-displayed panels disagree with each other and nothing says why.
//
// R4 asks for member choice to be a deterministic function of (placement,
// generation) so that divergence is impossible rather than discouraged.
// That function is here. The roster it operates on is NOT — a placement map
// is site data, published through the introspection registry (E5) by
// whoever owns the deployment.

// SelectMember picks the member of a placement that an affinity token
// belongs to. ok is false when the placement is empty.
//
// This is not a balancer, and swapping it for one would be a mistake: the
// property being bought is that the SAME token always yields the SAME
// member, so one evaluation generation cannot straddle two replicas. Load
// spreading across generations falls out of tokens differing, which is a
// consequence rather than the goal — and any policy richer than this
// (weights, health, locality) is site policy that belongs with the roster.
//
// An empty affinity is a token like any other. It selects deterministically
// too; it simply means the caller declared no generation, and therefore
// gets no cross-query guarantee beyond the one member it lands on.
//
// The order members arrive in does not affect the answer. That matters more
// than it looks: a roster assembled from a map has no order at all, and a
// selection that silently depended on iteration luck would break exactly
// the guarantee this function exists to provide. Duplicates are left alone
// — a roster listing a member twice is the caller's statement, not an error
// to repair.
func SelectMember(members []string, affinity string) (member string, ok bool) {
	if len(members) == 0 {
		return
	}
	sorted := slices.Clone(members)
	slices.Sort(sorted)
	h := fnv.New64a()
	// FNV's Write never fails; the hash absorbs everything it is given.
	_, _ = h.Write([]byte(affinity))
	member = sorted[h.Sum64()%uint64(len(sorted))]
	ok = true
	return
}
