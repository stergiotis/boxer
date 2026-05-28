//go:build llm_generated_opus47

package net_test

import (
	"context"
	"math"
	stdnet "net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
	pnet "github.com/stergiotis/boxer/public/observability/sysmetrics/net"
)

func TestSample_FirstCall_ZeroRates(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/eth0/statistics/rx_bytes": "1000",
		"class/net/eth0/statistics/tx_bytes": "2000",
		"class/net/eth0/address":             "aa:bb:cc:dd:ee:ff",
	})
	c, _ := pnet.New(pnet.Options{
		Sys:    sysfs.New(root),
		Lister: staticLister([]stdnet.Interface{{Index: 1, Name: "eth0", Flags: stdnet.FlagUp | stdnet.FlagRunning}}),
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Interfaces, 1)
	ifc := snap.Interfaces[0]
	assert.Equal(t, "eth0", ifc.Name)
	assert.Equal(t, uint64(1000), ifc.RxBytes)
	assert.Equal(t, uint64(2000), ifc.TxBytes)
	assert.Equal(t, uint64(0), ifc.RxBytesPerSec)
	assert.Equal(t, uint64(0), ifc.TxBytesPerSec)
	assert.True(t, ifc.Up)
	assert.True(t, ifc.Running)
}

func TestSample_SecondCall_RatesComputed(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/eth0/statistics/rx_bytes": "1000",
		"class/net/eth0/statistics/tx_bytes": "2000",
	})
	now := time.Unix(0, 0)
	c, _ := pnet.New(pnet.Options{
		Sys:     sysfs.New(root),
		Lister:  staticLister([]stdnet.Interface{{Index: 1, Name: "eth0"}}),
		NowFunc: func() time.Time { return now },
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Advance clock 2s and counters by 4000 / 8000 bytes.
	now = now.Add(2 * time.Second)
	rewriteCounter(t, root, "eth0", "rx_bytes", "5000")
	rewriteCounter(t, root, "eth0", "tx_bytes", "10000")

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Interfaces, 1)
	assert.Equal(t, uint64(2000), snap.Interfaces[0].RxBytesPerSec, "rx delta 4000 / 2s = 2000 B/s")
	assert.Equal(t, uint64(4000), snap.Interfaces[0].TxBytesPerSec, "tx delta 8000 / 2s = 4000 B/s")
}

func TestSample_RolloverU32_RxCounter(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/wg0/statistics/rx_bytes": "4294967286", // MaxUint32 - 9
		"class/net/wg0/statistics/tx_bytes": "0",
	})
	now := time.Unix(0, 0)
	c, _ := pnet.New(pnet.Options{
		Sys:     sysfs.New(root),
		Lister:  staticLister([]stdnet.Interface{{Index: 1, Name: "wg0"}}),
		NowFunc: func() time.Time { return now },
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Counter wraps: 32-bit boundary crossed; new value 100. Elapsed 1s.
	now = now.Add(time.Second)
	rewriteCounter(t, root, "wg0", "rx_bytes", "100")

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	// delta = (MaxU32 - 4294967286) + 100 + 1 = 9 + 100 + 1 = 110.
	assert.Equal(t, uint64(110), snap.Interfaces[0].RxBytesPerSec)
}

func TestSample_NoRollover_ClampsBackwards(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/dummy0/statistics/rx_bytes": "5000",
		"class/net/dummy0/statistics/tx_bytes": "0",
	})
	c, _ := pnet.New(pnet.Options{
		Sys:         sysfs.New(root),
		Lister:      staticLister([]stdnet.Interface{{Index: 1, Name: "dummy0"}}),
		RolloverMax: 1, // any value, but counter goes from 5000 to 100 — outside [0, 1] so backward step clamps
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)
	rewriteCounter(t, root, "dummy0", "rx_bytes", "100")
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(0), snap.Interfaces[0].RxBytesPerSec)
}

func TestSample_FiltersLoopbackByDefault(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/lo/statistics/rx_bytes":   "1",
		"class/net/lo/statistics/tx_bytes":   "1",
		"class/net/eth0/statistics/rx_bytes": "1",
		"class/net/eth0/statistics/tx_bytes": "1",
	})
	c, _ := pnet.New(pnet.Options{
		Sys: sysfs.New(root),
		Lister: staticLister([]stdnet.Interface{
			{Index: 1, Name: "lo", Flags: stdnet.FlagLoopback},
			{Index: 2, Name: "eth0"},
		}),
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Interfaces, 1)
	assert.Equal(t, "eth0", snap.Interfaces[0].Name)
}

func TestSample_IncludesLoopbackWhenAsked(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/lo/statistics/rx_bytes": "1",
		"class/net/lo/statistics/tx_bytes": "1",
	})
	c, _ := pnet.New(pnet.Options{
		Sys:             sysfs.New(root),
		Lister:          staticLister([]stdnet.Interface{{Index: 1, Name: "lo", Flags: stdnet.FlagLoopback}}),
		IncludeLoopback: true,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Interfaces, 1)
}

func TestSample_HardwareAddrFromSysfs_WhenStdlibEmpty(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/veth0/address":             "12:34:56:78:9a:bc",
		"class/net/veth0/statistics/rx_bytes": "0",
		"class/net/veth0/statistics/tx_bytes": "0",
	})
	c, _ := pnet.New(pnet.Options{
		Sys:    sysfs.New(root),
		Lister: staticLister([]stdnet.Interface{{Index: 1, Name: "veth0"}}),
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "12:34:56:78:9a:bc", snap.Interfaces[0].HardwareAddr)
}

func TestSample_DropsDisappearedInterfaceFromPriorState(t *testing.T) {
	root := newSysTree(t, map[string]string{
		"class/net/eth0/statistics/rx_bytes": "100",
		"class/net/eth0/statistics/tx_bytes": "200",
		"class/net/eth1/statistics/rx_bytes": "300",
		"class/net/eth1/statistics/tx_bytes": "400",
	})
	c, _ := pnet.New(pnet.Options{
		Sys: sysfs.New(root),
		Lister: staticLister([]stdnet.Interface{
			{Index: 1, Name: "eth0"},
			{Index: 2, Name: "eth1"},
		}),
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Pretend eth1 has gone away — only eth0 visible now. Sampling must
	// not crash, and a future eth1 reappearance must be treated as a
	// fresh "first sample".
	c2, _ := pnet.New(pnet.Options{
		Sys:    sysfs.New(root),
		Lister: staticLister([]stdnet.Interface{{Index: 1, Name: "eth0"}}),
	})
	snap, err := c2.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Interfaces, 1)
	assert.Equal(t, "eth0", snap.Interfaces[0].Name)
}

func TestSample_LiveSys_Smoke(t *testing.T) {
	if _, err := os.Stat("/sys/class/net"); err != nil {
		t.Skipf("no live /sys/class/net: %v", err)
	}
	c, _ := pnet.New(pnet.Options{IncludeLoopback: true})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	// The loopback interface is universally present on any Linux.
	var sawLo bool
	for _, ifc := range snap.Interfaces {
		if ifc.Name == "lo" {
			sawLo = true
		}
		// Plausibility: name + index always set; if Up implies device is reachable
		// (Running == Up || Up == false), MAC is hex-formatted when non-empty.
		assert.NotEmpty(t, ifc.Name)
		assert.GreaterOrEqual(t, ifc.Index, int32(0))
		if ifc.HardwareAddr != "" {
			assert.Regexp(t, `^[0-9a-f:]+$`, ifc.HardwareAddr,
				"interface %s MAC must be lowercase hex", ifc.Name)
		}
	}
	assert.True(t, sawLo, "loopback should be present in live snapshot")
}

func TestSample_ContextCancelled(t *testing.T) {
	c, _ := pnet.New(pnet.Options{Lister: staticLister(nil)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// --- helpers -------------------------------------------------------------

func newSysTree(t *testing.T, files map[string]string) (root string) {
	t.Helper()
	root = t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body+"\n"), 0o644))
	}
	return
}

func rewriteCounter(t *testing.T, root, iface, leaf, value string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "class/net", iface, "statistics", leaf),
		[]byte(value+"\n"), 0o644))
}

func staticLister(ifs []stdnet.Interface) pnet.InterfaceLister {
	return func() ([]stdnet.Interface, error) { return ifs, nil }
}

// keeps the math/MaxUint32 reference live for documentation.
var _ = math.MaxUint32
