package play

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Play's own writes (ADR-0270 §SD7): pins (boxer.resultsets, boxer.pin_*)
// and Series verdicts (boxer.tslabels) are play's bookkeeping, not a
// statement the person wrote. Each of their statements takes a dispatch
// decision, as a run does, and passes play's own gate for them: the
// BOXER_PLAY_APP_WRITES allowance, the confined label of the data it
// carries, and the grant of an agent that caused it. BOXER_PLAY_ALLOW_WRITES
// keeps governing the person's own INSERT and DDL.
//
// deferred: the statements still travel on play's HTTP transport to the
// decided target, not through queryengine's Deliver, because a request has
// no way to carry an insert body there (chserver refuses Inputs). Moving
// them needs a body on queryengine.Request.

// appWriteLabel is what one of play's own writes carries to its gate.
type appWriteLabel struct {
	// confined says the data the write carries came from a confined
	// result (ADR-0145): it goes only where sealed plaintext may.
	confined bool
	// agent is the context of the task whose call caused the write; nil
	// for the person's.
	agent *app.OnBehalfOf
}

// appStatement sends one statement of play's own writes, with body as its
// insert data (nil for none), and returns the response body.
func (inst *Client) appStatement(ctx context.Context, sql string, body io.Reader, label appWriteLabel) (raw []byte, err error) {
	if inst.cfg.AppWritesOff {
		err = eh.Errorf("play's own writes are off (BOXER_PLAY_APP_WRITES=off)")
		return
	}
	// The statement is final, so it is placed as it is: the client-side
	// rewrites Dispatch runs are for the person's SQL.
	dec := inst.dispatchResidual(sql, "")
	target, err := dec.target()
	if err != nil {
		return
	}
	if (label.confined || dec.sensitivity == queryengine.SensitivityConfined) && !inst.servesConfined(target) {
		err = eb.Build().Str("endpoint", target).Errorf("play: refusing to write confined data to an endpoint not declared as allowed to see sealed data")
		return
	}
	if label.agent != nil && dec.class != dispatchClassIntrospection {
		if dest := DestinationClickHouse(endpointHost(target)); !slices.Contains(label.agent.Destinations, dest) {
			err = &AgentLimitError{Reason: "the grant does not list " + dest}
			return
		}
	}
	raw, err = inst.postStatement(ctx, target, sql, body)
	return
}

// appRead reads back what play's own writes wrote, from where the same
// decision sends them, so a read never looks at another endpoint than the
// writes reached. A read needs no allowance.
func (inst *Client) appRead(ctx context.Context, sql string) (raw []byte, err error) {
	dec := inst.dispatchResidual(sql, "")
	target, err := dec.target()
	if err != nil {
		return
	}
	if dec.sensitivity == queryengine.SensitivityConfined && !inst.servesConfined(target) {
		err = eb.Build().Str("endpoint", target).Errorf("play: refusing a confined read on an endpoint not declared as allowed to see sealed data")
		return
	}
	raw, err = inst.postStatement(ctx, target, sql, nil)
	return
}

// servesConfined reports whether target may see sealed plaintext
// (ADR-0145 §SD5): it is this process's plane, or it has demonstrated it
// can fetch from that plane. Derived from the target, never from a
// decision's label.
func (inst *Client) servesConfined(target string) (ok bool) {
	ok = target == introspect.LocalQueryEndpoint() || inst.reach.isProven(target)
	return
}

// postStatement POSTs sql to endpoint, as the URL's query parameter with
// body as the data (the ClickHouse HTTP convention for an insert), and
// returns the response body.
func (inst *Client) postStatement(ctx context.Context, endpoint string, sql string, body io.Reader) (raw []byte, err error) {
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	if body == nil {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+sep+"query="+url.QueryEscape(sql), body)
	if err != nil {
		err = eh.Errorf("unable to build request: %w", err)
		return
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if inst.cfg.User != "" {
		req.Header.Set("X-ClickHouse-User", inst.cfg.User)
	}
	if inst.cfg.Password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.cfg.Password)
	}
	resp, err := inst.http.Do(req)
	if err != nil {
		err = eh.Errorf("request failed: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", strings.TrimSpace(string(msg))).Errorf("http")
		return
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		err = eh.Errorf("unable to read response: %w", err)
	}
	return
}
