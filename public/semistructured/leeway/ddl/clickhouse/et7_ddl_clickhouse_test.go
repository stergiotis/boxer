package clickhouse

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/sample"
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
	assert.Greater(t, coverage.CoverageTypeTemporal, 0.75)
	assert.Equal(t, []string{}, coverage.NotCovered)
	require.Greater(t, coverage.CoverageTypeTotal, 0.75)
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
	require.NoError(t, err)
	cmd := exec.Command(clickhouseBinary, "local", "-S", code.String(), "--query", "SELECT * FROM table LIMIT 0")
	var out []byte
	out, err = cmd.CombinedOutput()
	require.NoError(t, err)
	require.Empty(t, out)
}
