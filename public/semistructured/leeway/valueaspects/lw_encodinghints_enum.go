package valueaspects

import (
	"slices"
)

const (
	AspectNone                             AspectE = 0
	AspectScaleOfMeasurementNominal        AspectE = 1
	AspectScaleOfMeasurementOrdinal        AspectE = 2
	AspectScaleOfMeasurementMetricInterval AspectE = 3
	AspectScaleOfMeasurementMetricRatio    AspectE = 4

	AspectUnitDurationSI          AspectE = 5
	AspectUnitLengthSI            AspectE = 6
	AspectUnitMassSI              AspectE = 7
	AspectUnitElectricCurrentSI   AspectE = 8
	AspectUnitTemperatureSI       AspectE = 9
	AspectUnitMoleSI              AspectE = 10
	AspectUnitLuminousIntensitySI AspectE = 11

	AspectUnitPlaneAngleSI          AspectE = 12
	AspectUnitSolidAngleSI          AspectE = 13
	AspectUnitFrequencySI           AspectE = 14
	AspectUnitForceSI               AspectE = 15
	AspectUnitPressureSI            AspectE = 16
	AspectUnitEnergySI              AspectE = 17
	AspectUnitPowerSI               AspectE = 18
	AspectUnitElectricChargeSI      AspectE = 19
	AspectUnitVoltageSI             AspectE = 20
	AspectUnitCapacitanceSI         AspectE = 21
	AspectUnitResistanceSI          AspectE = 22
	AspectUnitConductanceSI         AspectE = 23
	AspectUnitMagneticFluxSI        AspectE = 24
	AspectUnitMagneticFluxDensitySI AspectE = 25
	AspectUnitInductanceSI          AspectE = 26
	AspectUnitTemperatureCelsiusSI  AspectE = 27
	AspectUnitLuminousFluxSI        AspectE = 28
	AspectUnitIlluminanceSI         AspectE = 29
	AspectUnitRadioactiveActivitySI AspectE = 30
	AspectUnitAbsorbedDoseSI        AspectE = 31
	AspectUnitDoseEquivalentSI      AspectE = 32
	AspectUnitCatalyticActivitySI   AspectE = 33

	AspectUnitAccelerationSI      AspectE = 34
	AspectUnitAreaSI              AspectE = 35
	AspectUnitComputationSI       AspectE = 36
	AspectUnitDataTransferBitsSI  AspectE = 37
	AspectUnitDataTransferBytesSI AspectE = 38
	AspectUnitConcentrationSI     AspectE = 39
	AspectUnitFlowSI              AspectE = 40
	AspectUnitRotationalSpeedSI   AspectE = 41
	AspectUnitVelocitySI          AspectE = 42
	AspectUnitVolumeSI            AspectE = 43
	AspectUnitTorqueSI            AspectE = 44

	AspectVectorValue        AspectE = 45
	AspectCanonicalizedValue AspectE = 46

	AspectApplicationLevelEncrypted AspectE = 47

	AspectHumanReadable   AspectE = 48
	AspectMachineReadable AspectE = 49

	AspectUltraShortLifespan AspectE = 50
	AspectShortLifespan      AspectE = 51
	AspectMediumLifespan     AspectE = 52
	AspectLongLifespan       AspectE = 53
	AspectUltraLongLifespan  AspectE = 54

	AspectJsonScalar AspectE = 55
	AspectJsonArray  AspectE = 56
	AspectJsonObject AspectE = 57
	AspectJson       AspectE = 58
	AspectCborScalar AspectE = 59
	AspectCborArray  AspectE = 60
	AspectCborMap    AspectE = 61
	AspectCbor       AspectE = 62

	AspectUrl AspectE = 63 // follow the WHATWG recommendation to forget URI and use URL (see https://url.spec.whatwg.org/#goals)

	//AspectFeature                   AspectE = 64
	//AspectFeatureOneHot             AspectE = 65
	//AspectFeatureScalingStandardN01 AspectE = 66
	//AspectFeatureScalingMinMax01    AspectE = 67
	//AspectFeatureScalingRobust01    AspectE = 68
	//AspectFeatureBinarized          AspectE = 69
	//AspectFeatureOrdinal            AspectE = 71
	//AspectFeatureLabel              AspectE = 72

	//AspectIdNaturalKey AspectE = 73
	//AspectIdSurrogateKey AspectE = 74
	//AspectIdDurableSuperNaturalKey AspectE = 75
	//AspectIdContentAddressableKey AspectE = 76
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectNone,
	AspectScaleOfMeasurementNominal,
	AspectScaleOfMeasurementOrdinal,
	AspectScaleOfMeasurementMetricInterval,
	AspectScaleOfMeasurementMetricRatio,

	AspectUnitDurationSI,
	AspectUnitLengthSI,
	AspectUnitMassSI,
	AspectUnitElectricCurrentSI,
	AspectUnitTemperatureSI,
	AspectUnitMoleSI,
	AspectUnitLuminousIntensitySI,

	AspectUnitPlaneAngleSI,
	AspectUnitSolidAngleSI,
	AspectUnitFrequencySI,
	AspectUnitForceSI,
	AspectUnitPressureSI,
	AspectUnitEnergySI,
	AspectUnitPowerSI,
	AspectUnitElectricChargeSI,
	AspectUnitVoltageSI,
	AspectUnitCapacitanceSI,
	AspectUnitResistanceSI,
	AspectUnitConductanceSI,
	AspectUnitMagneticFluxSI,
	AspectUnitMagneticFluxDensitySI,
	AspectUnitInductanceSI,
	AspectUnitTemperatureCelsiusSI,
	AspectUnitLuminousFluxSI,
	AspectUnitIlluminanceSI,
	AspectUnitRadioactiveActivitySI,
	AspectUnitAbsorbedDoseSI,
	AspectUnitDoseEquivalentSI,
	AspectUnitCatalyticActivitySI,

	AspectUnitAccelerationSI,
	AspectUnitAreaSI,
	AspectUnitComputationSI,
	AspectUnitDataTransferBitsSI,
	AspectUnitDataTransferBytesSI,
	AspectUnitConcentrationSI,
	AspectUnitFlowSI,
	AspectUnitRotationalSpeedSI,
	AspectUnitVelocitySI,
	AspectUnitVolumeSI,
	AspectUnitTorqueSI,

	AspectVectorValue,
	AspectCanonicalizedValue,

	AspectApplicationLevelEncrypted,

	AspectHumanReadable,
	AspectMachineReadable,

	AspectUltraShortLifespan,
	AspectShortLifespan,
	AspectMediumLifespan,
	AspectLongLifespan,
	AspectUltraLongLifespan,

	AspectJsonScalar,
	AspectJsonArray,
	AspectJsonObject,
	AspectJson,
	AspectCborScalar,
	AspectCborArray,
	AspectCborMap,
	AspectCbor,

	AspectUrl,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool {
	return inst < MaxAspectExcl
}
func (inst AspectE) String() string {
	switch inst {
	case AspectNone:
		return "none"
	case AspectScaleOfMeasurementNominal:
		return "scale-of-measurement-nominal"
	case AspectScaleOfMeasurementOrdinal:
		return "scale-of-measurement-ordinal"
	case AspectScaleOfMeasurementMetricInterval:
		return "scale-of-measurement-metric-interval"
	case AspectScaleOfMeasurementMetricRatio:
		return "scale-of-measurement-metric-ratio"
	case AspectUnitDurationSI:
		return "unit-duration-si"
	case AspectUnitLengthSI:
		return "unit-length-si"
	case AspectUnitMassSI:
		return "unit-mass-si"
	case AspectUnitElectricCurrentSI:
		return "unit-electric-current-si"
	case AspectUnitTemperatureSI:
		return "unit-temperature-si"
	case AspectUnitMoleSI:
		return "unit-mole-si"
	case AspectUnitLuminousIntensitySI:
		return "unit-luminous-intensity-si"

	case AspectUnitPlaneAngleSI:
		return "unit-plane-angle-si"
	case AspectUnitSolidAngleSI:
		return "unit-solid-angle-si"
	case AspectUnitFrequencySI:
		return "unit-frequency-si"
	case AspectUnitForceSI:
		return "unit-force-si"
	case AspectUnitPressureSI:
		return "unit-pressure-si"
	case AspectUnitEnergySI:
		return "unit-energy-si"
	case AspectUnitPowerSI:
		return "unit-power-si"
	case AspectUnitElectricChargeSI:
		return "unit-electric-charge-si"
	case AspectUnitVoltageSI:
		return "unit-voltage-si"
	case AspectUnitCapacitanceSI:
		return "unit-capacitance-si"
	case AspectUnitResistanceSI:
		return "unit-resistance-si"
	case AspectUnitConductanceSI:
		return "unit-conductance-si"
	case AspectUnitMagneticFluxSI:
		return "unit-magnetic-flux-si"
	case AspectUnitMagneticFluxDensitySI:
		return "unit-magnetic-flux-density-si"
	case AspectUnitInductanceSI:
		return "unit-inductance-si"
	case AspectUnitTemperatureCelsiusSI:
		return "unit-temperature-celsius-si"
	case AspectUnitLuminousFluxSI:
		return "unit-luminous-flux-si"
	case AspectUnitIlluminanceSI:
		return "unit-illuminance-si"
	case AspectUnitRadioactiveActivitySI:
		return "unit-radioactive-activity-si"
	case AspectUnitAbsorbedDoseSI:
		return "unit-absorbed-dose-si"
	case AspectUnitDoseEquivalentSI:
		return "unit-dose-equivalent-si"
	case AspectUnitCatalyticActivitySI:
		return "unit-catalytic-activity-si"

	case AspectUnitAccelerationSI:
		return "unit-acceleration-si"
	case AspectUnitAreaSI:
		return "unit-area-si"
	case AspectUnitComputationSI:
		return "unit-computation-si"
	case AspectUnitDataTransferBitsSI:
		return "unit-data-transfer-bits-si"
	case AspectUnitDataTransferBytesSI:
		return "unit-data-transfer-bytes-si"
	case AspectUnitConcentrationSI:
		return "unit-data-concentration-si"
	case AspectUnitFlowSI:
		return "unit-flow-si"
	case AspectUnitRotationalSpeedSI:
		return "unit-rotation-speed-si"
	case AspectUnitVelocitySI:
		return "unit-velocity-si"
	case AspectUnitVolumeSI:
		return "unit-volume-si"
	case AspectUnitTorqueSI:
		return "unit-torque-si"

	case AspectVectorValue:
		return "vector-value"
	case AspectCanonicalizedValue:
		return "canonicalized-value"
	case AspectApplicationLevelEncrypted:
		return "application-level-encrypted"
	case AspectHumanReadable:
		return "human-readable"
	case AspectMachineReadable:
		return "machine-readable"
	case AspectUltraShortLifespan:
		return "ultra-short-lifespan"
	case AspectShortLifespan:
		return "short-lifespan"
	case AspectMediumLifespan:
		return "medium-lifespan"
	case AspectLongLifespan:
		return "long-lifespan"
	case AspectUltraLongLifespan:
		return "ultra-long-lifespan"
	case AspectJsonScalar:
		return "json-scalar"
	case AspectJsonArray:
		return "json-array"
	case AspectJsonObject:
		return "json-object"
	case AspectJson:
		return "json"
	case AspectCborScalar:
		return "cbor-scalar"
	case AspectCborArray:
		return "cbor-array"
	case AspectCborMap:
		return "cbor-map"
	case AspectCbor:
		return "cbor"
	case AspectUrl:
		return "url"
	}
	return InvalidAspectEnumValueString
}
func (inst AspectE) Value() uint8 {
	return uint8(inst)
}
