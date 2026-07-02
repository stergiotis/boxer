package packageprops

import "testing"

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{
		KindUnspecified:     "unspecified",
		KindDemo:            "demo",
		KindExample:         "example",
		KindIntegrationTest: "integration-test",
		Kind(200):           "unspecified", // out-of-range falls back like the zero value
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
