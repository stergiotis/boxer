package passreg

import (
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// markerPass appends a block-comment marker to the body — enough to observe
// application order without parsing.
func markerPass(name, marker string) nanopass.Pass {
	return nanopass.LiftBodyPass(name, func(sql string) (string, error) {
		return sql + " /*" + marker + "*/", nil
	}, nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody})
}

func failingPass(name string) nanopass.Pass {
	return nanopass.LiftBodyPass(name, func(sql string) (string, error) {
		return "", errors.New("boom")
	}, nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody})
}

func TestRegisterValidation(t *testing.T) {
	r := NewRegistry()

	require.Error(t, r.Register(Entry{Pass: markerPass("P", "p"), Stage: StageInvalid}), "zero stage must be rejected")
	require.Error(t, r.Register(Entry{Pass: markerPass("P", "p"), Stage: StageE(99)}), "unknown stage must be rejected")
	require.Error(t, r.Register(Entry{Pass: nanopass.Pass{}, Stage: StagePreExecute}), "empty pass name must be rejected")
	require.Error(t, r.Register(Entry{Pass: nanopass.Pass{Name: "NoApply"}, Stage: StagePreExecute}), "nil Apply must be rejected")

	require.NoError(t, r.Register(Entry{Pass: markerPass("P", "p"), Stage: StagePreExecute}))
	require.Error(t, r.Register(Entry{Pass: markerPass("P", "q"), Stage: StagePreExecute}), "duplicate (stage, name) must be rejected")
	require.Len(t, r.Entries(StagePreExecute), 1)
}

func TestEntriesDeterministicOrder(t *testing.T) {
	r := NewRegistry()
	// Registered deliberately out of order; ties on Order break by name.
	require.NoError(t, r.Register(Entry{Pass: markerPass("Zeta", "z"), Stage: StagePreExecute, Order: 200}))
	require.NoError(t, r.Register(Entry{Pass: markerPass("Beta", "b"), Stage: StagePreExecute, Order: 100}))
	require.NoError(t, r.Register(Entry{Pass: markerPass("Alpha", "a"), Stage: StagePreExecute, Order: 100}))

	es := r.Entries(StagePreExecute)
	require.Len(t, es, 3)
	require.Equal(t, "Alpha", es[0].Pass.Name)
	require.Equal(t, "Beta", es[1].Pass.Name)
	require.Equal(t, "Zeta", es[2].Pass.Name)

	all := r.All()
	require.Len(t, all, 3)
	require.Equal(t, "Alpha", all[0].Pass.Name)
}

func TestApplyBestEffortSkipsFailingEntry(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(Entry{Pass: markerPass("First", "first"), Stage: StagePreExecute, Order: 1}))
	require.NoError(t, r.Register(Entry{Pass: failingPass("Broken"), Stage: StagePreExecute, Order: 2}))
	require.NoError(t, r.Register(Entry{Pass: markerPass("Last", "last"), Stage: StagePreExecute, Order: 3}))

	out := r.ApplyBestEffort(StagePreExecute, "SELECT 1", zerolog.Nop())
	require.Contains(t, out, "/*first*/", "pass before the failure must apply")
	require.Contains(t, out, "/*last*/", "pass after the failure must still apply")
	idxFirst := strings.Index(out, "/*first*/")
	idxLast := strings.Index(out, "/*last*/")
	require.Less(t, idxFirst, idxLast, "entries must apply in Order order")
}

func TestApplyBestEffortEmptyStageIsIdentity(t *testing.T) {
	r := NewRegistry()
	require.Equal(t, "SELECT 1", r.ApplyBestEffort(StagePreExecute, "SELECT 1", zerolog.Nop()))
}

func TestComposeIsStrict(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(Entry{Pass: markerPass("First", "first"), Stage: StagePreExecute, Order: 1}))
	require.NoError(t, r.Register(Entry{Pass: failingPass("Broken"), Stage: StagePreExecute, Order: 2}))

	_, err := r.Compose("preexec", StagePreExecute).Run("SELECT 1")
	require.Error(t, err, "a strict composition must surface the first failing pass")
}

func TestComposeEmptyIsPassThrough(t *testing.T) {
	r := NewRegistry()
	out, err := r.Compose("preexec", StagePreExecute).Run("SELECT 1")
	require.NoError(t, err)
	require.Equal(t, "SELECT 1", out)
}

func TestDefaultWrappers(t *testing.T) {
	// Default is process-global; use a name no other test registers.
	require.NoError(t, Register(Entry{Pass: markerPass("DefaultWrapperProbe", "probe"), Stage: StagePreExecute, Order: 999}))
	found := false
	for _, e := range Entries(StagePreExecute) {
		if e.Pass.Name == "DefaultWrapperProbe" {
			found = true
		}
	}
	require.True(t, found)
}

func TestStageString(t *testing.T) {
	require.Equal(t, "pre-execute", StagePreExecute.String())
	require.Equal(t, "invalid", StageInvalid.String())
	require.Equal(t, "invalid", StageE(99).String())
}
