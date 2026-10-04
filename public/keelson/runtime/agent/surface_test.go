package agent

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestParseGrantedEntryAndLaunch(t *testing.T) {
	e, err := ParseGrantedEntry("7:doc:observe:get_text,set_text")
	require.NoError(t, err)
	assert.Equal(t, GrantedEntry{Instance: 7, App: "doc", Mode: ModeObserve, Ops: []string{"get_text", "set_text"}}, e)
	assert.True(t, e.Covers("set_text"))
	assert.False(t, e.Covers("export"))

	e, err = ParseGrantedEntry("12:apps.play:act")
	require.NoError(t, err)
	assert.Empty(t, e.Ops)
	assert.True(t, e.Covers("anything"), "an entry naming none covers every operation")

	for _, bad := range []string{"", "7:doc", "x:doc:act", "7:doc:fly"} {
		_, err = ParseGrantedEntry(bad)
		assert.Error(t, err, bad)
	}

	l, err := ParseGrantedLaunch("apps.play:act:2")
	require.NoError(t, err)
	assert.Equal(t, GrantLaunch{App: "apps.play", Mode: ModeAct, Count: 2}, l)
	_, err = ParseGrantedLaunch("apps.play:act")
	assert.Error(t, err)
}

// The surface's status of a cell agrees with what the dispatcher does with
// a call on it: the grant is read back from keelson('agent_grants')'s
// spelling, as a coordinator reads it.
func TestClassifyOperationAgreesWithTheDispatcher(t *testing.T) {
	cat := docOps.Catalog()
	edit := Ceiling{Mode: ModeAct, Effect: app.OperationEffectDocument}
	cases := []struct {
		name    string
		ceiling *Ceiling
		mode    ModeE
		ops     []string
		op      string
		args    string
		want    CellStatusE
		phases  []string
	}{
		{"read under act", nil, ModeAct, nil, "get_text", "{}", CellStatusGranted, []string{"completed"}},
		{"write under act", nil, ModeAct, nil, "set_text", `{"text":"a"}`, CellStatusGranted, []string{"accepted"}},
		{"write under observe", nil, ModeObserve, nil, "set_text", `{"text":"a"}`, CellStatusObserveOnly, []string{"input_required"}},
		{"read under observe", nil, ModeObserve, nil, "get_text", "{}", CellStatusGranted, []string{"completed"}},
		{"op the entry leaves out", nil, ModeAct, []string{"get_text"}, "set_text", `{"text":"a"}`, CellStatusNotGranted, []string{"input_required"}},
		{"above an edit ceiling", &edit, ModeAct, nil, "export", "{}", CellStatusAboveCeiling, []string{"refused"}},
		{"write under an edit ceiling", &edit, ModeAct, nil, "set_text", `{"text":"a"}`, CellStatusGranted, []string{"accepted"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, true)
			g, err := r.cli.Request(context.Background(), GrantRequest{Plan: "p", Ceiling: tc.ceiling,
				Entries: []GrantEntry{{Instance: r.docKey, Mode: tc.mode, Operations: tc.ops}}})
			require.NoError(t, err)
			var e *GrantedEntry
			for _, row := range r.svc.Grants() {
				if row.Task == g.Task {
					require.Len(t, row.Entries, 1)
					pe, perr := ParseGrantedEntry(row.Entries[0])
					require.NoError(t, perr)
					e = &pe
				}
			}
			require.NotNil(t, e)
			spec, ok := cat.Lookup(tc.op)
			require.True(t, ok)
			assert.Equal(t, tc.want, ClassifyOperation(tc.ceiling, e, tc.op, spec.Effect))
			out := r.call(g, "k"+strconv.Itoa(i), tc.op, tc.args)
			assert.Contains(t, tc.phases, out.Phase, out.Reason)
		})
	}
	assert.Equal(t, CellStatusNotGranted, ClassifyOperation(nil, nil, "get_text", app.OperationEffectNone), "a window no grant names")
}

func TestClassifyLaunchAndDesktop(t *testing.T) {
	read := Ceiling{Mode: ModeObserve}
	all := Unlimited()
	assert.Equal(t, CellStatusAboveCeiling, ClassifyLaunch(&read, &GrantLaunch{App: "doc", Count: 1}))
	assert.Equal(t, CellStatusNotGranted, ClassifyLaunch(&all, nil))
	assert.Equal(t, CellStatusGranted, ClassifyLaunch(&all, &GrantLaunch{App: "doc", Count: 1}))

	assert.Equal(t, CellStatusAboveCeiling, ClassifyArrange(&read, ModeAct))
	assert.Equal(t, CellStatusNotGranted, ClassifyArrange(&all, ModeUnspecified))
	assert.Equal(t, CellStatusObserveOnly, ClassifyArrange(&all, ModeObserve))
	assert.Equal(t, CellStatusGranted, ClassifyArrange(nil, ModeAct))

	act := &GrantedEntry{Instance: 7, App: "doc", Mode: ModeAct}
	assert.Equal(t, CellStatusNotGranted, ClassifyWindowVerb(&all, nil))
	assert.Equal(t, CellStatusAboveCeiling, ClassifyWindowVerb(&read, act))
	assert.Equal(t, CellStatusObserveOnly, ClassifyWindowVerb(&all, &GrantedEntry{Instance: 7, Mode: ModeObserve}))
	assert.Equal(t, CellStatusGranted, ClassifyWindowVerb(&all, act))
}

// keelson('agent_grants') carries a task's launches.
func TestGrantsRowCarriesLaunches(t *testing.T) {
	r := newRig(t, true)
	g, err := r.cli.Request(context.Background(), GrantRequest{Plan: "open",
		Launches: []GrantLaunch{{App: string(docAppId), Mode: ModeAct, Count: 2}}})
	require.NoError(t, err)
	for _, row := range r.svc.Grants() {
		if row.Task == g.Task {
			assert.Equal(t, []string{string(docAppId) + ":act:2"}, row.Launches)
			return
		}
	}
	t.Fatal("no grant row for the task")
}
