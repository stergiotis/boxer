//go:build llm_generated_opus47

package proc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseStat_Plain(t *testing.T) {
	in := []byte("1234 (bash) S 1233 1234 1234 0 -1 4194304 100 200 5 10 1500 50 0 0 20 0 1 0 12345 1234567 890 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	got, err := parseStat(in)
	assert.NoError(t, err)
	assert.Equal(t, byte('S'), got.state)
	assert.Equal(t, uint32(1233), got.ppid)
	assert.Equal(t, uint64(1500), got.utime)
	assert.Equal(t, uint64(50), got.stime)
	assert.Equal(t, int32(20), got.priority)
	assert.Equal(t, int32(0), got.nice)
	assert.Equal(t, int32(1), got.numThreads)
	assert.Equal(t, uint64(12345), got.starttimeTicks)
	assert.Equal(t, uint64(1234567), got.vsize)
	assert.Equal(t, uint64(890), got.rssPages)
}

func TestParseStat_CommWithSpaces(t *testing.T) {
	in := []byte("100 (program with spaces) R 1 100 100 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 100 1024 32 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	got, err := parseStat(in)
	assert.NoError(t, err)
	assert.Equal(t, byte('R'), got.state)
	assert.Equal(t, uint32(1), got.ppid)
}

func TestParseStat_CommWithParens(t *testing.T) {
	in := []byte("100 (weird (name)) S 1 100 100 0 -1 0 0 0 0 0 1 1 0 0 20 0 1 0 100 1024 32 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	got, err := parseStat(in)
	assert.NoError(t, err)
	assert.Equal(t, byte('S'), got.state)
	assert.Equal(t, uint32(1), got.ppid)
}

func TestParseStat_NoParens(t *testing.T) {
	in := []byte("1234 noparens S 1 1 1 0 -1\n")
	_, err := parseStat(in)
	assert.Error(t, err)
}

func TestParseStat_TooFewFields(t *testing.T) {
	in := []byte("1234 (short) S 1\n")
	_, err := parseStat(in)
	assert.Error(t, err)
}

func TestParseStatusUidGid(t *testing.T) {
	in := []byte("Name:\tbash\nState:\tS (sleeping)\nUid:\t1000\t1000\t1000\t1000\nGid:\t100\t100\t100\t100\n")
	uid, gid, ok := parseStatusUidGid(in)
	assert.True(t, ok)
	assert.Equal(t, uint32(1000), uid)
	assert.Equal(t, uint32(100), gid)
}

func TestParseStatusUidGid_Missing(t *testing.T) {
	in := []byte("Name:\tbash\nState:\tS\n")
	_, _, ok := parseStatusUidGid(in)
	assert.False(t, ok)
}

func TestFormatCmdline(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"\x00\x00", ""},
		{"bash\x00", "bash"},
		{"/usr/bin/bash\x00-c\x00echo\x00", "/usr/bin/bash -c echo"},
		{"x\x00y", "x y"},
	}
	for _, c := range cases {
		got := formatCmdline([]byte(c.in))
		assert.Equal(t, c.want, got, "input=%q", c.in)
	}
}

func TestFormatCmdline_Truncates(t *testing.T) {
	in := make([]byte, CmdMaxBytes+50)
	for i := range in {
		in[i] = 'a'
	}
	got := formatCmdline(in)
	assert.Equal(t, CmdMaxBytes, len(got))
}

// FuzzParseStat covers the same paren-edge-case territory as cpu's
// parseStat — separately fuzzed because /proc/[pid]/stat carries more
// fields than /proc/stat and the same parser handles both shapes.
func FuzzParseStat(f *testing.F) {
	f.Add([]byte("1234 (bash) S 1233 1234 1234 0 -1 4194304 100 200 5 10 1500 50 0 0 20 0 1 0 12345 1234567 890 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"))
	f.Add([]byte(""))
	f.Add([]byte("(("))
	f.Add([]byte("1 () X 0\n"))
	f.Add([]byte("\xff\x00\xc3\x28\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseStat(data)
	})
}

// FuzzFormatCmdline asserts formatCmdline never panics on arbitrary
// bytes. The function mutates the input slice in place (NUL → space)
// so adversarial bytes including embedded multi-byte UTF-8 are useful
// fuzz seeds.
func FuzzFormatCmdline(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("\x00\x00\x00"))
	f.Add([]byte("/usr/bin/bash\x00-c\x00echo\x00hi\x00"))
	f.Add(make([]byte, CmdMaxBytes*2))
	f.Add([]byte("\xff\xfe\xfd"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = formatCmdline(data)
	})
}

// FuzzParseStatusUidGid handles mixed key:value content where the
// "Uid:" and "Gid:" lines may be malformed.
func FuzzParseStatusUidGid(f *testing.F) {
	f.Add([]byte("Name:\tbash\nUid:\t1000\t1000\t1000\t1000\nGid:\t100\t100\t100\t100\n"))
	f.Add([]byte(""))
	f.Add([]byte("Uid:\n"))
	f.Add([]byte("Uid: foo bar baz\n"))
	f.Add([]byte("Uid:\t999999999999999999999\t0\t0\t0\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = parseStatusUidGid(data)
	})
}
