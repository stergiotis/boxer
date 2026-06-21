package sysmsnap

// TempReading is a single temperature sample with its sensor metadata.
type TempReading struct {
	// Name is "<chip>/<label>", e.g. "coretemp/Package id 0".
	Name string

	// Path is the sysfs-relative path to the temp*_input file the reading
	// came from. Useful for diagnostics and for re-reading without re-
	// discovering.
	Path string

	// TempC is the current temperature in degrees Celsius. Sysfs reports
	// millidegrees as integers; the collector divides by 1000.
	TempC float32

	// CriticalC is the manufacturer-declared critical threshold in degrees
	// Celsius, or the collector's default when no _crit file exists.
	CriticalC float32

	// KindCPUPackage is true when the label looks like a CPU package
	// sensor: "Package id N", "Tdie", or "SoC Temperature".
	KindCPUPackage bool

	// KindCPUCore is true when the label looks like a per-core CPU
	// sensor: "Core N" or "Tccd N".
	KindCPUCore bool
}
