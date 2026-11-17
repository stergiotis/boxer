package queryendpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An endpoint is listed from its publish to its retract, found by name,
// and listed by name among the others.
func TestPublishLookupRetract(t *testing.T) {
	gate := Endpoint{Name: "t-gate", Display: "Gate", URL: "http://127.0.0.1:8124"}
	retractGate, err := Publish(gate)
	require.NoError(t, err)
	retractPlane, err := Publish(Endpoint{Name: "t-a-plane", URL: "http://localhost:9/query"})
	require.NoError(t, err)

	got, ok := Lookup("t-gate")
	require.True(t, ok)
	assert.Equal(t, gate, got)
	assert.Equal(t, "Gate", got.Label())
	got, ok = Lookup("t-a-plane")
	require.True(t, ok)
	assert.Equal(t, "t-a-plane", got.Label(), "an endpoint without a display name shows its name")
	_, ok = Lookup("t-nowhere")
	assert.False(t, ok)

	var names []string
	for _, e := range All() {
		if e.Name == "t-gate" || e.Name == "t-a-plane" {
			names = append(names, e.Name)
		}
	}
	assert.Equal(t, []string{"t-a-plane", "t-gate"}, names)

	_, err = Publish(Endpoint{Name: "t-gate", URL: "http://127.0.0.1:1"})
	require.ErrorIs(t, err, ErrTaken)

	retractGate()
	retractGate()
	_, ok = Lookup("t-gate")
	assert.False(t, ok)
	again, err := Publish(gate)
	require.NoError(t, err, "a retracted name is free again")
	again()
	retractPlane()
}

// A malformed or reserved name and a URL that is not http on loopback are
// refused.
func TestPublishRefuses(t *testing.T) {
	for _, c := range []struct {
		e    Endpoint
		want error
	}{
		{Endpoint{Name: "", URL: "http://127.0.0.1:1"}, ErrName},
		{Endpoint{Name: "Gate", URL: "http://127.0.0.1:1"}, ErrName},
		{Endpoint{Name: "a gate", URL: "http://127.0.0.1:1"}, ErrName},
		{Endpoint{Name: NameDefault, URL: "http://127.0.0.1:1"}, ErrReserved},
		{Endpoint{Name: "t-far", URL: "http://example.com:8123"}, ErrURL},
		{Endpoint{Name: "t-tls", URL: "https://127.0.0.1:8123"}, ErrURL},
		{Endpoint{Name: "t-none", URL: ""}, ErrURL},
	} {
		retract, err := Publish(c.e)
		require.ErrorIs(t, err, c.want, c.e.Name)
		assert.Nil(t, retract)
	}
}
