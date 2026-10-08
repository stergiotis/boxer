package logcomment

import (
	"reflect"
	"testing"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The keys a query log already holds must keep parsing: the stamp is read
// back from rows written before any field was added.
func TestParse_ExistingKeys(t *testing.T) {
	st, ok := Parse(`{"run_id":"r1","app":"data.play","instance":3,"lane":"map","authored_fp":"a","sent_fp":"s","chain_fp":"c","env_fp":"e","task":"t","task_epoch":2,"task_call":"k"}`)
	require.True(t, ok)
	assert.Equal(t, Stamp{RunId: "r1", App: "data.play", Instance: 3, Lane: "map", AuthoredFp: "a", SentFp: "s", ChainFp: "c", EnvFp: "e", Task: "t", TaskEpoch: 2, TaskCall: "k"}, st)

	for _, s := range []string{"", "plain comment", `{"unrelated":"json"}`, `{"run_id":`, "queryrunsd-extract"} {
		_, ok = Parse(s)
		assert.False(t, ok, "%q", s)
	}
}

func TestMarshal_OmitsAbsentFields(t *testing.T) {
	assert.Equal(t, "", Marshal(Stamp{}))
	assert.Equal(t, `{"run_id":"r1"}`, Marshal(Stamp{RunId: "r1"}))
	assert.Equal(t, `{"batch":"b"}`, Marshal(Stamp{Batch: "b"}), "a zero Instance stays off the wire")
}

func TestFromCallIdentity_RoundTrip(t *testing.T) {
	ci := callident.CallIdentity{
		Origin: callident.Origin{Run: "r", App: "a", Instance: 9},
		Claims: callident.Claims{Principal: "p:42", Purpose: "support", Correlation: "op-1"},
	}
	st := FromCallIdentity(ci, "B1")
	got, ok := Parse(Marshal(st))
	require.True(t, ok)
	assert.Equal(t, st, got)
	assert.Equal(t, "p:42", got.Principal)
	assert.Equal(t, "B1", got.Batch)
}

// One invalid byte must not cost the whole stamp: the JSON encoder refuses
// invalid UTF-8, and an empty stamp would send the statement out anonymous.
func TestMarshal_RepairsInvalidUTF8(t *testing.T) {
	st, ok := Parse(Marshal(Stamp{Principal: "p:\xff1", Batch: "B"}))
	require.True(t, ok)
	assert.Equal(t, "p:\uFFFD1", st.Principal)
	assert.Equal(t, "B", st.Batch)
}

// Every string field of Stamp is repaired: a field added to the struct but not
// to Marshal's list would bring back the silent drop.
func TestMarshal_RepairsEveryStringField(t *testing.T) {
	typ := reflect.TypeFor[Stamp]()
	for i := range typ.NumField() {
		if typ.Field(i).Type.Kind() != reflect.String {
			continue
		}
		var st Stamp
		reflect.ValueOf(&st).Elem().Field(i).SetString("\xff")
		assert.NotEmpty(t, Marshal(st), "field %s", typ.Field(i).Name)
	}
}
