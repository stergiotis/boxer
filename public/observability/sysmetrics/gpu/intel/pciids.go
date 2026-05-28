//go:build linux && gpu_intel && llm_generated_opus47

package intel

import "strings"

// pciIDNames is a small subset of i915_pciids.h covering the modern
// Intel GPU codenames we expect users to encounter on contemporary
// hardware. Unknown ids fall back to "Intel Graphics".
//
// Maintenance: keep this small — the kernel header has thousands of
// rows; we mirror only mainline-shipping desktop and mobile parts.
// Extend as the user base hits "0x????" name fallbacks.
var pciIDNames = map[string]string{
	// Tiger Lake (Gen 12)
	"0x9a40": "Tiger Lake-LP GT2",
	"0x9a49": "Tiger Lake-H GT2",
	"0x9a78": "Tiger Lake-H GT1",
	"0x9ac0": "Tiger Lake-U GT2",
	"0x9ac9": "Tiger Lake-U GT2",

	// Rocket Lake (Gen 12)
	"0x4c8a": "Rocket Lake-S GT1",
	"0x4c8b": "Rocket Lake-S GT0.5",
	"0x4c90": "Rocket Lake-S GT1",

	// Alder Lake (Gen 12 / Xe-LP)
	"0x4626": "Alder Lake-P GT2",
	"0x4628": "Alder Lake-P GT1",
	"0x462a": "Alder Lake-P GT0.5",
	"0x4680": "Alder Lake-S GT1",
	"0x4682": "Alder Lake-S GT0.5",
	"0x4688": "Alder Lake-S GT1",
	"0x468a": "Alder Lake-S GT0.5",
	"0x4690": "Alder Lake-S GT1",
	"0x4692": "Alder Lake-S GT0.5",
	"0x4693": "Alder Lake-S GT0.5",
	"0xa780": "Raptor Lake-S GT1",
	"0xa783": "Raptor Lake-S GT0.5",
	"0xa788": "Raptor Lake-S GT1",
	"0xa78a": "Raptor Lake-S GT0.5",
	"0xa721": "Raptor Lake-P GT2",

	// Arc / Meteor Lake (Xe-HPG / Xe-LPG)
	"0x56a0": "Arc A770 (DG2-512)",
	"0x56a1": "Arc A750 (DG2-512)",
	"0x56a5": "Arc A380 (DG2-128)",
	"0x56a6": "Arc A310 (DG2-128)",
	"0x7d40": "Meteor Lake-P GT2",
	"0x7d45": "Meteor Lake-P GT1",
	"0x7d55": "Meteor Lake-P GT2",
	"0x7d60": "Meteor Lake-U GT1",
	"0x7dd5": "Meteor Lake-P GT2",

	// Older but still common
	"0x3e90": "Coffee Lake-S GT1",
	"0x3e91": "Coffee Lake-S GT2",
	"0x3e92": "Coffee Lake-S GT2",
	"0x3e98": "Coffee Lake-S GT2",
	"0x3e9a": "Coffee Lake-H GT2",
	"0x3e9b": "Coffee Lake-H GT2",
	"0x3ea0": "Whiskey Lake-U GT2",
	"0x3ea5": "Coffee Lake-U GT3e",
	"0x9bc4": "Comet Lake-H GT2",
	"0x9bc5": "Comet Lake-S GT2",
	"0x9bca": "Comet Lake-U GT2",
	"0x9bcc": "Comet Lake-U GT2",
}

// pciIDName returns the codename for a /sys/class/drm/card*/device/device
// reading. Lookups are case-insensitive — callers commonly hand us the
// raw "0x9A49" form, the lowercase "0x9a49" form, or just "9a49".
func pciIDName(rawID string) (name string) {
	id := strings.ToLower(strings.TrimSpace(rawID))
	if !strings.HasPrefix(id, "0x") {
		id = "0x" + id
	}
	if n, ok := pciIDNames[id]; ok {
		return n
	}
	return "Intel Graphics"
}
