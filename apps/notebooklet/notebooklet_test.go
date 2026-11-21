package notebooklet

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// Every notebook of every registered book reads, composes its buffer and
// classifies read: the pane is built for it (ADR-0380 §SD6). This is the
// gate that keeps a notebook that would be refused off main.
func TestCorpus_everyNotebookRunsLive(t *testing.T) {
	books := snapshotBooks()
	require.NotEmpty(t, books)
	for _, b := range books {
		defs, errs := parseBook(b)
		require.Empty(t, errs, "book %s", b.id)
		require.NotEmpty(t, defs, "book %s holds no notebook", b.id)
		for _, def := range defs {
			_, err := notebook.NewEmbedded(def.Text, notebook.EmbedConfig{Host: def.Manifest.Id, Key: def.Slug, Files: def.Files, OwnWindow: true})
			assert.NoError(t, err, "%s/%s", b.id, def.Path)
		}
	}
}

// The minted manifests are notebooklets, and declare what the pane needs
// and the hand-over to the notebook window: nothing of collection or the
// store.
func TestMintedManifestsHoldToThePanesCaps(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), snapshotBooks())
	require.Empty(t, errs)
	require.Equal(t, 3, minted, "the three samples")
	allowed := map[string]bool{windowopen.OpenSubject: true}
	for _, cp := range notebook.EmbedCaps() {
		allowed[cp.Pattern] = true
	}
	for _, slug := range []string{"minimal", "report", "renderers"} {
		m, ok := reg.LookupManifest(app.AppIdT(idPrefix + slug))
		require.True(t, ok, slug)
		assert.Equal(t, app.KindNotebooklet, m.Kind)
		assert.LessOrEqual(t, len([]rune(m.Summary)), 72, "the launcher's summary budget")
		assert.Equal(t, slug, Slug(m.Id))
		require.NoError(t, m.Operations.Validate())
		for _, name := range []string{opGetDocument, opOpenInNotebook, notebook.OperationRunLive, notebook.OperationShowStored, notebook.OperationGetReading} {
			spec, found := m.Operations.Lookup(name)
			require.True(t, found, name)
			assert.True(t, spec.Agents, name)
		}
		for _, cp := range m.Caps {
			assert.Truef(t, allowed[cp.Pattern], "%s declares %s, which a notebooklet does not need", slug, cp.Pattern)
		}
	}
	assert.Empty(t, Slug("github.com/stergiotis/boxer/apps/notebook"))
}

func TestADocumentThatIsNoNotebookIsPassedOver(t *testing.T) {
	fsys := fstest.MapFS{
		"README.md":   {Data: []byte("# A book\n\nNo record here.\n")},
		"bad name.md": {Data: []byte("# Bad\n\n```sql\nSELECT 1 AS one\n```\n\n^one\n")},
		"sub/good.md": {Data: []byte("---\ntitle: Good\n---\n# Good\n\n```sql\nSELECT 1 AS one\n```\n\n^one\n")},
	}
	_, err := ParseDocSource("t", "README.md", fsys["README.md"].Data, fsys, []app.TopicT{app.TopicData})
	assert.True(t, errors.Is(err, ErrNotANotebook))
	defs, errs := parseBook(registeredBook{id: "t", fsys: fsys, topics: []app.TopicT{app.TopicData}})
	require.Len(t, errs, 1, "a file name that is not a slug is an error")
	assert.Contains(t, errs[0].Error(), "slug")
	require.Len(t, defs, 1)
	assert.Equal(t, "good", defs[0].Slug)
	assert.Equal(t, "Good", defs[0].Manifest.Display)
	_, sErr := fs.Stat(defs[0].Files, "good.md")
	assert.NoError(t, sErr, "a notebook's files are its directory's")
}

// storedServer answers a record's statement with what the notebook stores
// for it.
func storedServer(t *testing.T, def *Def) mdnotebook.RunFunc {
	t.Helper()
	nb, err := mdnotebook.Read(def.Text)
	require.NoError(t, err)
	return func(_ context.Context, statement string) ([]byte, error) {
		sent := strings.TrimSpace(strings.TrimSuffix(statement, "FORMAT ArrowStream"))
		for i := range nb.Cells {
			cell := &nb.Cells[i]
			if cell.Kind == mdnotebook.CellKindRecord && strings.HasSuffix(sent, strings.TrimSpace(cell.Body)) {
				ref, found := nb.Record(cell.Name)
				require.True(t, found, cell.Name)
				return mdnotebook.ReadBlob(def.Files, ref.Blob)
			}
		}
		return nil, errors.New("no such record")
	}
}

func command(t *testing.T, h app.OperationsHandlerI, op string) error {
	t.Helper()
	raw, err := buscodec.Encode(struct{}{})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, op, raw)
	return err
}

// The window is the pane over its notebook: a run through its catalog
// shows the records computed, and get_reading says so.
func TestTheWindowRunsItsNotebookLive(t *testing.T) {
	t.Cleanup(scenetest.Install())
	def, err := Find("report")
	require.NoError(t, err)
	w := newWindow(def)
	w.run = storedServer(t, def)
	w.build()
	require.NotNil(t, w.pane, w.paneErr)
	assert.Equal(t, string(def.Manifest.Id), w.pane.Identity(), "a notebooklet's runs are its own")
	h := w.Operations()

	require.NoError(t, command(t, h, notebook.OperationRunLive))
	deadline := time.Now().Add(10 * time.Second)
	for {
		w.pane.Poll()
		started, ended := w.pane.Runs()
		if started > 0 && ended == started {
			break
		}
		require.True(t, time.Now().Before(deadline), "the run did not end in time")
		time.Sleep(2 * time.Millisecond)
	}
	res, err := h.Snapshot().Query(notebook.OperationGetReading, nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[notebook.LiveState](res)
	require.NoError(t, err)
	assert.Equal(t, "live", st.Reading, st.LastErr)
	assert.Equal(t, 5, st.Records)

	require.NoError(t, command(t, h, notebook.OperationShowStored))
	assert.Equal(t, "stored", w.pane.State().Reading)
	err = command(t, h, opOpenInNotebook)
	require.Error(t, err, "a window with no bus opens nothing")
}

// A host embeds a registered notebook by its slug.
func TestEmbedBySlug(t *testing.T) {
	pane, err := Embed("minimal", notebook.EmbedConfig{Host: "demo.host"})
	require.NoError(t, err)
	assert.Equal(t, "demo.host#minimal", pane.Identity())
	_, err = Embed("nope", notebook.EmbedConfig{Host: "demo.host"})
	require.Error(t, err)
}

func TestHelpCorpusIndexes(t *testing.T) {
	_, errs := MintManifests(zerolog.Nop())
	require.Empty(t, errs)
	help.SyncFromRegistry()
	b, ok := help.Book(app.AppIdT(idPrefix + "minimal"))
	require.True(t, ok, "a notebooklet's help book is indexed from Manifest.Help")
	require.Empty(t, b.Validate())
	found := false
	for _, info := range b.Docs() {
		found = found || info.Path == "overview"
		doc, _, parsed := b.Doc(info.Path)
		require.Truef(t, parsed, "doc %q must parse", info.Path)
		require.Emptyf(t, doc.Dropped(), "doc %q loses content in the lowering: %+v", info.Path, doc.Dropped())
	}
	require.True(t, found)
}

var helpOperationRow = regexp.MustCompile("(?m)^\\| `([a-z][a-z0-9_]*)` \\|")

// The overview's table and the catalog name the same operations.
func TestHelpNamesEveryOperation(t *testing.T) {
	raw, err := fs.ReadFile(helpFS, "help/overview.md")
	require.NoError(t, err)
	documented := map[string]int{}
	for _, m := range helpOperationRow.FindAllStringSubmatch(string(raw), -1) {
		documented[m[1]]++
	}
	catalog := ops.Catalog()
	for _, spec := range catalog.Operations {
		assert.Equalf(t, 1, documented[spec.Name], "the operation %s has one row in the overview", spec.Name)
		delete(documented, spec.Name)
	}
	assert.Empty(t, documented, "the overview lists an operation the catalog does not have")
}
