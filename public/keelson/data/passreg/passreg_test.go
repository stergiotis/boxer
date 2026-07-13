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

// markerFactory builds a Factory whose realised pass appends a marker. Its
// Build accepts a bool binding — true realises the pass, anything else declines
// it (modelling a consumer that lacks the binding a factory needs, e.g. the
// unbound /query path).
func markerFactory(name, marker string, order int) Factory {
	return Factory{
		Name:        name,
		Stage:       StagePreExecute,
		Order:       order,
		Description: "marker factory " + marker,
		Provenance:  "example/" + name,
		Properties:  nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
		Build: func(binding any) (nanopass.Pass, bool) {
			on, ok := binding.(bool)
			if !ok || !on {
				return nanopass.Pass{}, false
			}
			return markerPass(name, marker), true
		},
	}
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

func TestRegisterFactoryValidation(t *testing.T) {
	r := NewRegistry()
	nilBuild := Factory{Name: "F", Stage: StagePreExecute}

	require.Error(t, r.RegisterFactory(Factory{Name: "F", Stage: StageInvalid, Build: markerFactory("F", "f", 1).Build}), "zero stage must be rejected")
	require.Error(t, r.RegisterFactory(Factory{Name: "", Stage: StagePreExecute, Build: markerFactory("F", "f", 1).Build}), "empty name must be rejected")
	require.Error(t, r.RegisterFactory(nilBuild), "nil Build must be rejected")

	require.NoError(t, r.RegisterFactory(markerFactory("F", "f", 100)))
	require.Error(t, r.RegisterFactory(markerFactory("F", "g", 100)), "duplicate factory (stage, name) must be rejected")

	// The (stage, name) namespace is shared with concrete entries: neither kind
	// may shadow the other's name at a stage.
	require.Error(t, r.Register(Entry{Pass: markerPass("F", "e"), Stage: StagePreExecute}), "an entry must not shadow a factory name")
	require.NoError(t, r.Register(Entry{Pass: markerPass("E", "e"), Stage: StagePreExecute}))
	require.Error(t, r.RegisterFactory(markerFactory("E", "f2", 100)), "a factory must not shadow an entry name")
}

func TestApplyBestEffortBoundMergesEntriesAndFactories(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(Entry{Pass: markerPass("Concrete", "concrete"), Stage: StagePreExecute, Order: 100}))
	require.NoError(t, r.RegisterFactory(markerFactory("Late", "late", 200)))

	// An accepting binding realises the factory; both units apply, concrete
	// (100) before late (200).
	out := r.ApplyBestEffortBound(StagePreExecute, "SELECT 1", true, zerolog.Nop())
	require.Contains(t, out, "/*concrete*/")
	require.Contains(t, out, "/*late*/")
	require.Less(t, strings.Index(out, "/*concrete*/"), strings.Index(out, "/*late*/"), "units apply in (Order, Name) order")

	// A declining binding skips the factory; only the concrete entry applies.
	out = r.ApplyBestEffortBound(StagePreExecute, "SELECT 1", false, zerolog.Nop())
	require.Contains(t, out, "/*concrete*/")
	require.NotContains(t, out, "/*late*/", "a factory that declines the binding must be skipped")

	// Unbound ApplyBestEffort never realises factories at all.
	out = r.ApplyBestEffort(StagePreExecute, "SELECT 1", zerolog.Nop())
	require.Contains(t, out, "/*concrete*/")
	require.NotContains(t, out, "/*late*/", "ApplyBestEffort must not apply factories")
}

func TestCatalogIncludesFactoriesWithLateBound(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(Entry{Pass: markerPass("Concrete", "c"), Stage: StagePreExecute, Order: 100, Description: "c", Provenance: "p"}))
	require.NoError(t, r.RegisterFactory(markerFactory("Late", "l", 200)))

	rows := r.Catalog()
	require.Len(t, rows, 2, "catalog spans both entries and factories")
	require.Equal(t, "Concrete", rows[0].Name)
	require.False(t, rows[0].LateBound, "a concrete entry is not late-bound")
	require.Equal(t, "Late", rows[1].Name)
	require.True(t, rows[1].LateBound, "a factory descriptor is late-bound")

	// The typed views stay split: Entries is concrete-only, Factories is the
	// factory view.
	require.Len(t, r.Entries(StagePreExecute), 1)
	fs := r.Factories(StagePreExecute)
	require.Len(t, fs, 1)
	require.Equal(t, "Late", fs[0].Name)
}

func TestStageString(t *testing.T) {
	require.Equal(t, "pre-execute", StagePreExecute.String())
	require.Equal(t, "invalid", StageInvalid.String())
	require.Equal(t, "invalid", StageE(99).String())
}
