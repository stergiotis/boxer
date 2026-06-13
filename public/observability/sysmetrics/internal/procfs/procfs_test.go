package procfs_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
)

func TestReader_ReadFile(t *testing.T) {
	r := procfs.New("testdata")
	data, err := r.ReadFile("meminfo")
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestReader_ReadFile_NotFound(t *testing.T) {
	r := procfs.New("testdata")
	_, err := r.ReadFile("does-not-exist")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "expected fs.ErrNotExist sentinel preserved through eb wrap")
}

func TestReader_ReadFileInto_ReusesBuffer(t *testing.T) {
	r := procfs.New("testdata")
	// First call seeds the scratch from nil; second call must reuse the
	// same backing array as long as the file fits in the previous cap.
	first, err := r.ReadFileInto("meminfo", nil)
	require.NoError(t, err)
	require.NotEmpty(t, first)
	firstCap := cap(first)
	firstBackingPtr := &first[:1][0]

	second, err := r.ReadFileInto("meminfo", first[:0])
	require.NoError(t, err)
	require.Equal(t, first, second, "byte content must be identical across reuse")
	assert.Equal(t, firstCap, cap(second), "buffer reuse must not reallocate when cap suffices")
	assert.Same(t, firstBackingPtr, &second[:1][0], "second read must alias the first read's backing array")
}

func TestReader_ReadFileInto_GrowsFromSmallSeed(t *testing.T) {
	r := procfs.New("testdata")
	// 8-byte seed forces at least one geometric growth before meminfo
	// (>1 KB on the fixture) can fit. The returned slice must still
	// hold the full content.
	seed := make([]byte, 0, 8)
	out, err := r.ReadFileInto("meminfo", seed)
	require.NoError(t, err)
	require.NotEmpty(t, out)
	assert.GreaterOrEqual(t, cap(out), len(out), "returned slice must be self-consistent")
}

func TestReader_ReadFileInto_NotFound(t *testing.T) {
	r := procfs.New("testdata")
	_, err := r.ReadFileInto("does-not-exist", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "fs.ErrNotExist sentinel must survive the eb wrap")
}

func TestReader_DefaultRoot(t *testing.T) {
	r := procfs.New("")
	assert.Equal(t, procfs.DefaultRoot, r.Root())
}

func TestIterLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a\n", []string{"a"}},
		{"a\nb\nc\n", []string{"a", "b", "c"}},
		{"a\nb", []string{"a", "b"}},
		{"\n", []string{""}},
		{"\n\nx\n", []string{"", "", "x"}},
	}
	for _, c := range cases {
		got := collectStrings(procfs.IterLines([]byte(c.in)))
		assert.Equal(t, c.want, got, "input=%q", c.in)
	}
}

func TestIterLines_BreakEarly(t *testing.T) {
	var got []string
	for line := range procfs.IterLines([]byte("a\nb\nc\n")) {
		got = append(got, string(line))
		if len(got) == 2 {
			break
		}
	}
	assert.Equal(t, []string{"a", "b"}, got)
}

func TestIterFields(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{"a b c", []string{"a", "b", "c"}},
		{"  a   b\tc ", []string{"a", "b", "c"}},
		{"\ta\tb\t", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := collectStrings(procfs.IterFields([]byte(c.in)))
		assert.Equal(t, c.want, got, "input=%q", c.in)
	}
}

func TestIterKV(t *testing.T) {
	in := "MemTotal:       16384000 kB\n" +
		"MemFree:         8192000 kB\n" +
		"\n" +
		"NoColonLine\n" +
		"Cached:          4096000 kB\n"

	type kv struct{ k, v string }
	var got []kv
	for k, v := range procfs.IterKV([]byte(in)) {
		got = append(got, kv{string(k), string(v)})
	}
	assert.Equal(t, []kv{
		{"MemTotal", "16384000 kB"},
		{"MemFree", "8192000 kB"},
		{"Cached", "4096000 kB"},
	}, got)
}

func TestIterKV_BreakEarly(t *testing.T) {
	in := "A:1\nB:2\nC:3\n"
	var got [][2]string
	for k, v := range procfs.IterKV([]byte(in)) {
		got = append(got, [2]string{string(k), string(v)})
		if len(got) == 2 {
			break
		}
	}
	assert.Equal(t, [][2]string{{"A", "1"}, {"B", "2"}}, got)
}

func TestReader_ReadsRealMeminfo(t *testing.T) {
	// Smoke test against the live /proc on Linux test runners.
	if _, statErr := os.Stat("/proc/meminfo"); statErr != nil {
		t.Skipf("no live /proc/meminfo available: %v", statErr)
	}
	r := procfs.New("")
	data, err := r.ReadFile("meminfo")
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var sawMemTotal bool
	for k := range procfs.IterKV(data) {
		if string(k) == "MemTotal" {
			sawMemTotal = true
			break
		}
	}
	assert.True(t, sawMemTotal, "live /proc/meminfo should expose MemTotal")
}

func TestReader_FixtureTree(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "synth"), []byte("hello\n"), 0o644))
	r := procfs.New(tmp)
	data, err := r.ReadFile("synth")
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(data))
}

// FuzzIterKV asserts the key:value iterator never panics on arbitrary
// bytes. Real /proc files have stable shape but kernel-quirk patches
// can produce surprising line content; the iterator must be robust.
func FuzzIterKV(f *testing.F) {
	f.Add([]byte("MemTotal: 16384000 kB\n"))
	f.Add([]byte(""))
	f.Add([]byte(":\n"))
	f.Add([]byte("a:b:c:d\n"))
	f.Add([]byte("\x00\xff\n"))
	f.Add([]byte("no-colon-line\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for k, v := range procfs.IterKV(data) {
			_ = k
			_ = v
		}
	})
}

// FuzzIterFields asserts whitespace-tokenization never panics.
func FuzzIterFields(f *testing.F) {
	f.Add([]byte("a b c"))
	f.Add([]byte(""))
	f.Add([]byte("\t\t\t"))
	f.Add([]byte("\xff\xff\xff"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for v := range procfs.IterFields(data) {
			_ = v
		}
	})
}

// FuzzIterLines asserts line iteration never panics.
func FuzzIterLines(f *testing.F) {
	f.Add([]byte("a\nb\nc\n"))
	f.Add([]byte(""))
	f.Add([]byte("\n\n\n"))
	f.Add([]byte("no-newline-at-end"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for l := range procfs.IterLines(data) {
			_ = l
		}
	})
}

func collectStrings(seq func(yield func([]byte) bool)) (out []string) {
	for b := range seq {
		out = append(out, string(b))
	}
	if out == nil {
		return nil
	}
	return slices.Clone(out)
}
