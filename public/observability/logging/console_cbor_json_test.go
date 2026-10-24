package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_zerologCborToJson_integers checks the transcoder on hand-written CBOR
// in zerolog's shape (an indefinite-length map), so the default lane, whose
// events are JSON, covers it too.
func Test_zerologCborToJson_integers(t *testing.T) {
	for _, c := range []struct {
		name string
		cbor []byte
		json string
	}{
		{"2⁶³", []byte{0xbf, 0x61, 'v', 0x1b, 0x80, 0, 0, 0, 0, 0, 0, 0, 0xff}, `{"v":9223372036854775808}`},
		{"2⁶⁴-1", []byte{0xbf, 0x61, 'v', 0x1b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, `{"v":18446744073709551615}`},
		{"-2⁶⁴", []byte{0xbf, 0x61, 'v', 0x3b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, `{"v":-18446744073709551616}`},
		{"array", []byte{0xbf, 0x61, 'v', 0x9f, 0x01, 0x20, 0xff, 0xff}, `{"v":[1,-1]}`},
		{"byte string escaped", []byte{0xbf, 0x61, 'v', 0x43, 'a', '"', 'b', 0xff}, `{"v":"a\"b"}`},
		{"trailing event ignored", []byte{0xa1, 0x61, 'v', 0x01, 0xa0}, `{"v":1}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			j, err := zerologCborToJson(c.cbor)
			require.NoError(t, err)
			require.Equal(t, c.json, string(j))
		})
	}
	_, err := zerologCborToJson([]byte{0xbf, 0x61, 'v'})
	require.Error(t, err, "a truncated event is reported, and ConsoleWriter falls back to zerolog")
}
