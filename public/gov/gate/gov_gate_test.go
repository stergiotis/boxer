package gate

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStep struct {
	name   string
	status StatusE
	err    error
	ran    *[]string
}

var _ StepI = (*fakeStep)(nil)

func (inst *fakeStep) Name() (s string) { return inst.name }

func (inst *fakeStep) Run(ctx context.Context, cfg Config, w io.Writer) (status StatusE, err error) {
	if inst.ran != nil {
		*inst.ran = append(*inst.ran, inst.name)
	}
	fmt.Fprintf(w, "%s output\n", inst.name)
	return inst.status, inst.err
}

func TestConfigDefaults(t *testing.T) {
	var c Config
	assert.Equal(t, ".", c.root())
	assert.Equal(t, "tags", c.tagsFile())
	assert.Equal(t, []string{"."}, c.docRoots())
	assert.Equal(t, []string{"./public/..."}, c.codePatterns())

	c = Config{Root: "/x", TagsFile: "t", DocRoots: []string{"d"}, CodePatterns: []string{"./p/..."}}
	assert.Equal(t, "/x", c.root())
	assert.Equal(t, "t", c.tagsFile())
	assert.Equal(t, []string{"d"}, c.docRoots())
	assert.Equal(t, []string{"./p/..."}, c.codePatterns())
}

func TestRunExecutesEveryStepInOrder(t *testing.T) {
	ran := make([]string, 0, 3)
	steps := []StepI{
		&fakeStep{name: "a", status: StatusPass, ran: &ran},
		&fakeStep{name: "b", status: StatusWarn, ran: &ran},
		&fakeStep{name: "c", status: StatusPass, ran: &ran},
	}
	var buf strings.Builder
	rep := Run(context.Background(), Config{}, steps, &buf)

	assert.Equal(t, []string{"a", "b", "c"}, ran)
	require.Len(t, rep.Steps, 3)
	assert.False(t, rep.Failed())
	assert.Contains(t, buf.String(), "=== a ===")
	assert.Contains(t, buf.String(), "b output")
}

// A step that cannot run must not stop the others: the gate's value is a
// complete picture, not the first problem it hits.
func TestRunContinuesPastAStepThatCannotRun(t *testing.T) {
	ran := make([]string, 0, 3)
	steps := []StepI{
		&fakeStep{name: "a", status: StatusPass, ran: &ran},
		&fakeStep{name: "boom", err: eh.Errorf("no such file"), ran: &ran},
		&fakeStep{name: "c", status: StatusPass, ran: &ran},
	}
	var buf strings.Builder
	rep := Run(context.Background(), Config{}, steps, &buf)

	assert.Equal(t, []string{"a", "boom", "c"}, ran)
	assert.True(t, rep.Failed(), "a step that could not run is not a passing step")
	assert.Equal(t, StatusFail, rep.Steps[1].Status)
	assert.Error(t, rep.Steps[1].Err)
	assert.Contains(t, buf.String(), "step could not run")
}

func TestRunHonoursStepFilter(t *testing.T) {
	ran := make([]string, 0, 3)
	steps := []StepI{
		&fakeStep{name: "a", status: StatusPass, ran: &ran},
		&fakeStep{name: "b", status: StatusPass, ran: &ran},
		&fakeStep{name: "c", status: StatusPass, ran: &ran},
	}
	var buf strings.Builder
	rep := Run(context.Background(), Config{Steps: []string{"b"}}, steps, &buf)

	assert.Equal(t, []string{"b"}, ran)
	assert.Len(t, rep.Steps, 1)
}

func TestReportFailedSemantics(t *testing.T) {
	pass := Report{Steps: []StepResult{{Name: "a", Status: StatusPass}}}
	warn := Report{Steps: []StepResult{{Name: "a", Status: StatusWarn}}}
	skip := Report{Steps: []StepResult{{Name: "a", Status: StatusSkip}}}
	fail := Report{Steps: []StepResult{{Name: "a", Status: StatusFail}}}

	assert.False(t, pass.Failed())
	assert.False(t, warn.Failed(), "a warn is visible but non-blocking")
	assert.False(t, skip.Failed())
	assert.True(t, fail.Failed())
}

func TestWriteTrailerNamesFailingAndWarningSteps(t *testing.T) {
	rep := Report{
		Steps: []StepResult{
			{Name: "buildtags", Status: StatusPass},
			{Name: "doclint", Status: StatusFail},
			{Name: "codelint", Status: StatusWarn},
			{Name: "rustfmt", Status: StatusSkip},
		},
	}
	var buf strings.Builder
	rep.WriteTrailer(&buf)
	out := buf.String()

	assert.Contains(t, out, "=== summary ===")
	assert.Contains(t, out, "exit 1")
	assert.Contains(t, out, "failing: doclint")
	assert.Contains(t, out, "warnings: codelint")
	assert.Contains(t, out, "skipped: rustfmt")
	// The name column is padded to the widest step name.
	assert.Contains(t, out, "buildtags  pass")
}

func TestWriteTrailerCleanRunExitsZero(t *testing.T) {
	rep := Report{Steps: []StepResult{{Name: "a", Status: StatusPass}}}
	var buf strings.Builder
	rep.WriteTrailer(&buf)
	assert.Contains(t, buf.String(), "exit 0")
	assert.NotContains(t, buf.String(), "failing:")
}

func TestValidateStepNamesE(t *testing.T) {
	steps := DefaultSteps()
	assert.NoError(t, ValidateStepNamesE(steps, nil))
	assert.NoError(t, ValidateStepNamesE(steps, []string{"doclint", "buildtags"}))

	err := ValidateStepNamesE(steps, []string{"doclint", "nosuch"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown gate step")
}

// The published step list is the artifact this package exists to stop
// consumers from copying; a change to it is a change to the contract.
func TestDefaultStepsIsThePublishedList(t *testing.T) {
	assert.Equal(t,
		[]string{"buildtags", "doclint", "entry-points", "codelint"},
		stepNames(DefaultSteps()))
}

func TestStepBuildTagsReportsAMissingManifest(t *testing.T) {
	var buf strings.Builder
	_, err := NewStepBuildTags().Run(context.Background(),
		Config{Root: t.TempDir()}, &buf)
	require.Error(t, err, "an unreadable manifest is a step that could not run, not a clean one")
}
