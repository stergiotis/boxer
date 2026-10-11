package jsonshred

import (
	"encoding/json/jsontext"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestExplodeTheSkillsExample(t *testing.T) {
	attrs, err := Explode([]byte(`{"hostname":"server-alpha","tags":["production","eu-west"],"metrics":{"cpu":45.5,"mem":80,"error":null},"active":true}`), Options{})
	require.NoError(t, err)
	assert.Equal(t, []Attr{
		{Path: "/hostname", Params: nil, Section: SectionString, Value: "server-alpha"},
		{Path: "/tags/_", Params: []uint64{0}, Section: SectionString, Value: "production"},
		{Path: "/tags/_", Params: []uint64{1}, Section: SectionString, Value: "eu-west"},
		{Path: "/metrics/cpu", Params: nil, Section: SectionF64, Value: 45.5},
		{Path: "/metrics/mem", Params: nil, Section: SectionI64, Value: int64(80)},
		{Path: "/metrics/error", Params: nil, Section: SectionNull, Value: nil},
		{Path: "/active", Params: nil, Section: SectionBool, Value: true},
	}, attrs)
}

// Each fidelity rule of the package doc, as an input and the canonical
// document Compose rebuilds.
func TestRoundTripRules(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"sorted keys, no whitespace", `{ "b" : 1, "a" : [ 2 , 3 ] }`, `{"a":[2,3],"b":1}`},
		{"escaped keys", `{"a/b":1,"c~d":2,"_":3,"~n":4}`, `{"_":3,"a/b":1,"c~d":2,"~n":4}`},
		{"nested arrays", `[[1,[2]],[]]`, `[[1,[2]],[]]`},
		{"empty containers and null", `{"o":{},"a":[],"n":null,"s":""}`, `{"a":[],"n":null,"o":{},"s":""}`},
		{"integer beyond int64", `{"big":18446744073709551616}`, `{"big":18446744073709551616}`},
		{"a decimal float64 cannot hold", `0.12345678901234567890123`, `0.12345678901234567890123`},
		{"shortest float", `[1.0, 1e0, -0.0, 0.1, 1e21, 2.5e-7]`, `[1,1,-0,0.1,1e+21,2.5e-07]`},
		{"out of range float", `1e400`, `1e400`},
		{"root scalar", `"x"`, `"x"`},
		{"unicode", `{"k":"é\u0000"}`, `{"k":"é\u0000"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			attrs, err := Explode([]byte(c.in), Options{})
			require.NoError(t, err)
			got, err := Compose(attrs)
			require.NoError(t, err)
			assert.Equal(t, c.want, string(got))
			canon, err := Canonical([]byte(c.in))
			require.NoError(t, err)
			assert.Equal(t, c.want, string(canon), "Canonical agrees without the attributes")
		})
	}
}

func TestPathsAndSuffixes(t *testing.T) {
	attrs, err := Explode([]byte(`{"_":{"x":[[1]]},"n":18446744073709551616}`), Options{})
	require.NoError(t, err)
	assert.Equal(t, "/~2/x/_/_", attrs[0].Path)
	assert.Equal(t, []uint64{0, 0}, attrs[0].Params)
	assert.Equal(t, "/n~n", attrs[1].Path)
	assert.Equal(t, SectionString, attrs[1].Section)
	assert.Equal(t, "18446744073709551616", attrs[1].Value)
	assert.Equal(t, "a/b", UnescapeKey(EscapeKey("a/b")))
	assert.Equal(t, "~1", UnescapeKey(EscapeKey("~1")))
	assert.Equal(t, "_", UnescapeKey(EscapeKey("_")))
}

func TestPlainModeDropsTheValueless(t *testing.T) {
	attrs, err := Explode([]byte(`{"a":null,"b":{},"c":[],"d":1}`), Options{Plain: true})
	require.NoError(t, err)
	assert.Equal(t, []Attr{{Path: "/d", Section: SectionI64, Value: int64(1)}}, attrs)
}

func TestSymbolOnlyWhenHinted(t *testing.T) {
	enum := func(path string) bool { return path == "/state" }
	attrs, err := Explode([]byte(`{"state":"open","note":"open"}`), Options{Symbol: enum})
	require.NoError(t, err)
	assert.Equal(t, SectionSymbol, attrs[0].Section)
	assert.Equal(t, SectionString, attrs[1].Section, "the same value elsewhere is not a symbol")
	doc, err := Compose(attrs)
	require.NoError(t, err)
	assert.Equal(t, `{"note":"open","state":"open"}`, string(doc))
}

func TestRefusals(t *testing.T) {
	for _, in := range []string{`{"a":1,"a":2}`, `{"a":`, `[1,]`, `1 2`, ``, `{"a":1} x`} {
		_, err := Explode([]byte(in), Options{})
		assert.Error(t, err, "%q", in)
	}
	for _, attrs := range [][]Attr{
		{},
		{{Path: "/a", Section: SectionI64, Value: int64(1)}, {Path: "/a/b", Section: SectionI64, Value: int64(2)}},
		{{Path: "/a/_", Params: []uint64{1}, Section: SectionI64, Value: int64(1)}},
		{{Path: "/a/_", Section: SectionI64, Value: int64(1)}},
		{{Path: "/a", Params: []uint64{0}, Section: SectionI64, Value: int64(1)}},
		{{Path: "/a", Section: SectionI64, Value: "1"}},
		{{Path: "/a~n", Section: SectionString, Value: "nope"}},
		{{Path: "/a", Section: SectionI64, Value: int64(1)}, {Path: "/a", Section: SectionI64, Value: int64(1)}},
		{{Path: "/a/_", Params: []uint64{0}, Section: SectionI64, Value: int64(1)}, {Path: "/a/b", Section: SectionI64, Value: int64(1)}},
	} {
		_, err := Compose(attrs)
		assert.Error(t, err, "%+v", attrs)
	}
}

// genValue draws a JSON value, written with random key order and spacing,
// to depth.
func genValue(t *rapid.T, depth int) string {
	kinds := []string{"null", "bool", "int", "big", "float", "string"}
	if depth > 0 {
		kinds = append(kinds, "object", "array")
	}
	ws := func() string { return rapid.SampledFrom([]string{"", " ", "\n\t "}).Draw(t, "ws") }
	switch rapid.SampledFrom(kinds).Draw(t, "kind") {
	case "null":
		return "null"
	case "bool":
		return strconv.FormatBool(rapid.Bool().Draw(t, "b"))
	case "int":
		return strconv.FormatInt(rapid.Int64().Draw(t, "i"), 10)
	case "big":
		return "1" + strings.Repeat("0", rapid.IntRange(19, 40).Draw(t, "zeros"))
	case "float":
		f := rapid.Float64().Draw(t, "f")
		if math.IsInf(f, 0) || math.IsNaN(f) {
			f = 0.5
		}
		return strconv.FormatFloat(f, rapid.SampledFrom([]byte{'e', 'f', 'g'}).Draw(t, "fmt"), -1, 64)
	case "string":
		q, _ := jsontext.AppendQuote(nil, rapid.String().Draw(t, "s"))
		return string(q)
	case "object":
		n := rapid.IntRange(0, 4).Draw(t, "n")
		keys := rapid.SliceOfNDistinct(rapid.SampledFrom([]string{"a", "b", "_", "~", "/", "~n", "x/y", "é", ""}), n, n, rapid.ID[string]).Draw(t, "keys")
		parts := make([]string, 0, n)
		for _, k := range keys {
			q, _ := jsontext.AppendQuote(nil, k)
			parts = append(parts, ws()+string(q)+ws()+":"+genValue(t, depth-1))
		}
		return "{" + strings.Join(parts, ",") + ws() + "}"
	default:
		n := rapid.IntRange(0, 4).Draw(t, "n")
		parts := make([]string, 0, n)
		for range n {
			parts = append(parts, ws()+genValue(t, depth-1)+ws())
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
}

func sortedAttrs(attrs []Attr) []Attr {
	out := slices.Clone(attrs)
	slices.SortFunc(out, func(a, b Attr) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return slices.Compare(a.Params, b.Params)
	})
	return out
}

// The lossless law: Compose(Explode(x)) is Canonical(x), and exploding the
// canonical document gives the same attributes back, in any order.
func TestLosslessRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		doc := genValue(t, 3)
		attrs, err := Explode([]byte(doc), Options{})
		if err != nil {
			t.Fatalf("explode %q: %v", doc, err)
		}
		got, err := Compose(attrs)
		if err != nil {
			t.Fatalf("compose %q: %v", doc, err)
		}
		want, err := Canonical([]byte(doc))
		if err != nil {
			t.Fatalf("canonical %q: %v", doc, err)
		}
		if string(got) != string(want) {
			t.Fatalf("%q composed as %q, canonical %q", doc, got, want)
		}
		again, err := Explode(got, Options{})
		if err != nil {
			t.Fatalf("explode the canonical %q: %v", got, err)
		}
		if a, b := sortedAttrs(attrs), sortedAttrs(again); !assert.ObjectsAreEqual(a, b) {
			t.Fatalf("%q exploded unlike its canonical form", doc)
		}
	})
}

func FuzzLosslessRoundTrip(f *testing.F) {
	for _, s := range []string{`{"a":[1,{"_":null}],"b":"x"}`, `[[],{}]`, `1e400`, `0.1`, `18446744073709551616`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, doc []byte) {
		attrs, err := Explode(doc, Options{})
		if err != nil {
			return
		}
		got, err := Compose(attrs)
		require.NoError(t, err)
		want, err := Canonical(doc)
		require.NoError(t, err)
		require.Equal(t, string(want), string(got))
	})
}
