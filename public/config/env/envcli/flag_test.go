package envcli_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/config/env/envcli"
	cli "github.com/urfave/cli/v3"
)

func expandHomeForTest(t *testing.T, p string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~/"))
}

// runWithFlag parses args through a real cli.Command carrying v's derived flag,
// so the test sees exactly what urfave/cli hands the flag Action.
func runWithFlag(t *testing.T, flag cli.Flag, args ...string) {
	t.Helper()
	app := &cli.Command{
		Name:   "fixture",
		Flags:  []cli.Flag{flag},
		Writer: io.Discard,
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	if err := app.Run(context.Background(), append([]string{"fixture"}, args...)); err != nil {
		t.Fatalf("app.Run: %v", err)
	}
}

func newStringFixture(t *testing.T, name string) *env.StringVar {
	t.Helper()
	v := env.NewString(env.Spec{
		Name:        name,
		Default:     "fallback",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "fixture",
	})
	// Subtests redeclare the same spec and get the same handle back;
	// start each from a clean state.
	v.ClearCache()
	v.ClearOverride()
	return v
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
		wantSrc env.ValueSourceE
	}{
		{name: "env absent", want: "fallback", wantSrc: env.ValueSourceDefault},
		{name: "env empty", env: ptr(""), want: "fallback", wantSrc: env.ValueSourceDefault},
		{name: "env set", env: ptr("from-env"), want: "from-env", wantSrc: env.ValueSourceFlag},
		{name: "flag given", args: []string{"--fixture=from-flag"}, want: "from-flag", wantSrc: env.ValueSourceFlag},
		{name: "flag empty", env: ptr("from-env"), args: []string{"--fixture="}, want: "from-env", wantSrc: env.ValueSourceEnv},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newStringFixture(t, "BOXER_TEST_RESOLVE_STR")
			if tc.env != nil {
				t.Setenv(v.Spec().Name, *tc.env)
			}
			runWithFlag(t, envcli.Flag(v), tc.args...)
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
	flag := envcli.Flag(v, envcli.WithStringAction(func(context.Context, *cli.Command, string) error {
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
	v := env.NewPath(env.Spec{
		Name:        "BOXER_TEST_RESOLVE_PATH",
		Default:     "~/fallback",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "fixture",
	})
	t.Setenv(v.Spec().Name, "")
	runWithFlag(t, envcli.Flag(v))
	if got, want := v.Get(), expandHomeForTest(t, "~/fallback"); got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
}

func TestCategorialStringVarEmptyEnvMeansUnset(t *testing.T) {
	v := env.NewCategorialString(env.Spec{
		Name:        "BOXER_TEST_RESOLVE_CAT",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "fixture",
	}, []string{"a", "b"})
	t.Setenv(v.Spec().Name, "")
	runWithFlag(t, envcli.Flag(v))
	if got := v.Get(); got != "" {
		t.Errorf("Get = %q, want empty (unset)", got)
	}
	runWithFlag(t, envcli.Flag(v), "--fixture=b")
	if got := v.Get(); got != "b" {
		t.Errorf("Get after --fixture=b = %q, want b", got)
	}
}

// Override shadows every other tier until cleared, and never reaches the
// process environment.
func TestOverridePrecedence(t *testing.T) {
	v := newStringFixture(t, "BOXER_TEST_OVERRIDE_STR")
	t.Setenv(v.Spec().Name, "from-env")
	runWithFlag(t, envcli.Flag(v), "--fixture=from-flag")
	v.Override("seeded")
	if got := v.Get(); got != "seeded" {
		t.Errorf("Get under override = %q, want seeded", got)
	}
	if got := v.ValueSource(); got != env.ValueSourceOverride {
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
	runWithFlag(t, envcli.Flag(v), "--fixture=later")
	if got := v.Get(); got != "seeded-again" {
		t.Errorf("Get = %q; a later flag parse must not displace an override", got)
	}
}

func ptr(s string) *string { return &s }

func TestCategorialStringVarAsCliFlagUsageHasAllowedSuffix(t *testing.T) {
	v := env.NewCategorialString(env.Spec{
		Name:        "BOXER_TEST_CAT_USAGE",
		Default:     "a",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	sf, ok := envcli.Flag(v).(*cli.StringFlag)
	if !ok {
		t.Fatalf("AsCliFlag() returned %T, want *cli.StringFlag", envcli.Flag(v))
	}
	if !strings.Contains(sf.Usage, "(one of: a|b|c)") {
		t.Errorf("Usage = %q, want suffix '(one of: a|b|c)'", sf.Usage)
	}
	if sf.Value != "a" {
		t.Errorf("StringFlag.Value = %q, want %q (Spec.Default)", sf.Value, "a")
	}
}

func TestCategorialStringVarAsCliFlagRejectsExplicitInvalidValue(t *testing.T) {
	v := env.NewCategorialString(env.Spec{
		Name:        "BOXER_TEST_CAT_REJECT_FLAG",
		Default:     "a",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	sf := envcli.Flag(v).(*cli.StringFlag)
	// env unset; the only source of "invalid" is the explicit flag.
	err := sf.Action(context.Background(), nil, "invalid")
	if err == nil {
		t.Fatalf("expected error for explicit invalid --flag value")
	}
	if !strings.Contains(err.Error(), "invalid") || !strings.Contains(err.Error(), "testCat") {
		t.Errorf("error = %v, want it to mention 'invalid' and the flag name 'testCat'", err)
	}
}

func TestCategorialStringVarAsCliFlagSilentFallbackOnInvalidEnv(t *testing.T) {
	v := env.NewCategorialString(env.Spec{
		Name:        "BOXER_TEST_CAT_ENV_FALLBACK",
		Default:     "a",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	t.Setenv("BOXER_TEST_CAT_ENV_FALLBACK", "invalid")
	sf := envcli.Flag(v).(*cli.StringFlag)
	// urfave/cli passes parsed=envValue when env is set and no explicit
	// flag is supplied; replicate that shape directly.
	err := sf.Action(context.Background(), nil, "invalid")
	if err != nil {
		t.Fatalf("expected silent fallback for env-supplied invalid value, got error: %v", err)
	}
	if got := v.Get(); got != "a" {
		t.Errorf("Get after env fallback = %q, want %q (Default)", got, "a")
	}
}

func TestCategorialStringVarAsCliFlagUserActionRunsAfterValidation(t *testing.T) {
	var captured string
	v := env.NewCategorialString(env.Spec{
		Name:        "BOXER_TEST_CAT_USERACTION",
		Default:     "a",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "testCat",
	}, []string{"a", "b", "c"})
	sf := envcli.Flag(v, envcli.WithStringAction(func(ctx context.Context, cmd *cli.Command, parsed string) error {
		captured = parsed
		return nil
	})).(*cli.StringFlag)
	// Valid input: user action runs and the cache reflects the value.
	if err := sf.Action(context.Background(), nil, "b"); err != nil {
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
	if err := sf.Action(context.Background(), nil, "invalid"); err == nil {
		t.Fatalf("expected rejection of explicit invalid value")
	}
	if captured != "" {
		t.Errorf("userAction ran with invalid input (captured=%q); validation must gate it", captured)
	}
}

func TestAsCliFlagDerivation(t *testing.T) {
	v := env.NewString(env.Spec{
		Name:        "BOXER_TEST_FLAG",
		Default:     "hello",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "testFlag",
	})
	flag := envcli.Flag(v)
	names := flag.Names()
	if len(names) == 0 || names[0] != "testFlag" {
		t.Errorf("AsCliFlag().Names() = %v, want first=%q", names, "testFlag")
	}
}

func TestAsCliFlagWithOverride(t *testing.T) {
	v := env.NewString(env.Spec{
		Name:        "BOXER_TEST_FLAG_OV",
		Default:     "hello",
		Description: "test fixture",
		Category:    env.CategoryDev,
		CliFlagName: "testFlag",
	})
	flag := envcli.Flag(v, envcli.WithFlagName("prefixed.testFlag"))
	names := flag.Names()
	if len(names) == 0 || names[0] != "prefixed.testFlag" {
		t.Errorf("AsCliFlag(WithCliFlagName) Names() = %v, want first=%q", names, "prefixed.testFlag")
	}
}
