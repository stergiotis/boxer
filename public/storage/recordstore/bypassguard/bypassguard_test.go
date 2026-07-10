package bypassguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/storage/recordstore/example"
	"github.com/stretchr/testify/require"
)

// TestRawControlIsWalled proves the method-set half of the wall (ADR-0100
// SD6): a Raw() builder held by an external package does not satisfy any
// interface naming a control method — the methods are unexported, so a type
// assertion for one returns ok=false. The three checked here have fully
// public signatures (Builder, CommitEntity, TransferRecords), so they are
// exactly the ones a cast could otherwise recover if the wall were an
// interface rather than unexported methods. The safe section surface, by
// contrast, stays reachable.
func TestRawControlIsWalled(t *testing.T) {
	st := example.NewDeviceStore(nil, nil, example.DeviceStoreConfig{})
	defer st.Close()
	raw := st.Begin(1, time.Unix(0, 1)).Raw()

	// Raw() hands back the concrete builder (an un-nameable *lowlevel type).
	// The cast attack boxes it and asserts to a locally-declared interface;
	// with the control methods unexported, every such assertion returns false.
	boxed := any(raw)
	if _, ok := boxed.(interface{ CommitEntity() error }); ok {
		t.Error("CommitEntity is reachable through Raw() — the control wall is open")
	}
	if _, ok := boxed.(interface {
		TransferRecords([]arrow.RecordBatch) ([]arrow.RecordBatch, error)
	}); ok {
		t.Error("TransferRecords is reachable through Raw()")
	}
	if _, ok := boxed.(interface{ Builder() *array.RecordBuilder }); ok {
		t.Error("Builder is reachable through Raw() — the raw Arrow builder escapes")
	}

	// The safe attribute surface stays reachable: that this call compiles and
	// returns a usable section is the intended escape hatch working.
	require.NotNil(t, raw.GetSectionSymbol(), "the section surface must stay reachable")
}

// TestControlBypassDoesNotCompile proves the compile-time half of the wall:
// bypass_attempt.go (built only under -tags=compile_fail_probe) tries to
// import the internal drivers and call an unexported control method, and must
// fail to build. We build this package with the probe tag and assert the
// failure carries the internal-import error.
func TestControlBypassDoesNotCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the go-build probe in -short mode")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(thisFile)

	// The build needs the repo's own tags (the store's deps are tag-gated);
	// find them at the nearest go.mod and append the probe tag.
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		require.NotEqualf(t, parent, root, "no go.mod found above %s", dir)
		root = parent
	}
	tagBytes, err := os.ReadFile(filepath.Join(root, "tags"))
	require.NoError(t, err)
	tags := strings.TrimSpace(string(tagBytes))
	if tags != "" {
		tags += ","
	}
	tags += "compile_fail_probe"

	cmd := exec.Command("go", "build", "-tags="+tags, "./")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.Errorf(t, err, "the bypass attempt must not compile; build output:\n%s", out)
	require.Containsf(t, string(out), "internal",
		"expected an internal-package import error; build output:\n%s", out)
}
