package opjson

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type colourE uint8

func (colourE) OpEnumNames() []string { return []string{"red", "green", "blue"} }

type inner struct {
	Label string
}

type sample struct {
	Text     string `desc:"the text"`
	Count    int32
	Small    uint8
	Big      uint64
	Signed   int64
	Ratio    float64
	On       bool
	Raw      []byte
	Tags     []string
	Attrs    map[string]int32
	At       time.Time
	Wait     time.Duration
	Colour   colourE
	Maybe    *string
	Nested   inner
	Renamed  string `json:"sql"`
	Skipped  string `json:"-"`
	ResultID string
}

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"ResultRef": "result_ref", "SQLText": "sql_text", "Count": "count", "ResultID": "result_id", "A1B": "a1_b",
	} {
		assert.Equal(t, want, SnakeCase(in), in)
	}
}

func TestRoundTrip(t *testing.T) {
	s := "x"
	in := sample{
		Text: "hello", Count: -3, Small: 255, Big: math.MaxUint64, Signed: math.MinInt64, Ratio: 0.5, On: true,
		Raw: []byte{0x00, 0xff}, Tags: []string{"a"}, Attrs: map[string]int32{"b": 2, "a": 1},
		At: time.Date(2026, 9, 30, 12, 0, 0, 5, time.UTC), Wait: 1500 * time.Millisecond,
		Colour: 2, Maybe: &s, Nested: inner{Label: "l"}, Renamed: "select 1",
	}
	b, err := Encode(in)
	require.NoError(t, err)
	js := string(b)
	assert.Contains(t, js, `"big":"18446744073709551615"`)
	assert.Contains(t, js, `"signed":"-9223372036854775808"`)
	assert.Contains(t, js, `"raw":"00ff"`)
	assert.Contains(t, js, `"colour":"blue"`)
	assert.Contains(t, js, `"wait":"1.5s"`)
	assert.Contains(t, js, `"attrs":{"a":1,"b":2}`)
	assert.Contains(t, js, `"sql":"select 1"`)
	assert.NotContains(t, js, "skipped")

	v, err := Decode(b, reflect.TypeFor[sample]())
	require.NoError(t, err)
	assert.Equal(t, in, v.Interface().(sample))
}

func TestDecodeRefuses(t *testing.T) {
	ty := reflect.TypeFor[inner]()
	for _, tc := range []struct{ in, msg string }{
		{`{"label":"a","extra":1}`, "unknown field"},
		{`{}`, "required"},
		{`{"label":3}`, "expected a string"},
	} {
		_, err := Decode([]byte(tc.in), ty)
		require.Error(t, err, tc.in)
		assert.Contains(t, err.Error(), tc.msg, tc.in)
	}
	_, err := Decode([]byte(`{"count":1.5}`), reflect.TypeFor[struct{ Count int32 }]())
	require.Error(t, err)
	_, err = Decode([]byte(`{"count":300}`), reflect.TypeFor[struct{ Count uint8 }]())
	require.Error(t, err)
	_, err = Decode([]byte(`{"n":12}`), reflect.TypeFor[struct{ N uint64 }]())
	require.Error(t, err, "a 64-bit integer must arrive as a string")
	_, err = Decode([]byte(`{"x":"AB"}`), reflect.TypeFor[struct{ X []byte }]())
	require.Error(t, err, "hex is lowercase")
	_, err = Decode([]byte(`{"c":"purple"}`), reflect.TypeFor[struct{ C colourE }]())
	require.Error(t, err)
	_, err = Decode([]byte(`{"a":1}`), nil)
	require.Error(t, err, "an operation without arguments takes an empty object")
	_, err = Decode(nil, nil)
	require.NoError(t, err)
}

func TestOptionalAndNull(t *testing.T) {
	type opt struct{ P *int32 }
	v, err := Decode([]byte(`{}`), reflect.TypeFor[opt]())
	require.NoError(t, err)
	assert.Nil(t, v.Interface().(opt).P)
	v, err = Decode([]byte(`{"p":null}`), reflect.TypeFor[opt]())
	require.NoError(t, err)
	assert.Nil(t, v.Interface().(opt).P)
	b, err := Encode(opt{})
	require.NoError(t, err)
	assert.Equal(t, `{"p":null}`, string(b))
}

func TestEncodeRefusesNaN(t *testing.T) {
	_, err := Encode(struct{ F float64 }{F: math.NaN()})
	require.Error(t, err)
}

func TestSchema(t *testing.T) {
	s, err := Schema(reflect.TypeFor[sample]())
	require.NoError(t, err)
	js := string(s)
	for _, want := range []string{
		`"additionalProperties":false`,
		`"big":{"description":"a 64-bit unsigned integer as a decimal string","pattern":"^[0-9]+$","type":"string"}`,
		`"colour":{"enum":["red","green","blue"],"type":"string"}`,
		`"text":{"description":"the text","type":"string"}`,
		`"small":{"maximum":255,"minimum":0,"type":"integer"}`,
	} {
		assert.Contains(t, js, want)
	}
	assert.NotContains(t, js, `"maybe"],`, "a pointer field is not required")
	assert.True(t, strings.Contains(js, `"required":["text",`))

	empty, err := Schema(nil)
	require.NoError(t, err)
	assert.Equal(t, `{"additionalProperties":false,"properties":{},"type":"object"}`, string(empty))
}

func TestSchemaRefuses(t *testing.T) {
	type rec struct{ Next *rec }
	for _, ty := range []reflect.Type{
		reflect.TypeFor[struct{ F any }](),
		reflect.TypeFor[struct{ M map[int]string }](),
		reflect.TypeFor[struct{ C chan int }](),
		reflect.TypeFor[rec](),
		reflect.TypeFor[struct{ inner }](),
	} {
		_, err := Schema(ty)
		assert.Error(t, err, ty.String())
	}
}

func TestFieldNames(t *testing.T) {
	names, err := FieldNames(reflect.TypeFor[sample]())
	require.NoError(t, err)
	assert.Contains(t, names, "sql")
	assert.Contains(t, names, "result_id")
	assert.NotContains(t, names, "skipped")
}

func TestOmittableFieldsAreOptional(t *testing.T) {
	type args struct {
		Limit  uint32   `json:",omitzero"`
		Fields []string `json:"cols,omitempty"`
		Need   string
	}
	v, err := Decode([]byte(`{"need":"x"}`), reflect.TypeFor[args]())
	require.NoError(t, err)
	assert.Equal(t, args{Need: "x"}, v.Interface().(args))
	s, err := Schema(reflect.TypeFor[args]())
	require.NoError(t, err)
	assert.Contains(t, string(s), `"required":["need"]`)
	assert.Contains(t, string(s), `"cols"`)
}
