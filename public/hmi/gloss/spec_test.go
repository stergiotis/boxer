package gloss

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ParseSpec takes the lwsql spelling apart by prefix; an unprefixed token
// continues the previous value, which is how a name with a space and an
// Arrow type with spaces survive.
func TestParseSpec(t *testing.T) {
	s := ParseSpec("name:temperature section:sensor role:val ct:f64 enc:light-general-compression sem:measured sem:scale-of-measurement-metric-ratio use:data arrow:list<item: float64, nullable>")
	assert.Equal(t, "temperature", s.Name)
	assert.Equal(t, "sensor", s.Section)
	assert.Equal(t, "val", s.Role)
	assert.Equal(t, "", s.Item)
	assert.Equal(t, "f64", s.CT)
	assert.Equal(t, []string{"light-general-compression"}, s.Enc)
	assert.Equal(t, []string{"measured", "scale-of-measurement-metric-ratio"}, s.Sem)
	assert.Equal(t, []string{"data"}, s.Use)
	assert.Equal(t, "list<item: float64, nullable>", s.Arrow, "the type's own spaces continue the arrow: token; `list<item:` is not an item: token")
	assert.Contains(t, s.Line, "name:temperature")

	s = ParseSpec("name:ts item:ts ct:z64 sem:transaction-time arrow:timestamp[ms, tz=UTC]")
	assert.Equal(t, "ts", s.Item)
	assert.Equal(t, "timestamp[ms, tz=UTC]", s.Arrow)
	assert.Empty(t, s.Section)

	s = ParseSpec("name:my temp@gloss/temperature;unit=K arrow:float64")
	assert.Equal(t, "my temp@gloss/temperature;unit=K", s.Name, "a space in a result column name")
	assert.Equal(t, "float64", s.Arrow)

	s = ParseSpec("")
	assert.Equal(t, Spec{}, s)
	assert.Equal(t, "x", ParseSpec("stray tokens first name:x").Name, "tokens before the first prefix are dropped")
}
