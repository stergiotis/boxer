//go:build linux && gpu_intel && llm_generated_opus47

package intel

// Test-only re-exports of unexported helpers so the external _test
// package can drive PMU-config-id math without surfacing the encoding
// constants in the public API.

func ConfigEngineBusyTest(class, instance uint64) uint64 {
	return configEngineBusy(class, instance)
}

func ConfigFreqActualTest() uint64 {
	return configFreqActual
}

func ConfigFreqRequestedTest() uint64 {
	return configFreqRequested
}
