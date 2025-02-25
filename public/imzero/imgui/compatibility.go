//go:build !bootstrap

package imgui

func GetCompatibilityRecordBase64() string {
	return fffiCompatibilityRecord
}
