package wasmsurvey

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/packageprops"
)

func row(path string, wasi packageprops.WASMState) (r HarvestRow) {
	return HarvestRow{ImportPath: path, WASMWASI: wasi, WASMJS: wasi, WASMFreestanding: wasi}
}

func entry(path string, wasi packageprops.WASMState) (e packageprops.Entry) {
	return packageprops.Entry{
		ImportPath: path,
		Props:      packageprops.Props{WASMWASI: wasi, WASMJS: wasi, WASMFreestanding: wasi},
	}
}

func TestDriftAgainstTable_Agrees(t *testing.T) {
	tbl := packageprops.Table{entry("a", packageprops.WASMCompiles)}
	got := DriftAgainstTable(tbl, []HarvestRow{row("a", packageprops.WASMCompiles)})
	assert.Empty(t, got)
}

// The failure the gate exists for: a package is declared and the table has no
// row. Every query over the table then returns a confident short answer — no
// error, no null — which is why this cannot be left to a reader to notice.
func TestDriftAgainstTable_MissingRow(t *testing.T) {
	got := DriftAgainstTable(packageprops.Table{}, []HarvestRow{row("a", packageprops.WASMCompiles)})
	require.Len(t, got, 1)
	assert.Equal(t, DriftMissing, got[0].Kind)
	assert.Equal(t, "a", got[0].ImportPath)
}

// The other direction: a package was deleted or renamed and its row outlived
// it, so the table reports something that does not exist.
func TestDriftAgainstTable_ExtraRow(t *testing.T) {
	tbl := packageprops.Table{entry("gone", packageprops.WASMCompiles)}
	got := DriftAgainstTable(tbl, nil)
	require.Len(t, got, 1)
	assert.Equal(t, DriftExtra, got[0].Kind)
	assert.Equal(t, "gone", got[0].ImportPath)
}

func TestDriftAgainstTable_ChangedVerdict(t *testing.T) {
	tbl := packageprops.Table{entry("a", packageprops.WASMCompiles)}
	got := DriftAgainstTable(tbl, []HarvestRow{row("a", packageprops.WASMBlocked)})
	require.Len(t, got, 1)
	assert.Equal(t, DriftChanged, got[0].Kind)
	assert.Equal(t, packageprops.WASMCompiles, got[0].Table.WASMWASI)
	assert.Equal(t, packageprops.WASMBlocked, got[0].Source.WASMWASI)
}

// Kind is not reconciled by `props verify` (it has no computable oracle,
// ADR-0080's 2026-07-02 entry), so the drift check is the only thing that can
// catch it going stale in the table. Comparing whole Props rather than the
// three WASM fields is what makes that work — and what keeps working when
// ADR-0080 SD4's next field lands.
func TestDriftAgainstTable_CatchesKindOnly(t *testing.T) {
	tbl := packageprops.Table{entry("a", packageprops.WASMCompiles)}
	src := row("a", packageprops.WASMCompiles)
	src.Kind = packageprops.KindIntegrationTest
	got := DriftAgainstTable(tbl, []HarvestRow{src})
	require.Len(t, got, 1)
	assert.Equal(t, DriftChanged, got[0].Kind)
}

func TestDriftAgainstTable_OrdersByImportPath(t *testing.T) {
	tbl := packageprops.Table{entry("z", packageprops.WASMCompiles)}
	got := DriftAgainstTable(tbl, []HarvestRow{row("a", packageprops.WASMCompiles)})
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].ImportPath)
	assert.Equal(t, "z", got[1].ImportPath)
}
