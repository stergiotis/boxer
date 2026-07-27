package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// Component detection (ADR-0146 D2): decide whether a row carries a kind, and
// whether it carries it soundly, WITHOUT decoding any value.
//
// This is the Go twin of the read-back generator's Presence / Validator
// artefacts (ADR-0066), and the primitive ADR-0075 specified for its ECS
// component registry and never built. Conformance is an attribute-count
// question, so answering it costs one pass over the membership columns of the
// sections a kind claims — no value column is touched.

// Contract returns the read contract T's Plan implies: the (section,
// membership) slots the kind claims and the attribute arity the writer
// guarantees for each. It is the introspection surface for a component
// registry, and what Detect checks a row against.
func Contract[T any]() (c mappingplan.ReadContract, err error) {
	r, err := resolveForType(reflect.TypeFor[T]())
	if err != nil {
		return
	}
	return mappingplan.DeriveReadContract(r.plan)
}

// Detect reports whether row i carries T.
//
//   - PresenceAbsent — nothing T claims is populated, or a slot T requires is
//     missing. Either way no sound decode exists, so the kind is not there.
//   - PresenceApproximate — every required slot is populated, but some slot
//     carries more (or fewer) attributes than T's shape admits. The kind is
//     recognisably present and cannot be decoded soundly — the case that
//     arises when two components claim one slot.
//   - PresenceExact — the row conforms; ReadComponent will decode it.
//
// lookup resolves ref-channel membership names to ids, exactly as Unmarshal
// uses it; pass nil when every membership is verbatim. readers must cover
// every section T declares, checked up front as in Unmarshal.
//
// Detect does not read values, so it does not report whether a value decodes —
// only whether the row's attribute layout matches T's contract.
func Detect[T any](readers *SectionReaders, i int, lookup LookupI, opts ...ReadOption) (p mappingplan.PresenceE, err error) {
	defer recoverContract(&err)
	counts, c, err := slotCounts[T](readers, i, lookup, buildReadOptions(opts))
	if err != nil {
		return
	}
	return c.Verdict(counts), nil
}

// DetectAll runs Detect over every row the readers carry, returning one
// verdict per row. It resolves the plan and the membership ids once for the
// whole batch rather than per row.
func DetectAll[T any](readers *SectionReaders, lookup LookupI, opts ...ReadOption) (out []mappingplan.PresenceE, err error) {
	defer recoverContract(&err)
	if readers == nil {
		err = eb.Build().Errorf("SectionReaders is nil")
		return
	}
	ro := buildReadOptions(opts)
	out = make([]mappingplan.PresenceE, 0, readers.numRows)
	for i := range readers.numRows {
		var counts map[int]int
		var c mappingplan.ReadContract
		counts, c, err = slotCounts[T](readers, i, lookup, ro)
		if err != nil {
			return
		}
		out = append(out, c.Verdict(counts))
	}
	return
}

// slotCounts tallies, for row i, how many attributes each of T's slots carries.
// The tally is keyed by slot index so a slot with an empty membership name (a
// tuple-owned section) stays distinguishable from a named one.
func slotCounts[T any](readers *SectionReaders, i int, lookup LookupI, ro readOptions) (counts map[int]int, c mappingplan.ReadContract, err error) {
	if lookup == nil {
		lookup = NoLookup{}
	}
	if readers == nil {
		err = eb.Build().Errorf("SectionReaders is nil")
		return
	}
	r, err := resolveForType(reflect.TypeFor[T]())
	if err != nil {
		return
	}
	if err = readers.checkCoverage(r.plan, r.groups); err != nil {
		return
	}
	c, err = mappingplan.DeriveReadContract(r.plan)
	if err != nil {
		return
	}

	// Resolve ref-channel membership ids once. A slot on a verbatim channel
	// matches by literal bytes and needs none; a tuple-owned slot matches
	// nothing (every attribute in its section is its own).
	ids := make(map[int]uint64, len(c.Slots))
	for si, s := range c.Slots {
		if s.OwnsSection || s.Membership == "" || s.Channel.UsesCarrier() || !s.Channel.NeedsKindVar() {
			continue
		}
		var id uint64
		id, err = lookup.LookupMembership(s.Membership)
		if err != nil {
			err = eb.Build().Str("membership", s.Membership).Str("section", s.Section).Errorf("%w", err)
			return
		}
		ids[si] = id
	}

	counts = make(map[int]int, len(c.Slots))
	for si, s := range c.Slots {
		sr, ok := readers.sections[s.Section]
		if !ok {
			err = eb.Build().Str("section", s.Section).Errorf("no reader registered for section")
			return
		}
		attrs := reflect.ValueOf(sr.attrs)
		n := mustCall(attrs, "GetNumberOfAttributes", reflect.ValueOf(entityIdx(i)))[0].Int()
		if s.OwnsSection || s.Channel.UsesCarrier() {
			// Every attribute in the section belongs to this slot: tuple
			// exclusivity (ADR-0103) for the first, and for the second the
			// one-membership-per-carrier-section rule resolveCarriers enforces
			// — a carrier channel's identity is per-row data, so there is no
			// schema-side name to match it against (ADR-0072's per-row identity).
			counts[si] = int(n)
			continue
		}
		membs := reflect.ValueOf(sr.membs)
		method := "GetMembValue" + s.Channel.AddMethodSuffix()
		embedsName := s.Channel.EmbedsLiteralName()
		// The same role filter the decode uses. Detection and decode must agree
		// about which memberships select, or a row could detect Exact and then
		// fail to decode (ADR-0146 D3).
		rf := ro.newRoleFilter(s.Section, s.Channel)
		total := 0
		for attrJ := int64(0); attrJ < n; attrJ++ {
			seq := mustCall(membs, method, reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
			for _, v := range collectIterSeq(seq) {
				if embedsName {
					if name := string(v.Bytes()); name == s.Membership && rf.admitsVerbatim(name) {
						total++
					}
					continue
				}
				if id, has := ids[si]; has && v.Uint() == id && rf.admitsRef(id) {
					total++
				}
			}
		}
		counts[si] = total
	}
	return
}
