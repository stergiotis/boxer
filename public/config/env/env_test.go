package env

import (
	"errors"
	"strings"
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
	setForTest(t, v, "overridden")
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

func TestCategorialStringVarDefault(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_DEFAULT",
		Default:     "info",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"trace", "debug", "info", "warn", "error"})
	got := v.Get()
	if got != "info" {
		t.Errorf("Get default = %q, want %q", got, "info")
	}
	spec := v.Spec()
	if spec.Type != TypeCategorialString {
		t.Errorf("Spec.Type = %q, want %q", spec.Type, TypeCategorialString)
	}
	if len(spec.Allowed) != 5 {
		t.Errorf("Spec.Allowed len = %d, want 5", len(spec.Allowed))
	}
}

func TestCategorialStringVarAcceptsAllowedEnvValue(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_OK",
		Default:     "info",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"debug", "info", "warn"})
	setForTest(t, v, "debug")
	got := v.Get()
	if got != "debug" {
		t.Errorf("Get with allowed env value = %q, want %q", got, "debug")
	}
}

func TestCategorialStringVarFallsBackOnInvalidEnvValue(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_BAD",
		Default:     "info",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"debug", "info", "warn"})
	setForTest(t, v, "purple")
	got := v.Get()
	if got != "info" {
		t.Errorf("Get with out-of-set env value = %q, want fallback %q", got, "info")
	}
}

func TestCategorialStringVarRejectsBadDefault(t *testing.T) {
	resetRegistryForTest()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic when Default is not in allowed set")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "default") || !strings.Contains(msg, "allowed values") {
			t.Errorf("panic message = %q, want it to mention 'default' and 'allowed values'", msg)
		}
	}()
	_ = NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_BAD_DEFAULT",
		Default:     "purple",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"red", "green", "blue"})
}

func TestCategorialStringVarRejectsEmptyAllowed(t *testing.T) {
	resetRegistryForTest()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic when allowed values is empty")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "allowed values") {
			t.Errorf("panic message = %q, want it to mention 'allowed values'", msg)
		}
	}()
	_ = NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_EMPTY",
		Description: "test fixture",
		Category:    CategoryDev,
	}, nil)
}

func TestCategorialStringVarEmptyDefaultMeansUnset(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_EMPTY_DEFAULT",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"a", "b"})
	if got := v.Get(); got != "" {
		t.Errorf("Get with empty default and no env = %q, want empty (unset)", got)
	}
	if v.IsAllowed("") {
		t.Errorf("empty must not count as an allowed value")
	}
	setForTest(t, v, "b")
	if got := v.Get(); got != "b" {
		t.Errorf("Get after SetForTest = %q, want b", got)
	}
}

func TestCategorialStringVarRejectsEmptyAllowedMember(t *testing.T) {
	resetRegistryForTest()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic when the allowed set contains the empty string")
		}
	}()
	NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_EMPTY_MEMBER",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"a", ""})
}

func TestCategorialStringVarAllowedIsDefensiveCopy(t *testing.T) {
	resetRegistryForTest()
	allowed := []string{"a", "b"}
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_COPY",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
	}, allowed)
	allowed[0] = "mutated"
	if got := v.Allowed(); got[0] != "a" {
		t.Errorf("Allowed()[0] = %q, want %q (registration must defensive-copy)", got[0], "a")
	}
	got := v.Allowed()
	got[0] = "tampered"
	if again := v.Allowed(); again[0] != "a" {
		t.Errorf("Allowed()[0] = %q after caller mutation, want %q", again[0], "a")
	}
}

func TestCategorialStringVarSpecIsDefensiveCopy(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_SPEC_COPY",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"a", "b", "c"})
	s := v.Spec()
	s.Allowed[0] = "MUTATED"
	if !v.IsAllowed("a") {
		t.Errorf("IsAllowed(\"a\") false after mutating Spec().Allowed[0]; Spec() must defensive-copy the slice")
	}
	if v.IsAllowed("MUTATED") {
		t.Errorf("IsAllowed(\"MUTATED\") true; Spec() leaked the backing array")
	}
}

func TestCategorialStringVarIsAllowed(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_ISALLOWED",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"a", "b"})
	if !v.IsAllowed("a") {
		t.Errorf("IsAllowed(\"a\") = false, want true")
	}
	if v.IsAllowed("z") {
		t.Errorf("IsAllowed(\"z\") = true, want false")
	}
	if v.IsAllowed("") {
		t.Errorf("IsAllowed(\"\") = true, want false")
	}
}

func TestCategorialStringVarLookupReturnsRaw(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_LOOKUP",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"a", "b"})
	raw, set := v.Lookup()
	if set {
		t.Errorf("Lookup() set=%v, want false (env unset)", set)
	}
	if raw != "" {
		t.Errorf("Lookup() raw=%q, want empty", raw)
	}
	t.Setenv("BOXER_TEST_CAT_LOOKUP", "out-of-set-value")
	raw, set = v.Lookup()
	if !set {
		t.Errorf("Lookup() set=false, want true after env Set")
	}
	if raw != "out-of-set-value" {
		t.Errorf("Lookup() raw=%q, want %q (Lookup returns raw without membership check)", raw, "out-of-set-value")
	}
}

func TestFloatVarDefault(t *testing.T) {
	resetRegistryForTest()
	v := NewFloat(Spec{
		Name:        "BOXER_TEST_FLOAT",
		Default:     "1.5",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	got := v.Get()
	if got != 1.5 {
		t.Errorf("Get default = %v, want 1.5", got)
	}
}

func TestFloatVarSetForTest(t *testing.T) {
	resetRegistryForTest()
	v := NewFloat(Spec{
		Name:        "BOXER_TEST_FLOAT_SFT",
		Default:     "1.0",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	setForTest(t, v, "3.14")
	got := v.Get()
	if got != 3.14 {
		t.Errorf("Get after SetForTest = %v, want 3.14", got)
	}
}

func TestFloatVarBadEnvFallsBack(t *testing.T) {
	resetRegistryForTest()
	v := NewFloat(Spec{
		Name:        "BOXER_TEST_FLOAT_BAD",
		Default:     "2.0",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	setForTest(t, v, "not-a-number")
	got := v.Get()
	if got != 2.0 {
		t.Errorf("Get with bad env = %v, want fallback 2.0", got)
	}
}

func TestFloatVarBadDefaultPanics(t *testing.T) {
	resetRegistryForTest()
	v := NewFloat(Spec{
		Name:        "BOXER_TEST_FLOAT_BAD_DEFAULT",
		Default:     "not-a-float",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic for unparseable float default")
		}
	}()
	_ = v.Get() // triggers parseDefault
}

func TestFloatVarZeroDefault(t *testing.T) {
	resetRegistryForTest()
	v := NewFloat(Spec{
		Name:        "BOXER_TEST_FLOAT_ZERO",
		Description: "test fixture",
		Category:    CategoryDev,
	})
	got := v.Get()
	if got != 0 {
		t.Errorf("Get with empty Default = %v, want 0", got)
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

func TestIdenticalDuplicateReturnsTheFirstHandle(t *testing.T) {
	resetRegistryForTest()
	spec := Spec{Name: "BOXER_TEST_DUP", Description: "test fixture", Category: CategoryDev}
	first := NewString(spec)
	second := NewString(spec)
	if first != second {
		t.Fatalf("an identical redeclaration must return the first handle")
	}
	if err := RegistrationErrors(); err != nil {
		t.Fatalf("an identical redeclaration is not a conflict: %v", err)
	}
}

func TestConflictingDuplicateIsReportedNotPanicked(t *testing.T) {
	resetRegistryForTest()
	first := NewString(Spec{Name: "BOXER_TEST_DUP", Description: "test fixture", Category: CategoryDev})
	second := NewString(Spec{Name: "BOXER_TEST_DUP", Description: "another meaning", Category: CategoryDev})
	if first == second {
		t.Fatalf("a conflicting redeclaration must not share the first handle")
	}
	if held, _ := LookupVar("BOXER_TEST_DUP"); held != VarI(first) {
		t.Fatalf("the registry must keep the first declaration")
	}
	err := RegistrationErrors()
	if !errors.Is(err, ErrConflictingDeclaration) {
		t.Fatalf("RegistrationErrors = %v, want ErrConflictingDeclaration", err)
	}
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

// setForTest is envtest.Set for this package's own tests, which cannot
// import envtest without a cycle.
func setForTest(t testing.TB, v interface {
	VarI
	ClearCache()
	ClearOverride()
}, value string) {
	t.Helper()
	v.ClearCache()
	v.ClearOverride()
	t.Setenv(v.Spec().Name, value)
	t.Cleanup(func() {
		v.ClearCache()
		v.ClearOverride()
	})
}
