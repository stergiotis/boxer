package observability

import (
	"bytes"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	logging2 "github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stretchr/testify/require"
	"testing"
)

func a() error {
	return eh.Errorf("a: %d", 0)
}
func b() error {
	return eh.Errorf("b: %w", a())
}
func c() error {
	return eb.Build().Str("k1", "v1").Errorf("c: %w", b())
}
func d() error {
	return eb.Build().Str("k2", "v2").Errorf("d: %w", c())
}
func e() error {
	return fmt.Errorf("e: %w", d())
}
func f() error {
	return fmt.Errorf("f: %w", e())
}

func TestErrorf(t *testing.T) {
	buf := &bytes.Buffer{}
	log.Logger = log.Output(logging2.NewJsonIndentLogger(buf))
	logging2.SetupZeroLog()
	err := f()
	require.Error(t, err)
	log.Error().Stack().Err(err).Msg("an error")
	fmt.Printf("%s", buf.String())
	// FIXME
	/*want := ``
	wantL := strings.Split(want, "\n")
	actualL := strings.Split(buf.String(), "\n")
	for i, a := range actualL {
		if !strings.Contains(a, "\"time\"") {
			require.EqualValues(t, wantL[i], actualL[i])
		}
	}*/
}
