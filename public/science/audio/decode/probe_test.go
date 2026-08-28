package decode

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ffprobe prints most numbers as JSON strings and a few bare, so the fixtures
// below are shaped the way ffprobe 8's `-of json` output actually is.
const probeStereoWAV = `{
    "programs": [],
    "stream_groups": [],
    "streams": [
        {
            "sample_rate": "48000",
            "channels": 2,
            "duration": "3.000000"
        }
    ],
    "format": {
        "duration": "3.000000"
    }
}`

func TestParseProbeStreamDuration(t *testing.T) {
	format, frames, err := parseProbeE([]byte(probeStereoWAV))
	require.NoError(t, err)
	require.EqualValues(t, 48000, format.SampleRate)
	require.EqualValues(t, 2, format.Channels)
	require.Equal(t, int64(144000), frames)
}

func TestParseProbeFallsBackToFormatDuration(t *testing.T) {
	const fixture = `{"streams":[{"sample_rate":"44100","channels":1,"duration":"N/A"}],
	                 "format":{"duration":"2.5"}}`
	format, frames, err := parseProbeE([]byte(fixture))
	require.NoError(t, err)
	require.EqualValues(t, 44100, format.SampleRate)
	require.EqualValues(t, 1, format.Channels)
	require.Equal(t, int64(110250), frames)
}

func TestParseProbeAcceptsBareNumbers(t *testing.T) {
	const fixture = `{"streams":[{"sample_rate":16000,"channels":6,"duration":1.5}],"format":{}}`
	format, frames, err := parseProbeE([]byte(fixture))
	require.NoError(t, err)
	require.EqualValues(t, 16000, format.SampleRate)
	require.EqualValues(t, 6, format.Channels, "more than two channels is decoded as it is")
	require.Equal(t, int64(24000), frames)
}

func TestParseProbeRoundsTheFrameCount(t *testing.T) {
	const fixture = `{"streams":[{"sample_rate":"48000","channels":2,"duration":"0.123456789"}],"format":{}}`
	_, frames, err := parseProbeE([]byte(fixture))
	require.NoError(t, err)
	require.Equal(t, int64(5926), frames)
}

func TestParseProbeRejectsUnusableOutput(t *testing.T) {
	cases := map[string]string{
		"no audio stream":      `{"streams":[],"format":{"duration":"3.0"}}`,
		"no duration anywhere": `{"streams":[{"sample_rate":"48000","channels":2}],"format":{}}`,
		"duration not availab": `{"streams":[{"sample_rate":"48000","channels":2,"duration":"N/A"}],"format":{"duration":"N/A"}}`,
		"zero duration":        `{"streams":[{"sample_rate":"48000","channels":2,"duration":"0.000000"}],"format":{"duration":"0"}}`,
		"null duration":        `{"streams":[{"sample_rate":"48000","channels":2,"duration":null}],"format":{"duration":null}}`,
		"no sample rate":       `{"streams":[{"channels":2,"duration":"3.0"}],"format":{}}`,
		"zero sample rate":     `{"streams":[{"sample_rate":"0","channels":2,"duration":"3.0"}],"format":{}}`,
		"no channels":          `{"streams":[{"sample_rate":"48000","duration":"3.0"}],"format":{}}`,
		"garbled number":       `{"streams":[{"sample_rate":"forty-eight thousand","channels":2,"duration":"3.0"}],"format":{}}`,
		"not json":             `ffprobe: command not found`,
	}
	for name, fixture := range cases {
		t.Run(name, func(t *testing.T) {
			_, frames, err := parseProbeE([]byte(fixture))
			require.Error(t, err)
			require.Zero(t, frames)
		})
	}
}

func TestProbeArgsSelectTheFirstAudioStream(t *testing.T) {
	args := probeArgs("/tmp/x.flac")
	require.Equal(t, []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=sample_rate,channels,duration:format=duration",
		"-of", "json",
		"/tmp/x.flac",
	}, args)
}
