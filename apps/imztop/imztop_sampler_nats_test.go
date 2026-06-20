package imztop

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/natsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
	"github.com/stretchr/testify/require"
)

// TestSampler_NATSMode proves the ADR-0090 P3 consumer path: with
// IMZERO2_SYSMETRICS_NATS_URL set, NewSampler builds a pure NATS subscriber
// (no collectors, no producer), and a standalone scraper publishing over the
// same server drives its PublishedSnapshot. The scraper here is a Producer
// over an empty Bundle, so the test needs a NATS server but not /proc.
//
// Linux-only by convention (the nats-server binary is installed there for
// these tests); skips cleanly when nats-server is absent.
func TestSampler_NATSMode(t *testing.T) {
	url := startNATSForTest(t)
	sysmetricsbus.NatsURL.SetForTest(t, url) // NewSampler now selects NATS mode

	// Stand in for the standalone sysmetricsd scraper: publish an empty
	// bundle to a concrete host subject the consumer's wildcard matches.
	scraper, err := natsbus.Connect(natsbus.Options{URL: url, AppId: sysmetricsbus.ServiceAppId})
	require.NoError(t, err)
	t.Cleanup(func() { _ = scraper.Close() })
	bundle, err := sysmetrics.NewBundle(sysmetrics.BundleOptions{})
	require.NoError(t, err)
	producer, err := sysmetricsbus.NewProducer(sysmetricsbus.ProducerOptions{
		Bundle:   bundle,
		Bus:      scraper,
		Subject:  sysmetricsbus.BundleSubject("testhost"),
		Codec:    sysmetricsbus.NewCBORCodec(),
		Interval: 30 * time.Millisecond,
	})
	require.NoError(t, err)

	// imztop in NATS consumer-only mode.
	s, err := NewSampler(SamplerOptions{UpdateInterval: 30 * time.Millisecond, HistoryWindow: time.Second})
	require.NoError(t, err)
	require.Nil(t, s.producer, "NATS mode must not build a local producer")
	require.NotNil(t, s.natsClient, "NATS mode must hold a NATS client")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx) // subscribes
	t.Cleanup(func() { _ = s.Close() })
	producer.Start(ctx) // scraper begins publishing
	t.Cleanup(func() { _ = producer.Close() })

	var snap *PublishedSnapshot
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if snap = s.Latest(); snap != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotNil(t, snap, "imztop NATS-mode received no snapshot over the bus")
	require.NotEmpty(t, snap.HistoryTimeUnixSec, "windowing produced no history")

	// Pause must freeze the published frame even in NATS mode (the remote
	// scraper keeps publishing; onBundle drops frames locally).
	s.Pause(true)
	require.True(t, s.IsPaused())
	time.Sleep(90 * time.Millisecond)
	frozen := s.Latest().SampledAtUnixMs
	time.Sleep(120 * time.Millisecond)
	require.Equal(t, frozen, s.Latest().SampledAtUnixMs, "paused NATS-mode sampler kept advancing")
}

// --- nats-server test harness (see also runtime/natsbus tests) ---

func startNATSForTest(t *testing.T) (url string) {
	t.Helper()
	bin := findNatsServerForTest(t)
	port := freePortForTest(t)
	cmd := exec.Command(bin, "-a", "127.0.0.1", "-p", strconv.Itoa(port))
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, derr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if derr == nil {
			_ = conn.Close()
			return "nats://" + addr
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("nats-server at %s did not become ready", addr)
	return
}

func findNatsServerForTest(t *testing.T) (bin string) {
	t.Helper()
	if p, err := exec.LookPath("nats-server"); err == nil {
		return p
	}
	if out, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
		cand := filepath.Join(strings.TrimSpace(string(out)), "bin", "nats-server")
		if info, statErr := os.Stat(cand); statErr == nil && !info.IsDir() {
			return cand
		}
	}
	t.Skip("nats-server not found on PATH or in GOPATH/bin; skipping NATS integration test")
	return
}

func freePortForTest(t *testing.T) (port int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	port = l.Addr().(*net.TCPAddr).Port
	return
}
