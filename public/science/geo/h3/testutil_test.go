//go:build llm_generated_opus47

package h3

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestRuntime constructs a Runtime for the test or skips the test with a
// clear instruction when the embedded wasm has not been built yet (the
// repository ships an 8-byte placeholder until scripts/dev/build_h3_wasm.sh
// runs against a host that has the wasm32-unknown-unknown target).
func newTestRuntime(tb testing.TB, poolSize int) (inst *Runtime) {
	tb.Helper()
	var err error
	inst, err = NewRuntime(context.Background(), RuntimeConfig{PoolSize: poolSize})
	if err != nil {
		if errors.Is(err, ErrExportNotFound) || errors.Is(err, ErrNoWasmBytes) {
			tb.Skipf("h3 wasm bridge not built; run scripts/dev/build_h3_wasm.sh (%v)", err)
			return
		}
		require.NoError(tb, err)
	}
	tb.Cleanup(func() {
		_ = inst.Close()
	})
	return
}

func readNDJSON[T any](tb testing.TB, file string) (out []T) {
	tb.Helper()
	p := filepath.Join("testdata", file)
	f, err := os.Open(p)
	require.NoError(tb, err)
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec T
		require.NoError(tb, json.Unmarshal(line, &rec))
		out = append(out, rec)
	}
	require.NoError(tb, sc.Err())
	return
}
