//go:build llm_generated_opus47

package env

import (
	"strings"
	"testing"
	"time"

	cli "github.com/urfave/cli/v2"
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
	v.SetForTest(t, "debug")
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
	v.SetForTest(t, "purple")
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

func TestCategorialStringVarRejectsEmptyDefault(t *testing.T) {
	resetRegistryForTest()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic when Default is empty for categorial var")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "Default") {
			t.Errorf("panic message = %q, want it to mention 'Default'", msg)
		}
	}()
	_ = NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_EMPTY_DEFAULT",
		Description: "test fixture",
		Category:    CategoryDev,
	}, []string{"a", "b"})
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

func TestCategorialStringVarAsCliFlagUsageHasAllowedSuffix(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_USAGE",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	sf, ok := v.AsCliFlag().(*cli.StringFlag)
	if !ok {
		t.Fatalf("AsCliFlag() returned %T, want *cli.StringFlag", v.AsCliFlag())
	}
	if !strings.Contains(sf.Usage, "(one of: a|b|c)") {
		t.Errorf("Usage = %q, want suffix '(one of: a|b|c)'", sf.Usage)
	}
	if sf.Value != "a" {
		t.Errorf("StringFlag.Value = %q, want %q (Spec.Default)", sf.Value, "a")
	}
}

func TestCategorialStringVarAsCliFlagRejectsExplicitInvalidValue(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_REJECT_FLAG",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	sf := v.AsCliFlag().(*cli.StringFlag)
	// env unset; the only source of "invalid" is the explicit flag.
	err := sf.Action(nil, "invalid")
	if err == nil {
		t.Fatalf("expected error for explicit invalid --flag value")
	}
	if !strings.Contains(err.Error(), "invalid") || !strings.Contains(err.Error(), "testCat") {
		t.Errorf("error = %v, want it to mention 'invalid' and the flag name 'testCat'", err)
	}
}

func TestCategorialStringVarAsCliFlagSilentFallbackOnInvalidEnv(t *testing.T) {
	resetRegistryForTest()
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_ENV_FALLBACK",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	t.Setenv("BOXER_TEST_CAT_ENV_FALLBACK", "invalid")
	sf := v.AsCliFlag().(*cli.StringFlag)
	// urfave/cli passes parsed=envValue when env is set and no explicit
	// flag is supplied; replicate that shape directly.
	err := sf.Action(nil, "invalid")
	if err != nil {
		t.Fatalf("expected silent fallback for env-supplied invalid value, got error: %v", err)
	}
	if got := v.Get(); got != "a" {
		t.Errorf("Get after env fallback = %q, want %q (Default)", got, "a")
	}
}

func TestCategorialStringVarAsCliFlagUserActionRunsAfterValidation(t *testing.T) {
	resetRegistryForTest()
	var captured string
	v := NewCategorialString(Spec{
		Name:        "BOXER_TEST_CAT_USERACTION",
		Default:     "a",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	sf := v.AsCliFlag(WithStringAction(func(ctx *cli.Context, parsed string) error {
		captured = parsed
		return nil
	})).(*cli.StringFlag)
	// Valid input: user action runs and the cache reflects the value.
	if err := sf.Action(nil, "b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "b" {
		t.Errorf("userAction captured %q, want %q", captured, "b")
	}
	if got := v.Get(); got != "b" {
		t.Errorf("Get after Action = %q, want %q", got, "b")
	}
	// Invalid explicit input: user action must NOT run.
	captured = ""
	if err := sf.Action(nil, "invalid"); err == nil {
		t.Fatalf("expected rejection of explicit invalid value")
	}
	if captured != "" {
		t.Errorf("userAction ran with invalid input (captured=%q); validation must gate it", captured)
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
