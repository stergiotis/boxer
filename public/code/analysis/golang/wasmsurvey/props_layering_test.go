package wasmsurvey

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckLayering(t *testing.T) {
	const m = "example.com/mod"
	imports := map[string][]string{
		m + "/public/containers":                       {m + "/public/keelson/runtime/app"},
		m + "/public/keelson/runtime/windowhost":       {m + "/public/thestack/imzero2/egui2"},
		m + "/apps/play":                               {m + "/public/keelson/runtime/app"},
		m + "/public/semistructured/markdown/obsidian": {m + "/public/keelson/designsystem/web"},
		m + "/public/semistructured/cbor/buscodec":     {m + "/public/semistructured/cbor", "fmt"},
		m + "/public/keelsonish":                       {m + "/public/thestackish"},
	}
	got := CheckLayering(imports, m)
	require.Equal(t, []LayeringFinding{{ImportPath: m + "/public/containers", Imported: m + "/public/keelson/runtime/app"}}, got)
}
