package logging

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Concurrent writes — zerolog calls Write from every goroutine that logs —
// each land whole, and the logger keeps working after them.
func TestJsonIndentLoggerConcurrentWrites(t *testing.T) {
	var out bytes.Buffer
	l := NewJsonIndentLogger(&out)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, err := l.Write([]byte(`{"level":"info","module":"line","message":"tab"}`))
				require.NoError(t, err)
			}
		}()
	}
	wg.Wait()
	_, err := l.Write([]byte(`{"level":"info","module":"after","message":"tab"}`))
	require.NoError(t, err)
	require.Equal(t, 401, strings.Count(out.String(), `"message": "tab"`))
}
