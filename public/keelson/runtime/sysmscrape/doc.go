// Package sysmscrape is the scraper-side glue of the system-metrics plane: it
// wires the /proc- and /sys-reading collectors (public/observability/sysmetrics)
// to a sysmetricsbus Producer.
//
// It is deliberately a separate package from sysmetricsbus so that the bus
// package stays free of collector imports (ADR-0090 SD6): a pure subscriber
// (apps/imztop) imports sysmetricsbus for its Consumer/Codec and pulls in no
// /proc reader, while only the components that actually scrape — the carousel
// host's co-located producer, the standalone sysmetricsd command, the
// screenshot tour, and tests — import sysmscrape.
package sysmscrape
