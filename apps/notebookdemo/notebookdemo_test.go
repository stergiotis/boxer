package notebookdemo

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// fakeServer answers the report sample's records with what it stores, and
// the host's own notebook with nothing: those records fail, which is enough
// to see a run end.
func fakeServer(t *testing.T) mdnotebook.RunFunc {
	t.Helper()
	text, err := fs.ReadFile(samples.FS(), "report.md")
	require.NoError(t, err)
	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	return func(_ context.Context, statement string) ([]byte, error) {
		sent := strings.TrimSpace(strings.TrimSuffix(statement, "FORMAT ArrowStream"))
		for i := range nb.Cells {
			cell := &nb.Cells[i]
			if cell.Kind == mdnotebook.CellKindRecord && strings.HasSuffix(sent, strings.TrimSpace(cell.Body)) {
				ref, _ := nb.Record(cell.Name)
				return mdnotebook.ReadBlob(samples.FS(), ref.Blob)
			}
		}
		return nil, errors.New("this stand-in answers only the report sample")
	}
}

func query[T any](t *testing.T, h app.OperationsHandlerI, op string) T {
	t.Helper()
	res, err := h.Snapshot().Query(op, nil)
	require.NoError(t, err)
	out, err := buscodec.Decode[T](res)
	require.NoError(t, err)
	return out
}

func command(t *testing.T, h app.OperationsHandlerI, op string, args any) error {
	t.Helper()
	raw, err := buscodec.Encode(args)
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, op, raw)
	return err
}

// The host builds both panes under composed identities, reads their state,
// and runs one through its own catalog.
func TestTheHostEmbedsBothWays(t *testing.T) {
	t.Cleanup(egui2test.Install())
	inst := newApp()
	inst.run = fakeServer(t)
	h := inst.Operations()
	ps := query[Panes](t, h, opGetPanes)
	require.Len(t, ps.Panes, 2)
	for _, p := range ps.Panes {
		assert.Empty(t, p.Error, p.Key)
		assert.Equal(t, string(ManifestId)+"#"+p.Key, p.Identity)
		assert.Equal(t, "stored", p.Live.Reading)
		assert.Equal(t, "read", p.Live.Class)
	}
	assert.True(t, ps.Panes[0].Shown)

	require.NoError(t, command(t, h, opShowPane, PaneArgs{Key: keyOwn}))
	assert.Equal(t, 1, inst.shown)
	require.Error(t, command(t, h, opShowPane, PaneArgs{Key: "nope"}))

	require.NoError(t, command(t, h, opRunPane, PaneArgs{Key: keyRegistered}))
	pane := inst.panes[0].pane
	deadline := time.Now().Add(10 * time.Second)
	for {
		pane.Poll()
		if started, ended := pane.Runs(); started > 0 && ended == started {
			break
		}
		require.True(t, time.Now().Before(deadline), "the run did not end in time")
		time.Sleep(2 * time.Millisecond)
	}
	ps = query[Panes](t, h, opGetPanes)
	assert.Equal(t, "live", ps.Panes[0].Live.Reading, ps.Panes[0].Live.LastErr)
	assert.Equal(t, 5, ps.Panes[0].Live.Records)
}

// The host's manifest carries what a pane needs and nothing more.
func TestTheHostDeclaresThePanesCaps(t *testing.T) {
	assert.Equal(t, notebook.EmbedCaps(), manifest.Caps)
	require.NoError(t, manifest.Operations.Validate())
}

func TestHelpCorpusIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(manifest.Id)
	require.True(t, ok)
	require.Empty(t, b.Validate())
	for _, info := range b.Docs() {
		doc, _, parsed := b.Doc(info.Path)
		require.Truef(t, parsed, "doc %q must parse", info.Path)
		require.Emptyf(t, doc.Dropped(), "doc %q loses content in the lowering: %+v", info.Path, doc.Dropped())
	}
}

var helpOperationRow = regexp.MustCompile("(?m)^\\| `([a-z][a-z0-9_]*)` \\|")

func TestHelpNamesEveryOperation(t *testing.T) {
	raw, err := fs.ReadFile(helpFS, "help/overview.md")
	require.NoError(t, err)
	documented := map[string]int{}
	for _, m := range helpOperationRow.FindAllStringSubmatch(string(raw), -1) {
		documented[m[1]]++
	}
	for _, spec := range manifest.Operations.Operations {
		assert.Equalf(t, 1, documented[spec.Name], "the operation %s has one row in the overview", spec.Name)
		delete(documented, spec.Name)
	}
	assert.Empty(t, documented)
}
