package units

import (
	"slices"
)

const (
	AspectNone AspectE = 0

	AspectDurationSI          AspectE = 1
	AspectLengthSI            AspectE = 2
	AspectMassSI              AspectE = 3
	AspectElectricCurrentSI   AspectE = 4
	AspectTemperatureSI       AspectE = 5
	AspectMoleSI              AspectE = 6
	AspectLuminousIntensitySI AspectE = 7

	AspectPlaneAngleSI          AspectE = 8
	AspectSolidAngleSI          AspectE = 9
	AspectFrequencySI           AspectE = 10
	AspectForceSI               AspectE = 11
	AspectPressureSI            AspectE = 12
	AspectEnergySI              AspectE = 13
	AspectPowerSI               AspectE = 14
	AspectElectricChargeSI      AspectE = 15
	AspectVoltageSI             AspectE = 16
	AspectCapacitanceSI         AspectE = 17
	AspectResistanceSI          AspectE = 18
	AspectConductanceSI         AspectE = 19
	AspectMagneticFluxSI        AspectE = 20
	AspectMagneticFluxDensitySI AspectE = 21
	AspectInductanceSI          AspectE = 22
	AspectTemperatureCelsiusSI  AspectE = 23
	AspectLuminousFluxSI        AspectE = 24
	AspectIlluminanceSI         AspectE = 25
	AspectRadioactiveActivitySI AspectE = 26
	AspectAbsorbedDoseSI        AspectE = 27
	AspectDoseEquivalentSI      AspectE = 28
	AspectCatalyticActivitySI   AspectE = 29

	AspectAccelerationSI      AspectE = 30
	AspectAreaSI              AspectE = 31
	AspectComputationSI       AspectE = 32
	AspectDataTransferBitsSI  AspectE = 33
	AspectDataTransferBytesSI AspectE = 34
	AspectConcentrationSI     AspectE = 35
	AspectFlowSI              AspectE = 36
	AspectRotationalSpeedSI   AspectE = 37
	AspectVelocitySI          AspectE = 38
	AspectVolumeSI            AspectE = 39
	AspectTorqueSI            AspectE = 40
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectNone,
	AspectDurationSI,
	AspectLengthSI,
	AspectMassSI,
	AspectElectricCurrentSI,
	AspectTemperatureSI,
	AspectMoleSI,
	AspectLuminousIntensitySI,

	AspectPlaneAngleSI,
	AspectSolidAngleSI,
	AspectFrequencySI,
	AspectForceSI,
	AspectPressureSI,
	AspectEnergySI,
	AspectPowerSI,
	AspectElectricChargeSI,
	AspectVoltageSI,
	AspectCapacitanceSI,
	AspectResistanceSI,
	AspectConductanceSI,
	AspectMagneticFluxSI,
	AspectMagneticFluxDensitySI,
	AspectInductanceSI,
	AspectTemperatureCelsiusSI,
	AspectLuminousFluxSI,
	AspectIlluminanceSI,
	AspectRadioactiveActivitySI,
	AspectAbsorbedDoseSI,
	AspectDoseEquivalentSI,
	AspectCatalyticActivitySI,

	AspectAccelerationSI,
	AspectAreaSI,
	AspectComputationSI,
	AspectDataTransferBitsSI,
	AspectDataTransferBytesSI,
	AspectConcentrationSI,
	AspectFlowSI,
	AspectRotationalSpeedSI,
	AspectVelocitySI,
	AspectVolumeSI,
	AspectTorqueSI,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool {
	return inst < MaxAspectExcl
}
func (inst AspectE) String() string {
	switch inst {
	case AspectNone:
		return "none"
	case AspectDurationSI:
		return "duration-si"
	case AspectLengthSI:
		return "length-si"
	case AspectMassSI:
		return "mass-si"
	case AspectElectricCurrentSI:
		return "electric-current-si"
	case AspectTemperatureSI:
		return "temperature-si"
	case AspectMoleSI:
		return "mole-si"
	case AspectLuminousIntensitySI:
		return "luminous-intensity-si"
	case AspectPlaneAngleSI:
		return "plane-angle-si"
	case AspectSolidAngleSI:
		return "solid-angle-si"
	case AspectFrequencySI:
		return "frequency-si"
	case AspectForceSI:
		return "force-si"
	case AspectPressureSI:
		return "pressure-si"
	case AspectEnergySI:
		return "energy-si"
	case AspectPowerSI:
		return "power-si"
	case AspectElectricChargeSI:
		return "electric-charge-si"
	case AspectVoltageSI:
		return "voltage-si"
	case AspectCapacitanceSI:
		return "capacitance-si"
	case AspectResistanceSI:
		return "resistance-si"
	case AspectConductanceSI:
		return "conductance-si"
	case AspectMagneticFluxSI:
		return "magnetic-flux-si"
	case AspectMagneticFluxDensitySI:
		return "magnetic-flux-density-si"
	case AspectInductanceSI:
		return "inductance-si"
	case AspectTemperatureCelsiusSI:
		return "temperature-celsius-si"
	case AspectLuminousFluxSI:
		return "luminous-flux-si"
	case AspectIlluminanceSI:
		return "illuminance-si"
	case AspectRadioactiveActivitySI:
		return "radioactive-activity-si"
	case AspectAbsorbedDoseSI:
		return "absorbed-dose-si"
	case AspectDoseEquivalentSI:
		return "dose-equivalent-si"
	case AspectCatalyticActivitySI:
		return "catalytic-activity-si"

	case AspectAccelerationSI:
		return "acceleration-si"
	case AspectAreaSI:
		return "area-si"
	case AspectComputationSI:
		return "computation-si"
	case AspectDataTransferBitsSI:
		return "data-transfer-bits-si"
	case AspectDataTransferBytesSI:
		return "data-transfer-bytes-si"
	case AspectConcentrationSI:
		return "data-concentration-si"
	case AspectFlowSI:
		return "flow-si"
	case AspectRotationalSpeedSI:
		return "rotation-speed-si"
	case AspectVelocitySI:
		return "velocity-si"
	case AspectVolumeSI:
		return "volume-si"
	case AspectTorqueSI:
		return "torque-si"
	}
	return InvalidAspectEnumValueString
}
func (inst AspectE) Value() uint8 {
	return uint8(inst)
}
