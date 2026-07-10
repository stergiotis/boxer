//go:build compile_fail_probe

// This file is compiled only under -tags=compile_fail_probe. It exists in
// order to FAIL to compile: it is exactly the bypass an external caller would
// attempt against a private-control store (ADR-0100 SD6), and each numbered
// line below is a compile error, which is the proof the wall holds.
// TestControlBypassDoesNotCompile builds this package with the probe tag and
// asserts the failure.
package bypassguard

import (
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore/example"
	// (2) Internal rule: a package rooted outside example/ may not import this.
	"github.com/stergiotis/boxer/public/storage/recordstore/example/internal/lowlevel"
)

func attemptBypass() {
	st := example.NewDeviceStore(nil, nil, example.DeviceStoreConfig{})
	raw := st.Begin(1, time.Unix(0, 1)).Raw()

	// (1) The control methods are unexported, so this is undefined.
	_ = raw.CommitEntity()

	// (2) The drivers live in the internal package this file cannot import.
	_ = lowlevel.InEntityDeviceTableCommitEntity(raw)
}
