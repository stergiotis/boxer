package codegen

import (
	"fmt"
	"iter"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/rs/zerolog/log"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stretchr/testify/require"
)

func iterateTypes() iter.Seq[canonicalTypes2.PrimitiveAstNodeI] {
	return func(yield func(canonicalTypes2.PrimitiveAstNodeI) bool) {
		for i := uint64(0); i < sample.SampleMachineNumericMaxExcl; i++ {
			ct := sample.GenerateSampleMachineNumericType(i)
			if ct.IsValid() {
				if !yield(ct) {
					return
				}
			}
		}
		for i := uint64(0); i < sample.SampleStringTypeMaxExcl; i++ {
			ct := sample.GenerateSampleStringType(i)
			if ct.WidthModifier == canonicalTypes2.WidthModifierNone && ct.IsValid() {
				if !yield(ct) {
					return
				}
			}
		}
		for i := uint64(0); i < sample.SampleTemporalTypeMaxExcl; i++ {
			ct := sample.GenerateSampleTemporalType(i)
			if ct.IsValid() {
				if !yield(ct) {
					return
				}
			}
		}
	}
}
func TestGenerateGoCode(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	// FIXME
	dest := path.Join(home, "repo", "boxer", "public", "semistructured", "leeway", "canonicalTypes", "codegen", "canonicalTypes_go_codegen_dummy_test.gen.go")
	_ = os.Remove(dest)

	s := &strings.Builder{}
	var imports []string
	for ct := range iterateTypes() {
		var typeCode, zeroValueLiteral string
		var imp []string
		typeCode, zeroValueLiteral, imp, err = GenerateGoCode(ct, encodingaspects.EmptyAspectSet)
		if err == nil {
			imports = append(imports, imp...)
			require.NoError(t, err)
			var _ = typeCode
			var _ = zeroValueLiteral
			_, err = fmt.Fprintf(s, "\t{\n\t\tvar %s %s\n\t\trequire.Equal(t, %s, %s)\n\t}\n", ct.String(), typeCode, ct.String(), zeroValueLiteral)
			require.NoError(t, err)
		} else {
			log.Warn().Err(err).Stringer("ct", ct).Msg("unable to generate go code")
			err = nil
		}
	}
	slices.Sort(imports)
	imports = slices.Compact(imports)

	_, err = s.WriteString("}\n")
	require.NoError(t, err)

	s2 := &strings.Builder{}
	_, err = s2.WriteString(`
// Code generated DO NOT EDIT
package codegen
`)
	require.NoError(t, err)
	_, err = s2.WriteString(`
import (
	"testing"
	"github.com/stretchr/testify/require"
`)
	for _, im := range imports {
		_, err = fmt.Fprintf(s2, "\t%q\n", im)
		require.NoError(t, err)
	}
	require.NoError(t, err)
	_, err = s2.WriteString(`
)
func TestGeneratedGoCodeOutput(t *testing.T) {
`)
	require.NoError(t, err)
	err = os.WriteFile(dest, []byte(s2.String()+s.String()), os.ModePerm)
	require.NoError(t, err)
}
