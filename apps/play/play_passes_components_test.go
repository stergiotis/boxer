package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// TestRegisterComponents_KindRoster pins what this host's LW_COMPONENT can
// resolve — the wiring-site review RegisterComponents' comment promises. A
// kind disappearing from here is a query surface silently going dark for
// whoever authored against it (mdedit's send-to-play launch query included).
func TestRegisterComponents_KindRoster(t *testing.T) {
	r := componentsql.NewRegistry()
	require.NoError(t, RegisterComponents(r))
	kinds := r.Kinds()
	for _, kind := range []string{"SysMem", "LadingMount", "MdDoc"} {
		assert.Contains(t, kinds, kind)
	}
}
