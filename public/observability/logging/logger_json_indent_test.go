package logging

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// zerologEvent is one event as zerolog writes it in this build: JSON, or CBOR
// under binary_log, which is what JsonIndentLogger then reads.
func zerologEvent(module string) []byte {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Info().Str("module", module).Msg("tab")
	return buf.Bytes()
}

// Concurrent writes — zerolog calls Write from every goroutine that logs —
// each land whole, and the logger keeps working after them.
func TestJsonIndentLoggerConcurrentWrites(t *testing.T) {
	var out bytes.Buffer
	l := NewJsonIndentLogger(&out)
	line, after := zerologEvent("line"), zerologEvent("after")
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, err := l.Write(line)
				require.NoError(t, err)
			}
		}()
	}
	wg.Wait()
	_, err := l.Write(after)
	require.NoError(t, err)
	require.Equal(t, 401, strings.Count(out.String(), `"message": "tab"`))
}
