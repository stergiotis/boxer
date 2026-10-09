package profiling

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	cli "github.com/urfave/cli/v3"
)

// TestCpuProfileStopPath runs the flags through a cli.Command wired like the
// real hosts (After calls ProfilingHandleExit) and then requires the
// output to be a complete gzip stream. runtime/pprof serializes the
// profile only in StopCPUProfile, so an unreadable or empty file means
// the exit handler did not recognise the flag.
func TestCpuProfileStopPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu.pb.gz")
	app := &cli.Command{
		Flags:  ProfilingFlags,
		Action: func(context.Context, *cli.Command) error { return nil },
		After: func(ctx context.Context, cmd *cli.Command) error {
			ProfilingHandleExit(ctx, cmd)
			return nil
		},
	}
	err := app.Run(context.Background(), []string{"prog", "--" + flagNameCpuOutputFile, path})
	if err != nil {
		t.Fatalf("app run: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open profile: %v", err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("profile is not a gzip stream (stop path did not run?): %v", err)
	}
	n, err := io.Copy(io.Discard, zr)
	if err != nil {
		t.Fatalf("profile gzip stream is truncated: %v", err)
	}
	if err = zr.Close(); err != nil {
		t.Fatalf("profile gzip stream fails checksum: %v", err)
	}
	if n == 0 {
		t.Fatal("profile payload is empty")
	}
}
