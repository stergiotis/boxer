package cbor

import (
	"bytes"
	"hash"
	"math/rand"

	"github.com/rs/zerolog/log"
	"github.com/zeebo/xxh3"
)

type Generator struct {
	MaxNestingLevel int
	Hasher          hash.Hash64
	Enc             *Encoder
	maxStringLength int
	rand            *rand.Rand
	seed            int64
	stringTemplate  []byte
	charset         string
	buf             *bytes.Buffer
}

func NewGenerator(w EncoderWriter, randSeed int64, charset string) *Generator {
	const maxStringLength = 1024 * 1024
	ra := rand.New(rand.NewSource(randSeed))
	b := bytes.NewBuffer(make([]byte, 0, maxStringLength))
	hasher := xxh3.New()
	r := &Generator{
		MaxNestingLevel: 8,
		Enc:             NewEncoder(w, hasher),
		rand:            ra,
		seed:            randSeed,
		stringTemplate:  b.Bytes(),
		Hasher:          hasher,
		charset:         charset,
		buf:             b,
	}
	r.SetMaxStringLength(maxStringLength)
	return r
}

func (inst *Generator) SetMaxStringLength(n int) {
	inst.maxStringLength = n
	charset := inst.charset
	l := len(charset)
	t := make([]rune, 0, l)
	ra := inst.rand
	b := inst.buf
	b.Reset()
	b.Grow(n)
	for _, c := range charset {
		t = append(t, c)
	}
	for b.Len() < n {
		_, err := b.WriteRune(t[ra.Intn(l)])
		if err != nil {
			log.Fatal().Err(err).Msg("unable to write to buffer")
		}
	}
}

func (inst *Generator) Reset() {
	inst.rand.Seed(inst.seed)
	inst.Enc.Reset()
}

func (inst *Generator) GenerateRandomCborScalar() (n int, err error) {
	var u int
	enc := inst.Enc
	maxStringLen := inst.maxStringLength
	b := inst.stringTemplate
	ra := inst.rand
	switch ra.Intn(5) {
	case 0:
		u, err = enc.EncodeByteSlice(b[:ra.Intn(maxStringLen)])
		n += u
		if err != nil {
			return
		}
		break
	case 1:
		u, err = enc.EncodeString(string(b[:ra.Intn(maxStringLen)]))
		n += u
		if err != nil {
			return
		}
		break
	case 2:
		u, err = enc.EncodeBool(ra.Float32() < 0.5)
		n += u
		if err != nil {
			return
		}
		break
	case 3:
		u, err = enc.EncodeInt(ra.Int63())
		n += u
		if err != nil {
			return
		}
		break
	case 4:
		u, err = enc.EncodeUint(ra.Uint64())
		n += u
		if err != nil {
			return
		}
		break
	}
	return n, nil
}

func (inst *Generator) GenerateRandomCbor() (n int, err error) {
	return inst.generateRandomCbor(0)
}

func (inst *Generator) generateRandomCbor(level int) (n int, err error) {
	u := 0
	enc := inst.Enc
	maxStringLen := inst.maxStringLength
	b := inst.stringTemplate
	maxLevel := inst.MaxNestingLevel
	if level >= maxLevel {
		u, err = inst.GenerateRandomCborScalar()
		n += u
		if err != nil {
			return
		}
		return
	}
	ra := inst.rand

	switch ra.Intn(12) {
	case 0:
		l := ra.Intn(12)
		u, err = enc.EncodeArrayDefinite(uint64(l))
		n += u
		if err != nil {
			return
		}
		for i := 0; i < l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		break
	case 1:
		l := ra.Intn(12)
		u, err = enc.EncodeArrayIndefinite()
		n += u
		if err != nil {
			return
		}
		for i := 0; i < l; i++ {
			u, err = inst.generateRandomCbor(level + 1)
			n += u
			if err != nil {
				return
			}
		}
		u, err = enc.EncodeBreak()
		n += u
		break
	case 2:
		l := ra.Intn(12)
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
		break
	case 3:
		l := ra.Intn(12)
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
		break
	case 4:
		u, err = enc.EncodeTagSmall(TagExpectConversionToBase64Std)
		n += u
		if err != nil {
			return
		}
		u, err = enc.EncodeByteSlice(b[:ra.Intn(maxStringLen)])
		n += u
		if err != nil {
			return
		}
		break
	default:
		u, err = inst.GenerateRandomCborScalar()
		n += u
		if err != nil {
			return
		}
	}
	return
}
