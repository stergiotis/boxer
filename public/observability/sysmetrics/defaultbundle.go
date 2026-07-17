package sysmetrics

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/battery"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/container"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/disk"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/mem"
	netcoll "github.com/stergiotis/boxer/public/observability/sysmetrics/net"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/psi"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sensors"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sockets"
)

// DefaultProcSampleCap bounds the number of processes fully sampled per tick
// (proc.Options.MaxProcs). 256 exceeds what a process panel surfaces in
// practice while keeping the per-tick /proc walk allocation-flat.
const DefaultProcSampleCap = 256

// DefaultBundleOptions wires the standard Linux collector set (ADR-0020 M2:
// CPU + Mem + Disk + Net + Battery + Sensors + Proc + Container + PSI) into a
// BundleOptions, shared by every scraper (imztop's co-located producer and
// the standalone sysmetricsd command) so the collector wiring lives once.
//
// CPU and Mem are mandatory: failing to build either returns an error. Every
// other collector is best-effort — a build failure logs a warning and omits
// that domain (its section simply goes empty downstream). GPU is excluded on
// purpose: it is vendor-build-tag-gated, so the caller wires it (imztop's
// wireGPUSampler) after this returns, where the tags are visible.
func DefaultBundleOptions() (bopts BundleOptions, err error) {
	cpuC, cerr := cpu.New(cpu.Options{})
	if cerr != nil {
		err = eh.Errorf("sysmetrics: build cpu collector: %w", cerr)
		return
	}
	bopts.CPU = cpuC

	memC, merr := mem.New(mem.Options{})
	if merr != nil {
		err = eh.Errorf("sysmetrics: build mem collector: %w", merr)
		return
	}
	bopts.Mem = memC

	if diskC, derr := disk.New(disk.Options{}); derr != nil {
		log.Warn().Err(derr).Msg("sysmetrics: build disk collector failed; disk metrics disabled")
	} else {
		bopts.Disk = diskC
	}
	if netC, nerr := netcoll.New(netcoll.Options{}); nerr != nil {
		log.Warn().Err(nerr).Msg("sysmetrics: build net collector failed; net metrics disabled")
	} else {
		bopts.Net = netC
	}
	if batC, berr := battery.New(battery.Options{}); berr != nil {
		log.Warn().Err(berr).Msg("sysmetrics: build battery collector failed; battery metrics disabled")
	} else {
		bopts.Battery = batC
	}
	if senC, serr := sensors.New(sensors.Options{}); serr != nil {
		log.Warn().Err(serr).Msg("sysmetrics: build sensors collector failed; sensors disabled")
	} else {
		bopts.Sensors = senC
	}
	if procC, perr := proc.New(proc.Options{MaxProcs: DefaultProcSampleCap}); perr != nil {
		log.Warn().Err(perr).Msg("sysmetrics: build proc collector failed; process table disabled")
	} else {
		bopts.Proc = procC
	}
	if cntC, cerr2 := container.New(container.Options{}); cerr2 != nil {
		log.Warn().Err(cerr2).Msg("sysmetrics: build container detector failed; container badge disabled")
	} else {
		bopts.Container = cntC
	}
	if psiC, psierr := psi.New(psi.Options{}); psierr != nil {
		log.Warn().Err(psierr).Msg("sysmetrics: build PSI collector failed; pressure disabled")
	} else {
		bopts.PSI = psiC
	}
	if sockC, sockerr := sockets.New(sockets.Options{}); sockerr != nil {
		log.Warn().Err(sockerr).Msg("sysmetrics: build sockets collector failed; listener table disabled")
	} else {
		bopts.Sockets = sockC
	}
	// GPU is vendor-build-tag-gated (gpu_rocm wires AMD; a no-op otherwise).
	wireGPU(&bopts)
	return
}
