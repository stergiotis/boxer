package envdoc

import (
	"strings"
	"testing"
)

func TestRenderNamesEnvFileCommand(t *testing.T) {
	const sentence = "`env gen-envfile` renders the same variables as a commented environment file to fill in."
	out := renderSpecs(envFileSpecs(), Options{EnvFileCommand: "env gen-envfile"})
	if !strings.Contains(out, sentence) {
		t.Errorf("page lacks %q:\n%s", sentence, out)
	}
	out = renderSpecs(envFileSpecs(), Options{})
	if strings.Contains(out, "gen-envfile") {
		t.Errorf("page names gen-envfile with EnvFileCommand empty:\n%s", out)
	}
}
