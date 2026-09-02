package canonform

import (
	"bytes"
	"slices"
	"sort"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// Options parameterizes the form's two inputs beyond the record itself
// (ADR-0201 SD1, SD5, SD7). Two parties computing digests must agree on all of
// them.
type Options struct {
	// Classifier decides per membership instance whether it is primary
	// (content) or secondary (annotation, not encoded). nil means every
	// membership is primary.
	Classifier membershiprole.ClassifierI
	// Plains selects which plain values are content. The zero value keeps
	// every plain except the entity id (SD1); see PlainsMask.
	Plains PlainsMask
	// Digester supplies the leaf and record hashers; nil selects the keyed
	// BLAKE3 default (NewBlake3Digester).
	Digester DigesterI
	// OnRecord, when set, is called after each entity with its digest. The
	// slice is valid until the next entity; copy it to keep it.
	OnRecord func(entityIdx int, digest []byte) error
}

// Encoder computes the canonical record form of every entity a
// streamreadaccess.Driver drives through it. It is a SinkI and implements the
// MembershipSinkI, ArrowValueSinkI and CoSectionTagSinkI capabilities; it
// must be driven with the Arrow lane (any SinkI-capable driver does that
// automatically) — the text lane is refused.
//
// Per attribute the encoder holds the column views and memberships the driver
// pushes and, at EndTaggedValue, classifies the memberships, skips the
// attribute if no primary one remains, and streams `[memberships, value]`
// into a fresh leaf hasher. At EndEntity it sorts the leaf digests and
// streams the entity item `{0: plains, 1: [leaf digests]}` into a record
// hasher. No canonical bytes are assembled; what is held back is the leaf
// digests (Size bytes each) and the plain-value views.
//
// Not goroutine-safe; one Encoder per driving goroutine.
type Encoder struct {
	opts Options
	dig  DigesterI
	size int
	cw   *runtime.CborWriter // the hasher writer
	sw   *runtime.CborWriter // the scratch writer, for elements sorted before hashing
	kw   *runtime.CborWriter // the key writer, for the cached name encodings
	sbuf bytes.Buffer

	// Per co-section group (by key): the owning section of every merged value
	// column, in the driver's merged order (ADR-0201 SD6).
	coGroupColSections map[naming.Key][]naming.StylableName
	layouts            map[string]*valueLayout
	plainKeys          map[naming.StylableName][]byte
	// Per plain value column (by nominal name): whether it is content under
	// Options.Plains — resolved once against the IR at construction.
	plainInclude map[naming.StylableName]bool

	// batch
	recordDigests []byte
	nRecords      int
	entityIdx     int

	// entity
	leaves []byte
	plains []colView

	// section
	curSectionName naming.StylableName
	curTagSec      membershiprole.SectionContext
	curLayout      *valueLayout
	inCoGroup      bool
	coGroupKey     naming.Key

	// attribute
	cols    []colView
	members []memberRec
	curCol  *colView

	// sorting
	ranges []scratchRange
	tmp    []byte

	err error
}

var _ streamreadaccess.SinkI = (*Encoder)(nil)
var _ streamreadaccess.MembershipSinkI = (*Encoder)(nil)
var _ streamreadaccess.ArrowValueSinkI = (*Encoder)(nil)
var _ streamreadaccess.CoSectionTagSinkI = (*Encoder)(nil)

type viewKindE uint8

const (
	viewKindNone viewKindE = iota
	viewKindScalar
	viewKindArray
	viewKindSet
)

// view is a zero-copy reference into the retained RecordBatch: one element
// for a scalar, a range for a container.
type view struct {
	kind  viewKindE
	arr   arrow.Array
	idx   int
	start int
	end   int
}

type colView struct {
	name naming.StylableName
	ct   canonicaltypes.PrimitiveAstNodeI
	v    view
}

type memberRec struct {
	kind     membership.IdentityEncoding
	ref      uint64
	verbatim string
	params   string
	sec      membershiprole.SectionContext
	keep     bool
}

// valueLayout is the per-section (or per-co-group) precomputation of the map
// keys and their sorted order (ADR-0201 SD6): the value columns of a section
// are the same for every attribute, so the key encodings and the permutation
// are computed once at the first BeginSection.
type valueLayout struct {
	nCols  int
	keyEnc [][]byte
	order  []int
}

type scratchRange struct {
	start int
	end   int
}

// NewEncoder prepares an encoder for the table the driver will read. ir is
// the same IR the driver is constructed with; it is consulted once, to learn
// which section owns each value column of a co-section group and which plain
// columns Options.Plains selects.
func NewEncoder(ir *common.IntermediateTableRepresentation, opts Options) (inst *Encoder, err error) {
	if ir == nil {
		err = eh.Errorf("canonform: the encoder needs the table's intermediate representation")
		return
	}
	dig := opts.Digester
	if dig == nil {
		dig = NewBlake3Digester()
	}
	if dig.Size() <= 0 {
		err = eh.Errorf("canonform: digester reports a non-positive digest size")
		return
	}
	if err = opts.Plains.validate(ir); err != nil {
		return
	}
	inst = &Encoder{
		opts:               opts,
		dig:                dig,
		size:               dig.Size(),
		coGroupColSections: make(map[naming.Key][]naming.StylableName, 4),
		layouts:            make(map[string]*valueLayout, 16),
		plainKeys:          make(map[naming.StylableName][]byte, 8),
		plainInclude:       make(map[naming.StylableName]bool, 8),
	}
	for _, p := range ir.PlainValueDesc {
		if p == nil {
			continue
		}
		for _, cp := range []*common.IntermediateColumnProps{p.Scalar, p.NonScalarHomogenousArray, p.NonScalarSet} {
			if cp == nil {
				continue
			}
			for _, name := range cp.Names {
				inst.plainInclude[name] = opts.Plains.includes(p.ItemType, name)
			}
		}
	}
	if inst.cw, err = runtime.NewCborWriter(nil); err != nil {
		inst = nil
		return
	}
	if inst.sw, err = runtime.NewCborWriter(&inst.sbuf); err != nil {
		inst = nil
		return
	}
	if inst.kw, err = runtime.NewCborWriter(nil); err != nil {
		inst = nil
		return
	}
	for _, tv := range ir.TaggedValueDesc {
		if tv == nil || tv.CoSectionGroup == "" {
			continue
		}
		key := tv.CoSectionGroup
		names := inst.coGroupColSections[key]
		for _, cp := range []*common.IntermediateColumnProps{tv.Scalar, tv.NonScalarHomogenousArray, tv.NonScalarSet} {
			if cp == nil {
				continue
			}
			for range cp.Names {
				names = append(names, tv.SectionName)
			}
		}
		inst.coGroupColSections[key] = names
	}
	return
}

// DigestSize is the length in bytes of every digest this encoder produces.
func (inst *Encoder) DigestSize() int { return inst.size }

// NumRecords is the number of entities digested since the last BeginBatch.
func (inst *Encoder) NumRecords() int { return inst.nRecords }

// RecordDigest returns the digest of the i-th entity of the current batch.
// The slice aliases the encoder's buffer and is valid until the next
// BeginBatch; copy it to keep it.
func (inst *Encoder) RecordDigest(i int) []byte {
	return inst.recordDigests[i*inst.size : (i+1)*inst.size]
}

// Err returns the first error the encoder met since the last BeginBatch; the
// driver also surfaces it through the End* return values.
func (inst *Encoder) Err() error { return inst.err }

func (inst *Encoder) fail(err error) {
	if inst.err == nil && err != nil {
		inst.err = err
	}
}

// --- batch / entity ---

func (inst *Encoder) BeginBatch() {
	inst.recordDigests = inst.recordDigests[:0]
	inst.nRecords = 0
	inst.entityIdx = -1
	inst.err = nil
}

func (inst *Encoder) EndBatch() (err error) { return inst.err }

func (inst *Encoder) BeginEntity() {
	inst.entityIdx++
	inst.leaves = inst.leaves[:0]
	inst.plains = inst.plains[:0]
}

func (inst *Encoder) EndEntity() (err error) {
	if inst.err != nil {
		return inst.err
	}
	h := inst.dig.NewRecord()
	cw := inst.cw
	cw.Reset(h)
	cw.MapHead(2)
	cw.WriteUint(0)
	inst.writePlains(cw)
	cw.WriteUint(1)
	inst.writeLeaves(cw)
	if cw.Err() != nil {
		inst.fail(cw.Err())
		return inst.err
	}
	before := len(inst.recordDigests)
	inst.recordDigests = h.Sum(inst.recordDigests)
	inst.nRecords++
	if inst.opts.OnRecord != nil {
		if err = inst.opts.OnRecord(inst.entityIdx, inst.recordDigests[before:]); err != nil {
			inst.fail(err)
			return inst.err
		}
	}
	return
}

// writePlains writes the plains map: name → value, keys sorted bytewise by
// their text encoding (ADR-0201 SD1).
func (inst *Encoder) writePlains(cw *runtime.CborWriter) {
	ps := inst.plains
	slices.SortFunc(ps, func(a, b colView) int {
		return bytes.Compare(inst.plainKey(a.name), inst.plainKey(b.name))
	})
	cw.MapHead(len(ps))
	for i := range ps {
		cw.Write(inst.plainKey(ps[i].name))
		inst.writeView(cw, &ps[i].v, ps[i].ct)
	}
}

// plainKey returns the CBOR text encoding of a plain value name, cached.
func (inst *Encoder) plainKey(name naming.StylableName) []byte {
	if k, ok := inst.plainKeys[name]; ok {
		return k
	}
	var b bytes.Buffer
	w := inst.kw
	w.Reset(&b)
	w.WriteTextString(string(name))
	k := append([]byte(nil), b.Bytes()...)
	inst.plainKeys[name] = k
	return k
}

// writeLeaves sorts the leaf digests bytewise and writes them as an array of
// byte strings.
func (inst *Encoder) writeLeaves(cw *runtime.CborWriter) {
	n := len(inst.leaves) / inst.size
	if len(inst.tmp) < inst.size {
		inst.tmp = make([]byte, inst.size)
	}
	sort.Sort(&chunkSorter{b: inst.leaves, size: inst.size, n: n, tmp: inst.tmp})
	cw.ArrayHead(n)
	for i := range n {
		cw.WriteBytes(inst.leaves[i*inst.size : (i+1)*inst.size])
	}
}

type chunkSorter struct {
	b    []byte
	size int
	n    int
	tmp  []byte
}

func (s *chunkSorter) Len() int { return s.n }
func (s *chunkSorter) Less(i, j int) bool {
	return bytes.Compare(s.b[i*s.size:(i+1)*s.size], s.b[j*s.size:(j+1)*s.size]) < 0
}
func (s *chunkSorter) Swap(i, j int) {
	a := s.b[i*s.size : (i+1)*s.size]
	c := s.b[j*s.size : (j+1)*s.size]
	copy(s.tmp, a)
	copy(a, c)
	copy(c, s.tmp)
}

// --- plain sections ---

func (inst *Encoder) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.curSectionName = ""
	inst.curLayout = nil
}

func (inst *Encoder) EndPlainSection() (err error) {
	return inst.err
}

func (inst *Encoder) BeginPlainValue()           {}
func (inst *Encoder) EndPlainValue() (err error) { return inst.err }

// --- tagged sections ---

func (inst *Encoder) BeginTaggedSections()           {}
func (inst *Encoder) EndTaggedSections() (err error) { return inst.err }

func (inst *Encoder) BeginCoSectionGroup(name naming.Key) {
	inst.inCoGroup = true
	inst.coGroupKey = name
}

func (inst *Encoder) EndCoSectionGroup() (err error) {
	inst.inCoGroup = false
	inst.coGroupKey = ""
	return inst.err
}

func (inst *Encoder) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, useAspects useaspects.AspectSet, nAttrs int) {
	inst.curSectionName = name
	inst.curTagSec = membershiprole.SectionContext{Name: name, UseAspects: useAspects}
	inst.curLayout = inst.layoutFor(name, valueNames)
}

func (inst *Encoder) EndSection() (err error) {
	inst.curLayout = nil
	return inst.err
}

// layoutFor returns the cached value layout of a standalone section (keyed by
// its name) or of the current co-section group (keyed by the group key),
// computing it on first sight: the map keys are the column names, or the
// `section:column` handles inside a co-group, and the order is the bytewise
// order of their text encodings (ADR-0201 SD6).
func (inst *Encoder) layoutFor(sectionName naming.StylableName, valueNames []naming.StylableName) *valueLayout {
	key := string(sectionName)
	if inst.inCoGroup {
		key = "\x00" + string(inst.coGroupKey)
	}
	if l, ok := inst.layouts[key]; ok {
		return l
	}
	l := &valueLayout{nCols: len(valueNames), keyEnc: make([][]byte, len(valueNames)), order: make([]int, len(valueNames))}
	var secs []naming.StylableName
	if inst.inCoGroup {
		secs = inst.coGroupColSections[inst.coGroupKey]
		if len(secs) != len(valueNames) {
			inst.fail(eb.Build().Str("coGroup", string(inst.coGroupKey)).Int("irColumns", len(secs)).Int("drivenColumns", len(valueNames)).Errorf("canonform: co-section group column count disagrees between the IR and the driver"))
		}
	}
	var b bytes.Buffer
	w := inst.kw
	w.Reset(&b)
	for i, n := range valueNames {
		b.Reset()
		if inst.inCoGroup && i < len(secs) {
			w.WriteTextString(string(secs[i]) + ":" + string(n))
		} else {
			w.WriteTextString(string(n))
		}
		l.keyEnc[i] = append([]byte(nil), b.Bytes()...)
		l.order[i] = i
	}
	slices.SortFunc(l.order, func(a, b int) int { return bytes.Compare(l.keyEnc[a], l.keyEnc[b]) })
	inst.layouts[key] = l
	return l
}

// --- attributes ---

func (inst *Encoder) BeginTaggedValue() {
	inst.cols = inst.cols[:0]
	inst.members = inst.members[:0]
	inst.curCol = nil
	// The tag-frame context defaults to the section's own; BeginCoSectionTags
	// narrows it per co-section.
	inst.curTagSec = membershiprole.SectionContext{Name: inst.curSectionName, UseAspects: inst.curTagSec.UseAspects}
}

func (inst *Encoder) EndTaggedValue() (err error) {
	if inst.err != nil {
		return inst.err
	}
	// Classify: placeholders out, secondaries out; an attribute that carried
	// memberships but no primary one is an annotation overlay and contributes
	// nothing (ADR-0201 SD5).
	nReal, nPrimary := 0, 0
	for i := range inst.members {
		m := &inst.members[i]
		m.keep = false
		mv := membership.MembershipValue{Kind: m.kind, Ref: m.ref, Verbatim: m.verbatim, Params: m.params}
		if membership.IsPlaceholder(mv) {
			continue
		}
		nReal++
		role := membershiprole.MembershipRolePrimary
		if inst.opts.Classifier != nil {
			role, _ = inst.opts.Classifier.Classify(m.sec, mv)
		}
		if role == membershiprole.MembershipRolePrimary {
			m.keep = true
			nPrimary++
		}
	}
	if nReal > 0 && nPrimary == 0 {
		return
	}
	h := inst.dig.NewLeaf()
	cw := inst.cw
	cw.Reset(h)
	cw.ArrayHead(2)
	inst.writeMemberships(cw)
	inst.writeAttributeValue(cw)
	if cw.Err() != nil {
		inst.fail(cw.Err())
		return inst.err
	}
	inst.leaves = h.Sum(inst.leaves)
	return inst.err
}

// writeMemberships writes the kept memberships as an array sorted bytewise by
// element encoding (ADR-0201 SD5). The elements are encoded into the scratch
// buffer first, sorted by offset, then written.
func (inst *Encoder) writeMemberships(cw *runtime.CborWriter) {
	sw := inst.sw
	inst.sbuf.Reset()
	sw.Reset(&inst.sbuf)
	inst.ranges = inst.ranges[:0]
	for i := range inst.members {
		m := &inst.members[i]
		if !m.keep {
			continue
		}
		start := inst.sbuf.Len()
		switch m.kind {
		case membership.IdentityRef:
			sw.WriteUint(m.ref)
		case membership.IdentityVerbatim:
			sw.WriteBytesString(m.verbatim)
		case membership.IdentityPerRowId:
			sw.ArrayHead(2)
			sw.WriteUint(m.ref)
			sw.WriteBytesString(m.params)
		case membership.IdentityPerRowName:
			sw.ArrayHead(2)
			sw.WriteBytesString(m.verbatim)
			sw.WriteBytesString(m.params)
		case membership.IdentityPerRowBlob:
			sw.ArrayHead(1)
			sw.WriteBytesString(m.params)
		default:
			inst.fail(eb.Build().Stringer("kind", m.kind).Errorf("canonform: unknown membership identity encoding"))
			return
		}
		inst.ranges = append(inst.ranges, scratchRange{start: start, end: inst.sbuf.Len()})
	}
	if sw.Err() != nil {
		inst.fail(sw.Err())
		return
	}
	buf := inst.sbuf.Bytes()
	slices.SortFunc(inst.ranges, func(a, b scratchRange) int { return bytes.Compare(buf[a.start:a.end], buf[b.start:b.end]) })
	cw.ArrayHead(len(inst.ranges))
	for _, r := range inst.ranges {
		cw.Write(buf[r.start:r.end])
	}
}

// writeAttributeValue writes the value part of the attribute item (ADR-0201
// SD6): null for a value-less section, the bare value for a single column of
// a standalone section, otherwise a map keyed by column name — or by the
// `section:column` handle inside a co-section group — in the layout's order.
func (inst *Encoder) writeAttributeValue(cw *runtime.CborWriter) {
	n := len(inst.cols)
	if n == 0 {
		cw.WriteNull()
		return
	}
	if n == 1 && !inst.inCoGroup {
		inst.writeView(cw, &inst.cols[0].v, inst.cols[0].ct)
		return
	}
	l := inst.curLayout
	if l == nil || l.nCols != n {
		inst.fail(eb.Build().Str("section", string(inst.curSectionName)).Int("driven", n).Errorf("canonform: attribute column count disagrees with the section layout"))
		return
	}
	cw.MapHead(n)
	for _, ord := range l.order {
		cw.Write(l.keyEnc[ord])
		inst.writeView(cw, &inst.cols[ord].v, inst.cols[ord].ct)
	}
}

// writeView writes one column's value from its Arrow view (ADR-0201 SD3/SD4):
// the scalar form, an array of element forms in order, or a tag-258 set of
// the distinct element forms sorted bytewise.
func (inst *Encoder) writeView(cw *runtime.CborWriter, v *view, ct canonicaltypes.PrimitiveAstNodeI) {
	switch v.kind {
	case viewKindScalar:
		if err := writeScalar(cw, v.arr, v.idx, ct); err != nil {
			inst.fail(err)
		}
	case viewKindArray:
		elem := scalarOf(ct)
		cw.ArrayHead(v.end - v.start)
		for i := v.start; i < v.end; i++ {
			if err := writeScalar(cw, v.arr, i, elem); err != nil {
				inst.fail(err)
				return
			}
		}
	case viewKindSet:
		inst.writeSet(cw, v, scalarOf(ct))
	default:
		inst.fail(errNoView)
	}
}

// writeSet encodes the set's elements into the scratch buffer, sorts them
// bytewise, drops duplicates (equality of canonical bytes) and writes
// tag 258 + the array.
func (inst *Encoder) writeSet(cw *runtime.CborWriter, v *view, elem canonicaltypes.PrimitiveAstNodeI) {
	sw := inst.sw
	inst.sbuf.Reset()
	sw.Reset(&inst.sbuf)
	inst.ranges = inst.ranges[:0]
	for i := v.start; i < v.end; i++ {
		start := inst.sbuf.Len()
		if err := writeScalar(sw, v.arr, i, elem); err != nil {
			inst.fail(err)
			return
		}
		inst.ranges = append(inst.ranges, scratchRange{start: start, end: inst.sbuf.Len()})
	}
	if sw.Err() != nil {
		inst.fail(sw.Err())
		return
	}
	buf := inst.sbuf.Bytes()
	rs := inst.ranges
	slices.SortFunc(rs, func(a, b scratchRange) int { return bytes.Compare(buf[a.start:a.end], buf[b.start:b.end]) })
	// Dedup in place.
	w := 0
	for i := range rs {
		if i > 0 && bytes.Equal(buf[rs[i].start:rs[i].end], buf[rs[w-1].start:rs[w-1].end]) {
			continue
		}
		rs[w] = rs[i]
		w++
	}
	rs = rs[:w]
	cw.Tag(runtime.TagSet)
	cw.ArrayHead(len(rs))
	for _, r := range rs {
		cw.Write(buf[r.start:r.end])
	}
}

// --- columns and values ---

func (inst *Encoder) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI, valueSemantics valueaspects.AspectSet) {
	if inst.curLayout == nil && inst.curSectionName == "" {
		// Plain section: the per-column verdict was resolved against the IR
		// at construction (Options.Plains).
		inc, known := inst.plainInclude[name]
		if !known {
			inst.fail(eb.Build().Str("name", string(name)).Errorf("canonform: driven plain column is not in the IR"))
			inst.curCol = nil
			return
		}
		if !inc {
			inst.curCol = nil
			return
		}
		inst.plains = append(inst.plains, colView{name: name, ct: canonicalType})
		inst.curCol = &inst.plains[len(inst.plains)-1]
		return
	}
	inst.cols = append(inst.cols, colView{name: name, ct: canonicalType})
	inst.curCol = &inst.cols[len(inst.cols)-1]
}

func (inst *Encoder) EndColumn() { inst.curCol = nil }

func (inst *Encoder) BeginScalarValue() {
	if inst.curCol != nil {
		inst.curCol.v.kind = viewKindScalar
	}
}
func (inst *Encoder) EndScalarValue() (err error) { return inst.err }

func (inst *Encoder) BeginHomogenousArrayValue(card int) {
	if inst.curCol != nil {
		inst.curCol.v.kind = viewKindArray
	}
}
func (inst *Encoder) EndHomogenousArrayValue() {}

func (inst *Encoder) BeginSetValue(card int) {
	if inst.curCol != nil {
		inst.curCol.v.kind = viewKindSet
	}
}
func (inst *Encoder) EndSetValue() {}

func (inst *Encoder) BeginValueItem(index int) {}
func (inst *Encoder) EndValueItem()            {}

// WriteArrowScalar / WriteArrowRange are the typed lane (ArrowValueSinkI).
func (inst *Encoder) WriteArrowScalar(arr arrow.Array, flatIdx int) {
	if inst.curCol == nil {
		return
	}
	inst.curCol.v.arr = arr
	inst.curCol.v.idx = flatIdx
}

func (inst *Encoder) WriteArrowRange(arr arrow.Array, start int, end int) {
	if inst.curCol == nil {
		return
	}
	inst.curCol.v.arr = arr
	inst.curCol.v.start = start
	inst.curCol.v.end = end
}

// Write and WriteString are the text lane, which the encoder refuses: a value
// that arrives formatted is not the exact value (ADR-0201 Context).
func (inst *Encoder) Write(p []byte) (n int, err error) {
	inst.fail(errNoView)
	return len(p), nil
}

func (inst *Encoder) WriteString(s string) (n int, err error) {
	inst.fail(errNoView)
	return len(s), nil
}

// --- memberships ---

func (inst *Encoder) BeginTags(nTags int) {}
func (inst *Encoder) EndTags()            {}

func (inst *Encoder) BeginCoSectionTags(sectionName naming.StylableName, useAspects useaspects.AspectSet) {
	inst.curTagSec = membershiprole.SectionContext{Name: sectionName, UseAspects: useAspects}
}

func (inst *Encoder) addMember(kind membership.IdentityEncoding, ref uint64, verbatim string, params string) {
	inst.members = append(inst.members, memberRec{kind: kind, ref: ref, verbatim: verbatim, params: params, sec: inst.curTagSec})
}

func (inst *Encoder) AddMembershipRef(lowCard bool, ref uint64) {
	inst.addMember(membership.IdentityRef, ref, "", "")
}

func (inst *Encoder) AddMembershipVerbatim(lowCard bool, verbatim string) {
	inst.addMember(membership.IdentityVerbatim, 0, verbatim, "")
}

func (inst *Encoder) AddMembershipRefParametrized(lowCard bool, ref uint64, params string) {
	inst.addMember(membership.IdentityPerRowBlob, ref, "", params)
}

func (inst *Encoder) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.addMember(membership.IdentityPerRowId, ref, "", params)
}

func (inst *Encoder) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.addMember(membership.IdentityPerRowName, 0, verbatim, params)
}

// ClassifierPinAllPrimary is the pin of the nil classifier: every membership
// is primary (ADR-0201 SD5).
const ClassifierPinAllPrimary = "all-primary"

// FormPin renders the identity of everything a digest from this encoder is a
// function of beyond the record itself: the form version, the digester, the
// plains mask and the classifier (ADR-0201 SD5/SD7). A consumer that stores
// digests stores this string beside them; two parties agree on digests only
// if they agree on the pin. It fails when the classifier does not implement
// membershiprole.PinnableI — an unpinnable classifier yields digests whose
// domain cannot be named, which is exactly what a digest-storing consumer
// must not build on.
func (inst *Encoder) FormPin() (pin string, err error) {
	cpin := ClassifierPinAllPrimary
	if c := inst.opts.Classifier; c != nil {
		p, ok := c.(membershiprole.PinnableI)
		if !ok {
			err = eb.Build().Errorf("canonform: the classifier does not implement membershiprole.PinnableI, so digests computed under it cannot be pinned")
			return
		}
		cpin = p.Pin()
	}
	pin = "canonform/v1 digester=" + inst.dig.Name() + "/" + strconv.Itoa(inst.size) +
		" " + inst.opts.Plains.CanonicalString() + " classifier=" + cpin
	return
}
