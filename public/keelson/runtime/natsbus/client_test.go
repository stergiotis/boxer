package natsbus_test

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

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/natsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
	"github.com/stretchr/testify/require"
)

// findNatsServer locates the nats-server binary (PATH, then GOPATH/bin). The
// natsbus integration tests run against a real server (ADR-0090 P3 chose the
// installed binary over an embedded test-server dependency), so they skip
// cleanly when it is absent.
func findNatsServer(t *testing.T) (bin string) {
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

// startNATS launches a throwaway nats-server on a free port and returns its
// URL; it is killed at test cleanup.
func startNATS(t *testing.T) (url string) {
	t.Helper()
	bin := findNatsServer(t)
	port := freePort(t)
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

func freePort(t *testing.T) (port int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	port = l.Addr().(*net.TCPAddr).Port
	return
}

// TestClient_PublishSubscribe proves the BusI publish/subscribe path over a
// real NATS server.
func TestClient_PublishSubscribe(t *testing.T) {
	url := startNATS(t)

	pub, err := natsbus.Connect(natsbus.Options{URL: url, AppId: "test.pub"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pub.Close() })
	sub, err := natsbus.Connect(natsbus.Options{URL: url, AppId: "test.sub"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })

	got := make(chan []byte, 1)
	unsub, err := sub.Subscribe("sysmetrics.demo.bundle", func(m *app.Msg) {
		select {
		case got <- m.Payload:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(unsub)
	require.NoError(t, sub.Flush()) // SUB visible before we publish

	require.NoError(t, pub.Publish("sysmetrics.demo.bundle", []byte("hello")))
	select {
	case payload := <-got:
		require.Equal(t, []byte("hello"), payload)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive published message over NATS")
	}
}

// TestClient_RequestReply proves the request/reply path (the responder
// publishes to the reply subject).
func TestClient_RequestReply(t *testing.T) {
	url := startNATS(t)

	c, err := natsbus.Connect(natsbus.Options{URL: url, AppId: "test.rr"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	unsub, err := c.Subscribe("svc.echo", func(m *app.Msg) {
		_ = c.Publish(m.Reply, append([]byte("echo:"), m.Payload...))
	})
	require.NoError(t, err)
	t.Cleanup(unsub)
	require.NoError(t, c.Flush())

	reply, err := c.Request("svc.echo", []byte("ping"))
	require.NoError(t, err)
	require.Equal(t, []byte("echo:ping"), reply)
}

// TestSysmetricsProducerConsumerOverNATS is the P3 end-to-end proof: the
// sysmetricsbus Producer publishes BundleSnapshots over a real NATS server
// and the Consumer receives them — the same producer/consumer code P2
// exercised over inprocbus, now cross-transport. An empty Bundle (no
// collectors) exercises the transport without needing /proc.
func TestSysmetricsProducerConsumerOverNATS(t *testing.T) {
	url := startNATS(t)

	pub, err := natsbus.Connect(natsbus.Options{URL: url, AppId: sysmetricsbus.ServiceAppId})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pub.Close() })
	sub, err := natsbus.Connect(natsbus.Options{URL: url, AppId: "test.consumer"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })

	bundle, err := sysmetrics.NewBundle(sysmetrics.BundleOptions{})
	require.NoError(t, err)

	subject := sysmetricsbus.BundleSubject("test")
	codec := sysmetricsbus.NewCBORCodec()

	got := make(chan *sysmetrics.BundleSnapshot, 4)
	consumer, err := sysmetricsbus.NewConsumer(sysmetricsbus.ConsumerOptions{
		Bus:     sub,
		Subject: subject,
		Codec:   codec,
		Handler: func(snap *sysmetrics.BundleSnapshot) {
			select {
			case got <- snap:
			default:
			}
		},
	})
	require.NoError(t, err)
	require.NoError(t, consumer.Start())
	t.Cleanup(func() { _ = consumer.Close() })
	require.NoError(t, sub.Flush()) // SUB visible before the producer publishes

	producer, err := sysmetricsbus.NewProducer(sysmetricsbus.ProducerOptions{
		Bundle:   bundle,
		Bus:      pub,
		Subject:  subject,
		Codec:    codec,
		Interval: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	producer.Start(context.Background())
	t.Cleanup(func() { _ = producer.Close() })

	select {
	case snap := <-got:
		require.NotNil(t, snap)
	case <-time.After(3 * time.Second):
		t.Fatal("no BundleSnapshot delivered producer->NATS->consumer")
	}
}

// TestHeadlessBridge_EndToEnd proves the ADR-0090 headless deployment dataflow:
// a sysmetricsd-style scraper publishes to NATS; a carrier-side Bridge relays
// it onto an in-proc host bus; an imztop-style Consumer on that in-proc bus
// receives it. The carrier (in-proc side) never reads /proc — the scraper does,
// in its own process. An empty Bundle exercises the transport without /proc.
func TestHeadlessBridge_EndToEnd(t *testing.T) {
	url := startNATS(t)

	// External sysmetricsd: publish to NATS.
	scraperNats, err := natsbus.Connect(natsbus.Options{URL: url, AppId: sysmetricsbus.ServiceAppId})
	require.NoError(t, err)
	t.Cleanup(func() { _ = scraperNats.Close() })
	bundle, err := sysmetrics.NewBundle(sysmetrics.BundleOptions{})
	require.NoError(t, err)
	producer, err := sysmetricsbus.NewProducer(sysmetricsbus.ProducerOptions{
		Bundle:   bundle,
		Bus:      scraperNats,
		Subject:  sysmetricsbus.BundleSubject("box1"),
		Codec:    sysmetricsbus.NewCBORCodec(),
		Interval: 30 * time.Millisecond,
	})
	require.NoError(t, err)

	// Carrier: bridge NATS -> in-proc host bus.
	hostBus := inprocbus.NewInst(zerolog.Nop())
	bridgeNats, err := natsbus.Connect(natsbus.Options{URL: url, AppId: "carrier.bridge"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = bridgeNats.Close() })
	hostPub := hostBus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
	})
	bridgeStop, err := sysmetricsbus.Bridge(bridgeNats, hostPub, sysmetricsbus.BundleSubjectWildcard())
	require.NoError(t, err)
	t.Cleanup(bridgeStop)

	// imztop-side consumer on the in-proc host bus.
	got := make(chan *sysmetrics.BundleSnapshot, 4)
	hostSub := hostBus.NewClient("imztop", []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub},
	})
	consumer, err := sysmetricsbus.NewConsumer(sysmetricsbus.ConsumerOptions{
		Bus:     hostSub,
		Subject: sysmetricsbus.BundleSubjectWildcard(),
		Codec:   sysmetricsbus.NewCBORCodec(),
		Handler: func(s *sysmetrics.BundleSnapshot) {
			select {
			case got <- s:
			default:
			}
		},
	})
	require.NoError(t, err)
	require.NoError(t, consumer.Start())
	t.Cleanup(func() { _ = consumer.Close() })

	require.NoError(t, bridgeNats.Flush()) // bridge SUB visible before the scraper publishes
	producer.Start(context.Background())
	t.Cleanup(func() { _ = producer.Close() })

	select {
	case s := <-got:
		require.NotNil(t, s)
	case <-time.After(3 * time.Second):
		t.Fatal("no snapshot delivered scraper->NATS->bridge->inproc->consumer")
	}
}
