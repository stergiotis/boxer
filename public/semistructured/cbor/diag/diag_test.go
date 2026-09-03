package diag_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fx "github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	boxercbor "github.com/stergiotis/boxer/public/semistructured/cbor"
	. "github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

// rfcVectors is RFC 8949 Appendix A, "Examples of Encoded CBOR Data Items",
// in the fxamacker spelling the compact mode reproduces. Where that
// spelling departs from the RFC's own column it is noted on the row:
// floats always carry a fraction or an exponent (1.0, 100000.0,
// 1.0e+300), bignums render as their integer, undefined and simple(n) by
// those names.
var rfcVectors = []struct {
	hex    string
	expect string
}{
	{"00", "0"},
	{"01", "1"},
	{"0a", "10"},
	{"17", "23"},
	{"1818", "24"},
	{"1819", "25"},
	{"1864", "100"},
	{"1903e8", "1000"},
	{"1a000f4240", "1000000"},
	{"1b000000e8d4a51000", "1000000000000"},
	{"1bffffffffffffffff", "18446744073709551615"},
	{"c249010000000000000000", "18446744073709551616"}, // tag 2 renders as the integer
	{"3bffffffffffffffff", "-18446744073709551616"},
	{"c349010000000000000000", "-18446744073709551617"}, // tag 3 renders as the integer
	{"20", "-1"},
	{"29", "-10"},
	{"3863", "-100"},
	{"3903e7", "-1000"},
	{"f90000", "0.0"},
	{"f98000", "-0.0"},
	{"f93c00", "1.0"},
	{"fb3ff199999999999a", "1.1"},
	{"f93e00", "1.5"},
	{"f97bff", "65504.0"},
	{"fa47c35000", "100000.0"},
	{"fa7f7fffff", "3.4028234663852886e+38"},
	{"fb7e37e43c8800759c", "1.0e+300"},
	{"f90001", "5.960464477539063e-8"},
	{"f90400", "0.00006103515625"},
	{"f9c400", "-4.0"},
	{"fbc010666666666666", "-4.1"},
	{"f97c00", "Infinity"},
	{"f97e00", "NaN"},
	{"f9fc00", "-Infinity"},
	{"fa7f800000", "Infinity"},
	{"fa7fc00000", "NaN"},
	{"faff800000", "-Infinity"},
	{"fb7ff0000000000000", "Infinity"},
	{"fb7ff8000000000000", "NaN"},
	{"fbfff0000000000000", "-Infinity"},
	{"f4", "false"},
	{"f5", "true"},
	{"f6", "null"},
	{"f7", "undefined"},
	{"f0", "simple(16)"},
	{"f8ff", "simple(255)"},
	{"c074323031332d30332d32315432303a30343a30305a", `0("2013-03-21T20:04:00Z")`},
	{"c11a514b67b0", "1(1363896240)"},
	{"c1fb41d452d9ec200000", "1(1363896240.5)"},
	{"d74401020304", "23(h'01020304')"},
	{"d818456449455446", "24(h'6449455446')"},
	{"d82076687474703a2f2f7777772e6578616d706c652e636f6d", `32("http://www.example.com")`},
	{"40", "h''"},
	{"4401020304", "h'01020304'"},
	{"60", `""`},
	{"6161", `"a"`},
	{"6449455446", `"IETF"`},
	{"62225c", `"\"\\"`},
	{"62c3bc", `"\u00fc"`}, // non-ASCII escapes as UTF-16
	{"63e6b0b4", `"\u6c34"`},
	{"64f0908591", `"\ud800\udd51"`},
	{"80", "[]"},
	{"83010203", "[1, 2, 3]"},
	{"8301820203820405", "[1, [2, 3], [4, 5]]"},
	{"98190102030405060708090a0b0c0d0e0f101112131415161718181819", "[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25]"},
	{"a0", "{}"},
	{"a201020304", "{1: 2, 3: 4}"},
	{"a26161016162820203", `{"a": 1, "b": [2, 3]}`},
	{"826161a161626163", `["a", {"b": "c"}]`},
	{"a56161614161626142616361436164614461656145", `{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E"}`},
	{"5f42010243030405ff", "(_ h'0102', h'030405')"},
	{"7f657374726561646d696e67ff", `(_ "strea", "ming")`},
	{"9fff", "[_ ]"},
	{"9f018202039f0405ffff", "[_ 1, [2, 3], [_ 4, 5]]"},
	{"9f01820203820405ff", "[_ 1, [2, 3], [4, 5]]"},
	{"83018202039f0405ff", "[1, [2, 3], [_ 4, 5]]"},
	{"83019f0203ff820405", "[1, [_ 2, 3], [4, 5]]"},
	{"9f0102030405060708090a0b0c0d0e0f101112131415161718181819ff", "[_ 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25]"},
	{"bf61610161629f0203ffff", `{_ "a": 1, "b": [_ 2, 3]}`},
	{"826161bf61626163ff", `["a", {_ "b": "c"}]`},
	{"bf6346756ef563416d7421ff", `{_ "Fun": true, "Amt": -2}`},
	// Beyond the RFC table: the chunkless indefinite strings and a tag in
	// a key position.
	{"5fff", "''_"},
	{"7fff", `""_`},
	{"a1c11a514b67b001", "{1(1363896240): 1}"},
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func fxMode(t *testing.T, precision bool, sequence bool) fx.DiagMode {
	t.Helper()
	dm, err := fx.DiagOptions{
		FloatPrecisionIndicator: precision,
		CBORSequence:            sequence,
		MaxNestedLevels:         65535,
		MaxArrayElements:        math.MaxInt32,
		MaxMapPairs:             math.MaxInt32,
	}.DiagMode()
	require.NoError(t, err)
	return dm
}

// checkSpans pins the output contract: spans contiguous from 0, each
// carrying exactly its text, together the whole string.
func checkSpans(t *testing.T, spans []Span, s string) {
	t.Helper()
	pos := 0
	for i, sp := range spans {
		require.Equal(t, pos, sp.Start, "span %d start", i)
		require.Equal(t, sp.Start+len(sp.Text), sp.Stop, "span %d stop", i)
		require.Equal(t, s[sp.Start:sp.Stop], sp.Text, "span %d text", i)
		pos = sp.Stop
	}
	require.Equal(t, len(s), pos, "spans cover the text")
	require.Equal(t, s, Text(spans))
}

// assertOracle renders b compact and requires the fxamacker rendering.
func assertOracle(t *testing.T, dm fx.DiagMode, b []byte, opts Options) {
	t.Helper()
	opts.Compact = true
	want, err := dm.Diagnose(b)
	require.NoError(t, err, "oracle refused %x", b)
	got, err := String(b, opts)
	require.NoError(t, err, "%x", b)
	require.Equal(t, want, got, "%x", b)
	spans, err := Print(b, opts)
	require.NoError(t, err)
	checkSpans(t, spans, got)
}

func TestRFCVectors(t *testing.T) {
	dm := fxMode(t, false, false)
	for _, v := range rfcVectors {
		b := unhex(t, v.hex)
		got, err := String(b, Options{Compact: true})
		require.NoError(t, err, v.hex)
		assert.Equal(t, v.expect, got, v.hex)
		assertOracle(t, dm, b, Options{})
	}
}

func TestRFCVectorsFloatPrecision(t *testing.T) {
	dm := fxMode(t, true, false)
	for _, v := range rfcVectors {
		assertOracle(t, dm, unhex(t, v.hex), Options{FloatPrecision: true})
	}
}

// TestFloat16Exhaustive walks every binary16 bit pattern through the
// hand-written widening against the library's.
func TestFloat16Exhaustive(t *testing.T) {
	dm := fxMode(t, true, false)
	b := []byte{0xf9, 0, 0}
	for v := range 1 << 16 {
		binary.BigEndian.PutUint16(b[1:], uint16(v))
		want, err := dm.Diagnose(b)
		require.NoError(t, err)
		got, err := String(b, Options{Compact: true, FloatPrecision: true})
		require.NoError(t, err)
		require.Equal(t, want, got, "%x", b)
	}
}

func TestOracleGeneratorCorpus(t *testing.T) {
	dm := fxMode(t, false, false)
	for seed := int64(1); seed <= 64; seed++ {
		buf := &bytes.Buffer{}
		gen := boxercbor.NewGenerator(buf, seed)
		gen.MaxNestingLevel = 5
		gen.MaxTotalPrimitives = 200
		gen.SetMaxStringLength(48)
		_, err := gen.GenerateRandomCbor()
		require.NoError(t, err)
		assertOracle(t, dm, buf.Bytes(), Options{})
	}
}

// randomValue draws a Go value the fxamacker encoder turns into the
// shapes the boxer generator does not produce: every float width, tags
// with arbitrary numbers, simple values, bignums, nested maps with
// integer keys.
func randomValue(t *rapid.T, depth int) any {
	kind := rapid.IntRange(0, 11).Draw(t, "kind")
	if depth <= 0 && kind > 8 {
		kind = 0
	}
	switch kind {
	case 0:
		return rapid.Int64().Draw(t, "i64")
	case 1:
		return rapid.Uint64().Draw(t, "u64")
	case 2:
		return rapid.Float32().Draw(t, "f32")
	case 3:
		return rapid.Float64().Draw(t, "f64")
	case 4:
		return rapid.String().Draw(t, "s")
	case 5:
		return rapid.SliceOfN(rapid.Byte(), 0, 40).Draw(t, "y")
	case 6:
		return rapid.Bool().Draw(t, "b")
	case 7:
		if rapid.Bool().Draw(t, "nil") {
			return nil
		}
		// 24…31 are reserved and the oracle's encoder refuses them.
		v := rapid.Uint8().Draw(t, "simple")
		if v >= 24 && v <= 31 {
			v += 8
		}
		return fx.SimpleValue(v)
	case 8:
		bi := new(big.Int).SetUint64(rapid.Uint64().Draw(t, "big"))
		bi.Lsh(bi, uint(rapid.IntRange(0, 70).Draw(t, "shift")))
		if rapid.Bool().Draw(t, "neg") {
			bi.Neg(bi)
		}
		return bi
	case 9:
		n := rapid.IntRange(0, 4).Draw(t, "n")
		arr := make([]any, n)
		for i := range arr {
			arr[i] = randomValue(t, depth-1)
		}
		return arr
	case 10:
		n := rapid.IntRange(0, 4).Draw(t, "n")
		m := make(map[int64]any, n)
		for range n {
			m[rapid.Int64().Draw(t, "k")] = randomValue(t, depth-1)
		}
		return m
	default:
		// Tags 2 and 3 are bignums to the oracle, which refuses them over
		// anything but a byte string.
		num := rapid.Uint64Range(4, 70000).Draw(t, "tag")
		return fx.Tag{Number: num, Content: randomValue(t, depth-1)}
	}
}

func TestOracleRapid(t *testing.T) {
	em, err := fx.EncOptions{ShortestFloat: fx.ShortestFloat16, BigIntConvert: fx.BigIntConvertNone}.EncMode()
	require.NoError(t, err)
	dm := fxMode(t, true, false)
	dmSeq := fxMode(t, true, true)
	rapid.Check(t, func(rt *rapid.T) {
		v := randomValue(rt, 4)
		b, err := em.Marshal(v)
		require.NoError(t, err)
		assertOracle(t, dm, b, Options{FloatPrecision: true})
		// The same item three times as a sequence.
		seq := append(append(append([]byte{}, b...), b...), b...)
		assertOracle(t, dmSeq, seq, Options{FloatPrecision: true, Sequence: true})
	})
}

// TestOracleCanonformGoldens renders the leeway canonical-form items the
// canonform package pins, which are the bytes the cbordiag widget shows
// first (ADR-0219 §SD5).
func TestOracleCanonformGoldens(t *testing.T) {
	dm := fxMode(t, false, false)
	files, err := filepath.Glob(filepath.Join("..", "..", "leeway", "canonform", "canonform_*_gold.out.txt"))
	require.NoError(t, err)
	n := 0
	for _, f := range files {
		fh, err := os.Open(f)
		require.NoError(t, err)
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) != 2 {
				continue
			}
			b, err := hex.DecodeString(fields[1])
			if err != nil || len(b) == 0 || fx.Wellformed(b) != nil {
				continue
			}
			assertOracle(t, dm, b, Options{})
			n++
		}
		require.NoError(t, sc.Err())
		_ = fh.Close()
	}
	require.Greater(t, n, 0, "no golden items found")
}

func TestKeysAreKeys(t *testing.T) {
	// {1: "a", "b": 2, 1(1363896240): 3}
	spans, err := Print(unhex(t, "a3016161616202c11a514b67b003"), Options{Compact: true})
	require.NoError(t, err)
	var keys, values []string
	for _, sp := range spans {
		switch sp.Category {
		case CategoryKey:
			keys = append(keys, sp.Text)
		case CategoryNumber, CategoryText:
			values = append(values, sp.Text)
		}
	}
	assert.Equal(t, []string{"1", `"b"`, "1363896240"}, keys)
	assert.Equal(t, []string{`"a"`, "2", "3"}, values)
}

func TestDegradation(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want error
	}{
		{"truncated array", "830102", ErrTruncated},
		{"truncated head", "1b0000", ErrTruncated},
		{"truncated text", "6449", ErrTruncated},
		{"reserved info", "1c", ErrReservedInfo},
		{"break outside", "8201ff", ErrUnexpectedBreak},
		{"indefinite uint", "1f", ErrIndefiniteHead},
		{"chunk type", "5f6161ff", ErrChunkType},
		{"invalid utf8", "62c328", ErrInvalidUTF8},
		{"trailing", "0102", ErrTrailingBytes},
		{"empty", "", ErrEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := unhex(t, tc.hex)
			for _, compact := range []bool{false, true} {
				s, err := String(b, Options{Compact: compact})
				require.ErrorIs(t, err, tc.want)
				assert.Contains(t, s, "/ error: ")
				spans, err2 := Print(b, Options{Compact: compact})
				require.ErrorIs(t, err2, tc.want)
				checkSpans(t, spans, s)
				var hasErr bool
				for _, sp := range spans {
					hasErr = hasErr || sp.Category == CategoryError
				}
				assert.True(t, hasErr)
			}
		})
	}
	// The rendered prefix survives and the remainder follows as hex.
	s, err := String(unhex(t, "8301021c"), Options{Compact: true})
	require.ErrorIs(t, err, ErrReservedInfo)
	assert.True(t, strings.HasPrefix(s, "[1, 2"), s)
	assert.True(t, strings.HasSuffix(s, "h'1c'"), s)
}

func TestSequenceOffTrailingIsError(t *testing.T) {
	_, err := String(unhex(t, "0102"), Options{Compact: true})
	require.ErrorIs(t, err, ErrTrailingBytes)
	s, err := String(unhex(t, "0102"), Options{Compact: true, Sequence: true})
	require.NoError(t, err)
	assert.Equal(t, "1, 2", s)
	s, err = String(unhex(t, "0102"), Options{Sequence: true})
	require.NoError(t, err)
	assert.Equal(t, "1\n2", s)
}

func TestPrettyLayout(t *testing.T) {
	// Fits: stays on one line.
	s, err := String(unhex(t, "8301820203820405"), Options{})
	require.NoError(t, err)
	assert.Equal(t, "[1, [2, 3], [4, 5]]", s)
	// Forced narrow: outer breaks, inner arrays still fit their lines.
	s, err = String(unhex(t, "8301820203820405"), Options{Width: 12})
	require.NoError(t, err)
	assert.Equal(t, "[\n  1,\n  [2, 3],\n  [4, 5]\n]", s)
	// A map with a tag comment and a nested container that breaks.
	s, err = String(unhex(t, "a201616102d903e9a1011a514b67b0"), Options{Width: 20, TagComments: true, Indent: "\t"})
	require.NoError(t, err)
	assert.Equal(t, "{\n\t1: \"a\",\n\t2: 1001(/ time / {\n\t\t1: 1363896240\n\t})\n}", s)
	spans, err := Print(unhex(t, "a201616102d903e9a1011a514b67b0"), Options{Width: 20, TagComments: true, Indent: "\t"})
	require.NoError(t, err)
	checkSpans(t, spans, s)
}

func TestBytesFold(t *testing.T) {
	b := unhex(t, "4a00112233445566778899")
	s, err := String(b, Options{BytesFold: 4})
	require.NoError(t, err)
	assert.Equal(t, "h'\n  00112233\n  44556677\n  8899\n'", s)
	// Folding never touches compact mode.
	s, err = String(b, Options{BytesFold: 4, Compact: true})
	require.NoError(t, err)
	assert.Equal(t, "h'00112233445566778899'", s)
}

func TestAnnotate(t *testing.T) {
	hook := func(path []PathElem) string {
		if len(path) == 0 {
			return "entity"
		}
		last := path[len(path)-1]
		switch last.Kind {
		case PathElemIndex:
			if len(path) == 1 && last.Index == 0 {
				return "version"
			}
		case PathElemKey:
			return "slot " + string(last.Key[1:])
		case PathElemTag:
			return "tagged"
		}
		return ""
	}
	// [1, {}, {"f32": 10}]
	b := unhex(t, "8301a0a1636633320a")
	s, err := String(b, Options{Annotate: hook, Compact: true})
	require.NoError(t, err)
	assert.Equal(t, `[1 / version /, {}, {"f32": 10 / slot f32 /}] / entity /`, s)
	s, err = String(b, Options{Annotate: hook, Width: 12})
	require.NoError(t, err)
	assert.Equal(t, "[ / entity /\n  1 / version /,\n  {},\n  {\n    \"f32\": 10 / slot f32 /\n  }\n]", s)
}

func TestTagOverNonBytesBignum(t *testing.T) {
	// Tag 2 over a non-byte-string is not a bignum; it renders as a tag.
	s, err := String(unhex(t, "c201"), Options{Compact: true})
	require.NoError(t, err)
	assert.Equal(t, "2(1)", s)
}

func TestErrorWrapsSentinel(t *testing.T) {
	_, err := String(unhex(t, "83"), Options{})
	require.True(t, errors.Is(err, ErrTruncated))
}
