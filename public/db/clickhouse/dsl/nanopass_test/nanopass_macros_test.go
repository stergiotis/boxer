//go:build llm_generated_opus46

package nanopass_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMacroExpanderSimple(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("tenantFilter", func(args []nanopass.LiteralArg) (string, error) {
		if len(args) != 1 {
			return "", fmt.Errorf("tenantFilter expects 1 arg, got %d", len(args))
		}
		return "tenant_id = " + args[0].Value, nil
	})

	pass := expander.Pass()
	got, err := pass("SELECT a FROM t WHERE tenantFilter(42)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t WHERE tenant_id = 42", got)

	// Verify parseable
	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestMacroExpanderStringArg(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("jsonCol", func(args []nanopass.LiteralArg) (string, error) {
		if len(args) != 1 || args[0].Type != nanopass.LiteralTypeString {
			return "", fmt.Errorf("jsonCol expects 1 string arg")
		}
		return "JSONExtractString(payload, " + args[0].Value + ")", nil
	})

	pass := expander.Pass()
	got, err := pass("SELECT jsonCol('name') FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT JSONExtractString(payload, 'name') FROM t", got)
}

func TestMacroExpanderMultipleArgs(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("between", func(args []nanopass.LiteralArg) (string, error) {
		if len(args) != 3 {
			return "", fmt.Errorf("between expects 3 args")
		}
		return args[0].Value + " BETWEEN " + args[1].Value + " AND " + args[2].Value, nil
	})

	pass := expander.Pass()
	got, err := pass("SELECT a FROM t WHERE between('col', 1, 10)")
	require.NoError(t, err)
	assert.Contains(t, got, "'col' BETWEEN 1 AND 10")
}

func TestMacroExpanderCaseInsensitive(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("MyMacro", func(args []nanopass.LiteralArg) (string, error) {
		return "42", nil
	})

	pass := expander.Pass()

	got, err := pass("SELECT mymacro() FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 42 FROM t", got)
}

func TestMacroExpanderNoMatch(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("myMacro", func(args []nanopass.LiteralArg) (string, error) {
		return "42", nil
	})

	pass := expander.Pass()

	// count is not registered — should be untouched
	got, err := pass("SELECT count(*) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT count(*) FROM t", got)
}

func TestMacroExpanderNonLiteralArgSkipped(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("myMacro", func(args []nanopass.LiteralArg) (string, error) {
		return "replaced", nil
	})

	pass := expander.Pass()

	// Non-literal argument (column reference) — macro should be skipped
	got, err := pass("SELECT myMacro(a) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT myMacro(a) FROM t", got)
}

func TestMacroExpanderMultipleCalls(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("c", func(args []nanopass.LiteralArg) (string, error) {
		return strings.Trim(args[0].Value, "'"), nil
	})

	pass := expander.Pass()
	got, err := pass("SELECT c('x'), c('y') FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT x, y FROM t", got)
}

func TestMacroExpanderNegativeArg(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("offset", func(args []nanopass.LiteralArg) (string, error) {
		return "created_at + " + args[0].Value, nil
	})

	pass := expander.Pass()
	got, err := pass("SELECT offset(-3) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT created_at + -3 FROM t", got)
}

func TestMacroExpanderIdempotent(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("m", func(args []nanopass.LiteralArg) (string, error) {
		return "42", nil
	})

	pass := expander.Pass()
	pass1, err := pass("SELECT m() FROM t")
	require.NoError(t, err)
	pass2, err := pass(pass1)
	require.NoError(t, err)
	assert.Equal(t, pass1, pass2)
}

func TestMacroExpanderNestedWithFixedPoint(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("outer_m", func(args []nanopass.LiteralArg) (string, error) {
		return "inner_m(" + args[0].Value + ")", nil
	})
	expander.Register("inner_m", func(args []nanopass.LiteralArg) (string, error) {
		return args[0].Value + " + 1", nil
	})

	// Single pass only expands outer
	pass := expander.Pass()
	got, err := pass("SELECT outer_m(5) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT inner_m(5) FROM t", got)

	// Fixed-point expands both
	fpPass := nanopass.FixedPoint(pass, 5)
	got, err = fpPass("SELECT outer_m(5) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 5 + 1 FROM t", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestMacroExpanderErrorPropagation(t *testing.T) {
	expander := nanopass.NewMacroExpander()
	expander.Register("bad_macro", func(args []nanopass.LiteralArg) (string, error) {
		return "", fmt.Errorf("intentional error")
	})

	pass := expander.Pass()
	_, err := pass("SELECT bad_macro(1) FROM t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intentional error")
}
