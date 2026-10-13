package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// keelson('agent_tools') lists each app's tools in the order its model sees
// them, numbered from zero per app; a second publication replaces the
// first.
func TestPublishedToolsKeepTheirOrder(t *testing.T) {
	PublishTools("test/tools", []ToolRow{{Name: "b"}, {Name: "a"}})
	PublishTools("test/tools", []ToolRow{{Name: "z", Group: "fixed"}, {Name: "y"}, {Name: "x"}})
	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg))
	p, ok := reg.Lookup(TableTools)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer rec.Release()
	var got []string
	for i := range int(rec.NumRows()) {
		if rec.Column(0).ValueStr(i) != "test/tools" {
			continue
		}
		got = append(got, rec.Column(1).ValueStr(i)+":"+rec.Column(2).ValueStr(i))
	}
	assert.Equal(t, []string{"0:z", "1:y", "2:x"}, got)
}
