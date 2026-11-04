package chat

import (
	"os"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
)

// TestMain runs the package's tests against the defaults of the variables
// that seed a chat window or its profile. The tests assert those defaults —
// the rounds range, Apps off, the Artefact off — and a developer's shell
// that seeds chat windows (BOXER_CHAT_*, BOXER_AGENT_COORDINATORS) would
// otherwise move them. A test that wants a value sets it with SetForTest,
// which still applies: nothing here overrides, it only unsets.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "BOXER_CHAT_") {
			_ = os.Unsetenv(name)
		}
	}
	_ = os.Unsetenv(agent.CoordinatorsEnv.Spec().Name)
	os.Exit(m.Run())
}
