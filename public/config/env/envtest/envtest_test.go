package envtest_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/config/env/envtest"
)

var fixture = env.NewString(env.Spec{
	Name:        "BOXER_TEST_ENVTEST_SET",
	Default:     "fallback",
	Description: "test fixture",
	Category:    env.CategoryDev,
})

func TestSetClearsTheOverride(t *testing.T) {
	fixture.Override("seeded")
	envtest.Set(t, fixture, "from-test")
	if got := fixture.Get(); got != "from-test" {
		t.Errorf("Get after Set = %q, want from-test (Set resets the override)", got)
	}
}

func TestSetIsUndoneAfterTheTest(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		envtest.Set(t, fixture, "inner")
		if got := fixture.Get(); got != "inner" {
			t.Errorf("Get = %q, want inner", got)
		}
	})
	if got := fixture.Get(); got != "fallback" {
		t.Errorf("Get after the subtest = %q, want the default", got)
	}
}
