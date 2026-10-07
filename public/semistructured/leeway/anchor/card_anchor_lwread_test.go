package anchor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwread"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// TestReadModelOverTheAnchorBatch reads the membership demo batch through
// the read model (ADR-0289 §SD3) with the anchor's ref formatter:
// every attribute is named by what it is — a ref by the formatter's name, an
// attribute with no membership by its section — never by its position, and
// the bytes natural key labels its record as text.
func TestReadModelOverTheAnchorBatch(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tblDesc, clickhouse.NewTechnologySpecificCodeGenerator()))
	driver, err := streamreadaccess.NewDriver(&tblDesc, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)

	recs := buildMembershipDemoBatch(t)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	s := lwread.NewSink(lwread.Options{Renderer: membership.NewRenderer(anchorRefFormatter{}, nil, nil)})
	require.NoError(t, driver.DriveRecordBatch(s, recs[0]))
	m := s.Model()
	m.Qualify()
	require.Len(t, m.Records, 2)

	names := func(r lwread.Record) map[string]lwread.Attribute {
		out := map[string]lwread.Attribute{}
		for _, a := range r.Attributes {
			assert.NotEmpty(t, a.Name)
			out[a.Name] = a
		}
		return out
	}
	drone := names(m.Records[0])
	assert.Equal(t, "TRK-DEMO-1", m.Records[0].Label)
	require.Contains(t, drone, "model:AeroQuad")
	assert.Equal(t, "IN_TRANSIT", drone["model:AeroQuad"].Values[0].Items[0].Text)
	assert.Contains(t, drone, "customer:42")

	incident := names(m.Records[1])
	assert.Equal(t, "INC-DEMO-2", m.Records[1].Label)
	assert.Contains(t, incident, "port:22")
	assert.Contains(t, incident, "port:443")
	assert.Contains(t, incident, "timeRange", "an attribute with no membership is named by its section")

	for _, s := range m.Header() {
		if s.Name == "port:22" {
			assert.Equal(t, "LW_GET('symbol', 22)", s.Handle)
		}
	}
}
