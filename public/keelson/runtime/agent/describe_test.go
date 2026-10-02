package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

type notes struct{ text string }

type setNoteArgs struct {
	Text string `desc:"the new note"`
}

type noteSnap struct{ text string }

func notesSet() *appops.Set[*notes, noteSnap] {
	s := appops.NewSet(func(n *notes) noteSnap { return noteSnap{text: n.text} })
	s.Resource("note", "the note text", func(n *notes) any { return n.text })
	appops.Command(s, app.OperationSpec{Name: "set_note", Version: 1, Summary: "replace the note",
		Effect: app.OperationEffectDocument, Writes: []string{"note"}, Agents: true},
		func(n *notes, call app.OperationCall, in setNoteArgs) (appops.None, error) {
			n.text = in.Text
			return appops.None{}, nil
		})
	appops.Query(s, app.OperationSpec{Name: "debug_dump", Version: 1, Summary: "dump internals", Reads: []string{"note"}},
		func(sn noteSnap, in appops.None) (appops.None, error) { return appops.None{}, nil })
	return s
}

func registry(t *testing.T) *app.Registry {
	t.Helper()
	r := app.NewRegistry()
	m := app.Manifest{Id: "github.com/x/apps/notes", Display: "Notes", Summary: "keep a note",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.AllTopics[0]}, Operations: notesSet().Catalog()}
	require.NoError(t, r.RegisterFactory(m, func() (app.AppI, error) { return nil, nil }))
	plain := app.Manifest{Id: "github.com/x/apps/plain", Display: "Plain", Summary: "no catalog",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.AllTopics[0]}}
	require.NoError(t, r.RegisterFactory(plain, func() (app.AppI, error) { return nil, nil }))
	return r
}

func serve(t *testing.T) *Client {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), Config{Registry: registry(t)})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli := NewClient(bus.NewClient("test.coordinator", ClientCaps("test: drive apps")))
	cli.Timeout = 5 * time.Second
	return cli
}

func TestDescribeListsOnlyAgentOperationsWithoutSchemas(t *testing.T) {
	cli := serve(t)
	apps, err := cli.Describe(context.Background(), DescribeRequest{})
	require.NoError(t, err)
	require.Len(t, apps, 1, "an app without a catalog is not listed")
	assert.Equal(t, "Notes", apps[0].Display)
	require.Len(t, apps[0].Operations, 1, "debug_dump is not exposed to agents")
	op := apps[0].Operations[0]
	assert.Equal(t, "set_note", op.Name)
	assert.Equal(t, "command", op.Class)
	assert.Equal(t, "document", op.Effect)
	assert.Empty(t, op.ArgsSchema, "schemas load on demand")
	require.Len(t, apps[0].Resources, 1)
}

func TestDescribeOneOperationCarriesItsSchemas(t *testing.T) {
	cli := serve(t)
	apps, err := cli.Describe(context.Background(), DescribeRequest{App: "notes", Operation: "set_note"})
	require.NoError(t, err)
	require.Len(t, apps, 1)
	op := apps[0].Operations[0]
	assert.Contains(t, op.ArgsSchema, `"text":{"description":"the new note","type":"string"}`)
	assert.Contains(t, op.ResultSchema, `"properties":{}`)

	_, err = cli.Describe(context.Background(), DescribeRequest{App: "notes", Operation: "debug_dump"})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "an operation not exposed to agents is not described")
	_, err = cli.Describe(context.Background(), DescribeRequest{Operation: "set_note"})
	require.True(t, errors.As(err, &refused), "naming an operation needs the app")
}

func TestDescribeSearch(t *testing.T) {
	cli := serve(t)
	apps, err := cli.Describe(context.Background(), DescribeRequest{Search: "REPLACE"})
	require.NoError(t, err)
	require.Len(t, apps, 1)
	apps, err = cli.Describe(context.Background(), DescribeRequest{Search: "nothing like this"})
	require.NoError(t, err)
	assert.Empty(t, apps)
}

func TestDescribeNeedsTheCap(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), Config{Registry: registry(t)})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli := NewClient(bus.NewClient("test.nocaps", nil))
	_, err = cli.Describe(context.Background(), DescribeRequest{})
	require.ErrorIs(t, err, inprocbus.ErrPermissionViolation)
}
