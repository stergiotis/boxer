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

func TestCS002_FlagsMisplacedCtx(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs002/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS002())

	var findings []codelint.Finding
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		findings = append(findings, f)
	}

	require.Len(t, findings, 4, "expected 4 unsuppressed CS002 findings (suppressed one omitted)")
	for _, f := range findings {
		assert.Equal(t, "CS002", f.RuleId)
		assert.Equal(t, codelint.FindingSeverityWarn, f.Severity)
		assert.Contains(t, f.Path, "bad.go")
	}
}

func TestCS002_PassesGoodFile(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs002/good")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS002())

	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		t.Fatalf("unexpected finding: %+v", f)
	}
}

func TestCS003_FlagsPointerMutexFields(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs003/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS003())

	var findings []codelint.Finding
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		findings = append(findings, f)
	}

	require.Len(t, findings, 4, "expected 4 unsuppressed CS003 findings (one suppressed)")
	for _, f := range findings {
		assert.Equal(t, "CS003", f.RuleId)
		assert.Equal(t, codelint.FindingSeverityWarn, f.Severity)
		assert.Contains(t, f.Path, "bad.go")
	}
}

func TestCS003_PassesGoodFile(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs003/good")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS003())

	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		t.Fatalf("unexpected finding: %+v", f)
	}
}

func TestCS004_FlagsLegacyAtomicAPI(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs004/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS004())

	var findings []codelint.Finding
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		findings = append(findings, f)
	}

	require.Len(t, findings, 8, "expected 8 unsuppressed CS004 findings (one suppressed)")
	for _, f := range findings {
		assert.Equal(t, "CS004", f.RuleId)
		assert.Equal(t, codelint.FindingSeverityWarn, f.Severity)
		assert.Contains(t, f.Path, "bad.go")
	}
}

func TestCS004_PassesGoodFile(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs004/good")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS004())

	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		t.Fatalf("unexpected finding: %+v", f)
	}
}

func TestCS005_FlagsInterfaceWithoutISuffix(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs005/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS005())

	var findings []codelint.Finding
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		findings = append(findings, f)
	}

	require.Len(t, findings, 3, "expected 3 unsuppressed CS005 findings (one suppressed)")
	for _, f := range findings {
		assert.Equal(t, "CS005", f.RuleId)
		assert.Equal(t, codelint.FindingSeverityWarn, f.Severity)
		assert.Contains(t, f.Path, "bad.go")
	}
}

func TestCS005_PassesGoodFile(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs005/good")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS005())

	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		t.Fatalf("unexpected finding: %+v", f)
	}
}

func TestCS006_FlagsEnumWithoutESuffix(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs006/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS006())

	var findings []codelint.Finding
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		findings = append(findings, f)
	}

	require.Len(t, findings, 2, "expected 2 unsuppressed CS006 findings (Suppressed and SoloOK excluded)")
	names := []string{}
	for _, f := range findings {
		assert.Equal(t, "CS006", f.RuleId)
		assert.Equal(t, codelint.FindingSeverityWarn, f.Severity)
		assert.Contains(t, f.Path, "bad.go")
		names = append(names, f.Message)
	}
	assert.Contains(t, names[0]+names[1], `"Status"`)
	assert.Contains(t, names[0]+names[1], `"weekday"`)
}

func TestCS006_PassesGoodFile(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs006/good")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS006())

	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		t.Fatalf("unexpected finding: %+v", f)
	}
}

func TestCS007_FlagsUnprefixedEnumValues(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs007/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS007())

	var findings []codelint.Finding
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		findings = append(findings, f)
	}

	require.Len(t, findings, 6, "expected 6 unsuppressed CS007 findings (one suppressed)")
	for _, f := range findings {
		assert.Equal(t, "CS007", f.RuleId)
		assert.Equal(t, codelint.FindingSeverityWarn, f.Severity)
		assert.Contains(t, f.Path, "bad.go")
	}
}

func TestCS007_PassesGoodFile(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs007/good")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS007())

	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		t.Fatalf("unexpected finding: %+v", f)
	}
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
