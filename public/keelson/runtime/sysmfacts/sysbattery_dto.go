package sysmfacts

import "time"

// SysBattery is one power-supply sample of one host, column-major.
//
// It carries two independently lengthed groups: the battery arrays (Name…
// SecondsToEmpty) and the mains-adapter arrays (AcName, AcOnline). Each group
// is internally aligned by index — see [SysNet] for the contract — but the two
// groups are not aligned with each other, and a machine commonly has a
// different count of each.
type SysBattery struct {
	_ struct{} `kind:"sysBattery"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindBattery,symbol"`
	Host string `lw:"sysmBatteryHost,symbol"`

	// Name is the sysfs entry ("BAT0"); Type is the kernel's own kind
	// ("Battery" or "UPS").
	Name    []string `lw:"sysmBatteryName,symbolArray"`
	Type    []string `lw:"sysmBatteryType,symbolArray"`
	Percent []uint8  `lw:"sysmBatteryPercent,u8Array,ct=u8h"`
	// State is the normalized charge state as its numeric code, not its label,
	// so a stored row cannot drift with a rename on the Go side.
	State []uint8 `lw:"sysmBatteryState,u8Array,ct=u8h"`
	// PowerWatts is instantaneous draw or fill rate; 0 where the kernel exposes
	// no power_now or current+voltage path for that battery.
	PowerWatts []float32 `lw:"sysmBatteryPowerWatts,f32Array"`
	// The remaining-time fields carry the collector's -1 sentinel for "unknown,
	// or not in the state this field measures", which is why they are signed.
	SecondsToFull  []int64 `lw:"sysmBatterySecondsToFull,i64Array"`
	SecondsToEmpty []int64 `lw:"sysmBatterySecondsToEmpty,i64Array"`

	// Mains adapters — a second group, with its own length.
	AcName   []string `lw:"sysmAcAdapterName,symbolArray"`
	AcOnline []uint8  `lw:"sysmAcAdapterOnline,u8Array,ct=u8h"`
}
