//go:build !bootstrap

package implot

func GetCompatibilityRecordBase64() string {
	return fffiCompatibilityRecord
}
