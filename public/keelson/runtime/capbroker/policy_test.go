package capbroker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestAutoApprovePolicy_AlwaysGrants(t *testing.T) {
	p := &AutoApprovePolicy{}
	cases := []GrantRequest{
		{AppId: "a", SubjectFilter: app.SubjectFilter{Pattern: "x", Direction: app.CapDirectionPub}},
		{AppId: "b", SubjectFilter: app.SubjectFilter{Pattern: "fs.>", Direction: app.CapDirectionBoth}},
	}
	for _, req := range cases {
		d := p.Decide(req)
		assert.True(t, d.Granted)
		assert.NotEmpty(t, d.Reason)
	}
}

func TestDenyAllPolicy_AlwaysDenies(t *testing.T) {
	p := &DenyAllPolicy{}
	d := p.Decide(GrantRequest{AppId: "a"})
	assert.False(t, d.Granted)
	assert.NotEmpty(t, d.Reason)
}

func TestFuncPolicy_DispatchesToFunc(t *testing.T) {
	var calls int
	p := FuncPolicy(func(req GrantRequest) (d GrantDecision) {
		calls++
		if req.AppId == "ok" {
			d = GrantDecision{Granted: true, Reason: "match"}
			return
		}
		d = GrantDecision{Granted: false, Reason: "no match"}
		return
	})
	assert.True(t, p.Decide(GrantRequest{AppId: "ok"}).Granted)
	assert.False(t, p.Decide(GrantRequest{AppId: "nope"}).Granted)
	assert.Equal(t, 2, calls)
}
