package notebook

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook/samples"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// storedServer answers a record's statement with what the notebook stores
// for it: the record whose statement ends the one sent. failOn names a
// record it answers with an error instead.
func storedServer(t *testing.T, text []byte, fsys fs.FS, failOn string, asked *[]string) mdnotebook.RunFunc {
	t.Helper()
	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	var mu sync.Mutex
	return func(_ context.Context, statement string) ([]byte, error) {
		mu.Lock()
		*asked = append(*asked, statement)
		mu.Unlock()
		sent := strings.TrimSpace(strings.TrimSuffix(statement, "FORMAT ArrowStream"))
		for i := range nb.Cells {
			cell := &nb.Cells[i]
			if cell.Kind != mdnotebook.CellKindRecord || !strings.HasSuffix(sent, strings.TrimSpace(cell.Body)) {
				continue
			}
			if cell.Name == failOn {
				return nil, errors.New("the server is gone")
			}
			ref, found := nb.Record(cell.Name)
			require.True(t, found, cell.Name)
			return mdnotebook.ReadBlob(fsys, ref.Blob)
		}
		t.Errorf("no record ends the statement %q", statement)
		return nil, errors.New("no such record")
	}
}

func sampleText(t *testing.T, name string) []byte {
	t.Helper()
	text, err := fs.ReadFile(samples.FS(), name+".md")
	require.NoError(t, err)
	return text
}

// ran polls the window until the run that was started has ended.
func ran(t *testing.T, inst *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for inst.pane.runDone < inst.pane.runSeq {
		require.True(t, time.Now().Before(deadline), "the run did not end in time")
		inst.poll()
		time.Sleep(2 * time.Millisecond)
	}
	inst.poll()
}

// A sample is run live: what collection would send is sent, the Read face
// shows the records computed, and show_stored returns to what is stored.
func TestASampleRunsLiveAndReturnsToWhatItStores(t *testing.T) {
	t.Cleanup(scenetest.Install())
	inst := newApp()
	h := inst.Operations()
	var asked []string
	inst.run = storedServer(t, sampleText(t, "minimal"), samples.FS(), "", &asked)
	require.NoError(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "minimal"}))
	read(t, inst)
	require.Equal(t, phaseSealed, inst.observePhase())

	st, err := query[LiveState](t, h, opGetReading)
	require.NoError(t, err)
	assert.Equal(t, readingNameStored, st.Reading)
	assert.Equal(t, "read", st.Class)
	assert.Empty(t, st.Refusal)

	require.NoError(t, command(t, h, opRunLive, struct{}{}))
	ran(t, inst)
	require.Empty(t, inst.pane.lastErr)
	assert.Len(t, asked, 2, "one statement per record, as collection sends")
	st, err = query[LiveState](t, h, opGetReading)
	require.NoError(t, err)
	assert.Equal(t, readingNameLive, st.Reading)
	assert.Equal(t, 2, st.Records)
	assert.False(t, st.RunAt.IsZero())
	assert.Empty(t, st.Problems, "a bound reading owes no stored result")
	assert.Empty(t, st.Refused)
	assert.Equal(t, phaseLive, inst.observePhase())
	assert.True(t, inst.check.Sealed(), "the stored reading's check is kept beside the live one")
	assert.True(t, inst.pane.view.Notebook() != nil)

	require.NoError(t, command(t, h, opShowStored, struct{}{}))
	read(t, inst)
	inst.poll()
	st, err = query[LiveState](t, h, opGetReading)
	require.NoError(t, err)
	assert.Equal(t, readingNameStored, st.Reading)
	assert.Equal(t, phaseSealed, inst.observePhase())
}

// A run that fails names the record and leaves the earlier reading on
// screen (ADR-0380 §SD4).
func TestAFailedRunKeepsTheEarlierReading(t *testing.T) {
	t.Cleanup(scenetest.Install())
	inst := newApp()
	h := inst.Operations()
	var asked []string
	inst.run = storedServer(t, sampleText(t, "minimal"), samples.FS(), "totals", &asked)
	require.NoError(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "minimal"}))
	read(t, inst)
	given := inst.pane.givenKey

	require.NoError(t, command(t, h, opRunLive, struct{}{}))
	ran(t, inst)
	st, err := query[LiveState](t, h, opGetReading)
	require.NoError(t, err)
	assert.Equal(t, readingNameStored, st.Reading)
	assert.Contains(t, st.LastErr, "totals")
	assert.Contains(t, st.LastErr, "the server is gone")
	assert.Equal(t, given, inst.pane.givenKey, "the view still holds what it held")
	assert.Equal(t, phaseSealed, inst.observePhase())
}

// A text that changes drops the live reading: its records were computed
// from statements the text no longer holds.
func TestAnEditedTextDropsTheLiveReading(t *testing.T) {
	t.Cleanup(scenetest.Install())
	inst := newApp()
	h := inst.Operations()
	var asked []string
	text := sampleText(t, "minimal")
	inst.run = storedServer(t, text, samples.FS(), "", &asked)
	require.NoError(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "minimal"}))
	read(t, inst)
	require.NoError(t, command(t, h, opRunLive, struct{}{}))
	ran(t, inst)
	require.Equal(t, readingLive, inst.pane.shown)

	require.True(t, inst.setText([]byte(strings.Replace(string(text), "# A notebook in two records", "# A notebook, edited", 1))))
	read(t, inst)
	assert.Equal(t, readingStored, inst.pane.shown)
	assert.Nil(t, inst.pane.live)
}

// The text of a notebook whose record reaches outside the server.
const egressNotebook = "# Out\n\n```sql\nSELECT * FROM url('http://example.invalid/x.csv', CSVWithNames)\n```\n\n^outside\n"

// Only a notebook whose records classify read runs live; another is
// refused with what forced its class, and is not embedded.
func TestOnlyANotebookThatReadsRunsLive(t *testing.T) {
	nb, err := mdnotebook.Read([]byte(egressNotebook))
	require.NoError(t, err)
	refusal, class := classifyRecords(nb)
	assert.Equal(t, "read-egress", class.String())
	assert.Contains(t, refusal, "url")
	assert.Contains(t, refusal, "collect it instead")

	p := newPane(paneConfig{identity: "t"})
	p.setDocument(nb, nil, "egress")
	err = p.startRun()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-egress")
	assert.Zero(t, p.runSeq, "nothing ran")

	_, err = NewEmbedded([]byte(egressNotebook), EmbedConfig{Host: "demo.host", Key: "outside"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not embedded")

	refusal, _ = classifyRecords(mustRead(t, sampleText(t, "report")))
	assert.Empty(t, refusal, "the samples read")
}

func mustRead(t *testing.T, text []byte) *mdnotebook.Notebook {
	t.Helper()
	nb, err := mdnotebook.Read(text)
	require.NoError(t, err)
	return nb
}

// An embedded pane runs under its composed identity and declares what a
// run needs, and nothing of collection.
func TestAnEmbeddedPaneRunsUnderItsHostsName(t *testing.T) {
	t.Cleanup(scenetest.Install())
	text := sampleText(t, "minimal")
	var asked []string
	pane, err := NewEmbedded(text, EmbedConfig{Host: "demo.host", Key: "minimal", Files: samples.FS(),
		Run: storedServer(t, text, samples.FS(), "", &asked)})
	require.NoError(t, err)
	assert.Equal(t, "demo.host#minimal", pane.Identity())
	_, err = NewEmbedded(text, EmbedConfig{Host: "demo.host", Key: "two words"})
	require.Error(t, err)

	require.NoError(t, pane.Run())
	deadline := time.Now().Add(10 * time.Second)
	for pane.pane.runDone < pane.pane.runSeq || pane.State().Reading != readingNameLive {
		require.True(t, time.Now().Before(deadline), "the run did not end in time: %s", pane.pane.lastErr)
		pane.pane.poll()
		time.Sleep(2 * time.Millisecond)
	}
	assert.Equal(t, 2, pane.State().Records)

	var dials, publishes int
	for _, cp := range EmbedCaps() {
		if cp.Pattern == "runtime.ch.server.read.records" {
			dials++
		}
		if cp.Pattern == ladingpublish.Subject(mountTag) {
			publishes++
		}
	}
	assert.Equal(t, 1, dials)
	assert.Zero(t, publishes, "a pane does not publish to the store")
}

// What a run sends to the server: readonly set, and the identity of the
// pane in the query log's comment. A result over the byte bound is not
// read whole.
func TestARunIsReadOnlyAndStamped(t *testing.T) {
	blob, err := mdnotebook.ReadBlob(samples.FS(), mustRead(t, sampleText(t, "minimal")).Records[0].Blob)
	require.NoError(t, err)
	var got []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Query().Get("readonly"), r.URL.Query().Get("log_comment"))
		mu.Unlock()
		_, _ = w.Write(blob)
	}))
	defer srv.Close()

	run := serverRun(chclient.Config{URL: srv.URL + "/", User: "default"}, "demo.host#minimal", "run-1", 7, int64(len(blob)))
	stream, err := run(context.Background(), "SELECT 1 FORMAT ArrowStream")
	require.NoError(t, err)
	assert.Equal(t, blob, stream)
	require.Len(t, got, 2)
	assert.Equal(t, "2", got[0])
	var st logcomment.Stamp
	require.NoError(t, json.Unmarshal([]byte(got[1]), &st))
	assert.Equal(t, logcomment.Stamp{RunId: "run-1", App: "demo.host#minimal", Instance: 7, Lane: liveLane}, st)

	short := serverRun(chclient.Config{URL: srv.URL + "/", User: "default"}, "t", "", 0, 16)
	stream, err = short(context.Background(), "SELECT 1 FORMAT ArrowStream")
	require.NoError(t, err)
	assert.Len(t, stream, 17, "one byte past the bound, so Compute fails the record")
}
