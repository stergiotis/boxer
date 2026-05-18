//go:build llm_generated_opus47

package env

import (
	"testing"
	"time"
)

func TestPackageFromFuncName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"github.com/stergiotis/boxer/public/observability/logging.init", "github.com/stergiotis/boxer/public/observability/logging"},
		{"github.com/stergiotis/boxer/public/observability/logging.glob..func1", "github.com/stergiotis/boxer/public/observability/logging"},
		{"main.init", "main"},
		{"github.com/x/y.SomeFunc", "github.com/x/y"},
	}
	for _, c := range cases {
		got := packageFromFuncName(c.in)
		if got != c.want {
			t.Errorf("packageFromFuncName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestModuleFromPackage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"github.com/stergiotis/boxer/public/observability/logging", "github.com/stergiotis/boxer"},
		{"github.com/x/y/z", "github.com/x/y"},
		{"main", "main"},
	}
	for _, c := range cases {
		got := moduleFromPackage(c.in)
		if got != c.want {
			t.Errorf("moduleFromPackage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStringVarDefault(t *testing.T) {
	resetRegistryForTest()
	v := NewString(Spec{
		Name:        "BOXER_TEST_STR_DEFAULT",
		Default:     "fallback",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	got := v.Get()
	if got != "fallback" {
		t.Errorf("Get default = %q, want %q", got, "fallback")
	}
}

func TestStringVarSetForTest(t *testing.T) {
	resetRegistryForTest()
	v := NewString(Spec{
		Name:        "BOXER_TEST_STR_SFT",
		Default:     "fallback",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	v.SetForTest(t, "overridden")
	got := v.Get()
	if got != "overridden" {
		t.Errorf("Get after SetForTest = %q, want %q", got, "overridden")
	}
}

func TestBoolVarDefault(t *testing.T) {
	resetRegistryForTest()
	v := NewBool(Spec{
		Name:        "BOXER_TEST_BOOL",
		Default:     "true",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	if !v.Get() {
		t.Errorf("Get default = false, want true")
	}
}

func TestIntVarDefault(t *testing.T) {
	resetRegistryForTest()
	v := NewInt(Spec{
		Name:        "BOXER_TEST_INT",
		Default:     "42",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	got := v.Get()
	if got != 42 {
		t.Errorf("Get default = %d, want 42", got)
	}
}

func TestDurationVarDefault(t *testing.T) {
	resetRegistryForTest()
	v := NewDuration(Spec{
		Name:        "BOXER_TEST_DUR",
		Default:     "1500ms",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	got := v.Get()
	want := 1500 * time.Millisecond
	if got != want {
		t.Errorf("Get default = %v, want %v", got, want)
	}
}

func TestPathVarHomeExpansion(t *testing.T) {
	resetRegistryForTest()
	v := NewPath(Spec{
		Name:        "BOXER_TEST_PATH",
		Default:     "~/somewhere",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	got := v.Get()
	if got == "~/somewhere" {
		t.Errorf("Get returned unexpanded %q; expected home prefix", got)
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	resetRegistryForTest()
	_ = NewString(Spec{
		Name:        "BOXER_TEST_DUP",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on duplicate registration")
		}
	}()
	_ = NewString(Spec{
		Name:        "BOXER_TEST_DUP",
		Description: "test fixture",
		Category:    CategoryDev,
	})
}

func TestRegistryFilters(t *testing.T) {
	resetRegistryForTest()
	_ = NewString(Spec{
		Name:        "BOXER_FILTER_A",
		Description: "a",
		Category:    CategoryDev,
	})
	_ = NewString(Spec{
		Name:        "BOXER_FILTER_B",
		Description: "b",
		Category:    CategoryLLM,
	})
	got := len(All())
	if got != 2 {
		t.Errorf("All() len = %d, want 2", got)
	}
	got = len(ByCategory(CategoryDev))
	if got != 1 {
		t.Errorf("ByCategory(Dev) len = %d, want 1", got)
	}
	got = len(ByPrefix("BOXER_FILTER_"))
	if got != 2 {
		t.Errorf("ByPrefix len = %d, want 2", got)
	}
	snap := Snapshot()
	if len(snap) != 2 {
		t.Errorf("Snapshot len = %d, want 2", len(snap))
	}
	if snap[0].Name >= snap[1].Name {
		t.Errorf("Snapshot not sorted: %q before %q", snap[0].Name, snap[1].Name)
	}
}

func TestAsCliFlagDerivation(t *testing.T) {
	resetRegistryForTest()
	v := NewString(Spec{
		Name:        "BOXER_TEST_FLAG",
		Default:     "hello",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "testFlag",
	})
	flag := v.AsCliFlag()
	names := flag.Names()
	if len(names) == 0 || names[0] != "testFlag" {
		t.Errorf("AsCliFlag().Names() = %v, want first=%q", names, "testFlag")
	}
}

func TestAsCliFlagWithOverride(t *testing.T) {
	resetRegistryForTest()
	v := NewString(Spec{
		Name:        "BOXER_TEST_FLAG_OV",
		Default:     "hello",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "testFlag",
	})
	flag := v.AsCliFlag(WithCliFlagName("prefixed.testFlag"))
	names := flag.Names()
	if len(names) == 0 || names[0] != "prefixed.testFlag" {
		t.Errorf("AsCliFlag(WithCliFlagName) Names() = %v, want first=%q", names, "prefixed.testFlag")
	}
}

func TestFormatValueRedaction(t *testing.T) {
	got := FormatValue(Spec{Sensitive: true}, "secret")
	if got != "<redacted>" {
		t.Errorf("FormatValue(sensitive) = %q, want %q", got, "<redacted>")
	}
	got = FormatValue(Spec{}, "ordinary")
	if got != "ordinary" {
		t.Errorf("FormatValue(non-sensitive) = %q, want %q", got, "ordinary")
	}
}
