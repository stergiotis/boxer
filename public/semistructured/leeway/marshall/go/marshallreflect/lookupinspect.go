package marshallreflect

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// Diagnosing a wrong membership lookup (ADR-0146 §Scope, the deferred
// "verify resolved lookup ids against the wire").
//
// A ref-channel membership rides the wire as a uint64 id and nothing else — the
// name lives in the registry the LookupI wraps. So a reader whose lookup maps
// `health` to a different id than the writer used asks for an id that is not
// there, and the slot reads as unpopulated. For a mandatory scalar the arity
// gate catches that; for an Option or a container, zero attributes is a legal
// state, so the field reads back empty and nothing is wrong as far as the
// contract can tell.
//
// That case cannot be turned into a check. Absence and a wrong id produce the
// identical wire observation for one slot, and under fusion a component really
// may be absent from every row of a batch. Verification would have to compare
// against the registry, which is the thing already assumed correct.
//
// What IS possible is refusing to let it be silent. InspectLookup reports what
// each slot resolved to and what the wire actually carried, so the answer to
// "why is this field empty?" is one call away instead of a bisect. The same
// observation is attached to the arity gate's under-population error, where a
// wrong lookup shows up most often.

// SlotObservation is one slot's resolved identity and what it found.
type SlotObservation struct {
	Section    string
	Membership string
	Channel    mappingplan.MembershipChannel
	// ResolvedID is the id the lookup returned, meaningful only when Resolved
	// is true (a ref channel that resolved). A verbatim channel matches by
	// literal name and needs no lookup.
	ResolvedID uint64
	Resolved   bool
	// Rows is how many rows carried at least one attribute for this slot;
	// Attributes is the total across the batch.
	Rows       int
	Attributes int
}

// LookupReport is the per-kind picture of a batch: what the DTO's slots
// resolved to, and which memberships the sections actually carry.
type LookupReport struct {
	Kind    string
	NumRows int
	Slots   []SlotObservation
	// SectionRefIDs and SectionNames list the DISTINCT memberships each claimed
	// section carries across the batch — ref ids and verbatim names
	// respectively — including memberships this kind does not claim. That is
	// the comparison that makes a wrong lookup obvious: the id you asked for is
	// absent while the section is populated by others.
	SectionRefIDs map[string][]uint64
	SectionNames  map[string][]string
}

// Suspect returns the slots that look like a lookup mismatch rather than an
// honest absence: the slot resolved to an id, found nothing, and its section is
// nevertheless carrying memberships.
//
// This is a heuristic and says so — under fusion a section is often populated
// entirely by components this kind does not claim, which produces the same
// shape. It narrows where to look; it does not decide.
func (r LookupReport) Suspect() (out []SlotObservation) {
	for _, s := range r.Slots {
		if !s.Resolved || s.Attributes > 0 {
			continue
		}
		if len(r.SectionRefIDs[s.Section]) > 0 {
			out = append(out, s)
		}
	}
	return
}

// String renders the report as a short block, slot per line, with each claimed
// section's observed memberships underneath.
func (r LookupReport) String() string {
	var b strings.Builder
	b.WriteString(r.Kind)
	b.WriteString(" over ")
	b.WriteString(strconv.Itoa(r.NumRows))
	b.WriteString(" row(s):\n")
	for _, s := range r.Slots {
		b.WriteString("  ")
		b.WriteString(s.Section)
		b.WriteString("@")
		b.WriteString(s.Membership)
		if s.Resolved {
			b.WriteString(" id=")
			b.WriteString(strconv.FormatUint(s.ResolvedID, 10))
		} else {
			b.WriteString(" (verbatim)")
		}
		b.WriteString("  rows=")
		b.WriteString(strconv.Itoa(s.Rows))
		b.WriteString(" attrs=")
		b.WriteString(strconv.Itoa(s.Attributes))
		b.WriteString("\n")
	}
	for _, section := range sortedKeys(r.SectionRefIDs) {
		b.WriteString("  section ")
		b.WriteString(section)
		b.WriteString(" carries ids ")
		b.WriteString(joinUints(r.SectionRefIDs[section]))
		b.WriteString("\n")
	}
	for _, section := range sortedKeys(r.SectionNames) {
		b.WriteString("  section ")
		b.WriteString(section)
		b.WriteString(" carries names ")
		b.WriteString(strings.Join(r.SectionNames[section], ", "))
		b.WriteString("\n")
	}
	return b.String()
}

// InspectLookup reports, for every row the readers carry, what T's slots
// resolved to and what their sections actually hold. It reads only membership
// columns.
//
// Reach for it when a field decodes empty and you cannot tell whether the data
// is absent or the LookupI disagrees with the writer's registry.
func InspectLookup[T any](readers *SectionReaders, lookup LookupI, opts ...ReadOption) (rep LookupReport, err error) {
	defer recoverContract(&err)
	if readers == nil {
		err = eb.Build().Errorf("SectionReaders is nil")
		return
	}
	if lookup == nil {
		lookup = NoLookup{}
	}
	ro := buildReadOptions(opts)
	r, err := resolveForType(reflect.TypeFor[T]())
	if err != nil {
		return
	}
	if err = readers.checkCoverage(r.plan, r.groups); err != nil {
		return
	}
	c := r.contract

	rep = LookupReport{
		Kind:          c.Kind,
		NumRows:       readers.numRows,
		SectionRefIDs: map[string][]uint64{},
		SectionNames:  map[string][]string{},
	}
	for _, s := range c.Slots {
		obs := SlotObservation{Section: s.Section, Membership: s.Membership, Channel: s.Channel}
		// A lookup failure is reported as unresolved rather than aborting: the
		// point of this call is to explain a lookup problem, so it must survive
		// one.
		if !s.OwnsSection && s.Membership != "" && !s.Channel.UsesCarrier() && s.Channel.NeedsKindVar() {
			if id, lerr := lookup.LookupMembership(s.Membership); lerr == nil {
				obs.ResolvedID, obs.Resolved = id, true
			}
		}
		rep.Slots = append(rep.Slots, obs)
	}

	for i := 0; i < readers.numRows; i++ {
		counts, cerr := countSlots(readers, i, lookup, c, ro)
		if cerr != nil {
			// countSlots resolves ids too; a lookup that cannot resolve is the
			// very thing being diagnosed, so report what is known so far.
			err = eb.Build().Int("row", i).Errorf("%w", cerr)
			return rep, err
		}
		for si := range rep.Slots {
			if n := counts[si]; n > 0 {
				rep.Slots[si].Rows++
				rep.Slots[si].Attributes += n
			}
		}
	}

	for _, section := range c.Sections() {
		sr := readers.sections[section]
		ch := sectionChannel(c, section)
		ids, names := observeSection(reflect.ValueOf(sr.attrs), reflect.ValueOf(sr.membs), readers.numRows, ch)
		if len(ids) > 0 {
			rep.SectionRefIDs[section] = ids
		}
		if len(names) > 0 {
			rep.SectionNames[section] = names
		}
	}
	return
}

// sectionChannel returns the (uniform) membership channel of a contract's
// section.
func sectionChannel(c mappingplan.ReadContract, section string) mappingplan.MembershipChannel {
	for _, s := range c.Slots {
		if s.Section == section {
			return s.Channel
		}
	}
	return mappingplan.MembershipChannelLowCardRef
}

// observeSection collects the distinct memberships a section carries across
// rows — ref ids or verbatim names, per its channel. Carrier channels carry
// per-row identity with no schema-side name, so they yield nothing.
func observeSection(attrs, membs reflect.Value, numRows int, ch mappingplan.MembershipChannel) (ids []uint64, names []string) {
	if ch.UsesCarrier() {
		return
	}
	method := "GetMembValue" + ch.AddMethodSuffix()
	embedsName := ch.EmbedsLiteralName()
	for i := 0; i < numRows; i++ {
		n := mustCall(attrs, "GetNumberOfAttributes", reflect.ValueOf(entityIdx(i)))[0].Int()
		for attrJ := int64(0); attrJ < n; attrJ++ {
			seq := mustCall(membs, method, reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
			for _, v := range collectIterSeq(seq) {
				if embedsName {
					if name := string(v.Bytes()); !slices.Contains(names, name) {
						names = append(names, name)
					}
					continue
				}
				if id := v.Uint(); !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
	}
	slices.Sort(ids)
	slices.Sort(names)
	return
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func joinUints(v []uint64) string {
	parts := make([]string, len(v))
	for i, u := range v {
		parts[i] = strconv.FormatUint(u, 10)
	}
	return strings.Join(parts, ", ")
}
