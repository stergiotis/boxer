package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestAgentLimits(t *testing.T) {
	remote := dispatchDecision{targetURL: "http://db.example:8123/", class: dispatchClassManual}
	local := dispatchDecision{class: dispatchClassIntrospection}
	grant := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps"), DestinationClickHouse("db.example:8123")}}
	narrow := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps")}}
	var limit *AgentLimitError
	for name, tc := range map[string]struct {
		sql  string
		dec  dispatchDecision
		obo  *app.OnBehalfOf
		pass bool
	}{
		"a read the grant covers":            {"SELECT 1", remote, grant, true},
		"an introspection read":              {"SELECT * FROM keelson('apps')", local, narrow, true},
		"a keelson table outside the grant":  {"SELECT * FROM keelson('windows')", local, grant, false},
		"an endpoint outside the grant":      {"SELECT 1", remote, narrow, false},
		"an egress read":                     {"SELECT * FROM url('http://x/', 'LineAsString')", remote, grant, false},
		"a write":                            {"INSERT INTO t SELECT 1", remote, grant, false},
		"a settings change":                  {"SET max_threads = 1; SELECT 1", remote, grant, false},
		"a statement that does not classify": {"DROP TABLE t", remote, grant, false},
	} {
		err := checkAgentLimits(tc.sql, tc.dec, tc.obo)
		if tc.pass {
			assert.NoError(t, err, name)
			continue
		}
		require.Error(t, err, name)
		assert.ErrorAs(t, err, &limit, name)
	}
}
