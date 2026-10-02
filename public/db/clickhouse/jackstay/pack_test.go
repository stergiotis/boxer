package jackstay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

// writeTestPack writes a complete one-table, one-chunk pack whose chunk file
// holds body, with the leaf digest {n, kd, rd} in leaf 0.
func writeTestPack(t *testing.T, body string, n uint64, kd uint64, rd uint64) (dir string, m PackManifest) {
	t.Helper()
	dir = t.TempDir()
	file := packFileStem("s.t", "") + packFileExt("zstd")
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644))
	sum := sha256.Sum256([]byte(body))
	m = PackManifest{
		FormatVersion: PackFormatVersion, ExportId: "e1", Complete: true,
		Source: Endpoint{URL: "http://src/"}, Server: ServerInfo{Version: "26.9.1.1"},
		Databases: []DatabaseInfo{{Name: "s", Engine: "Atomic"}},
		Selection: Selection{Databases: []string{"s"}},
		Tables: []PackTable{{
			Source: ref("s", "t"), Engine: "MergeTree", SortingKey: "k",
			CreateQuery: "CREATE TABLE s.t (`k` UInt64, `v` String) ENGINE = MergeTree ORDER BY k",
			Columns:     []ColumnInfo{{Name: "k", Type: "UInt64", Position: 1}, {Name: "v", Type: "String", Position: 2}},
			Filter:      "k > 1", CopyColumns: []string{"k", "v"},
			Chunking: Chunking{Kind: ChunkingSingle, Leaves: 1}, Done: true,
			Chunks: []PackChunk{{Id: "", File: file, Bytes: uint64(len(body)), Sha256: hex.EncodeToString(sum[:]), Encoding: "zstd",
				Rows: n, Leaves: []PackLeaf{{Leaf: 0, N: n, Kd: kd, Rd: rd}}}},
		}},
	}
	require.NoError(t, m.SaveIn(dir))
	return
}

func TestPack_ManifestAndRefusals(t *testing.T) {
	dir, m := writeTestPack(t, "native-bytes", 2, 3, 3)
	back, err := LoadPackManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, m, back)

	p, err := OpenPack(dir)
	require.NoError(t, err)
	assert.Equal(t, Endpoint{Pack: p.dir, Export: "e1"}, p.Endpoint())
	_, err = OpenPackFor(Endpoint{Pack: dir, Export: "older"})
	assert.ErrorContains(t, err, "exported again")

	inv, err := p.discover(context.Background())
	require.NoError(t, err)
	require.Len(t, inv.Tables, 1)
	assert.Equal(t, "k > 1", inv.Tables[0].Filter)
	assert.Equal(t, uint64(2), inv.Tables[0].TotalRows)

	pt := singleChunkTable("t", "t")
	pt.Filter = "k > 1"
	spec, _, _ := pt.DigestSpecs(false)
	ls, err := sourceLeafSet(context.Background(), p, &spec)
	require.NoError(t, err)
	assert.Equal(t, leafDigest{n: 2, kd: 3, rd: 3}, ls.total())

	for name, s := range map[string]DigestSpec{
		"filter":   func() DigestSpec { s := spec; s.Filter = "k > 2"; return s }(),
		"columns":  func() DigestSpec { s := spec; s.CopyColumns = []string{"k"}; return s }(),
		"layout":   func() DigestSpec { s := spec; s.Chunking.Leaves = 2; return s }(),
		"final":    func() DigestSpec { s := spec; s.Final = true; return s }(),
		"leaves":   spec.ForChunk("", "").ForLeaves([]uint32{0}),
		"sample":   spec.ForSample(1, 2),
		"opaque":   spec.With("k = 3"),
		"no table": func() DigestSpec { s := spec; s.Ref = ref("s", "u"); return s }(),
	} {
		_, err = p.digests(context.Background(), &s)
		assert.Error(t, err, name)
	}

	m.Complete = false
	require.NoError(t, m.SaveIn(dir))
	_, err = OpenPack(dir)
	assert.ErrorContains(t, err, "not complete")
}

// A pack is a source a full sync reads chunk files from, verified against
// the manifest's digests; a damaged file fails before anything is inserted.
func TestSyncTable_FromPack(t *testing.T) {
	dir, _ := writeTestPack(t, "native-bytes", 2, 3, 3)
	p, err := OpenPack(dir)
	require.NoError(t, err)
	pt := singleChunkTable("t", "t")
	pt.Filter = "k > 1"
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyRefuse}
	dstDigests := 0
	var got []byte
	var gotEncoding string
	dst := &recordingClient{fakeClient: fakeClient{answer: func(sql string) (string, error) {
		switch {
		case isDigestQuery(sql, "t"):
			dstDigests++
			if dstDigests == 1 {
				return "", nil
			}
			return digestRow("", 2, 3, 3), nil
		}
		return `{"n":0}` + "\n", nil
	}}}
	dst.onInsert = func(body []byte, opts chclient.StreamOptions) { got, gotEncoding = body, opts.ContentEncoding }
	rep, err := SyncTable(context.Background(), p, dst, pt, openTestJournal(t, "r"), DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Copied, rep.Problems)
	assert.Equal(t, "native-bytes", string(got), "the file is the insert's body, undecoded")
	assert.Equal(t, "zstd", gotEncoding)

	pt.Sync.Mode, pt.Sync.SampleNum, pt.Sync.SampleDen = SyncModeSample, 1, 2
	_, err = SyncTable(context.Background(), p, dst, pt, openTestJournal(t, "r2"), DefaultSyncOptions(), time.Now)
	assert.ErrorContains(t, err, "whole chunks")

	// One flipped byte: the stream refuses before the insert.
	file := filepath.Join(dir, p.m.Tables[0].Chunks[0].File)
	require.NoError(t, os.WriteFile(file, []byte("native-bytez"), 0o644))
	pt.Sync.Mode = SyncModeFull
	dstDigests = 0
	inserts := dst.inserts
	rep, err = SyncTable(context.Background(), p, dst, pt, openTestJournal(t, "r3"), DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Failed)
	assert.Contains(t, rep.Problems[0], "damaged")
	assert.Equal(t, inserts, dst.inserts, "nothing was inserted")
}

type recordingClient struct {
	fakeClient
	onInsert func(body []byte, opts chclient.StreamOptions)
}

func (inst *recordingClient) InsertStream(ctx context.Context, insertSQL string, body io.Reader, opts chclient.StreamOptions) (err error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return
	}
	inst.mu.Lock()
	inst.inserts++
	inst.mu.Unlock()
	if inst.onInsert != nil {
		inst.onInsert(b, opts)
	}
	return
}
