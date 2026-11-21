package env

import "testing"

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

func TestValueSourceString(t *testing.T) {
	for src, want := range map[ValueSourceE]string{
		ValueSourceDefault: "default", ValueSourceEnv: "env", ValueSourceFlag: "flag", ValueSourceOverride: "override", ValueSourceE(9): "unknown",
	} {
		if got := src.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", src, got, want)
		}
	}
}
