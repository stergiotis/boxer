package diag_test

import (
	"embed"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

//go:embed diag_*_gold.out.txt
var goldFS embed.FS

// rewriteGold regenerates the goldens instead of comparing. Flip once,
// run, flip back, review the diff.
const rewriteGold = false

func gold(t *testing.T, name string, got string) {
	t.Helper()
	if rewriteGold {
		require.NoError(t, os.WriteFile(name, []byte(got), 0o644))
		return
	}
	b, err := goldFS.ReadFile(name)
	require.NoError(t, err, "golden %s missing — set rewriteGold = true once to create it", name)
	require.Equal(t, string(b), got, "golden %s", name)
}

// prettyCases are the inputs the pretty golden renders: the RFC nesting
// examples, a leeway-wire-shaped entity with tag comments and an annotation
// hook, a folded byte string, a sequence, and a malformed item.
var prettyCases = []struct {
	name string
	hex  string
	opts Options
}{
	{"rfc-nested", "9f018202039f0405ffff", Options{Width: 12}},
	{"rfc-map", "a26161016162820203", Options{Width: 8}},
	{"chunks", "5f42010243030405ff", Options{Width: 8}},
	{"tags", "a401616102d903e9a1011a514b67b003d901028301020304f93e00", Options{Width: 24, TagComments: true, FloatPrecision: true}},
	{"entity", "8301a1008218ff1903e8a463663332818281820007f93e006673782d7536348182818201426162636162636474696d65d903e9a2011a514b67b028182863736574d90102820102", Options{
		Width:       40,
		TagComments: true,
		Annotate: func(path []PathElem) string {
			switch len(path) {
			case 0:
				return "entity"
			case 1:
				switch path[0].Index {
				case 0:
					return "version"
				case 1:
					return "plains"
				case 2:
					return "tagged"
				}
			case 2:
				if path[0].Index == 2 && path[1].Kind == PathElemKey {
					return "slot"
				}
			}
			return ""
		},
	}},
	{"bytes-fold", "5820000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", Options{BytesFold: 8}},
	{"sequence", "0102830102036161", Options{Sequence: true, Width: 6}},
	{"malformed", "8301021c", Options{}},
}

func TestPrettyGolden(t *testing.T) {
	var sb strings.Builder
	for _, tc := range prettyCases {
		b, err := hex.DecodeString(tc.hex)
		require.NoError(t, err)
		s, err := String(b, tc.opts)
		if tc.name != "malformed" {
			require.NoError(t, err, tc.name)
		} else {
			require.Error(t, err)
		}
		spans, _ := Print(b, tc.opts)
		checkSpans(t, spans, s)
		sb.WriteString("## ")
		sb.WriteString(tc.name)
		sb.WriteString(" ")
		sb.WriteString(tc.hex)
		sb.WriteString("\n")
		sb.WriteString(s)
		sb.WriteString("\n\n")
	}
	gold(t, "diag_pretty_gold.out.txt", sb.String())
}
