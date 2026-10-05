package canonicaltypes

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

// Regression: MustParse* replaced the parse error with a fresh one that did
// not wrap it, so the panic log lost the syntax error and its position.
func TestMustParseKeepsSyntaxError(t *testing.T) {
	p := NewParser()
	for name, call := range map[string]func(){
		"primitive":     func() { p.MustParsePrimitiveTypeAst("u0") },
		"type-or-group": func() { p.MustParseTypeOrGroupAst("u0") },
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			saved := log.Logger
			log.Logger = zerolog.New(&buf)
			defer func() { log.Logger = saved }()
			require.Panics(t, call)
			require.Contains(t, buf.String(), "unable to parse canonical type")
			require.Contains(t, buf.String(), "syntax error")
		})
	}
}
