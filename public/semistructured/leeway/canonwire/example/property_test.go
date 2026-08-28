package example

import (
	"bytes"
	"encoding/hex"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	rartime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// The properties of ADR-0210 M2, over three of the goldens: test_table
// (scalars, an `h` container, two membership channels), place (a co-section
// group and a section whose `h` and `m` columns are co-containers) and json
// (the two ambiguity sets, driven through the built-in ordinal tagger and
// dispatcher).
//
// Three claims per table:
//
//   - **round trip** — a drawn batch encodes to bytes₁, decodes into a fresh
//     builder and re-encodes to bytes₂; bytes₁ == bytes₂ and bytes₁ passes the
//     table-free checker. This is the losslessness claim: a column the decoder
//     dropped, widened or reordered moves the second pass.
//   - **permutation invariance** — the same content written twice with the
//     attributes in different orders, the memberships of each attribute in
//     different orders and the `m` columns' elements in different orders gives
//     identical bytes. This is what "canonical" means here, and it is the
//     claim the SD3 sort exists to make.
//   - **content sensitivity** — change one value, one membership or one set
//     element and the bytes differ. Without it the first two claims would be
//     satisfied by an encoder that wrote nothing.
//
// The drawn values deliberately include the edges the form has rules for: 0,
// MaxUint64, MinInt64, -0.0, ±Inf, empty and unicode strings, empty
// containers, and duplicate memberships and set elements. NaN is left out on
// purpose: the form folds every NaN to one (SD1), so a NaN payload is the one
// mutation content sensitivity could not see.

// ------------------------------------------------------------------- helpers

// shuffle permutes [0,n). It is how the permutation-invariance property writes
// the same content in a different order.
type shuffle func(n int) []int

func identityOrder(n int) (p []int) {
	p = make([]int, n)
	for i := range p {
		p[i] = i
	}
	return
}

// shuffleFromSeed derives a deterministic permutation source from one drawn
// integer, so a failing case shrinks to a seed rather than to a tree of draws.
func shuffleFromSeed(seed uint64) shuffle {
	r := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	return func(n int) (p []int) {
		p = identityOrder(n)
		r.Shuffle(n, func(i int, j int) { p[i], p[j] = p[j], p[i] })
		return
	}
}

// loadBatch is the round-trip suite's transfer over rapid's TestingT.
func loadBatch(t require.TestingT, records func([]arrow.RecordBatch) ([]arrow.RecordBatch, error), load func(rartime.RecordI) error) {
	recs, err := records(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.NoError(t, load(recs[0]))
}

var propF32 = []float32{
	0, float32(math.Copysign(0, -1)), 1, -1, 0.5, -0.25, 3.4028235e38, -3.4028235e38,
	1.4e-45, float32(math.Inf(1)), float32(math.Inf(-1)),
}

var propF64 = []float64{
	0, math.Copysign(0, -1), 1, -1, 0.1, math.MaxFloat64, -math.MaxFloat64,
	math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1),
}

var propU64 = []uint64{0, 1, 2, 255, 1 << 32, 1 << 63, math.MaxUint64}

var propU32 = []uint32{0, 1, 7, math.MaxUint32}

var propI64 = []int64{0, -1, 1, math.MinInt64, math.MaxInt64}

var propStrings = []string{"", "a", "aa", "héllo", "日本語", "\x00", strings.Repeat("z", 40)}

var propBytes = [][]byte{nil, {}, {0x00}, []byte("kind"), []byte("héllo")}

func drawF32(rt *rapid.T, label string) float32 {
	return rapid.SampledFrom(propF32).Draw(rt, label)
}

func drawU64(rt *rapid.T, label string) uint64 {
	return rapid.SampledFrom(propU64).Draw(rt, label)
}

func drawString(rt *rapid.T, label string) string {
	return rapid.SampledFrom(propStrings).Draw(rt, label)
}

func drawBytes(rt *rapid.T, label string) []byte {
	return rapid.SampledFrom(propBytes).Draw(rt, label)
}

// otherF32 returns a pool value whose bits differ from v, so a mutation of a
// float column is always observable. Bits, not ==, because -0.0 == 0.0 and the
// form keeps them apart.
func otherF32(v float32) float32 {
	for _, c := range propF32 {
		if math.Float32bits(c) != math.Float32bits(v) {
			return c
		}
	}
	return v
}

// ------------------------------------------------------------- memberships

// propMemb is one membership of a section that accepts a ref channel and a
// mixed carrier channel. channel 0 is LowCardRef, 1 is MixedLowCardVerbatim.
type propMemb struct {
	verbatim []byte
	params   []byte
	ref      uint64
	channel  int
}

// drawMembs draws 0..4 memberships over both channels, with duplicates
// admissible — aliasing is content and the form keeps it.
func drawMembs(rt *rapid.T, label string) (ms []propMemb) {
	n := rapid.IntRange(0, 4).Draw(rt, label+"Count")
	ms = make([]propMemb, 0, n)
	for range n {
		m := propMemb{channel: rapid.IntRange(0, 1).Draw(rt, label+"Channel")}
		switch m.channel {
		case 0:
			m.ref = drawU64(rt, label+"Ref")
		default:
			m.verbatim = drawBytes(rt, label+"Verbatim")
			m.params = drawBytes(rt, label+"Params")
		}
		ms = append(ms, m)
	}
	return
}

// mutateMemb changes one membership so the bytes must move.
func mutateMemb(m propMemb) propMemb {
	if m.channel == 0 {
		m.ref ^= 1
		return m
	}
	m.verbatim = append(append([]byte(nil), m.verbatim...), 0x7f)
	return m
}

// ------------------------------------------------------------------ test_table

type ttGeoAttr struct {
	membs    []propMemb
	lat, lng float32
	h1, h2   uint64
}

type ttTextAttr struct {
	text  string
	lens  []uint32
	words []string
	membs []propMemb
}

type ttEntity struct {
	ts   time.Time
	proc []time.Time
	geo  []ttGeoAttr
	text []ttTextAttr
	id   uint64
}

func drawTimestamp(rt *rapid.T, label string) time.Time {
	return time.Unix(int64(rapid.IntRange(0, 2_000_000_000).Draw(rt, label)), 0).UTC()
}

func drawTestTableBatch(rt *rapid.T) (ents []ttEntity) {
	n := rapid.IntRange(1, 8).Draw(rt, "entities")
	ents = make([]ttEntity, 0, n)
	for range n {
		e := ttEntity{id: drawU64(rt, "id"), ts: drawTimestamp(rt, "ts")}
		for range rapid.IntRange(0, 3).Draw(rt, "procCount") {
			e.proc = append(e.proc, drawTimestamp(rt, "proc"))
		}
		for range rapid.IntRange(0, 5).Draw(rt, "geoCount") {
			e.geo = append(e.geo, ttGeoAttr{
				lat: drawF32(rt, "lat"), lng: drawF32(rt, "lng"),
				h1: drawU64(rt, "h1"), h2: drawU64(rt, "h2"),
				membs: drawMembs(rt, "geoMemb"),
			})
		}
		for range rapid.IntRange(0, 5).Draw(rt, "textCount") {
			a := ttTextAttr{text: drawString(rt, "text"), membs: drawMembs(rt, "textMemb")}
			for range rapid.IntRange(0, 3).Draw(rt, "wordCount") {
				a.lens = append(a.lens, rapid.SampledFrom(propU32).Draw(rt, "wordLength"))
				a.words = append(a.words, drawString(rt, "word"))
			}
			e.text = append(e.text, a)
		}
		ents = append(ents, e)
	}
	return
}

func encodeTestTableBatch(t require.TestingT, ents []ttEntity, sh shuffle) (raw []byte) {
	dml := NewInEntityTestTable(memory.DefaultAllocator, 128)
	secGeo := dml.GetSectionGeo()
	secText := dml.GetSectionText()
	for i := range ents {
		e := &ents[i]
		ent := dml.BeginEntity()
		ent.SetId(e.id)
		ent.SetTimestamp(e.ts, e.proc)
		for _, a := range sh(len(e.geo)) {
			g := &e.geo[a]
			at := secGeo.BeginAttribute(g.lat, g.lng, g.h1, g.h2)
			for _, k := range sh(len(g.membs)) {
				m := g.membs[k]
				if m.channel == 0 {
					at.AddMembershipLowCardRefP(m.ref)
				} else {
					at.AddMembershipMixedLowCardVerbatimP(m.verbatim, m.params)
				}
			}
			at.EndAttributeP()
		}
		for _, a := range sh(len(e.text)) {
			x := &e.text[a]
			at := secText.BeginAttribute(x.text)
			// The `h` columns are co-containers and their order is content, so
			// they are never shuffled.
			for j := range x.words {
				at.AddToCoContainersP(x.lens[j], x.words[j])
			}
			for _, k := range sh(len(x.membs)) {
				m := x.membs[k]
				if m.channel == 0 {
					at.AddMembershipLowCardRefP(m.ref)
				} else {
					at.AddMembershipMixedLowCardVerbatimP(m.verbatim, m.params)
				}
			}
			at.EndAttributeP()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	ra := NewReadAccessTestTable()
	loadBatch(t, dml.TransferRecords, ra.LoadFromRecord)
	enc, err := NewCanonWireEncoderTestTable(ra, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, enc.EncodeAll(&buf))
	return buf.Bytes()
}

func cloneTestTableBatch(ents []ttEntity) (out []ttEntity) {
	out = make([]ttEntity, len(ents))
	for i := range ents {
		e := ents[i]
		e.proc = append([]time.Time(nil), ents[i].proc...)
		e.geo = make([]ttGeoAttr, len(ents[i].geo))
		for j := range ents[i].geo {
			g := ents[i].geo[j]
			g.membs = append([]propMemb(nil), ents[i].geo[j].membs...)
			e.geo[j] = g
		}
		e.text = make([]ttTextAttr, len(ents[i].text))
		for j := range ents[i].text {
			x := ents[i].text[j]
			x.membs = append([]propMemb(nil), ents[i].text[j].membs...)
			x.lens = append([]uint32(nil), ents[i].text[j].lens...)
			x.words = append([]string(nil), ents[i].text[j].words...)
			e.text[j] = x
		}
		out[i] = e
	}
	return
}

func TestPropertyTestTableRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawTestTableBatch(rt)
		first := encodeTestTableBatch(rt, ents, identityOrder)
		n, err := cwruntime.VerifyCanonicalSequence(first)
		require.NoError(rt, err)
		require.Equal(rt, len(ents), n)

		dst := NewInEntityTestTable(memory.DefaultAllocator, 128)
		dec, err := NewCanonWireDecoderTestTable(dst, nil)
		require.NoError(rt, err)
		decoded, err := dec.DecodeAll(first)
		require.NoError(rt, err)
		require.Equal(rt, len(ents), decoded)

		ra := NewReadAccessTestTable()
		loadBatch(rt, dst.TransferRecords, ra.LoadFromRecord)
		enc, err := NewCanonWireEncoderTestTable(ra, nil)
		require.NoError(rt, err)
		var second bytes.Buffer
		require.NoError(rt, enc.EncodeAll(&second))
		require.Equal(rt, hex.EncodeToString(first), hex.EncodeToString(second.Bytes()))
	})
}

func TestPropertyTestTablePermutationInvariance(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawTestTableBatch(rt)
		a := encodeTestTableBatch(rt, ents, shuffleFromSeed(rapid.Uint64().Draw(rt, "seedA")))
		b := encodeTestTableBatch(rt, ents, shuffleFromSeed(rapid.Uint64().Draw(rt, "seedB")))
		require.Equal(rt, hex.EncodeToString(a), hex.EncodeToString(b))
	})
}

func TestPropertyTestTableContentSensitivity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawTestTableBatch(rt)
		// The mutation sites of this batch, as closures over a clone.
		type site func(out []ttEntity)
		sites := make([]site, 0, 8)
		for i := range ents {
			for j := range ents[i].geo {
				sites = append(sites, func(out []ttEntity) { out[i].geo[j].lat = otherF32(out[i].geo[j].lat) })
				sites = append(sites, func(out []ttEntity) { out[i].geo[j].h1 ^= 1 })
				for k := range ents[i].geo[j].membs {
					sites = append(sites, func(out []ttEntity) {
						out[i].geo[j].membs[k] = mutateMemb(out[i].geo[j].membs[k])
					})
				}
			}
			for j := range ents[i].text {
				sites = append(sites, func(out []ttEntity) { out[i].text[j].text += "!" })
				for k := range ents[i].text[j].words {
					sites = append(sites, func(out []ttEntity) { out[i].text[j].words[k] += "!" })
					sites = append(sites, func(out []ttEntity) { out[i].text[j].lens[k] ^= 1 })
				}
				for k := range ents[i].text[j].membs {
					sites = append(sites, func(out []ttEntity) {
						out[i].text[j].membs[k] = mutateMemb(out[i].text[j].membs[k])
					})
				}
			}
		}
		if len(sites) == 0 {
			rt.Skip("the drawn batch carries no attribute to mutate")
		}
		pick := rapid.IntRange(0, len(sites)-1).Draw(rt, "site")
		mutated := cloneTestTableBatch(ents)
		sites[pick](mutated)
		require.NotEqual(rt,
			hex.EncodeToString(encodeTestTableBatch(rt, ents, identityOrder)),
			hex.EncodeToString(encodeTestTableBatch(rt, mutated, identityOrder)))
	})
}

// ------------------------------------------------------------------------ place

// plPair is one attribute of the co-section group: geo and h3 are written in
// step, so their attribute counts agree and the pairing is what the slot
// preserves.
type plPair struct {
	geoMembs []uint64
	h3Membs  []uint64
	lat, lng float32
	cell     uint64
}

type plTagAttr struct {
	tags  []string
	ids   []uint64
	membs [][]byte
}

type plEntity struct {
	pairs []plPair
	tags  []plTagAttr
	id    uint64
}

func drawPlaceBatch(rt *rapid.T) (ents []plEntity) {
	n := rapid.IntRange(1, 8).Draw(rt, "entities")
	ents = make([]plEntity, 0, n)
	for range n {
		e := plEntity{id: drawU64(rt, "id")}
		for range rapid.IntRange(0, 5).Draw(rt, "pairCount") {
			p := plPair{lat: drawF32(rt, "lat"), lng: drawF32(rt, "lng"), cell: drawU64(rt, "cell")}
			for range rapid.IntRange(0, 4).Draw(rt, "geoMembCount") {
				p.geoMembs = append(p.geoMembs, drawU64(rt, "geoRef"))
			}
			for range rapid.IntRange(0, 4).Draw(rt, "h3MembCount") {
				p.h3Membs = append(p.h3Membs, drawU64(rt, "h3Ref"))
			}
			e.pairs = append(e.pairs, p)
		}
		for range rapid.IntRange(0, 5).Draw(rt, "tagCount") {
			a := plTagAttr{}
			for range rapid.IntRange(0, 4).Draw(rt, "tagElems") {
				a.tags = append(a.tags, drawString(rt, "tag"))
				a.ids = append(a.ids, drawU64(rt, "tagId"))
			}
			for range rapid.IntRange(0, 4).Draw(rt, "tagMembCount") {
				a.membs = append(a.membs, drawBytes(rt, "tagMemb"))
			}
			e.tags = append(e.tags, a)
		}
		ents = append(ents, e)
	}
	return
}

func encodePlaceBatch(t require.TestingT, ents []plEntity, sh shuffle) (raw []byte) {
	dml := NewInEntityPlace(memory.DefaultAllocator, 128)
	secGeo := dml.GetSectionGeo()
	secH3 := dml.GetSectionH3()
	secTags := dml.GetSectionTags()
	for i := range ents {
		e := &ents[i]
		ent := dml.BeginEntity()
		ent.SetId(e.id)
		// One order for both members of the co-section group: they are one
		// atomic unit and the DML pairs them by attribute index.
		order := sh(len(e.pairs))
		for _, a := range order {
			p := &e.pairs[a]
			at := secGeo.BeginAttribute(p.lat, p.lng)
			for _, k := range sh(len(p.geoMembs)) {
				at.AddMembershipLowCardRefP(p.geoMembs[k])
			}
			at.EndAttributeP()
			ah := secH3.BeginAttribute(p.cell)
			for _, k := range sh(len(p.h3Membs)) {
				ah.AddMembershipLowCardRefP(p.h3Membs[k])
			}
			ah.EndAttributeP()
		}
		for _, a := range sh(len(e.tags)) {
			x := &e.tags[a]
			at := secTags.BeginAttribute()
			// The `h` column keeps its order; the `m` column beside it is a
			// co-container, so its elements are permuted across the positions
			// rather than sorted here — which is exactly the freedom the form
			// says a set has.
			ids := sh(len(x.ids))
			for j := range x.tags {
				at.AddToCoContainersP(x.tags[j], x.ids[ids[j]])
			}
			for _, k := range sh(len(x.membs)) {
				at.AddMembershipLowCardVerbatimP(x.membs[k])
			}
			at.EndAttributeP()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	ra := NewReadAccessPlace()
	loadBatch(t, dml.TransferRecords, ra.LoadFromRecord)
	enc, err := NewCanonWireEncoderPlace(ra, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, enc.EncodeAll(&buf))
	return buf.Bytes()
}

func clonePlaceBatch(ents []plEntity) (out []plEntity) {
	out = make([]plEntity, len(ents))
	for i := range ents {
		e := ents[i]
		e.pairs = make([]plPair, len(ents[i].pairs))
		for j := range ents[i].pairs {
			p := ents[i].pairs[j]
			p.geoMembs = append([]uint64(nil), ents[i].pairs[j].geoMembs...)
			p.h3Membs = append([]uint64(nil), ents[i].pairs[j].h3Membs...)
			e.pairs[j] = p
		}
		e.tags = make([]plTagAttr, len(ents[i].tags))
		for j := range ents[i].tags {
			x := ents[i].tags[j]
			x.tags = append([]string(nil), ents[i].tags[j].tags...)
			x.ids = append([]uint64(nil), ents[i].tags[j].ids...)
			x.membs = make([][]byte, len(ents[i].tags[j].membs))
			for k := range ents[i].tags[j].membs {
				x.membs[k] = append([]byte(nil), ents[i].tags[j].membs[k]...)
			}
			e.tags[j] = x
		}
		out[i] = e
	}
	return
}

func TestPropertyPlaceRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawPlaceBatch(rt)
		first := encodePlaceBatch(rt, ents, identityOrder)
		n, err := cwruntime.VerifyCanonicalSequence(first)
		require.NoError(rt, err)
		require.Equal(rt, len(ents), n)

		dst := NewInEntityPlace(memory.DefaultAllocator, 128)
		dec, err := NewCanonWireDecoderPlace(dst, nil)
		require.NoError(rt, err)
		decoded, err := dec.DecodeAll(first)
		require.NoError(rt, err)
		require.Equal(rt, len(ents), decoded)

		ra := NewReadAccessPlace()
		loadBatch(rt, dst.TransferRecords, ra.LoadFromRecord)
		enc, err := NewCanonWireEncoderPlace(ra, nil)
		require.NoError(rt, err)
		var second bytes.Buffer
		require.NoError(rt, enc.EncodeAll(&second))
		require.Equal(rt, hex.EncodeToString(first), hex.EncodeToString(second.Bytes()))
	})
}

func TestPropertyPlacePermutationInvariance(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawPlaceBatch(rt)
		a := encodePlaceBatch(rt, ents, shuffleFromSeed(rapid.Uint64().Draw(rt, "seedA")))
		b := encodePlaceBatch(rt, ents, shuffleFromSeed(rapid.Uint64().Draw(rt, "seedB")))
		require.Equal(rt, hex.EncodeToString(a), hex.EncodeToString(b))
	})
}

func TestPropertyPlaceContentSensitivity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawPlaceBatch(rt)
		type site func(out []plEntity)
		sites := make([]site, 0, 8)
		for i := range ents {
			for j := range ents[i].pairs {
				sites = append(sites, func(out []plEntity) { out[i].pairs[j].lat = otherF32(out[i].pairs[j].lat) })
				sites = append(sites, func(out []plEntity) { out[i].pairs[j].cell ^= 1 })
				for k := range ents[i].pairs[j].geoMembs {
					sites = append(sites, func(out []plEntity) { out[i].pairs[j].geoMembs[k] ^= 1 })
				}
				for k := range ents[i].pairs[j].h3Membs {
					sites = append(sites, func(out []plEntity) { out[i].pairs[j].h3Membs[k] ^= 1 })
				}
			}
			for j := range ents[i].tags {
				for k := range ents[i].tags[j].tags {
					sites = append(sites, func(out []plEntity) { out[i].tags[j].tags[k] += "!" })
					// The set element: a change here has to move the bytes, or
					// the `m` column would be carrying no information at all.
					sites = append(sites, func(out []plEntity) { out[i].tags[j].ids[k] ^= 1 })
				}
				for k := range ents[i].tags[j].membs {
					sites = append(sites, func(out []plEntity) {
						out[i].tags[j].membs[k] = append(append([]byte(nil), out[i].tags[j].membs[k]...), 0x7f)
					})
				}
			}
		}
		if len(sites) == 0 {
			rt.Skip("the drawn batch carries no attribute to mutate")
		}
		pick := rapid.IntRange(0, len(sites)-1).Draw(rt, "site")
		mutated := clonePlaceBatch(ents)
		sites[pick](mutated)
		require.NotEqual(rt,
			hex.EncodeToString(encodePlaceBatch(rt, ents, identityOrder)),
			hex.EncodeToString(encodePlaceBatch(rt, mutated, identityOrder)))
	})
}

// ------------------------------------------------------------------------- json

// jsAttr is one attribute of the json table. kind selects the section; the
// value fields the section does not have stay zero. Sections 0..3 are the
// value-less ones that share the empty signature, 4 and 5 share `s`.
type jsAttr struct {
	str     string
	membs   []propMemb
	f64     float64
	i64     int64
	kind    int
	boolean bool
}

const (
	jsNull = iota
	jsUndefined
	jsEmptyObject
	jsEmptyArray
	jsString
	jsSymbol
	jsBool
	jsFloat64
	jsInt64
	jsKinds
)

type jsEntity struct {
	id    []byte
	attrs []jsAttr
}

// drawJsonMembs draws the one channel every json section accepts.
func drawJsonMembs(rt *rapid.T) (ms []propMemb) {
	n := rapid.IntRange(0, 4).Draw(rt, "jsonMembCount")
	ms = make([]propMemb, 0, n)
	for range n {
		ms = append(ms, propMemb{
			channel:  1,
			verbatim: drawBytes(rt, "jsonMembVerbatim"),
			params:   drawBytes(rt, "jsonMembParams"),
		})
	}
	return
}

func drawJsonBatch(rt *rapid.T) (ents []jsEntity) {
	n := rapid.IntRange(1, 8).Draw(rt, "entities")
	ents = make([]jsEntity, 0, n)
	for range n {
		e := jsEntity{id: drawBytes(rt, "id")}
		for range rapid.IntRange(0, 5).Draw(rt, "attrCount") {
			a := jsAttr{kind: rapid.IntRange(0, jsKinds-1).Draw(rt, "kind"), membs: drawJsonMembs(rt)}
			switch a.kind {
			case jsString, jsSymbol:
				a.str = drawString(rt, "value")
			case jsBool:
				a.boolean = rapid.Bool().Draw(rt, "value")
			case jsFloat64:
				a.f64 = rapid.SampledFrom(propF64).Draw(rt, "value")
			case jsInt64:
				a.i64 = rapid.SampledFrom(propI64).Draw(rt, "value")
			}
			e.attrs = append(e.attrs, a)
		}
		ents = append(ents, e)
	}
	return
}

// jsonAdder is the one membership call every json section takes.
type jsonAdder interface {
	AddMembershipMixedLowCardVerbatimP(lmv []byte, mvhp []byte)
	EndAttributeP()
}

func addJsonMembs(at jsonAdder, membs []propMemb, sh shuffle) {
	for _, k := range sh(len(membs)) {
		at.AddMembershipMixedLowCardVerbatimP(membs[k].verbatim, membs[k].params)
	}
	at.EndAttributeP()
}

func encodeJsonBatch(t require.TestingT, ents []jsEntity, sh shuffle) (raw []byte) {
	dml := NewInEntityJson(memory.DefaultAllocator, 128)
	secs := []func(a *jsAttr) jsonAdder{
		func(a *jsAttr) jsonAdder { return dml.GetSectionNull().BeginAttribute() },
		func(a *jsAttr) jsonAdder { return dml.GetSectionUndefined().BeginAttribute() },
		func(a *jsAttr) jsonAdder { return dml.GetSectionEmptyObject().BeginAttribute() },
		func(a *jsAttr) jsonAdder { return dml.GetSectionEmptyArray().BeginAttribute() },
		func(a *jsAttr) jsonAdder { return dml.GetSectionString().BeginAttribute(a.str) },
		func(a *jsAttr) jsonAdder { return dml.GetSectionSymbol().BeginAttribute(a.str) },
		func(a *jsAttr) jsonAdder { return dml.GetSectionBool().BeginAttribute(a.boolean) },
		func(a *jsAttr) jsonAdder { return dml.GetSectionFloat64().BeginAttribute(a.f64) },
		func(a *jsAttr) jsonAdder { return dml.GetSectionInt64().BeginAttribute(a.i64) },
	}
	for i := range ents {
		e := &ents[i]
		ent := dml.BeginEntity()
		ent.SetId(e.id)
		for _, a := range sh(len(e.attrs)) {
			at := &e.attrs[a]
			addJsonMembs(secs[at.kind](at), at.membs, sh)
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	ra := NewReadAccessJson()
	loadBatch(t, dml.TransferRecords, ra.LoadFromRecord)
	enc, err := NewCanonWireEncoderJson(ra, CanonWireOrdinalTaggerJson{})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, enc.EncodeAll(&buf))
	return buf.Bytes()
}

func cloneJsonBatch(ents []jsEntity) (out []jsEntity) {
	out = make([]jsEntity, len(ents))
	for i := range ents {
		e := ents[i]
		e.id = append([]byte(nil), ents[i].id...)
		e.attrs = make([]jsAttr, len(ents[i].attrs))
		for j := range ents[i].attrs {
			a := ents[i].attrs[j]
			a.membs = append([]propMemb(nil), ents[i].attrs[j].membs...)
			e.attrs[j] = a
		}
		out[i] = e
	}
	return
}

func TestPropertyJsonRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawJsonBatch(rt)
		first := encodeJsonBatch(rt, ents, identityOrder)
		n, err := cwruntime.VerifyCanonicalSequence(first)
		require.NoError(rt, err)
		require.Equal(rt, len(ents), n)

		dst := NewInEntityJson(memory.DefaultAllocator, 128)
		dec, err := NewCanonWireDecoderJson(dst, CanonWireOrdinalDispatcherJson{})
		require.NoError(rt, err)
		decoded, err := dec.DecodeAll(first)
		require.NoError(rt, err)
		require.Equal(rt, len(ents), decoded)

		ra := NewReadAccessJson()
		loadBatch(rt, dst.TransferRecords, ra.LoadFromRecord)
		enc, err := NewCanonWireEncoderJson(ra, CanonWireOrdinalTaggerJson{})
		require.NoError(rt, err)
		var second bytes.Buffer
		require.NoError(rt, enc.EncodeAll(&second))
		require.Equal(rt, hex.EncodeToString(first), hex.EncodeToString(second.Bytes()))
	})
}

func TestPropertyJsonPermutationInvariance(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawJsonBatch(rt)
		a := encodeJsonBatch(rt, ents, shuffleFromSeed(rapid.Uint64().Draw(rt, "seedA")))
		b := encodeJsonBatch(rt, ents, shuffleFromSeed(rapid.Uint64().Draw(rt, "seedB")))
		require.Equal(rt, hex.EncodeToString(a), hex.EncodeToString(b))
	})
}

func TestPropertyJsonContentSensitivity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ents := drawJsonBatch(rt)
		type site func(out []jsEntity)
		sites := make([]site, 0, 8)
		for i := range ents {
			for j := range ents[i].attrs {
				// The section itself is content: the ordinal tagger writes the
				// slot's position in its ambiguity set, so moving an attribute
				// from null to undefined has to move the bytes.
				sites = append(sites, func(out []jsEntity) {
					out[i].attrs[j].kind = (out[i].attrs[j].kind + 1) % 4
				})
				switch ents[i].attrs[j].kind {
				case jsString, jsSymbol:
					sites = append(sites, func(out []jsEntity) { out[i].attrs[j].str += "!" })
				case jsBool:
					sites = append(sites, func(out []jsEntity) { out[i].attrs[j].boolean = !out[i].attrs[j].boolean })
				case jsInt64:
					sites = append(sites, func(out []jsEntity) { out[i].attrs[j].i64 ^= 1 })
				}
				for k := range ents[i].attrs[j].membs {
					sites = append(sites, func(out []jsEntity) {
						out[i].attrs[j].membs[k] = mutateMemb(out[i].attrs[j].membs[k])
					})
				}
			}
		}
		if len(sites) == 0 {
			rt.Skip("the drawn batch carries no attribute to mutate")
		}
		pick := rapid.IntRange(0, len(sites)-1).Draw(rt, "site")
		mutated := cloneJsonBatch(ents)
		sites[pick](mutated)
		require.NotEqual(rt,
			hex.EncodeToString(encodeJsonBatch(rt, ents, identityOrder)),
			hex.EncodeToString(encodeJsonBatch(rt, mutated, identityOrder)))
	})
}
