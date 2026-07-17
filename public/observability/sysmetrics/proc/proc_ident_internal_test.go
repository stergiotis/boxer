package proc

import "testing"

func TestParseCgroupUnit(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"v2 service", "0::/system.slice/imzero2-demo.service\n", "imzero2-demo.service"},
		{"v2 nested scope", "0::/user.slice/user-1000.slice/session-2.scope\n", "session-2.scope"},
		{"v2 unit above leaf", "0::/system.slice/clickhouse-server.service/supervisor\n", "clickhouse-server.service"},
		{"v2 slice only", "0::/user.slice\n", ""},
		{"v2 root", "0::/\n", ""},
		{"v2 wins over v1 lines", "12:pids:/other.slice\n0::/system.slice/a.service\n", "a.service"},
		{"v1 hybrid fallback", "12:pids:/system.slice/b.service\n11:cpuset:/\n", "b.service"},
		{"escaped instance name", "0::/system.slice/imzero2-deploy@ref\\x2da.service\n", "imzero2-deploy@ref\\x2da.service"},
		{"path with colon", "0::/system.slice/odd:name.service\n", "odd:name.service"},
		{"empty", "", ""},
		{"malformed", "garbage\n", ""},
	}
	for _, tc := range cases {
		if got := parseCgroupUnit([]byte(tc.in)); got != tc.want {
			t.Errorf("%s: parseCgroupUnit(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestScanEnvironValue(t *testing.T) {
	prefix := []byte(DefaultComponentEnvVar + "=")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"present", "HOME=/x\x00BOXER_COMPONENT=caddy\x00LANG=C\x00", "caddy"},
		{"first entry", "BOXER_COMPONENT=nats\x00HOME=/x\x00", "nats"},
		{"last entry no trailing NUL", "HOME=/x\x00BOXER_COMPONENT=clickhouse", "clickhouse"},
		{"absent", "HOME=/x\x00LANG=C\x00", ""},
		{"empty value", "BOXER_COMPONENT=\x00", ""},
		{"name-prefix collision must not match", "BOXER_COMPONENT_EXTRA=nope\x00", ""},
		{"empty environ (kernel thread shape)", "", ""},
	}
	for _, tc := range cases {
		if got := scanEnvironValue([]byte(tc.in), prefix); got != tc.want {
			t.Errorf("%s: scanEnvironValue(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}

	// The value cap bounds a pathological entry.
	long := make([]byte, 0, 4096)
	long = append(long, prefix...)
	for range 1000 {
		long = append(long, 'a')
	}
	if got := scanEnvironValue(long, prefix); len(got) != ComponentValueMaxBytes {
		t.Errorf("cap: got %d bytes, want %d", len(got), ComponentValueMaxBytes)
	}
}
