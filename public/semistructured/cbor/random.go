package cbor

import (
	"hash"
	"math/bits"
	"math/rand/v2"
	"strings"
	"unicode/utf8"

	"github.com/zeebo/xxh3"
)

type Generator struct {
	Hasher             hash.Hash64
	Enc                *Encoder
	rand               *rand.Rand
	stringGenerators   []func() string
	MaxNestingLevel    int
	MaxTotalPrimitives int
	maxStringLength    int
	seed               int64
	nPrimitives        int
}

const maxByteSliceLength = 4 * 1024

// randomWords is a deliberately small, self-contained corpus. The encoder only
// observes byte length and UTF-8 validity, so lexical variety beyond this buys
// nothing; it exists to keep generated strings distinguishable when inspected
// by hand.
var randomWords = [...]string{
	"alpha", "anchor", "arena", "beacon", "bridge", "buffer",
	"cadence", "canopy", "cipher", "cluster", "column", "cursor",
	"delta", "digest", "domain", "ember", "envelope", "fabric",
	"filter", "forest", "gadget", "gateway", "granite", "harbor",
	"header", "index", "island", "kernel", "lattice", "ledger",
	"marker", "meadow", "module", "nested", "nozzle", "opaque",
	"orbit", "packet", "parcel", "pebble", "pointer", "quarry",
	"record", "region", "ripple", "sample", "scalar", "segment",
	"shuttle", "signal", "slice", "socket", "stream", "tangent",
	"tessera", "thicket", "token", "vector", "vertex", "window",
}

// sampleRunes spans all four UTF-8 encoded widths.
var sampleRunes = [...]rune{
	'a', 'q', 'z', '7', '~',
	'ä', 'ß', 'π', 'ж',
	'漢', '€', '↦',
	'🚀', '🌍', '𝄞',
}

// cborLengthBoundaries are the byte lengths either side of every head-size
// class change in encodeHead.
var cborLengthBoundaries = [...]int{0, 1, 23, 24, 255, 256, 65535, 65536}

func NewGenerator(w EncoderWriterI, randSeed int64) *Generator {
	const maxStringLength = 4 * 1024
	src := rand.NewPCG(uint64(randSeed), uint64(-randSeed))
	ra := rand.New(src)
	hasher := xxh3.New()
	r := &Generator{
		MaxNestingLevel:    8,
		MaxTotalPrimitives: 1000,
		Enc:                NewEncoder(w, hasher),
		rand:               ra,
		seed:               randSeed,
		Hasher:             hasher,
		nPrimitives:        0,
		maxStringLength:    maxStringLength,
		stringGenerators:   nil,
	}
	r.SetMaxStringLength(maxStringLength)
	return r
}

func (inst *Generator) SetMaxStringLength(n int) {
	if n < 0 {
		return
	}
	inst.maxStringLength = n

	if inst.stringGenerators == nil {
		sg := make([]func() string, 0, 12)
		add := func(weight int, f func() string) {
			for range weight {
				sg = append(sg, f)
			}
		}
		// The multiplicities weight the draw in generateString towards short,
		// word-shaped strings, leaving the wider shapes as a minority.
		add(3, inst.generateWord)
		add(3, inst.generateSentence)
		add(2, inst.generateASCIIRun)
		add(2, inst.generateUnicodeRun)
		add(1, inst.generateBoundaryRun)
		add(1, func() string { return "" })
		inst.stringGenerators = sg
	}
}

func (inst *Generator) Reset() {
	inst.Enc.Reset()
	inst.nPrimitives = 0
}

// randomLength draws a length that is log-uniform in magnitude: short values
// stay common while every head-size class of encodeHead still gets exercised.
func (inst *Generator) randomLength(maxLen int) int {
	if maxLen <= 0 {
		return 0
	}
	l := inst.rand.IntN(1 << inst.rand.IntN(bits.Len(uint(maxLen))+1))
	return min(l, maxLen)
}

func (inst *Generator) generateWord() string {
	return randomWords[inst.rand.IntN(len(randomWords))]
}

func (inst *Generator) generateSentence() string {
	var b strings.Builder
	n := 1 + inst.rand.IntN(12)
	for i := range n {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(inst.generateWord())
	}
	return b.String()
}

func (inst *Generator) generateASCIIRun() string {
	const first, last = ' ', '~'
	l := inst.randomLength(inst.maxStringLength)
	b := make([]byte, l)
	for i := range b {
		b[i] = byte(first + rune(inst.rand.IntN(last-first+1)))
	}
	return string(b)
}

func (inst *Generator) generateUnicodeRun() string {
	budget := inst.randomLength(inst.maxStringLength)
	var b strings.Builder
	b.Grow(budget)
	for {
		r := sampleRunes[inst.rand.IntN(len(sampleRunes))]
		if b.Len()+utf8.RuneLen(r) > budget {
			return b.String()
		}
		b.WriteRune(r)
	}
}

// generateBoundaryRun returns a run of single-byte runes whose length is
// exactly one of cborLengthBoundaries, so the head-size class changes are hit
// head-on rather than only by chance.
func (inst *Generator) generateBoundaryRun() string {
	l := cborLengthBoundaries[inst.rand.IntN(len(cborLengthBoundaries))]
	return strings.Repeat("x", min(l, inst.maxStringLength))
}

func (inst *Generator) generateString() string {
	sg := inst.stringGenerators
	s := sg[inst.rand.IntN(len(sg))]()
	if len(s) > inst.maxStringLength {
		// FIXME this may be slow, only the last codepoint may be torn...
		return strings.ToValidUTF8(s[:inst.maxStringLength], "-")
	}
	return s
}

func (inst *Generator) generateBytes() []byte {
	b := make([]byte, inst.randomLength(maxByteSliceLength))
	for i := range b {
		b[i] = byte(inst.rand.Uint64())
	}
	return b
}

func (inst *Generator) GenerateRandomCborScalar() (n int, err error) {
	var u int
	enc := inst.Enc
	ra := inst.rand
	switch ra.IntN(16) {
	case 0:
		u, err = enc.EncodeByteSlice(inst.generateBytes())
		n += u
		if err != nil {
			return
		}
	case 1, 2, 3, 4, 5, 6, 7, 8:
		u, err = enc.EncodeString(inst.generateString())
		n += u
		if err != nil {
			return
		}
	case 9:
		u, err = enc.EncodeBool(ra.Float32() < 0.5)
		n += u
		if err != nil {
			return
		}
	case 10, 11, 12:
		u, err = enc.EncodeInt(ra.Int64())
		n += u
		if err != nil {
			return
		}
	case 13, 14, 15, 16:
		u, err = enc.EncodeUint(ra.Uint64())
		n += u
		if err != nil {
			return
		}
	}
	return n, nil
}

func (inst *Generator) GenerateRandomCbor() (n int, err error) {
	return inst.generateRandomCbor(0)
}

func (inst *Generator) generateRandomCbor(level int) (n int, err error) {
	u := 0
	enc := inst.Enc
	maxLevel := inst.MaxNestingLevel
	if level >= maxLevel {
		inst.nPrimitives++
		u, err = inst.GenerateRandomCborScalar()
		n += u
		if err != nil {
			return
		}
		return
	}
	ra := inst.rand
	t := ra.IntN(12)
	maxL := inst.nextMaxContainerSize()
	if maxL <= 0 {
		t = 6 // generate scalars only
	}

	switch t {
	case 0:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeArrayDefinite(uint64(l))
		n += u
		if err != nil {
			return
		}
		for range l {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
	case 1:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeArrayIndefinite()
		n += u
		if err != nil {
			return
		}
		for range l {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		u, err = enc.EncodeBreak()
		n += u
	case 2:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeMapDefinite(uint64(l))
		n += u
		if err != nil {
			return
		}
		for i := 0; i < 2*l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
	case 3:
		l := ra.IntN(maxL)
		inst.nPrimitives += l
		u, err = enc.EncodeMapIndefinite()
		n += u
		if err != nil {
			return
		}
		for i := 0; i < 2*l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		u, err = enc.EncodeBreak()
		n += u
	case 4:
		u, err = enc.EncodeTagSmall(TagExpectConversionToBase64Std)
		n += u
		if err != nil {
			return
		}
		u, err = enc.EncodeByteSlice(inst.generateBytes())
		n += u
		if err != nil {
			return
		}
	default:
		u, err = inst.GenerateRandomCborScalar()
		n += u
		if err != nil {
			return
		}
	}
	return
}
func (inst *Generator) nextMaxContainerSize() (nMaxLength int) {
	d := inst.MaxTotalPrimitives - inst.nPrimitives
	if d <= 0 {
		return 0
	} else if d > 12 {
		return 12
	}
	return d
}
