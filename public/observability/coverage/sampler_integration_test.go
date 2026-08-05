//go:build integration

package coverage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The sampler probe is a separate module that imports THIS package via a
// replace directive and runs the real WriteCounters→decode→fold loop inside
// a live instrumented binary — the happy path that test binaries cannot
// reach. Only the probe's own module is instrumented, so the sampler code
// itself stays uninstrumented (no observer effect).
const samplerProbeMain = `package main

import (
	"fmt"
	"os"

	cov "github.com/stergiotis/boxer/public/observability/coverage"
)

func extraCode(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i * i
	}
	return s
}

func main() {
	s, err := cov.NewSampler(cov.SamplerOptions{})
	if err != nil {
		fmt.Println("CONSTRUCT_ERR", err)
		os.Exit(1)
	}
	u1, err := s.Sample()
	if err != nil {
		fmt.Println("SAMPLE_ERR", err)
		os.Exit(1)
	}
	fmt.Println("U1", u1.Full, u1.Units.GetCardinality(), u1.Status.CoveredUnits, u1.Status.TotalUnits, len(u1.Funcs))
	_ = extraCode(5)
	u2, err := s.Sample()
	if err != nil {
		fmt.Println("SAMPLE_ERR", err)
		os.Exit(1)
	}
	fmt.Println("U2", u2.Full, u2.Units.GetCardinality(), u2.Status.CoveredUnits)
}
`

func TestSamplerInsideLiveInstrumentedBinary(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)

	dir := t.TempDir()
	goMod := "module covsamplerprobe\n\ngo 1.26\n\nreplace github.com/stergiotis/boxer => " + repoRoot + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(samplerProbeMain), 0o644))

	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOPROXY=off")
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = env
	out, err := tidy.CombinedOutput()
	require.NoError(t, err, "go mod tidy failed: %s", out)

	probeBin := filepath.Join(dir, "probe")
	build := exec.Command("go", "build", "-cover", "-covermode=atomic", "-o", probeBin, ".")
	build.Dir = dir
	build.Env = env
	out, err = build.CombinedOutput()
	require.NoError(t, err, "go build -cover failed: %s", out)

	run := exec.Command(probeBin)
	run.Dir = dir
	// GOCOVERDIR silences the instrumented binary's exit-emission warning
	// (the strict line parser below would trip on it).
	run.Env = append(env, "GOCOVERDIR="+t.TempDir())
	out, err = run.CombinedOutput()
	require.NoError(t, err, "probe run failed: %s", out)

	var u1, u2 []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "U1":
			u1 = fields[1:]
		case "U2":
			u2 = fields[1:]
		default:
			t.Fatalf("unexpected probe output line %q (full output: %s)", line, out)
		}
	}
	require.Len(t, u1, 5, "probe output: %s", out)
	require.Len(t, u2, 3, "probe output: %s", out)

	num := func(s string) uint64 {
		v, err := strconv.ParseUint(s, 10, 64)
		require.NoError(t, err)
		return v
	}

	// First sample: full statement, Units IS the covered set.
	require.Equal(t, "true", u1[0])
	card1, covered1, total1, funcs1 := num(u1[1]), num(u1[2]), num(u1[3]), num(u1[4])
	require.Equal(t, covered1, card1)
	require.Positive(t, covered1)
	require.Greater(t, total1, covered1, "extraCode has not run yet")
	require.Positive(t, funcs1)

	// Second sample: delta with extraCode's units; absolute totals grew by
	// exactly the delta's cardinality.
	require.Equal(t, "false", u2[0])
	card2, covered2 := num(u2[1]), num(u2[2])
	require.Positive(t, card2)
	require.Equal(t, covered1+card2, covered2)
}
