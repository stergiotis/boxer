package gloss

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tideGloss stands in for a consumer's domain gloss: a rendering boxer has no
// business knowing about, registered from outside this package.
type tideGloss struct{ mt string }

func (g tideGloss) MediaType() string  { return g.mt }
func (tideGloss) Doc() string          { return "a tide state" }
func (tideGloss) Params() []ParamSpec  { return nil }
func (tideGloss) Affinities() []string { return nil }
func (g tideGloss) Bind(params map[string]string) (InstanceI, error) {
	return &tideInstance{gloss: g, params: params}, nil
}

type tideInstance struct {
	gloss  tideGloss
	params map[string]string
}

func (inst *tideInstance) Gloss() GlossI                     { return inst.gloss }
func (inst *tideInstance) Params() map[string]string         { return inst.params }
func (inst *tideInstance) Accepts(ValueKindE) (bool, string) { return true, "" }
func (inst *tideInstance) Inline(cell CellI) Inline          { return Inline{Text: "tide " + cell.Text()} }

// withExtras runs fn with the process-wide extras replaced, so a test can
// register without leaking into the ones that assert Default's built-in
// membership.
//
// The restore is deferred here rather than through t.Cleanup: a caller
// asserting that the registration is gone runs before its own cleanup would,
// and a helper whose effect outlives its call is a trap. The defer still
// covers a panic inside fn.
func withExtras(t *testing.T, gs []GlossI, fn func()) {
	t.Helper()
	extraMu.Lock()
	saved := extras
	extras = nil
	extraMu.Unlock()
	defer func() {
		extraMu.Lock()
		extras = saved
		extraMu.Unlock()
	}()
	for _, g := range gs {
		RegisterDefault(g)
	}
	fn()
}

// TestRegisterDefaultReachesEveryHost is the point of the extension point: a
// consumer's gloss must be resolvable through Default, because that is what
// every host calls. Before it existed a consumer could build a catalog with
// NewCatalog and nothing would ever look in it.
func TestRegisterDefaultReachesEveryHost(t *testing.T) {
	withExtras(t, []GlossI{tideGloss{mt: "sailing/tidestate"}}, func() {
		g, ok := Default().Lookup("sailing/tidestate")
		require.True(t, ok, "a registered gloss resolves through Default")
		assert.Equal(t, "sailing/tidestate", g.MediaType())

		d, declared := Default().ParseColumn("t@sailing/tidestate")
		require.True(t, declared)
		require.Empty(t, d.Reason)
		assert.Equal(t, Inline{Text: "tide flood"}, d.Instance.Inline(txt("flood")))

		assert.Len(t, RegisteredDefaults(), 1)
	})
	// The registration is undone, so nothing leaks between tests.
	_, ok := Default().Lookup("sailing/tidestate")
	assert.False(t, ok)
}

// TestRegisterDefaultAppendsAfterBuiltins pins the ordering rule: registration
// order is affinity order, so an extension must not be able to take a column a
// built-in would have claimed.
func TestRegisterDefaultAppendsAfterBuiltins(t *testing.T) {
	withExtras(t, []GlossI{tideGloss{mt: "sailing/tidestate"}}, func() {
		var order []string
		for g := range Default().All() {
			order = append(order, g.MediaType())
		}
		require.NotEmpty(t, order)
		assert.Equal(t, "sailing/tidestate", order[len(order)-1], "extras come last")
		assert.Equal(t, MediaTypeRaw, order[len(order)-2], "the built-in tail is undisturbed")
	})
}

// TestRegisterDefaultRefusesADuplicate pins that two packages cannot claim one
// media type: it would otherwise resolve by import order, which neither of
// them can see.
func TestRegisterDefaultRefusesADuplicate(t *testing.T) {
	withExtras(t, nil, func() {
		assert.PanicsWithValue(t,
			"gloss: RegisterDefault("+MediaTypeVelocity+"): a built-in gloss already holds that media type",
			func() { RegisterDefault(tideGloss{mt: MediaTypeVelocity}) },
			"a built-in cannot be shadowed")

		RegisterDefault(tideGloss{mt: "sailing/tidestate"})
		assert.PanicsWithValue(t,
			"gloss: RegisterDefault(sailing/tidestate): already registered by another package",
			func() { RegisterDefault(tideGloss{mt: "sailing/tidestate"}) })
	})
}

// TestRegisteredDefaultsIsACopy pins that a caller cannot mutate the registry
// through the listing it is handed.
func TestRegisteredDefaultsIsACopy(t *testing.T) {
	withExtras(t, []GlossI{tideGloss{mt: "sailing/tidestate"}}, func() {
		got := RegisteredDefaults()
		require.Len(t, got, 1)
		got[0] = tideGloss{mt: "other/thing"}
		assert.Equal(t, "sailing/tidestate", RegisteredDefaults()[0].MediaType())
	})
}
