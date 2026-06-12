//go:build llm_generated_opus46

package eh

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Integration strategy 1: ErrorMarshalFunc (simplest)
//
// This replaces the global error marshaler so that .Err(err) produces
// the human-readable text in JSON logs. Best for development/debugging.
//
// Usage:
//
//	func init() {
//	    zerolog.ErrorMarshalFunc = eh.ErrorMarshalFuncHuman
//	}
//
//	// Then use normally:
//	log.Error().Err(err).Msg("request failed")
//	// Output: {"level":"error","error":"query failed\n  └── cause: connection refused\n      at ...","message":"request failed"}
// ---------------------------------------------------------------------------

// ErrorMarshalFuncHuman is a drop-in replacement for zerolog.ErrorMarshalFunc
// that produces the human-readable plain-text format (no ANSI) in the "error" field.
func ErrorMarshalFuncHuman(err error) interface{} {
	return FormatErrorPlainS(err)
}

// ErrorMarshalFuncStructured preserves the original structured marshaling from
// MarshalError for JSON logs, but you can use both: structured for machine logs,
// human for console.
func ErrorMarshalFuncStructured(err error) interface{} {
	return MarshalError(err)
}

// ---------------------------------------------------------------------------
// Integration strategy 2: ConsoleWriter with FormatErrFieldValue (recommended)
//
// This keeps JSON logs untouched but renders errors beautifully in the
// console. The error field is replaced with the multi-line human-readable
// output, while other fields render normally.
//
// Usage:
//
//	writer := zerolog.ConsoleWriter{
//	    Out:                 os.Stderr,
//	    FormatErrFieldValue: eh.ConsoleFormatError(false),
//	}
//	logger := zerolog.New(writer).With().Timestamp().Logger()
//	logger.Error().Err(err).Msg("request failed")
//
// With color:
//
//	writer := zerolog.ConsoleWriter{
//	    Out:                 os.Stderr,
//	    FormatErrFieldValue: eh.ConsoleFormatError(true),
//	}
// ---------------------------------------------------------------------------

// ConsoleFormatError returns a zerolog Formatter that renders errors using
// the human-readable formatter. Set useColor=true for ANSI-colored output.
//
// When the error is a simple string (no stack trace), it passes through
// unchanged. When it's a complex error object, it renders the full
// formatted output with indentation.
func ConsoleFormatError(useColor bool) zerolog.Formatter {
	return func(i interface{}) string {
		switch v := i.(type) {
		case string:
			// Simple string error — try to parse it as something we produced,
			// otherwise pass through
			if strings.Contains(v, "\n") || strings.Contains(v, "cause:") {
				// Already formatted by ErrorMarshalFuncHuman
				return "\n" + indentBlock(v, "  ")
			}
			return v
		case error:
			return formatForConsole(v, useColor)
		case map[string]interface{}:
			// This is what zerolog produces when ErrorMarshalFunc returns
			// a LogObjectMarshaler (like our MarshalError). The ConsoleWriter
			// receives the JSON-decoded map. We could try to reconstruct,
			// but it's better to intercept earlier.
			return fmt.Sprintf("%v", v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
}

func formatForConsole(err error, useColor bool) string {
	if useColor {
		return "\n" + indentBlock(FormatErrorWithStackS(err), "  ")
	}
	return "\n" + indentBlock(FormatErrorPlainS(err), "  ")
}

func indentBlock(s string, indent string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if line != "" {
			b.WriteString(indent)
		}
		b.WriteString(line)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Integration strategy 3: FormatExtra for full control (advanced)
//
// This uses ConsoleWriter's FormatExtra to append the formatted error
// as a separate block below the normal log line, rather than inlining
// it in the error= field. This gives the cleanest visual output.
//
// Usage:
//
//	writer := zerolog.ConsoleWriter{
//	    Out:         os.Stderr,
//	    FormatExtra: eh.ConsoleFormatErrorExtra(false),
//	    // Optionally hide the default error field:
//	    FieldsExclude: []string{"error"},
//	}
//	logger := zerolog.New(writer).With().Timestamp().Logger()
//	logger.Error().Err(err).Str("user", "alice").Msg("request failed")
//
// Output:
//
//	12:34PM ERR request failed user=alice
//	  Error: user lookup failed
//	  ├── cause: query failed
//	  │   at Server.HandleRequest (server.go:42)
//	  └── cause: connection refused
//	      at DB.Connect (db.go:15)
// ---------------------------------------------------------------------------

// ConsoleFormatErrorExtra returns a FormatExtra function that appends the
// formatted error below the log line. It reads the "error" field from the
// event map and renders it.
func ConsoleFormatErrorExtra(useColor bool) func(map[string]interface{}, *bytes.Buffer) error {
	return func(evt map[string]interface{}, buf *bytes.Buffer) error {
		errField, ok := evt[zerolog.ErrorFieldName]
		if !ok {
			return nil
		}

		// The error field is a string when ErrorMarshalFunc returns a string,
		// or it's the JSON-decoded representation of whatever ErrorMarshalFunc returned.
		var formatted string
		switch v := errField.(type) {
		case string:
			if v == "" {
				return nil
			}
			// If it's already our formatted output, use it directly
			if strings.Contains(v, "cause:") || strings.Contains(v, "Error:") {
				formatted = v
			} else {
				// Simple error string — just show it inline, no need for the block
				return nil
			}
		default:
			// Not a string — let the default handler deal with it
			return nil
		}

		buf.WriteByte('\n')
		lines := strings.Split(strings.TrimRight(formatted, "\n"), "\n")
		for i, line := range lines {
			if i > 0 {
				buf.WriteByte('\n')
			}
			buf.WriteString("  ")
			buf.WriteString(line)
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// SetupConsoleLogger is a convenience function that configures a logger for
// human-readable console output with properly formatted errors.
//
// Usage:
//
//	logger := eh.SetupConsoleLogger(os.Stderr)
//	logger.Error().Err(err).Msg("failed")
// ---------------------------------------------------------------------------

// SetupConsoleLogger creates a zerolog.Logger configured for human-readable
// terminal output. Errors are formatted using the multi-line formatter,
// and the "error" JSON field is excluded from the inline fields (it appears
// as a block below the log line instead).
//
// This sets zerolog.ErrorMarshalFunc globally as a side effect.
func SetupConsoleLogger(out *bytes.Buffer) zerolog.Logger {
	// Override global error marshaling to produce our human-readable format
	zerolog.ErrorMarshalFunc = ErrorMarshalFuncHuman

	writer := zerolog.ConsoleWriter{
		Out:           out,
		NoColor:       false,
		FieldsExclude: []string{zerolog.ErrorFieldName},
		FormatExtra:   ConsoleFormatErrorExtra(true),
	}

	return zerolog.New(writer).With().Timestamp().Logger()
}
