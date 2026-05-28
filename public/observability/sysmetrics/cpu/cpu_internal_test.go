//go:build llm_generated_opus47

package cpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCPUSet(t *testing.T) {
	cases := []struct {
		in   string
		want []int32
	}{
		{"", nil},
		{"0", []int32{0}},
		{"0-3", []int32{0, 1, 2, 3}},
		{"0-1,3", []int32{0, 1, 3}},
		{"0,2,4-5", []int32{0, 2, 4, 5}},
		{" 0-3 \n", []int32{0, 1, 2, 3}},
		{"5-3", nil}, // inverted range — silently dropped per btop tolerance
		{"foo", nil},
	}
	for _, c := range cases {
		got := parseCPUSet(c.in)
		assert.Equal(t, c.want, got, "input=%q", c.in)
	}
}

func TestParseStat_Basic(t *testing.T) {
	in := []byte("cpu  10 20 30 40 50 0 0 0 0 0\n" +
		"cpu0 2 4 6 8 10 0 0 0 0 0\n" +
		"cpu1 2 4 6 8 10 0 0 0 0 0\n" +
		"intr 12345\n")
	total, perCore, err := parseStat(in)
	assert.NoError(t, err)
	assert.Equal(t, 10, total.fieldCount)
	assert.Len(t, perCore, 2)
}

func TestParseStat_TooFewFields(t *testing.T) {
	in := []byte("cpu 1 2\n")
	_, _, err := parseStat(in)
	assert.Error(t, err)
}

func TestParseStat_MissingAggregate(t *testing.T) {
	in := []byte("intr 1 2 3\n")
	_, _, err := parseStat(in)
	assert.Error(t, err)
}

func TestPct_Clamping(t *testing.T) {
	// Standard case: dT=100, dI=25 → 75%
	assert.Equal(t, uint8(75), pct(200, 75, 100, 50))

	// All-idle case: dT == dI → 0
	assert.Equal(t, uint8(0), pct(200, 150, 100, 50))

	// Counter went backwards (kernel weirdness): clamp to 0
	assert.Equal(t, uint8(0), pct(50, 25, 100, 50))

	// Idle exceeds total (impossible but defend)
	assert.Equal(t, uint8(0), pct(150, 200, 100, 50))
}

func TestCpuLine_TotalAndIdle_Excludes_Guest(t *testing.T) {
	cl := cpuLine{
		times:      [10]uint64{100, 50, 200, 1000, 50, 0, 0, 0, 75, 25},
		fieldCount: 10,
	}
	total, idle := cl.totalAndIdle()
	// Sum=100+50+200+1000+50+0+0+0+75+25=1500. Subtract guest (75) + guest_nice (25) = 1400.
	assert.Equal(t, uint64(1400), total)
	// idle = 1000 + 50 = 1050
	assert.Equal(t, uint64(1050), idle)
}

func TestCpuLine_FewFields(t *testing.T) {
	cl := cpuLine{
		times:      [10]uint64{100, 50, 200, 1000},
		fieldCount: 4,
	}
	total, idle := cl.totalAndIdle()
	assert.Equal(t, uint64(1350), total)
	assert.Equal(t, uint64(1000), idle)
}

// FuzzParseStat asserts parseStat never panics on arbitrary bytes.
// The parser handles paren-delimited comm fields with embedded spaces
// and unbalanced parens, so adversarial bytes are realistic input.
func FuzzParseStat(f *testing.F) {
	f.Add([]byte("100 (proc) S 1 100 100 0 -1 4194304 0 0 0 0 1 1 0 0 20 0 1 0 100 1024 32 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"))
	f.Add([]byte(""))
	f.Add([]byte("100"))
	f.Add([]byte("100 (weird (name)) S 1 100 100 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 100 1024 32 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"))
	f.Add([]byte("(((\n"))
	f.Add([]byte("100 (\x00\xff)\xc3 S 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = parseStat(data)
	})
}

// FuzzParseCPUSet asserts parseCPUSet handles arbitrary cgroup-cpuset
// strings without panicking. Real-world inputs include comma-separated
// ranges with mixed digit widths and stray whitespace.
func FuzzParseCPUSet(f *testing.F) {
	f.Add("0-3,7,9-11")
	f.Add("")
	f.Add("0")
	f.Add("--")
	f.Add(",,,")
	f.Add("9999999999999999999999")
	f.Add("0-")
	f.Fuzz(func(t *testing.T, s string) {
		_ = parseCPUSet(s)
	})
}
