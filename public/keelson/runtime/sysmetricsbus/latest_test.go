package sysmetricsbus_test

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

func TestParseBundleSubjectHost(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOk bool
	}{
		{"sysmetrics.demo-box-1.bundle", "demo-box-1", true},
		{"sysmetrics.local.bundle", "local", true},
		{"sysmetrics..bundle", "", false},          // empty host token
		{"sysmetrics.a.b.bundle", "", false},       // deeper hierarchy
		{"sysmetrics.a.cpu", "", false},            // not the bundle leaf
		{"othermetrics.a.bundle", "", false},       // wrong root
		{"sysmetrics.bundle", "", false},           // no host token
	}
	for _, tc := range cases {
		host, ok := sysmetricsbus.ParseBundleSubjectHost(tc.in)
		if ok != tc.wantOk || host != tc.want {
			t.Errorf("ParseBundleSubjectHost(%q) = (%q, %v), want (%q, %v)",
				tc.in, host, ok, tc.want, tc.wantOk)
		}
	}
}

// TestLatestHolder_TracksLatestPerHost proves the holder keys by host
// token, keeps only the newest bundle per host, and survives a corrupt
// frame. inprocbus dispatch is synchronous, so no settling waits.
func TestLatestHolder_TracksLatestPerHost(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	subCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}}
	pubCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub}}

	now := time.UnixMilli(5_000)
	holder, err := sysmetricsbus.StartLatestHolder(sysmetricsbus.LatestHolderOptions{
		Bus:     bus.NewClient("holder", subCap),
		NowFunc: func() time.Time { return now },
		Log:     zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Close() })
	require.Empty(t, holder.Hosts())

	pub := bus.NewClient("scraper", pubCap)
	codec := sysmetricsbus.NewCBORCodec()
	publish := func(host string, sampledAt int64) {
		payload, cerr := codec.Encode(&sysmsnap.BundleSnapshot{SampledAtUnixMs: sampledAt})
		require.NoError(t, cerr)
		require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject(host), payload))
	}

	publish("box-b", 100)
	publish("box-a", 200)
	now = now.Add(time.Second)
	publish("box-a", 300) // newer bundle replaces box-a's entry

	require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject("box-c"), []byte("not cbor"))) // dropped, no entry

	hosts := holder.Hosts()
	require.Len(t, hosts, 2)
	require.Equal(t, "box-a", hosts[0].Host)
	require.Equal(t, int64(300), hosts[0].Snap.SampledAtUnixMs)
	require.Equal(t, int64(6_000), hosts[0].ReceivedAtUnixMs)
	require.Equal(t, "box-b", hosts[1].Host)
	require.Equal(t, int64(100), hosts[1].Snap.SampledAtUnixMs)
}
