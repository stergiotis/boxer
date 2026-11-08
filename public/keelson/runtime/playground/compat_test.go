package playground_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/factswrapper"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Facts-CBOR bytes as written before the DTO moved here (ADR-0361 §SD1,
// Verification plan), captured from apps/play/launchcfg.PlayLaunch at the
// move. A launch row's config blob (ADR-0135 §SD6) and a stored workingset
// record (ADR-0148) are these bytes; if a future change alters the wire,
// these fail rather than leaving old rows undecodable. They were encoded by
// the old package under the kind `playLaunch`.
var (
	beforeMoveAt = time.Unix(1700000000, 0).UTC()

	launchRowHex = "81b81a626964006a6e61747572616c4b6579406274731b17979cfe362a0000696578706972657341743b5e4dfc14c2e5ffff6f7465787441727261792e76616c756582782453454c45435420636f756e7428292046524f4d206b65656c736f6e28276974656d7327297253454c454354202761272041532062616e646c7465787441727261792e6c72821b00000003000000851b00000003000000886d7465787441727261792e6c656e820101707465787441727261792e687263617264820000707465787441727261792e6c7263617264820101717465787441727261792e6c6d726361726482000071737472696e6741727261792e76616c756584656974656d736572756c657366726573756c7461726e737472696e6741727261792e6c72821b00000003000000c31b00000003000000f06f737472696e6741727261792e6c656e82020272737472696e6741727261792e68726361726482000072737472696e6741727261792e6c726361726482010173737472696e6741727261792e6c6d72636172648200006c73796d626f6c2e76616c7565836874696d656c696e656d696e74726f7370656374696f6e6573616c65736973796d626f6c2e6c72831b00000003000000891b000000030000008a1b00000003000000ee6d73796d626f6c2e687263617264830000006d73796d626f6c2e6c7263617264830101016e73796d626f6c2e6c6d7263617264830000006a626f6f6c2e76616c756582f5f567626f6f6c2e6c72821b00000003000000861b00000003000000876b626f6f6c2e6872636172648200006b626f6f6c2e6c72636172648201016c626f6f6c2e6c6d7263617264820000"

	workingsetHex = "81b4626964006a6e61747572616c4b6579406274731b17979cfe362a0000696578706972657341743b5e4dfc14c2e5ffff6f7465787441727261792e76616c7565827153454c4543542027726573746f726564276a53454c454354202762276c7465787441727261792e6c72821b00000003000000851b00000003000000886d7465787441727261792e6c656e820101707465787441727261792e687263617264820000707465787441727261792e6c7263617264820101717465787441727261792e6c6d72636172648200006c73796d626f6c2e76616c756583657461626c6560606973796d626f6c2e6c72831b00000003000000891b000000030000008a1b00000003000000ee6d73796d626f6c2e687263617264830000006d73796d626f6c2e6c7263617264830101016e73796d626f6c2e6c6d7263617264830000006a626f6f6c2e76616c756582f4f567626f6f6c2e6c72821b00000003000000861b00000003000000876b626f6f6c2e6872636172648200006b626f6f6c2e6c72636172648201016c626f6f6c2e6c6d7263617264820000"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

// TestLaunchRowBytesDecodeUnderTheMovedDTO decodes a fully populated
// launch-row config encoded under the old package, and re-encodes the
// decoded value to the identical bytes: the DTO's wire form did not move.
func TestLaunchRowBytesDecodeUnderTheMovedDTO(t *testing.T) {
	before := mustHex(t, launchRowHex)
	got, err := buscodec.Decode[playground.Launch](before)
	require.NoError(t, err)
	want := playground.Launch{
		At: beforeMoveAt, Sql: "SELECT count() FROM keelson('items')",
		AutoRun: true, Live: true, BandsSql: "SELECT 'a' AS band", Tab: "timeline",
		Endpoint: playground.EndpointIntrospection,
		Datasets: []string{"items", "rules"}, DatasetNames: []string{"result", "r"}, Bundle: "sales",
	}
	got.At, want.At = got.At.UTC(), want.At.UTC()
	assert.Equal(t, want.At, got.At)
	assert.Equal(t, want.Sql, got.Sql)
	assert.Equal(t, want.BandsSql, got.BandsSql)
	assert.Equal(t, want.Tab, got.Tab)
	assert.Equal(t, want.Endpoint, got.Endpoint)
	assert.Equal(t, want.Datasets, got.Datasets)
	assert.Equal(t, want.DatasetNames, got.DatasetNames)
	assert.Equal(t, want.Bundle, got.Bundle)
	assert.True(t, got.AutoRun)
	assert.True(t, got.Live)

	reencoded, err := buscodec.Encode(want)
	require.NoError(t, err)
	assert.Equal(t, before, reencoded, "the wire form of the moved DTO is byte-identical")
}

// TestWorkingsetBytesDecodeUnderTheMovedDTO decodes a workingset record
// as play's ComposeLaunch produced it under the old package (ADR-0148
// §SD4), whose omitted fields are empty and whose restored Sql survives.
func TestWorkingsetBytesDecodeUnderTheMovedDTO(t *testing.T) {
	before := mustHex(t, workingsetHex)
	got, err := buscodec.Decode[playground.Launch](before)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'restored'", got.Sql)
	assert.Equal(t, "SELECT 'b'", got.BandsSql)
	assert.Equal(t, "table", got.Tab)
	assert.True(t, got.Live)
	assert.False(t, got.AutoRun)
	assert.Empty(t, got.Datasets)
	assert.Empty(t, got.Bundle)
	assert.Equal(t, beforeMoveAt, got.At.UTC())
}

// TestCodecIsPlaysGoldenRenamed generates the codec from the DTO as it
// stood in apps/play/launchcfg (testdata, frozen at the move) and from
// this package's, and asserts they differ only by the package name and the
// Go type name (ADR-0361 §SD1: the Go names name the role). A generator
// change moves both, so this holds across generator work while still
// catching a change to the moved DTO.
func TestCodecIsPlaysGoldenRenamed(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	before, err := factswrapper.FactsWrapper{}.Generate(filepath.Join(dir, "testdata", "launchcfg_before_move.go.txt"), "")
	require.NoError(t, err)
	after, err := factswrapper.FactsWrapper{}.Generate(filepath.Join(dir, "playground.go"), "")
	require.NoError(t, err)
	committed, err := os.ReadFile(filepath.Join(dir, "playground.out.go"))
	require.NoError(t, err)
	require.Equal(t, string(after), string(committed), "the committed golden is the generator's output")

	renamed := string(before)
	renamed = strings.Replace(renamed, "package launchcfg\n", "package playground\n", 1)
	renamed = strings.ReplaceAll(renamed, `"launchcfg: `, `"playground: `)
	// Identifiers derived from the Go type name PlayLaunch, not from the wire
	// kind or the memberships: "playLaunch…" wire names and vdd members keep
	// their spelling.
	renamed = regexp.MustCompile(`\bnewPlayLaunchReader\b`).ReplaceAllString(renamed, "newLaunchReader")
	renamed = regexp.MustCompile(`\bplayLaunch(BusCodec|Pool|Reader)\b`).ReplaceAllString(renamed, "launch$1")
	renamed = regexp.MustCompile(`\bPlayLaunch`).ReplaceAllString(renamed, "Launch")
	if renamed != string(after) {
		t.Fatalf("the moved codec differs from play's golden by more than the renames; generator output length %d vs %d", len(renamed), len(after))
	}
}
