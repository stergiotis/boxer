package mdnotebook_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

const draft = "---\ntitle: T\ntags: [a, b]\n---\n\n" +
	"Total [?](val:total.n), mean [?](val:total.mean \"gloss/duration;unit=ms\").\n\n" +
	"```sql\nSELECT k, v FROM src ORDER BY k\n```\n\n^by-key\n\n" +
	"```sql\nWITH big AS (SELECT * FROM by_key WHERE v > 1)\nSELECT count() AS n, avg(v) AS mean FROM big;\n```\n\n^total\n\n" +
	"```sql table\nSELECT * FROM by_key\n```\n"

// stream writes rows of (k String, v Float64) or (n UInt64, mean Float64)
// as a stream of batches of one row each.
func stream(t *testing.T, schema *arrow.Schema, fill func(b *array.RecordBuilder)) []byte {
	t.Helper()
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	fill(b)
	rec := b.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	for i := int64(0); i < rec.NumRows(); i++ {
		part := rec.NewSlice(i, i+1)
		require.NoError(t, w.Write(part))
		part.Release()
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func draftServer(t *testing.T, asked *[]string) mdnotebook.RunFunc {
	byKey := arrow.NewSchema([]arrow.Field{{Name: "k", Type: arrow.BinaryTypes.String}, {Name: "v", Type: arrow.PrimitiveTypes.Float64}}, nil)
	total := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Uint64}, {Name: "mean", Type: arrow.PrimitiveTypes.Float64}}, nil)
	return func(_ context.Context, statement string) ([]byte, error) {
		*asked = append(*asked, statement)
		if strings.Contains(statement, "count() AS n") {
			return stream(t, total, func(b *array.RecordBuilder) {
				b.Field(0).(*array.Uint64Builder).Append(2)
				b.Field(1).(*array.Float64Builder).Append(2.5)
			}), nil
		}
		return stream(t, byKey, func(b *array.RecordBuilder) {
			b.Field(0).(*array.StringBuilder).AppendValues([]string{"a", "b", "c"}, nil)
			b.Field(1).(*array.Float64Builder).AppendValues([]float64{1, 2, 3}, nil)
		}), nil
	}
}

func TestCollectSealsADraft(t *testing.T) {
	nb, err := mdnotebook.Read([]byte(draft))
	require.NoError(t, err)
	var asked []string
	out, err := mdnotebook.Collect(context.Background(), nb, "t.blobs", draftServer(t, &asked))
	require.NoError(t, err)

	require.Len(t, asked, 2)
	assert.Equal(t, "SELECT k, v FROM src ORDER BY k FORMAT ArrowStream", asked[0])
	assert.Equal(t, "WITH `by_key` AS (\nSELECT k, v FROM src ORDER BY k\n), big AS (SELECT * FROM by_key WHERE v > 1)\nSELECT count() AS n, avg(v) AS mean FROM big FORMAT ArrowStream",
		asked[1], "the records before it lead the record's own WITH list, and the record stays the outermost SELECT")

	text := string(out.Text)
	assert.True(t, strings.HasPrefix(text, "---\nnotebook: 1\ntitle: T\ntags: [a, b]\nrecords:\n  by_key: {blob: t.blobs/"), "the format key leads and the other keys are kept:\n%s", text)
	assert.Contains(t, text, "Total [2](val:total.n), mean [2.5 ms](val:total.mean \"gloss/duration;unit=ms\").")
	assert.Len(t, out.Blobs, 2)

	// What was written reads back sealed, and collecting it again changes
	// nothing.
	sealed, err := mdnotebook.Read(out.Text)
	require.NoError(t, err)
	assert.Empty(t, sealed.Problems)
	require.Len(t, sealed.Records, 2)
	for _, ref := range sealed.Records {
		assert.Equal(t, mdnotebook.BlobHash(out.Blobs[ref.Blob])+mdnotebook.RecordExt, strings.TrimPrefix(ref.Blob, "t.blobs/"))
	}
	// The second record's fence ends in `;`: what collection hashed is what
	// a reader hashes, so nothing is stale.
	ld := mdnotebook.Load(sealed, blobFS(out))
	defer ld.Release()
	assert.Empty(t, ld.Problems)
	assert.Empty(t, ld.Stale)
	again, err := mdnotebook.Collect(context.Background(), sealed, "t.blobs", draftServer(t, &asked))
	require.NoError(t, err)
	assert.Equal(t, text, string(again.Text))
	assert.Equal(t, mdnotebook.PinOf(sealed.Records), mdnotebook.PinOf(again.Records))

	buffer, err := mdnotebook.Buffer(sealed)
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSuffix(asked[1], " FORMAT ArrowStream"), buffer, "the play buffer is the last record's statement")
}

// blobFS is a collected notebook's blobs as the directory they are written
// into.
func blobFS(out *mdnotebook.Collected) fstest.MapFS {
	fsys := fstest.MapFS{}
	for p, data := range out.Blobs {
		fsys[p] = &fstest.MapFile{Data: data}
	}
	return fsys
}

// A blob's directory follows the notebook's file name. Whatever that holds,
// the frontmatter collection writes reads back as the path it wrote.
func TestCollectWritesABlobPathTheFrontmatterReadsBack(t *testing.T) {
	for _, blobDir := range []string{"report, final.blobs", "notes #3.blobs", "Q3: results.blobs", "{draft}.blobs", "1e5", "plain.blobs"} {
		t.Run(blobDir, func(t *testing.T) {
			nb, err := mdnotebook.Read([]byte("```sql\nSELECT k, v FROM src\n```\n\n^by-key\n"))
			require.NoError(t, err)
			var asked []string
			out, err := mdnotebook.Collect(context.Background(), nb, blobDir, draftServer(t, &asked))
			require.NoError(t, err)
			sealed, err := mdnotebook.Read(out.Text)
			require.NoError(t, err, string(out.Text))
			assert.Empty(t, sealed.Problems)
			require.Len(t, sealed.Records, 1)
			assert.Contains(t, out.Blobs, sealed.Records[0].Blob)
			assert.NotEmpty(t, sealed.Records[0].Of)
			ld := mdnotebook.Load(sealed, blobFS(out))
			defer ld.Release()
			assert.Empty(t, ld.Problems)
		})
	}
}

// A `val:` link in a code span or a fenced block is a sample of the
// spelling, not a figure: it is not checked, and collection does not rewrite
// it.
func TestAValLinkInCodeIsNoFigure(t *testing.T) {
	src := "Total [?](val:total.n). Write `[?](val:total.n)` for a figure, or ``[?](val:total.mean)``:\n\n" +
		"```markdown\nMean [?](val:total.mean).\n```\n\n" +
		"- in a list item:\n\n  ```markdown\n  [?](val:nowhere.n)\n  ```\n\n" +
		"An escaped \\` opens nothing: [?](val:total.mean).\n\n" +
		"```sql\nSELECT count() AS n, avg(v) AS mean FROM src\n```\n\n^total\n"
	nb, err := mdnotebook.Read([]byte(src))
	require.NoError(t, err)
	require.Len(t, nb.Vals, 2)
	assert.Equal(t, "n", nb.Vals[0].Column)
	assert.Equal(t, "mean", nb.Vals[1].Column)
	var asked []string
	out, err := mdnotebook.Collect(context.Background(), nb, "b", draftServer(t, &asked))
	require.NoError(t, err)
	text := string(out.Text)
	assert.Contains(t, text, "Total [2](val:total.n). Write `[?](val:total.n)` for a figure, or ``[?](val:total.mean)``:")
	assert.Contains(t, text, "```markdown\nMean [?](val:total.mean).\n```")
	assert.Contains(t, text, "  [?](val:nowhere.n)\n")
	assert.Contains(t, text, "opens nothing: [2.5](val:total.mean).")
	sealed, err := mdnotebook.Read(out.Text)
	require.NoError(t, err)
	assert.Empty(t, sealed.Problems, "a sample that names no record is not a problem")
}

// A value that reads as no text at all cannot be a link's label: the link
// would no longer be found as a figure.
func TestCollectFailsOnAFigureWithNoText(t *testing.T) {
	nb, err := mdnotebook.Read([]byte("Named [?](val:who.name).\n\n```sql\nSELECT '' AS name\n```\n\n^who\n"))
	require.NoError(t, err)
	schema := arrow.NewSchema([]arrow.Field{{Name: "name", Type: arrow.BinaryTypes.String}}, nil)
	_, err = mdnotebook.Collect(context.Background(), nb, "b", func(context.Context, string) ([]byte, error) {
		return stream(t, schema, func(b *array.RecordBuilder) { b.Field(0).(*array.StringBuilder).Append("") }), nil
	})
	assert.ErrorContains(t, err, "label cannot hold")
}

func TestCollectWritesFrontmatterWhereThereIsNone(t *testing.T) {
	nb, err := mdnotebook.Read([]byte("# T\n\n```sql\nSELECT k, v FROM src\n```\n\n^by-key\n"))
	require.NoError(t, err)
	var asked []string
	out, err := mdnotebook.Collect(context.Background(), nb, "b", draftServer(t, &asked))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(out.Text), "---\nnotebook: 1\nrecords:\n  by_key: {blob: b/"), string(out.Text))
	assert.True(t, strings.HasSuffix(string(out.Text), "---\n\n# T\n\n```sql\nSELECT k, v FROM src\n```\n\n^by-key\n"), string(out.Text))
}

// A frontmatter entry with no cell is a relation stored by other means;
// collection keeps it.
func TestCollectKeepsAnEntryWithoutACell(t *testing.T) {
	src := "---\nrecords:\n  handed_in: {blob: b/00.arrow}\n  by_key: {blob: b/old.arrow, of: 00}\ntitle: T\n---\n\n```sql\nSELECT k, v FROM src\n```\n\n^by-key\n"
	nb, err := mdnotebook.Read([]byte(src))
	require.NoError(t, err)
	var asked []string
	out, err := mdnotebook.Collect(context.Background(), nb, "b", draftServer(t, &asked))
	require.NoError(t, err)
	text := string(out.Text)
	assert.Contains(t, text, "  handed_in: {blob: b/00.arrow}\ntitle: T\n---\n")
	assert.NotContains(t, text, "b/old.arrow")
	assert.Equal(t, 1, strings.Count(text, "records:"))
}

func TestCollectRefusesWhatIsNotOneSelect(t *testing.T) {
	for name, body := range map[string]string{
		"a format":    "SELECT 1 AS a FORMAT CSV",
		"a set":       "SET max_threads = 1; SELECT 1 AS a",
		"an insert":   "INSERT INTO t SELECT 1",
		"no parse":    "SELEKT 1",
		"empty":       "",
		"two records": "",
	} {
		t.Run(name, func(t *testing.T) {
			src := "```sql\n" + body + "\n```\n\n^r\n"
			if name == "two records" {
				src = "```sql\nSELECT 1 AS a\n```\n\n^r\n\n```sql\nSELECT 2 AS a\n```\n\n^r\n"
			}
			nb, err := mdnotebook.Read([]byte(src))
			require.NoError(t, err)
			_, err = mdnotebook.Collect(context.Background(), nb, "b", func(context.Context, string) ([]byte, error) {
				t.Fatal("nothing is sent for a notebook that is refused")
				return nil, nil
			})
			assert.Error(t, err)
		})
	}
}

// A figure whose record has more than one row fails collection: the text
// would claim a value the record does not have.
func TestCollectFailsOnAFigureItCannotRead(t *testing.T) {
	nb, err := mdnotebook.Read([]byte("First [?](val:by_key.v).\n\n```sql\nSELECT k, v FROM src\n```\n\n^by-key\n"))
	require.NoError(t, err)
	var asked []string
	_, err = mdnotebook.Collect(context.Background(), nb, "b", draftServer(t, &asked))
	assert.ErrorContains(t, err, "one row")
}

// Compute is collection's computing half: the same statements, the records
// by name, and nothing stored (ADR-0380 §SD1).
func TestComputeAnswersTheRecordsCollectionStores(t *testing.T) {
	nb, err := mdnotebook.Read([]byte(draft))
	require.NoError(t, err)
	var collected, computed []string
	_, err = mdnotebook.Collect(context.Background(), nb, "t.blobs", draftServer(t, &collected))
	require.NoError(t, err)
	var done []string
	records, err := mdnotebook.Compute(context.Background(), nb, draftServer(t, &computed), mdnotebook.ComputeOptions{
		Done: func(record string) { done = append(done, record) },
	})
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	assert.Equal(t, collected, computed, "a live reading sends what collection sends")
	assert.Equal(t, []string{"by_key", "total"}, done)
	require.Len(t, records, 2)
	assert.EqualValues(t, 3, records["by_key"].NumRows(), "a stream of several batches is one record")
	assert.EqualValues(t, 1, records["total"].NumRows())
}

// A record over the bound fails by name and nothing is returned: a result
// is not cut short.
func TestComputeFailsARecordOverItsBound(t *testing.T) {
	nb, err := mdnotebook.Read([]byte(draft))
	require.NoError(t, err)
	for name, opts := range map[string]mdnotebook.ComputeOptions{
		"rows":           {MaxRows: 2},
		"bytes":          {MaxBytes: 16},
		"rows, parallel": {MaxRows: 2, Parallel: 4},
	} {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var asked []string
			server := draftServer(t, &asked)
			records, err := mdnotebook.Compute(context.Background(), nb, func(ctx context.Context, statement string) ([]byte, error) {
				mu.Lock()
				defer mu.Unlock()
				return server(ctx, statement)
			}, opts)
			assert.Nil(t, records)
			var rErr *mdnotebook.RecordError
			require.ErrorAs(t, err, &rErr)
			assert.Equal(t, "by_key", rErr.Record)
			assert.ErrorContains(t, err, "not cut short")
		})
	}
}

// Sent several at once, the records are the ones sent one after another.
func TestComputeInParallelAnswersTheSameRecords(t *testing.T) {
	nb, err := mdnotebook.Read([]byte(draft))
	require.NoError(t, err)
	var mu sync.Mutex
	var asked []string
	server := draftServer(t, &asked)
	records, err := mdnotebook.Compute(context.Background(), nb, func(ctx context.Context, statement string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		return server(ctx, statement)
	}, mdnotebook.ComputeOptions{Parallel: 4})
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	assert.Len(t, asked, 2)
	assert.EqualValues(t, 3, records["by_key"].NumRows())
	assert.EqualValues(t, 1, records["total"].NumRows())
}

// TestCollectKeepsAFormatKeyItFinds: a notebook that names its format is
// not given a second key.
func TestCollectKeepsAFormatKeyItFinds(t *testing.T) {
	nb, err := mdnotebook.Read([]byte("---\nnotebook: 1\ntitle: T\n---\n\n```sql\nSELECT k, v FROM src\n```\n\n^by-key\n"))
	require.NoError(t, err)
	var asked []string
	out, err := mdnotebook.Collect(context.Background(), nb, "b", draftServer(t, &asked))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(out.Text), "notebook:"), string(out.Text))
}

// TestReadRefusesANewerFormat: a notebook written in a format this build
// does not know is refused with both numbers, not misread.
func TestReadRefusesANewerFormat(t *testing.T) {
	_, err := mdnotebook.Read([]byte("---\nnotebook: 1\n---\n\n# T\n"))
	require.NoError(t, err)
	_, err = mdnotebook.Read([]byte("---\ntitle: no key is format 1\n---\n\n# T\n"))
	require.NoError(t, err)
	_, err = mdnotebook.Read([]byte("---\nnotebook: 2\n---\n\n# T\n"))
	require.ErrorContains(t, err, "written by a newer boxer (format 2; this build reads up to 1)")
	_, err = mdnotebook.Read([]byte("---\nnotebook: two\n---\n\n# T\n"))
	require.Error(t, err)
}
