package packageprops

import "testing"

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{
		KindUnspecified:     "unspecified",
		KindDemo:            "demo",
		KindExample:         "example",
		KindIntegrationTest: "integration-test",
		KindTestSupport:     "test-support",
		KindTool:            "tool",
		Kind(200):           "unspecified", // out-of-range falls back like the zero value
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestStabilityString(t *testing.T) {
	for st, want := range map[StabilityE]string{
		StabilityUnspecified:  "unspecified",
		StabilityStable:       "stable",
		StabilityExperimental: "experimental",
		StabilityInternal:     "internal",
		StabilityE(200):       "unspecified", // out-of-range falls back like the zero value
	} {
		if got := st.String(); got != want {
			t.Errorf("StabilityE(%d).String() = %q, want %q", st, got, want)
		}
	}
	if len(AllStabilities) != 4 || AllStabilities[0] != StabilityUnspecified {
		t.Errorf("AllStabilities = %v, want the four tiers in declaration order", AllStabilities)
	}
}
