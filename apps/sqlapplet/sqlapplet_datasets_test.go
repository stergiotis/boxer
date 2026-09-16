package sqlapplet

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRenderDatasetNotice pins the text the applet shows over empty panes:
// the alias, because that is what the failing query and the catalog name,
// and the author's hint, because nothing else can say how to produce it.
func TestRenderDatasetNotice(t *testing.T) {
	assert.Nil(t, renderDatasetNotice(nil, "hint"), "a cleared condition renders nothing")

	one := string(renderDatasetNotice([]string{"pprof_cpu"}, "Capture one from imzrt → Profiles → Capture CPU."))
	assert.Contains(t, one, "Waiting for dataset `pprof_cpu`")
	assert.Contains(t, one, "Capture one from imzrt")
	assert.Contains(t, one, "no need to reopen")

	many := string(renderDatasetNotice([]string{"a", "b"}, ""))
	assert.Contains(t, many, "Waiting for datasets `a`, `b`")
}
