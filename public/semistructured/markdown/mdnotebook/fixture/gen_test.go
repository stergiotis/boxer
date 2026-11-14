package fixture_test

import (
	"bytes"
	"context"
	"flag"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

var update = flag.Bool("update", false, "rewrite the fixture files")

const blobDir = "w45.blobs"

const stmtStats = `SELECT quantile(0.99)(lag_ms) AS p99_ms,
       quantileIf(0.99)(lag_ms, week = 44) AS p99_prev_ms,
       count() AS samples
FROM ingest_lag`

const stmtHourly = `SELECT toStartOfHour(ts) AS t, region, quantile(0.99)(lag_ms) AS p99
FROM ingest_lag GROUP BY t, region`

const stmtCalls = `SELECT caller AS source, callee AS target, count() AS calls
FROM spans GROUP BY source, target`

const text = `---
title: Ingest lag, week 45
---

# Ingest lag, week 45

The 99th percentile was [?](val:stats.p99_ms "gloss/duration;unit=ms"), against
[?](val:stats.p99_prev_ms "gloss/duration;unit=ms") the week before, over
[?](val:stats.samples) samples.

## How the figures were collected

Three records. Each is computed on the server when the notebook is
collected, and stored; nothing below this section computes.

` + "```sql\n" + stmtStats + "\n```" + `

^stats

` + "```sql\n" + stmtHourly + "\n```" + `

^p99-by-hour

` + "```sql\n" + stmtCalls + "\n```" + `

^calls

## By hour

The record as it is stored, eight rows of it:

` + "```sql table\nSELECT * FROM p99_by_hour LIMIT 8\n```" + `

The same record under the names a reader wants, with the lag shown as a
duration:

` + "```sql table\nSELECT t AS hour, region, p99 AS \"p99@gloss/duration;unit=ms\" FROM p99_by_hour LIMIT 8\n```" + `

As a chart. The cell gives the columns the names the chart reads: ` + "`x`" + ` is
the abscissa, ` + "`series`" + ` splits the rows into lines, and every number left
is a line's value:

` + "```sql chart\nSELECT t AS x, region AS series, p99 FROM p99_by_hour\n```" + `

The first half-day in a chart of its own, from the same stored record:

` + "```sql chart\nSELECT t AS x, region AS series, p99 AS \"p99@gloss/duration;unit=ms\" FROM p99_by_hour LIMIT 24\n```" + `

## What a cell may not do

A cell that filters is refused; a filtered view is a record of its own:

` + "```sql table\nSELECT * FROM p99_by_hour WHERE region = 'emea'\n```" + `

A cell that computes is refused; the computation belongs in a record:

` + "```sql table\nSELECT region, max(p99) FROM p99_by_hour GROUP BY region\n```" + `

## More than one relation

A panel that reads several relations takes them from the cell's WITH list.
The second member is a row of literals, the panel's settings:

` + "```sql graphview\nWITH edges AS (SELECT source, target FROM calls),\n     graph_opts AS (SELECT 'hierarchical' AS layout)\nSELECT * FROM edges\n```" + `

## Media

An image is prose; the markdown widget draws it. A recording is a cell:

![Lag by region, as drawn on the wall display]({{png}})

![Stand-up, Tuesday]({{wav}})

A fence inside a list item is not a cell; it is shown as code:

- the statement an earlier draft used:

  ` + "```sql table\n  SELECT * FROM p99_by_hour\n  ```" + `
`

// arrowStream is rec as a server might stream it: in batches of at most
// rows rows, so collection has something to join.
func arrowStream(t *testing.T, rec arrow.RecordBatch, rows int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()))
	for at := int64(0); at < rec.NumRows(); at += rows {
		part := rec.NewSlice(at, min(at+rows, rec.NumRows()))
		require.NoError(t, w.Write(part))
		part.Release()
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func statsRecord() arrow.RecordBatch {
	mem := memory.DefaultAllocator
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "p99_ms", Type: arrow.PrimitiveTypes.Float64},
		{Name: "p99_prev_ms", Type: arrow.PrimitiveTypes.Float64},
		{Name: "samples", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	b.Field(0).(*array.Float64Builder).Append(412.3)
	b.Field(1).(*array.Float64Builder).Append(317.0)
	b.Field(2).(*array.Uint64Builder).Append(1204113)
	return b.NewRecordBatch()
}

func hourlyRecord() arrow.RecordBatch {
	mem := memory.DefaultAllocator
	ts := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t", Type: ts},
		{Name: "region", Type: arrow.BinaryTypes.String},
		{Name: "p99", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 24; h++ {
		for r, region := range []string{"emea", "apac"} {
			at := start.Add(time.Duration(h) * time.Hour)
			b.Field(0).(*array.TimestampBuilder).Append(arrow.Timestamp(at.UnixMicro()))
			b.Field(1).(*array.StringBuilder).Append(region)
			phase := float64(h)/24*2*math.Pi + float64(r)*math.Pi/2
			v := 300 + 180*math.Sin(phase) + 40*math.Sin(3*phase)
			b.Field(2).(*array.Float64Builder).Append(math.Round(v*10) / 10)
		}
	}
	return b.NewRecordBatch()
}

func callsRecord() arrow.RecordBatch {
	mem := memory.DefaultAllocator
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "source", Type: arrow.BinaryTypes.String},
		{Name: "target", Type: arrow.BinaryTypes.String},
		{Name: "calls", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	for _, e := range []struct {
		s, t string
		n    uint64
	}{{"gateway", "ingest", 9120}, {"ingest", "queue", 9118}, {"queue", "writer", 9101}, {"writer", "store", 9101}, {"gateway", "auth", 4410}, {"auth", "store", 4398}} {
		b.Field(0).(*array.StringBuilder).Append(e.s)
		b.Field(1).(*array.StringBuilder).Append(e.t)
		b.Field(2).(*array.Uint64Builder).Append(e.n)
	}
	return b.NewRecordBatch()
}

func pngBlob(t *testing.T) []byte {
	t.Helper()
	const w, h = 360, 96
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0x16, G: 0x1b, B: 0x22, A: 0xff})
		}
	}
	for i := 0; i < 24; i++ {
		v := 0.5 + 0.4*math.Sin(float64(i)/24*2*math.Pi)
		top := h - 8 - int(v*float64(h-16))
		for y := top; y < h-8; y++ {
			for x := 6 + i*15; x < 6+i*15+11; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 0x4f, G: 0xb3, B: 0xa5, A: 0xff})
			}
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func wavBlob(t *testing.T) []byte {
	t.Helper()
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	const frames = 8000 * 3
	src, err := pcm.NewSynthSource(format, frames, func(frame int64, _ int) float32 {
		at := float64(frame) / float64(format.SampleRate)
		env := 0.15 + 0.85*math.Abs(math.Sin(at*math.Pi*1.5))
		return float32(0.6 * env * math.Sin(2*math.Pi*220*at))
	})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, wavfile.Write(context.Background(), &buf, format, wavfile.EncodingPCMInt, 16, src))
	return buf.Bytes()
}

// generate builds the fixture as a notebook is made: a draft with no
// stored result and no figure, collected. The server is a function that
// answers each record's statement with a record built here.
func generate(t *testing.T) (md string, blobs map[string][]byte) {
	t.Helper()
	media := make(map[string][]byte)
	put := func(data []byte, ext string) string {
		p := blobDir + "/" + mdnotebook.BlobHash(data) + ext
		media[p] = data
		return p
	}
	draft := strings.NewReplacer("{{png}}", put(pngBlob(t), ".png"), "{{wav}}", put(wavBlob(t), ".wav")).Replace(text)
	nb, err := mdnotebook.Read([]byte(draft))
	require.NoError(t, err)

	stats, hourly, calls := statsRecord(), hourlyRecord(), callsRecord()
	defer stats.Release()
	defer hourly.Release()
	defer calls.Release()
	var asked []string
	run := func(_ context.Context, statement string) ([]byte, error) {
		asked = append(asked, statement)
		body, isArrow := strings.CutSuffix(statement, " FORMAT ArrowStream")
		require.True(t, isArrow, statement)
		// A record's statement is the outermost SELECT of what collection
		// sends, after the records before it.
		switch {
		case strings.HasSuffix(body, stmtStats):
			return arrowStream(t, stats, 1), nil
		case strings.HasSuffix(body, stmtHourly):
			return arrowStream(t, hourly, 7), nil
		case strings.HasSuffix(body, stmtCalls):
			return arrowStream(t, calls, 6), nil
		}
		t.Fatalf("collection asked for a statement the fixture does not know: %s", statement)
		return nil, nil
	}
	out, err := mdnotebook.Collect(context.Background(), nb, blobDir, run)
	require.NoError(t, err)
	require.Len(t, asked, 3, "each record is computed once")
	require.True(t, strings.HasPrefix(asked[2], "WITH `stats` AS ("), "a record's statement follows the records before it: %s", asked[2])

	blobs = out.Blobs
	for p, data := range media {
		blobs[p] = data
	}
	return string(out.Text), blobs
}

func TestFixture(t *testing.T) {
	md, blobs := generate(t)
	if *update {
		require.NoError(t, os.RemoveAll(blobDir))
		require.NoError(t, os.MkdirAll(blobDir, 0o755))
		require.NoError(t, os.WriteFile("w45.md", []byte(md), 0o644))
		for p, data := range blobs {
			require.NoError(t, os.WriteFile(filepath.FromSlash(p), data, 0o644))
		}
		return
	}
	got, err := os.ReadFile("w45.md")
	require.NoError(t, err, "run with -update to write the fixture")
	require.Equal(t, md, string(got), "the fixture's text is not what the generator writes; run with -update")
	entries, err := os.ReadDir(blobDir)
	require.NoError(t, err)
	require.Len(t, entries, len(blobs))
	for p, data := range blobs {
		onDisk, rErr := os.ReadFile(filepath.FromSlash(p))
		require.NoError(t, rErr)
		require.True(t, bytes.Equal(data, onDisk), p)
	}
}
