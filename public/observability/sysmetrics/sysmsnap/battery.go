package sysmsnap

// StateE classifies a battery's charge/discharge state.
type StateE uint8

const (
	StateUnknown StateE = iota
	StateCharging
	StateDischarging
	StateFull
	StateNotCharging
)

func (s StateE) String() (out string) {
	switch s {
	case StateCharging:
		return "charging"
	case StateDischarging:
		return "discharging"
	case StateFull:
		return "full"
	case StateNotCharging:
		return "not_charging"
	default:
		return "unknown"
	}
}

// AllStates lists every defined [StateE] value.
var AllStates = []StateE{StateUnknown, StateCharging, StateDischarging, StateFull, StateNotCharging}

// BatteryStatus is the per-battery sample.
type BatteryStatus struct {
	Name string // sysfs entry name, e.g. "BAT0"
	Type string // kernel-reported, "Battery" or "UPS"

	// Percent is the charge level [0..100]. Resolved from `capacity` when
	// present, otherwise derived from energy_now/energy_full or
	// charge_now/charge_full.
	Percent uint8

	// State is the kernel-reported charge state, normalized.
	State StateE

	// PowerWatts is the instantaneous power draw or fill rate. 0 when no
	// power_now / current+voltage path is exposed by the kernel for this
	// battery.
	PowerWatts float32

	// SecondsToFull is positive when charging; -1 when unknown or not
	// charging.
	SecondsToFull int64

	// SecondsToEmpty is positive when discharging; -1 when unknown or
	// charging.
	SecondsToEmpty int64
}

// ACAdapter is one Mains-type power supply.
type ACAdapter struct {
	Name   string // "AC", "ACAD", "ADP1"
	Online bool
}

// BatterySnapshot is a single sample of all power supplies.
type BatterySnapshot struct {
	SampledAtUnixMs int64
	Batteries       []BatteryStatus
	ACAdapters      []ACAdapter
}
