package env

import (
	"io"
	"testing"

	cli "github.com/urfave/cli/v2"
)

// runWithFlag parses args through a real cli.App carrying v's derived flag,
// so the test sees exactly what urfave/cli hands the flag Action.
func runWithFlag(t *testing.T, flag cli.Flag, args ...string) {
	t.Helper()
	app := &cli.App{
		Name:   "fixture",
		Flags:  []cli.Flag{flag},
		Writer: io.Discard,
		Action: func(*cli.Context) error { return nil },
	}
	if err := app.Run(append([]string{"fixture"}, args...)); err != nil {
		t.Fatalf("app.Run: %v", err)
	}
}

func newStringFixture(t *testing.T, name string) *StringVar {
	t.Helper()
	resetRegistryForTest()
	return NewString(Spec{
		Name:        name,
		Default:     "fallback",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "fixture",
	})
}

// The five sources a string var can resolve from, and the tier each lands
// on. The two empty cases are the ones that used to disagree: urfave/cli
// treats a set-but-empty variable (and a bare --flag=) as a set flag, and
// the Action cached "" while Get read empty as unset.
func TestStringVarEmptyMeansUnset(t *testing.T) {
	cases := []struct {
		name    string
		env     *string
		args    []string
		want    string
		wantSrc ValueSourceE
	}{
		{name: "env absent", want: "fallback", wantSrc: ValueSourceDefault},
		{name: "env empty", env: ptr(""), want: "fallback", wantSrc: ValueSourceDefault},
		{name: "env set", env: ptr("from-env"), want: "from-env", wantSrc: ValueSourceFlag},
		{name: "flag given", args: []string{"--fixture=from-flag"}, want: "from-flag", wantSrc: ValueSourceFlag},
		{name: "flag empty", env: ptr("from-env"), args: []string{"--fixture="}, want: "from-env", wantSrc: ValueSourceEnv},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newStringFixture(t, "BOXER_TEST_RESOLVE_STR")
			if tc.env != nil {
				t.Setenv(v.Spec().Name, *tc.env)
			}
			runWithFlag(t, v.AsCliFlag(), tc.args...)
			if got := v.Get(); got != tc.want {
				t.Errorf("Get = %q, want %q", got, tc.want)
			}
			if got := v.ValueSource(); got != tc.wantSrc {
				t.Errorf("ValueSource = %s, want %s", got, tc.wantSrc)
			}
		})
	}
}

func TestStringVarEmptyFlagDoesNotRunUserAction(t *testing.T) {
	v := newStringFixture(t, "BOXER_TEST_RESOLVE_STR_ACTION")
	ran := false
	flag := v.AsCliFlag(WithStringAction(func(*cli.Context, string) error {
		ran = true
		return nil
	}))
	t.Setenv(v.Spec().Name, "")
	runWithFlag(t, flag)
	if ran {
		t.Errorf("user action ran for an empty value; empty means unset")
	}
}

func TestPathVarEmptyEnvMeansUnset(t *testing.T) {
	resetRegistryForTest()
	v := NewPath(Spec{
		Name:        "BOXER_TEST_RESOLVE_PATH",
		Default:     "~/fallback",
		Description: "test fixture",
		Category:    CategoryDev,
		CliFlagName: "fixture",
	})
	t.Setenv(v.Spec().Name, "")
	runWithFlag(t, v.AsCliFlag())
	if got, want := v.Get(), expandHome("~/fallback"); got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

// Override shadows every other tier until cleared, and never reaches the
// process environment.
func TestOverridePrecedence(t *testing.T) {
	v := newStringFixture(t, "BOXER_TEST_OVERRIDE_STR")
	t.Setenv(v.Spec().Name, "from-env")
	runWithFlag(t, v.AsCliFlag(), "--fixture=from-flag")
	v.Override("seeded")
	if got := v.Get(); got != "seeded" {
		t.Errorf("Get under override = %q, want seeded", got)
	}
	if got := v.ValueSource(); got != ValueSourceOverride {
		t.Errorf("ValueSource = %s, want override", got)
	}
	if raw, _ := v.Lookup(); raw != "from-env" {
		t.Errorf("Lookup = %q; an override must not touch the environment", raw)
	}
	v.ClearOverride()
	if got := v.Get(); got != "from-flag" {
		t.Errorf("Get after ClearOverride = %q, want the flag value", got)
	}
	// A flag parsed after the override does not displace it.
	v.Override("seeded-again")
	runWithFlag(t, v.AsCliFlag(), "--fixture=later")
	if got := v.Get(); got != "seeded-again" {
		t.Errorf("Get = %q; a later flag parse must not displace an override", got)
	}
}

func TestOverrideTypedVars(t *testing.T) {
	resetRegistryForTest()
	b := NewBool(Spec{Name: "BOXER_TEST_OVERRIDE_BOOL", Default: "false", Description: "f", Category: CategoryDev})
	i := NewInt(Spec{Name: "BOXER_TEST_OVERRIDE_INT", Default: "1", Description: "f", Category: CategoryDev})
	f := NewFloat(Spec{Name: "BOXER_TEST_OVERRIDE_FLOAT", Default: "1.5", Description: "f", Category: CategoryDev})
	d := NewDuration(Spec{Name: "BOXER_TEST_OVERRIDE_DUR", Default: "1s", Description: "f", Category: CategoryDev})
	p := NewPath(Spec{Name: "BOXER_TEST_OVERRIDE_PATH", Description: "f", Category: CategoryDev})
	c := NewCategorialString(Spec{Name: "BOXER_TEST_OVERRIDE_CAT", Default: "a", Description: "f", Category: CategoryDev}, []string{"a", "b"})
	b.Override(true)
	i.Override(42)
	f.Override(2.5)
	d.Override(3e9)
	p.Override("~/x")
	c.Override("b")
	if !b.Get() || i.Get() != 42 || f.Get() != 2.5 || d.Get() != 3e9 || c.Get() != "b" {
		t.Errorf("typed overrides not honoured: %v %d %v %v %q", b.Get(), i.Get(), f.Get(), d.Get(), c.Get())
	}
	if got, want := p.Get(), expandHome("~/x"); got != want {
		t.Errorf("path override = %q, want home-expanded %q", got, want)
	}
	for _, src := range []ValueSourceE{b.ValueSource(), i.ValueSource(), f.ValueSource(), d.ValueSource(), p.ValueSource(), c.ValueSource()} {
		if src != ValueSourceOverride {
			t.Errorf("ValueSource = %s, want override", src)
		}
	}
	b.ClearOverride()
	if b.Get() {
		t.Errorf("bool still overridden after ClearOverride")
	}
}

func TestSetForTestClearsOverride(t *testing.T) {
	v := newStringFixture(t, "BOXER_TEST_OVERRIDE_SFT")
	v.Override("seeded")
	v.SetForTest(t, "from-test")
	if got := v.Get(); got != "from-test" {
		t.Errorf("Get after SetForTest = %q, want from-test (SetForTest resets the override)", got)
	}
}

func TestValueSourceString(t *testing.T) {
	for src, want := range map[ValueSourceE]string{
		ValueSourceDefault: "default", ValueSourceEnv: "env", ValueSourceFlag: "flag", ValueSourceOverride: "override", ValueSourceE(9): "unknown",
	} {
		if got := src.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", src, got, want)
		}
	}
}

func ptr(s string) *string { return &s }
