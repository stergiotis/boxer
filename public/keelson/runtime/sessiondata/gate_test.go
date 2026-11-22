package sessiondata

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nestedComment hides an expression from boxer's grammar, which ends a
// block comment at its first `*/`, inside a quoted alias; ClickHouse nests
// block comments, ends this one after `"x */`, and so reads the expression
// as part of the SELECT.
const nestedComment = "SELECT 1 /* /* */ AS \"x */ , throwIf(1, 'escaped the check') AS z --\"\nFROM numbers(1)"

// TestTheCreateCarriesNoCommentTheServerCouldReadOtherwise: a definition
// the server would tokenise differently is refused, and what the capability
// wraps is the SELECT's tokens without comments, proven a read again.
func TestTheCreateCarriesNoCommentTheServerCouldReadOtherwise(t *testing.T) {
	_, err := checkRead(nestedComment)
	assert.ErrorContains(t, err, "not a read")

	cr, err := checkRead("SELECT a /* why */ FROM t -- trailing\nWHERE b = '/* not a comment */' // also\n")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a   FROM t  WHERE b = '/* not a comment */'", cr.inner)
	assert.NotContains(t, cr.inner, "--")

	r := newRig(t, Limits{})
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: r.fullAgent("t1"), Endpoint: r.endpoint(), Name: "a", SQL: nestedComment})
	assert.Error(t, err)
	assert.Empty(t, r.stub.creates(), "nothing the server would read otherwise is sent")
	r.define(r.fullAgent("t1"), "b", "SELECT 1 /* why */ AS x")
	assert.NotContains(t, r.stub.lastCreate().SQL, "/*", "no comment reaches the server")
}

// TestAnAgentsPreparedStatementMayNotNestAComment: the caller sends the
// prepared statement as the task's own, so a statement the server would
// read differently from its parse is refused.
func TestAnAgentsPreparedStatementMayNotNestAComment(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	_, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(),
		SQL: "SELECT x /* /* */ AS \"y */ , 2 AS z --\"\nFROM session('a')"})
	assert.ErrorContains(t, err, "not a read")
	b, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT x /* fine */ FROM session('a')"})
	require.NoError(t, err)
	require.NoError(t, r.svc.Release(person, b.Lease))
}

// TestAnEndpointURLMayNotCarryTheStatement: the server reads a `query`
// URL parameter as the start of the statement, and the capability sets its
// statements' other parameters itself.
func TestAnEndpointURLMayNotCarryTheStatement(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	for _, suffix := range []string{"/?query=DROP%20TABLE%20t%20--", "/?readonly=0", "/?log_comment=x", "/?param_x=1", "/?t_structure=x"} {
		e := r.endpoint()
		e.URL += suffix
		_, err := r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: e, Name: "a", SQL: "SELECT 1 AS x"})
		assert.ErrorContains(t, err, "may not carry", suffix)
		_, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: e, SQL: "SELECT * FROM session('a')"})
		assert.ErrorContains(t, err, "may not carry", suffix)
	}
	e := r.endpoint()
	e.URL = "file:///etc/passwd"
	_, err := r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: e, Name: "a", SQL: "SELECT 1 AS x"})
	assert.Error(t, err)
	assert.Empty(t, r.stub.creates())
	e = r.endpoint()
	e.URL += "/?database=enron"
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: e, Name: "a", SQL: "SELECT 1 AS x"})
	require.NoError(t, err, "a parameter of the connection's own is the caller's")
}

// TestIsSessionLostReadsARunsError: a run through the server engine fails
// with the server's diagnostic as text; the how-to tells a caller to test
// it with IsSessionLost.
func TestIsSessionLostReadsARunsError(t *testing.T) {
	run := errors.New("clickhouse http 404: Code: 372. DB::Exception: Session 5f2 not found. (SESSION_NOT_FOUND)")
	assert.True(t, IsSessionLost(run))
	assert.False(t, IsSessionBusy(run))
	assert.True(t, IsSessionBusy(errors.New("clickhouse http 500: Code: 373. DB::Exception: Session is locked")))
	assert.True(t, IsSessionLost(&ServerError{Status: 404, Code: 372}))
	assert.False(t, IsSessionLost(errors.New("request failed: connection refused")))
	assert.False(t, IsSessionLost(nil))
}
