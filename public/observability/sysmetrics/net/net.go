package net

import (
	"context"
	"errors"
	"io/fs"
	"math"
	stdnet "net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// SysClassNet is the sysfs-relative root for kernel network interfaces.
const SysClassNet = "class/net"

// DefaultRolloverMax is the rollover boundary used for kernel byte
// counters when a per-interface override is not configured. 64-bit
// counters effectively never wrap; setting the boundary here to
// math.MaxUint32 catches the 32-bit virtual-NIC case (cited at
// btop_collect.cpp:2760) at the cost of a one-sample miscount per
// rollover for the unusual 64-bit-but-resetting case.
const DefaultRolloverMax uint64 = math.MaxUint32

// CollectorI is the public surface a network sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap sysmsnap.NetSnapshot, err error)
}

// InterfaceLister returns the live network interface list as the standard
// library's [net.Interface] slice. The default delegates to [net.Interfaces];
// tests inject a synthetic list.
type InterfaceLister func() (ifaces []stdnet.Interface, err error)

// Options configures a [Collector].
type Options struct {
	Sys *sysfs.Reader

	// Lister, when non-nil, overrides the system call that enumerates
	// interfaces. Defaults to [net.Interfaces].
	Lister InterfaceLister

	// NowFunc, when non-nil, overrides the wall+monotonic clock.
	NowFunc func() time.Time

	// RolloverMax overrides the per-counter rollover boundary used when
	// a kernel counter steps backwards. 0 disables rollover handling
	// (clamp to zero on backwards step). When unset, [DefaultRolloverMax]
	// applies.
	RolloverMax uint64

	// IncludeLoopback, when false (the default), filters out the kernel
	// loopback interface. Set true to include it.
	IncludeLoopback bool
}

// Collector samples network interface state.
type Collector struct {
	sys             *sysfs.Reader
	lister          InterfaceLister
	nowFn           func() time.Time
	rolloverMax     uint64
	includeLoopback bool

	prev map[string]ifaceTick
}

type ifaceTick struct {
	at      time.Time
	rxBytes uint64
	txBytes uint64
}

// New returns a network Collector configured by opts. The returned
// error is always nil today; the signature reserves the slot for
// forward-compatibility.
func New(opts Options) (inst *Collector, err error) {
	if opts.Sys == nil {
		opts.Sys = sysfs.New("")
	}
	if opts.Lister == nil {
		opts.Lister = stdnet.Interfaces
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	if opts.RolloverMax == 0 {
		opts.RolloverMax = DefaultRolloverMax
	}
	inst = &Collector{
		sys:             opts.Sys,
		lister:          opts.Lister,
		nowFn:           opts.NowFunc,
		rolloverMax:     opts.RolloverMax,
		includeLoopback: opts.IncludeLoopback,
		prev:            map[string]ifaceTick{},
	}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample reads the live interface list and returns a Snapshot. Cumulative
// byte counters are read once per call; rates are derived against the
// prior call's tick.
func (inst *Collector) Sample(ctx context.Context) (snap sysmsnap.NetSnapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	now := inst.nowFn()
	snap.SampledAtUnixMs = now.UnixMilli()

	var ifaces []stdnet.Interface
	ifaces, err = inst.lister()
	if err != nil {
		err = eh.Errorf("net.Interfaces: %w", err)
		return
	}

	seen := make(map[string]struct{}, len(ifaces))
	for _, ifc := range ifaces {
		if !inst.includeLoopback && ifc.Flags&stdnet.FlagLoopback != 0 {
			continue
		}
		seen[ifc.Name] = struct{}{}

		entry := sysmsnap.NetInterface{
			Name:         ifc.Name,
			Index:        int32(ifc.Index),
			HardwareAddr: strings.ToLower(ifc.HardwareAddr.String()),
			Up:           ifc.Flags&stdnet.FlagUp != 0,
			Running:      ifc.Flags&stdnet.FlagRunning != 0,
		}

		// IP enumeration is best-effort — interface might disappear
		// between Interfaces() and Addrs() and that should not abort the
		// whole snapshot.
		if addrs, addrErr := ifc.Addrs(); addrErr == nil {
			for _, a := range addrs {
				ip := extractIP(a)
				if ip == nil {
					continue
				}
				if v4 := ip.To4(); v4 != nil {
					entry.IPv4 = append(entry.IPv4, v4.String())
				} else {
					entry.IPv6 = append(entry.IPv6, ip.String())
				}
			}
		}

		if entry.HardwareAddr == "" {
			// /sys/class/net/{name}/address is the kernel-side fallback;
			// for some virtual interfaces stdlib leaves HardwareAddr empty.
			if mac, mErr := inst.sys.ReadString(filepath.Join(SysClassNet, ifc.Name, "address")); mErr == nil {
				entry.HardwareAddr = strings.ToLower(mac)
			}
		}

		entry.RxBytes = inst.readByteCounter(ifc.Name, "rx_bytes")
		entry.TxBytes = inst.readByteCounter(ifc.Name, "tx_bytes")

		// Rate computation against prior tick.
		if prev, ok := inst.prev[ifc.Name]; ok {
			elapsed := now.Sub(prev.at)
			if elapsed > 0 {
				entry.RxBytesPerSec = perSecond(prev.rxBytes, entry.RxBytes, inst.rolloverMax, elapsed)
				entry.TxBytesPerSec = perSecond(prev.txBytes, entry.TxBytes, inst.rolloverMax, elapsed)
			}
		}
		inst.prev[ifc.Name] = ifaceTick{at: now, rxBytes: entry.RxBytes, txBytes: entry.TxBytes}

		snap.Interfaces = append(snap.Interfaces, entry)
	}

	// Drop disappeared interfaces from the prior-tick map so it does not
	// grow without bound.
	for name := range inst.prev {
		if _, ok := seen[name]; !ok {
			delete(inst.prev, name)
		}
	}

	slices.SortFunc(snap.Interfaces, func(a, b sysmsnap.NetInterface) int {
		return strings.Compare(a.Name, b.Name)
	})
	return
}

func (inst *Collector) readByteCounter(name, leaf string) (n uint64) {
	s, err := inst.sys.ReadString(filepath.Join(SysClassNet, name, "statistics", leaf))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0
		}
		return 0
	}
	v, perr := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if perr != nil {
		return 0
	}
	return v
}

// perSecond computes a per-second rate from prev/now byte counters. When
// now < prev, a counter rollover at rolloverMax is assumed (matches
// btop_collect.cpp:2760-2767). When rolloverMax is 0, backward steps
// clamp to zero rate.
func perSecond(prev, now, rolloverMax uint64, elapsed time.Duration) (rate uint64) {
	var delta uint64
	if now >= prev {
		delta = now - prev
	} else if rolloverMax > 0 && prev <= rolloverMax {
		delta = (rolloverMax - prev) + now + 1
	} else {
		return 0
	}
	secs := elapsed.Seconds()
	if secs <= 0 {
		return 0
	}
	return uint64(float64(delta) / secs)
}

func extractIP(addr stdnet.Addr) (ip stdnet.IP) {
	switch v := addr.(type) {
	case *stdnet.IPNet:
		return v.IP
	case *stdnet.IPAddr:
		return v.IP
	}
	return nil
}
