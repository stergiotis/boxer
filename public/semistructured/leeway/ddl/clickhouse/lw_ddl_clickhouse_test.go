package clickhouse

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	ddl2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTechnologySpecificCodeGenerator_Coverage(t *testing.T) {
	gen := NewTechnologySpecificCodeGenerator()
	b := &strings.Builder{}
	gen.SetCodeBuilder(b)
	coverage := ddl2.MeasureTechCoverage(gen)
	assert.Greater(t, coverage.CoverageTypeString, 0.75)
	assert.Greater(t, coverage.CoverageTypeMachineNumeric, 0.75)
	assert.Greater(t, coverage.CoverageTypeTemporal, 0.30)
	assert.Equal(t, []string{"f8", "f16", "f8l", "f16l", "f8n", "f16n", "f8h", "f16h", "f8lh", "f16lh", "f8nh", "f16nh", "f8m", "f16m", "f8lm", "f16lm", "f8nm", "f16nm", "d32", "t32", "d64", "t64", "d32h", "t32h", "d64h", "t64h", "d32m", "t32m", "d64m", "t64m", "bx0", "bx128", "bx145", "bx192", "bx0h", "bx128h", "bx145h", "bx192h", "bx0m", "bx128m", "bx145m", "bx192m"}, coverage.NotCovered)
	require.Greater(t, coverage.CoverageTypeTotal, 0.50)
}
func TestTechnologySpecificCodeGenerator_GeneratedCode(t *testing.T) {
	gen := NewTechnologySpecificCodeGenerator()
	b := &strings.Builder{}
	gen.SetCodeBuilder(b)
	code := &strings.Builder{}
	for n := uint64(0); n < sample.SampleTypeMaxExcl; n++ {
		typ := sample.GenerateSampleType(n)
		if !typ.IsValid() {
			continue
		}
		b.Reset()
		err := gen.GenerateType(typ)
		if err != nil {
			if errors.Is(err, common.ErrNotImplemented) {
				// skip
				continue
			}
			require.NoError(t, err)
		}

		_, err = fmt.Fprintf(code, "col_%s %s,", typ.String(), b.String())
		require.NoError(t, err)
	}
	_, err := code.WriteString("dummy String")
	require.NoError(t, err)
	var clickhouseBinary string
	clickhouseBinary, err = GetClickHouseBinaryPath()
	if err != nil {
		t.Skip("no clickhouse binary available")
		return
	}
	require.NoError(t, err)
	cmd := exec.Command(clickhouseBinary, "local", "-S", code.String(), "--query", "SELECT * FROM table LIMIT 0")
	var out []byte
	out, err = cmd.CombinedOutput()
	require.NoError(t, err)
	require.Empty(t, out)
}
