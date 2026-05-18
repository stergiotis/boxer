//go:build llm_generated_opus47

package codelint_test

import (
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/gov/codelint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCS001_FlagsFmtErrorfOutsideEh(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs001/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS001())

	var findings []codelint.Finding
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		findings = append(findings, f)
	}

	require.Len(t, findings, 1, "expected exactly one unsuppressed CS001 finding")
	assert.Equal(t, "CS001", findings[0].RuleId)
	assert.Equal(t, codelint.FindingSeverityWarn, findings[0].Severity)
	assert.Contains(t, findings[0].Path, "bad.go")
	assert.Equal(t, int32(9), findings[0].Line)
}

func TestCS001_PassesGoodFile(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs001/good")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS001())

	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		t.Fatalf("unexpected finding: %+v", f)
	}
}
