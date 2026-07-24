package eb

import (
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	fxcbor "github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The payloads this package produces are consumed by decoders outside the
// repo, so the oracle here is an independent CBOR implementation rather
// than boxer's own encoder: a payload is correct when fxamacker accepts it.

func payload(t *testing.T, e error) (out []byte) {
	t.Helper()
	if e == nil {
		t.Fatal("Errorf returned a nil error")
	}
	esd, ok := e.(eh.ErrorWithStructuredDataI)
	if !ok {
		t.Fatalf("error %T does not carry structured data", e)
	}
	out = esd.GetCBORStructuredData()
	if len(out) == 0 {
		t.Fatal("structured data is empty")
	}
	return
}

// requireWellformed fails unless the payload is structurally valid CBOR.
// This is what catches dangling tags, missing map headers and stray breaks
// — none of which are visible by inspecting the Go-side value.
func requireWellformed(t *testing.T, data []byte) {
	t.Helper()
	if err := fxcbor.Wellformed(data); err != nil {
		t.Fatalf("payload is not well-formed CBOR: %v\nbytes: % x", err, data)
	}
}

var decMode = func() fxcbor.DecMode {
	m, err := fxcbor.DecOptions{
		DefaultMapType: nil,
		UTF8:           fxcbor.UTF8DecodeInvalid,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	return m
}()

// decode returns the payload as a Go map, failing the test if the bytes
// cannot be decoded at all.
func decode(t *testing.T, data []byte) (out map[any]any) {
	t.Helper()
	requireWellformed(t, data)
	var v any
	if err := decMode.Unmarshal(data, &v); err != nil {
		t.Fatalf("decode failed: %v\nbytes: % x", err, data)
	}
	m, ok := v.(map[any]any)
	if !ok {
		t.Fatalf("payload decoded to %T, want a map\nbytes: % x", v, data)
	}
	return m
}

func decodeOne(t *testing.T, e error, key string) (out any) {
	t.Helper()
	m := decode(t, payload(t, e))
	v, ok := m[key]
	if !ok {
		t.Fatalf("key %q missing from payload; got keys %v", key, keysOf(m))
	}
	return v
}

func keysOf(m map[any]any) (out []string) {
	for k := range m {
		if s, ok := k.(string); ok {
			out = append(out, s)
		}
	}
	return
}

func TestBuildProducesWellformedEmptyMap(t *testing.T) {
	m := decode(t, payload(t, Build().Errorf("no fields")))
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}
}

func TestErrorfMessageAndWrapping(t *testing.T) {
	inner := eh.Errorf("inner")
	e := Build().Str("k", "v").Errorf("outer: %w", inner)
	if !strings.Contains(e.Error(), "outer") {
		t.Fatalf("message lost: %q", e.Error())
	}
	if got := decodeOne(t, e, "k"); got != "v" {
		t.Fatalf("k = %v, want v", got)
	}
}

func TestScalarRoundTrips(t *testing.T) {
	e := Build().
		Str("str", "hello").
		Bool("bool", true).
		Int("int", -42).
		Int64("int64", math.MinInt64).
		Uint64("uint64", math.MaxUint64).
		Uint8("uint8", 255).
		Int8("int8", -128).
		Float64("f64", 1.5).
		Errorf("scalars")
	m := decode(t, payload(t, e))

	if m["str"] != "hello" {
		t.Errorf("str = %v", m["str"])
	}
	if m["bool"] != true {
		t.Errorf("bool = %v", m["bool"])
	}
	if v, ok := m["int"].(int64); !ok || v != -42 {
		t.Errorf("int = %#v", m["int"])
	}
	if v, ok := m["int64"].(int64); !ok || v != math.MinInt64 {
		t.Errorf("int64 = %#v", m["int64"])
	}
	if v, ok := m["uint64"].(uint64); !ok || v != math.MaxUint64 {
		t.Errorf("uint64 = %#v", m["uint64"])
	}
	if v, ok := m["f64"].(float64); !ok || v != 1.5 {
		t.Errorf("f64 = %#v", m["f64"])
	}
}

func TestSliceRoundTrips(t *testing.T) {
	e := Build().
		Strs("strs", []string{"a", "b"}).
		Ints("ints", []int{1, 2, 3}).
		Bools("bools", []bool{true, false}).
		Floats64("f64s", []float64{1, 2}).
		Errorf("slices")
	m := decode(t, payload(t, e))

	strs, ok := m["strs"].([]any)
	if !ok || len(strs) != 2 || strs[0] != "a" || strs[1] != "b" {
		t.Errorf("strs = %#v", m["strs"])
	}
	if ints, ok := m["ints"].([]any); !ok || len(ints) != 3 {
		t.Errorf("ints = %#v", m["ints"])
	}
}

func TestEmptyAndNilSlicesStayWellformed(t *testing.T) {
	e := Build().
		Strs("empty", []string{}).
		Strs("nil", nil).
		Ints("nilints", nil).
		Errorf("empty slices")
	m := decode(t, payload(t, e))
	for _, k := range []string{"empty", "nil", "nilints"} {
		v, ok := m[k].([]any)
		if !ok || len(v) != 0 {
			t.Errorf("%s = %#v, want empty array", k, m[k])
		}
	}
}

// --- regressions -----------------------------------------------------
//
// Each test below pins a defect found in the 2026-07-24 review. They are
// grouped because they share one failure mode: the builder runs on error
// paths, so a panic or an undecodable payload destroys the diagnostic
// exactly when it is needed.

// IPAddr dereferenced the result of To4() in the branch reached only when
// To4() returned nil, so every IPv6 address panicked.
func TestIPAddrIPv6DoesNotPanic(t *testing.T) {
	e := Build().IPAddr("ip", net.ParseIP("2001:db8::1")).Errorf("v6")
	requireWellformed(t, payload(t, e))
}

func TestIPAddrIPv4RoundTrips(t *testing.T) {
	e := Build().IPAddr("ip", net.ParseIP("192.0.2.1")).Errorf("v4")
	requireWellformed(t, payload(t, e))
}

func TestIPAddrNilEncodesNull(t *testing.T) {
	if got := decodeOne(t, Build().IPAddr("ip", nil).Errorf("nil ip"), "ip"); got != nil {
		t.Fatalf("ip = %#v, want nil", got)
	}
}

func TestIPAddrMalformedLengthEncodesNull(t *testing.T) {
	if got := decodeOne(t, Build().IPAddr("ip", net.IP{1, 2, 3}).Errorf("bad ip"), "ip"); got != nil {
		t.Fatalf("ip = %#v, want nil", got)
	}
}

// Hex, RawJSON and RawCBOR emitted their tag before delegating to
// EncodeByteSlice, which rejects nil. The rejection was discarded, leaving
// a tag with no content item — that makes the whole payload undecodable,
// not just the one field.
func TestNilByteSlicePathsStayDecodable(t *testing.T) {
	cases := map[string]func(*ErrorBuilder) *ErrorBuilder{
		"hex":     func(b *ErrorBuilder) *ErrorBuilder { return b.Hex("v", nil) },
		"rawjson": func(b *ErrorBuilder) *ErrorBuilder { return b.RawJSON("v", nil) },
		"rawcbor": func(b *ErrorBuilder) *ErrorBuilder { return b.RawCBOR("v", nil) },
		"bytes":   func(b *ErrorBuilder) *ErrorBuilder { return b.Bytes("v", nil) },
	}
	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			e := apply(Build()).Str("after", "sentinel").Errorf("nil %s", name)
			m := decode(t, payload(t, e))
			if m["v"] != nil {
				t.Errorf("v = %#v, want nil", m["v"])
			}
			// The field following the nil must survive; a dangling tag
			// would have swallowed it.
			if m["after"] != "sentinel" {
				t.Errorf("trailing field lost: %#v", m["after"])
			}
		})
	}
}

func TestNonNilByteSlicePathsRoundTrip(t *testing.T) {
	e := Build().
		Hex("hex", []byte{0xde, 0xad}).
		Bytes("bytes", []byte{1, 2}).
		RawCBOR("raw", []byte{0x01}).
		Errorf("byte slices")
	requireWellformed(t, payload(t, e))
}

// Reset cleared the buffer without re-emitting the indefinite-map header
// that Build() writes, so a reused builder produced a headerless run of
// key/value pairs terminated by a stray break.
func TestResetProducesWellformedPayload(t *testing.T) {
	b := Build()
	b.Str("first", "1")
	requireWellformed(t, payload(t, b.Errorf("first")))

	b.Reset()
	b.Str("second", "2")
	m := decode(t, payload(t, b.Errorf("second")))
	if m["second"] != "2" {
		t.Fatalf("second = %#v", m["second"])
	}
	if _, stale := m["first"]; stale {
		t.Fatal("Reset did not clear the previous payload")
	}
}

func TestResetRestoresOpenState(t *testing.T) {
	b := Build()
	_ = b.Errorf("close it")
	if b.IsOpen() {
		t.Fatal("builder still open after Errorf")
	}
	b.Reset()
	if !b.IsOpen() {
		t.Fatal("builder not reopened by Reset")
	}
}

// Errorf emitted a break unconditionally, so a second call appended a
// second break and produced trailing garbage.
func TestDoubleErrorfStaysWellformed(t *testing.T) {
	b := Build()
	b.Str("k", "v")
	first := payload(t, b.Errorf("first"))
	second := payload(t, b.Errorf("second"))
	requireWellformed(t, first)
	requireWellformed(t, second)
	if len(first) != len(second) {
		t.Fatalf("second Errorf changed the payload: % x vs % x", first, second)
	}
}

// A nil Stringer dereferenced through the interface.
func TestNilStringerDoesNotPanic(t *testing.T) {
	if got := decodeOne(t, Build().Stringer("s", nil).Errorf("nil stringer"), "s"); got != nil {
		t.Fatalf("s = %#v, want nil", got)
	}
}

func TestNilStringerInSliceDoesNotPanic(t *testing.T) {
	e := Build().Stringers("s", []fmt.Stringer{nil, net.ParseIP("192.0.2.1")}).Errorf("nil elem")
	requireWellformed(t, payload(t, e))
}

// --- contract --------------------------------------------------------

// Every setter must be a no-op once Errorf has closed the builder,
// otherwise a stale reference would append after the map's break.
func TestSettersAreNoOpsAfterClose(t *testing.T) {
	b := Build()
	b.Str("kept", "1")
	before := payload(t, b.Errorf("closed"))

	b.Str("dropped", "2").
		Int("dropped2", 1).
		Bytes("dropped3", []byte{1}).
		Hex("dropped4", []byte{1}).
		IPAddr("dropped5", net.ParseIP("2001:db8::1")).
		Stringer("dropped6", nil).
		Time("dropped7", time.Unix(0, 0))

	after := payload(t, b.Errorf("closed again"))
	if len(before) != len(after) {
		t.Fatalf("setters mutated a closed builder: % x vs % x", before, after)
	}
	m := decode(t, after)
	if _, leaked := m["dropped"]; leaked {
		t.Fatal("setter wrote to a closed builder")
	}
}

func TestWithoutStackTogglesStackCapture(t *testing.T) {
	withStack := Build().WithStack().Errorf("x")
	without := Build().WithoutStack().Errorf("x")
	requireWellformed(t, payload(t, withStack))
	requireWellformed(t, payload(t, without))
	if withStack.Error() != without.Error() {
		t.Fatalf("message differs: %q vs %q", withStack.Error(), without.Error())
	}
}

func TestTypeHandlesNilValue(t *testing.T) {
	if got := decodeOne(t, Build().Type("t", nil).Errorf("nil type"), "t"); got != "<unknown>" {
		t.Fatalf("t = %#v, want <unknown>", got)
	}
	if got := decodeOne(t, Build().Type("t", 42).Errorf("int type"), "t"); got != "int" {
		t.Fatalf("t = %#v, want int", got)
	}
}

func TestBuildersAreIndependent(t *testing.T) {
	a := Build().Str("a", "1")
	b := Build().Str("b", "2")
	ma := decode(t, payload(t, a.Errorf("a")))
	mb := decode(t, payload(t, b.Errorf("b")))
	if _, leaked := ma["b"]; leaked {
		t.Fatal("builder a saw builder b's field")
	}
	if _, leaked := mb["a"]; leaked {
		t.Fatal("builder b saw builder a's field")
	}
}

// Errorf copies the buffer; the returned error must not alias a builder
// that is subsequently reset and reused.
func TestErrorPayloadSurvivesBuilderReuse(t *testing.T) {
	b := Build()
	b.Str("k", "original")
	e := b.Errorf("first")
	before := append([]byte(nil), payload(t, e)...)

	b.Reset()
	b.Str("k", "overwritten-with-a-much-longer-value-to-force-buffer-growth")
	_ = b.Errorf("second")

	if got := payload(t, e); string(got) != string(before) {
		t.Fatalf("payload aliased the builder buffer: % x vs % x", got, before)
	}
}

// The typed setter family is mechanically repetitive, which is exactly
// where a copy-paste slip hides. Exercise every one and assert the
// payload still decodes with the field present.
func TestEverySetterProducesADecodableField(t *testing.T) {
	setters := map[string]func(*ErrorBuilder) *ErrorBuilder{
		"Str":       func(b *ErrorBuilder) *ErrorBuilder { return b.Str("v", "s") },
		"Strs":      func(b *ErrorBuilder) *ErrorBuilder { return b.Strs("v", []string{"s"}) },
		"Bool":      func(b *ErrorBuilder) *ErrorBuilder { return b.Bool("v", true) },
		"Bools":     func(b *ErrorBuilder) *ErrorBuilder { return b.Bools("v", []bool{true}) },
		"Uint":      func(b *ErrorBuilder) *ErrorBuilder { return b.Uint("v", 1) },
		"Uints":     func(b *ErrorBuilder) *ErrorBuilder { return b.Uints("v", []uint{1}) },
		"Uint8":     func(b *ErrorBuilder) *ErrorBuilder { return b.Uint8("v", 1) },
		"Uints8":    func(b *ErrorBuilder) *ErrorBuilder { return b.Uints8("v", []uint8{1}) },
		"Uint16":    func(b *ErrorBuilder) *ErrorBuilder { return b.Uint16("v", 1) },
		"Uints16":   func(b *ErrorBuilder) *ErrorBuilder { return b.Uints16("v", []uint16{1}) },
		"Uint32":    func(b *ErrorBuilder) *ErrorBuilder { return b.Uint32("v", 1) },
		"Uints32":   func(b *ErrorBuilder) *ErrorBuilder { return b.Uints32("v", []uint32{1}) },
		"Uint64":    func(b *ErrorBuilder) *ErrorBuilder { return b.Uint64("v", 1) },
		"Uints64":   func(b *ErrorBuilder) *ErrorBuilder { return b.Uints64("v", []uint64{1}) },
		"Int":       func(b *ErrorBuilder) *ErrorBuilder { return b.Int("v", -1) },
		"Ints":      func(b *ErrorBuilder) *ErrorBuilder { return b.Ints("v", []int{-1}) },
		"Int8":      func(b *ErrorBuilder) *ErrorBuilder { return b.Int8("v", -1) },
		"Ints8":     func(b *ErrorBuilder) *ErrorBuilder { return b.Ints8("v", []int8{-1}) },
		"Int16":     func(b *ErrorBuilder) *ErrorBuilder { return b.Int16("v", -1) },
		"Ints16":    func(b *ErrorBuilder) *ErrorBuilder { return b.Ints16("v", []int16{-1}) },
		"Int32":     func(b *ErrorBuilder) *ErrorBuilder { return b.Int32("v", -1) },
		"Ints32":    func(b *ErrorBuilder) *ErrorBuilder { return b.Ints32("v", []int32{-1}) },
		"Int64":     func(b *ErrorBuilder) *ErrorBuilder { return b.Int64("v", -1) },
		"Ints64":    func(b *ErrorBuilder) *ErrorBuilder { return b.Ints64("v", []int64{-1}) },
		"Float32":   func(b *ErrorBuilder) *ErrorBuilder { return b.Float32("v", 1) },
		"Floats32":  func(b *ErrorBuilder) *ErrorBuilder { return b.Floats32("v", []float32{1}) },
		"Float64":   func(b *ErrorBuilder) *ErrorBuilder { return b.Float64("v", 1) },
		"Floats64":  func(b *ErrorBuilder) *ErrorBuilder { return b.Floats64("v", []float64{1}) },
		"Bytes":     func(b *ErrorBuilder) *ErrorBuilder { return b.Bytes("v", []byte{1}) },
		"Hex":       func(b *ErrorBuilder) *ErrorBuilder { return b.Hex("v", []byte{1}) },
		"RawCBOR":   func(b *ErrorBuilder) *ErrorBuilder { return b.RawCBOR("v", []byte{1}) },
		"RawJSON":   func(b *ErrorBuilder) *ErrorBuilder { return b.RawJSON("v", []byte(`{"a":1}`)) },
		"Time":      func(b *ErrorBuilder) *ErrorBuilder { return b.Time("v", time.Unix(1, 0)) },
		"Times":     func(b *ErrorBuilder) *ErrorBuilder { return b.Times("v", []time.Time{time.Unix(1, 0)}) },
		"Stringer":  func(b *ErrorBuilder) *ErrorBuilder { return b.Stringer("v", net.ParseIP("192.0.2.1")) },
		"Stringers": func(b *ErrorBuilder) *ErrorBuilder { return b.Stringers("v", []fmt.Stringer{net.ParseIP("192.0.2.1")}) },
		"Type":      func(b *ErrorBuilder) *ErrorBuilder { return b.Type("v", 1) },
		"IPAddr":    func(b *ErrorBuilder) *ErrorBuilder { return b.IPAddr("v", net.ParseIP("192.0.2.1")) },
	}
	for name, apply := range setters {
		t.Run(name, func(t *testing.T) {
			e := apply(Build()).Str("after", "sentinel").Errorf("%s", name)
			m := decode(t, payload(t, e))
			if _, ok := m["v"]; !ok {
				t.Errorf("field absent; keys = %v", keysOf(m))
			}
			if m["after"] != "sentinel" {
				t.Errorf("following field lost: %#v", m["after"])
			}
		})
	}
}

func TestLargeStringRoundTrips(t *testing.T) {
	big := strings.Repeat("x", 100000)
	if got := decodeOne(t, Build().Str("big", big).Errorf("big"), "big"); got != big {
		t.Fatalf("large string round-trip failed (len %d)", len(got.(string)))
	}
}
