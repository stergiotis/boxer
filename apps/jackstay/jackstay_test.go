package jackstay

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
)

func TestProposePlanNameSkipsTakenNames(t *testing.T) {
	src := jk.Endpoint{URL: "http://a.example:8123/"}
	dst := jk.Endpoint{URL: "http://b.example:8123/"}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	base := "2026-09-28-a.example-8123-to-b.example-8123"

	assert.Equal(t, base+".json", proposePlanName(src, dst, now, nil))
	taken := []fsbroker.AppDataEntry{{Name: base + ".json"}, {Name: base + "-2.json"}}
	name := proposePlanName(src, dst, now, taken)
	assert.Equal(t, base+"-3.json", name)
	assert.True(t, fsbroker.ValidAppDataName(name), "a proposed name is one the data area accepts")
}

// Whatever the picked file was called, the imported plan gets a name the data
// area accepts.
func TestImportBaseMakesADataAreaName(t *testing.T) {
	for _, display := range []string{"plan.json", "my plan (copy).json", ".hidden.json", ".json", "", "noext"} {
		base, ext := importBase(display)
		assert.Truef(t, fsbroker.ValidAppDataName(base+ext), "%q → %q", display, base+ext)
	}
	base, ext := importBase("prod-to-replica.json")
	assert.Equal(t, "prod-to-replica.json", base+ext)
}
