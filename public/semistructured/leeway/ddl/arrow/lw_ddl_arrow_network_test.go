package arrow

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stretchr/testify/require"
)

func TestGenerateNetworkType(t *testing.T) {
	gen := NewTechnologySpecificCodeGenerator()
	b := &strings.Builder{}
	gen.SetCodeBuilder(b)
	p := canonicaltypes.NewParser()
	cases := []struct{ sig, want string }{
		{"v", "&arrow.FixedSizeBinaryType{ByteWidth: 4}"},
		{"w", "&arrow.FixedSizeBinaryType{ByteWidth: 16}"},
		{"vc", "&arrow.FixedSizeBinaryType{ByteWidth: 5}"},
		{"wc", "&arrow.FixedSizeBinaryType{ByteWidth: 17}"},
		{"vh", "arrow.ListOfNonNullable(&arrow.FixedSizeBinaryType{ByteWidth: 4})"},
		{"wch", "arrow.ListOfNonNullable(&arrow.FixedSizeBinaryType{ByteWidth: 17})"},
	}
	for _, c := range cases {
		b.Reset()
		ct := p.MustParsePrimitiveTypeAst(c.sig)
		require.NoError(t, gen.GenerateType(ct), "sig %s", c.sig)
		require.Equal(t, c.want, b.String(), "sig %s", c.sig)
	}
}
