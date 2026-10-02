package appops

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

type doc struct {
	text   string
	locked bool
}

type docSnap struct {
	text   string
	locked bool
}

type setTextArgs struct{ Text string }
type textResult struct{ Text string }

func testSet() *Set[*doc, docSnap] {
	s := NewSet(func(d *doc) docSnap { return docSnap{text: d.text, locked: d.locked} })
	s.Resource("text", "the text", func(d *doc) any { return d.text })
	Command(s, app.OperationSpec{Name: "set_text", Version: 1, Summary: "replace the text",
		Effect: app.OperationEffectDocument, Writes: []string{"text"}, Agents: true},
		func(d *doc, call app.OperationCall, in setTextArgs) (None, error) {
			if d.locked {
				return None{}, app.RefuseOperation("locked")
			}
			d.text = in.Text
			return None{}, nil
		})
	Query(s, app.OperationSpec{Name: "get_text", Version: 1, Summary: "read the text", Reads: []string{"text"}},
		func(sn docSnap, in None) (textResult, error) { return textResult{Text: sn.text}, nil })
	s.Available("get_text", func(sn docSnap) (bool, string) { return !sn.locked, "locked" })
	return s
}

func TestCatalogTypesComeFromTheHandlers(t *testing.T) {
	c := testSet().Catalog()
	require.NoError(t, c.Validate())
	set, ok := c.Lookup("set_text")
	require.True(t, ok)
	assert.Equal(t, app.OperationClassCommand, set.Class)
	assert.Equal(t, "setTextArgs", set.Args.Name())
	assert.Nil(t, set.Result, "None means no result")
	get, _ := c.Lookup("get_text")
	assert.Equal(t, app.OperationClassQuery, get.Class)
	assert.Equal(t, app.OperationEffectNone, get.Effect)
	assert.Nil(t, get.Args)
}

func TestCommandAndQueryThroughTheUntypedInterface(t *testing.T) {
	d := &doc{text: "a"}
	h := testSet().Bind(d)
	assert.Equal(t, "a", h.ResourceValue("text"))

	args, err := buscodec.Encode(setTextArgs{Text: "b"})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:1"}, "set_text", args)
	require.NoError(t, err)
	assert.Equal(t, "b", d.text)

	snap := h.Snapshot()
	d.text = "c" // a snapshot does not see later changes
	raw, err := snap.Query("get_text", nil)
	require.NoError(t, err)
	res, err := buscodec.Decode[textResult](raw)
	require.NoError(t, err)
	assert.Equal(t, "b", res.Text)

	d.locked = true
	_, err = h.ApplyCommand(app.OperationCall{}, "set_text", args)
	var refusal *app.OperationRefusal
	require.True(t, errors.As(err, &refusal))
	assert.False(t, refusal.Conflict)

	locked := h.Snapshot()
	ok, reason := locked.Available("get_text")
	assert.False(t, ok)
	assert.Equal(t, "locked", reason)
	_, err = locked.Query("get_text", nil)
	require.True(t, errors.As(err, &refusal))

	_, err = h.ApplyCommand(app.OperationCall{}, "nope", nil)
	require.Error(t, err)
}

// An external read runs over the snapshot like a query and receives the
// call, so the app can check the on-behalf-of context against its agent
// limits.
func TestAnExternalReadReceivesTheCall(t *testing.T) {
	s := testSet()
	ExternalRead(s, app.OperationSpec{Name: "probe", Version: 1, Summary: "probe outside the app"},
		func(sn docSnap, call app.OperationCall, in None) (textResult, error) {
			if call.OnBehalfOf == nil {
				return textResult{}, app.RefuseOperation("an agent's call")
			}
			return textResult{Text: sn.text + "@" + call.OnBehalfOf.Task}, nil
		})
	c := s.Catalog()
	require.NoError(t, c.Validate())
	spec, _ := c.Lookup("probe")
	assert.Equal(t, app.OperationClassExternalRead, spec.Class)
	assert.Equal(t, app.OperationEffectNone, spec.Effect)

	snap := s.Bind(&doc{text: "a"}).Snapshot()
	raw, confined, err := snap.ExternalRead(app.OperationCall{OnBehalfOf: &app.OnBehalfOf{Task: "t"}}, "probe", nil)
	require.NoError(t, err)
	assert.False(t, confined)
	res, err := buscodec.Decode[textResult](raw)
	require.NoError(t, err)
	assert.Equal(t, "a@t", res.Text)

	_, _, err = snap.ExternalRead(app.OperationCall{}, "probe", nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	_, _, err = snap.ExternalRead(app.OperationCall{}, "get_text", nil)
	require.Error(t, err, "a query is not an external read")
}

type sealedResult struct{ Sealed bool }

func (inst sealedResult) ResultConfined() (confined bool) { return inst.Sealed }

// A result that says it carries confined content labels the read so.
func TestAnExternalReadResultCanBeConfined(t *testing.T) {
	s := testSet()
	ExternalRead(s, app.OperationSpec{Name: "probe", Version: 1, Summary: "probe outside the app"},
		func(sn docSnap, call app.OperationCall, in setTextArgs) (sealedResult, error) {
			return sealedResult{Sealed: in.Text == "sealed"}, nil
		})
	snap := s.Bind(&doc{}).Snapshot()
	for text, want := range map[string]bool{"sealed": true, "open": false} {
		args, err := buscodec.Encode(setTextArgs{Text: text})
		require.NoError(t, err)
		_, confined, err := snap.ExternalRead(app.OperationCall{}, "probe", args)
		require.NoError(t, err)
		assert.Equal(t, want, confined, text)
	}
}
