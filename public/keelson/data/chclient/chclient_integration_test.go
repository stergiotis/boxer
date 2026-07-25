//go:build integration

package chclient

import (
	"context"
	"testing"
)

// TestPing_LiveServer exercises Ping against the project's localhost CH.
//
// It lives in the integration lane (ENGINEERING_PRACTICES §4) rather than
// beside the httptest-backed unit tests: it needs a real server, and the lane
// runs serially because its members share one. The rest of chclient_test.go
// stands up its own httptest server per test and stays in the default lane.
func TestPing_LiveServer(t *testing.T) {
	c := New(Defaults(), nil)
	if err := c.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", Defaults().URL, err)
	}
}
