package sensors

import (
	"context"
	"errors"
	"io/fs"
	"iter"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

// HwmonClassDir is the sysfs-relative root for kernel hwmon devices.
const HwmonClassDir = "class/hwmon"

// DefaultCriticalC is the per-sensor critical threshold to use when no
// "<basepath>_crit" file exists. Matches btop's 95000-millidegree default.
const DefaultCriticalC float32 = 95

// TempReading is a single temperature sample with its sensor metadata.
type TempReading struct {
	// Name is "<chip>/<label>", e.g. "coretemp/Package id 0".
	Name string

	// Path is the sysfs-relative path to the temp*_input file the reading
	// came from. Useful for diagnostics and for re-reading without re-
	// discovering.
	Path string

	// TempC is the current temperature in degrees Celsius. Sysfs reports
	// millidegrees as integers; we divide by 1000.
	TempC float32

	// CriticalC is the manufacturer-declared critical threshold in degrees
	// Celsius, or [DefaultCriticalC] when no _crit file exists.
	CriticalC float32

	// KindCPUPackage is true when the label looks like a CPU package
	// sensor: "Package id N", "Tdie", or "SoC Temperature".
	KindCPUPackage bool

	// KindCPUCore is true when the label looks like a per-core CPU
	// sensor: "Core N" or "Tccd N".
	KindCPUCore bool
}

// CollectorI is the public surface a sensor enumerator implements.
type CollectorI interface {
	All(ctx context.Context) iter.Seq2[TempReading, error]
	Sample(ctx context.Context) (readings []TempReading, err error)
}

// Options configures a [Collector].
type Options struct {
	// Sys, when non-nil, overrides the sysfs.Reader the Collector reads
	// from. The default is sysfs.New("") which resolves "/sys".
	Sys *sysfs.Reader
}

// Collector enumerates temperature sensors under /sys/class/hwmon. The
// zero value is not directly usable — call [New].
type Collector struct {
	sys *sysfs.Reader
}

// New returns a sensor Collector configured by opts. The returned error
// is always nil today; the signature reserves the slot for forward-
// compatibility.
func New(opts Options) (inst *Collector, err error) {
	if opts.Sys == nil {
		opts.Sys = sysfs.New("")
	}
	inst = &Collector{sys: opts.Sys}
	return
}

var _ CollectorI = (*Collector)(nil)

// All iterates discovered temperature sensors. Discovery is repeated each
// call (cheap on a typical machine — one or two hwmon entries with a
// handful of inputs); a hot loop should call [Sample] and reuse the slice.
//
// A missing /sys/class/hwmon directory yields nothing without error; this
// matches low-end / virtualized hardware that expose no hwmon class.
func (inst *Collector) All(ctx context.Context) (seq iter.Seq2[TempReading, error]) {
	return func(yield func(TempReading, error) bool) {
		names, err := inst.sys.ListDir(HwmonClassDir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return
			}
			yield(TempReading{}, err)
			return
		}
		for _, hwmonName := range names {
			if !strings.HasPrefix(hwmonName, "hwmon") {
				continue
			}
			select {
			case <-ctx.Done():
				yield(TempReading{}, ctx.Err())
				return
			default:
			}
			if !inst.walkHwmon(hwmonName, yield) {
				return
			}
		}
	}
}

// Sample returns all discovered temperature readings as a slice. Errors
// from individual sensors are appended via yield-then-skip; the slice is
// the union of successful reads. The first hard error (e.g. ctx cancel)
// aborts and returns the partial slice.
func (inst *Collector) Sample(ctx context.Context) (readings []TempReading, err error) {
	for r, ierr := range inst.All(ctx) {
		if ierr != nil {
			err = ierr
			return
		}
		readings = append(readings, r)
	}
	return
}

// walkHwmon enumerates one hwmon device, yielding TempReadings via yield.
// Returns false when the caller broke out of iteration so the outer loop
// can stop too.
func (inst *Collector) walkHwmon(hwmonName string, yield func(TempReading, error) bool) (cont bool) {
	base := filepath.Join(HwmonClassDir, hwmonName)
	chip, _ := inst.sys.ReadString(filepath.Join(base, "name"))
	if chip == "" {
		chip = hwmonName
	}

	// Try the hwmon dir first. If it has no temp*_input files, fall back
	// to <hwmon>/device/. Some chipsets (k10temp on certain kernels) put
	// the temp files under device/.
	dir := base
	names, err := inst.sys.ListDir(base)
	if err != nil {
		return true
	}
	if !hasTempInputs(names) {
		deviceDir := filepath.Join(base, "device")
		dnames, derr := inst.sys.ListDir(deviceDir)
		if derr != nil || !hasTempInputs(dnames) {
			return true
		}
		dir = deviceDir
		names = dnames
	}

	for _, n := range names {
		if !strings.HasPrefix(n, "temp") || !strings.HasSuffix(n, "_input") {
			continue
		}
		inputPath := filepath.Join(dir, n)
		basePath := strings.TrimSuffix(inputPath, "_input")

		tempStr, terr := inst.sys.ReadString(inputPath)
		if terr != nil {
			// Best-effort: skip unreadable inputs. Kernel files may
			// transiently disappear (sensor unplug), have unusual
			// permissions, or sit behind broken symlink chains; none
			// warrant aborting enumeration of the remaining sensors.
			continue
		}
		tempC, perr := parseMilliC(tempStr)
		if perr != nil {
			continue
		}

		label, _ := inst.sys.ReadString(basePath + "_label")
		if label == "" {
			label = strings.TrimSuffix(n, "_input")
		}

		critC := DefaultCriticalC
		if critStr, cerr := inst.sys.ReadString(basePath + "_crit"); cerr == nil && critStr != "" {
			if c, err := parseMilliC(critStr); err == nil {
				critC = c
			}
		}

		tr := TempReading{
			Name:           chip + "/" + label,
			Path:           inputPath,
			TempC:          tempC,
			CriticalC:      critC,
			KindCPUPackage: isCPUPackageLabel(label),
			KindCPUCore:    isCPUCoreLabel(label),
		}
		if !yield(tr, nil) {
			return false
		}
	}
	return true
}

func hasTempInputs(names []string) (yes bool) {
	for _, n := range names {
		if strings.HasPrefix(n, "temp") && strings.HasSuffix(n, "_input") {
			return true
		}
	}
	return false
}

func parseMilliC(s string) (c float32, err error) {
	var n int64
	n, err = strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return
	}
	c = float32(n) / 1000
	return
}

func isCPUPackageLabel(label string) (yes bool) {
	return strings.HasPrefix(label, "Package id") ||
		strings.HasPrefix(label, "Tdie") ||
		strings.HasPrefix(label, "SoC Temperature")
}

func isCPUCoreLabel(label string) (yes bool) {
	return strings.HasPrefix(label, "Core") ||
		strings.HasPrefix(label, "Tccd")
}
