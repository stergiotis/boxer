package appops

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// A component whose types are known only at run time serves its bytes; the
// spec it is declared with carries them, and its availability reads what
// the mount captured.
func TestMountedRawOperations(t *testing.T) {
	s := NewSet(func(d *doc) docSnap { return docSnap{text: d.text} })
	s.Mount("comp", func(d *doc) any { return d.text })
	argsT := reflect.TypeFor[setTextArgs]()
	MountedCommandRaw(s, app.OperationSpec{Name: "comp_set", Version: 1, Summary: "set through the component",
		Effect: app.OperationEffectDocument, Writes: []string{"text"}, Args: argsT, Agents: true},
		func(d *doc, call app.OperationCall, args []byte) ([]byte, error) {
			in, err := buscodec.Decode[setTextArgs](args)
			if err != nil {
				return nil, err
			}
			d.text = in.Text
			return nil, nil
		})
	s.Resource("text", "the text", func(d *doc) any { return d.text })
	MountedQueryRaw(s, "comp", app.OperationSpec{Name: "comp_get", Version: 1, Summary: "read through the component",
		Reads: []string{"text"}, Result: reflect.TypeFor[textResult]()},
		func(v any, args []byte) ([]byte, error) { return buscodec.Encode(textResult{Text: v.(string)}) })
	s.MountedAvailable("comp_get", "comp", func(v any) (bool, string) { return v.(string) != "", "nothing captured" })
	require.NoError(t, s.Catalog().Validate())
	spec, ok := s.Catalog().Lookup("comp_set")
	require.True(t, ok)
	assert.Equal(t, app.OperationClassCommand, spec.Class)
	assert.Equal(t, argsT, spec.Args)

	d := &doc{}
	h := s.Bind(d)
	ok, reason := h.Snapshot().Available("comp_get")
	assert.False(t, ok)
	assert.Equal(t, "nothing captured", reason)
	args, err := buscodec.Encode(setTextArgs{Text: "hi"})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{}, "comp_set", args)
	require.NoError(t, err)
	raw, err := h.Snapshot().Query("comp_get", nil)
	require.NoError(t, err)
	out, err := buscodec.Decode[textResult](raw)
	require.NoError(t, err)
	assert.Equal(t, "hi", out.Text)
}
