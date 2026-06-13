package mem

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMeminfo_NoTotal_Errors(t *testing.T) {
	_, err := parseMeminfo([]byte("MemFree: 1024 kB\n"))
	assert.Error(t, err)
}

func TestParseMeminfo_KBSuffix(t *testing.T) {
	got, err := parseMeminfo([]byte("MemTotal:       2048 kB\nMemFree: 1024 kB\n"))
	assert.NoError(t, err)
	assert.Equal(t, uint64(2048<<10), got.TotalBytes)
	assert.Equal(t, uint64(1024<<10), got.FreeBytes)
}

func TestParseKB_Variants(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"1024 kB", 1024 << 10, true},
		{"1024 KB", 1024 << 10, true},
		{"1024", 1024, true},
		{"  1024 kB", 1024 << 10, true},
		{"", 0, false},
		{"abc", 0, false},
		{"-5 kB", 0, false}, // strconv.ParseUint rejects negative
	}
	for _, c := range cases {
		n, ok := parseKB([]byte(c.in))
		assert.Equal(t, c.ok, ok, "input=%q", c.in)
		if ok {
			assert.Equal(t, c.want, n, "input=%q", c.in)
		}
	}
}

// FuzzParseMeminfo asserts parseMeminfo never panics on arbitrary
// bytes. The parser reads /proc/meminfo which is well-formed in
// practice but may contain unusual unit suffixes on patched kernels.
func FuzzParseMeminfo(f *testing.F) {
	f.Add([]byte("MemTotal: 16384000 kB\nMemFree: 8192000 kB\n"))
	f.Add([]byte(""))
	f.Add([]byte("MemTotal:\n"))
	f.Add([]byte("MemTotal: -1 kB\n"))
	f.Add([]byte("MemTotal: 99999999999999999999999 kB\n"))
	f.Add([]byte(":::\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseMeminfo(data)
	})
}

// FuzzParseKB asserts parseKB never panics on arbitrary bytes.
func FuzzParseKB(f *testing.F) {
	f.Add([]byte("1024 kB"))
	f.Add([]byte(""))
	f.Add([]byte("    "))
	f.Add([]byte("9999999999999999999999"))
	f.Add([]byte("\xff\x00 kB"))
	f.Add([]byte("123 kBkB"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseKB(data)
	})
}
