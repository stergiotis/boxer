package providersgodep

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/code/analysis/golang/codevol"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

func stubVolumeCache(calls *atomic.Int64, byPkg map[string]codevol.Volume) (inst *volumeCache) {
	inst = newVolumeCache(Config{})
	inst.load = func(context.Context, Config) map[string]codevol.Volume {
		calls.Add(1)
		return byPkg
	}
	return
}

func TestVolumeColumnsRender(t *testing.T) {
	var calls atomic.Int64
	vol := stubVolumeCache(&calls, map[string]codevol.Volume{
		"github.com/example/mod/a": {
			Files: 2, CodeLines: 100, CommentLines: 20, BlankLines: 5,
			GeneratedFiles: 1, GeneratedCode: 60, OtherLangLines: 7,
			Generators: []string{"ANTLR 4.13.2", "Leeway DML"},
		},
	})

	rec := packagesTable(readyCache().snap, vol).Build(introspect.AllColumns(), 2)
	defer rec.Release()

	assert.EqualValues(t, 100, int64At(t, rec, "code_lines", 0))
	assert.EqualValues(t, 20, int64At(t, rec, "comment_lines", 0))
	assert.EqualValues(t, 5, int64At(t, rec, "blank_lines", 0))
	assert.EqualValues(t, 1, int64At(t, rec, "generated_files", 0))
	assert.EqualValues(t, 60, int64At(t, rec, "generated_code", 0))
	assert.EqualValues(t, 7, int64At(t, rec, "other_lang_lines", 0))
	assert.Equal(t, []string{"ANTLR 4.13.2", "Leeway DML"}, stringListAt(t, rec, "generators", 0))

	// A package the pass did not tally reads zero rather than erroring.
	assert.EqualValues(t, 0, int64At(t, rec, "code_lines", 1))
	// And an untallied package's generators are empty, not a placeholder —
	// the empty-not-absent shape the rest of these columns have.
	assert.Empty(t, stringListAt(t, rec, "generators", 1))

	// However many rows and columns were rendered, the expensive pass ran
	// once.
	assert.EqualValues(t, 1, calls.Load())
}

// The whole point of the separate cache (ADR-0173 §SD3): a query that does
// not select a volume column must not pay for the counting pass.
func TestVolumeCacheIsNotTouchedByUnrelatedProjection(t *testing.T) {
	var calls atomic.Int64
	vol := stubVolumeCache(&calls, nil)

	rec := packagesTable(readyCache().snap, vol).
		Build(introspect.Columns("import_path", "class", "num_imports"), 2)
	defer rec.Release()

	require.EqualValues(t, 2, rec.NumRows())
	assert.EqualValues(t, 0, calls.Load(), "counting must not run for a projection that omits volume columns")
}

// A nil cache is what the schema-only path and the existing tests pass; it
// must read as zeroes, never panic.
func TestVolumeCacheNilIsSafe(t *testing.T) {
	var inst *volumeCache
	assert.Equal(t, codevol.Volume{}, inst.get("anything"))
}
