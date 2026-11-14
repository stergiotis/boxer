package lanesink

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The wire fixture lives in the viewer page's audio suite, between two
// markers, where the page's reader is tested against it. This test holds the
// Go codec to the same bytes: each entry must be what the Append function
// writes for its fields, and must decode to them (ADR-0372, proposed,
// verification plan).
const (
	fixtureFile  = "rust/imzero2/src/imzero2/viewer/tests/suite-phase5-audio-lane.js"
	fixtureBegin = "/* lane-fixture-begin */"
	fixtureEnd   = "/* lane-fixture-end */"
)

type fixtureEntry struct {
	Name       string  `json:"name"`
	Hex        string  `json:"hex"`
	Stream     uint32  `json:"stream"`
	Epoch      uint32  `json:"epoch"`
	SampleRate uint32  `json:"sampleRate"`
	Channels   uint8   `json:"channels"`
	State      uint8   `json:"state"`
	Frames     uint64  `json:"frames"`
	First      uint64  `json:"first"`
	Samples    []int16 `json:"samples"`
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) (root string) {
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err = os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above the package")
		dir = parent
	}
}

func TestWireMatchesTheViewerPagesFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(fixtureFile)))
	if os.IsNotExist(err) {
		t.Skipf("the viewer page's suite is not in this tree: %v", err)
	}
	require.NoError(t, err)
	text := string(raw)
	begin, end := strings.Index(text, fixtureBegin), strings.Index(text, fixtureEnd)
	require.True(t, begin >= 0 && end > begin, "the fixture's markers are missing from %s", fixtureFile)
	var entries []fixtureEntry
	require.NoError(t, json.Unmarshal([]byte(text[begin+len(fixtureBegin):end]), &entries))

	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Name] = true
		want, err := hex.DecodeString(strings.Join(strings.Fields(e.Hex), ""))
		require.NoError(t, err, e.Name)
		var got []byte
		expect := Message{Stream: e.Stream, Epoch: e.Epoch}
		switch e.Name {
		case "want":
			got = AppendWant(nil)
			expect.Kind = KindWant
		case "open":
			got = AppendOpen(nil, e.Stream, e.Epoch, e.SampleRate, e.Channels)
			expect.Kind, expect.SampleRate, expect.Channels, expect.Format = KindOpen, e.SampleRate, e.Channels, SampleFormatS16LE
		case "chunk":
			samples := make([]float32, len(e.Samples))
			for i, s := range e.Samples {
				samples[i] = float32(s) / 32767
			}
			got = AppendChunk(nil, e.Stream, e.Epoch, e.First, samples)
			expect.Kind, expect.Frames, expect.Samples = KindChunk, e.First, want[chunkHeaderLen:]
		case "flush":
			got = AppendFlush(nil, e.Stream, e.Epoch)
			expect.Kind = KindFlush
		case "close":
			got = AppendClose(nil, e.Stream)
			expect.Kind = KindClose
		case "probe":
			got = AppendProbe(nil)
			expect.Kind = KindProbe
		case "device", "suspended":
			got = AppendDevice(nil, DeviceStateE(e.State), e.SampleRate)
			expect.Kind, expect.State, expect.SampleRate = KindDevice, DeviceStateE(e.State), e.SampleRate
		case "played":
			got = AppendPlayed(nil, e.Stream, e.Epoch, e.Frames)
			expect.Kind, expect.Frames = KindPlayed, e.Frames
		default:
			t.Fatalf("the fixture has an entry %q this test does not know", e.Name)
		}
		require.Equal(t, hex.EncodeToString(want), hex.EncodeToString(got), "the bytes of %q", e.Name)
		m, err := Decode(want)
		require.NoError(t, err, e.Name)
		require.Equal(t, expect, m, "the fields of %q", e.Name)
	}
	// The states are spelled out in the fixture as numbers, which is what
	// the page writes; they must be this package's.
	require.Equal(t, DeviceStateE(1), DeviceSuspended)
	require.Equal(t, DeviceStateE(2), DeviceRunning)
	for _, name := range []string{"want", "probe", "open", "chunk", "flush", "close", "device", "suspended", "played"} {
		require.True(t, seen[name], "the fixture has no %q entry", name)
	}
}
