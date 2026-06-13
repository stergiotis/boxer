package demo

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestManifestsToArrowIPC_SchemaAndRowCount(t *testing.T) {
	mans := []runtimeapp.Manifest{
		{
			Id:      "github.com/example/foo",
			Display: "Foo",
			Title:   "Foo Title",
			Icon:    "F",
			Surface: runtimeapp.SurfaceWindowed,
			SurfaceHints: runtimeapp.SurfaceHints{
				PreferredWidth:  800,
				PreferredHeight: 600,
			},
			Version:  "0.1.0",
			Category: "test",
		},
		{
			Id:      "github.com/example/bar",
			Display: "Bar",
			Surface: runtimeapp.SurfaceHeadless,
		},
	}
	buf, err := manifestsToArrowIPC(mans)
	require.NoError(t, err)
	require.NotEmpty(t, buf)

	alloc := memory.NewGoAllocator()
	rdr, err := ipc.NewReader(bytes.NewReader(buf), ipc.WithAllocator(alloc))
	require.NoError(t, err)
	defer rdr.Release()

	require.True(t, rdr.Next(), "expected at least one record")
	rec := rdr.Record()
	assert.Equal(t, int64(2), rec.NumRows())
	assert.Equal(t, 13, int(rec.NumCols()))

	// Sorted by Id — "github.com/example/bar" sorts before "...foo".
	idCol := rec.Column(0).(arrowStringView)
	assert.Equal(t, "github.com/example/bar", idCol.Value(0))
	assert.Equal(t, "github.com/example/foo", idCol.Value(1))

	// Field 1 = subject_alias — last '/'-segment.
	aliasCol := rec.Column(1).(arrowStringView)
	assert.Equal(t, "bar", aliasCol.Value(0))
	assert.Equal(t, "foo", aliasCol.Value(1))

	// Field 4 = title — composed via WindowTitle ("Icon Title" for foo,
	// Display fallback for bar).
	titleCol := rec.Column(4).(arrowStringView)
	assert.Equal(t, "Bar", titleCol.Value(0))
	assert.Equal(t, "F Foo Title", titleCol.Value(1))

	// Field 6 = surface — String() form.
	surfaceCol := rec.Column(6).(arrowStringView)
	assert.Equal(t, "headless", surfaceCol.Value(0))
	assert.Equal(t, "windowed", surfaceCol.Value(1))
}

// arrowStringView mirrors the subset of array.String we need for
// reading back values. Using the concrete *array.String would also
// work; this alias keeps the cast at the call site readable.
type arrowStringView interface {
	Value(i int) string
}

func TestManifestsToArrowIPC_LegacyCodeNullForUnmapped(t *testing.T) {
	mans := []runtimeapp.Manifest{
		{
			Id:      "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets",
			Display: "Widgets",
			Surface: runtimeapp.SurfaceWindowed,
		},
		{
			Id:      "github.com/example/no-code",
			Display: "No Code",
			Surface: runtimeapp.SurfaceWindowed,
		},
	}
	buf, err := manifestsToArrowIPC(mans)
	require.NoError(t, err)

	alloc := memory.NewGoAllocator()
	rdr, err := ipc.NewReader(bytes.NewReader(buf), ipc.WithAllocator(alloc))
	require.NoError(t, err)
	defer rdr.Release()
	require.True(t, rdr.Next())
	rec := rdr.Record()

	legacy := rec.Column(2)
	// example/no-code sorts before stergiotis/...widgets, so row 0 is
	// the unmapped one and must be null; row 1 must be present (= 1).
	assert.True(t, legacy.IsNull(0), "unmapped row should be null")
	assert.False(t, legacy.IsNull(1), "widgets row should be present")
}

func TestRenderManifestsAscii_ContainsRows(t *testing.T) {
	mans := []runtimeapp.Manifest{
		{
			Id:       "github.com/example/foo",
			Display:  "Foo",
			Surface:  runtimeapp.SurfaceWindowed,
			Category: "test",
		},
	}
	var out bytes.Buffer
	err := renderManifestsAscii(mans, &out)
	require.NoError(t, err)
	s := out.String()
	assert.Contains(t, s, "Foo")
	assert.Contains(t, s, "github.com/example/foo")
	assert.Contains(t, s, "windowed")
	assert.Contains(t, s, "1 application(s) registered")
}
