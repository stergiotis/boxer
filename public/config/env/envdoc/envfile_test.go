package envdoc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/config/env"
)

func envFileSpecs() (specs []env.Spec) {
	specs = []env.Spec{
		{
			Name:        "T_PLAIN",
			Default:     "a=b,c",
			Description: "a plain variable whose description is long enough that the comment above its assignment has to wrap onto a second line",
			Category:    "zeta",
			Type:        env.TypeString,
			CliFlagName: "plain",
			Origin:      env.Origin{Package: "example.com/a/package/path/long/enough/to/pass/the/comment/width/on/its/own"},
		},
		{
			Name:        "T_QUOTED",
			Default:     "it's a value; with $HOME",
			Description: "needs quoting",
			Category:    "alpha",
			Type:        env.TypeString,
		},
		{
			Name:        "T_TIER",
			Default:     "GREEN",
			Description: "a categorial",
			Category:    "alpha",
			Type:        env.TypeCategorialString,
			Allowed:     []string{"GREEN", "RED"},
		},
		{
			Name:        "T_SECRET",
			Description: "a key",
			Category:    "alpha",
			Type:        env.TypeString,
			Sensitive:   true,
		},
	}
	return
}

func TestRenderEnvFileTemplate(t *testing.T) {
	out := RenderEnvFile(envFileSpecs(), EnvFileOptions{GeneratorPath: "gen"})
	for _, want := range []string{
		"#T_PLAIN=a=b,c\n",
		"#T_QUOTED='it'\\''s a value; with $HOME'\n",
		"#T_TIER=GREEN\n",
		"# one of: GREEN | RED\n",
		"#T_SECRET=\n",
		"# sensitive: holds a secret; keep a file that sets it private\n",
		"# type: string; flag: --plain\n",
		"# declared in: example.com/a/package/path/long/enough/to/pass/the/comment/width/on/its/own\n",
		"# 4 variables across 2 categories.\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "# alpha (3)") > strings.Index(out, "# zeta (1)") {
		t.Errorf("categories are not sorted:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "# declared in: ") && len([]rune(line)) > envFileWidth {
			t.Errorf("comment line wider than %d: %q", envFileWidth, line)
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			t.Errorf("template has an active line: %q", line)
		}
	}
	if again := RenderEnvFile(envFileSpecs(), EnvFileOptions{GeneratorPath: "gen"}); again != out {
		t.Errorf("two renders differ")
	}
}

func TestRenderEnvFileCurrent(t *testing.T) {
	values := map[string]string{
		"T_PLAIN":  "x y",
		"T_TIER":   "BLUE",
		"T_SECRET": "hunter2",
		"T_QUOTED": "",
	}
	lookup := func(name string) (v string, ok bool) {
		v, ok = values[name]
		return
	}
	out := RenderEnvFile(envFileSpecs(), EnvFileOptions{Export: true, Lookup: lookup})
	for _, want := range []string{
		"export T_PLAIN='x y'\n",
		"# type: string; flag: --plain; default: a=b,c\n",
		"export T_TIER=BLUE\n",
		"# the value below is not one of the allowed values; it reads as the default\n",
		"#export T_SECRET=\n",
		"# set in the environment; the value is not written\n",
		"#export T_QUOTED='it'\\''s a value; with $HOME'\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hunter2") {
		t.Errorf("a sensitive value was written:\n%s", out)
	}
}

// TestRenderEnvFileSources checks the quoting against a real POSIX shell:
// every active assignment reads back as the value it was rendered from.
func TestRenderEnvFileSources(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	values := map[string]string{
		"T_PLAIN":  `a 'b' "c" $d \e`,
		"T_QUOTED": "line1\nline2",
		"T_TIER":   "RED",
	}
	lookup := func(name string) (v string, ok bool) {
		v, ok = values[name]
		return
	}
	path := filepath.Join(t.TempDir(), "test.env")
	err = os.WriteFile(path, []byte(RenderEnvFile(envFileSpecs(), EnvFileOptions{Lookup: lookup})), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range values {
		cmd := exec.Command(sh, "-c", `set -a; . "$1"; set +a; printf %s "$(printenv "$2")"`, "sh", path, name)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		got, runErr := cmd.Output()
		if runErr != nil {
			t.Fatalf("source %s: %v", path, runErr)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
