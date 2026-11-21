package extbin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The programs this module declares name only declared variables.
func TestDeclaredProgramsNameDeclaredVariables(t *testing.T) {
	CheckDeclarations(t)
}

func TestDeclarationErrorsReportEveryOffender(t *testing.T) {
	p := &Program{
		Name:             "boxer-decl-fixture",
		Kind:             Host,
		OverrideEnv:      "EXTBIN_TEST_NEVER_DECLARED",
		InheritSensitive: []string{"EXTBIN_TEST_ALSO_NEVER_DECLARED", "BOXER_FAKE_OV", "EXTBIN_TEST_KEPT"},
	}
	errs := p.declarationErrors()
	require.Len(t, errs, 3, "an undeclared override, an undeclared and a non-Sensitive InheritSensitive entry; the Sensitive one passes")
	for _, err := range errs {
		assert.ErrorIs(t, err, ErrUndeclaredVariable)
	}
}

// An undeclared override fails resolution with an error, not a panic.
func TestUndeclaredOverrideFailsResolution(t *testing.T) {
	p := &Program{Name: "boxer-decl-fixture-run", Kind: Host, OverrideEnv: "EXTBIN_TEST_NEVER_DECLARED"}
	_, err := p.Command(context.Background(), Opts{})
	require.ErrorIs(t, err, ErrUndeclaredVariable)
	_, available := p.Resolve()
	assert.False(t, available)

	tc := &Program{Name: "boxer-decl-fixture-go", Kind: GoToolchain, OverrideEnv: "EXTBIN_TEST_NEVER_DECLARED"}
	_, err = tc.Command(context.Background(), Opts{})
	require.True(t, errors.Is(err, ErrUndeclaredVariable))
}

type recordingReporter struct{ failed bool }

func (inst *recordingReporter) Helper() {}
func (inst *recordingReporter) Errorf(format string, args ...any) {
	inst.failed = true
}

func TestCheckDeclarationsFailsTheTest(t *testing.T) {
	p := Declare(Program{Name: "boxer-decl-fixture-registered", Kind: Host, OverrideEnv: "EXTBIN_TEST_NEVER_DECLARED"})
	t.Cleanup(func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		delete(registry, p.Name)
	})
	r := &recordingReporter{}
	CheckDeclarations(r)
	assert.True(t, r.failed)
}
