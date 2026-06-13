// Package mem samples Linux memory statistics from /proc/meminfo and,
// optionally, the ZFS ARC accounting at /proc/spl/kstat/zfs/arcstats.
//
// Provenance: btop src/linux/btop_collect.cpp:2118 (Mem::collect). The
// shape of the [Snapshot] tracks the fields btop populates plus an
// explicit Buffers field btop folds into "cached" on the UI side; we keep
// it separate because callers may want either grouping.
//
// # Usage
//
//	c := mem.New(mem.Options{})
//	for {
//	    snap, err := c.Sample(ctx)
//	    if err != nil { /* ... */ }
//	    fmt.Println(snap.UsedBytes, "/", snap.TotalBytes)
//	    time.Sleep(time.Second)
//	}
//
// All byte counts are absolute bytes; the kB lines from /proc/meminfo are
// scaled left-shift-10 (the kernel's documented kB unit is binary-1024).
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md (M1).
package mem
