package logging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// runApply runs a minimal cli.App whose Before is logging.Apply (the real
// flag/env resolution path) and whose Action emits one info event, so the
// test exercises exactly what a host does at startup.
func runApply(t *testing.T, args []string, msg string) {
	t.Helper()
	app := &cli.App{
		Flags:  LoggingFlags,
		Before: Apply,
		Action: func(c *cli.Context) error {
			log.Info().Str("k", "v").Msg(msg)
			return nil
		},
	}
	require.NoError(t, app.Run(append([]string{"prog"}, args...)))
}

// Test_Apply_HonorsLogFileFormatColor proves the primary logger (and thus
// the facts-log-bridge passthrough, which reuses OperatorWriter) honors
// --logFile, --logFormat, and --logColor end-to-end — the config the
// bridge install used to silently drop.
func Test_Apply_HonorsLogFileFormatColor(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "out.log")
	runApply(t, []string{"--logFormat=console", "--logFile=" + logPath, "--logLevel=info"}, "hello-file")

	require.NotNil(t, OperatorWriter(), "Apply must record the operator writer for the bridge")

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, "hello-file", "--logFile must capture the event (not stderr)")
	require.Contains(t, out, "k=v", "console field rendering must reach the file")
	require.NotContains(t, out, "\x1b[", "a file destination must force no-color (no ANSI escapes)")
}

// Test_Apply_EnvVarsHonored proves BOXER_LOG_FORMAT and BOXER_LOG_FILE
// (no CLI flags at all) drive the writer, via the flag EnvVars binding.
func Test_Apply_EnvVarsHonored(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "env.log")
	t.Setenv("BOXER_LOG_FORMAT", "json")
	t.Setenv("BOXER_LOG_FILE", logPath)

	runApply(t, nil /* no CLI flags — env only */, "env-json")

	_, ok := OperatorWriter().(*JsonIndentLogger)
	require.Truef(t, ok, "BOXER_LOG_FORMAT=json must select the JSON writer, got %T", OperatorWriter())

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "env-json", "BOXER_LOG_FILE must capture the event")
}
